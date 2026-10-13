//go:build linux && amd64

package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
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
	if err := verifyLinuxOwnerStateDirBinding(configured, owner); err != nil {
		t.Fatalf("matching independently pinned StateDir rejected: %v", err)
	}

	other := filepath.Join(root, "other-state")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	owner.StateDir = other
	if err := verifyLinuxOwnerStateDirBinding(configured, owner); err == nil {
		t.Fatal("different root-installed StateDir was accepted")
	}

	link := filepath.Join(root, "state-link")
	if err := os.Symlink(configured, link); err != nil {
		t.Fatal(err)
	}
	owner.StateDir = link
	if err := verifyLinuxOwnerStateDirBinding(link, owner); err == nil {
		t.Fatal("symlinked root-installed StateDir was accepted")
	}

	owner.StateDir = configured
	if err := os.Chmod(configured, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := verifyLinuxOwnerStateDirBinding(configured, owner); err == nil {
		t.Fatal("non-private state directory identity was accepted")
	}
}

func TestCanonicalLinuxOwnerStateDirUsesRootWorkingDirectory(t *testing.T) {
	for input, want := range map[string]string{
		".symmetry/state":           "/.symmetry/state",
		"var/lib/symmetry/state":    "/var/lib/symmetry/state",
		"../var/lib/symmetry/state": "/var/lib/symmetry/state",
	} {
		got, err := canonicalLinuxOwnerStateDir(input)
		if err != nil || got != want {
			t.Fatalf("canonicalLinuxOwnerStateDir(%q) = %q, %v; want %q", input, got, err, want)
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
	a := linuxACLSubject{uid: 1001, gid: 1002}
	b := linuxACLSubject{uid: 1003, gid: 1004}
	if err := validateLinuxOwnerManagerKillMetadata(0, 0, unix.S_IFREG|0o200, 1001, a, b); err != nil {
		t.Fatalf("root-only parent cgroup.kill metadata was rejected: %v", err)
	}
	if err := validateLinuxOwnerManagerKillMetadata(1001, 0, unix.S_IFREG|0o200, 1001, a, b); err == nil {
		t.Fatal("A-owned parent cgroup.kill was accepted instead of remaining root-only")
	}
	if err := validateLinuxOwnerManagerKillMetadata(0, 0, unix.S_IFREG|0o220, 1001, a, b); err == nil {
		t.Fatal("group-writable parent cgroup.kill was accepted")
	}

	wrapper := trustedLinuxAsset{uid: 0, gid: 1002, mode: 0o4750, links: 1, denyBAll: true}
	if err := validateTrustedLinuxAssetIdentity(linuxStatIdentity{uid: 0, gid: 1002, mode: 0o4750, links: 1}, true, wrapper); err != nil {
		t.Fatalf("valid root wrapper metadata: %v", err)
	}
	if err := validateTrustedLinuxAssetIdentity(linuxStatIdentity{uid: 0, gid: 1002, mode: 0o4755, links: 1}, true, wrapper); err == nil {
		t.Fatal("world-executable privileged wrapper was accepted")
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
	a := linuxACLSubject{uid: 1001, gid: 1002}
	b := linuxACLSubject{uid: 1003, gid: 1004}
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
	a := linuxACLSubject{uid: 1001, gid: 1002}
	b := linuxACLSubject{uid: 1003, gid: 1004}
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
	a := linuxACLSubject{uid: 1001, gid: 1002}
	b := linuxACLSubject{uid: 1003, gid: 1004}
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
	git := linuxStatIdentity{uid: 0, gid: 0, mode: uint32(unix.S_IFREG | 0o755), links: 1}
	if err := validateLinuxTrustedGitExecutableMetadata(git, true); err != nil {
		t.Fatalf("valid root-owned Git executable: %v", err)
	}
	for name, changed := range map[string]linuxStatIdentity{
		"non-root owner":  {uid: 1001, gid: 0, mode: uint32(unix.S_IFREG | 0o755), links: 1},
		"group writable":  {uid: 0, gid: 0, mode: uint32(unix.S_IFREG | 0o775), links: 1},
		"world writable":  {uid: 0, gid: 0, mode: uint32(unix.S_IFREG | 0o757), links: 1},
		"not executable":  {uid: 0, gid: 0, mode: uint32(unix.S_IFREG | 0o644), links: 1},
		"setuid":          {uid: 0, gid: 0, mode: uint32(unix.S_IFREG | 0o4755), links: 1},
		"multiply linked": {uid: 0, gid: 0, mode: uint32(unix.S_IFREG | 0o755), links: 2},
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

	hook := linuxStatIdentity{uid: 1001, gid: 1002, mode: uint32(unix.S_IFREG | 0o755), links: 1}
	if err := validateLinuxGitHookMetadata(hook, 0o755, 1001, 1002); err != nil {
		t.Fatalf("valid A-owned hook: %v", err)
	}
	for name, changed := range map[string]linuxStatIdentity{
		"B-owned":         {uid: 1003, gid: 1002, mode: uint32(unix.S_IFREG | 0o755), links: 1},
		"multiply linked": {uid: 1001, gid: 1002, mode: uint32(unix.S_IFREG | 0o755), links: 2},
		"world writable":  {uid: 1001, gid: 1002, mode: uint32(unix.S_IFREG | 0o757), links: 1},
	} {
		t.Run(name, func(t *testing.T) {
			mode := os.FileMode(changed.mode & 0o7777)
			if err := validateLinuxGitHookMetadata(changed, mode, 1001, 1002); err == nil {
				t.Fatal("unsafe Git hook was accepted")
			}
		})
	}
	if err := validateLinuxGitHookMetadata(linuxStatIdentity{uid: 1001, gid: 1002, links: 2}, os.ModeDir|0o750, 1001, 1002); err != nil {
		t.Fatalf("directory link counts are not single-link file identity: %v", err)
	}
}

func TestLinuxTrustedAncestorsRequireRootOrAOwnership(t *testing.T) {
	for name, identity := range map[string]linuxStatIdentity{
		"root-owned mode 0555": {uid: 0, gid: 0, mode: uint32(unix.S_IFDIR | 0o555)},
		"A-owned mode 0555":    {uid: 1001, gid: 1002, mode: uint32(unix.S_IFDIR | 0o555)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateLinuxTrustedPathAncestor(identity, true, 1001); err != nil {
				t.Fatalf("trusted ancestor rejected: %v", err)
			}
		})
	}
	for name, identity := range map[string]linuxStatIdentity{
		"B-owned but currently read-only": {uid: 1003, gid: 1004, mode: uint32(unix.S_IFDIR | 0o555)},
		"third-party-owned":               {uid: 1005, gid: 1006, mode: uint32(unix.S_IFDIR | 0o555)},
		"not a directory":                 {uid: 0, gid: 0, mode: uint32(unix.S_IFREG | 0o555)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateLinuxTrustedPathAncestor(identity, identity.mode&unix.S_IFMT == unix.S_IFDIR, 1001); err == nil {
				t.Fatal("untrusted mutable path ancestor was accepted")
			}
		})
	}
}

func TestLinuxSystemdAncestorsRequireRootOwnership(t *testing.T) {
	if err := validateLinuxSystemdDirectoryIdentity(linuxStatIdentity{uid: 0, gid: 0}, true, 0o755); err != nil {
		t.Fatalf("root-owned systemd ancestor rejected: %v", err)
	}
	for name, identity := range map[string]linuxStatIdentity{
		"B-owned read-only": {uid: 1003, gid: 1004},
		"A-owned":           {uid: 1001, gid: 1002},
		"wrong root group":  {uid: 0, gid: 1004},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateLinuxSystemdDirectoryIdentity(identity, true, 0o555); err == nil {
				t.Fatal("replaceable systemd ancestor was accepted")
			}
		})
	}
	if err := validateLinuxSystemdDirectoryIdentity(linuxStatIdentity{uid: 0, gid: 0}, true, 0o775); err == nil {
		t.Fatal("group-writable root systemd ancestor was accepted")
	}
}

func TestLinuxAclWriterCheckUsesEffectiveMaskAndDedicatedGroup(t *testing.T) {
	identity := linuxStatIdentity{uid: 1001, gid: 1002}
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
	if err := validateEffectiveLinuxSystemdProperties(properties, policy, nil, linuxACLSubject{uid: 1001, gid: 1002}, linuxACLSubject{uid: 1003, gid: 1004}); err != nil {
		t.Fatalf("valid effective owner unit properties: %v", err)
	}
	humanized := cloneStringMap(properties)
	humanized["TimeoutStopUSec"] = "12min 0s"
	if err := validateEffectiveLinuxSystemdProperties(humanized, policy, nil, linuxACLSubject{uid: 1001, gid: 1002}, linuxACLSubject{uid: 1003, gid: 1004}); err != nil {
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
		"reverse dependency": func(values map[string]string) { values["Before"] = "symmetry-daemon.service" },
		"unknown drop-in": func(values map[string]string) {
			values["DropInPaths"] = "/run/systemd/system/symmetry-owner.service.d/override.conf"
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := cloneStringMap(properties)
			mutate(mutated)
			if err := validateEffectiveLinuxSystemdProperties(mutated, policy, nil, linuxACLSubject{uid: 1001, gid: 1002}, linuxACLSubject{uid: 1003, gid: 1004}); err == nil {
				t.Fatal("unsafe effective systemd state was accepted")
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
	permissions, err := permissionsFromLinuxACL(unsafe, linuxStatIdentity{uid: aUID, gid: aGID}, linuxACLSubject{uid: bUID, gid: bGID})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEffectiveLinuxPermissions(permissions, 0, 2, linuxACLSubject{uid: bUID, gid: bGID}); err == nil {
		t.Fatal("named-user B write ACL was accepted for protected host data")
	}

	readOnly := []linuxACLEntry{
		{tag: linuxACLUserObj, perm: 7, id: linuxACLNoID},
		{tag: linuxACLUser, perm: 5, id: bUID},
		{tag: linuxACLGroupObj, perm: 0, id: linuxACLNoID},
		{tag: linuxACLMask, perm: 5, id: linuxACLNoID},
		{tag: linuxACLOther, perm: 0, id: linuxACLNoID},
	}
	permissions, err = permissionsFromLinuxACL(readOnly, linuxStatIdentity{uid: aUID, gid: aGID}, linuxACLSubject{uid: bUID, gid: bGID})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEffectiveLinuxPermissions(permissions, 5, 2, linuxACLSubject{uid: bUID, gid: bGID}); err != nil {
		t.Fatalf("read/search-only B ACL was rejected: %v", err)
	}

	otherGroup := []linuxACLEntry{
		{tag: linuxACLUserObj, perm: 7, id: linuxACLNoID},
		{tag: linuxACLGroupObj, perm: 0, id: linuxACLNoID},
		{tag: linuxACLGroup, perm: 7, id: aGID},
		{tag: linuxACLMask, perm: 7, id: linuxACLNoID},
		{tag: linuxACLOther, perm: 0, id: linuxACLNoID},
	}
	permissions, err = permissionsFromLinuxACL(otherGroup, linuxStatIdentity{uid: aUID, gid: aGID}, linuxACLSubject{uid: bUID, gid: bGID, groups: []uint32{bGID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEffectiveLinuxPermissions(permissions, 5, 0, linuxACLSubject{uid: bUID, gid: bGID}); err == nil {
		t.Fatal("host supplementary group rights were assigned to the B user namespace")
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
