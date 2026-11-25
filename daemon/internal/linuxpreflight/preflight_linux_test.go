//go:build linux && amd64

package linuxpreflight

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

func TestLinuxOwnerStateDirBindingRequiresCanonicalInstalledIdentity(t *testing.T) {
	root := t.TempDir()
	configured := filepath.Join(root, "state")
	if err := os.Mkdir(configured, 0o700); err != nil {
		t.Fatal(err)
	}
	owner := linuxgate.OwnerConfig{
		AUID: uint32(os.Geteuid()), AGID: uint32(os.Getegid()),
		BUID: uint32(os.Geteuid() + 1), BGID: uint32(os.Getegid() + 1), StateDir: configured,
	}
	if err := VerifyOwnerStateDirBinding(configured, owner); err != nil {
		t.Fatalf("matching independently pinned StateDir rejected: %v", err)
	}

	other := filepath.Join(root, "other-state")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	owner.StateDir = other
	if err := VerifyOwnerStateDirBinding(configured, owner); err == nil {
		t.Fatal("different root-installed StateDir was accepted")
	}

	link := filepath.Join(root, "state-link")
	if err := os.Symlink(configured, link); err != nil {
		t.Fatal(err)
	}
	owner.StateDir = link
	if err := VerifyOwnerStateDirBinding(link, owner); err == nil {
		t.Fatal("symlinked root-installed StateDir was accepted")
	}

	owner.StateDir = configured
	if err := os.Chmod(configured, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := VerifyOwnerStateDirBinding(configured, owner); err == nil {
		t.Fatal("non-private state directory identity was accepted")
	}
}

func TestCanonicalLinuxOwnerStateDirUsesRootWorkingDirectory(t *testing.T) {
	for input, want := range map[string]string{
		".symmetry/state":           "/.symmetry/state",
		"var/lib/symmetry/state":    "/var/lib/symmetry/state",
		"../var/lib/symmetry/state": "/var/lib/symmetry/state",
	} {
		got, err := NormalizeOwnerStateDir(input)
		if err != nil || got != want {
			t.Fatalf("NormalizeOwnerStateDir(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

func TestLegacyOwnerDrainRejectsUnprovedV1MarkersAndUnknownRuns(t *testing.T) {
	marker := terminalLinuxDrainJournal()
	marker.PID = 42
	marker.ProcessIdentity = "linux:42:unproved"
	marker.StartedAt = time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC)
	if err := verifyDrainedLinuxJournals([]state.RunJournal{marker}); err == nil || !strings.Contains(err.Error(), "unresolved v1 process marker") {
		t.Fatalf("unproved process marker error = %v, want blocked drain", err)
	}

	handoff := terminalLinuxDrainJournal()
	handoff.ContainmentHandoff = &authority.SupervisorHandoff{Version: authority.SupervisorHandoffVersion}
	if err := verifyDrainedLinuxJournals([]state.RunJournal{handoff}); err == nil || !strings.Contains(err.Error(), "without an exact terminal stop receipt") {
		t.Fatalf("unproved v1 handoff error = %v, want exact receipt requirement", err)
	}

	unknown := state.RunJournal{RunID: "unknown", Generation: 1, LocalState: "running"}
	if err := verifyDrainedLinuxJournals([]state.RunJournal{unknown}); err == nil || !strings.Contains(err.Error(), "not terminal") {
		t.Fatalf("unknown live journal error = %v, want blocked drain", err)
	}

	v2 := terminalLinuxDrainJournal()
	v2.ContainmentCgroup = &state.LinuxCgroupReservation{}
	if err := verifyDrainedLinuxJournals([]state.RunJournal{v2}); err == nil || !strings.Contains(err.Error(), "unexpected v2") {
		t.Fatalf("v2 journal error = %v, want explicit v2 rejection", err)
	}
}

func TestLegacyOwnerDrainRequiresExactV1ReceiptAndReleasedAuthority(t *testing.T) {
	supervisor := testLinuxV1DrainSupervisor()
	receipt := testLinuxV1DrainReceipt(supervisor)
	if !receipt.ValidFor(supervisor) {
		t.Fatal("test receipt does not exactly match its v1 authority")
	}

	journal := terminalLinuxDrainJournal()
	journal.PID = supervisor.TargetPID
	journal.ProcessIdentity = supervisor.TargetIdentity
	journal.StartedAt = time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC)
	withoutReceipt := supervisor
	withoutReceipt.StopReceipt = nil
	journal.ContainmentAuthority = &withoutReceipt
	if err := verifyDrainedLinuxJournals([]state.RunJournal{journal}); err == nil || !strings.Contains(err.Error(), "without an exact terminal stop receipt") {
		t.Fatalf("missing v1 receipt error = %v, want receipt-required block", err)
	}

	withReceipt := supervisor
	withReceipt.StopReceipt = &receipt
	journal.ContainmentAuthority = &withReceipt
	if err := verifyDrainedLinuxJournals([]state.RunJournal{journal}); err == nil || !strings.Contains(err.Error(), "release and clear") {
		t.Fatalf("receipt-bearing authority error = %v, want old-topology release barrier", err)
	}

	changed := receipt
	changed.TargetIdentity += ":different"
	withReceipt.StopReceipt = &changed
	journal.ContainmentAuthority = &withReceipt
	if err := verifyDrainedLinuxJournals([]state.RunJournal{journal}); err == nil || !strings.Contains(err.Error(), "without an exact terminal stop receipt") {
		t.Fatalf("mismatched v1 receipt error = %v, want exact-subject rejection", err)
	}
}

func TestLegacyOwnerDrainLeavesGoalDeliveryForNormalRecovery(t *testing.T) {
	journal := terminalLinuxDrainJournal()
	journal.PendingGoalDeliveries = []state.GoalDelivery{{Kind: state.GoalDeliveryEvidence, DeliveryID: "evidence-1"}}
	if err := verifyDrainedLinuxJournals([]state.RunJournal{journal}); err != nil {
		t.Fatalf("terminal journal with a pending Goal delivery blocked process drain: %v", err)
	}
}

func TestLegacyOwnerDrainUsesExclusiveStoreAndDoesNotWriteJournals(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the daemon owner preflight intentionally rejects root")
	}
	directory := t.TempDir()
	store, key := claimedStoreAt(t, directory)
	if _, err := store.SetProcessDetails(key, 42, "linux:42:unproved", time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "terminal-1", State: "failed", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	journalPath := filepath.Join(directory, linuxOwnerStateDirName, linuxJournalFilename(key))
	before, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	restore := locked.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return nil
	})
	err = verifyDrainedLinuxStore(locked, directory)
	restore()
	if closeErr := locked.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err == nil {
		t.Fatal("legacy drain verification accepted an unproved process marker")
	}
	if writes != 0 {
		t.Fatalf("read-only drain called the journal writer %d times", writes)
	}
	after, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("legacy drain verification changed the journal")
	}
}

func TestLegacyOwnerDrainRejectsUnknownRunFilesWithoutCleaningThem(t *testing.T) {
	directory := t.TempDir()
	store, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	unknownPath := filepath.Join(directory, linuxOwnerStateDirName, "legacy-run.json")
	contents := []byte(`{"run_id":"legacy"}`)
	if err := os.WriteFile(unknownPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		writes++
		return nil
	})
	err = verifyDrainedLinuxStore(store, directory)
	restore()
	if err == nil || !strings.Contains(err.Error(), "unexpected or temporary run-state entry") {
		t.Fatalf("unknown run entry error = %v, want explicit fail-closed rejection", err)
	}
	if writes != 0 {
		t.Fatalf("unknown-run scan called the journal writer %d times", writes)
	}
	after, err := os.ReadFile(unknownPath)
	if err != nil || !bytes.Equal(after, contents) {
		t.Fatalf("unknown run file changed or was removed: contents=%q err=%v", after, err)
	}
}

func TestLegacyOwnerDrainFailsWhenStoreLockIsHeld(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the daemon owner preflight intentionally rejects root")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(home, ".symmetry-owner-drain-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	store, _ := claimedStoreAt(t, directory)
	defer store.Close()
	if err := VerifyLegacyLinuxOwnersDrained(configForLinuxPreflight(directory)); !errors.Is(err, state.ErrStoreInUse) {
		t.Fatalf("VerifyLegacyLinuxOwnersDrained() = %v, want shared Store lock conflict", err)
	}
}

func TestLinuxOwnerHostPrerequisiteChecksFailClosed(t *testing.T) {
	if err := validateLinuxOwnerAccountMapping(1001, 1002, 1003, 1004, 1001, 1002, 1005, 1004); err == nil {
		t.Fatal("passwd B UID mismatch with installed fixed config was accepted")
	}
	if err := validateLinuxOwnerProcessIDs(1001, 1002, 1003, 1004, 1000, 1001, 1002, 1002); err == nil {
		t.Fatal("owner identity mismatch was accepted")
	}

	if err := validateDiscoveredLinuxOwnerAnchor(platform.LinuxOwnerAnchorIdentity{}, 1001); err == nil {
		t.Fatal("missing owner anchor was accepted")
	}
	parent := authority.LinuxCgroupParentIdentity{
		BootID: "01234567-89ab-cdef-0123-456789abcdef", MountID: 1, Device: 2, Inode: 3, Path: "/system.slice/symmetry-owner.service/manager",
	}
	anchor := platform.LinuxOwnerAnchorIdentity{
		Process: authority.LinuxCgroupProcessIdentity{PID: 10, Identity: "linux:10:started"},
		Parent:  parent, OwnerUID: 1001, ManagerPath: parent.Path,
	}
	if err := validateDiscoveredLinuxOwnerAnchor(anchor, 1001); err != nil {
		t.Fatalf("valid anchor fixture: %v", err)
	}
	changedParent := parent
	changedParent.Inode++
	if err := validateLinuxOwnerParentIdentity(changedParent, 1001, 1001, anchor); err == nil {
		t.Fatal("delegated-parent identity mismatch was accepted")
	}
	wrapper := trustedLinuxAsset{uid: 0, gid: 1002, mode: 0o4750, links: 1, denyBAll: true}
	if err := validateTrustedLinuxAssetIdentity(StatIdentity{UID: 0, GID: 1002, Mode: 0o4750, Links: 1}, true, wrapper); err != nil {
		t.Fatalf("valid root wrapper metadata: %v", err)
	}
	if err := validateTrustedLinuxAssetIdentity(StatIdentity{UID: 0, GID: 1002, Mode: 0o4755, Links: 1}, true, wrapper); err == nil {
		t.Fatal("world-executable privileged wrapper was accepted")
	}
}

func TestLinuxOwnerKillBoundaryMetadata(t *testing.T) {
	a := ACLSubject{UID: 1001, GID: 1002}
	b := ACLSubject{UID: 1003, GID: 1004}
	for _, test := range []struct {
		name  string
		uid   uint32
		gid   uint32
		mode  uint32
		valid bool
	}{
		{name: "root-owned", mode: unix.S_IFREG | 0o200, valid: true},
		{name: "A-owned", uid: 1001, gid: 1002, mode: unix.S_IFREG | 0o200, valid: true},
		{name: "B-owned", uid: 1003, gid: 1004, mode: unix.S_IFREG | 0o200},
		{name: "A-owned with the root group", uid: 1001, mode: unix.S_IFREG | 0o200},
		{name: "root-owned with A's group", gid: 1002, mode: unix.S_IFREG | 0o200},
		{name: "group-writable", mode: unix.S_IFREG | 0o220},
		{name: "other-writable", mode: unix.S_IFREG | 0o202},
		{name: "owner-unwritable", mode: unix.S_IFREG | 0o400},
		{name: "not-a-plain-file", mode: unix.S_IFDIR | 0o700},
	} {
		if err := validateLinuxOwnerManagerKillMetadata(test.uid, test.gid, test.mode, 1001, a, b); (err == nil) != test.valid {
			t.Errorf("manager cgroup.kill %s: err = %v, accepted = %t, want accepted = %t", test.name, err, err == nil, test.valid)
		}
	}
	for _, test := range []struct {
		name  string
		uid   uint32
		gid   uint32
		mode  uint32
		valid bool
	}{
		{name: "root-owned", mode: unix.S_IFREG | 0o200, valid: true},
		{name: "A-owned", uid: 1001, gid: 1002, mode: unix.S_IFREG | 0o200},
		{name: "B-owned", uid: 1003, gid: 1004, mode: unix.S_IFREG | 0o200},
		{name: "root-owned with a foreign group", gid: 1002, mode: unix.S_IFREG | 0o200},
		{name: "group-writable", mode: unix.S_IFREG | 0o220},
		{name: "other-writable", mode: unix.S_IFREG | 0o202},
		{name: "owner-unwritable", mode: unix.S_IFREG | 0o400},
		{name: "not-a-plain-file", mode: unix.S_IFDIR | 0o700},
	} {
		if err := validateLinuxOwnerUnitKillMetadata(test.uid, test.gid, test.mode, a, b); (err == nil) != test.valid {
			t.Errorf("unit cgroup.kill %s: err = %v, accepted = %t, want accepted = %t", test.name, err, err == nil, test.valid)
		}
	}
}

func TestLinuxOwnerUnitCgroupKillPathFollowsTheDelegatedParent(t *testing.T) {
	for delegated, want := range map[string]string{
		"manager": "cgroup.kill",
		"system.slice/symmetry-owner.service/manager": "system.slice/symmetry-owner.service/cgroup.kill",
		"a/b/c/manager": "a/b/c/cgroup.kill",
	} {
		if got := linuxOwnerUnitCgroupKillPath(delegated); got != want {
			t.Errorf("linuxOwnerUnitCgroupKillPath(%q) = %q, want %q", delegated, got, want)
		}
	}
}

func TestLinuxOwnerUnitKillWriteProbeDecisions(t *testing.T) {
	for _, test := range []struct {
		name     string
		writeErr error
		valid    bool
	}{
		{name: "refused with EACCES", writeErr: unix.EACCES, valid: true},
		{name: "refused with EPERM", writeErr: unix.EPERM, valid: true},
		{name: "wrapped EACCES", writeErr: fmt.Errorf("openat2: %w", unix.EACCES), valid: true},
		{name: "wrapped EPERM", writeErr: fmt.Errorf("openat2: %w", unix.EPERM), valid: true},
		{name: "opened for writing", writeErr: nil},
		{name: "missing file", writeErr: unix.ENOENT},
		{name: "wrapped missing file", writeErr: fmt.Errorf("openat2: %w", unix.ENOENT)},
		{name: "invalid flags", writeErr: unix.EINVAL},
		{name: "wrapped invalid flags", writeErr: fmt.Errorf("openat2: %w", unix.EINVAL)},
	} {
		if err := validateLinuxOwnerUnitKillWriteProbe(test.writeErr); (err == nil) != test.valid {
			t.Errorf("unit kill write probe %s: err = %v, accepted = %t, want accepted = %t", test.name, err, err == nil, test.valid)
		}
	}
}

func TestLinuxOwnerUnitKillControlReadsTheDerivedUnitPath(t *testing.T) {
	a := ACLSubject{UID: 1001, GID: 1002}
	b := ACLSubject{UID: 1003, GID: 1004}
	metadataHow := &unix.OpenHow{Flags: uint64(unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV)}
	root := t.TempDir()
	unit := filepath.Join(root, "unit")
	if err := os.MkdirAll(filepath.Join(unit, "manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)
	if err := verifyLinuxOwnerUnitKillControl(rootFD, "unit/manager", metadataHow, a, b); !errors.Is(err, unix.ENOENT) {
		t.Fatalf("absent unit cgroup.kill: err = %v, want an ENOENT refusal at the derived path", err)
	}
	kill := filepath.Join(unit, "cgroup.kill")
	if err := os.WriteFile(kill, nil, 0o200); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(kill, 0o200); err != nil {
		t.Fatal(err)
	}
	err = verifyLinuxOwnerUnitKillControl(rootFD, "unit/manager", metadataHow, a, b)
	if os.Geteuid() == 0 {
		if err == nil || !strings.Contains(err.Error(), "A can open the owning unit cgroup.kill for writing") {
			t.Fatalf("unit cgroup.kill as root: err = %v, want the write-probe refusal", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), "unit cgroup.kill must remain root:root") {
		t.Fatalf("unit cgroup.kill without root: err = %v, want the root:root metadata refusal", err)
	}
}

func TestLinuxOwnerHostPreflightBlocksFallbackAndMissingDelegation(t *testing.T) {
	value := config.Config{
		Runtime: config.Runtime{HarnessKind: config.RuntimeHarnessGeneric, Workspace: "native"},
		Workspaces: map[string]config.Workspace{
			"native": {Policy: config.WorkspacePolicyGitWorktree, Repository: "/repo", Root: "/worktrees", Ref: "HEAD"},
		},
	}
	if err := verifyLinuxNativeRuntimeSelection(value); err == nil || !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("generic runtime selection error = %v, want fallback rejection", err)
	}

	value.Runtime.HarnessKind = config.RuntimeHarnessCodex
	value.Runtime.HarnessVersion = "0.156.1"
	value.Runtime.AdapterVersion = "native-test"
	value.Runtime.AdapterProtocolVersion = 1
	value.Workspaces["native"] = config.Workspace{
		Policy: config.WorkspacePolicyGitCloneV2, Repository: "/repo", Root: "/worktrees", SeedRoot: "/seeds",
	}
	if err := verifyLinuxNativeRuntimeSelection(value); err == nil || !strings.Contains(err.Error(), "explicitly configured") {
		t.Fatalf("missing delegated parent configuration error = %v, want unavailable prerequisite", err)
	}
	value.Workspaces["native"] = config.Workspace{Policy: config.WorkspacePolicyGitWorktree, Repository: "/repo", Root: "/worktrees", Ref: "HEAD"}
	if err := verifyLinuxNativeRuntimeSelection(value); err == nil || !strings.Contains(err.Error(), "git_clone_v2") {
		t.Fatalf("legacy worktree selection error = %v, want clone-v2 requirement", err)
	}
}

func TestNativeRuntimeSelectionVocabularyMatchesTheConfiguredKinds(t *testing.T) {
	for configured, configuredKind := range map[string]harness.Kind{
		config.RuntimeHarnessGeneric:    harness.KindGeneric,
		config.RuntimeHarnessCodex:      harness.KindCodex,
		config.RuntimeHarnessClaudeCode: harness.KindClaude,
		config.RuntimeHarnessPi:         harness.KindPi,
		config.RuntimeHarnessOMP:        harness.KindOMP,
		config.RuntimeHarnessOmO:        harness.KindOmO,
		config.RuntimeHarnessOpenCode:   harness.KindOpenCode,
	} {
		kind, err := harness.ParseKind(configured)
		if err != nil || kind != configuredKind {
			t.Fatalf("harness.ParseKind(%q) = (%q, %v), want %q", configured, kind, err, configuredKind)
		}
		wantNative := configuredKind != harness.KindGeneric
		if got := kind.NativeGoal(); got != wantNative {
			t.Fatalf("harness.Kind(%q).NativeGoal() = %v, want %v", kind, got, wantNative)
		}
	}
	if _, err := harness.ParseKind("warpdrive"); err == nil || err.Error() != `runtime.harness_kind "warpdrive" is unsupported` {
		t.Fatalf("harness.ParseKind(unsupported) = %v, want the exact unsupported-kind refusal", err)
	}
}

func TestLinuxOwnerSystemdUnitsRequireAAndNoNewPrivilegesNo(t *testing.T) {
	contents := []byte(`[Unit]
Description=owner
After=local-fs.target
[Service]
User=symmetry
Group=symmetry
NoNewPrivileges=no
Delegate=yes
DelegateSubgroup=manager
`)
	directives, err := parseLinuxSystemdServiceDirectives(contents)
	if err != nil {
		t.Fatal(err)
	}
	policy := linuxSystemdUnitPolicy{unit: "symmetry-owner.service", user: "symmetry", group: "symmetry", home: "/home/symmetry", delegate: "yes", delegatedSubgroup: "manager"}
	a := ACLSubject{UID: 1001, GID: 1002}
	b := ACLSubject{UID: 1003, GID: 1004}
	if err := requireLinuxSystemdDirective(directives, "nonewprivileges", "no"); err != nil {
		t.Fatalf("NoNewPrivileges=no policy: %v", err)
	}
	if err := requireLinuxSystemdIdentityDirective(directives, "user", policy.user); err != nil {
		t.Fatalf("fixed A user policy: %v", err)
	}
	if err := requireLinuxSystemdIdentityDirective(directives, "group", policy.group); err != nil {
		t.Fatalf("fixed A group policy: %v", err)
	}
	if !systemdListHasItem(directives["after"], "local-fs.target") {
		t.Fatalf("Unit.After directive was not parsed: %#v", directives)
	}

	if err := validateLinuxSystemdOverrides(map[string][]string{"environment": {"LANG=C"}}, policy, a, b); err != nil {
		t.Fatalf("safe locale-only drop-in was rejected: %v", err)
	}
	if err := validateLinuxSystemdOverrides(map[string][]string{"nonewprivileges": {"yes"}}, policy, a, b); err == nil {
		t.Fatal("NoNewPrivileges=yes systemd override was accepted")
	}
	if err := requireLinuxSystemdDirective(map[string][]string{"nonewprivileges": {"no", "yes"}}, "nonewprivileges", "no"); err == nil {
		t.Fatal("conflicting NoNewPrivileges directives were accepted")
	}
	if err := validateLinuxSystemdEnvironment(map[string][]string{"environmentfile": {"/etc/symmetry/daemon.env"}}, "/home/a", a, b); err == nil {
		t.Fatal("EnvironmentFile credential source was accepted")
	}
	if err := validateLinuxSystemdEnvironment(map[string][]string{"environment": {"GIT_CONFIG_GLOBAL=/tmp/config"}}, "/home/a", a, b); err == nil {
		t.Fatal("systemd Git configuration override was accepted")
	}
	if err := validateLinuxSystemdEnvironment(map[string][]string{"environment": {"HOME=/home/b"}}, "/home/a", a, b); err == nil {
		t.Fatal("B HOME was assigned to the A daemon service")
	}
}

func TestLinuxSystemdCandidateEnforcesExactOwnerAndDaemonTopology(t *testing.T) {
	a := ACLSubject{UID: 1001, GID: 1002}
	b := ACLSubject{UID: 1003, GID: 1004}
	owner := linuxSystemdUnitPolicy{
		unit: "symmetry-owner.service", user: "symmetry", group: "symmetry", home: "/home/symmetry",
		owner: true, delegate: "yes", delegatedSubgroup: "manager",
		execStart: []string{"/usr/libexec/symmetry/symmetry-daemon", "-linux-owner-anchor", "-cgroup-mount", "/sys/fs/cgroup",
			"-delegated-path", "system.slice/symmetry-owner.service/manager", "-socket", "/run/symmetry-owner/owner.sock"},
		execStop: []string{"/usr/libexec/symmetry/symmetry-daemon", "-recover-containment-only", "-config", "/etc/symmetry/daemon.json"},
	}
	ownerDirectives := linuxSystemdCandidateFixture(owner)
	if err := validateLinuxSystemdCandidate(ownerDirectives, owner, a, b); err != nil {
		t.Fatalf("valid owner unit candidate: %v", err)
	}
	for name, mutate := range map[string]func(map[string][]string){
		"sigkill disabled": func(values map[string][]string) { values["sendsigkill"] = []string{"no"} },
		"reverse dependency": func(values map[string][]string) {
			values["after"] = []string{"local-fs.target", "symmetry-daemon.service"}
		},
		"declared before edge": func(values map[string][]string) {
			values["before"] = []string{"symmetry-daemon.service"}
		},
		"stop post added": func(values map[string][]string) { values["execstoppost"] = []string{"/bin/true"} },
		"list reset":      func(values map[string][]string) { values["after"] = []string{"local-fs.target", ""} },
	} {
		t.Run("owner/"+name, func(t *testing.T) {
			mutated := cloneSystemdDirectives(ownerDirectives)
			mutate(mutated)
			if err := validateLinuxSystemdCandidate(mutated, owner, a, b); err == nil {
				t.Fatal("unsafe owner candidate was accepted")
			}
		})
	}
	if err := validateLinuxSystemdOverrides(map[string][]string{"before": {"symmetry-daemon.service"}}, owner, a, b); err == nil {
		t.Fatal("owner drop-in declaring an ordering edge to the daemon was accepted")
	}

	daemon := linuxSystemdUnitPolicy{
		unit: "symmetry-daemon.service", user: "symmetry", group: "symmetry", home: "/home/symmetry",
		execStart:    []string{"/usr/libexec/symmetry/symmetry-daemon", "-config", "/etc/symmetry/daemon.json"},
		execStartPre: []string{"/usr/libexec/symmetry/symmetry-daemon", "-verify-linux-owner-install", "-config", "/etc/symmetry/daemon.json"},
	}
	daemonDirectives := linuxSystemdCandidateFixture(daemon)
	if err := validateLinuxSystemdCandidate(daemonDirectives, daemon, a, b); err != nil {
		t.Fatalf("valid daemon unit candidate: %v", err)
	}
	delete(daemonDirectives, "bindsto")
	if err := validateLinuxSystemdCandidate(daemonDirectives, daemon, a, b); err == nil {
		t.Fatal("daemon candidate without BindsTo owner was accepted")
	}
}

func TestLinuxSystemdCandidatesAcceptShippedTemplates(t *testing.T) {
	serviceDirectory := filepath.Join("..", "..", "..", "scripts", "systemd")
	ownerTemplate, err := os.ReadFile(filepath.Join(serviceDirectory, "symmetry-owner.service.in"))
	if err != nil {
		t.Fatal(err)
	}
	daemonTemplate, err := os.ReadFile(filepath.Join(serviceDirectory, "symmetry-daemon.service.in"))
	if err != nil {
		t.Fatal(err)
	}
	ownerText := strings.NewReplacer("@A_USER@", "symmetry", "@A_GROUP@", "symmetry").Replace(string(ownerTemplate))
	daemonText := strings.NewReplacer("@A_USER@", "symmetry", "@A_GROUP@", "symmetry").Replace(string(daemonTemplate))
	ownerDirectives, err := parseLinuxSystemdServiceDirectives([]byte(ownerText))
	if err != nil {
		t.Fatalf("parse shipped owner template: %v", err)
	}
	daemonDirectives, err := parseLinuxSystemdServiceDirectives([]byte(daemonText))
	if err != nil {
		t.Fatalf("parse shipped daemon template: %v", err)
	}
	owner := linuxSystemdUnitPolicy{
		unit: "symmetry-owner.service", user: "symmetry", group: "symmetry", home: "/home/symmetry",
		owner: true, delegate: "yes", delegatedSubgroup: "manager",
		execStart: []string{"/usr/libexec/symmetry/symmetry-daemon", "-linux-owner-anchor", "-cgroup-mount", "/sys/fs/cgroup",
			"-delegated-path", "system.slice/symmetry-owner.service/manager", "-socket", "/run/symmetry-owner/owner.sock"},
		execStop: []string{"/usr/libexec/symmetry/symmetry-daemon", "-recover-containment-only", "-config", "/etc/symmetry/daemon.json"},
	}
	daemon := linuxSystemdUnitPolicy{
		unit: "symmetry-daemon.service", user: "symmetry", group: "symmetry", home: "/home/symmetry",
		execStart:    []string{"/usr/libexec/symmetry/symmetry-daemon", "-config", "/etc/symmetry/daemon.json"},
		execStartPre: []string{"/usr/libexec/symmetry/symmetry-daemon", "-verify-linux-owner-install", "-config", "/etc/symmetry/daemon.json"},
	}
	a := ACLSubject{UID: 1001, GID: 1002}
	b := ACLSubject{UID: 1003, GID: 1004}
	if err := validateLinuxSystemdCandidate(ownerDirectives, owner, a, b); err != nil {
		t.Fatalf("shipped owner template violates the contract: %v", err)
	}
	if err := validateLinuxSystemdCandidate(daemonDirectives, daemon, a, b); err != nil {
		t.Fatalf("shipped daemon template violates the contract: %v", err)
	}
}

func linuxSystemdCandidateFixture(policy linuxSystemdUnitPolicy) map[string][]string {
	directives := map[string][]string{
		"nonewprivileges": {"no"}, "user": {policy.user}, "group": {policy.group}, "umask": {"0077"},
		"workingdirectory": {"/"}, "killmode": {"control-group"}, "sendsigkill": {"yes"}, "timeoutstartsec": {"30s"},
		"restart": {"on-failure"}, "restartsec": {"5s"}, "execstart": {strings.Join(policy.execStart, " ")},
		"timeoutstopsec": {"2min"}, "after": {"local-fs.target symmetry-owner.service"},
	}
	if policy.owner {
		directives["type"] = []string{"notify"}
		directives["notifyaccess"] = []string{"main"}
		directives["slice"] = []string{"system.slice"}
		directives["runtimedirectory"] = []string{"symmetry-owner"}
		directives["runtimedirectorymode"] = []string{"0700"}
		directives["delegate"] = []string{policy.delegate}
		directives["delegatesubgroup"] = []string{policy.delegatedSubgroup}
		directives["timeoutstopsec"] = []string{"12min"}
		directives["after"] = []string{"local-fs.target"}
		directives["requiresmountsfor"] = []string{"/sys/fs/cgroup"}
		directives["execstop"] = []string{strings.Join(policy.execStop, " ")}
		return directives
	}
	directives["execstartpre"] = []string{strings.Join(policy.execStartPre, " ")}
	directives["type"] = []string{"simple"}
	directives["bindsto"] = []string{"symmetry-owner.service"}
	return directives
}

func cloneSystemdDirectives(value map[string][]string) map[string][]string {
	copy := make(map[string][]string, len(value))
	for key, values := range value {
		copy[key] = append([]string(nil), values...)
	}
	return copy
}

func TestLinuxCloneWorkspaceACLContracts(t *testing.T) {
	if err := validateLinuxCloneSourcePermissions(7, 0); err != nil {
		t.Fatalf("valid A-only Git source permissions: %v", err)
	}
	if err := validateLinuxCloneSourcePermissions(7, 1); err == nil {
		t.Fatal("B execute access to the source repository was accepted")
	}

	if err := validateLinuxCloneSeedPermissions(7, 5, 7, 5); err != nil {
		t.Fatalf("valid A-owned, B-read-only seed ACLs: %v", err)
	}
	for _, permissions := range [][4]uint16{
		{7, 7, 7, 5},
		{7, 3, 7, 5},
		{7, 5, 3, 5},
		{7, 5, 7, 3},
	} {
		if err := validateLinuxCloneSeedPermissions(permissions[0], permissions[1], permissions[2], permissions[3]); err == nil {
			t.Fatalf("unsafe seed access/default ACLs were accepted: %#o", permissions)
		}
	}

	if err := validateLinuxCloneRootPermissions(5, 7, 5, 7); err != nil {
		t.Fatalf("valid B-owned target-root ACLs: %v", err)
	}
	for _, permissions := range [][4]uint16{
		{7, 7, 5, 7},
		{5, 5, 5, 7},
		{5, 7, 7, 7},
		{5, 7, 5, 5},
	} {
		if err := validateLinuxCloneRootPermissions(permissions[0], permissions[1], permissions[2], permissions[3]); err == nil {
			t.Fatalf("unsafe clone-root access/default ACLs were accepted: %#o", permissions)
		}
	}
}

func TestLinuxCloneRequiresTrustedGitExecutableAndAOwnedHooks(t *testing.T) {
	git := StatIdentity{UID: 0, GID: 0, Mode: uint32(unix.S_IFREG | 0o755), Links: 1}
	if err := validateLinuxTrustedGitExecutableMetadata(git, true); err != nil {
		t.Fatalf("valid root-owned Git executable: %v", err)
	}
	for name, changed := range map[string]StatIdentity{
		"non-root owner":  {UID: 1001, GID: 0, Mode: uint32(unix.S_IFREG | 0o755), Links: 1},
		"group writable":  {UID: 0, GID: 0, Mode: uint32(unix.S_IFREG | 0o775), Links: 1},
		"world writable":  {UID: 0, GID: 0, Mode: uint32(unix.S_IFREG | 0o757), Links: 1},
		"not executable":  {UID: 0, GID: 0, Mode: uint32(unix.S_IFREG | 0o644), Links: 1},
		"setuid":          {UID: 0, GID: 0, Mode: uint32(unix.S_IFREG | 0o4755), Links: 1},
		"multiply linked": {UID: 0, GID: 0, Mode: uint32(unix.S_IFREG | 0o755), Links: 2},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateLinuxTrustedGitExecutableMetadata(changed, true); err == nil {
				t.Fatal("unsafe fixed Git executable was accepted")
			}
		})
	}
	if err := validateLinuxTrustedGitExecutableMetadata(git, false); err == nil {
		t.Fatal("non-regular fixed Git executable was accepted")
	}

	hook := StatIdentity{UID: 1001, GID: 1002, Mode: uint32(unix.S_IFREG | 0o755), Links: 1}
	if err := validateLinuxGitHookMetadata(hook, 0o755, 1001, 1002); err != nil {
		t.Fatalf("valid A-owned hook: %v", err)
	}
	for name, changed := range map[string]StatIdentity{
		"B-owned":         {UID: 1003, GID: 1002, Mode: uint32(unix.S_IFREG | 0o755), Links: 1},
		"multiply linked": {UID: 1001, GID: 1002, Mode: uint32(unix.S_IFREG | 0o755), Links: 2},
		"world writable":  {UID: 1001, GID: 1002, Mode: uint32(unix.S_IFREG | 0o757), Links: 1},
	} {
		t.Run(name, func(t *testing.T) {
			mode := os.FileMode(changed.Mode & 0o7777)
			if err := validateLinuxGitHookMetadata(changed, mode, 1001, 1002); err == nil {
				t.Fatal("unsafe Git hook was accepted")
			}
		})
	}
	if err := validateLinuxGitHookMetadata(StatIdentity{UID: 1001, GID: 1002, Links: 2}, os.ModeDir|0o750, 1001, 1002); err != nil {
		t.Fatalf("directory link counts are not single-link file identity: %v", err)
	}
}

func TestLinuxTrustedAncestorsRequireRootOrAOwnership(t *testing.T) {
	for name, identity := range map[string]StatIdentity{
		"root-owned mode 0555": {UID: 0, GID: 0, Mode: uint32(unix.S_IFDIR | 0o555)},
		"A-owned mode 0555":    {UID: 1001, GID: 1002, Mode: uint32(unix.S_IFDIR | 0o555)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateLinuxTrustedPathAncestor(identity, true, 1001); err != nil {
				t.Fatalf("trusted ancestor rejected: %v", err)
			}
		})
	}
	for name, identity := range map[string]StatIdentity{
		"B-owned but currently read-only": {UID: 1003, GID: 1004, Mode: uint32(unix.S_IFDIR | 0o555)},
		"third-party-owned":               {UID: 1005, GID: 1006, Mode: uint32(unix.S_IFDIR | 0o555)},
		"not a directory":                 {UID: 0, GID: 0, Mode: uint32(unix.S_IFREG | 0o555)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateLinuxTrustedPathAncestor(identity, identity.Mode&unix.S_IFMT == unix.S_IFDIR, 1001); err == nil {
				t.Fatal("untrusted mutable path ancestor was accepted")
			}
		})
	}
}

func TestLinuxSystemdAncestorsRequireRootOwnership(t *testing.T) {
	if err := validateLinuxSystemdDirectoryIdentity(StatIdentity{UID: 0, GID: 0}, true, 0o755); err != nil {
		t.Fatalf("root-owned systemd ancestor rejected: %v", err)
	}
	for name, identity := range map[string]StatIdentity{
		"B-owned read-only": {UID: 1003, GID: 1004},
		"A-owned":           {UID: 1001, GID: 1002},
		"wrong root group":  {UID: 0, GID: 1004},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateLinuxSystemdDirectoryIdentity(identity, true, 0o555); err == nil {
				t.Fatal("replaceable systemd ancestor was accepted")
			}
		})
	}
	if err := validateLinuxSystemdDirectoryIdentity(StatIdentity{UID: 0, GID: 0}, true, 0o775); err == nil {
		t.Fatal("group-writable root systemd ancestor was accepted")
	}
}

func TestLinuxAclWriterCheckUsesEffectiveMaskAndDedicatedGroup(t *testing.T) {
	identity := StatIdentity{UID: 1001, GID: 1002}
	trustedUIDs := []uint32{0, 1001}
	trustedGIDs := []uint32{1002}
	entries := []linuxACLEntry{
		{tag: linuxACLUserObj, perm: 7, id: linuxACLNoID},
		{tag: linuxACLUser, perm: 6, id: 2001},
		{tag: linuxACLGroupObj, perm: 0, id: linuxACLNoID},
		{tag: linuxACLMask, perm: 4, id: linuxACLNoID},
		{tag: linuxACLOther, perm: 0, id: linuxACLNoID},
	}
	if err := validateLinuxACLWriteEntries(entries, identity, trustedUIDs, trustedGIDs); err != nil {
		t.Fatalf("masked-off named-user write was not recognized as read-only: %v", err)
	}
	entries[3].perm = 6
	if err := validateLinuxACLWriteEntries(entries, identity, trustedUIDs, trustedGIDs); err == nil {
		t.Fatal("named third-party ACL write after mask was accepted")
	}
	entries = []linuxACLEntry{
		{tag: linuxACLUserObj, perm: 7, id: linuxACLNoID},
		{tag: linuxACLGroupObj, perm: 0, id: linuxACLNoID},
		{tag: linuxACLGroup, perm: 2, id: 1002},
		{tag: linuxACLMask, perm: 2, id: linuxACLNoID},
		{tag: linuxACLOther, perm: 0, id: linuxACLNoID},
	}
	if err := validateLinuxACLWriteEntries(entries, identity, trustedUIDs, trustedGIDs); err != nil {
		t.Fatalf("dedicated A group writer was rejected: %v", err)
	}
	entries[2].id = 2002
	if err := validateLinuxACLWriteEntries(entries, identity, trustedUIDs, trustedGIDs); err == nil {
		t.Fatal("untrusted named group ACL writer was accepted")
	}
}

func TestLinuxDedicatedAGroupRejectsAdditionalPrimaryAndSupplementaryMembers(t *testing.T) {
	passwd := []byte("root:x:0:0:root:/root:/bin/sh\nsymmetry:x:1001:1002:svc:/home/symmetry:/bin/sh\nnative-b:x:1003:1004:b:/home/native-b:/bin/sh\n")
	group := []byte("symmetry:x:1002:symmetry\n")
	if err := validateLinuxDedicatedGroupDatabases(passwd, group, "symmetry", 1001, 1002); err != nil {
		t.Fatalf("valid dedicated A group: %v", err)
	}
	sharedPrimary := []byte("root:x:0:0:root:/root:/bin/sh\nsymmetry:x:1001:1002:svc:/home/symmetry:/bin/sh\nother:x:1005:1002:other:/home/other:/bin/sh\n")
	if err := validateLinuxDedicatedGroupDatabases(sharedPrimary, group, "symmetry", 1001, 1002); err == nil {
		t.Fatal("another primary member of A group was accepted")
	}
	if err := validateLinuxDedicatedGroupDatabases(passwd, []byte("symmetry:x:1002:symmetry,other\n"), "symmetry", 1001, 1002); err == nil {
		t.Fatal("another supplementary member of A group was accepted")
	}
}

func TestLinuxCloneSeedLayoutNamesAreCanonical(t *testing.T) {
	if !validLinuxCloneSeedDirectoryName("binding-primary", 0) || !validLinuxCloneSeedDirectoryName("run-test-run", 1) {
		t.Fatal("valid binding/run seed directory names were rejected")
	}
	if validLinuxCloneSeedDirectoryName("run-test-run", 0) || validLinuxCloneSeedDirectoryName("binding-primary", 1) {
		t.Fatal("seed layout accepted directory names at the wrong level")
	}
	if !validLinuxCloneBundleName("generation-1.bundle") || validLinuxCloneBundleName("generation-01.bundle") || validLinuxCloneBundleName("generation-0.bundle") {
		t.Fatal("seed bundle generation name boundary is incorrect")
	}
	if !validLinuxCloneSeedTempName(".clone-seed-"+strings.Repeat("a", 24)) || validLinuxCloneSeedTempName(".clone-seed-"+strings.Repeat("A", 24)) {
		t.Fatal("seed temporary name must be exactly lowercase hexadecimal")
	}
}

func TestLinuxSystemdEffectivePropertiesRequireLiveOwnerAndExactCommands(t *testing.T) {
	policy := linuxSystemdUnitPolicy{
		unit: "symmetry-owner.service", user: "symmetry", group: "symmetry", home: "/home/symmetry",
		owner: true, delegate: "yes", delegatedSubgroup: "manager",
		execStart: []string{"/usr/libexec/symmetry/symmetry-daemon", "-linux-owner-anchor", "-cgroup-mount", "/sys/fs/cgroup",
			"-delegated-path", "system.slice/symmetry-owner.service/manager", "-socket", "/run/symmetry-owner/owner.sock"},
		execStop: []string{"/usr/libexec/symmetry/symmetry-daemon", "-recover-containment-only", "-config", "/etc/symmetry/daemon.json"},
	}
	properties := linuxOwnerSystemdShowFixture(policy)
	if err := validateEffectiveLinuxSystemdProperties(properties, policy, nil, ACLSubject{UID: 1001, GID: 1002}, ACLSubject{UID: 1003, GID: 1004}); err != nil {
		t.Fatalf("valid effective owner unit properties: %v", err)
	}
	humanized := cloneStringMap(properties)
	humanized["TimeoutStopUSec"] = "12min 0s"
	if err := validateEffectiveLinuxSystemdProperties(humanized, policy, nil, ACLSubject{UID: 1001, GID: 1002}, ACLSubject{UID: 1003, GID: 1004}); err != nil {
		t.Fatalf("systemctl's exact humanized owner timeout was rejected: %v", err)
	}
	if duration, err := parseLinuxSystemdDurationMicroseconds("2min"); err != nil || duration != 120_000_000 {
		t.Fatalf("parse systemctl daemon timeout = (%d, %v), want 120000000 usec", duration, err)
	}
	for name, mutate := range map[string]func(map[string]string){
		"send sigkill disabled": func(values map[string]string) { values["SendSIGKILL"] = "no" },
		"short stop deadline":   func(values map[string]string) { values["TimeoutStopUSec"] = "119999999" },
		"long stop deadline":    func(values map[string]string) { values["TimeoutStopUSec"] = "13min" },
		"wrong cgroup anchor":   func(values map[string]string) { values["ControlGroup"] = "/system.slice/wrong.service" },
		"extra stop post": func(values map[string]string) {
			values["ExecStopPost"] = linuxSystemdShowCommand([]string{"/bin/true"})
		},
		"privileged stop": func(values map[string]string) {
			values["ExecStop"] = strings.Replace(values["ExecStop"], "flags= ;", "flags=fully-privileged ;", 1)
		},
		"ignore stop failure": func(values map[string]string) {
			values["ExecStop"] = strings.Replace(values["ExecStop"], "ignore_errors=no", "ignore_errors=yes", 1)
		},
		"unknown drop-in": func(values map[string]string) {
			values["DropInPaths"] = "/run/systemd/system/symmetry-owner.service.d/override.conf"
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := cloneStringMap(properties)
			mutate(mutated)
			if err := validateEffectiveLinuxSystemdProperties(mutated, policy, nil, ACLSubject{UID: 1001, GID: 1002}, ACLSubject{UID: 1003, GID: 1004}); err == nil {
				t.Fatal("unsafe effective systemd state was accepted")
			}
		})
	}
	// systemd registers the inverse of the daemon's own required After edge on the owner
	// (inverse_table[UNIT_AFTER] = UNIT_BEFORE), so an effective owner Before is expected here.
	inverseOrdering := cloneStringMap(properties)
	inverseOrdering["Before"] = "symmetry-daemon.service"
	if err := validateEffectiveLinuxSystemdProperties(inverseOrdering, policy, nil, ACLSubject{UID: 1001, GID: 1002}, ACLSubject{UID: 1003, GID: 1004}); err != nil {
		t.Fatalf("systemd's inverse owner ordering edge to the daemon was rejected: %v", err)
	}
	for _, key := range []string{"After", "BindsTo", "Requires", "Requisite", "Wants", "PartOf", "Conflicts"} {
		t.Run("owner/"+key, func(t *testing.T) {
			mutated := cloneStringMap(properties)
			if key == "After" {
				mutated[key] = "local-fs.target symmetry-daemon.service"
			} else {
				mutated[key] = "symmetry-daemon.service"
			}
			err := validateEffectiveLinuxSystemdProperties(mutated, policy, nil, ACLSubject{UID: 1001, GID: 1002}, ACLSubject{UID: 1003, GID: 1004})
			if err == nil || !strings.Contains(err.Error(), "through "+key) {
				t.Fatalf("effective owner %s edge to the daemon was accepted: %v", key, err)
			}
		})
	}
	parsed, err := parseLinuxSystemdShowProperties([]byte("LoadState=loaded\nFragmentPath=/etc/systemd/system/symmetry-owner.service\n"))
	if err != nil || parsed["LoadState"] != "loaded" {
		t.Fatalf("parse systemd show output = %#v, %v", parsed, err)
	}
	if _, err := parseLinuxSystemdShowProperties([]byte("LoadState=loaded\nLoadState=not-loaded\n")); err == nil {
		t.Fatal("duplicate effective systemd property was accepted")
	}
	if _, err := parseLinuxSystemdShowExec(linuxSystemdShowCommand(policy.execStart) + " { path=/bin/true ; argv[]=/bin/true }"); err == nil {
		t.Fatal("multiple effective systemd ExecStart commands were accepted")
	}
}

func TestLinuxSystemdEffectivePropertiesAllowOmittedEmptyExecHooks(t *testing.T) {
	policy := linuxSystemdUnitPolicy{
		unit: "symmetry-daemon.service", user: "symmetry", group: "symmetry", home: "/home/symmetry",
		execStart:    []string{"/usr/libexec/symmetry/symmetry-daemon", "-config", "/etc/symmetry/daemon.json"},
		execStartPre: []string{"/usr/libexec/symmetry/symmetry-daemon", "-verify-linux-owner-install", "-config", "/etc/symmetry/daemon.json"},
	}
	properties := map[string]string{
		"LoadState": "loaded", "FragmentPath": "/etc/systemd/system/symmetry-daemon.service", "DropInPaths": "",
		"Type": "simple", "User": "symmetry", "Group": "symmetry", "NoNewPrivileges": "no", "UMask": "0077",
		"WorkingDirectory": "/", "KillMode": "control-group", "SendSIGKILL": "yes", "TimeoutStopUSec": "120000000",
		"Restart": "on-failure", "ExecStart": linuxSystemdShowCommand(policy.execStart),
		"ExecStartPre": linuxSystemdShowCommand(policy.execStartPre),
		"Slice":        "system.slice", "Delegate": "no", "DelegateSubgroup": "", "RuntimeDirectory": "",
		"RuntimeDirectoryMode": "", "After": "local-fs.target symmetry-owner.service",
		"BindsTo": "symmetry-owner.service", "Environment": "", "EnvironmentFiles": "", "PassEnvironment": "",
		"UnsetEnvironment": "", "SupplementaryGroups": "", "Before": "", "Requires": "", "Requisite": "",
		"Wants": "", "PartOf": "", "Conflicts": "", "RequiresMountsFor": "",
	}
	// systemctl omits unset optional Exec hooks rather than returning an empty
	// property, which is equivalent to an empty command list.
	delete(properties, "ExecStop")
	delete(properties, "ExecStopPost")
	delete(properties, "EnvironmentFiles")
	delete(properties, "PassEnvironment")
	delete(properties, "UnsetEnvironment")
	delete(properties, "SupplementaryGroups")
	if err := validateEffectiveLinuxSystemdProperties(properties, policy, nil, ACLSubject{UID: 1001, GID: 1002}, ACLSubject{UID: 1003, GID: 1004}); err != nil {
		t.Fatalf("omitted empty Exec hooks were rejected: %v", err)
	}
	delete(properties, "ExecStart")
	if err := validateEffectiveLinuxSystemdProperties(properties, policy, nil, ACLSubject{UID: 1001, GID: 1002}, ACLSubject{UID: 1003, GID: 1004}); err == nil {
		t.Fatal("omitted ExecStart was accepted")
	}
	parsed, err := parseLinuxSystemdShowExec("{ path=/bin/true ; argv[]=/bin/true ; ignore_errors=no }")
	if err != nil || parsed.flags != "" || len(parsed.argv) != 1 || parsed.argv[0] != "/bin/true" {
		t.Fatalf("omitted empty Exec flags = %#v, %v", parsed, err)
	}
}

func linuxOwnerSystemdShowFixture(policy linuxSystemdUnitPolicy) map[string]string {
	return map[string]string{
		"LoadState": "loaded", "FragmentPath": "/etc/systemd/system/" + policy.unit, "DropInPaths": "",
		"ActiveState": "active", "SubState": "running", "ControlGroup": "/system.slice/symmetry-owner.service",
		"Type": "notify", "NotifyAccess": "main", "User": policy.user, "Group": policy.group,
		"NoNewPrivileges": "no", "UMask": "0077", "WorkingDirectory": "/", "KillMode": "control-group", "SendSIGKILL": "yes",
		"TimeoutStartUSec": "30000000", "TimeoutStopUSec": "720000000", "Restart": "on-failure", "RestartUSec": "5000000",
		"ExecStart": linuxSystemdShowCommand(policy.execStart), "ExecStartPre": "", "ExecStop": linuxSystemdShowCommand(policy.execStop), "ExecStopPost": "",
		"Slice": "system.slice", "RuntimeDirectory": "symmetry-owner", "RuntimeDirectoryMode": "0700",
		"Delegate": "yes", "DelegateSubgroup": "manager", "After": "local-fs.target", "Before": "", "BindsTo": "",
		"Requires": "", "Requisite": "", "Wants": "", "PartOf": "", "Conflicts": "", "RequiresMountsFor": "/sys/fs/cgroup",
		"Environment": "", "EnvironmentFiles": "", "PassEnvironment": "", "UnsetEnvironment": "", "SupplementaryGroups": "",
	}
}

func linuxSystemdShowCommand(argv []string) string {
	return "{ path=" + argv[0] + " ; argv[]=" + strings.Join(argv, " ") + " ; flags= ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }"
}

func cloneStringMap(value map[string]string) map[string]string {
	copy := make(map[string]string, len(value))
	for key, item := range value {
		copy[key] = item
	}
	return copy
}

func TestLinuxOwnerAclRejectsUnsafeBWriteAccess(t *testing.T) {
	const (
		aUID = 1001
		aGID = 1002
		bUID = 1003
		bGID = 1004
	)
	unsafe := []linuxACLEntry{
		{tag: linuxACLUserObj, perm: 7, id: linuxACLNoID},
		{tag: linuxACLUser, perm: 7, id: bUID},
		{tag: linuxACLGroupObj, perm: 0, id: linuxACLNoID},
		{tag: linuxACLMask, perm: 7, id: linuxACLNoID},
		{tag: linuxACLOther, perm: 0, id: linuxACLNoID},
	}
	permissions, err := permissionsFromLinuxACL(unsafe, StatIdentity{UID: aUID, GID: aGID}, ACLSubject{UID: bUID, GID: bGID})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEffectiveLinuxPermissions(permissions, 0, 2, ACLSubject{UID: bUID, GID: bGID}); err == nil {
		t.Fatal("named-user B write ACL was accepted for protected host data")
	}

	readOnly := []linuxACLEntry{
		{tag: linuxACLUserObj, perm: 7, id: linuxACLNoID},
		{tag: linuxACLUser, perm: 5, id: bUID},
		{tag: linuxACLGroupObj, perm: 0, id: linuxACLNoID},
		{tag: linuxACLMask, perm: 5, id: linuxACLNoID},
		{tag: linuxACLOther, perm: 0, id: linuxACLNoID},
	}
	permissions, err = permissionsFromLinuxACL(readOnly, StatIdentity{UID: aUID, GID: aGID}, ACLSubject{UID: bUID, GID: bGID})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEffectiveLinuxPermissions(permissions, 5, 2, ACLSubject{UID: bUID, GID: bGID}); err != nil {
		t.Fatalf("read/search-only B ACL was rejected: %v", err)
	}

	otherGroup := []linuxACLEntry{
		{tag: linuxACLUserObj, perm: 7, id: linuxACLNoID},
		{tag: linuxACLGroupObj, perm: 0, id: linuxACLNoID},
		{tag: linuxACLGroup, perm: 7, id: aGID},
		{tag: linuxACLMask, perm: 7, id: linuxACLNoID},
		{tag: linuxACLOther, perm: 0, id: linuxACLNoID},
	}
	permissions, err = permissionsFromLinuxACL(otherGroup, StatIdentity{UID: aUID, GID: aGID}, ACLSubject{UID: bUID, GID: bGID, groups: []uint32{bGID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEffectiveLinuxPermissions(permissions, 5, 0, ACLSubject{UID: bUID, GID: bGID}); err == nil {
		t.Fatal("host supplementary group rights were assigned to the B user namespace")
	}
}

func TestUnescapeLinuxMountPathDecodesMountinfoEscapes(t *testing.T) {
	for value, want := range map[string]string{
		"/mnt/plain":             "/mnt/plain",
		`/mnt/with\040space`:     "/mnt/with space",
		`/mnt/with\011tab`:       "/mnt/with\ttab",
		`/mnt/with\012newline`:   "/mnt/with\nnewline",
		`/mnt/with\134backslash`: `/mnt/with\backslash`,
		`/mnt/a\040b\040c`:       "/mnt/a b c",
		`/mnt/a\040\011b`:        "/mnt/a \tb",
		`/mnt/a\040`:             "/mnt/a ",
	} {
		got, err := unescapeLinuxMountPath(value)
		if err != nil {
			t.Errorf("unescapeLinuxMountPath(%q) = %v, want %q", value, err, want)
			continue
		}
		if got != want {
			t.Errorf("unescapeLinuxMountPath(%q) = %q, want %q", value, got, want)
			continue
		}
		if !filepath.IsAbs(got) || filepath.Clean(got) != got {
			t.Errorf("unescapeLinuxMountPath(%q) = %q, want the absolute canonical mount point the mount check accepts", value, got)
		}
	}
	for _, value := range []string{`\`, `abc\`, `/mnt/short\04`, `/mnt/\777`, `/mnt/\04x`} {
		if got, err := unescapeLinuxMountPath(value); err == nil {
			t.Errorf("unescapeLinuxMountPath(%q) = %q, want escape rejection", value, got)
		}
	}
}

func terminalLinuxDrainJournal() state.RunJournal {
	return state.RunJournal{
		RunID: "run-terminal", Generation: 1, LocalState: "terminal_pending", TerminalState: "failed",
		TerminalPendingAt: time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC),
	}
}

func testLinuxV1DrainSupervisor() authority.Supervisor {
	return authority.Supervisor{
		Version: authority.SupervisorVersion, Secret: strings.Repeat("a", authority.SecretBytes*2),
		LaunchToken: strings.Repeat("b", authority.TokenBytes*2), OwnerKind: authority.OwnerKindLinuxHelper, OwnerContext: "/manager",
		TargetPID: 42, TargetIdentity: "linux:42:target", PipeToken: strings.Repeat("c", authority.TokenBytes*2),
		JobID: strings.Repeat("d", authority.TokenBytes*2), SupervisorPID: 43, SupervisorIdentity: "linux:43:helper",
	}
}

func testLinuxV1DrainReceipt(value authority.Supervisor) authority.StopReceipt {
	return authority.StopReceipt{
		Version: authority.SupervisorVersion, Status: "stopped", OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext,
		TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, PipeToken: value.PipeToken, JobID: value.JobID,
		SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, ActiveProcesses: 0,
	}
}

func configForLinuxPreflight(stateDir string) config.Config {
	return config.Config{StateDir: stateDir}
}

func TestLinuxAppArmorUsernsDecisionRows(t *testing.T) {
	validFile := linuxAppArmorUsernsFacts{restriction: "1", policyInterface: "read", profileFilePresent: true, profileFileValid: true}
	loadedValid := linuxAppArmorUsernsFacts{
		restriction: "1", policyInterface: "read",
		profileFilePresent: true, profileFileValid: true,
		loaded: true, loadedMode: "unconfined",
		loadedPolicyHash: strings.Repeat("a", 64), compiledHashes: []string{strings.Repeat("a", 64)},
	}
	for _, test := range []struct {
		name   string
		facts  linuxAppArmorUsernsFacts
		expect string
	}{
		{name: "restriction-absent-nothing-installed", facts: linuxAppArmorUsernsFacts{restriction: "absent", policyInterface: "absent"}, expect: LinuxAppArmorUsernsNotNeeded},
		{name: "restriction-zero-nothing-installed", facts: linuxAppArmorUsernsFacts{restriction: "0", policyInterface: "read"}, expect: LinuxAppArmorUsernsNotNeeded},
		{name: "restriction-zero-with-absent-interface", facts: linuxAppArmorUsernsFacts{restriction: "0", policyInterface: "absent"}, expect: LinuxAppArmorUsernsNotNeeded},
		{name: "restriction-zero-with-installed-file", facts: linuxAppArmorUsernsFacts{restriction: "0", policyInterface: "read", profileFilePresent: true}, expect: LinuxAppArmorUsernsUnexpected},
		{name: "restriction-zero-with-loaded-profile", facts: linuxAppArmorUsernsFacts{restriction: "0", policyInterface: "read", loaded: true}, expect: LinuxAppArmorUsernsUnexpected},
		{name: "restriction-zero-with-unreadable-interface", facts: linuxAppArmorUsernsFacts{restriction: "0", policyInterface: "unreadable"}, expect: LinuxAppArmorUsernsStateUnreadable},
		{name: "restriction-unreadable", facts: linuxAppArmorUsernsFacts{restriction: "unreadable"}, expect: LinuxAppArmorUsernsStateUnreadable},
		{name: "restriction-invalid", facts: linuxAppArmorUsernsFacts{restriction: "invalid"}, expect: LinuxAppArmorUsernsSysctlInvalid},
		{name: "restriction-one-with-absent-interface", facts: linuxAppArmorUsernsFacts{restriction: "1", policyInterface: "absent"}, expect: LinuxAppArmorUsernsStateUnreadable},
		{name: "restriction-one-with-absent-file", facts: linuxAppArmorUsernsFacts{restriction: "1", policyInterface: "read"}, expect: LinuxAppArmorUsernsAbsent},
		{name: "restriction-one-with-untrusted-file", facts: linuxAppArmorUsernsFacts{restriction: "1", policyInterface: "read", profileFilePresent: true, profileFileValid: false}, expect: LinuxAppArmorUsernsFileMismatch},
		{name: "restriction-one-without-load", facts: validFile, expect: LinuxAppArmorUsernsNotLoaded},
		{name: "restriction-one-with-foreign-mode", facts: linuxAppArmorUsernsFacts{restriction: "1", policyInterface: "read", profileFilePresent: true, profileFileValid: true, loaded: true, loadedMode: "enforce"}, expect: LinuxAppArmorUsernsLoadedMismatch},
		{name: "restriction-one-with-duplicate-name", facts: linuxAppArmorUsernsFacts{restriction: "1", policyInterface: "read", profileFilePresent: true, profileFileValid: true, loaded: true, loadedDuplicate: true, loadedMode: "unconfined"}, expect: LinuxAppArmorUsernsLoadedMismatch},
		{name: "restriction-one-without-parser", facts: linuxAppArmorUsernsFacts{restriction: "1", policyInterface: "read", profileFilePresent: true, profileFileValid: true, loaded: true, loadedMode: "unconfined", compiledUnavailable: "missing"}, expect: LinuxAppArmorUsernsParserAbsent},
		{name: "restriction-one-with-failed-parser", facts: linuxAppArmorUsernsFacts{restriction: "1", policyInterface: "read", profileFilePresent: true, profileFileValid: true, loaded: true, loadedMode: "unconfined", compiledUnavailable: "failed"}, expect: LinuxAppArmorUsernsParserFailed},
		{name: "restriction-one-with-unreadable-hash", facts: linuxAppArmorUsernsFacts{restriction: "1", policyInterface: "read", profileFilePresent: true, profileFileValid: true, loaded: true, loadedMode: "unconfined"}, expect: LinuxAppArmorUsernsStateUnreadable},
		{name: "restriction-one-with-malformed-hash", facts: linuxAppArmorUsernsFacts{restriction: "1", policyInterface: "read", profileFilePresent: true, profileFileValid: true, loaded: true, loadedMode: "unconfined", loadedPolicyHash: "not-a-hash"}, expect: LinuxAppArmorUsernsStateUnreadable},
		{name: "restriction-one-with-compression-disabled-whole", facts: loadedValid, expect: LinuxAppArmorUsernsPerProgram},
		{name: "restriction-one-with-uncompressed-payload", facts: func() linuxAppArmorUsernsFacts {
			facts := loadedValid
			facts.compiledHashes = []string{strings.Repeat("b", 64), facts.loadedPolicyHash}
			return facts
		}(), expect: LinuxAppArmorUsernsPerProgram},
		{name: "restriction-one-with-different-policy", facts: func() linuxAppArmorUsernsFacts {
			facts := loadedValid
			facts.compiledHashes = []string{strings.Repeat("b", 64), strings.Repeat("c", 64)}
			return facts
		}(), expect: LinuxAppArmorUsernsLoadedMismatch},
	} {
		decision, err := decideLinuxAppArmorUserns(test.facts)
		if decision != test.expect {
			t.Errorf("%s: decision = %q, want %q (err = %v)", test.name, decision, test.expect, err)
			continue
		}
		if test.expect == LinuxAppArmorUsernsNotNeeded || test.expect == LinuxAppArmorUsernsPerProgram {
			if err != nil {
				t.Errorf("%s: acceptance carried an error: %v", test.name, err)
			}
			continue
		}
		if err == nil || !strings.HasPrefix(err.Error(), "apparmor_userns="+test.expect+":") {
			t.Errorf("%s: refusal error = %v, want the %s decision line", test.name, err, test.expect)
		}
	}
}

// The installed preflight must compile exactly the bytes scripts/install-linux-owner.sh
// publishes: the text path is the single source and its attachments stay exact and minimal.
func TestLinuxAppArmorProfileTextMatchesTheInstaller(t *testing.T) {
	output, err := exec.Command("sh", filepath.Join("..", "..", "..", "scripts", "install-linux-owner.sh"), "-print-userns-profile").Output()
	if err != nil {
		t.Fatalf("run the installer's userns profile emit mode: %v", err)
	}
	text := string(output)
	if text != linuxAppArmorProfileText() {
		t.Fatalf("installer profile text differs from the preflight text:\n%q\n%q", text, linuxAppArmorProfileText())
	}
	lines := strings.Split(text, "\n")
	if len(lines) != 6 || lines[5] != "" {
		t.Fatalf("profile text must be exactly five lines: %q", text)
	}
	if lines[0] != "abi <abi/4.0>," || lines[1] != "include <tunables/global>" {
		t.Fatalf("profile boilerplate differs from the documented per-program pattern: %q", text)
	}
	if lines[2] != "profile "+linuxAppArmorProfileName+" "+linuxAppArmorGateAttachment+" flags=(unconfined) {" {
		t.Fatalf("profile stanza must attach one exact installed path: %q", lines[2])
	}
	if lines[3] != "  userns," || lines[4] != "}" {
		t.Fatalf("profile body must be exactly the userns rule: %q", text)
	}
	for _, forbidden := range []string{"*", "?", "[", "]", "capability", "signal", "network", "mount", "ptrace", "dbus", "complain", "mediate", "kill", "hat"} {
		if strings.Contains(lines[2], forbidden) {
			t.Fatalf("profile stanza carries a non-exact attachment or an extra flag: %q", lines[2])
		}
	}
	if strings.Count(text, "profile ") != 1 {
		t.Fatalf("profile text must carry exactly one profile stanza: %q", text)
	}
}

func TestLinuxAppArmorLoadedProfileScan(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, linuxAppArmorProfileName+".7")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte(value), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("name", linuxAppArmorProfileName+"\n")
	writeFile("mode", "unconfined\n")
	writeFile("raw_sha256", strings.Repeat("c", 64)+"\n")
	other := filepath.Join(root, "other.1")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "name"), []byte("other\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stray-file"), []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	loaded, matched, duplicate, err := parseLinuxAppArmorLoadedProfiles(root)
	if err != nil || !matched || duplicate {
		t.Fatalf("scan of a valid single profile: loaded = %+v, matched = %t, duplicate = %t, err = %v", loaded, matched, duplicate, err)
	}
	if loaded.mode != "unconfined" || loaded.policyHash != strings.Repeat("c", 64) {
		t.Fatalf("scan must read the exact profile state: %+v", loaded)
	}
	if err := os.Mkdir(filepath.Join(root, linuxAppArmorProfileName+".8"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, linuxAppArmorProfileName+".8", "name"), []byte(linuxAppArmorProfileName+"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, _, duplicate, err := parseLinuxAppArmorLoadedProfiles(root); err != nil || !duplicate {
		t.Fatalf("duplicate occurrences of the fixed name must be reported: duplicate = %t, err = %v", duplicate, err)
	}
}

// The host-state readers must keep the closed decision vocabulary on real file state: the
// sysctl reader, the profile-file verifier and the compression-disabled compile.
func TestLinuxAppArmorHostStateRows(t *testing.T) {
	previousSysctl, previousPath, previousDir := linuxAppArmorSysctlPath, linuxAppArmorInstallPath, linuxAppArmorPolicyPath
	t.Cleanup(func() {
		linuxAppArmorSysctlPath, linuxAppArmorInstallPath, linuxAppArmorPolicyPath = previousSysctl, previousPath, previousDir
	})
	directory := t.TempDir()
	linuxAppArmorSysctlPath = filepath.Join(directory, "restrict_unprivileged_userns")
	if got := readLinuxAppArmorUsernsRestriction(); got != "absent" {
		t.Errorf("absent sysctl = %q, want absent", got)
	}
	for value, want := range map[string]string{"1\n": "1", "0\n": "0", "2\n": "invalid", "": "invalid"} {
		if err := os.WriteFile(linuxAppArmorSysctlPath, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readLinuxAppArmorUsernsRestriction(); got != want {
			t.Errorf("sysctl %q = %q, want %q", value, got, want)
		}
	}
	if err := os.Remove(linuxAppArmorSysctlPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(linuxAppArmorSysctlPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := readLinuxAppArmorUsernsRestriction(); got != "unreadable" {
		t.Errorf("directory sysctl = %q, want unreadable", got)
	}
	if err := os.Remove(linuxAppArmorSysctlPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "restrict-target")
	if err := os.WriteFile(target, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, linuxAppArmorSysctlPath); err != nil {
		t.Fatal(err)
	}
	if got := readLinuxAppArmorUsernsRestriction(); got != "unreadable" {
		t.Errorf("symlinked sysctl = %q, want unreadable (the installer refuses it too)", got)
	}
	if err := os.Remove(linuxAppArmorSysctlPath); err != nil {
		t.Fatal(err)
	}

	linuxAppArmorPolicyPath = directory
	linuxAppArmorInstallPath = filepath.Join(directory, linuxAppArmorProfileName)
	minor := ACLSubject{UID: 4242, GID: 4242}
	if present, valid := verifyLinuxOwnerAppArmorProfileFile(minor, minor); present || valid {
		t.Errorf("absent profile file: present = %t, valid = %t", present, valid)
	}
	if err := os.WriteFile(linuxAppArmorInstallPath, []byte(linuxAppArmorProfileText()), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(linuxAppArmorInstallPath, 0o666); err != nil {
		t.Fatal(err)
	}
	if present, valid := verifyLinuxOwnerAppArmorProfileFile(minor, minor); !present || valid {
		t.Errorf("group/world-writable profile file: present = %t, valid = %t", present, valid)
	}
	if err := os.Chmod(linuxAppArmorInstallPath, 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(directory, "symlinked-userns-profile")
	if err := os.Symlink(linuxAppArmorInstallPath, symlink); err != nil {
		t.Fatal(err)
	}
	linuxAppArmorInstallPath = symlink
	if present, valid := verifyLinuxOwnerAppArmorProfileFile(minor, minor); !present || valid {
		t.Errorf("symlinked profile file: present = %t, valid = %t", present, valid)
	}
	// Content and directory checks need a root-owned path, so the production positive rows are
	// the runner's own install and verify; the unprivileged test user only reaches the
	// metadata refusals above.
	linuxAppArmorInstallPath = filepath.Join(directory, "foreign-userns-profile")
	if err := os.WriteFile(linuxAppArmorInstallPath, []byte("profile other /bin/true flags=(unconfined) {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		if present, valid := verifyLinuxOwnerAppArmorProfileFile(minor, minor); !present || valid {
			t.Errorf("foreign profile text as root: present = %t, valid = %t", present, valid)
		}
		if err := os.WriteFile(linuxAppArmorInstallPath, []byte(linuxAppArmorProfileText()), 0o644); err != nil {
			t.Fatal(err)
		}
		if present, valid := verifyLinuxOwnerAppArmorProfileFile(minor, minor); !present || !valid {
			t.Errorf("exact profile text as root: present = %t, valid = %t", present, valid)
		}
	}

	linuxAppArmorInstallPath = filepath.Join(directory, "staged-userns-profile")
	if err := os.WriteFile(linuxAppArmorInstallPath, []byte(linuxAppArmorProfileText()), 0o644); err != nil {
		t.Fatal(err)
	}
	hashes, unavailable := compileLinuxAppArmorProfile(minor, minor)
	if unavailable != "" || len(hashes) == 0 {
		t.Skipf("no usable apparmor_parser on this host: unavailable = %q", unavailable)
	}
	for _, hash := range hashes {
		if len(hash) != linuxAppArmorPolicyHashHexLength || !isLowerHex(hash) {
			t.Errorf("compiled digest %q is not a lowercase sha256", hash)
		}
	}
}

// The compile order is the readback's core: compression is disabled first, and only a parser
// that rejects -c0 (the parser < 5 shape) gets the plain compilation.
func TestLinuxAppArmorCompileRows(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the parser identity and directory checks require root")
	}
	previousPaths, previousInstall := linuxAppArmorParserPaths, linuxAppArmorInstallPath
	t.Cleanup(func() { linuxAppArmorParserPaths, linuxAppArmorInstallPath = previousPaths, previousInstall })
	directory := t.TempDir()
	calls := filepath.Join(directory, "parser-calls.log")
	linuxAppArmorInstallPath = filepath.Join(directory, "profile")
	if err := os.WriteFile(linuxAppArmorInstallPath, []byte(linuxAppArmorProfileText()), 0o644); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(directory, "apparmor_parser")
	stubText := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$APPARMOR_STUB_CALLS\"\ncase \" $* \" in\n  *\" -c0 \"*) [ \"$APPARMOR_STUB_MODE\" = reject-c0 ] && exit 0 ;;\nesac\nprintf 'stub-policy-bytes\\n'\n"
	if err := os.WriteFile(stub, []byte(stubText), 0o755); err != nil {
		t.Fatal(err)
	}
	linuxAppArmorParserPaths = []string{stub}
	t.Setenv("APPARMOR_STUB_CALLS", calls)
	minor := ACLSubject{UID: 4242, GID: 4242}
	expected := sha256.Sum256([]byte("stub-policy-bytes\n"))
	readCalls := func() []string {
		t.Helper()
		logged, err := os.ReadFile(calls)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(logged)), "\n")
	}

	t.Setenv("APPARMOR_STUB_MODE", "reject-c0")
	hashes, unavailable := compileLinuxAppArmorProfile(minor, minor)
	if unavailable != "" || len(hashes) == 0 {
		t.Fatalf("a rejecting -c0 parser must still compile: unavailable = %q, hashes = %v", unavailable, hashes)
	}
	if hashes[0] != hex.EncodeToString(expected[:]) {
		t.Errorf("whole-output digest = %q, want the stub's bytes", hashes[0])
	}
	lines := readCalls()
	if len(lines) != 2 {
		t.Fatalf("a rejected -c0 attempt must be followed by exactly one plain compile: %v", lines)
	}
	if !strings.Contains(lines[0], "-c0") || strings.Contains(lines[1], "-c0") {
		t.Errorf("compile order must be -c0 then plain: %v", lines)
	}

	if err := os.Remove(calls); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPARMOR_STUB_MODE", "accept-c0")
	if hashes, unavailable = compileLinuxAppArmorProfile(minor, minor); unavailable != "" || len(hashes) == 0 {
		t.Fatalf("an accepting -c0 parser must compile: unavailable = %q, hashes = %v", unavailable, hashes)
	}
	if lines = readCalls(); len(lines) != 1 || !strings.Contains(lines[0], "-c0") {
		t.Errorf("an accepting -c0 parser must be compiled exactly once with -c0: %v", lines)
	}
}

func claimedStoreAt(t *testing.T, directory string) (*state.Store, state.RunKey) {
	t.Helper()
	store, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	_, err = store.SaveClaimIntent(state.ClaimIntent{Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-1", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveClaimGrant(key, protocol.ClaimResponse{RunID: key.RunID, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetLocalState(key, "waiting_for_input"); err != nil {
		t.Fatal(err)
	}
	return store, key
}
