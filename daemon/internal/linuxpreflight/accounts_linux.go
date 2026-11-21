//go:build linux && amd64

package linuxpreflight

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

type OwnerAccounts struct {
	A        *user.User
	B        *user.User
	ASubject ACLSubject
	BSubject ACLSubject
}

func VerifyOwnerIdentity(owner linuxgate.OwnerConfig, uid, euid, gid, egid uint32) error {
	if err := owner.Validate(); err != nil {
		return fmt.Errorf("validate root-installed Linux A/B identities: %w", err)
	}
	return validateLinuxOwnerProcessIDs(owner.AUID, owner.AGID, owner.BUID, owner.BGID, uid, euid, gid, egid)
}

func validateLinuxOwnerProcessIDs(aUID, aGID, bUID, bGID, uid, euid, gid, egid uint32) error {
	if uid != aUID || euid != aUID || gid != aGID || egid != aGID || aUID == 0 || bUID == 0 || aUID == bUID || aGID == 0 || bGID == 0 || aGID == bGID {
		return errors.New("Linux owner preflight process does not run with the exact configured non-root A UID/GID and distinct B identity")
	}
	return nil
}

func VerifyOwnerAccounts(owner linuxgate.OwnerConfig) (OwnerAccounts, error) {
	a, err := user.LookupId(strconv.FormatUint(uint64(owner.AUID), 10))
	if err != nil {
		return OwnerAccounts{}, fmt.Errorf("lookup configured Linux owner A account: %w", err)
	}
	b, err := user.LookupId(strconv.FormatUint(uint64(owner.BUID), 10))
	if err != nil {
		return OwnerAccounts{}, fmt.Errorf("lookup configured fixed native B account: %w", err)
	}
	aUID, aUIDErr := strconv.ParseUint(a.Uid, 10, 32)
	aGID, aGIDErr := strconv.ParseUint(a.Gid, 10, 32)
	bUID, bUIDErr := strconv.ParseUint(b.Uid, 10, 32)
	bGID, bGIDErr := strconv.ParseUint(b.Gid, 10, 32)
	if aUIDErr != nil || aGIDErr != nil || bUIDErr != nil || bGIDErr != nil {
		return OwnerAccounts{}, errors.New("passwd account UID/GID values are invalid")
	}
	if err := validateLinuxOwnerAccountMapping(owner.AUID, owner.AGID, owner.BUID, owner.BGID, uint32(aUID), uint32(aGID), uint32(bUID), uint32(bGID)); err != nil {
		return OwnerAccounts{}, err
	}
	aSubject, err := ACLSubjectForAccount(a)
	if err != nil {
		return OwnerAccounts{}, fmt.Errorf("decode configured A supplementary groups: %w", err)
	}
	bSubject, err := ACLSubjectForAccount(b)
	if err != nil {
		return OwnerAccounts{}, fmt.Errorf("decode configured B supplementary groups: %w", err)
	}
	if aSubject.hasGroup(owner.BGID) || bSubject.hasGroup(owner.AGID) {
		return OwnerAccounts{}, errors.New("Linux A and B account group memberships overlap the other identity's primary group")
	}
	aGroup, err := user.LookupGroupId(strconv.FormatUint(uint64(owner.AGID), 10))
	if err != nil || aGroup.Gid != strconv.FormatUint(uint64(owner.AGID), 10) {
		return OwnerAccounts{}, errors.New("configured A service group is missing or ambiguous")
	}
	if err := verifyLinuxDedicatedGroupMembership(a, owner.AGID, aSubject, bSubject); err != nil {
		return OwnerAccounts{}, err
	}
	return OwnerAccounts{A: a, B: b, ASubject: aSubject, BSubject: bSubject}, nil
}

func verifyLinuxDedicatedGroupMembership(a *user.User, aGID uint32, aSubject, bSubject ACLSubject) error {
	if a == nil || a.Username == "" {
		return errors.New("A account name is unavailable for dedicated-group verification")
	}
	if err := verifyLinuxTrustedHostExecutable("/usr/bin/getent", aSubject, bSubject); err != nil {
		return fmt.Errorf("verify trusted group lookup utility: %w", err)
	}
	passwd, err := runLinuxIdentityLookup("passwd")
	if err != nil {
		return fmt.Errorf("enumerate passwd entries to revalidate the dedicated A group: %w", err)
	}
	group, err := runLinuxIdentityLookup("group", strconv.FormatUint(uint64(aGID), 10))
	if err != nil {
		return fmt.Errorf("look up dedicated A group members: %w", err)
	}
	aUID, err := strconv.ParseUint(a.Uid, 10, 32)
	if err != nil {
		return errors.New("A UID is invalid during dedicated-group verification")
	}
	if err := validateLinuxDedicatedGroupDatabases(passwd, group, a.Username, uint32(aUID), aGID); err != nil {
		return err
	}
	return nil
}

func runLinuxIdentityLookup(arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/getent", arguments...)
	command.Env = []string{"HOME=/", "PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	var stdout, stderr boundedLinuxOutput
	stdout.limit = 8 << 20
	stderr.limit = 1 << 20
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if stdout.tooLarge || stderr.tooLarge || errors.Is(err, errLinuxPreflightOutputTooLarge) {
			return nil, errors.New("identity lookup output exceeded its preflight bound")
		}
		return nil, fmt.Errorf("getent %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func validateLinuxDedicatedGroupDatabases(passwd, group []byte, aName string, aUID, aGID uint32) error {
	passwdScanner := bufio.NewScanner(bytes.NewReader(passwd))
	passwdScanner.Buffer(make([]byte, 4096), 8<<20)
	aEntryCount := 0
	for lineNumber := 1; passwdScanner.Scan(); lineNumber++ {
		fields := strings.Split(passwdScanner.Text(), ":")
		if len(fields) != 7 || fields[0] == "" {
			return fmt.Errorf("passwd entry %d has an unsupported shape", lineNumber)
		}
		uid, uidErr := strconv.ParseUint(fields[2], 10, 32)
		gid, gidErr := strconv.ParseUint(fields[3], 10, 32)
		if uidErr != nil || gidErr != nil {
			return fmt.Errorf("passwd entry %d has invalid numeric IDs", lineNumber)
		}
		if uint32(uid) == aUID && fields[0] != aName {
			return errors.New("A UID is assigned to an unexpected passwd account")
		}
		if fields[0] == aName {
			aEntryCount++
			if uint32(uid) != aUID || uint32(gid) != aGID {
				return errors.New("A passwd identity differs from the configured dedicated service group")
			}
		}
		if uint32(gid) == aGID && fields[0] != aName {
			return fmt.Errorf("A primary group is shared with another passwd account %q", fields[0])
		}
	}
	if err := passwdScanner.Err(); err != nil {
		return err
	}
	if aEntryCount != 1 {
		return errors.New("A passwd entry is missing or ambiguous during dedicated-group verification")
	}

	groupScanner := bufio.NewScanner(bytes.NewReader(group))
	groupScanner.Buffer(make([]byte, 4096), 1<<20)
	groupEntryCount := 0
	for lineNumber := 1; groupScanner.Scan(); lineNumber++ {
		fields := strings.Split(groupScanner.Text(), ":")
		if len(fields) != 4 || fields[0] == "" {
			return fmt.Errorf("group entry %d has an unsupported shape", lineNumber)
		}
		gid, gidErr := strconv.ParseUint(fields[2], 10, 32)
		if gidErr != nil {
			return fmt.Errorf("group entry %d has an invalid GID", lineNumber)
		}
		if uint32(gid) != aGID {
			continue
		}
		groupEntryCount++
		for _, member := range strings.Split(fields[3], ",") {
			if member != "" && member != aName {
				return fmt.Errorf("A service group has an untrusted supplementary member %q", member)
			}
		}
	}
	if err := groupScanner.Err(); err != nil {
		return err
	}
	if groupEntryCount != 1 {
		return errors.New("A service group is missing or ambiguous during dedicated-group verification")
	}
	return nil
}

func validateLinuxOwnerAccountMapping(aUID, aGID, bUID, bGID, accountAUID, accountAGID, accountBUID, accountBGID uint32) error {
	if aUID != accountAUID || aGID != accountAGID || bUID != accountBUID || bGID != accountBGID {
		return errors.New("passwd account UID/GID values differ from the root-installed A/B mapping")
	}
	return nil
}

func ACLSubjectForAccount(account *user.User) (ACLSubject, error) {
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return ACLSubject{}, errors.New("account UID is invalid")
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return ACLSubject{}, errors.New("account primary GID is invalid")
	}
	// B's gate clears supplementary groups, but other sessions of the shared B
	// account may retain them. Host access checks must include the account's
	// actual supplementary memberships rather than assume the gate's group list.
	groupIDs, err := account.GroupIds()
	if err != nil {
		return ACLSubject{}, fmt.Errorf("read account supplementary groups: %w", err)
	}
	groups := make([]uint32, 0, len(groupIDs)+1)
	groups = append(groups, uint32(gid))
	for _, groupID := range groupIDs {
		parsed, err := strconv.ParseUint(groupID, 10, 32)
		if err != nil {
			return ACLSubject{}, errors.New("account supplementary GID is invalid")
		}
		groups = append(groups, uint32(parsed))
	}
	return ACLSubject{UID: uint32(uid), GID: uint32(gid), Groups: groups}, nil
}

func verifyLinuxOwnerAccountHome(accounts OwnerAccounts, harnessKind string, excludedPaths ...string) error {
	if accounts.A == nil || accounts.B == nil {
		return errors.New("fixed A/B account metadata is unavailable")
	}
	aUID, err := strconv.ParseUint(accounts.A.Uid, 10, 32)
	if err != nil {
		return errors.New("configured A account has an invalid UID")
	}
	aGID, err := strconv.ParseUint(accounts.A.Gid, 10, 32)
	if err != nil {
		return errors.New("configured A account has an invalid primary GID")
	}
	aHome, err := LstatCanonicalPath(accounts.A.HomeDir)
	if err != nil {
		return fmt.Errorf("inspect configured A HOME: %w", err)
	}
	aHomeIdentity, err := FileIdentity(aHome)
	if err != nil || !aHome.IsDir() || aHomeIdentity.UID != uint32(aUID) || aHomeIdentity.GID != uint32(aGID) {
		return errors.New("configured A HOME must be a canonical A-owned directory")
	}
	if err := VerifyEffectiveAccess(accounts.A.HomeDir, accounts.ASubject, 7, 0); err != nil {
		return fmt.Errorf("A cannot read/search/update its HOME: %w", err)
	}
	if err := VerifyEffectiveAccess(accounts.A.HomeDir, accounts.BSubject, 0, 7); err != nil {
		return fmt.Errorf("B can access A HOME or credentials: %w", err)
	}
	if err := VerifyNoUntrustedACLWrite(accounts.A.HomeDir, []uint32{0, uint32(aUID)}, []uint32{uint32(aGID)}, true); err != nil {
		return fmt.Errorf("A HOME is changeable by an untrusted identity: %w", err)
	}
	if err := VerifyDefaultAccess(accounts.A.HomeDir, accounts.BSubject, 0, 7); err != nil {
		return fmt.Errorf("future A HOME entries are accessible to B: %w", err)
	}
	if err := verifyLinuxParentsNotWritable(accounts.A.HomeDir, accounts.BSubject); err != nil {
		return fmt.Errorf("B can replace A HOME: %w", err)
	}
	bUID, err := strconv.ParseUint(accounts.B.Uid, 10, 32)
	if err != nil {
		return errors.New("fixed B account has an invalid UID")
	}
	bGID, err := strconv.ParseUint(accounts.B.Gid, 10, 32)
	if err != nil {
		return errors.New("fixed B account has an invalid primary GID")
	}
	if accounts.B.HomeDir == "" || !filepath.IsAbs(accounts.B.HomeDir) || filepath.Clean(accounts.B.HomeDir) != accounts.B.HomeDir || accounts.B.HomeDir == "/" {
		return errors.New("fixed B account lacks a canonical operator-provisioned HOME")
	}
	for _, excluded := range excludedPaths {
		if excluded != "" && pathsOverlapLinux(accounts.B.HomeDir, excluded) {
			return fmt.Errorf("fixed B HOME must remain outside StateDir and managed workspace paths: %s", excluded)
		}
	}
	home, err := LstatCanonicalPath(accounts.B.HomeDir)
	if err != nil {
		return fmt.Errorf("inspect fixed B HOME: %w", err)
	}
	homeIdentity, err := FileIdentity(home)
	if err != nil || !home.IsDir() || homeIdentity.UID != uint32(bUID) || homeIdentity.GID != uint32(bGID) {
		return errors.New("fixed B HOME must be a B-owned directory on the configured primary group")
	}
	b := accounts.BSubject
	a := accounts.ASubject
	if err := VerifyPathTraversal(accounts.B.HomeDir, b); err != nil {
		return fmt.Errorf("B cannot traverse the path to its HOME: %w", err)
	}
	if err := VerifyPathTraversal(accounts.B.HomeDir, a); err != nil {
		return fmt.Errorf("A cannot stat known B HOME credential/session paths: %w", err)
	}
	if err := VerifyEffectiveAccess(accounts.B.HomeDir, b, 7, 0); err != nil {
		return fmt.Errorf("fixed B cannot read, search, and update its HOME: %w", err)
	}
	if err := VerifyEffectiveAccess(accounts.B.HomeDir, a, 1, 6); err != nil {
		return fmt.Errorf("A must be able to stat known B credential/session paths without listing or reading B HOME: %w", err)
	}
	credential, err := linuxNativeCredentialPath(accounts.B.HomeDir, harnessKind)
	if err != nil {
		return err
	}
	for path := filepath.Dir(credential); path != accounts.B.HomeDir; path = filepath.Dir(path) {
		info, err := LstatCanonicalPath(path)
		if err != nil {
			return fmt.Errorf("inspect B credential directory %s: %w", path, err)
		}
		identity, err := FileIdentity(info)
		if err != nil || !info.IsDir() || identity.UID != uint32(bUID) || identity.GID != uint32(bGID) {
			return fmt.Errorf("B credential directory %s must be owned by B", path)
		}
		if err := VerifyEffectiveAccess(path, b, 7, 0); err != nil {
			return fmt.Errorf("B cannot read/search/update credential directory %s: %w", path, err)
		}
		if err := VerifyEffectiveAccess(path, a, 1, 6); err != nil {
			return fmt.Errorf("A cannot stat known credential path without accessing B directory contents %s: %w", path, err)
		}
		if filepath.Dir(path) == path {
			return errors.New("B credential path escaped HOME")
		}
	}
	credentialInfo, err := LstatCanonicalPath(credential)
	if err != nil {
		return fmt.Errorf("inspect configured B native credential file: %w", err)
	}
	credentialIdentity, err := FileIdentity(credentialInfo)
	if err != nil || !credentialInfo.Mode().IsRegular() || credentialIdentity.UID != uint32(bUID) || credentialIdentity.GID != uint32(bGID) || credentialIdentity.Links != 1 || credentialInfo.Size() <= 0 || credentialInfo.Size() > linuxOwnerCredentialFile {
		return errors.New("native credential file must be a nonempty, singly-linked B-owned regular file")
	}
	if err := VerifyEffectiveAccess(credential, b, 6, 0); err != nil {
		return fmt.Errorf("configured native credential file is not read/write accessible to B: %w", err)
	}
	if err := VerifyEffectiveAccess(credential, a, 0, 6); err != nil {
		return fmt.Errorf("native credential file is readable or writable by host A: %w", err)
	}
	return nil
}

func linuxNativeCredentialPath(home, kind string) (string, error) {
	var relative string
	switch kind {
	case config.RuntimeHarnessCodex:
		relative = ".codex/auth.json"
	case config.RuntimeHarnessClaudeCode:
		relative = ".claude/.credentials.json"
	case config.RuntimeHarnessPi:
		relative = ".pi/agent/auth.json"
	case config.RuntimeHarnessOpenCode:
		relative = ".local/share/opencode/auth.json"
	default:
		return "", fmt.Errorf("native credentials for harness %q have no read-only preflight contract", kind)
	}
	return filepath.Join(home, relative), nil
}
