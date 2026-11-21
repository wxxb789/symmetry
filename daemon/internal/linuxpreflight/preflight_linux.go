//go:build linux && amd64

// Package linuxpreflight verifies a Linux amd64 host installation of the
// Symmetry daemon owner: the installed A/B accounts, the delegated cgroup
// parent, the fixed Git clone/repository ownership, the systemd units, and the
// POSIX ACLs that fence A-private state from the fixed B runtime. Every
// verifier is read-only: none of them launches a process, mutates a journal, or
// certifies a future run, and none of them holds daemon state — callers pass the
// daemon configuration and the trusted root-installed owner configuration.
package linuxpreflight

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
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
	stateDir, err := NormalizeOwnerStateDir(value.StateDir)
	if err != nil {
		return fmt.Errorf("canonicalize configured Linux owner StateDir: %w", err)
	}
	value.StateDir = stateDir
	owner, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		return fmt.Errorf("read root-installed Linux A/B owner configuration: %w", err)
	}
	if err := VerifyOwnerIdentity(owner, uint32(os.Getuid()), uint32(os.Geteuid()), uint32(os.Getgid()), uint32(os.Getegid())); err != nil {
		return err
	}
	accounts, err := VerifyOwnerAccounts(owner)
	if err != nil {
		return err
	}
	if err := verifyLinuxNativeRuntimeSelection(value); err != nil {
		return err
	}
	if err := VerifyOwnerStateDirBinding(value.StateDir, owner); err != nil {
		return fmt.Errorf("verify configured StateDir against root-installed owner authority: %w", err)
	}
	if err := verifyExistingLinuxStateLayout(value.StateDir, owner.AUID, &accounts.BSubject); err != nil {
		return fmt.Errorf("verify A-private state and B isolation: %w", err)
	}
	// Legacy drain is a one-time cutover check under the old topology. Normal
	// daemon restarts must admit live v2 journals so the surviving A helper and
	// recovery-only path can stop them. This installation check also runs from
	// an already-running daemon holding the Store lock during native admission.
	if err := verifyInstalledLinuxOwnerAssets(owner, accounts.ASubject, accounts.BSubject); err != nil {
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
	if err := verifyLinuxOwnerParentKillBoundary(value.LinuxOwnerCgroupMount, value.LinuxOwnerDelegatedPath, owner.AUID, accounts.ASubject, accounts.BSubject); err != nil {
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

func linuxCredentialEnvironmentName(name string) bool {
	return strings.HasSuffix(name, "_API_KEY") || strings.HasSuffix(name, "_TOKEN") || strings.HasSuffix(name, "_SECRET") ||
		strings.HasSuffix(name, "_PASSWORD") || strings.HasSuffix(name, "_CREDENTIAL") || strings.HasSuffix(name, "_ACCESS_KEY") ||
		strings.HasSuffix(name, "_KEY_ID") || strings.HasSuffix(name, "_CLIENT_SECRET")
}

func verifyInstalledLinuxOwnerAssets(owner linuxgate.OwnerConfig, a, b ACLSubject) error {
	for _, path := range []string{"/usr", "/usr/libexec", "/usr/libexec/symmetry", "/etc", "/etc/symmetry"} {
		info, err := LstatCanonicalPath(path)
		if err != nil {
			return fmt.Errorf("inspect root-installed owner asset directory %s: %w", path, err)
		}
		identity, err := FileIdentity(info)
		if err != nil || !info.IsDir() || identity.UID != 0 || identity.GID != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("root-installed owner asset directory %s is not root-owned and non-writable by A/B", path)
		}
		if err := VerifyNoUntrustedACLWrite(path, []uint32{0}, nil, true); err != nil {
			return fmt.Errorf("root-installed owner asset directory %s has an untrusted ACL writer: %w", path, err)
		}
		if err := VerifyEffectiveAccess(path, a, 0, 2); err != nil {
			return fmt.Errorf("A can modify trusted owner asset directory %s: %w", path, err)
		}
	}
	for _, path := range []string{"/usr", "/usr/libexec", "/usr/libexec/symmetry", "/etc", "/etc/symmetry"} {
		if err := VerifyEffectiveAccess(path, b, 0, 2); err != nil {
			return fmt.Errorf("verify fixed B cannot modify trusted directory %s: %w", path, err)
		}
	}
	if err := verifyTrustedLinuxConfig(a, b); err != nil {
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

func verifyTrustedLinuxConfig(a, b ACLSubject) error {
	const path = "/etc/symmetry/linux-owner.conf"
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("inspect fixed Linux owner config metadata: %w", err)
	}
	identity, err := FileIdentity(info)
	if err != nil || !info.Mode().IsRegular() || identity.UID != 0 || identity.GID != 0 || identity.Links != 1 ||
		info.Size() <= 0 || info.Size() > linuxgate.OwnerConfigMaxBytes || info.Mode().Perm() != 0o644 {
		return errors.New("fixed Linux owner config must be root:root mode 0644, singly linked, and within its strict size bound")
	}
	if err := VerifyNoUntrustedACLWrite(path, []uint32{0}, nil, false); err != nil {
		return fmt.Errorf("fixed owner configuration has an untrusted ACL writer: %w", err)
	}
	for _, subject := range []ACLSubject{a, b} {
		if err := VerifyEffectiveAccess(path, subject, 4, 2); err != nil {
			return fmt.Errorf("fixed owner configuration access is unsafe for UID %d: %w", subject.UID, err)
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

type StatIdentity struct {
	UID   uint32
	GID   uint32
	Mode  uint32
	Links uint64
}

func FileIdentity(info os.FileInfo) (StatIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return StatIdentity{}, errors.New("filesystem did not expose Linux stat identity")
	}
	return StatIdentity{UID: stat.Uid, GID: stat.Gid, Mode: stat.Mode, Links: uint64(stat.Nlink)}, nil
}

func verifyTrustedLinuxAsset(path string, expected trustedLinuxAsset, a, b ACLSubject) error {
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return err
	}
	identity, err := FileIdentity(info)
	if err != nil {
		return err
	}
	if err := validateTrustedLinuxAssetIdentity(identity, info.Mode().IsRegular(), expected); err != nil {
		return err
	}
	if err := VerifyEffectiveAccess(path, a, 1, 2); err != nil {
		return fmt.Errorf("configured A cannot execute the installed owner image or can modify it: %w", err)
	}
	if err := VerifyNoUntrustedACLWrite(path, []uint32{0}, nil, false); err != nil {
		return fmt.Errorf("root-installed owner executable %s has an untrusted ACL writer: %w", path, err)
	}
	forbidden := uint16(2)
	if expected.denyBAll {
		forbidden = 7
	}
	if err := VerifyEffectiveAccess(path, b, 0, forbidden); err != nil {
		return fmt.Errorf("fixed B can access the privileged wrapper or modify an installed executable: %w", err)
	}
	return nil
}

func validateTrustedLinuxAssetIdentity(identity StatIdentity, regular bool, expected trustedLinuxAsset) error {
	if !regular || identity.UID != expected.uid || identity.GID != expected.gid || identity.Mode&07777 != expected.mode || identity.Links != expected.links {
		return errors.New("installed executable ownership, mode, or link count differs from the fixed policy")
	}
	if expected.mode&0111 == 0 || identity.Mode&0022 != 0 {
		return errors.New("installed executable is not executable or is group/world writable")
	}
	return nil
}

func verifyLinuxOwnerWorkspace(value config.Config, owner linuxgate.OwnerConfig, accounts OwnerAccounts) error {
	workspace, ok := value.Workspaces[value.Runtime.Workspace]
	if !ok || workspace.Policy != config.WorkspacePolicyGitCloneV2 || workspace.Path != "" || workspace.Ref != "" || workspace.SeedRoot == "" {
		return errors.New("Linux v2 owner preflight requires the exact git_clone_v2 Repository/SeedRoot/Root workspace contract")
	}
	a, b := accounts.ASubject, accounts.BSubject
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

func verifyLinuxOwnerPiShare(value config.Config, owner linuxgate.OwnerConfig, accounts OwnerAccounts) error {
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
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("inspect Linux Pi provider extension share: %w", err)
	}
	identity, err := FileIdentity(info)
	if err != nil || ValidatePiExtensionShareIdentity(info.IsDir(), identity, owner) != nil {
		return errors.New("Linux Pi extension share must be A-owned, B-primary-group, and mode 02750")
	}
	a := accounts.ASubject
	b := accounts.BSubject
	if err := VerifyEffectiveAccess(path, a, 7, 0); err != nil {
		return fmt.Errorf("A cannot publish Pi provider extensions to the configured share: %w", err)
	}
	if err := VerifyEffectiveAccess(path, b, 5, 2); err != nil {
		return fmt.Errorf("Pi extension share does not grant B read/search without write: %w", err)
	}
	if err := VerifyNoUntrustedACLWrite(path, []uint32{0, owner.AUID}, []uint32{owner.AGID}, true); err != nil {
		return fmt.Errorf("Pi extension share has an untrusted ACL writer: %w", err)
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		parentInfo, err := LstatCanonicalPath(parent)
		if err != nil {
			return fmt.Errorf("inspect Pi share ancestor %s: %w", parent, err)
		}
		parentIdentity, err := FileIdentity(parentInfo)
		if err != nil || !parentInfo.IsDir() || parentIdentity.UID != 0 && parentIdentity.UID != owner.AUID || parentInfo.Mode().Perm()&0022 != 0 && parentInfo.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("Pi share ancestor %s is writable by an untrusted identity", parent)
		}
		if err := VerifyEffectiveAccess(parent, b, 1, 2); err != nil {
			return fmt.Errorf("Pi share ancestor %s is writable or unsearchable by B: %w", parent, err)
		}
		if err := VerifyNoUntrustedACLWrite(parent, []uint32{0, owner.AUID}, []uint32{owner.AGID}, true); err != nil {
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
		info, err := LstatCanonicalPath(filepath.Join(path, entry.Name()))
		if err != nil {
			return err
		}
		fileIdentity, err := FileIdentity(info)
		if err != nil || !info.Mode().IsRegular() || fileIdentity.UID != owner.AUID || fileIdentity.GID != owner.BGID || info.Mode().Perm() != 0o440 || fileIdentity.Links != 1 {
			return fmt.Errorf("Pi shared extension %s is not A-owned, B-readable, and immutable to B", entry.Name())
		}
		if err := VerifyEffectiveAccess(filepath.Join(path, entry.Name()), b, 4, 2); err != nil {
			return fmt.Errorf("Pi extension file %s is not read-only to B: %w", entry.Name(), err)
		}
		if err := VerifyNoUntrustedACLWrite(filepath.Join(path, entry.Name()), []uint32{0, owner.AUID}, nil, false); err != nil {
			return fmt.Errorf("Pi extension file %s has an untrusted ACL writer: %w", entry.Name(), err)
		}
	}
	return nil
}

func ValidatePiExtensionShareIdentity(isDirectory bool, identity StatIdentity, owner linuxgate.OwnerConfig) error {
	if !isDirectory || owner.AUID == 0 || owner.BGID == 0 || identity.UID != owner.AUID ||
		identity.GID != owner.BGID || identity.Mode&07777 != 0o2750 {
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

func verifyLinuxParentsNotWritable(path string, subject ACLSubject) error {
	return VerifyTrustedPathAncestors(path, uint32(os.Geteuid()), uint32(os.Getegid()), &subject)
}

func VerifyTrustedPathAncestors(path string, ownerUID, ownerGID uint32, b *ACLSubject) error {
	if path == "/" {
		return nil
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := LstatCanonicalPath(parent)
		if err != nil {
			return err
		}
		identity, err := FileIdentity(info)
		if err != nil || validateLinuxTrustedPathAncestor(identity, info.IsDir(), ownerUID) != nil {
			return fmt.Errorf("directory %s is not controlled by root or A", parent)
		}
		if err := VerifyNoUntrustedACLWrite(parent, []uint32{0, ownerUID}, []uint32{ownerGID}, true); err != nil {
			return fmt.Errorf("directory %s has an untrusted owner, group, or ACL writer: %w", parent, err)
		}
		if b != nil {
			if err := VerifyEffectiveAccess(parent, *b, 1, 2); err != nil {
				return fmt.Errorf("directory %s is B-writable or unsearchable: %w", parent, err)
			}
		}
		if parent == "/" {
			return nil
		}
	}
}

func validateLinuxTrustedPathAncestor(identity StatIdentity, directory bool, ownerUID uint32) error {
	if !directory || identity.UID != 0 && identity.UID != ownerUID {
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
	for i := 0; i < len(value); i++ {
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
		i += 3 // The escape is four bytes: the backslash plus three octal digits.
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

func verifyLinuxServicePath(value string, a, b ACLSubject) error {
	if value == "" {
		return errors.New("systemd PATH must not be empty")
	}
	for _, path := range strings.Split(value, string(os.PathListSeparator)) {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("systemd PATH contains an empty or non-canonical entry")
		}
		if err := verifyLinuxServicePathAncestors(path, b, a.UID); err != nil {
			return fmt.Errorf("systemd PATH entry %s has an untrusted parent: %w", path, err)
		}
		linkInfo, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect systemd PATH entry %s: %w", path, err)
		}
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			linkIdentity, identityErr := FileIdentity(linkInfo)
			if identityErr != nil || linkIdentity.UID != 0 && linkIdentity.UID != a.UID {
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
		info, err := LstatCanonicalPath(resolved)
		if err != nil {
			return fmt.Errorf("inspect resolved systemd PATH entry %s: %w", resolved, err)
		}
		identity, err := FileIdentity(info)
		if err != nil || !info.IsDir() || identity.UID != 0 && identity.UID != a.UID || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("systemd PATH entry %s must resolve to a root/A-owned, non-writable directory", path)
		}
		if err := verifyLinuxAControlledGitParents(resolved, b, a.UID, a.GID); err != nil {
			return fmt.Errorf("systemd PATH entry %s can be replaced by B: %w", path, err)
		}
		if err := VerifyEffectiveAccess(resolved, a, 5, 2); err != nil {
			return fmt.Errorf("A cannot search/execute in systemd PATH entry %s: %w", path, err)
		}
	}
	return nil
}

func verifyLinuxServicePathAncestors(path string, b ACLSubject, ownerUID uint32) error {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := LstatCanonicalPath(parent)
		if err != nil {
			return err
		}
		identity, err := FileIdentity(info)
		if err != nil || !info.IsDir() || identity.UID != 0 && identity.UID != ownerUID || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("PATH ancestor %s is not root/A-owned and protected", parent)
		}
		if err := VerifyEffectiveAccess(parent, b, 1, 2); err != nil {
			return fmt.Errorf("PATH ancestor %s is B-writable or unsearchable: %w", parent, err)
		}
		if parent == "/" {
			return nil
		}
	}
}

func verifyExistingLinuxStateLayout(directory string, ownerUID uint32, b *ACLSubject) error {
	if directory == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return errors.New("state_dir must be an existing canonical absolute path for read-only owner preflight")
	}
	if err := VerifyTrustedPathAncestors(directory, ownerUID, uint32(os.Getegid()), b); err != nil {
		return fmt.Errorf("StateDir ancestor is not immutable against untrusted identities: %w", err)
	}
	for _, path := range []string{directory, filepath.Join(directory, linuxOwnerStateDirName)} {
		info, err := LstatCanonicalPath(path)
		if err != nil {
			return fmt.Errorf("inspect existing private state directory %s: %w", path, err)
		}
		identity, err := FileIdentity(info)
		if err != nil || !info.IsDir() || identity.UID != ownerUID || info.Mode().Perm() != 0o700 {
			return fmt.Errorf("existing state directory %s must be A-owned mode 0700", path)
		}
		if err := VerifyNoUntrustedACLWrite(path, []uint32{0, ownerUID}, []uint32{uint32(os.Getegid())}, true); err != nil {
			return fmt.Errorf("existing state directory %s is changeable by an untrusted identity: %w", path, err)
		}
		if b != nil {
			if err := VerifyDefaultAccess(path, *b, 0, 7); err != nil {
				return fmt.Errorf("future StateDir entries are accessible to fixed B: %w", err)
			}
		}
		if b != nil {
			if err := VerifyEffectiveAccess(path, *b, 0, 7); err != nil {
				return fmt.Errorf("fixed B can traverse or read private state directory %s: %w", path, err)
			}
		}
	}
	lockPath := filepath.Join(directory, linuxOwnerLockFile)
	lockInfo, err := LstatCanonicalPath(lockPath)
	if err != nil {
		return fmt.Errorf("inspect existing state Store lock: %w", err)
	}
	lockIdentity, err := FileIdentity(lockInfo)
	if err != nil || !lockInfo.Mode().IsRegular() || lockIdentity.UID != ownerUID || lockInfo.Mode().Perm() != 0o600 || lockIdentity.Links != 1 {
		return errors.New("existing Store lock must be a singly-linked A-owned mode-0600 regular file")
	}
	if err := VerifyNoUntrustedACLWrite(lockPath, []uint32{0, ownerUID}, []uint32{uint32(os.Getegid())}, false); err != nil {
		return fmt.Errorf("existing Store lock is changeable by an untrusted identity: %w", err)
	}
	if b != nil {
		if err := VerifyEffectiveAccess(lockPath, *b, 0, 7); err != nil {
			return fmt.Errorf("fixed B can access the private Store lock: %w", err)
		}
	}
	return nil
}

func VerifyOwnerStateDirBinding(configuredPath string, owner linuxgate.OwnerConfig) error {
	installedPath := owner.StateDir
	configuredPath, err := NormalizeOwnerStateDir(configuredPath)
	if err != nil || installedPath == "" || !filepath.IsAbs(installedPath) ||
		filepath.Clean(installedPath) != installedPath || configuredPath != installedPath {
		return errors.New("daemon StateDir must exactly match the root-installed canonical OwnerConfig.StateDir")
	}
	if _, err := LstatCanonicalPath(configuredPath); err != nil {
		return fmt.Errorf("inspect configured StateDir without following links: %w", err)
	}
	if _, err := LstatCanonicalPath(installedPath); err != nil {
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

func NormalizeOwnerStateDir(path string) (string, error) {
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

func LstatCanonicalPath(path string) (os.FileInfo, error) {
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
