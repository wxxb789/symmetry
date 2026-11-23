//go:build linux && amd64

package linuxpreflight

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

func configuredHarnessKind(value string) (harness.Kind, error) {
	switch value {
	case config.RuntimeHarnessGeneric:
		return harness.KindGeneric, nil
	case config.RuntimeHarnessCodex:
		return harness.KindCodex, nil
	case config.RuntimeHarnessClaudeCode:
		return harness.KindClaude, nil
	case config.RuntimeHarnessPi:
		return harness.KindPi, nil
	case config.RuntimeHarnessOMP:
		return harness.KindOMP, nil
	case config.RuntimeHarnessOmO:
		return harness.KindOmO, nil
	case config.RuntimeHarnessOpenCode:
		return harness.KindOpenCode, nil
	default:
		return "", fmt.Errorf("runtime.harness_kind %q is unsupported", value)
	}
}

// Adapter families whose launch contract is staged: process identity, then native
// session handle, then a native turn. Verbatim copy of the app-side owner.
func nativeGoalHarnessKind(kind harness.Kind) bool {
	switch kind {
	case harness.KindCodex, harness.KindClaude, harness.KindPi, harness.KindOMP, harness.KindOmO, harness.KindOpenCode:
		return true
	default:
		return false
	}
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

func verifyLinuxOwnerParentKillBoundary(mountPoint, delegatedPath string, ownerUID uint32, a, b ACLSubject) error {
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
	if parentStat.Uid != ownerUID || parentStat.Mode&unix.S_IFMT != unix.S_IFDIR || parentStat.Mode&0022 != 0 || a.UID != ownerUID || b.UID == ownerUID {
		return errors.New("delegated manager parent must be A-owned with no group/other write access")
	}
	metadataHow := &unix.OpenHow{Flags: uint64(unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV)}
	for _, control := range []struct {
		name           string
		ownerUID       uint32
		mustBeWritable bool
		allowAOwner    bool
	}{
		{name: "cgroup.procs", ownerUID: ownerUID, mustBeWritable: true},
		{name: "cgroup.subtree_control", ownerUID: ownerUID, mustBeWritable: true},
		{name: "cgroup.events", ownerUID: 0, allowAOwner: true},
		{name: "cgroup.controllers", ownerUID: 0, allowAOwner: true},
	} {
		controlStat, controlErr := statLinuxOwnerCgroupControl(parentFD, control.name, metadataHow)
		if controlErr != nil {
			return controlErr
		}
		ownerMatches := controlStat.Uid == control.ownerUID || control.allowAOwner && controlStat.Uid == ownerUID
		if controlStat.Mode&unix.S_IFMT != unix.S_IFREG || !ownerMatches || controlStat.Mode&0022 != 0 || control.mustBeWritable && controlStat.Mode&0200 == 0 {
			return fmt.Errorf("manager control %s is not owned/writable only by its trusted owner", control.name)
		}
	}
	// The manager kill control sits inside the delegated subtree systemd hands to A from
	// v254 on; the boundary that stays root-only is the owning unit cgroup's own control.
	managerKill, err := statLinuxOwnerCgroupControl(parentFD, "cgroup.kill", metadataHow)
	if err != nil {
		return err
	}
	if err := validateLinuxOwnerManagerKillMetadata(managerKill.Uid, managerKill.Gid, managerKill.Mode, ownerUID, a, b); err != nil {
		return err
	}
	unitKillPath := linuxOwnerUnitCgroupKillPath(delegatedPath)
	unitKill, err := statLinuxOwnerCgroupControl(mountFD, unitKillPath, metadataHow)
	if err != nil {
		return err
	}
	if err := validateLinuxOwnerUnitKillMetadata(unitKill.Uid, unitKill.Gid, unitKill.Mode, a, b); err != nil {
		return err
	}
	writeHow := &unix.OpenHow{Flags: uint64(unix.O_WRONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK), Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV)}
	writeFD, writeErr := unix.Openat2(mountFD, unitKillPath, writeHow)
	if writeErr == nil {
		_ = unix.Close(writeFD)
		return errors.New("A can open the owning unit cgroup.kill for writing; the unit kill control must remain root-only")
	}
	if !errors.Is(writeErr, unix.EACCES) && !errors.Is(writeErr, unix.EPERM) {
		return fmt.Errorf("cannot establish root-only unit cgroup.kill permissions: %w", writeErr)
	}
	return nil
}

func statLinuxOwnerCgroupControl(directoryFD int, relativePath string, how *unix.OpenHow) (unix.Stat_t, error) {
	fd, err := unix.Openat2(directoryFD, relativePath, how)
	if err != nil {
		return unix.Stat_t{}, fmt.Errorf("open cgroup control %s metadata read-only: %w", relativePath, err)
	}
	var stat unix.Stat_t
	statErr := unix.Fstat(fd, &stat)
	closeErr := unix.Close(fd)
	if statErr != nil || closeErr != nil {
		return unix.Stat_t{}, fmt.Errorf("read cgroup control %s metadata: %w", relativePath, errors.Join(statErr, closeErr))
	}
	return stat, nil
}

func linuxOwnerUnitCgroupKillPath(delegatedPath string) string {
	unit := filepath.Dir(delegatedPath)
	if unit == "." {
		return "cgroup.kill"
	}
	return filepath.ToSlash(unit) + "/cgroup.kill"
}

func linuxOwnerKillSubjectsDistinctAndNonRoot(a, b ACLSubject) bool {
	return a.UID != 0 && a.UID != b.UID && b.UID != 0
}

func validateLinuxOwnerManagerKillMetadata(uid, gid, mode, ownerUID uint32, a, b ACLSubject) error {
	if ownerUID == 0 || a.UID != ownerUID || b.UID == ownerUID || !linuxOwnerKillSubjectsDistinctAndNonRoot(a, b) {
		return errors.New("delegated manager cgroup.kill requires distinct non-root A and B accounts")
	}
	ownerGID := uint32(0)
	if uid == ownerUID {
		ownerGID = a.GID
	} else if uid != 0 {
		return errors.New("delegated manager cgroup.kill must be owned by root or A, never by B")
	}
	if gid != ownerGID {
		return errors.New("delegated manager cgroup.kill group must match its root or A owner")
	}
	return validateLinuxOwnerKillControlMode(mode)
}

func validateLinuxOwnerUnitKillMetadata(uid, gid, mode uint32, a, b ACLSubject) error {
	if uid != 0 || gid != 0 {
		return errors.New("unit cgroup.kill must remain root:root")
	}
	if !linuxOwnerKillSubjectsDistinctAndNonRoot(a, b) {
		return errors.New("unit cgroup.kill requires distinct non-root A and B accounts")
	}
	return validateLinuxOwnerKillControlMode(mode)
}

func validateLinuxOwnerKillControlMode(mode uint32) error {
	if mode&unix.S_IFMT != unix.S_IFREG || mode&0200 == 0 || mode&0022 != 0 {
		return errors.New("cgroup.kill must be a plain file writable only by its owner")
	}
	return nil
}

func verifyLinuxOwnerAnchorProcessContext(expected authority.LinuxCgroupProcessIdentity, accounts OwnerAccounts) error {
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
			if err := verifyLinuxServicePath(value, accounts.ASubject, accounts.BSubject); err != nil {
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

func canonicalLinuxCgroupPath(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value && !strings.ContainsRune(value, '\\') && !strings.ContainsRune(value, 0)
}
