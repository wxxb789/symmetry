//go:build linux && amd64

package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const (
	linuxCgroupMountMaxBytes      = 1 << 20
	linuxGitConfigMaxBytes        = 1 << 20
	linuxOwnerStateDirName        = "runs"
	linuxOwnerLockFile            = ".symmetry-daemon.lock"
	linuxOwnerJournalPrefix       = "journal-"
	linuxOwnerJournalSuffix       = ".json"
	linuxOwnerCredentialFile      = 64 << 10
	linuxSystemdUnitMaxBytes      = 64 << 10
	linuxSystemdShowMaxBytes      = 256 << 10
	linuxClonePreflightMaxEntries = 250_000
	linuxClonePreflightMaxDepth   = 32
)

func init() {
	normalizeLinuxNativeStateDir = canonicalLinuxOwnerStateDir
}

// LinuxOwnerPreflightRuntimeBoundary documents what static installation
// verification cannot prove. The runtime must set native HOME to B, verify the
// A-created leaf's cgroup.kill through its pinned FD, and validate per-run Git
// metadata before binding or entering the native gate.
const LinuxOwnerPreflightRuntimeBoundary = "read-only preflight does not launch B, certify a future cgroup leaf, or attest a future per-run Git metadata ACL"

// VerifyLegacyLinuxOwnersDrained takes the daemon's exclusive Store lock and
// verifies that every RunJournal is terminal and has no remaining process or
// containment authority. It never stops a process, creates a receipt, mutates
// a journal, or retires an outbox item; the old service topology must have
// completed any v1 helper recovery first.
func VerifyLegacyLinuxOwnersDrained(value config.Config) (result error) {
	if uint32(os.Geteuid()) == 0 || uint32(os.Getuid()) != uint32(os.Geteuid()) || uint32(os.Getgid()) != uint32(os.Getegid()) {
		return errors.New("legacy Linux owner drain verification must run as the unprivileged daemon owner")
	}
	uid := uint32(os.Geteuid())
	if err := verifyExistingLinuxStateLayout(value.StateDir, uid, nil); err != nil {
		return fmt.Errorf("verify private state before legacy owner drain: %w", err)
	}
	store, err := state.New(value.StateDir)
	if err != nil {
		return fmt.Errorf("acquire exclusive state Store lock for legacy owner drain: %w", err)
	}
	defer func() { result = errors.Join(result, store.Close()) }()
	return verifyDrainedLinuxStore(store, value.StateDir)
}

// VerifyLinuxOwnerInstallation performs the post-anchor, read-only host
// preflight. The installed A/B mapping comes only from the strict root-owned
// owner configuration; the cgroup path and socket come from the operator's
// daemon configuration and must match the live authenticated anchor.
func VerifyLinuxOwnerInstallation(value config.Config) (result error) {
	if err := value.Validate(); err != nil {
		return fmt.Errorf("validate Linux owner daemon configuration: %w", err)
	}
	stateDir, err := canonicalLinuxOwnerStateDir(value.StateDir)
	if err != nil {
		return fmt.Errorf("canonicalize configured Linux owner StateDir: %w", err)
	}
	value.StateDir = stateDir
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		return fmt.Errorf("read root-installed Linux A/B owner configuration: %w", err)
	}
	if err := verifyLinuxOwnerIdentity(owner, uint32(os.Getuid()), uint32(os.Geteuid()), uint32(os.Getgid()), uint32(os.Getegid())); err != nil {
		return err
	}
	accounts, err := verifyLinuxOwnerAccounts(owner)
	if err != nil {
		return err
	}
	if err := verifyLinuxNativeRuntimeSelection(value); err != nil {
		return err
	}
	if err := verifyLinuxOwnerStateDirBinding(value.StateDir, owner); err != nil {
		return fmt.Errorf("verify configured StateDir against root-installed owner authority: %w", err)
	}
	if err := verifyExistingLinuxStateLayout(value.StateDir, owner.AUID, &accounts.bSubject); err != nil {
		return fmt.Errorf("verify A-private state and B isolation: %w", err)
	}
	// Legacy drain is a one-time cutover check under the old topology. Normal
	// daemon restarts must admit live v2 journals so the surviving A helper and
	// recovery-only path can stop them. This installation check also runs from
	// an already-running daemon holding the Store lock during native admission.
	if err := verifyInstalledLinuxOwnerAssets(owner, accounts.aSubject, accounts.bSubject); err != nil {
		return err
	}

	anchor, err := discoverLinuxOwnerAnchor(value.LinuxOwnerSocket, owner)
	if err != nil {
		return err
	}
	parent, err := openConfiguredLinuxOwnerParent(value, owner, anchor)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, parent.Close()) }()
	if err := verifyLinuxOwnerParentKillBoundary(value.LinuxOwnerCgroupMount, value.LinuxOwnerDelegatedPath, owner.AUID, accounts.aSubject, accounts.bSubject); err != nil {
		return err
	}

	workspace := value.Workspaces[value.Runtime.Workspace]
	if err := verifyLinuxOwnerAccountHome(accounts, value.Runtime.HarnessKind, value.StateDir, workspace.Root, workspace.SeedRoot, workspace.Repository); err != nil {
		return err
	}
	if err := verifyLinuxOwnerPiShare(value, owner, accounts); err != nil {
		return err
	}
	if err := verifyLinuxSystemdUnits(value, accounts); err != nil {
		return err
	}
	if err := verifyLinuxOwnerAnchorProcessContext(anchor.Process, accounts); err != nil {
		return fmt.Errorf("verify live owner anchor privilege state: %w", err)
	}
	if err := verifyLinuxOwnerWorkspace(value, owner, accounts); err != nil {
		return err
	}
	return nil
}

type linuxOwnerAccounts struct {
	a        *user.User
	b        *user.User
	aSubject linuxACLSubject
	bSubject linuxACLSubject
}

func verifyLinuxOwnerIdentity(owner linuxgate.OwnerConfig, uid, euid, gid, egid uint32) error {
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

func verifyLinuxOwnerAccounts(owner linuxgate.OwnerConfig) (linuxOwnerAccounts, error) {
	a, err := user.LookupId(strconv.FormatUint(uint64(owner.AUID), 10))
	if err != nil {
		return linuxOwnerAccounts{}, fmt.Errorf("lookup configured Linux owner A account: %w", err)
	}
	b, err := user.LookupId(strconv.FormatUint(uint64(owner.BUID), 10))
	if err != nil {
		return linuxOwnerAccounts{}, fmt.Errorf("lookup configured fixed native B account: %w", err)
	}
	aUID, aUIDErr := strconv.ParseUint(a.Uid, 10, 32)
	aGID, aGIDErr := strconv.ParseUint(a.Gid, 10, 32)
	bUID, bUIDErr := strconv.ParseUint(b.Uid, 10, 32)
	bGID, bGIDErr := strconv.ParseUint(b.Gid, 10, 32)
	if aUIDErr != nil || aGIDErr != nil || bUIDErr != nil || bGIDErr != nil {
		return linuxOwnerAccounts{}, errors.New("passwd account UID/GID values are invalid")
	}
	if err := validateLinuxOwnerAccountMapping(owner.AUID, owner.AGID, owner.BUID, owner.BGID, uint32(aUID), uint32(aGID), uint32(bUID), uint32(bGID)); err != nil {
		return linuxOwnerAccounts{}, err
	}
	aSubject, err := linuxACLSubjectForAccount(a)
	if err != nil {
		return linuxOwnerAccounts{}, fmt.Errorf("decode configured A supplementary groups: %w", err)
	}
	bSubject, err := linuxACLSubjectForAccount(b)
	if err != nil {
		return linuxOwnerAccounts{}, fmt.Errorf("decode configured B supplementary groups: %w", err)
	}
	if aSubject.hasGroup(owner.BGID) || bSubject.hasGroup(owner.AGID) {
		return linuxOwnerAccounts{}, errors.New("Linux A and B account group memberships overlap the other identity's primary group")
	}
	aGroup, err := user.LookupGroupId(strconv.FormatUint(uint64(owner.AGID), 10))
	if err != nil || aGroup.Gid != strconv.FormatUint(uint64(owner.AGID), 10) {
		return linuxOwnerAccounts{}, errors.New("configured A service group is missing or ambiguous")
	}
	if err := verifyLinuxDedicatedGroupMembership(a, owner.AGID, aSubject, bSubject); err != nil {
		return linuxOwnerAccounts{}, err
	}
	return linuxOwnerAccounts{a: a, b: b, aSubject: aSubject, bSubject: bSubject}, nil
}

func verifyLinuxDedicatedGroupMembership(a *user.User, aGID uint32, aSubject, bSubject linuxACLSubject) error {
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

func linuxACLSubjectForAccount(account *user.User) (linuxACLSubject, error) {
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return linuxACLSubject{}, errors.New("account UID is invalid")
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return linuxACLSubject{}, errors.New("account primary GID is invalid")
	}
	// B's gate clears supplementary groups, but other sessions of the shared B
	// account may retain them. Host access checks must include the account's
	// actual supplementary memberships rather than assume the gate's group list.
	groupIDs, err := account.GroupIds()
	if err != nil {
		return linuxACLSubject{}, fmt.Errorf("read account supplementary groups: %w", err)
	}
	groups := make([]uint32, 0, len(groupIDs)+1)
	groups = append(groups, uint32(gid))
	for _, groupID := range groupIDs {
		parsed, err := strconv.ParseUint(groupID, 10, 32)
		if err != nil {
			return linuxACLSubject{}, errors.New("account supplementary GID is invalid")
		}
		groups = append(groups, uint32(parsed))
	}
	return linuxACLSubject{uid: uint32(uid), gid: uint32(gid), groups: groups}, nil
}

func verifyLinuxNativeRuntimeSelection(value config.Config) error {
	kind, err := configuredHarnessKind(value.Runtime.HarnessKind)
	if err != nil {
		return fmt.Errorf("select configured Linux native harness: %w", err)
	}
	if !nativeGoalHarnessKind(kind) || kind == "generic" {
		return fmt.Errorf("Linux owner preflight rejects generic/native-fallback harness selection %q", value.Runtime.HarnessKind)
	}
	if value.Runtime.HarnessVersion == "legacy" || value.Runtime.AdapterVersion == "legacy" || value.Runtime.AdapterProtocolVersion <= 0 {
		return errors.New("Linux owner preflight requires explicit native harness and adapter protocol versions")
	}
	workspace, ok := value.Workspaces[value.Runtime.Workspace]
	if !ok || workspace.Policy != config.WorkspacePolicyGitCloneV2 || workspace.Path != "" || workspace.Ref != "" || workspace.SeedRoot == "" {
		return errors.New("Linux v2 owner preflight requires a complete git_clone_v2 Repository/SeedRoot/Root binding")
	}
	profile := value.AgentProfiles[value.Runtime.AgentProfile]
	for _, name := range profile.EnvAllowlist {
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "GIT_") || linuxCredentialEnvironmentName(upper) || upper == "HOME" || strings.HasPrefix(upper, "XDG_") ||
			upper == "CODEX_HOME" || upper == "CLAUDE_CONFIG_DIR" || upper == "OPENCODE_CONFIG_DIR" || upper == "PI_CODING_AGENT_DIR" {
			return fmt.Errorf("native environment allowlist forwards host Git settings, credentials, or home paths through %q", name)
		}
	}
	if value.LinuxOwnerCgroupMount == "" || !filepath.IsAbs(value.LinuxOwnerCgroupMount) || filepath.Clean(value.LinuxOwnerCgroupMount) != value.LinuxOwnerCgroupMount ||
		value.LinuxOwnerDelegatedPath == "" || filepath.IsAbs(value.LinuxOwnerDelegatedPath) || filepath.Clean(value.LinuxOwnerDelegatedPath) != value.LinuxOwnerDelegatedPath ||
		value.LinuxOwnerDelegatedPath == "." || value.LinuxOwnerDelegatedPath == ".." || strings.HasPrefix(value.LinuxOwnerDelegatedPath, ".."+string(filepath.Separator)) ||
		value.LinuxOwnerSocket == "" || !filepath.IsAbs(value.LinuxOwnerSocket) || filepath.Clean(value.LinuxOwnerSocket) != value.LinuxOwnerSocket {
		return errors.New("Linux owner cgroup mount, delegated path, and anchor socket must be explicitly configured and canonical")
	}
	return nil
}

func linuxCredentialEnvironmentName(name string) bool {
	return strings.HasSuffix(name, "_API_KEY") || strings.HasSuffix(name, "_TOKEN") || strings.HasSuffix(name, "_SECRET") ||
		strings.HasSuffix(name, "_PASSWORD") || strings.HasSuffix(name, "_CREDENTIAL") || strings.HasSuffix(name, "_ACCESS_KEY") ||
		strings.HasSuffix(name, "_KEY_ID") || strings.HasSuffix(name, "_CLIENT_SECRET")
}

func discoverLinuxOwnerAnchor(socketPath string, owner linuxgate.OwnerConfig) (platform.LinuxOwnerAnchorIdentity, error) {
	client, err := platform.NewLinuxOwnerAnchorClient(socketPath)
	if err != nil {
		return platform.LinuxOwnerAnchorIdentity{}, fmt.Errorf("construct authenticated Linux owner anchor client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	anchor, err := client.Discover(ctx)
	if err != nil {
		return platform.LinuxOwnerAnchorIdentity{}, fmt.Errorf("discover live Linux owner anchor: %w", err)
	}
	if err := validateDiscoveredLinuxOwnerAnchor(anchor, owner.AUID); err != nil {
		return platform.LinuxOwnerAnchorIdentity{}, err
	}
	return anchor, nil
}

func validateDiscoveredLinuxOwnerAnchor(anchor platform.LinuxOwnerAnchorIdentity, ownerUID uint32) error {
	if anchor.Process.Validate() != nil || anchor.Parent.Validate() != nil || anchor.OwnerUID != ownerUID ||
		anchor.ManagerPath == "/" || !canonicalLinuxCgroupPath(anchor.ManagerPath) || anchor.Parent.Path != anchor.ManagerPath {
		return errors.New("live Linux owner anchor identity or manager subgroup differs from fixed A configuration")
	}
	return nil
}

func openConfiguredLinuxOwnerParent(value config.Config, owner linuxgate.OwnerConfig, anchor platform.LinuxOwnerAnchorIdentity) (*platform.LinuxCgroupV2Parent, error) {
	parent, err := platform.OpenLinuxCgroupV2Parent(value.LinuxOwnerCgroupMount, value.LinuxOwnerDelegatedPath)
	if err != nil {
		return nil, fmt.Errorf("open configured delegated cgroup v2 manager subgroup as A: %w", err)
	}
	if err := validateLinuxOwnerParentIdentity(parent.Identity(), parent.OwnerUID(), owner.AUID, anchor); err != nil {
		return nil, errors.Join(err, parent.Close())
	}
	return parent, nil
}

func validateLinuxOwnerParentIdentity(identity authority.LinuxCgroupParentIdentity, parentUID, ownerUID uint32, anchor platform.LinuxOwnerAnchorIdentity) error {
	if parentUID != ownerUID || identity.Path != anchor.ManagerPath || !identity.Equal(anchor.Parent) {
		return errors.New("configured delegated parent identity differs from the live owner anchor's exact manager subgroup")
	}
	return nil
}

func verifyLinuxOwnerParentKillBoundary(mountPoint, delegatedPath string, ownerUID uint32, a, b linuxACLSubject) error {
	mountFD, err := unix.Open(mountPoint, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open configured cgroup2 mount for read-only parent kill check: %w", err)
	}
	defer unix.Close(mountFD)
	parentHow := &unix.OpenHow{Flags: uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV)}
	parentFD, err := unix.Openat2(mountFD, filepath.ToSlash(delegatedPath), parentHow)
	if err != nil {
		return fmt.Errorf("open exact delegated manager subgroup for read-only ACL checks: %w", err)
	}
	defer unix.Close(parentFD)
	var parentStat unix.Stat_t
	if err := unix.Fstat(parentFD, &parentStat); err != nil {
		return fmt.Errorf("stat delegated manager subgroup: %w", err)
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(parentFD, &filesystem); err != nil || uint64(filesystem.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) {
		return errors.New("configured manager subgroup is not on a verified cgroup v2 filesystem")
	}
	if parentStat.Uid != ownerUID || parentStat.Mode&unix.S_IFMT != unix.S_IFDIR || parentStat.Mode&0022 != 0 || a.uid != ownerUID || b.uid == ownerUID {
		return errors.New("delegated manager parent must be A-owned with no group/other write access")
	}
	for _, control := range []struct {
		name           string
		ownerUID       uint32
		mustBeWritable bool
		allowAOwner    bool
	}{
		{name: "cgroup.procs", ownerUID: ownerUID, mustBeWritable: true},
		{name: "cgroup.subtree_control", ownerUID: ownerUID, mustBeWritable: true},
		{name: "cgroup.kill", ownerUID: 0, mustBeWritable: true},
		{name: "cgroup.events", ownerUID: 0, allowAOwner: true},
		{name: "cgroup.controllers", ownerUID: 0, allowAOwner: true},
	} {
		how := &unix.OpenHow{Flags: uint64(unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV)}
		fd, openErr := unix.Openat2(parentFD, control.name, how)
		if openErr != nil {
			return fmt.Errorf("open manager %s metadata read-only: %w", control.name, openErr)
		}
		var stat unix.Stat_t
		statErr := unix.Fstat(fd, &stat)
		closeErr := unix.Close(fd)
		ownerMatches := stat.Uid == control.ownerUID || control.allowAOwner && stat.Uid == ownerUID
		if statErr != nil || closeErr != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || !ownerMatches || stat.Mode&0022 != 0 || control.mustBeWritable && stat.Mode&0200 == 0 {
			return fmt.Errorf("manager control %s is not owned/writable only by its trusted owner", control.name)
		}
		if control.name == "cgroup.kill" {
			if err := validateLinuxOwnerManagerKillMetadata(stat.Uid, stat.Gid, stat.Mode, ownerUID, a, b); err != nil {
				return err
			}
		}
	}
	writeHow := &unix.OpenHow{Flags: uint64(unix.O_WRONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK), Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV)}
	writeFD, writeErr := unix.Openat2(parentFD, "cgroup.kill", writeHow)
	if writeErr == nil {
		_ = unix.Close(writeFD)
		return errors.New("A can open the manager cgroup.kill for writing; the parent kill control must remain root-only")
	}
	if !errors.Is(writeErr, unix.EACCES) && !errors.Is(writeErr, unix.EPERM) {
		return fmt.Errorf("cannot establish root-only manager cgroup.kill permissions: %w", writeErr)
	}
	return nil
}

func validateLinuxOwnerManagerKillMetadata(uid, gid, mode, ownerUID uint32, a, b linuxACLSubject) error {
	if uid != 0 || gid != 0 || mode&unix.S_IFMT != unix.S_IFREG || mode&0022 != 0 || ownerUID == 0 || a.uid != ownerUID || b.uid == ownerUID {
		return errors.New("delegated manager cgroup.kill must remain root-owned and unavailable to A/B write access")
	}
	return nil
}

func verifyInstalledLinuxOwnerAssets(owner linuxgate.OwnerConfig, a, b linuxACLSubject) error {
	for _, path := range []string{"/usr", "/usr/libexec", "/usr/libexec/symmetry", "/etc", "/etc/symmetry"} {
		info, err := lstatCanonicalLinuxPath(path)
		if err != nil {
			return fmt.Errorf("inspect root-installed owner asset directory %s: %w", path, err)
		}
		identity, err := linuxFileIdentity(info)
		if err != nil || !info.IsDir() || identity.uid != 0 || identity.gid != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("root-installed owner asset directory %s is not root-owned and non-writable by A/B", path)
		}
		if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0}, nil, true); err != nil {
			return fmt.Errorf("root-installed owner asset directory %s has an untrusted ACL writer: %w", path, err)
		}
		if err := verifyEffectiveLinuxAccess(path, a, 0, 2); err != nil {
			return fmt.Errorf("A can modify trusted owner asset directory %s: %w", path, err)
		}
	}
	for _, path := range []string{"/usr", "/usr/libexec", "/usr/libexec/symmetry", "/etc", "/etc/symmetry"} {
		if err := verifyEffectiveLinuxAccess(path, b, 0, 2); err != nil {
			return fmt.Errorf("verify fixed B cannot modify trusted directory %s: %w", path, err)
		}
	}
	if err := verifyTrustedLinuxConfig(owner, a, b); err != nil {
		return err
	}
	if err := verifyTrustedLinuxAsset("/usr/libexec/symmetry/symmetry-linux-launcher", trustedLinuxAsset{uid: 0, gid: owner.AGID, mode: 0o4750, links: 1, denyBAll: true}, a, b); err != nil {
		return fmt.Errorf("verify setuid Linux owner wrapper: %w", err)
	}
	for _, path := range []string{
		"/usr/libexec/symmetry/symmetry-linux-launcher-core",
		"/usr/libexec/symmetry/symmetry-linux-gate",
	} {
		if err := verifyTrustedLinuxAsset(path, trustedLinuxAsset{uid: 0, gid: 0, mode: 0o755, links: 1}, a, b); err != nil {
			return fmt.Errorf("verify static Linux owner executable %s: %w", path, err)
		}
	}
	if err := verifyLinuxMountAllowsSetID("/usr/libexec/symmetry/symmetry-linux-launcher"); err != nil {
		return err
	}
	return nil
}

func verifyTrustedLinuxConfig(owner linuxgate.OwnerConfig, a, b linuxACLSubject) error {
	const path = "/etc/symmetry/linux-owner.conf"
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return fmt.Errorf("inspect fixed Linux owner config metadata: %w", err)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || !info.Mode().IsRegular() || identity.uid != 0 || identity.gid != 0 || identity.links != 1 ||
		info.Size() <= 0 || info.Size() > linuxgate.OwnerConfigMaxBytes || info.Mode().Perm() != 0o644 {
		return errors.New("fixed Linux owner config must be root:root mode 0644, singly linked, and within its strict size bound")
	}
	if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0}, nil, false); err != nil {
		return fmt.Errorf("fixed owner configuration has an untrusted ACL writer: %w", err)
	}
	for _, subject := range []linuxACLSubject{a, b} {
		if err := verifyEffectiveLinuxAccess(path, subject, 4, 2); err != nil {
			return fmt.Errorf("fixed owner configuration access is unsafe for UID %d: %w", subject.uid, err)
		}
	}
	return nil
}

type trustedLinuxAsset struct {
	uid      uint32
	gid      uint32
	mode     uint32
	links    uint64
	denyBAll bool
}

type linuxStatIdentity struct {
	uid   uint32
	gid   uint32
	mode  uint32
	links uint64
}

func linuxFileIdentity(info os.FileInfo) (linuxStatIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return linuxStatIdentity{}, errors.New("filesystem did not expose Linux stat identity")
	}
	return linuxStatIdentity{uid: stat.Uid, gid: stat.Gid, mode: stat.Mode, links: uint64(stat.Nlink)}, nil
}

func verifyTrustedLinuxAsset(path string, expected trustedLinuxAsset, a, b linuxACLSubject) error {
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil {
		return err
	}
	if err := validateTrustedLinuxAssetIdentity(identity, info.Mode().IsRegular(), expected); err != nil {
		return err
	}
	if err := verifyEffectiveLinuxAccess(path, a, 1, 2); err != nil {
		return fmt.Errorf("configured A cannot execute the installed owner image or can modify it: %w", err)
	}
	if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0}, nil, false); err != nil {
		return fmt.Errorf("root-installed owner executable %s has an untrusted ACL writer: %w", path, err)
	}
	forbidden := uint16(2)
	if expected.denyBAll {
		forbidden = 7
	}
	if err := verifyEffectiveLinuxAccess(path, b, 0, forbidden); err != nil {
		return fmt.Errorf("fixed B can access the privileged wrapper or modify an installed executable: %w", err)
	}
	return nil
}

func validateTrustedLinuxAssetIdentity(identity linuxStatIdentity, regular bool, expected trustedLinuxAsset) error {
	if !regular || identity.uid != expected.uid || identity.gid != expected.gid || identity.mode&07777 != expected.mode || identity.links != expected.links {
		return errors.New("installed executable ownership, mode, or link count differs from the fixed policy")
	}
	if expected.mode&0111 == 0 || identity.mode&0022 != 0 {
		return errors.New("installed executable is not executable or is group/world writable")
	}
	return nil
}

func verifyLinuxOwnerAccountHome(accounts linuxOwnerAccounts, harnessKind string, excludedPaths ...string) error {
	if accounts.a == nil || accounts.b == nil {
		return errors.New("fixed A/B account metadata is unavailable")
	}
	aUID, err := strconv.ParseUint(accounts.a.Uid, 10, 32)
	if err != nil {
		return errors.New("configured A account has an invalid UID")
	}
	aGID, err := strconv.ParseUint(accounts.a.Gid, 10, 32)
	if err != nil {
		return errors.New("configured A account has an invalid primary GID")
	}
	aHome, err := lstatCanonicalLinuxPath(accounts.a.HomeDir)
	if err != nil {
		return fmt.Errorf("inspect configured A HOME: %w", err)
	}
	aHomeIdentity, err := linuxFileIdentity(aHome)
	if err != nil || !aHome.IsDir() || aHomeIdentity.uid != uint32(aUID) || aHomeIdentity.gid != uint32(aGID) {
		return errors.New("configured A HOME must be a canonical A-owned directory")
	}
	if err := verifyEffectiveLinuxAccess(accounts.a.HomeDir, accounts.aSubject, 7, 0); err != nil {
		return fmt.Errorf("A cannot read/search/update its HOME: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(accounts.a.HomeDir, accounts.bSubject, 0, 7); err != nil {
		return fmt.Errorf("B can access A HOME or credentials: %w", err)
	}
	if err := verifyLinuxNoUntrustedACLWrite(accounts.a.HomeDir, []uint32{0, uint32(aUID)}, []uint32{uint32(aGID)}, true); err != nil {
		return fmt.Errorf("A HOME is changeable by an untrusted identity: %w", err)
	}
	if err := verifyLinuxDefaultLinuxAccess(accounts.a.HomeDir, accounts.bSubject, 0, 7); err != nil {
		return fmt.Errorf("future A HOME entries are accessible to B: %w", err)
	}
	if err := verifyLinuxParentsNotWritable(accounts.a.HomeDir, accounts.bSubject); err != nil {
		return fmt.Errorf("B can replace A HOME: %w", err)
	}
	bUID, err := strconv.ParseUint(accounts.b.Uid, 10, 32)
	if err != nil {
		return errors.New("fixed B account has an invalid UID")
	}
	bGID, err := strconv.ParseUint(accounts.b.Gid, 10, 32)
	if err != nil {
		return errors.New("fixed B account has an invalid primary GID")
	}
	if accounts.b.HomeDir == "" || !filepath.IsAbs(accounts.b.HomeDir) || filepath.Clean(accounts.b.HomeDir) != accounts.b.HomeDir || accounts.b.HomeDir == "/" {
		return errors.New("fixed B account lacks a canonical operator-provisioned HOME")
	}
	for _, excluded := range excludedPaths {
		if excluded != "" && pathsOverlapLinux(accounts.b.HomeDir, excluded) {
			return fmt.Errorf("fixed B HOME must remain outside StateDir and managed workspace paths: %s", excluded)
		}
	}
	home, err := lstatCanonicalLinuxPath(accounts.b.HomeDir)
	if err != nil {
		return fmt.Errorf("inspect fixed B HOME: %w", err)
	}
	homeIdentity, err := linuxFileIdentity(home)
	if err != nil || !home.IsDir() || homeIdentity.uid != uint32(bUID) || homeIdentity.gid != uint32(bGID) {
		return errors.New("fixed B HOME must be a B-owned directory on the configured primary group")
	}
	b := accounts.bSubject
	a := accounts.aSubject
	if err := verifyLinuxPathTraversal(accounts.b.HomeDir, b); err != nil {
		return fmt.Errorf("B cannot traverse the path to its HOME: %w", err)
	}
	if err := verifyLinuxPathTraversal(accounts.b.HomeDir, a); err != nil {
		return fmt.Errorf("A cannot stat known B HOME credential/session paths: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(accounts.b.HomeDir, b, 7, 0); err != nil {
		return fmt.Errorf("fixed B cannot read, search, and update its HOME: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(accounts.b.HomeDir, a, 1, 6); err != nil {
		return fmt.Errorf("A must be able to stat known B credential/session paths without listing or reading B HOME: %w", err)
	}
	credential, err := linuxNativeCredentialPath(accounts.b.HomeDir, harnessKind)
	if err != nil {
		return err
	}
	for path := filepath.Dir(credential); path != accounts.b.HomeDir; path = filepath.Dir(path) {
		info, err := lstatCanonicalLinuxPath(path)
		if err != nil {
			return fmt.Errorf("inspect B credential directory %s: %w", path, err)
		}
		identity, err := linuxFileIdentity(info)
		if err != nil || !info.IsDir() || identity.uid != uint32(bUID) || identity.gid != uint32(bGID) {
			return fmt.Errorf("B credential directory %s must be owned by B", path)
		}
		if err := verifyEffectiveLinuxAccess(path, b, 7, 0); err != nil {
			return fmt.Errorf("B cannot read/search/update credential directory %s: %w", path, err)
		}
		if err := verifyEffectiveLinuxAccess(path, a, 1, 6); err != nil {
			return fmt.Errorf("A cannot stat known credential path without accessing B directory contents %s: %w", path, err)
		}
		if filepath.Dir(path) == path {
			return errors.New("B credential path escaped HOME")
		}
	}
	credentialInfo, err := lstatCanonicalLinuxPath(credential)
	if err != nil {
		return fmt.Errorf("inspect configured B native credential file: %w", err)
	}
	credentialIdentity, err := linuxFileIdentity(credentialInfo)
	if err != nil || !credentialInfo.Mode().IsRegular() || credentialIdentity.uid != uint32(bUID) || credentialIdentity.gid != uint32(bGID) || credentialIdentity.links != 1 || credentialInfo.Size() <= 0 || credentialInfo.Size() > linuxOwnerCredentialFile {
		return errors.New("native credential file must be a nonempty, singly-linked B-owned regular file")
	}
	if err := verifyEffectiveLinuxAccess(credential, b, 6, 0); err != nil {
		return fmt.Errorf("configured native credential file is not read/write accessible to B: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(credential, a, 0, 6); err != nil {
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

func verifyLinuxOwnerWorkspace(value config.Config, owner linuxgate.OwnerConfig, accounts linuxOwnerAccounts) error {
	workspace, ok := value.Workspaces[value.Runtime.Workspace]
	if !ok || workspace.Policy != config.WorkspacePolicyGitCloneV2 || workspace.Path != "" || workspace.Ref != "" || workspace.SeedRoot == "" {
		return errors.New("Linux v2 owner preflight requires the exact git_clone_v2 Repository/SeedRoot/Root workspace contract")
	}
	a, b := accounts.aSubject, accounts.bSubject
	paths := []string{workspace.Repository, workspace.Root, workspace.SeedRoot, value.StateDir}
	for left := 0; left < len(paths); left++ {
		for right := left + 1; right < len(paths); right++ {
			if pathsOverlapLinux(paths[left], paths[right]) {
				return errors.New("Linux clone Repository, Root, SeedRoot, and StateDir must be pairwise disjoint")
			}
		}
	}
	if err := verifyLinuxCloneSource(workspace.Repository, owner, a, b, workspace.Root, workspace.SeedRoot, value.StateDir); err != nil {
		return err
	}
	if err := verifyLinuxCloneSeedRoot(workspace.SeedRoot, owner, a, b); err != nil {
		return err
	}
	return verifyLinuxCloneRoot(workspace.Root, owner, a, b)
}

func verifyLinuxCloneSource(repository string, owner linuxgate.OwnerConfig, a, b linuxACLSubject, excludedPaths ...string) error {
	if err := verifyLinuxTrustedGitExecutable(a, b); err != nil {
		return err
	}
	if err := verifyLinuxAControlledGitEntry(repository, owner.AUID, owner.AGID, b, 0, true); err != nil {
		return fmt.Errorf("A-owned Git source is not inaccessible to B: %w", err)
	}
	info, err := lstatCanonicalLinuxPath(repository)
	if err != nil {
		return fmt.Errorf("inspect A-owned Git source: %w", err)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || identity.uid != owner.AUID {
		return errors.New("Git source root must be owned by A")
	}
	if err := verifyLinuxPathTraversal(repository, a); err != nil {
		return fmt.Errorf("A cannot traverse its configured Git source: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(repository, a, 7, 0); err != nil {
		return fmt.Errorf("A cannot read/search/write its configured Git source: %w", err)
	}
	aRepositoryAccess, err := effectiveLinuxPathPermissions(repository, a)
	if err != nil {
		return err
	}
	bRepositoryAccess, err := effectiveLinuxPathPermissions(repository, b)
	if err != nil {
		return err
	}
	if err := validateLinuxCloneSourcePermissions(aRepositoryAccess, bRepositoryAccess); err != nil {
		return err
	}

	dotGit := filepath.Join(repository, ".git")
	gitEntry, err := lstatCanonicalLinuxPath(dotGit)
	if err != nil {
		return fmt.Errorf("inspect A-owned Git source .git entry: %w", err)
	}
	if gitEntry.IsDir() {
		if err := verifyLinuxAControlledGitEntry(dotGit, owner.AUID, owner.AGID, b, 0, true); err != nil {
			return fmt.Errorf("A-owned Git source .git directory is accessible to B or replaceable: %w", err)
		}
		if err := verifyEffectiveLinuxAccess(dotGit, a, 7, 0); err != nil {
			return fmt.Errorf("A cannot manage its Git source metadata: %w", err)
		}
	} else if !gitEntry.Mode().IsRegular() {
		return errors.New("A-owned Git source .git entry must be a directory or regular linked-worktree file")
	} else {
		if err := verifyLinuxAControlledGitEntry(dotGit, owner.AUID, owner.AGID, b, 0, false); err != nil {
			return fmt.Errorf("A-owned Git source .git file is accessible to B or replaceable: %w", err)
		}
		if err := verifyEffectiveLinuxAccess(dotGit, a, 4, 0); err != nil {
			return fmt.Errorf("A cannot read its Git source .git file: %w", err)
		}
	}

	commonDir, worktreeGitDir, err := resolveLinuxGitDirectories(repository)
	if err != nil {
		return fmt.Errorf("resolve A-owned Git source metadata: %w", err)
	}
	metadataDirectories := uniqueLinuxPaths(worktreeGitDir, commonDir)
	for _, directory := range metadataDirectories {
		for _, excluded := range excludedPaths {
			if pathsOverlapLinux(directory, excluded) {
				return fmt.Errorf("Git metadata directory %s overlaps protected path %s", directory, excluded)
			}
		}
		if err := verifyLinuxAControlledGitEntry(directory, owner.AUID, owner.AGID, b, 0, true); err != nil {
			return fmt.Errorf("A-owned Git metadata directory %s is accessible to B or replaceable: %w", directory, err)
		}
		metadataInfo, err := lstatCanonicalLinuxPath(directory)
		if err != nil {
			return fmt.Errorf("inspect A-owned Git metadata directory %s: %w", directory, err)
		}
		metadataIdentity, err := linuxFileIdentity(metadataInfo)
		if err != nil || metadataIdentity.uid != owner.AUID {
			return fmt.Errorf("Git metadata directory %s must be owned by A", directory)
		}
		if err := verifyEffectiveLinuxAccess(directory, a, 7, 0); err != nil {
			return fmt.Errorf("A cannot manage Git metadata directory %s: %w", directory, err)
		}
		visited := make(map[string]struct{})
		for _, configPath := range []string{filepath.Join(directory, "config"), filepath.Join(directory, "config.worktree")} {
			if _, err := os.Lstat(configPath); errors.Is(err, os.ErrNotExist) {
				if filepath.Base(configPath) == "config" {
					return fmt.Errorf("required Git configuration %s is missing", configPath)
				}
				continue
			} else if err != nil {
				return fmt.Errorf("inspect Git configuration %s: %w", configPath, err)
			}
			if err := verifyLinuxGitConfig(configPath, b, a, visited, 0); err != nil {
				return fmt.Errorf("verify A-owned executable Git configuration: %w", err)
			}
		}
		if err := verifyLinuxGitHookTree(filepath.Join(directory, "hooks"), b, a, 0, new(int)); err != nil {
			return fmt.Errorf("verify A-owned, non-B-writable Git hooks: %w", err)
		}
		coveredByCommon := false
		for _, candidate := range metadataDirectories {
			if candidate == directory {
				continue
			}
			relative, relErr := filepath.Rel(candidate, directory)
			if relErr == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				coveredByCommon = true
				break
			}
		}
		if !coveredByCommon {
			metadataEntries := new(int)
			if err := verifyLinuxGitMetadataTree(directory, b, a, owner.AUID, owner.AGID, 0, metadataEntries); err != nil {
				return fmt.Errorf("verify A-owned Git metadata tree %s: %w", directory, err)
			}
		}
	}
	return nil
}

func verifyLinuxTrustedGitExecutable(a, b linuxACLSubject) error {
	return verifyLinuxTrustedHostExecutable("/usr/bin/git", a, b)
}

func verifyLinuxTrustedHostExecutable(path string, a, b linuxACLSubject) error {
	if err := verifyLinuxServicePathAncestors(path, b, 0); err != nil {
		return fmt.Errorf("trusted host executable %s is replaceable or inaccessible: %w", path, err)
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return fmt.Errorf("inspect trusted host executable %s: %w", path, err)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil {
		return err
	}
	if err := validateLinuxTrustedHostExecutableMetadata(path, identity, info.Mode().IsRegular()); err != nil {
		return err
	}
	if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0}, nil, false); err != nil {
		return fmt.Errorf("trusted host executable %s has an untrusted ACL writer: %w", path, err)
	}
	if err := verifyEffectiveLinuxAccess(path, a, 5, 2); err != nil {
		return fmt.Errorf("A cannot execute trusted host binary %s safely: %w", path, err)
	}
	if err := verifyEffectiveLinuxAccess(path, b, 0, 2); err != nil {
		return fmt.Errorf("B can modify trusted host binary %s: %w", path, err)
	}
	return nil
}

func verifyLinuxGitHookTree(path string, b, a linuxACLSubject, depth int, entries *int) error {
	if depth > 16 {
		return fmt.Errorf("Git hook tree %s exceeds the static depth bound", path)
	}
	info, err := lstatCanonicalLinuxPath(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil {
		return err
	}
	if err := validateLinuxGitHookMetadata(identity, info.Mode(), a.uid, a.gid); err != nil {
		return fmt.Errorf("Git hook entry %s is unsafe: %w", path, err)
	}
	if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0, a.uid}, []uint32{a.gid}, info.IsDir()); err != nil {
		return fmt.Errorf("Git hook entry %s has an untrusted ACL writer: %w", path, err)
	}
	if err := verifyEffectiveLinuxAccess(path, b, 0, 2); err != nil {
		return fmt.Errorf("B can modify Git hook entry %s: %w", path, err)
	}
	if info.IsDir() {
		if err := verifyEffectiveLinuxAccess(path, a, 5, 0); err != nil {
			return fmt.Errorf("A cannot read/search Git hook directory %s: %w", path, err)
		}
		children, err := readBoundedLinuxDirectory(path, entries)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := verifyLinuxGitHookTree(filepath.Join(path, child.Name()), b, a, depth+1, entries); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Git hook entry %s is not a regular file", path)
	}
	return verifyEffectiveLinuxAccess(path, a, 4, 0)
}

func validateLinuxTrustedGitExecutableMetadata(identity linuxStatIdentity, regular bool) error {
	return validateLinuxTrustedHostExecutableMetadata("/usr/bin/git", identity, regular)
}

func validateLinuxTrustedHostExecutableMetadata(path string, identity linuxStatIdentity, regular bool) error {
	if !regular || identity.uid != 0 || identity.gid != 0 || identity.links != 1 || identity.mode&06022 != 0 || identity.mode&0111 == 0 {
		return fmt.Errorf("trusted executable %s must be a root:root-owned, singly-linked, executable, non-writable regular file", path)
	}
	return nil
}

func validateLinuxGitHookMetadata(identity linuxStatIdentity, mode os.FileMode, aUID, aGID uint32) error {
	if identity.uid != aUID || !mode.IsDir() && identity.links != 1 || mode&os.ModeSymlink != 0 || !mode.IsDir() && !mode.IsRegular() ||
		mode.Perm()&0002 != 0 || mode.Perm()&0020 != 0 && identity.gid != aGID {
		return errors.New("hook entries must be A-owned regular files/directories and protected from untrusted writers")
	}
	return nil
}

func verifyLinuxCloneSeedRoot(seedRoot string, owner linuxgate.OwnerConfig, a, b linuxACLSubject) error {
	if err := verifyLinuxAControlledGitParents(seedRoot, b, owner.AUID, owner.AGID); err != nil {
		return fmt.Errorf("A-owned Linux clone SeedRoot ancestor policy is unsafe: %w", err)
	}
	if err := verifyLinuxPathTraversal(seedRoot, a); err != nil {
		return fmt.Errorf("A cannot traverse Linux clone SeedRoot: %w", err)
	}
	if err := verifyLinuxPathTraversal(seedRoot, b); err != nil {
		return fmt.Errorf("B cannot traverse Linux clone SeedRoot: %w", err)
	}
	info, err := lstatCanonicalLinuxPath(seedRoot)
	if err != nil {
		return fmt.Errorf("inspect A-owned Linux clone SeedRoot: %w", err)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || !info.IsDir() || identity.uid != owner.AUID || identity.gid != owner.AGID || info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && identity.gid != owner.AGID {
		return errors.New("Linux clone SeedRoot must be an A-owned directory without untrusted group/other write access")
	}
	if err := verifyLinuxCloneSeedDirectory(seedRoot, owner, a, b, 0, new(int)); err != nil {
		return fmt.Errorf("verify immutable Linux clone SeedRoot exchange ACLs: %w", err)
	}
	return nil
}

func verifyLinuxCloneSeedDirectory(path string, owner linuxgate.OwnerConfig, a, b linuxACLSubject, depth int, entries *int) error {
	if depth > 2 {
		return fmt.Errorf("seed directory %s exceeds the exact binding/run layout", path)
	}
	if err := verifyLinuxAControlledGitEntry(path, owner.AUID, owner.AGID, b, 5, true); err != nil {
		return err
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || identity.uid != owner.AUID || identity.gid != owner.AGID || info.Mode().Perm() != 0o750 {
		return fmt.Errorf("seed directory %s must be A-owned with mode 0750", path)
	}
	aAccess, err := effectiveLinuxPathPermissions(path, a)
	if err != nil {
		return fmt.Errorf("read A seed directory access ACL %s: %w", path, err)
	}
	bAccess, err := effectiveLinuxPathPermissions(path, b)
	if err != nil {
		return fmt.Errorf("read B seed directory access ACL %s: %w", path, err)
	}
	aDefault, err := defaultLinuxACLPermissions(path, a)
	if err != nil {
		return fmt.Errorf("read future A seed entry ACL at %s: %w", path, err)
	}
	bDefault, err := defaultLinuxACLPermissions(path, b)
	if err != nil {
		return fmt.Errorf("read future B seed entry ACL at %s: %w", path, err)
	}
	if err := validateLinuxCloneSeedPermissions(aAccess, bAccess, aDefault, bDefault); err != nil {
		return err
	}
	children, err := readBoundedLinuxDirectory(path, entries)
	if err != nil {
		return fmt.Errorf("list A-owned Linux clone seed directory %s: %w", path, err)
	}
	for _, child := range children {
		childPath := filepath.Join(path, child.Name())
		childInfo, err := lstatCanonicalLinuxPath(childPath)
		if err != nil {
			return fmt.Errorf("inspect A-owned clone seed entry %s: %w", childPath, err)
		}
		if childInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("A-owned clone seed entry %s is a symlink", childPath)
		}
		if childInfo.IsDir() {
			if depth == 2 || !validLinuxCloneSeedDirectoryName(child.Name(), depth) {
				return fmt.Errorf("unexpected directory %s in A-owned clone seed layout", childPath)
			}
			if err := verifyLinuxCloneSeedDirectory(childPath, owner, a, b, depth+1, entries); err != nil {
				return err
			}
			continue
		}
		if depth != 2 {
			return fmt.Errorf("unexpected non-directory %s in A-owned clone seed layout", childPath)
		}
		identity, err := linuxFileIdentity(childInfo)
		if err != nil || !childInfo.Mode().IsRegular() || identity.uid != owner.AUID || identity.links != 1 {
			return fmt.Errorf("seed file %s must be a singly-linked A-owned regular file", childPath)
		}
		if err := verifyLinuxNoUntrustedACLWrite(childPath, []uint32{0, owner.AUID}, []uint32{owner.AGID}, false); err != nil {
			return fmt.Errorf("seed file %s has an untrusted ACL writer: %w", childPath, err)
		}
		if strings.HasPrefix(child.Name(), ".clone-seed-") && validLinuxCloneSeedTempName(child.Name()) {
			if childInfo.Mode().Perm() != 0o600 && childInfo.Mode().Perm() != 0o440 || childInfo.Size() < 0 || childInfo.Size() > authority.LinuxCloneMaxSeedBytes+1 {
				return fmt.Errorf("in-progress seed temporary %s must remain A-owned and non-writable", childPath)
			}
			aRead := uint16(6)
			bRead, bForbidden := uint16(0), uint16(7)
			if childInfo.Mode().Perm() == 0o440 {
				aRead, bRead, bForbidden = 4, 4, 2
			}
			if err := verifyEffectiveLinuxAccess(childPath, a, aRead, 0); err != nil {
				return fmt.Errorf("A cannot read in-progress seed temporary %s: %w", childPath, err)
			}
			if err := verifyEffectiveLinuxAccess(childPath, b, bRead, bForbidden); err != nil {
				return fmt.Errorf("B can modify in-progress seed temporary %s: %w", childPath, err)
			}
			continue
		}
		if childInfo.Size() <= 0 || childInfo.Size() > authority.LinuxCloneMaxSeedBytes || !validLinuxCloneBundleName(child.Name()) || childInfo.Mode().Perm() != 0o440 {
			return fmt.Errorf("unexpected or mutable seed bundle entry %s", childPath)
		}
		if err := verifyEffectiveLinuxAccess(childPath, a, 4, 0); err != nil {
			return fmt.Errorf("A cannot read immutable seed bundle %s: %w", childPath, err)
		}
		if err := verifyEffectiveLinuxAccess(childPath, b, 4, 2); err != nil {
			return fmt.Errorf("B cannot read the immutable seed bundle without write access %s: %w", childPath, err)
		}
	}
	return nil
}

func validLinuxCloneSeedDirectoryName(name string, depth int) bool {
	if depth == 0 {
		return strings.HasPrefix(name, "binding-") && len(name) > len("binding-")
	}
	return strings.HasPrefix(name, "run-") && len(name) > len("run-")
}

func validLinuxCloneBundleName(name string) bool {
	if !strings.HasPrefix(name, "generation-") || !strings.HasSuffix(name, ".bundle") {
		return false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(name, "generation-"), ".bundle")
	generation, err := strconv.ParseUint(value, 10, 63)
	return err == nil && generation > 0 && strconv.FormatUint(generation, 10) == value
}

func validLinuxCloneSeedTempName(name string) bool {
	if len(name) != len(".clone-seed-")+24 || !strings.HasPrefix(name, ".clone-seed-") {
		return false
	}
	for _, value := range strings.TrimPrefix(name, ".clone-seed-") {
		if !(value >= '0' && value <= '9' || value >= 'a' && value <= 'f') {
			return false
		}
	}
	return true
}

func verifyLinuxCloneRoot(root string, owner linuxgate.OwnerConfig, a, b linuxACLSubject) error {
	if err := verifyLinuxAControlledGitParents(root, b, owner.AUID, owner.AGID); err != nil {
		return fmt.Errorf("B-owned Linux clone Root ancestor policy is unsafe: %w", err)
	}
	if err := verifyLinuxPathTraversal(root, a); err != nil {
		return fmt.Errorf("A cannot traverse B-owned Linux clone Root: %w", err)
	}
	if err := verifyLinuxPathTraversal(root, b); err != nil {
		return fmt.Errorf("B cannot traverse Linux clone Root: %w", err)
	}
	info, err := lstatCanonicalLinuxPath(root)
	if err != nil {
		return fmt.Errorf("inspect B-owned Linux clone Root: %w", err)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || !info.IsDir() || identity.uid != owner.BUID || identity.gid != owner.BGID || info.Mode().Perm()&0022 != 0 {
		return errors.New("Linux clone Root must be a B-owned directory not writable through group/other mode bits")
	}
	if err := verifyLinuxNoUntrustedACLWrite(root, []uint32{0, owner.BUID}, nil, true); err != nil {
		return fmt.Errorf("B-owned clone Root has an untrusted ACL writer: %w", err)
	}
	aAccess, err := effectiveLinuxPathPermissions(root, a)
	if err != nil {
		return fmt.Errorf("read A clone Root access ACL: %w", err)
	}
	bAccess, err := effectiveLinuxPathPermissions(root, b)
	if err != nil {
		return fmt.Errorf("read B clone Root access ACL: %w", err)
	}
	aDefault, err := defaultLinuxACLPermissions(root, a)
	if err != nil {
		return fmt.Errorf("read future A clone target ACL: %w", err)
	}
	bDefault, err := defaultLinuxACLPermissions(root, b)
	if err != nil {
		return fmt.Errorf("read future B clone target ACL: %w", err)
	}
	if err := validateLinuxCloneRootPermissions(aAccess, bAccess, aDefault, bDefault); err != nil {
		return err
	}
	return nil
}

func validateLinuxCloneSourcePermissions(a, b uint16) error {
	if a&7 != 7 || b&7 != 0 {
		return fmt.Errorf("source access must grant A rwx and deny B completely (A=%#o, B=%#o)", a, b)
	}
	return nil
}

func validateLinuxCloneSeedPermissions(a, b, aDefault, bDefault uint16) error {
	if a&7 != 7 || b&5 != 5 || b&2 != 0 || aDefault&7 != 7 || bDefault&5 != 5 || bDefault&2 != 0 {
		return fmt.Errorf("SeedRoot and inherited entries must be A-rwx/B-r-x without B write (access A=%#o B=%#o default A=%#o B=%#o)", a, b, aDefault, bDefault)
	}
	return nil
}

func validateLinuxCloneRootPermissions(a, b, aDefault, bDefault uint16) error {
	if a&5 != 5 || a&2 != 0 || b&7 != 7 || aDefault&5 != 5 || aDefault&2 != 0 || bDefault&7 != 7 {
		return fmt.Errorf("Root and inherited entries must grant B rwx and A r-x without A write (access A=%#o B=%#o default A=%#o B=%#o)", a, b, aDefault, bDefault)
	}
	return nil
}

func verifyLinuxGitMetadataTree(path string, b, a linuxACLSubject, ownerUID, ownerGID uint32, depth int, entries *int) error {
	if depth > linuxClonePreflightMaxDepth {
		return fmt.Errorf("Git metadata tree %s exceeds the static depth bound", path)
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || identity.uid != 0 && identity.uid != ownerUID || !info.IsDir() && identity.links != 1 || info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && identity.gid != ownerGID {
		return fmt.Errorf("Git metadata entry %s is not a protected root/A-owned inode", path)
	}
	if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0, ownerUID}, []uint32{ownerGID}, info.IsDir()); err != nil {
		return fmt.Errorf("Git metadata entry %s has an untrusted ACL writer: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Git metadata entry %s is a symlink", path)
	}
	if err := verifyEffectiveLinuxAccess(path, b, 0, 7); err != nil {
		return fmt.Errorf("Git metadata entry %s is accessible to B: %w", path, err)
	}
	if info.IsDir() {
		if err := verifyEffectiveLinuxAccess(path, a, 5, 0); err != nil {
			return fmt.Errorf("A cannot read/search Git metadata directory %s: %w", path, err)
		}
		children, err := readBoundedLinuxDirectory(path, entries)
		if err != nil {
			return err
		}
		for _, child := range children {
			childPath := filepath.Join(path, child.Name())
			if child.Name() == "alternates" {
				return errors.New("Git object alternates are not permitted by clone-v2 preflight")
			}
			if err := verifyLinuxGitMetadataTree(childPath, b, a, ownerUID, ownerGID, depth+1, entries); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Git metadata entry %s is not a regular file", path)
	}
	if err := verifyEffectiveLinuxAccess(path, a, 4, 0); err != nil {
		return fmt.Errorf("A cannot read Git metadata file %s: %w", path, err)
	}
	return nil
}

func readBoundedLinuxDirectory(path string, totalEntries *int) ([]os.DirEntry, error) {
	directory, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries := make([]os.DirEntry, 0, 32)
	for {
		batch, readErr := directory.ReadDir(256)
		*totalEntries += len(batch)
		if *totalEntries > linuxClonePreflightMaxEntries {
			return nil, errors.New("clone preflight metadata exceeds its static-entry bound")
		}
		entries = append(entries, batch...)
		if errors.Is(readErr, io.EOF) {
			return entries, nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
}

func resolveLinuxGitDirectories(repository string) (commonDir, worktreeGitDir string, err error) {
	dotGit := filepath.Join(repository, ".git")
	info, err := os.Lstat(dotGit)
	if err != nil {
		return "", "", fmt.Errorf("stat .git: %w", err)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "", "", errors.New("repository .git entry must not be a symlink")
	case info.IsDir():
		worktreeGitDir = dotGit
	case info.Mode().IsRegular():
		data, readErr := readBoundedLinuxFile(dotGit, 4096)
		if readErr != nil {
			return "", "", readErr
		}
		line := strings.TrimSuffix(string(data), "\n")
		if strings.Contains(line, "\n") || !strings.HasPrefix(line, "gitdir: ") || strings.TrimSpace(strings.TrimPrefix(line, "gitdir: ")) == "" {
			return "", "", errors.New("repository .git file has an unsupported shape")
		}
		worktreeGitDir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
		if !filepath.IsAbs(worktreeGitDir) {
			worktreeGitDir = filepath.Join(repository, worktreeGitDir)
		}
		worktreeGitDir = filepath.Clean(worktreeGitDir)
	default:
		return "", "", errors.New("repository .git entry is neither a directory nor a regular linked-worktree file")
	}
	gitInfo, err := lstatCanonicalLinuxPath(worktreeGitDir)
	if err != nil || !gitInfo.IsDir() {
		return "", "", errors.New("resolved repository Git directory is unavailable")
	}
	commonDir = worktreeGitDir
	commondirPath := filepath.Join(worktreeGitDir, "commondir")
	if commondirInfo, err := os.Lstat(commondirPath); err == nil {
		if !commondirInfo.Mode().IsRegular() {
			return "", "", errors.New("Git commondir entry must be a regular file")
		}
		data, readErr := readBoundedLinuxFile(commondirPath, 4096)
		if readErr != nil {
			return "", "", readErr
		}
		commonText := strings.TrimSuffix(string(data), "\n")
		if commonText == "" || strings.Contains(commonText, "\n") {
			return "", "", errors.New("Git commondir file has an unsupported shape")
		}
		if !filepath.IsAbs(commonText) {
			commonText = filepath.Join(worktreeGitDir, commonText)
		}
		commonDir = filepath.Clean(commonText)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	commonInfo, err := lstatCanonicalLinuxPath(commonDir)
	if err != nil || !commonInfo.IsDir() {
		return "", "", errors.New("resolved common Git directory is unavailable")
	}
	return commonDir, worktreeGitDir, nil
}

func uniqueLinuxPaths(paths ...string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result
}

func readBoundedLinuxFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file %s exceeds the read-only preflight limit", path)
	}
	return data, nil
}

func verifyLinuxGitConfig(path string, b, a linuxACLSubject, visited map[string]struct{}, depth int) error {
	if depth > 8 {
		return errors.New("Git configuration include depth exceeds the read-only preflight bound")
	}
	path = filepath.Clean(path)
	if _, ok := visited[path]; ok {
		return nil
	}
	visited[path] = struct{}{}
	if err := verifyLinuxAControlledGitParents(path, b, a.uid, a.gid); err != nil {
		return fmt.Errorf("Git config %s is beneath a replaceable ancestor: %w", path, err)
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return fmt.Errorf("Git configuration %s is unavailable: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Git configuration %s is not a regular file", path)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || identity.uid != a.uid || info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && identity.gid != a.gid {
		return fmt.Errorf("Git configuration %s is not A-owned and protected from untrusted writers", path)
	}
	if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0, a.uid}, []uint32{a.gid}, false); err != nil {
		return fmt.Errorf("Git configuration %s has an untrusted ACL writer: %w", path, err)
	}
	if err := verifyEffectiveLinuxAccess(path, b, 0, 7); err != nil {
		return fmt.Errorf("B can access executable Git configuration %s: %w", path, err)
	}
	if err := verifyEffectiveLinuxAccess(path, a, 4, 0); err != nil {
		return fmt.Errorf("A cannot read Git configuration %s: %w", path, err)
	}
	contents, err := readBoundedLinuxFile(path, linuxGitConfigMaxBytes)
	if err != nil {
		return fmt.Errorf("read executable Git configuration %s: %w", path, err)
	}
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), linuxGitConfigMaxBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line, err := stripGitConfigComment(scanner.Text())
		if err != nil {
			return fmt.Errorf("parse Git configuration %s:%d: %w", path, lineNumber, err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return fmt.Errorf("Git configuration %s:%d has an incomplete section", path, lineNumber)
			}
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section == "" {
				return fmt.Errorf("Git configuration %s:%d has an empty section", path, lineNumber)
			}
			continue
		}
		key, value, hasValue := strings.Cut(line, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || strings.ContainsAny(key, " \t") {
			return fmt.Errorf("Git configuration %s:%d has an invalid key", path, lineNumber)
		}
		if !hasValue {
			value = "true"
		}
		value, err = parseGitConfigScalar(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("Git configuration %s:%d value is not safely understood: %w", path, lineNumber, err)
		}
		if reason := linuxGitExternalCommandSetting(section, key, value); reason != "" {
			return fmt.Errorf("Git configuration %s:%d has unsupported executable setting %s: %s", path, lineNumber, key, reason)
		}
		switch {
		case strings.HasPrefix(section, "filter ") && (key == "clean" || key == "smudge" || key == "process") && value != "" && value != "false":
			return fmt.Errorf("Git filter command in executable config %s can consume B-controlled workspace content", path)
		case (section == "include" || strings.HasPrefix(section, "includeif ")) && key == "path":
			if value == "" || strings.HasPrefix(value, "~") || strings.Contains(value, "%(") {
				return fmt.Errorf("Git config include in %s uses an unresolved path form", path)
			}
			if !filepath.IsAbs(value) {
				value = filepath.Join(filepath.Dir(path), value)
			}
			if err := verifyLinuxGitConfig(filepath.Clean(value), b, a, visited, depth+1); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan Git configuration %s: %w", path, err)
	}
	return nil
}

func linuxGitExternalCommandSetting(section, key, value string) string {
	if value == "" {
		return ""
	}
	switch {
	case section == "core" && (key == "pager" || key == "editor" || key == "sshcommand" || key == "fsmonitor" || key == "askpass" || key == "attributesfile"):
		return "core setting can invoke an external program or executable attribute source"
	case strings.HasPrefix(section, "filter ") && (key == "clean" || key == "smudge" || key == "process"):
		return "filter command can execute while A checks out a B-controlled worktree"
	case strings.HasPrefix(section, "credential ") && key == "helper":
		return "credential helper can execute under A"
	case strings.HasPrefix(section, "diff ") && (key == "command" || key == "textconv"):
		return "diff driver can execute under A"
	case section == "diff" && key == "external":
		return "external diff command can execute under A"
	case strings.HasPrefix(section, "merge ") && key == "driver":
		return "merge driver can execute under A"
	case strings.HasPrefix(section, "difftool ") && key == "cmd":
		return "diff tool can execute under A"
	case strings.HasPrefix(section, "mergetool ") && key == "cmd":
		return "merge tool can execute under A"
	case section == "alias" && strings.HasPrefix(value, "!"):
		return "shell alias can execute under A"
	default:
		return ""
	}
}

func stripGitConfigComment(line string) (string, error) {
	quoted := false
	escaped := false
	for index := range len(line) {
		value := line[index]
		if escaped {
			escaped = false
			continue
		}
		if quoted && value == '\\' {
			escaped = true
			continue
		}
		if value == '"' {
			quoted = !quoted
			continue
		}
		if !quoted && (value == '#' || value == ';') {
			return line[:index], nil
		}
	}
	if quoted || escaped {
		return "", errors.New("unterminated quoted string")
	}
	return line, nil
}

func parseGitConfigScalar(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if value[0] == '"' {
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		return parsed, nil
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("Git config value contains a control character")
	}
	return strings.TrimSpace(value), nil
}

func verifyLinuxOwnerPiShare(value config.Config, owner linuxgate.OwnerConfig, accounts linuxOwnerAccounts) error {
	profile := value.AgentProfiles[value.Runtime.AgentProfile]
	if value.Runtime.HarnessKind != config.RuntimeHarnessPi || !profile.ProviderAccess {
		return nil
	}
	path := value.PiProviderExtensionDir
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Linux Pi provider access requires a canonical operator-provisioned extension share")
	}
	if pathsOverlapLinux(path, value.StateDir) {
		return errors.New("Linux Pi provider extension share must remain outside private StateDir")
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return fmt.Errorf("inspect Linux Pi provider extension share: %w", err)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || validateLinuxPiExtensionShareIdentity(info.IsDir(), identity, owner) != nil {
		return errors.New("Linux Pi extension share must be A-owned, B-primary-group, and mode 02750")
	}
	a := accounts.aSubject
	b := accounts.bSubject
	if err := verifyEffectiveLinuxAccess(path, a, 7, 0); err != nil {
		return fmt.Errorf("A cannot publish Pi provider extensions to the configured share: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(path, b, 5, 2); err != nil {
		return fmt.Errorf("Pi extension share does not grant B read/search without write: %w", err)
	}
	if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0, owner.AUID}, []uint32{owner.AGID}, true); err != nil {
		return fmt.Errorf("Pi extension share has an untrusted ACL writer: %w", err)
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		parentInfo, err := lstatCanonicalLinuxPath(parent)
		if err != nil {
			return fmt.Errorf("inspect Pi share ancestor %s: %w", parent, err)
		}
		parentIdentity, err := linuxFileIdentity(parentInfo)
		if err != nil || !parentInfo.IsDir() || parentIdentity.uid != 0 && parentIdentity.uid != owner.AUID || parentInfo.Mode().Perm()&0022 != 0 && parentInfo.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("Pi share ancestor %s is writable by an untrusted identity", parent)
		}
		if err := verifyEffectiveLinuxAccess(parent, b, 1, 2); err != nil {
			return fmt.Errorf("Pi share ancestor %s is writable or unsearchable by B: %w", parent, err)
		}
		if err := verifyLinuxNoUntrustedACLWrite(parent, []uint32{0, owner.AUID}, []uint32{owner.AGID}, true); err != nil {
			return fmt.Errorf("Pi share ancestor %s has an untrusted ACL writer: %w", parent, err)
		}
		if parent == "/" {
			break
		}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("list Pi provider extension share read-only: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "symmetry-pi-provider-") || !strings.HasSuffix(entry.Name(), ".ts") {
			return fmt.Errorf("unexpected Pi provider extension share entry %q", entry.Name())
		}
		info, err := lstatCanonicalLinuxPath(filepath.Join(path, entry.Name()))
		if err != nil {
			return err
		}
		fileIdentity, err := linuxFileIdentity(info)
		if err != nil || !info.Mode().IsRegular() || fileIdentity.uid != owner.AUID || fileIdentity.gid != owner.BGID || info.Mode().Perm() != 0o440 || fileIdentity.links != 1 {
			return fmt.Errorf("Pi shared extension %s is not A-owned, B-readable, and immutable to B", entry.Name())
		}
		if err := verifyEffectiveLinuxAccess(filepath.Join(path, entry.Name()), b, 4, 2); err != nil {
			return fmt.Errorf("Pi extension file %s is not read-only to B: %w", entry.Name(), err)
		}
		if err := verifyLinuxNoUntrustedACLWrite(filepath.Join(path, entry.Name()), []uint32{0, owner.AUID}, nil, false); err != nil {
			return fmt.Errorf("Pi extension file %s has an untrusted ACL writer: %w", entry.Name(), err)
		}
	}
	return nil
}

func validateLinuxPiExtensionShareIdentity(isDirectory bool, identity linuxStatIdentity, owner linuxgate.OwnerConfig) error {
	if !isDirectory || owner.AUID == 0 || owner.BGID == 0 || identity.uid != owner.AUID ||
		identity.gid != owner.BGID || identity.mode&07777 != 0o2750 {
		return errors.New("Linux Pi extension share identity differs from trusted A/B owner policy")
	}
	return nil
}

func pathsOverlapLinux(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if left == right {
		return true
	}
	return strings.HasPrefix(left, strings.TrimSuffix(right, string(filepath.Separator))+string(filepath.Separator)) ||
		strings.HasPrefix(right, strings.TrimSuffix(left, string(filepath.Separator))+string(filepath.Separator))
}

func verifyLinuxPathTraversal(path string, subject linuxACLSubject) error {
	if path == "/" {
		return nil
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		if err := verifyEffectiveLinuxAccess(parent, subject, 1, 0); err != nil {
			return fmt.Errorf("directory %s is not searchable: %w", parent, err)
		}
		if parent == "/" {
			return nil
		}
	}
}

func verifyLinuxAControlledGitParents(path string, b linuxACLSubject, ownerUID, ownerGID uint32) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Git control path is not canonical and absolute")
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := lstatCanonicalLinuxPath(parent)
		if err != nil {
			return err
		}
		identity, err := linuxFileIdentity(info)
		groupWritableByA := identity.gid == ownerGID
		if err != nil || !info.IsDir() || identity.uid != 0 && identity.uid != ownerUID || info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && !groupWritableByA {
			return fmt.Errorf("Git control ancestor %s is not root/A-owned and non-writable", parent)
		}
		if err := verifyLinuxNoUntrustedACLWrite(parent, []uint32{0, ownerUID}, []uint32{ownerGID}, true); err != nil {
			return fmt.Errorf("Git control ancestor %s has an untrusted ACL writer: %w", parent, err)
		}
		if err := verifyEffectiveLinuxAccess(parent, b, 0, 2); err != nil {
			return fmt.Errorf("Git control ancestor %s is B-writable: %w", parent, err)
		}
		if parent == "/" {
			return nil
		}
	}
}

func verifyLinuxAControlledGitEntry(path string, ownerUID, ownerGID uint32, b linuxACLSubject, bPermissions uint16, wantDirectory bool) error {
	if err := verifyLinuxAControlledGitParents(path, b, ownerUID, ownerGID); err != nil {
		return err
	}
	if bPermissions != 0 {
		if err := verifyLinuxPathTraversal(path, b); err != nil {
			return err
		}
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil {
		return err
	}
	if wantDirectory && !info.IsDir() || !wantDirectory && !info.Mode().IsRegular() {
		return errors.New("Git metadata entry has the wrong file type")
	}
	if identity.uid != 0 && identity.uid != ownerUID || info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && identity.gid != ownerGID {
		return errors.New("Git metadata entry is not root/A-owned and non-writable by untrusted groups")
	}
	if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0, ownerUID}, []uint32{ownerGID}, wantDirectory); err != nil {
		return fmt.Errorf("Git metadata entry has an untrusted ACL writer: %w", err)
	}
	forbidden := uint16(2)
	if bPermissions == 0 {
		forbidden = 7
	}
	if err := verifyEffectiveLinuxAccess(path, b, bPermissions, forbidden); err != nil {
		return err
	}
	return nil
}

func verifyLinuxParentsNotWritable(path string, subject linuxACLSubject) error {
	return verifyLinuxTrustedPathAncestors(path, uint32(os.Geteuid()), uint32(os.Getegid()), &subject)
}

func verifyLinuxTrustedPathAncestors(path string, ownerUID, ownerGID uint32, b *linuxACLSubject) error {
	if path == "/" {
		return nil
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := lstatCanonicalLinuxPath(parent)
		if err != nil {
			return err
		}
		identity, err := linuxFileIdentity(info)
		if err != nil || validateLinuxTrustedPathAncestor(identity, info.IsDir(), ownerUID) != nil {
			return fmt.Errorf("directory %s is not controlled by root or A", parent)
		}
		if err := verifyLinuxNoUntrustedACLWrite(parent, []uint32{0, ownerUID}, []uint32{ownerGID}, true); err != nil {
			return fmt.Errorf("directory %s has an untrusted owner, group, or ACL writer: %w", parent, err)
		}
		if b != nil {
			if err := verifyEffectiveLinuxAccess(parent, *b, 1, 2); err != nil {
				return fmt.Errorf("directory %s is B-writable or unsearchable: %w", parent, err)
			}
		}
		if parent == "/" {
			return nil
		}
	}
}

func validateLinuxTrustedPathAncestor(identity linuxStatIdentity, directory bool, ownerUID uint32) error {
	if !directory || identity.uid != 0 && identity.uid != ownerUID {
		return errors.New("path ancestor must be a directory owned by root or A")
	}
	return nil
}

func verifyLinuxMountAllowsSetID(path string) error {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("read mount options for setuid owner wrapper: %w", err)
	}
	defer file.Close()
	clean := filepath.Clean(path)
	bestMount := ""
	noSUID := false
	scanner := bufio.NewScanner(io.LimitReader(file, linuxCgroupMountMaxBytes))
	scanner.Buffer(make([]byte, 4096), linuxCgroupMountMaxBytes)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if separator < 6 || separator+3 >= len(fields) {
			return errors.New("mountinfo contains a malformed entry while checking setuid mount flags")
		}
		mountPoint, err := unescapeLinuxMountPath(fields[4])
		if err != nil || !filepath.IsAbs(mountPoint) || filepath.Clean(mountPoint) != mountPoint {
			return errors.New("mountinfo contains an invalid mount point while checking setuid flags")
		}
		if clean != mountPoint && !strings.HasPrefix(clean, strings.TrimSuffix(mountPoint, string(filepath.Separator))+string(filepath.Separator)) {
			continue
		}
		if len(mountPoint) < len(bestMount) {
			continue
		}
		bestMount = mountPoint
		noSUID = hasMountOption(fields[5], "nosuid") || hasMountOption(fields[separator+3], "nosuid")
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read mount options for setuid owner wrapper: %w", err)
	}
	if bestMount == "" || noSUID {
		return errors.New("setuid Linux owner wrapper is unavailable on a nosuid or unidentified mount")
	}
	return nil
}

func unescapeLinuxMountPath(value string) (string, error) {
	var result strings.Builder
	result.Grow(len(value))
	for i := range len(value) {
		if value[i] != '\\' {
			result.WriteByte(value[i])
			continue
		}
		if i+3 >= len(value) {
			return "", errors.New("short mount path escape")
		}
		switch value[i+1 : i+4] {
		case "040":
			result.WriteByte(' ')
		case "011":
			result.WriteByte('\t')
		case "012":
			result.WriteByte('\n')
		case "134":
			result.WriteByte('\\')
		default:
			return "", errors.New("unknown mount path escape")
		}
		i += 3
	}
	return result.String(), nil
}

func hasMountOption(options, expected string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == expected {
			return true
		}
	}
	return false
}

func verifyLinuxSystemdUnits(value config.Config, accounts linuxOwnerAccounts) error {
	group, err := user.LookupGroupId(accounts.a.Gid)
	if err != nil {
		return fmt.Errorf("lookup A systemd service group: %w", err)
	}
	delegatedSubgroup := filepath.Base(value.LinuxOwnerDelegatedPath)
	daemonPolicy := linuxSystemdUnitPolicy{
		unit: "symmetry-daemon.service", user: accounts.a.Username, group: group.Name, home: accounts.a.HomeDir,
		execStart:    []string{"/usr/libexec/symmetry/symmetry-daemon", "-config", "/etc/symmetry/daemon.json"},
		execStartPre: []string{"/usr/libexec/symmetry/symmetry-daemon", "-verify-linux-owner-install", "-config", "/etc/symmetry/daemon.json"},
	}
	ownerPolicy := linuxSystemdUnitPolicy{
		unit: "symmetry-owner.service", user: accounts.a.Username, group: group.Name, home: accounts.a.HomeDir,
		owner: true, delegate: "yes", delegatedSubgroup: delegatedSubgroup,
		execStart: []string{"/usr/libexec/symmetry/symmetry-daemon", "-linux-owner-anchor", "-cgroup-mount", value.LinuxOwnerCgroupMount,
			"-delegated-path", value.LinuxOwnerDelegatedPath, "-socket", value.LinuxOwnerSocket},
		execStop: []string{"/usr/libexec/symmetry/symmetry-daemon", "-recover-containment-only", "-config", "/etc/symmetry/daemon.json"},
	}
	for _, policy := range []linuxSystemdUnitPolicy{daemonPolicy, ownerPolicy} {
		if err := verifyLinuxSystemdUnit(policy, accounts.aSubject, accounts.bSubject); err != nil {
			return err
		}
	}
	return nil
}

type linuxSystemdUnitPolicy struct {
	unit              string
	user              string
	group             string
	home              string
	owner             bool
	delegate          string
	delegatedSubgroup string
	execStart         []string
	execStartPre      []string
	execStop          []string
}

func verifyLinuxSystemdUnit(policy linuxSystemdUnitPolicy, a, b linuxACLSubject) error {
	path := filepath.Join("/etc/systemd/system", policy.unit)
	transientDirectory := filepath.Join("/run/systemd/transient")
	if err := verifyLinuxSystemdDirectory(filepath.Join("/run/systemd"), a, b); err != nil {
		return fmt.Errorf("verify transient-unit ancestor: %w", err)
	}
	if _, err := lstatCanonicalLinuxPath(transientDirectory); err == nil {
		if err := verifyLinuxSystemdDirectory(transientDirectory, a, b); err != nil {
			return fmt.Errorf("verify transient-unit directory: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect transient-unit directory: %w", err)
	}
	if _, err := lstatCanonicalLinuxPath(filepath.Join(transientDirectory, policy.unit)); err == nil {
		return fmt.Errorf("transient unit overrides installed %s", policy.unit)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect transient unit override for %s: %w", policy.unit, err)
	}
	if err := verifyLinuxSystemdDirectory(filepath.Dir(path), a, b); err != nil {
		return fmt.Errorf("verify parent of staged %s unit: %w", policy.unit, err)
	}
	directives, err := readLinuxSystemdDirectives(path, a, b)
	if err != nil {
		return fmt.Errorf("verify staged %s unit: %w", policy.unit, err)
	}
	if err := validateLinuxSystemdCandidate(directives, policy, a, b); err != nil {
		return fmt.Errorf("staged %s unit violates the deployment contract: %w", policy.unit, err)
	}
	var validatedDropIns []string
	for _, directory := range linuxSystemdDropInDirectories(policy.unit) {
		present, err := verifyLinuxOptionalSystemdDirectory(directory, a, b)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return fmt.Errorf("list %s drop-ins read-only: %w", policy.unit, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
				if entry.IsDir() {
					return fmt.Errorf("unexpected nested directory in %s drop-ins: %s", policy.unit, entry.Name())
				}
				continue
			}
			dropIn := filepath.Join(directory, entry.Name())
			directives, err := readLinuxSystemdDirectives(dropIn, a, b)
			if err != nil {
				return fmt.Errorf("verify %s drop-in %s: %w", policy.unit, entry.Name(), err)
			}
			if err := validateLinuxSystemdOverrides(directives, policy, a, b); err != nil {
				return fmt.Errorf("%s drop-in %s weakens the owner boundary: %w", policy.unit, entry.Name(), err)
			}
			validatedDropIns = append(validatedDropIns, dropIn)
		}
	}
	return verifyEffectiveLinuxSystemdUnit(policy, validatedDropIns, a, b)
}

func verifyEffectiveLinuxSystemdUnit(policy linuxSystemdUnitPolicy, validatedDropIns []string, a, b linuxACLSubject) error {
	propertiesToShow := []string{
		"LoadState", "FragmentPath", "DropInPaths", "ActiveState", "SubState", "ControlGroup",
		"Type", "NotifyAccess", "User", "Group", "NoNewPrivileges", "UMask", "WorkingDirectory",
		"KillMode", "SendSIGKILL", "TimeoutStopUSec", "Restart",
		"ExecStart", "ExecStartPre", "ExecStop", "ExecStopPost", "Slice", "RuntimeDirectory",
		"RuntimeDirectoryMode", "Delegate", "DelegateSubgroup", "After", "Before", "BindsTo",
		"Requires", "Requisite", "Wants", "PartOf", "Conflicts",
		"RequiresMountsFor", "Environment", "EnvironmentFiles", "PassEnvironment", "UnsetEnvironment",
		"SupplementaryGroups",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/systemctl", "show", "--all", "--no-pager", "--property="+strings.Join(propertiesToShow, ","), policy.unit)
	command.Env = []string{"HOME=/", "PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "SYSTEMD_COLORS=0", "SYSTEMD_PAGER=cat"}
	var stdout, stderr boundedLinuxOutput
	stdout.limit = linuxSystemdShowMaxBytes
	stderr.limit = linuxSystemdShowMaxBytes
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("effective systemd show for %s is unavailable: %w", policy.unit, ctx.Err())
		}
		if stdout.tooLarge || stderr.tooLarge || errors.Is(err, errLinuxPreflightOutputTooLarge) {
			return fmt.Errorf("effective systemd show for %s exceeded its bounded output", policy.unit)
		}
		return fmt.Errorf("query effective systemd properties for %s: %w: %s", policy.unit, err, strings.TrimSpace(stderr.String()))
	}
	properties, err := parseLinuxSystemdShowProperties(stdout.Bytes())
	if err != nil {
		return fmt.Errorf("parse effective systemd properties for %s: %w", policy.unit, err)
	}
	if err := validateEffectiveLinuxSystemdProperties(properties, policy, validatedDropIns, a, b); err != nil {
		return fmt.Errorf("effective %s unit violates the owner service contract: %w", policy.unit, err)
	}
	return nil
}

var errLinuxPreflightOutputTooLarge = errors.New("read-only preflight command output exceeds its bound")

type boundedLinuxOutput struct {
	bytes.Buffer
	limit    int
	tooLarge bool
}

func (output *boundedLinuxOutput) Write(value []byte) (int, error) {
	if len(value) > output.limit-output.Len() {
		output.tooLarge = true
		return 0, errLinuxPreflightOutputTooLarge
	}
	return output.Buffer.Write(value)
}

func parseLinuxSystemdShowProperties(contents []byte) (map[string]string, error) {
	properties := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), linuxSystemdShowMaxBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if !found || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("systemd show line %d is not a property assignment", lineNumber)
		}
		key = strings.TrimSpace(key)
		if _, exists := properties[key]; exists {
			return nil, fmt.Errorf("systemd show repeats property %s", key)
		}
		properties[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return properties, nil
}

func validateEffectiveLinuxSystemdProperties(properties map[string]string, policy linuxSystemdUnitPolicy, validatedDropIns []string, a, b linuxACLSubject) error {
	for _, key := range []string{"LoadState", "FragmentPath", "DropInPaths", "Type", "User", "Group", "NoNewPrivileges", "UMask", "WorkingDirectory",
		"KillMode", "SendSIGKILL", "TimeoutStopUSec", "Restart", "ExecStart", "ExecStartPre",
		"ExecStop", "ExecStopPost", "Delegate", "DelegateSubgroup", "After", "Before", "BindsTo", "Requires", "Requisite",
		"Wants", "PartOf", "Conflicts", "RequiresMountsFor", "Environment", "EnvironmentFiles",
		"PassEnvironment", "UnsetEnvironment", "SupplementaryGroups"} {
		if _, ok := properties[key]; !ok {
			return fmt.Errorf("systemd show did not return required property %s", key)
		}
	}
	fragment := filepath.Join("/etc/systemd/system", policy.unit)
	if properties["LoadState"] != "loaded" || properties["FragmentPath"] != fragment {
		return fmt.Errorf("effective fragment is %q with LoadState %q, require the installed %s unit", properties["FragmentPath"], properties["LoadState"], fragment)
	}
	if err := verifyLinuxSystemdDropInList(properties["DropInPaths"], validatedDropIns); err != nil {
		return err
	}
	for key, expected := range map[string]string{
		"User": policy.user, "Group": policy.group, "NoNewPrivileges": "no", "UMask": "0077",
		"WorkingDirectory": "/", "KillMode": "control-group", "SendSIGKILL": "yes", "Restart": "on-failure",
	} {
		if !strings.EqualFold(properties[key], expected) {
			return fmt.Errorf("effective %s=%q, require %q", key, properties[key], expected)
		}
	}
	stopDuration := uint64(120_000_000)
	if policy.owner {
		stopDuration = 720_000_000
	}
	if err := requireLinuxSystemdShowDuration(properties, "TimeoutStopUSec", stopDuration); err != nil {
		return err
	}
	if err := requireLinuxSystemdShowExec(properties, "ExecStart", policy.execStart); err != nil {
		return err
	}
	if err := requireLinuxSystemdShowExec(properties, "ExecStartPre", policy.execStartPre); err != nil {
		return err
	}
	if err := requireLinuxSystemdShowExec(properties, "ExecStop", policy.execStop); err != nil {
		return err
	}
	if err := requireLinuxSystemdShowExec(properties, "ExecStopPost", nil); err != nil {
		return err
	}
	if !systemdListHasItem([]string{properties["After"]}, "local-fs.target") {
		return errors.New("effective unit ordering omits local-fs.target")
	}
	if policy.owner {
		for _, key := range []string{"Before", "After", "BindsTo", "Requires", "Requisite", "Wants", "PartOf", "Conflicts"} {
			if systemdListHasItem([]string{properties[key]}, "symmetry-daemon.service") {
				return fmt.Errorf("effective owner unit depends on or orders against symmetry-daemon.service through %s", key)
			}
		}
		for key, expected := range map[string]string{
			"Type": "notify", "NotifyAccess": "main", "Slice": "system.slice", "RuntimeDirectory": "symmetry-owner",
			"RuntimeDirectoryMode": "0700", "Delegate": "yes", "DelegateSubgroup": policy.delegatedSubgroup,
			"ControlGroup": "/system.slice/symmetry-owner.service", "ActiveState": "active", "SubState": "running",
		} {
			if properties[key] != expected {
				return fmt.Errorf("effective owner %s=%q, require %q", key, properties[key], expected)
			}
		}
		if !systemdListHasItem([]string{properties["RequiresMountsFor"]}, "/sys/fs/cgroup") {
			return errors.New("effective owner unit omits RequiresMountsFor=/sys/fs/cgroup")
		}
	} else {
		for key, expected := range map[string]string{"Type": "simple", "Delegate": "no"} {
			if !strings.EqualFold(properties[key], expected) {
				return fmt.Errorf("effective daemon %s=%q, require %q", key, properties[key], expected)
			}
		}
		if properties["DelegateSubgroup"] != "" || properties["RuntimeDirectory"] != "" {
			return errors.New("effective daemon unit must not own a delegated/runtime directory")
		}
		for _, key := range []string{"After", "BindsTo"} {
			if !systemdListHasItem([]string{properties[key]}, "symmetry-owner.service") {
				return fmt.Errorf("effective daemon %s omits symmetry-owner.service", key)
			}
		}
	}
	effectiveEnvironment := make(map[string][]string, 4)
	for property, key := range map[string]string{
		"Environment": "environment", "EnvironmentFiles": "environmentfile", "PassEnvironment": "passenvironment",
		"UnsetEnvironment": "unsetenvironment",
	} {
		if value := properties[property]; value != "" {
			effectiveEnvironment[key] = []string{value}
		}
	}
	if err := validateLinuxSystemdEnvironment(effectiveEnvironment, policy.home, a, b); err != nil {
		return fmt.Errorf("effective environment can alter trusted Git/credential behavior: %w", err)
	}
	if strings.TrimSpace(properties["SupplementaryGroups"]) != "" {
		return errors.New("effective SupplementaryGroups adds host groups to A")
	}
	if strings.TrimSpace(properties["EnvironmentFiles"]) != "" || strings.TrimSpace(properties["PassEnvironment"]) != "" || strings.TrimSpace(properties["UnsetEnvironment"]) != "" {
		return errors.New("effective unit has an EnvironmentFile/PassEnvironment/UnsetEnvironment override")
	}
	return nil
}

func requireLinuxSystemdShowDuration(properties map[string]string, key string, expected uint64) error {
	actual, err := parseLinuxSystemdDurationMicroseconds(properties[key])
	if err != nil || actual != expected {
		return fmt.Errorf("effective %s=%q, require exactly %d microseconds", key, properties[key], expected)
	}
	return nil
}

func parseLinuxSystemdDurationMicroseconds(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("empty systemd duration")
	}
	if raw, err := strconv.ParseUint(value, 10, 64); err == nil {
		return raw, nil
	}
	fields := strings.Fields(value)
	var total uint64
	for _, field := range fields {
		index := 0
		for index < len(field) && field[index] >= '0' && field[index] <= '9' {
			index++
		}
		if index == 0 || index == len(field) {
			return 0, fmt.Errorf("unsupported systemd duration component %q", field)
		}
		amount, err := strconv.ParseUint(field[:index], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid systemd duration component %q", field)
		}
		var multiplier uint64
		switch field[index:] {
		case "h":
			multiplier = 3_600_000_000
		case "min":
			multiplier = 60_000_000
		case "s":
			multiplier = 1_000_000
		case "ms":
			multiplier = 1_000
		case "us", "µs":
			multiplier = 1
		case "ns":
			if amount%1_000 != 0 {
				return 0, errors.New("systemd nanosecond duration is not an exact microsecond")
			}
			amount /= 1_000
			multiplier = 1
		default:
			return 0, fmt.Errorf("unsupported systemd duration unit %q", field[index:])
		}
		if amount > ^uint64(0)/multiplier {
			return 0, errors.New("systemd duration overflows microseconds")
		}
		component := amount * multiplier
		if total > ^uint64(0)-component {
			return 0, errors.New("systemd duration overflows microseconds")
		}
		total += component
	}
	return total, nil
}

func requireLinuxSystemdShowExec(properties map[string]string, key string, expected []string) error {
	actual, err := parseLinuxSystemdShowExec(properties[key])
	if err != nil {
		return fmt.Errorf("parse effective %s: %w", key, err)
	}
	if len(expected) == 0 {
		if len(actual.argv) != 0 {
			return fmt.Errorf("effective %s unexpectedly contains argv %q", key, actual.argv)
		}
		return nil
	}
	if actual.flags != "" || actual.ignoreErrors != "no" {
		return fmt.Errorf("effective %s has unsafe flags=%q ignore_errors=%q", key, actual.flags, actual.ignoreErrors)
	}
	if len(actual.argv) != len(expected) {
		return fmt.Errorf("effective %s argv is %q, require %q", key, actual.argv, expected)
	}
	for index := range expected {
		if actual.argv[index] != expected[index] {
			return fmt.Errorf("effective %s argv is %q, require %q", key, actual.argv, expected)
		}
	}
	return nil
}

type linuxSystemdExecProperty struct {
	argv         []string
	flags        string
	ignoreErrors string
}

func parseLinuxSystemdShowExec(value string) (linuxSystemdExecProperty, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "{}" || value == "[]" {
		return linuxSystemdExecProperty{}, nil
	}
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") || strings.Count(value, "{") != 1 || strings.Count(value, "}") != 1 {
		return linuxSystemdExecProperty{}, errors.New("systemd Exec property is not one command object")
	}
	var path, argv, flags, ignoreErrors string
	pathCount, argvCount, flagsCount, ignoreErrorsCount := 0, 0, 0, 0
	for _, part := range strings.Split(strings.TrimSpace(value[1:len(value)-1]), " ; ") {
		key, content, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			return linuxSystemdExecProperty{}, errors.New("systemd Exec property contains a malformed command field")
		}
		switch key {
		case "path":
			path, pathCount = content, pathCount+1
		case "argv[]":
			argv, argvCount = content, argvCount+1
		case "flags":
			flags, flagsCount = content, flagsCount+1
		case "ignore_errors":
			ignoreErrors, ignoreErrorsCount = content, ignoreErrorsCount+1
		}
	}
	if pathCount != 1 || argvCount != 1 || flagsCount != 1 || ignoreErrorsCount > 1 || path == "" || argv == "" {
		return linuxSystemdExecProperty{}, errors.New("systemd Exec property lacks unique path, argv[], and flags fields")
	}
	if ignoreErrorsCount == 0 {
		ignoreErrors = "no"
	}
	arguments := strings.Fields(argv)
	if len(arguments) == 0 || arguments[0] != path {
		return linuxSystemdExecProperty{}, errors.New("systemd Exec path differs from argv[0]")
	}
	return linuxSystemdExecProperty{argv: arguments, flags: flags, ignoreErrors: ignoreErrors}, nil
}

func verifyLinuxSystemdDropInList(actualValue string, expectedPaths []string) error {
	actualFields := strings.Fields(strings.TrimSpace(actualValue))
	actual := make(map[string]struct{}, len(actualFields))
	for _, path := range actualFields {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("effective systemd DropInPaths contains noncanonical path %q", path)
		}
		if _, exists := actual[path]; exists {
			return fmt.Errorf("effective systemd DropInPaths repeats %q", path)
		}
		actual[path] = struct{}{}
	}
	expected := make(map[string]struct{}, len(expectedPaths))
	for _, path := range expectedPaths {
		expected[path] = struct{}{}
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("effective systemd DropInPaths are %q, expected exactly %q", actualFields, expectedPaths)
	}
	for path := range expected {
		if _, ok := actual[path]; !ok {
			return fmt.Errorf("effective systemd DropInPaths omit the validated file %s", path)
		}
	}
	return nil
}

func validateLinuxSystemdCandidate(directives map[string][]string, policy linuxSystemdUnitPolicy, a, b linuxACLSubject) error {
	common := []struct{ key, value string }{
		{"nonewprivileges", "no"}, {"user", policy.user}, {"group", policy.group}, {"umask", "0077"},
		{"workingdirectory", "/"}, {"killmode", "control-group"}, {"sendsigkill", "yes"},
		{"timeoutstartsec", "30s"}, {"restart", "on-failure"}, {"restartsec", "5s"},
	}
	for _, directive := range common {
		if err := requireLinuxSystemdDirective(directives, directive.key, directive.value); err != nil {
			return err
		}
	}
	if len(directives["supplementarygroups"]) != 0 {
		for _, value := range directives["supplementarygroups"] {
			if strings.TrimSpace(value) != "" {
				return errors.New("SupplementaryGroups must not add host groups to A")
			}
		}
	}
	if len(directives["environmentfile"]) != 0 || len(directives["passenvironment"]) != 0 || len(directives["unsetenvironment"]) != 0 {
		return errors.New("EnvironmentFile/PassEnvironment/UnsetEnvironment are not permitted")
	}
	if err := validateLinuxSystemdEnvironment(directives, policy.home, a, b); err != nil {
		return err
	}
	if policy.owner {
		for _, directive := range []struct{ key, value string }{
			{"type", "notify"}, {"notifyaccess", "main"}, {"slice", "system.slice"},
			{"runtimedirectory", "symmetry-owner"}, {"runtimedirectorymode", "0700"},
			{"delegate", policy.delegate}, {"delegatesubgroup", policy.delegatedSubgroup},
			{"timeoutstopsec", "12min"},
		} {
			if err := requireLinuxSystemdDirective(directives, directive.key, directive.value); err != nil {
				return err
			}
		}
		if err := requireLinuxSystemdListExact(directives, "after", []string{"local-fs.target"}); err != nil {
			return err
		}
		if err := requireLinuxSystemdListExact(directives, "requiresmountsfor", []string{"/sys/fs/cgroup"}); err != nil {
			return err
		}
		if err := requireLinuxSystemdExec(directives, "execstart", policy.execStart); err != nil {
			return err
		}
		if err := requireLinuxSystemdExec(directives, "execstop", policy.execStop); err != nil {
			return err
		}
		if len(directives["execstoppost"]) != 0 || len(directives["execstartpre"]) != 0 {
			return errors.New("owner unit must have one recovery-only ExecStop and no ExecStopPost or ExecStartPre")
		}
		if err := rejectLinuxSystemdUnitLists(directives, "before", "bindsto", "requires", "requisite", "wants", "partof", "conflicts", "upholds", "onfailure", "onsuccess"); err != nil {
			return fmt.Errorf("owner unit has an unexpected reverse/dependent service edge: %w", err)
		}
		return nil
	}
	if err := requireLinuxSystemdDirective(directives, "type", "simple"); err != nil {
		return err
	}
	if err := requireLinuxSystemdDirective(directives, "timeoutstopsec", "2min"); err != nil {
		return err
	}
	if err := requireLinuxSystemdListExact(directives, "after", []string{"local-fs.target", "symmetry-owner.service"}); err != nil {
		return err
	}
	if err := requireLinuxSystemdListExact(directives, "bindsto", []string{"symmetry-owner.service"}); err != nil {
		return err
	}
	if err := rejectLinuxSystemdUnitLists(directives, "before", "requires", "requisite", "wants", "partof", "conflicts", "upholds", "onfailure", "onsuccess", "requiresmountsfor"); err != nil {
		return fmt.Errorf("daemon unit has an unexpected service dependency edge: %w", err)
	}
	if err := requireLinuxSystemdExec(directives, "execstartpre", policy.execStartPre); err != nil {
		return err
	}
	if err := requireLinuxSystemdExec(directives, "execstart", policy.execStart); err != nil {
		return err
	}
	if len(directives["execstop"]) != 0 || len(directives["execstoppost"]) != 0 {
		return errors.New("daemon unit must use systemd control-group termination without custom stop hooks")
	}
	if len(directives["delegate"]) != 0 && !systemdDirectiveIs(directives, "delegate", "no") || len(directives["delegatesubgroup"]) != 0 {
		return errors.New("daemon unit must not own or delegate the Linux run cgroup parent")
	}
	return nil
}

func requireLinuxSystemdListExact(directives map[string][]string, key string, expected []string) error {
	values := directives[key]
	if len(values) != 1 || !equalLinuxStrings(strings.Fields(values[0]), expected) {
		return fmt.Errorf("systemd directive %s values are %q, require exactly %q", key, values, strings.Join(expected, " "))
	}
	return nil
}

func equalLinuxStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func rejectLinuxSystemdUnitLists(directives map[string][]string, keys ...string) error {
	for _, key := range keys {
		if len(directives[key]) != 0 {
			return fmt.Errorf("unexpected %s directive %q", key, directives[key])
		}
	}
	return nil
}

func systemdListHasItem(values []string, expected string) bool {
	items := make(map[string]struct{})
	for _, value := range values {
		fields := strings.Fields(value)
		if len(fields) == 0 {
			items = make(map[string]struct{})
			continue
		}
		for _, item := range fields {
			items[item] = struct{}{}
		}
	}
	_, found := items[expected]
	return found
}

func requireLinuxSystemdExec(directives map[string][]string, key string, expected []string) error {
	if len(expected) == 0 || len(directives[key]) != 1 || strings.TrimSpace(directives[key][0]) != strings.Join(expected, " ") {
		return fmt.Errorf("systemd directive %s must contain the exact argv %q", key, strings.Join(expected, " "))
	}
	return nil
}

func systemdDirectiveIs(directives map[string][]string, key, expected string) bool {
	return len(directives[key]) == 1 && strings.EqualFold(strings.TrimSpace(directives[key][0]), expected)
}

func linuxSystemdDropInDirectories(unit string) []string {
	return []string{
		filepath.Join("/etc/systemd/system", unit+".d"),
		filepath.Join("/etc/systemd/system.control", unit+".d"),
		filepath.Join("/etc/systemd/system.attached", unit+".d"),
		filepath.Join("/run/systemd/system", unit+".d"),
		filepath.Join("/run/systemd/system.control", unit+".d"),
		filepath.Join("/run/systemd/system.attached", unit+".d"),
		filepath.Join("/run/systemd/generator.early", unit+".d"),
		filepath.Join("/run/systemd/generator", unit+".d"),
		filepath.Join("/run/systemd/generator.late", unit+".d"),
		filepath.Join("/usr/local/lib/systemd/system", unit+".d"),
		filepath.Join("/usr/lib/systemd/system", unit+".d"),
		filepath.Join("/usr/lib/systemd/system.control", unit+".d"),
	}
}

func verifyLinuxSystemdDirectory(path string, a, b linuxACLSubject) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("systemd directory path must be canonical and absolute")
	}
	for directory := path; ; directory = filepath.Dir(directory) {
		info, err := lstatCanonicalLinuxPath(directory)
		if err != nil {
			return fmt.Errorf("inspect systemd unit directory %s: %w", directory, err)
		}
		identity, err := linuxFileIdentity(info)
		if err != nil {
			return err
		}
		if err := validateLinuxSystemdDirectoryIdentity(identity, info.IsDir(), info.Mode()); err != nil {
			return fmt.Errorf("systemd unit ancestor %s is unsafe: %w", directory, err)
		}
		if err := verifyLinuxNoUntrustedACLWrite(directory, []uint32{0}, nil, true); err != nil {
			return fmt.Errorf("systemd unit ancestor %s has an untrusted ACL writer: %w", directory, err)
		}
		for _, subject := range []linuxACLSubject{a, b} {
			if err := verifyEffectiveLinuxAccess(directory, subject, 1, 2); err != nil {
				return fmt.Errorf("systemd unit ancestor %s is writable or unsearchable for UID %d: %w", directory, subject.uid, err)
			}
		}
		if directory == "/" {
			return nil
		}
	}
}

func validateLinuxSystemdDirectoryIdentity(identity linuxStatIdentity, directory bool, mode os.FileMode) error {
	if !directory || identity.uid != 0 || identity.gid != 0 || mode.Perm()&0022 != 0 {
		return errors.New("systemd unit directories and ancestors must be root:root-owned and group/other-nonwritable")
	}
	return nil
}

func verifyLinuxOptionalSystemdDirectory(path string, a, b linuxACLSubject) (bool, error) {
	info, err := lstatCanonicalLinuxPath(path)
	if err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("systemd drop-in path %s is not a directory", path)
		}
		return true, verifyLinuxSystemdDirectory(path, a, b)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect systemd drop-in directory %s: %w", path, err)
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := lstatCanonicalLinuxPath(parent)
		if err == nil {
			if !info.IsDir() {
				return false, fmt.Errorf("systemd drop-in ancestor %s is not a directory", parent)
			}
			if err := verifyLinuxSystemdDirectory(parent, a, b); err != nil {
				return false, err
			}
			return false, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect systemd drop-in ancestor %s: %w", parent, err)
		}
		if parent == "/" {
			return false, errors.New("cannot locate an existing systemd drop-in ancestor")
		}
	}
}

func readLinuxSystemdDirectives(path string, a, b linuxACLSubject) (map[string][]string, error) {
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return nil, err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || !info.Mode().IsRegular() || identity.uid != 0 || identity.gid != 0 || identity.links != 1 || info.Mode().Perm()&0022 != 0 || info.Size() <= 0 || info.Size() > linuxSystemdUnitMaxBytes {
		return nil, errors.New("unit file must be a root-owned, singly-linked, non-writable regular file with a bounded size")
	}
	if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0}, nil, false); err != nil {
		return nil, fmt.Errorf("unit file has an untrusted ACL writer: %w", err)
	}
	for _, subject := range []linuxACLSubject{a, b} {
		if err := verifyEffectiveLinuxAccess(path, subject, 4, 2); err != nil {
			return nil, fmt.Errorf("systemd unit file %s is unreadable or writable by UID %d: %w", path, subject.uid, err)
		}
	}
	contents, err := readBoundedLinuxFile(path, linuxSystemdUnitMaxBytes)
	if err != nil {
		return nil, err
	}
	return parseLinuxSystemdServiceDirectives(contents)
}

func parseLinuxSystemdServiceDirectives(contents []byte) (map[string][]string, error) {
	result := make(map[string][]string)
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), linuxSystemdUnitMaxBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line, err := stripGitConfigComment(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("systemd line %d: %w", lineNumber, err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("systemd line %d has an incomplete section", lineNumber)
			}
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("systemd line %d is not a directive assignment", lineNumber)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch section {
		case "unit":
			switch key {
			case "description":
			case "after", "before", "bindsto", "requires", "requisite", "wants", "partof", "conflicts", "upholds", "onfailure", "onsuccess", "requiresmountsfor":
				result[key] = append(result[key], value)
			default:
				return nil, fmt.Errorf("unreviewed systemd Unit directive %s", key)
			}
		case "service":
			switch key {
			case "nonewprivileges", "user", "group", "supplementarygroups", "delegate", "delegatesubgroup",
				"environment", "environmentfile", "passenvironment", "unsetenvironment", "execsearchpath",
				"type", "notifyaccess", "slice", "runtimedirectory", "runtimedirectorymode", "killmode", "sendsigkill",
				"timeoutstartsec", "timeoutstopsec", "restart", "restartsec", "umask", "workingdirectory",
				"execstart", "execstartpre", "execstop", "execstoppost":
				result[key] = append(result[key], value)
			default:
				return nil, fmt.Errorf("unreviewed systemd Service directive %s", key)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func requireLinuxSystemdDirective(directives map[string][]string, key, expected string) error {
	values := directives[key]
	valid := len(values) == 1 && values[0] == expected
	if len(values) == 1 && (key == "nonewprivileges" || key == "delegate") {
		valid = strings.EqualFold(values[0], expected)
	}
	if !valid {
		return fmt.Errorf("directive %s values are %q, require exactly %q", key, values, expected)
	}
	return nil
}

func requireLinuxSystemdIdentityDirective(directives map[string][]string, key, expected string) error {
	values := directives[key]
	if len(values) != 1 || values[0] != expected {
		return fmt.Errorf("directive %s values are %q, require exact A identity %q", key, values, expected)
	}
	return nil
}

func validateLinuxSystemdOverrides(directives map[string][]string, policy linuxSystemdUnitPolicy, a, b linuxACLSubject) error {
	for key, values := range directives {
		if len(values) == 0 {
			continue
		}
		if key == "environment" {
			if err := validateLinuxSystemdEnvironment(map[string][]string{"environment": values}, policy.home, a, b); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("systemd drop-in override of %s is not permitted", key)
	}
	return nil
}

func validateLinuxExecSearchPaths(values []string, a, b linuxACLSubject) error {
	for _, searchPath := range values {
		for _, directory := range strings.Fields(searchPath) {
			if err := verifyLinuxServicePath(directory, a, b); err != nil {
				return fmt.Errorf("ExecSearchPath directory is not trusted: %w", err)
			}
		}
	}
	return nil
}

func validateLinuxSystemdEnvironment(directives map[string][]string, aHome string, a, b linuxACLSubject) error {
	if len(directives["environmentfile"]) != 0 || len(directives["passenvironment"]) != 0 || len(directives["unsetenvironment"]) != 0 {
		return errors.New("EnvironmentFile/PassEnvironment/UnsetEnvironment can alter unverified credentials or Git behavior")
	}
	for _, directive := range directives["environment"] {
		assignments, err := splitLinuxSystemdEnvironment(directive)
		if err != nil {
			return err
		}
		for _, assignment := range assignments {
			key, value, found := strings.Cut(assignment, "=")
			if !found {
				return errors.New("systemd Environment contains an opaque assignment")
			}
			key = strings.ToUpper(strings.Trim(key, `"'`))
			if strings.HasPrefix(key, "GIT_") || linuxCredentialEnvironmentName(key) {
				return fmt.Errorf("systemd Environment injects forbidden Git or credential override %q", key)
			}
			switch {
			case key == "HOME":
				if value != aHome {
					return errors.New("systemd HOME must remain the configured A home; B home is only for the native launcher")
				}
			case key == "PATH":
				if err := verifyLinuxServicePath(value, a, b); err != nil {
					return err
				}
			case key == "LANG" || key == "LC_ALL" || key == "LC_CTYPE":
			case strings.HasPrefix(key, "LC_"):
			default:
				return fmt.Errorf("unapproved systemd Environment directive %q", key)
			}
		}
	}
	return validateLinuxExecSearchPaths(directives["execsearchpath"], a, b)
}

func splitLinuxSystemdEnvironment(value string) ([]string, error) {
	var result []string
	var token strings.Builder
	quoted := false
	escaped := false
	started := false
	for index := range len(value) {
		character := value[index]
		if escaped {
			if character != ' ' && character != '\t' && character != '\\' && character != '"' {
				return nil, errors.New("systemd Environment uses an unsupported escape")
			}
			token.WriteByte(character)
			escaped = false
			continue
		}
		if quoted && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			started = true
			continue
		}
		if !quoted && (character == ' ' || character == '\t') {
			if started {
				result = append(result, token.String())
				token.Reset()
				started = false
			}
			continue
		}
		token.WriteByte(character)
		started = true
	}
	if quoted || escaped {
		return nil, errors.New("systemd Environment has an unterminated quote or escape")
	}
	if started {
		result = append(result, token.String())
	}
	return result, nil
}

func verifyLinuxServicePath(value string, a, b linuxACLSubject) error {
	if value == "" {
		return errors.New("systemd PATH must not be empty")
	}
	for _, path := range strings.Split(value, string(os.PathListSeparator)) {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("systemd PATH contains an empty or non-canonical entry")
		}
		if err := verifyLinuxServicePathAncestors(path, b, a.uid); err != nil {
			return fmt.Errorf("systemd PATH entry %s has an untrusted parent: %w", path, err)
		}
		linkInfo, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect systemd PATH entry %s: %w", path, err)
		}
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			linkIdentity, identityErr := linuxFileIdentity(linkInfo)
			if identityErr != nil || linkIdentity.uid != 0 && linkIdentity.uid != a.uid {
				return fmt.Errorf("systemd PATH link %s is not root/A-owned", path)
			}
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve systemd PATH entry %s: %w", path, err)
		}
		if !filepath.IsAbs(resolved) || filepath.Clean(resolved) != resolved {
			return fmt.Errorf("systemd PATH entry %s resolves to a non-canonical directory", path)
		}
		info, err := lstatCanonicalLinuxPath(resolved)
		if err != nil {
			return fmt.Errorf("inspect resolved systemd PATH entry %s: %w", resolved, err)
		}
		identity, err := linuxFileIdentity(info)
		if err != nil || !info.IsDir() || identity.uid != 0 && identity.uid != a.uid || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("systemd PATH entry %s must resolve to a root/A-owned, non-writable directory", path)
		}
		if err := verifyLinuxAControlledGitParents(resolved, b, a.uid, a.gid); err != nil {
			return fmt.Errorf("systemd PATH entry %s can be replaced by B: %w", path, err)
		}
		if err := verifyEffectiveLinuxAccess(resolved, a, 5, 2); err != nil {
			return fmt.Errorf("A cannot search/execute in systemd PATH entry %s: %w", path, err)
		}
	}
	return nil
}

func verifyLinuxServicePathAncestors(path string, b linuxACLSubject, ownerUID uint32) error {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := lstatCanonicalLinuxPath(parent)
		if err != nil {
			return err
		}
		identity, err := linuxFileIdentity(info)
		if err != nil || !info.IsDir() || identity.uid != 0 && identity.uid != ownerUID || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("PATH ancestor %s is not root/A-owned and protected", parent)
		}
		if err := verifyEffectiveLinuxAccess(parent, b, 1, 2); err != nil {
			return fmt.Errorf("PATH ancestor %s is B-writable or unsearchable: %w", parent, err)
		}
		if parent == "/" {
			return nil
		}
	}
}

func verifyLinuxOwnerAnchorProcessContext(expected authority.LinuxCgroupProcessIdentity, accounts linuxOwnerAccounts) error {
	if expected.Validate() != nil {
		return errors.New("owner process identity is incomplete for privilege-state verification")
	}
	identity, err := platform.ProcessIdentity(expected.PID)
	if err != nil || identity != expected.Identity {
		return errors.New("owner process identity changed before privilege-state verification")
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", expected.PID))
	if err != nil {
		return fmt.Errorf("read owner process privilege state: %w", err)
	}
	noNewPrivileges := ""
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "NoNewPrivs:"); ok {
			noNewPrivileges = strings.TrimSpace(value)
			break
		}
	}
	after, identityErr := platform.ProcessIdentity(expected.PID)
	if identityErr != nil || after != expected.Identity {
		return errors.New("owner process identity changed during privilege-state verification")
	}
	if noNewPrivileges != "0" {
		return errors.New("live owner service has NoNewPrivileges enabled or unavailable")
	}
	environment, err := readBoundedLinuxFile(fmt.Sprintf("/proc/%d/environ", expected.PID), 1<<20)
	if err != nil {
		return fmt.Errorf("read live owner service environment: %w", err)
	}
	for _, entry := range bytes.Split(environment, []byte{0}) {
		key, value, found := strings.Cut(string(entry), "=")
		if !found || key == "" {
			continue
		}
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "GIT_") || linuxCredentialEnvironmentName(upper) {
			return fmt.Errorf("live owner service inherits unsafe Git or credential environment %q", key)
		}
		if key == "HOME" && value != accounts.a.HomeDir {
			return errors.New("live owner service HOME differs from the configured A home")
		}
		if key == "PATH" {
			if err := verifyLinuxServicePath(value, accounts.aSubject, accounts.bSubject); err != nil {
				return fmt.Errorf("live owner service PATH is not trusted: %w", err)
			}
		}
	}
	finalIdentity, finalErr := platform.ProcessIdentity(expected.PID)
	if finalErr != nil || finalIdentity != expected.Identity {
		return errors.New("owner process identity changed while checking its environment")
	}
	return nil
}

func verifyExistingLinuxStateLayout(directory string, ownerUID uint32, b *linuxACLSubject) error {
	if directory == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return errors.New("state_dir must be an existing canonical absolute path for read-only owner preflight")
	}
	if err := verifyLinuxTrustedPathAncestors(directory, ownerUID, uint32(os.Getegid()), b); err != nil {
		return fmt.Errorf("StateDir ancestor is not immutable against untrusted identities: %w", err)
	}
	for _, path := range []string{directory, filepath.Join(directory, linuxOwnerStateDirName)} {
		info, err := lstatCanonicalLinuxPath(path)
		if err != nil {
			return fmt.Errorf("inspect existing private state directory %s: %w", path, err)
		}
		identity, err := linuxFileIdentity(info)
		if err != nil || !info.IsDir() || identity.uid != ownerUID || info.Mode().Perm() != 0o700 {
			return fmt.Errorf("existing state directory %s must be A-owned mode 0700", path)
		}
		if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0, ownerUID}, []uint32{uint32(os.Getegid())}, true); err != nil {
			return fmt.Errorf("existing state directory %s is changeable by an untrusted identity: %w", path, err)
		}
		if b != nil {
			if err := verifyLinuxDefaultLinuxAccess(path, *b, 0, 7); err != nil {
				return fmt.Errorf("future StateDir entries are accessible to fixed B: %w", err)
			}
		}
		if b != nil {
			if err := verifyEffectiveLinuxAccess(path, *b, 0, 7); err != nil {
				return fmt.Errorf("fixed B can traverse or read private state directory %s: %w", path, err)
			}
		}
	}
	lockPath := filepath.Join(directory, linuxOwnerLockFile)
	lockInfo, err := lstatCanonicalLinuxPath(lockPath)
	if err != nil {
		return fmt.Errorf("inspect existing state Store lock: %w", err)
	}
	lockIdentity, err := linuxFileIdentity(lockInfo)
	if err != nil || !lockInfo.Mode().IsRegular() || lockIdentity.uid != ownerUID || lockInfo.Mode().Perm() != 0o600 || lockIdentity.links != 1 {
		return errors.New("existing Store lock must be a singly-linked A-owned mode-0600 regular file")
	}
	if err := verifyLinuxNoUntrustedACLWrite(lockPath, []uint32{0, ownerUID}, []uint32{uint32(os.Getegid())}, false); err != nil {
		return fmt.Errorf("existing Store lock is changeable by an untrusted identity: %w", err)
	}
	if b != nil {
		if err := verifyEffectiveLinuxAccess(lockPath, *b, 0, 7); err != nil {
			return fmt.Errorf("fixed B can access the private Store lock: %w", err)
		}
	}
	return nil
}

func verifyLinuxOwnerStateDirBinding(configuredPath string, owner linuxgate.OwnerConfig) error {
	installedPath := owner.StateDir
	configuredPath, err := canonicalLinuxOwnerStateDir(configuredPath)
	if err != nil || installedPath == "" || !filepath.IsAbs(installedPath) ||
		filepath.Clean(installedPath) != installedPath || configuredPath != installedPath {
		return errors.New("daemon StateDir must exactly match the root-installed canonical OwnerConfig.StateDir")
	}
	if _, err := lstatCanonicalLinuxPath(configuredPath); err != nil {
		return fmt.Errorf("inspect configured StateDir without following links: %w", err)
	}
	if _, err := lstatCanonicalLinuxPath(installedPath); err != nil {
		return fmt.Errorf("inspect installed OwnerConfig.StateDir without following links: %w", err)
	}
	configuredFD, err := unix.Open(configuredPath, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("pin configured StateDir: %w", err)
	}
	defer unix.Close(configuredFD)
	installedFD, err := unix.Open(installedPath, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("pin root-installed OwnerConfig.StateDir: %w", err)
	}
	defer unix.Close(installedFD)
	var configured, installed unix.Stat_t
	if err := unix.Fstat(configuredFD, &configured); err != nil {
		return fmt.Errorf("stat pinned configured StateDir: %w", err)
	}
	if err := unix.Fstat(installedFD, &installed); err != nil {
		return fmt.Errorf("stat pinned root-installed OwnerConfig.StateDir: %w", err)
	}
	if configured.Dev != installed.Dev || configured.Ino != installed.Ino ||
		configured.Uid != installed.Uid || configured.Gid != installed.Gid || configured.Mode != installed.Mode {
		return errors.New("configured and installed StateDir descriptors identify different filesystem objects")
	}
	if configured.Mode&unix.S_IFMT != unix.S_IFDIR || configured.Uid != owner.AUID || configured.Gid != owner.AGID ||
		configured.Mode&0o7777 != 0o700 {
		return errors.New("installed StateDir identity must be the fixed A-owned mode-0700 directory")
	}
	return nil
}

func canonicalLinuxOwnerStateDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" || strings.ContainsAny(path, "\x00\r\n") {
		return "", errors.New("StateDir is empty or contains a forbidden character")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(string(filepath.Separator), path)
	}
	path = filepath.Clean(path)
	if path == string(filepath.Separator) {
		return "", errors.New("StateDir must not resolve to the filesystem root")
	}
	return path, nil
}

func verifyDrainedLinuxStore(store *state.Store, directory string) error {
	if store == nil {
		return errors.New("legacy owner preflight Store is unavailable")
	}
	runDir := filepath.Join(directory, linuxOwnerStateDirName)
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return fmt.Errorf("list run directory before read-only journal scan: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() || !validLinuxJournalFilename(name) {
			return fmt.Errorf("unexpected or temporary run-state entry %q blocks read-only legacy owner preflight", name)
		}
	}
	journals, err := store.ListJournals()
	if err != nil {
		return fmt.Errorf("strictly read all run journals under the Store lock: %w", err)
	}
	known := make(map[string]struct{}, len(journals))
	for _, journal := range journals {
		known[linuxJournalFilename(journal.Key())] = struct{}{}
	}
	if len(known) != len(entries) {
		return errors.New("run directory contains an unrecognized journal or unreadable RunKey")
	}
	for _, entry := range entries {
		if _, ok := known[entry.Name()]; !ok {
			return fmt.Errorf("run journal %q does not map to one strict RunKey", entry.Name())
		}
	}
	return verifyDrainedLinuxJournals(journals)
}

func validLinuxJournalFilename(name string) bool {
	if len(name) != len(linuxOwnerJournalPrefix)+sha256.Size*2+len(linuxOwnerJournalSuffix) ||
		!strings.HasPrefix(name, linuxOwnerJournalPrefix) || !strings.HasSuffix(name, linuxOwnerJournalSuffix) {
		return false
	}
	return isLowerHex(name[len(linuxOwnerJournalPrefix) : len(name)-len(linuxOwnerJournalSuffix)])
}

func linuxJournalFilename(key state.RunKey) string {
	input := key.RunID + "\x00" + strconv.FormatInt(key.Generation, 10)
	digest := sha256.Sum256([]byte(input))
	return linuxOwnerJournalPrefix + hex.EncodeToString(digest[:]) + linuxOwnerJournalSuffix
}

func isLowerHex(value string) bool {
	for i := range value {
		if !(value[i] >= '0' && value[i] <= '9' || value[i] >= 'a' && value[i] <= 'f') {
			return false
		}
	}
	return value != ""
}

func verifyDrainedLinuxJournals(journals []state.RunJournal) error {
	for _, journal := range journals {
		key := journal.Key()
		if journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil ||
			journal.ContainmentAuthority != nil && journal.ContainmentAuthority.Version != authority.SupervisorVersion ||
			journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Version != authority.SupervisorHandoffVersion {
			return fmt.Errorf("run %s/%d contains unexpected v2 or unknown Linux containment state", key.RunID, key.Generation)
		}
		if journal.ContainmentAuthority != nil {
			value := journal.ContainmentAuthority
			if value.StopReceipt == nil || !value.StopReceipt.ValidFor(*value) || journal.PID != value.TargetPID || journal.ProcessIdentity != value.TargetIdentity {
				return fmt.Errorf("run %s/%d has a v1 authority without an exact terminal stop receipt", key.RunID, key.Generation)
			}
			return fmt.Errorf("run %s/%d retains v1 process authority after its stop receipt; old-topology release and clear are still required", key.RunID, key.Generation)
		}
		if journal.ContainmentHandoff != nil {
			value := journal.ContainmentHandoff
			if value.StopReceipt == nil || !value.StopReceipt.ValidForHandoff(*value) {
				return fmt.Errorf("run %s/%d has a v1 handoff without an exact terminal stop receipt", key.RunID, key.Generation)
			}
			return fmt.Errorf("run %s/%d retains a receipt-bearing v1 handoff; old-topology release and clear are still required", key.RunID, key.Generation)
		}
		if journal.ContainmentScans != nil || journal.ContainmentUnproven || journal.HasProcessDetails() || journal.HasPendingContainment() {
			return fmt.Errorf("run %s/%d has an unresolved v1 process marker, helper, scan, or containment proof", key.RunID, key.Generation)
		}
		if journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" {
			return fmt.Errorf("run %s/%d is not terminal; absence of a process marker is not a v1 stop receipt", key.RunID, key.Generation)
		}
	}
	return nil
}

func canonicalLinuxCgroupPath(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value && !strings.ContainsRune(value, '\\') && !strings.ContainsRune(value, 0)
}

func lstatCanonicalLinuxPath(path string) (os.FileInfo, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("path is not canonical and absolute")
	}
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(path, current), current)
	var info os.FileInfo
	for index, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || index != len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("path component %s is a symlink or non-directory", current)
		}
	}
	if info == nil {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		return info, nil
	}
	return info, nil
}

type linuxACLSubject struct {
	uid    uint32
	gid    uint32
	groups []uint32
}

func (subject linuxACLSubject) hasGroup(gid uint32) bool {
	if subject.gid == gid {
		return true
	}
	for _, group := range subject.groups {
		if group == gid {
			return true
		}
	}
	return false
}

type linuxACLEntry struct {
	tag  uint16
	perm uint16
	id   uint32
}

const (
	linuxACLVersion  = 2
	linuxACLUserObj  = 0x01
	linuxACLUser     = 0x02
	linuxACLGroupObj = 0x04
	linuxACLGroup    = 0x08
	linuxACLMask     = 0x10
	linuxACLOther    = 0x20
	linuxACLNoID     = ^uint32(0)
)

func verifyEffectiveLinuxAccess(path string, subject linuxACLSubject, required, forbidden uint16) error {
	permissions, err := effectiveLinuxPathPermissions(path, subject)
	if err != nil {
		return err
	}
	return validateEffectiveLinuxPermissions(permissions, required, forbidden, subject)
}

func effectiveLinuxPathPermissions(path string, subject linuxACLSubject) (uint16, error) {
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return 0, err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil {
		return 0, err
	}
	return effectiveLinuxACLPermissions(path, info.Mode(), identity, subject)
}

func validateEffectiveLinuxPermissions(permissions, required, forbidden uint16, subject linuxACLSubject) error {
	if permissions&required != required || permissions&forbidden != 0 {
		return fmt.Errorf("effective permissions for UID %d GID %d are %#o, require %#o and forbid %#o", subject.uid, subject.gid, permissions, required, forbidden)
	}
	return nil
}

func effectiveLinuxACLPermissions(path string, mode os.FileMode, identity linuxStatIdentity, subject linuxACLSubject) (uint16, error) {
	acl, found, err := readLinuxPOSIXACL(path, "system.posix_acl_access")
	if err != nil {
		return 0, err
	}
	if !found {
		return modePermissionForSubject(mode, identity, subject), nil
	}
	return permissionsFromLinuxACL(acl, identity, subject)
}

func defaultLinuxACLPermissions(path string, subject linuxACLSubject) (uint16, error) {
	acl, found, err := readLinuxPOSIXACL(path, "system.posix_acl_default")
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, errors.New("directory has no default POSIX ACL for future worktree entries")
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return 0, err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil {
		return 0, err
	}
	return permissionsFromLinuxACL(acl, identity, subject)
}

func verifyLinuxDefaultLinuxAccess(path string, subject linuxACLSubject, required, forbidden uint16) error {
	acl, found, err := readLinuxPOSIXACL(path, "system.posix_acl_default")
	if err != nil || !found {
		return err
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil {
		return err
	}
	permissions, err := permissionsFromLinuxACL(acl, identity, subject)
	if err != nil {
		return err
	}
	return validateEffectiveLinuxPermissions(permissions, required, forbidden, subject)
}

func readLinuxPOSIXACL(path, name string) ([]linuxACLEntry, bool, error) {
	const maximumACLBytes = 64 << 10
	data := make([]byte, maximumACLBytes)
	n, err := unix.Getxattr(path, name, data)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s ACL for %s: %w", name, path, err)
	}
	if n > len(data) || n < 4 || (n-4)%8 != 0 || binary.LittleEndian.Uint32(data[:4]) != linuxACLVersion {
		return nil, false, fmt.Errorf("%s ACL for %s has an unsupported or malformed encoding", name, path)
	}
	entries := make([]linuxACLEntry, 0, (n-4)/8)
	for offset := 4; offset < n; offset += 8 {
		entry := linuxACLEntry{
			tag:  binary.LittleEndian.Uint16(data[offset : offset+2]),
			perm: binary.LittleEndian.Uint16(data[offset+2 : offset+4]),
			id:   binary.LittleEndian.Uint32(data[offset+4 : offset+8]),
		}
		if entry.perm&^uint16(7) != 0 {
			return nil, false, fmt.Errorf("%s ACL for %s contains invalid permission bits", name, path)
		}
		entries = append(entries, entry)
	}
	if err := validateLinuxACLShape(entries); err != nil {
		return nil, false, fmt.Errorf("validate %s ACL for %s: %w", name, path, err)
	}
	return entries, true, nil
}

func verifyLinuxNoUntrustedACLWrite(path string, trustedUIDs, trustedGIDs []uint32, includeDefault bool) error {
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return err
	}
	identity, err := linuxFileIdentity(info)
	if err != nil {
		return err
	}
	access, found, err := readLinuxPOSIXACL(path, "system.posix_acl_access")
	if err != nil {
		return err
	}
	if found {
		if err := validateLinuxACLWriteEntries(access, identity, trustedUIDs, trustedGIDs); err != nil {
			return fmt.Errorf("access ACL: %w", err)
		}
	} else {
		if info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && !linuxIdentityAllowed(trustedGIDs, identity.gid) {
			return errors.New("mode grants write access to an untrusted group or other user")
		}
	}
	if includeDefault && info.IsDir() {
		defaults, found, err := readLinuxPOSIXACL(path, "system.posix_acl_default")
		if err != nil {
			return err
		}
		if found {
			if err := validateLinuxACLWriteEntries(defaults, identity, trustedUIDs, trustedGIDs); err != nil {
				return fmt.Errorf("default ACL: %w", err)
			}
		}
	}
	return nil
}

func validateLinuxACLWriteEntries(entries []linuxACLEntry, identity linuxStatIdentity, trustedUIDs, trustedGIDs []uint32) error {
	mask := uint16(7)
	for _, entry := range entries {
		if entry.tag == linuxACLMask {
			mask = entry.perm
			break
		}
	}
	for _, entry := range entries {
		permissions := entry.perm
		if entry.tag == linuxACLUser || entry.tag == linuxACLGroup || entry.tag == linuxACLGroupObj {
			permissions &= mask
		}
		if permissions&2 == 0 {
			continue
		}
		switch entry.tag {
		case linuxACLUserObj:
			if !linuxIdentityAllowed(trustedUIDs, identity.uid) {
				return fmt.Errorf("inode owner UID %d has write access but is not trusted", identity.uid)
			}
		case linuxACLGroupObj:
			if !linuxIdentityAllowed(trustedGIDs, identity.gid) {
				return fmt.Errorf("owning GID %d has untrusted effective write access", identity.gid)
			}
		case linuxACLUser:
			if !linuxIdentityAllowed(trustedUIDs, entry.id) {
				return fmt.Errorf("named UID %d has untrusted effective write access", entry.id)
			}
		case linuxACLGroup:
			if !linuxIdentityAllowed(trustedGIDs, entry.id) {
				return fmt.Errorf("named GID %d has untrusted effective write access", entry.id)
			}
		case linuxACLOther:
			return errors.New("other-class ACL grants write access")
		}
	}
	return nil
}

func linuxIdentityAllowed(identities []uint32, expected uint32) bool {
	for _, identity := range identities {
		if identity == expected {
			return true
		}
	}
	return false
}

func validateLinuxACLShape(entries []linuxACLEntry) error {
	counts := make(map[uint16]int, 6)
	for _, entry := range entries {
		if entry.tag < linuxACLUserObj || entry.tag > linuxACLOther {
			return errors.New("ACL contains an unknown entry tag")
		}
		counts[entry.tag]++
		if (entry.tag == linuxACLUserObj || entry.tag == linuxACLGroupObj || entry.tag == linuxACLMask || entry.tag == linuxACLOther) && entry.id != linuxACLNoID {
			return errors.New("ACL object, mask, or other entry has a named identity")
		}
		if (entry.tag == linuxACLUser || entry.tag == linuxACLGroup) && entry.id == linuxACLNoID {
			return errors.New("ACL named user/group entry has no identity")
		}
	}
	if counts[linuxACLUserObj] != 1 || counts[linuxACLGroupObj] != 1 || counts[linuxACLOther] != 1 || counts[linuxACLMask] > 1 {
		return errors.New("ACL lacks unique owner/group/other entries or has duplicate mask")
	}
	if counts[linuxACLUser]+counts[linuxACLGroup] != 0 && counts[linuxACLMask] != 1 {
		return errors.New("ACL with named entries lacks its mandatory mask")
	}
	seen := make(map[[2]uint32]struct{}, len(entries))
	for _, entry := range entries {
		if entry.tag != linuxACLUser && entry.tag != linuxACLGroup {
			continue
		}
		key := [2]uint32{uint32(entry.tag), entry.id}
		if _, ok := seen[key]; ok {
			return errors.New("ACL repeats a named user/group identity")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func permissionsFromLinuxACL(entries []linuxACLEntry, identity linuxStatIdentity, subject linuxACLSubject) (uint16, error) {
	var userObj, other, mask uint16
	var hasMask bool
	var namedUser *uint16
	var groupPermissions uint16
	matchedGroup := false
	for i := range entries {
		entry := &entries[i]
		switch entry.tag {
		case linuxACLUserObj:
			userObj = entry.perm
		case linuxACLGroupObj:
			if subject.hasGroup(identity.gid) {
				matchedGroup = true
				groupPermissions |= entry.perm
			}
		case linuxACLUser:
			if entry.id == subject.uid {
				perms := entry.perm
				namedUser = &perms
			}
		case linuxACLGroup:
			if subject.hasGroup(entry.id) {
				matchedGroup = true
				groupPermissions |= entry.perm
			}
		case linuxACLMask:
			mask, hasMask = entry.perm, true
		case linuxACLOther:
			other = entry.perm
		}
	}
	if subject.uid == identity.uid {
		return userObj, nil
	}
	if namedUser != nil {
		if !hasMask {
			return 0, errors.New("named ACL user has no mask")
		}
		return *namedUser & mask, nil
	}
	if matchedGroup {
		if !hasMask {
			return groupPermissions, nil
		}
		return groupPermissions & mask, nil
	}
	return other, nil
}

func modePermissionForSubject(mode os.FileMode, identity linuxStatIdentity, subject linuxACLSubject) uint16 {
	permissions := uint16(mode.Perm())
	if subject.uid == identity.uid {
		return permissions >> 6 & 7
	}
	if subject.hasGroup(identity.gid) {
		return permissions >> 3 & 7
	}
	return permissions & 7
}
