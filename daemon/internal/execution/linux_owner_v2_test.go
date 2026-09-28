//go:build linux && amd64

package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"golang.org/x/sys/unix"
)

type linuxV2FallbackTestContainment struct{}

func (linuxV2FallbackTestContainment) Terminate(bool) error { return nil }
func (linuxV2FallbackTestContainment) Close() error         { return nil }

func TestRunnerLinuxV2RequiredNeverFallsBackToDirectProcessStart(t *testing.T) {
	configured := false
	launched := false
	attached := false
	runner := Runner{
		configureProcess: func(*exec.Cmd) error {
			configured = true
			return nil
		},
		launchProcess: func(context.Context, *exec.Cmd, Invocation, *os.File, *os.File, *os.File) (*startedProcess, error) {
			launched = true
			return nil, errNativeProcessLauncherUnavailable
		},
		attachProcess: func(process *os.Process) (platform.Containment, string, error) {
			attached = true
			return linuxV2FallbackTestContainment{}, "legacy-process", nil
		},
	}
	process, err := runner.Start(context.Background(), Invocation{
		Program: "/bin/true", LinuxV2Required: true, LinuxV2: &LinuxV2Invocation{},
		InitialLeaseDeadlineAt: time.Now().Add(time.Minute), InitialLeaseSequence: 1,
	}, SinkFunc(func(context.Context, Event) error { return nil }))
	if process != nil {
		process.Wait()
	}
	if !errors.Is(err, errNativeProcessLauncherUnavailable) {
		t.Fatalf("Start() error = %v, want unavailable Linux v2 launcher", err)
	}
	if configured || attached || !launched {
		t.Fatalf("Linux v2 launch path configured=%t launched=%t attached=%t; want no legacy setup/attach and one v2 attempt", configured, launched, attached)
	}
}

func TestRunnerLinuxV2RequiredRejectsMissingGrantBeforeLaunch(t *testing.T) {
	configured := false
	launched := false
	runner := Runner{
		configureProcess: func(*exec.Cmd) error {
			configured = true
			return nil
		},
		launchProcess: func(context.Context, *exec.Cmd, Invocation, *os.File, *os.File, *os.File) (*startedProcess, error) {
			launched = true
			return nil, nil
		},
	}
	process, err := runner.Start(context.Background(), Invocation{
		Program: "/bin/true", LinuxV2Required: true,
	}, SinkFunc(func(context.Context, Event) error { return nil }))
	if process != nil || err == nil {
		t.Fatalf("Start() = (%v, %v), want a pre-launch missing-grant error", process, err)
	}
	if configured || launched {
		t.Fatalf("missing v2 grant had effects: configured=%t launched=%t", configured, launched)
	}
}

func TestDefaultLinuxLauncherDoesNotUseTestBinaryV1BypassForV2(t *testing.T) {
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process, err := NewRunner().Start(context.Background(), Invocation{
		Program: testBinary, Args: []string{"-test.run=^TestNoSuchV2FallbackTarget$"},
		LinuxV2Required: true, LinuxV2: &LinuxV2Invocation{},
		InitialLeaseDeadlineAt: time.Now().Add(time.Minute), InitialLeaseSequence: 1,
		PrepareSupervisorHandoff: func(authority.SupervisorHandoff) error { return nil },
	}, SinkFunc(func(context.Context, Event) error { return nil }))
	if process != nil {
		process.Wait()
		t.Fatal("default Linux v2 launch fell through to a process after its invalid v2 grant")
	}
	if err == nil {
		t.Fatal("default Linux v2 launch accepted an incomplete grant")
	}
}

func TestLinuxV2RejectsMissingCloneGrantBeforeReservation(t *testing.T) {
	callbacks := LinuxV2JournalCallbacks{
		Reserve:            func(state.LinuxCgroupReservation) error { return nil },
		MarkCreateStarted:  func(state.LinuxCgroupReservation) error { return nil },
		BindLeaf:           func(state.LinuxCgroupReservation, authority.LinuxCgroupIdentity) error { return nil },
		BindHelper:         func(state.LinuxCgroupReservation, authority.LinuxCgroupProcessIdentity) error { return nil },
		BindLauncher:       func(state.LinuxCgroupReservation, authority.LinuxCgroupProcessIdentity) error { return nil },
		ArmLauncherGrant:   func(state.LinuxCgroupReservation) error { return nil },
		RecordLauncherExit: func(state.LinuxCgroupReservation, authority.LinuxCgroupExitProof) error { return nil },
		RecordHelperExit:   func(state.LinuxCgroupReservation, authority.LinuxCgroupExitProof) error { return nil },
		BindGate:           func(state.LinuxCgroupReservation, authority.LinuxCgroupGateIdentity) error { return nil },
		PrepareHandoff:     func(authority.SupervisorHandoff) error { return nil },
		BindHandoffV2: func(authority.SupervisorHandoff, authority.LinuxCgroupProcessIdentity, authority.LinuxCgroupProcessIdentity) error {
			return nil
		},
		CommitHandoff:            func(authority.SupervisorHandoff, time.Time) error { return nil },
		RecordStopReceipt:        func(authority.SupervisorHandoff, authority.StopReceipt) error { return nil },
		RecordPreGateStopReceipt: func(state.LinuxCgroupReservation, authority.LinuxCgroupPreGateStopReceipt) error { return nil },
		AbortReservation:         func(state.LinuxCgroupReservation, authority.LinuxCgroupReservationAbortProof) error { return nil },
		ClearHandoffV2:           func(authority.SupervisorHandoff, platform.LinuxCgroupV2Release) error { return nil },
		ClearProcessDetailsV2:    func(int, string, platform.LinuxCgroupV2Release) error { return nil },
		ClearReservationV2:       func(state.LinuxCgroupReservation, platform.LinuxCgroupV2Release) error { return nil },
	}
	grant := &LinuxV2Invocation{
		RunKey: state.RunKey{RunID: "run-1", Generation: 1},
		Fence:  protocol.Fence{Generation: 1, RuntimeID: "runtime", RuntimeEpoch: 1, ClaimID: "claim", LeaseToken: "lease"},
		Parent: new(platform.LinuxCgroupV2Parent), Anchor: new(platform.LinuxOwnerAnchorClient),
		RecoveryEndpoint: "/run/symmetry/owner.sock", Callbacks: callbacks,
	}
	started, err := launchLinuxV2Process(context.Background(), exec.Command("/bin/true"), Invocation{
		LinuxV2Required: true, LinuxV2: grant,
	}, nil, nil, nil)
	if started != nil || err == nil || !strings.Contains(err.Error(), "clone grant") {
		t.Fatalf("launch without clone grant = (%v, %v), want pre-reservation clone error", started, err)
	}
}

func TestLinuxV2HandoffCarriesReservationSecretAndRecoveryEndpoint(t *testing.T) {
	parent := authority.LinuxCgroupParentIdentity{
		BootID: "11111111-1111-4111-8111-111111111111", MountID: 1, Device: 2, Inode: 3,
		Path: "/sys/fs/cgroup/symmetry",
	}
	leafName := strings.Repeat("e", authority.TokenBytes*2)
	leaf := authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: parent, LeafName: leafName,
		LeafDevice: 4, LeafInode: 5,
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: 100, Identity: "anchor:100"}
	helper := authority.LinuxCgroupProcessIdentity{PID: 101, Identity: "helper:101"}
	launcher := authority.LinuxCgroupProcessIdentity{PID: 102, Identity: "launcher:102"}
	mirror := authority.LinuxCgroupProcessIdentity{PID: 103, Identity: "mirror:103"}
	launcherExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: launcher, Witness: helper, Waited: true, Reaped: true,
	}
	gate := authority.LinuxCgroupGateIdentity{
		Process: authority.LinuxCgroupProcessIdentity{PID: 104, Identity: "gate:104"}, HostUID: 1000,
		ObservedAt: time.Now(), BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate,
		Cgroup: leaf, BornInLeaf: true, TrustedImage: true,
	}
	reservation := state.LinuxCgroupReservation{
		Version:     state.LinuxCgroupReservationVersion,
		LaunchToken: strings.Repeat("a", authority.TokenBytes*2), Secret: strings.Repeat("b", authority.SecretBytes*2),
		PipeToken: strings.Repeat("c", authority.TokenBytes*2), JobID: strings.Repeat("d", authority.TokenBytes*2),
		OwnerUID: 1000, RecoveryEndpoint: "/run/symmetry/owner.sock", Parent: parent, LeafName: leafName,
		CreateStarted: true, LauncherGrantPossible: true, LeafIdentity: &leaf, Anchor: anchor,
		Helper: &helper, Launcher: &launcher, LauncherExit: &launcherExit, Gate: &gate, Mirror: &mirror,
	}
	var prepared authority.SupervisorHandoff
	owner := &linuxV2ProcessOwner{
		invocation: &LinuxV2Invocation{Callbacks: LinuxV2JournalCallbacks{
			PrepareHandoff: func(value authority.SupervisorHandoff) error {
				prepared = value.Clone()
				return nil
			},
			BindHandoffV2: func(authority.SupervisorHandoff, authority.LinuxCgroupProcessIdentity, authority.LinuxCgroupProcessIdentity) error {
				return nil
			},
		}},
		reservation: reservation, gate: &gate, gateBound: true,
	}
	if err := owner.bindHandoff(); err != nil {
		t.Fatalf("bindHandoff() error = %v", err)
	}
	if prepared.Secret != reservation.Secret || prepared.OwnerContext != reservation.RecoveryEndpoint {
		t.Fatalf("handoff reservation binding mismatch: SecretMatch=%t OwnerContext=%q", prepared.Secret == reservation.Secret, prepared.OwnerContext)
	}
}

func TestNoPrepareHelperExitLostAckIsReadBackBeforeLeafAbort(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	journal := state.RunJournal{
		RunID: "run-no-prepare-abort", Generation: 1,
		RuntimeKey: "local", RuntimeID: "runtime-1", ClaimedRuntimeEpoch: 3,
		ClaimID: "claim-1", LeaseToken: "lease-1", LeaseExpiresAt: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		LocalState: "running", Work: protocol.Work{
			Goal: "implement", AgentProfile: "codex", Workspace: "isolated", Input: json.RawMessage(`{}`),
		},
		WorkspacePath: "/workspace/run", WorkspaceBindingKey: "binding-1", LastEventSequence: 1,
	}
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	loadJournal := func() state.RunJournal {
		t.Helper()
		loaded, err := store.LoadJournal(key)
		if err != nil {
			t.Fatal(err)
		}
		return loaded
	}
	parent := authority.LinuxCgroupParentIdentity{
		BootID: "00000000-0000-4000-8000-000000000001", MountID: 42, Device: 84, Inode: 126,
		Path: "/system.slice/symmetry.slice/delegated",
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: 10, Identity: "anchor:10:start"}
	mirror := authority.LinuxCgroupProcessIdentity{PID: 50, Identity: "daemon:50:start"}
	reservation := state.LinuxCgroupReservation{
		Version:     state.LinuxCgroupReservationVersion,
		LaunchToken: strings.Repeat("a", authority.TokenBytes*2), Secret: strings.Repeat("b", authority.SecretBytes*2),
		PipeToken: strings.Repeat("c", authority.TokenBytes*2), JobID: strings.Repeat("d", authority.TokenBytes*2),
		OwnerUID: 1000, LauncherUID: 0, RecoveryEndpoint: "/run/symmetry/recovery.sock",
		Parent: parent, LeafName: strings.Repeat("e", authority.TokenBytes*2), Anchor: anchor, Mirror: &mirror,
	}
	if _, err := store.ReserveLinuxCgroup(key, reservation); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLinuxCgroupCreateStarted(key, reservation); err != nil {
		t.Fatal(err)
	}
	reservation = loadJournal().ContainmentCgroup.Clone()
	leaf := authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: parent, LeafName: reservation.LeafName,
		LeafDevice: 168, LeafInode: 210,
	}
	if _, err := store.BindLinuxCgroupLeaf(key, reservation, leaf); err != nil {
		t.Fatal(err)
	}
	reservation = loadJournal().ContainmentCgroup.Clone()
	helper := authority.LinuxCgroupProcessIdentity{PID: 20, Identity: "helper:20:start"}
	if _, err := store.BindLinuxCgroupHelper(key, reservation, helper); err != nil {
		t.Fatal(err)
	}
	reservation = loadJournal().ContainmentCgroup.Clone()
	helperExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: anchor, Waited: true, Reaped: true,
	}
	abortHelperExit := helperExit
	abortProof := authority.LinuxCgroupReservationAbortProof{
		Version: authority.LinuxCgroupAbortProofVersion, LaunchToken: reservation.LaunchToken,
		Parent: parent, LeafName: reservation.LeafName, CreateStarted: true,
		LeafIdentity: &leaf, LeafDisposition: authority.LinuxCgroupAbortLeafRemovedEmpty,
		OwnerUID: reservation.OwnerUID, Anchor: anchor, Mirror: mirror, Helper: &helper,
		HelperExit: &abortHelperExit, Witness: anchor, CreatorFenced: true, NoProducerCanAct: true,
		ParentUnchanged: true, LeafEmpty: true, LeafAbsent: true, RmdirSucceeded: true,
	}
	if err := abortProof.Validate(); err != nil {
		t.Fatalf("test abort proof is invalid: %v", err)
	}
	if _, err := store.AbortLinuxCgroupReservation(key, reservation, abortProof); err == nil {
		t.Fatal("State accepted no-Prepare abort before the exact helper-exit proof was durable")
	}

	writes := 0
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if writes == 1 {
			return errors.New("simulated lost ACK after durable helper-exit write")
		}
		return nil
	})
	defer restoreWriter()

	var removeObservedHelperExit bool
	callbacks := LinuxV2JournalCallbacks{
		RecordHelperExit: func(expected state.LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) error {
			if expected.HelperExit != nil || !proof.Equal(helperExit) {
				return errors.New("helper-exit replay changed the exact base reservation or proof")
			}
			if _, err := store.RecordLinuxCgroupHelperExit(key, expected, proof); err != nil {
				return err
			}
			latest, err := store.LoadJournal(key)
			if err != nil || latest.ContainmentCgroup == nil || latest.ContainmentCgroup.HelperExit == nil ||
				!latest.ContainmentCgroup.HelperExit.Equal(helperExit) {
				return errors.Join(errors.New("exact helper-exit proof did not read back"), err)
			}
			return nil
		},
		AbortReservation: func(expected state.LinuxCgroupReservation, proof authority.LinuxCgroupReservationAbortProof) error {
			if expected.HelperExit == nil || !expected.HelperExit.Equal(helperExit) {
				return errors.New("abort CAS ran before the exact helper-exit proof was durable")
			}
			_, err := store.AbortLinuxCgroupReservation(key, expected, proof)
			return err
		},
	}
	owner := &linuxV2ProcessOwner{
		reservation: reservation,
		invocation:  &LinuxV2Invocation{Callbacks: callbacks},
	}
	updated, measured, err := owner.abortNoPrepareReservationAfterHelperExit(
		context.Background(), reservation, helperExit, nil,
		func(_ context.Context, expected state.LinuxCgroupReservation, exactExit authority.LinuxCgroupExitProof) (authority.LinuxCgroupReservationAbortProof, error) {
			latest := loadJournal()
			if latest.ContainmentCgroup == nil || latest.ContainmentCgroup.HelperExit == nil ||
				!latest.ContainmentCgroup.HelperExit.Equal(exactExit) || writes < 2 {
				return authority.LinuxCgroupReservationAbortProof{}, errors.New("leaf removal preceded helper-exit readback")
			}
			removeObservedHelperExit = true
			expectedAfterHelperExit := reservation.Clone()
			helperExitCopy := exactExit
			expectedAfterHelperExit.HelperExit = &helperExitCopy
			if !expected.Equal(expectedAfterHelperExit) {
				return authority.LinuxCgroupReservationAbortProof{}, errors.New("leaf removal received a different helper-exit reservation")
			}
			return abortProof, nil
		},
	)
	if err != nil {
		t.Fatalf("no-Prepare abort after lost helper-exit ACK: %v", err)
	}
	latest := loadJournal()
	if !removeObservedHelperExit || writes < 3 || updated.HelperExit == nil || !updated.HelperExit.Equal(helperExit) ||
		latest.ContainmentCgroup == nil || latest.ContainmentCgroup.HelperExit == nil ||
		!latest.ContainmentCgroup.HelperExit.Equal(helperExit) || latest.ContainmentCgroup.AbortProof == nil ||
		!latest.ContainmentCgroup.AbortProof.Equal(measured) {
		t.Fatalf("helper-exit abort order/readback mismatch: writes=%d observedBeforeRemoval=%t reservation=%#v cgroup=%#v",
			writes, removeObservedHelperExit, updated, latest.ContainmentCgroup)
	}
}

func TestRunnerLinuxV2RequiredRequiresClaimLeaseBeforeLaunch(t *testing.T) {
	launched := false
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(context.Context, *exec.Cmd, Invocation, *os.File, *os.File, *os.File) (*startedProcess, error) {
			launched = true
			return nil, nil
		},
	}
	process, err := runner.Start(context.Background(), Invocation{
		Program: "/bin/true", LinuxV2Required: true, LinuxV2: &LinuxV2Invocation{},
	}, SinkFunc(func(context.Context, Event) error { return nil }))
	if process != nil || err == nil {
		t.Fatalf("Start() = (%v, %v), want a pre-launch claim-lease error", process, err)
	}
	if launched {
		t.Fatal("Linux v2 launcher ran without the claim-derived initial lease and sequence 1")
	}
}

func TestLinuxV2ChildEnvironmentRequiresDistinctAbsoluteBHome(t *testing.T) {
	t.Setenv("HOME", "/home/owner-a")
	for _, test := range []struct {
		name string
		env  []string
		want bool
	}{
		{name: "B home", env: []string{"HOME=/home/owner-b", "XDG_CONFIG_HOME=/home/owner-b/.config"}, want: true},
		{name: "B XDG subdirectory", env: []string{"HOME=/home/owner-b", "XDG_CACHE_HOME=/home/owner-b/.cache"}, want: true},
		{name: "missing", env: []string{"PATH=/usr/bin:/bin"}},
		{name: "A home", env: []string{"HOME=/home/owner-a"}},
		{name: "relative home", env: []string{"HOME=owner-b"}},
		{name: "relative XDG", env: []string{"HOME=/home/owner-b", "XDG_DATA_HOME=owner-b/.local/share"}},
		{name: "A XDG", env: []string{"HOME=/home/owner-b", "XDG_CONFIG_HOME=/home/owner-a/.config"}},
		{name: "XDG traversal", env: []string{"HOME=/home/owner-b", "XDG_STATE_HOME=/home/owner-b/../owner-a/.local/state"}},
		{name: "duplicate HOME", env: []string{"HOME=/home/owner-b", "HOME=/home/owner-a"}},
		{name: "duplicate XDG", env: []string{"HOME=/home/owner-b", "XDG_CONFIG_HOME=/home/owner-b/.config", "XDG_CONFIG_HOME=/home/owner-a/.config"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateLinuxV2ChildEnvironment(test.env)
			if (err == nil) != test.want {
				t.Fatalf("validateLinuxV2ChildEnvironment() error = %v, want success=%t", err, test.want)
			}
		})
	}
}

func TestLinuxV2PeerRejectsNonzeroButUnverifiedNamespace(t *testing.T) {
	pidfd, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer pidfd.Close()
	owner := &linuxV2ProcessOwner{
		prepared: &platform.LinuxOwnerHelperV2Prepared{
			PIDFD: pidfd, UserNamespace: platform.LinuxUserNamespaceIdentity{Device: 1, Inode: 2},
		},
	}
	peer, err := owner.duplicatePeer()
	if peer != nil || !errors.Is(err, errors.ErrUnsupported) {
		if peer != nil {
			_ = peer.Close()
		}
		t.Fatalf("duplicatePeer() = (%v, %v), want unsupported without root-cross-checked nsfs identity", peer, err)
	}
}

func TestLinuxCloneAbortCallbackWaitsForExactRetirement(t *testing.T) {
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: "run-1", Generation: 3, BindingKey: "primary",
		ResourceID: "00000000-0000-4000-8000-000000000001", BaseCommit: strings.Repeat("a", 40),
		BaseTreeDigest: "sha256:" + strings.Repeat("b", 64), Nonce: strings.Repeat("c", authority.LinuxCloneNonceBytes*2),
		SeedPath: "/srv/symmetry/seeds/run-1.bundle", Target: "/srv/symmetry/work/run-1",
	}
	grant := authority.LinuxCloneGrant{
		Intent: intent,
		Seed: authority.LinuxCloneSeedReceipt{
			Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
			Digest: "sha256:" + strings.Repeat("d", 64), Size: 128, Device: 7, Inode: 11,
		},
	}
	if err := grant.Validate(); err != nil {
		t.Fatalf("test clone grant is invalid: %v", err)
	}
	parent := authority.LinuxCgroupParentIdentity{
		BootID: "11111111-1111-4111-8111-111111111111", MountID: 1, Device: 2, Inode: 3,
		Path: "/sys/fs/cgroup/symmetry",
	}
	leafName := strings.Repeat("e", 32)
	leafIdentity := authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: parent, LeafName: leafName,
		LeafDevice: 4, LeafInode: 5,
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: 100, Identity: "anchor:100"}
	mirror := authority.LinuxCgroupProcessIdentity{PID: 101, Identity: "mirror:101"}
	abortProof := authority.LinuxCgroupReservationAbortProof{
		Version: authority.LinuxCgroupAbortProofVersion, LaunchToken: strings.Repeat("f", authority.TokenBytes*2),
		Parent: parent, LeafName: leafName, CreateStarted: true, LeafIdentity: &leafIdentity,
		LeafDisposition: authority.LinuxCgroupAbortLeafRemovedEmpty, OwnerUID: 1000,
		Anchor: anchor, Mirror: mirror, Witness: anchor, CreatorFenced: true, NoProducerCanAct: true,
		ParentUnchanged: true, LeafEmpty: true, LeafAbsent: true, RmdirSucceeded: true,
	}
	if err := abortProof.Validate(); err != nil {
		t.Fatalf("test cgroup abort proof is invalid: %v", err)
	}
	seed := grant.Seed.Clone()
	reservation := state.LinuxCgroupReservation{
		LaunchToken: abortProof.LaunchToken, Parent: parent, LeafName: leafName, CreateStarted: true,
		LeafIdentity: &leafIdentity, OwnerUID: 1000, Anchor: anchor, Mirror: &mirror,
		AbortProof: &abortProof,
	}
	var calls int
	owner := &linuxV2ProcessOwner{
		invocation: &LinuxV2Invocation{Callbacks: LinuxV2JournalCallbacks{
			FinalizeNoCheckoutLinuxCloneAfterRetirement: func(
				_ context.Context,
				actualIntent authority.LinuxCloneIntent,
				actualSeed *authority.LinuxCloneSeedReceipt,
				preGate *authority.LinuxCgroupPreGateReleaseProof,
				actualAbort *authority.LinuxCgroupReservationAbortProof,
			) error {
				calls++
				if !actualIntent.Equal(intent) || actualSeed == nil || !actualSeed.Equal(seed) || preGate != nil ||
					actualAbort == nil || !actualAbort.Equal(abortProof) {
					return errors.New("callback received a different clone abort prerequisite")
				}
				return nil
			},
		}},
		cloneGrant: &grant, reservation: reservation,
		helperReleased: true,
	}
	if err := owner.finalizeNoCheckoutCloneAfterRetirement(context.Background(), nil, &abortProof); err == nil || calls != 0 {
		t.Fatalf("clone abort ran before anchor retirement: err=%v callbackCalls=%d", err, calls)
	}
	owner.mu.Lock()
	owner.anchorRetired = true
	owner.mu.Unlock()
	wrongProof := abortProof.Clone()
	wrongProof.Witness.PID++
	if err := owner.finalizeNoCheckoutCloneAfterRetirement(context.Background(), nil, &wrongProof); err == nil || calls != 0 {
		t.Fatalf("clone abort accepted a mismatched retirement proof: err=%v callbackCalls=%d", err, calls)
	}
	if err := owner.finalizeNoCheckoutCloneAfterRetirement(context.Background(), nil, &abortProof); err != nil {
		t.Fatalf("finalize exact no-checkout clone abort: %v", err)
	}
	if calls != 1 {
		t.Fatalf("clone abort callback calls = %d, want one after exact retirement", calls)
	}
	if err := owner.finalizeNoCheckoutCloneAfterRetirement(context.Background(), nil, &abortProof); err != nil || calls != 1 {
		t.Fatalf("exact clone abort replay = %v with %d callback calls, want idempotent local completion", err, calls)
	}
}

func TestLinuxCloneStopRunsWhileOutputDrainsAndPromotionWaitsForResult(t *testing.T) {
	var commitAcknowledged atomic.Bool
	var leaseAcknowledged atomic.Bool
	var stopStarted atomic.Bool
	var stopFinished atomic.Bool
	var finalizerSawOutput atomic.Bool
	var delivered atomic.Bool
	var outputMu sync.Mutex
	var output bytes.Buffer
	var deliveredOnce sync.Once
	var stopBeganOnce sync.Once
	stopBegan := make(chan struct{})
	outputDelivered := make(chan struct{})
	finalizerCalled := make(chan struct{})
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(_ context.Context, _ *exec.Cmd, _ Invocation, _ *os.File, stdoutWrite, _ *os.File) (*startedProcess, error) {
			return &startedProcess{
				pid: 42, identity: "linux:v2:gate",
				containmentHandoff: &authority.SupervisorHandoff{Version: authority.SupervisorHandoffVersionV2},
				wait:               func() (int, error) { return 0, nil },
				commitSupervisorHandoff: func(authority.SupervisorHandoff, time.Time) error {
					commitAcknowledged.Store(true)
					return nil
				},
				renewLinuxV2Lease: func(context.Context, time.Duration, uint64) error {
					if !commitAcknowledged.Load() {
						return errors.New("lease armed before durable commit")
					}
					leaseAcknowledged.Store(true)
					return nil
				},
				resume: func() error {
					if !leaseAcknowledged.Load() {
						return errors.New("native RELEASE preceded the initial lease ACK")
					}
					_, err := stdoutWrite.Write([]byte("candidate-result"))
					return err
				},
				stopLinuxV2BeforeOutputDrain: func() error {
					stopStarted.Store(true)
					stopBeganOnce.Do(func() { close(stopBegan) })
					<-outputDelivered
					stopFinished.Store(true)
					return nil
				},
				deferLinuxV2FinalizeUntilOutputDrain: true,
				closeLinuxV2Containment: func() error {
					finalizerSawOutput.Store(stopFinished.Load() && delivered.Load())
					close(finalizerCalled)
					return nil
				},
			}, nil
		},
	}
	process, err := runner.Start(context.Background(), Invocation{
		Program: "/bin/true", LinuxV2Required: true, LinuxV2: &LinuxV2Invocation{},
		InitialLeaseDeadlineAt: time.Now().Add(time.Minute), InitialLeaseSequence: 1,
	}, SinkFunc(func(_ context.Context, event Event) error {
		if event.Stream == Stdout {
			<-stopBegan
			outputMu.Lock()
			_, _ = output.Write(event.Data)
			seen := bytes.Contains(output.Bytes(), []byte("candidate-result"))
			outputMu.Unlock()
			if seen {
				deliveredOnce.Do(func() {
					delivered.Store(true)
					close(outputDelivered)
				})
			}
		}
		return nil
	}))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	result := process.Wait()
	select {
	case <-finalizerCalled:
	default:
		t.Fatal("clone finalizer did not run")
	}
	if !stopStarted.Load() || !stopFinished.Load() || !delivered.Load() || !finalizerSawOutput.Load() {
		t.Fatalf("clone ordering: commit=%t lease=%t stop-start=%t stop-finished=%t output=%t finalizer-after-output=%t, result=%#v",
			commitAcknowledged.Load(), leaseAcknowledged.Load(), stopStarted.Load(), stopFinished.Load(), delivered.Load(), finalizerSawOutput.Load(), result)
	}
	if !result.Success() {
		t.Fatalf("result = %#v, want a clean staged clone lifecycle", result)
	}
}

func TestLinuxCloneStopFailureDoesNotWaitForDetachedPipeWriter(t *testing.T) {
	var writerHold *os.File
	var finalizerCalled atomic.Bool
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(_ context.Context, _ *exec.Cmd, _ Invocation, _ *os.File, stdoutWrite, _ *os.File) (*startedProcess, error) {
			fd, err := unix.FcntlInt(stdoutWrite.Fd(), unix.F_DUPFD_CLOEXEC, 0)
			if err != nil {
				return nil, err
			}
			writerHold = os.NewFile(uintptr(fd), "detached-linux-v2-pipe-writer")
			if writerHold == nil {
				_ = unix.Close(fd)
				return nil, errors.New("wrap detached pipe writer")
			}
			return &startedProcess{
				pid: 42, identity: "linux:v2:gate",
				wait:              func() (int, error) { return 0, nil },
				renewLinuxV2Lease: func(context.Context, time.Duration, uint64) error { return nil },
				resume: func() error {
					_, err := stdoutWrite.Write([]byte("candidate-result"))
					return err
				},
				stopLinuxV2BeforeOutputDrain:         func() error { return errors.New("helper stop remains unproven") },
				deferLinuxV2FinalizeUntilOutputDrain: true,
				closeLinuxV2Containment: func() error {
					finalizerCalled.Store(true)
					return nil
				},
			}, nil
		},
	}
	process, err := runner.Start(context.Background(), Invocation{
		Program: "/bin/true", LinuxV2Required: true, LinuxV2: &LinuxV2Invocation{},
		InitialLeaseDeadlineAt: time.Now().Add(time.Minute), InitialLeaseSequence: 1,
	}, SinkFunc(func(context.Context, Event) error { return nil }))
	if err != nil {
		if writerHold != nil {
			_ = writerHold.Close()
		}
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if writerHold != nil {
			_ = writerHold.Close()
		}
	}()
	waitDone := make(chan Result, 1)
	go func() { waitDone <- process.Wait() }()
	var result Result
	select {
	case result = <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Process.Wait() blocked on a detached writer after an unproven cgroup stop")
	}
	if result.ContainmentError == nil || !result.OutputTruncated {
		t.Fatalf("result = %#v, want unresolved containment and explicitly truncated output", result)
	}
	if finalizerCalled.Load() {
		t.Fatal("unproven early stop path finalized/released the exact Linux owner")
	}
	if err := process.FinalizeContainment(); err != nil {
		t.Fatalf("retry exact containment finalization while pipe writer remains open: %v", err)
	}
	if !finalizerCalled.Load() {
		t.Fatal("retryable physical-stop owner was not retained after Wait returned")
	}
	select {
	case <-process.readersDone:
	default:
		t.Fatal("Runner-owned pipe readers were not closed after unproven stop")
	}
	select {
	case <-process.deliveryDone:
	default:
		t.Fatal("output delivery was not cancelled after unproven stop")
	}
}

func TestLinuxCloneSuccessfulStopBoundsDetachedPipeDrain(t *testing.T) {
	var writerHold *os.File
	var outcome LinuxCloneRunOutcome
	var finalizerCalled atomic.Bool
	runner := Runner{
		outputDrainTimeout: 20 * time.Millisecond,
		configureProcess:   func(*exec.Cmd) error { return nil },
		launchProcess: func(_ context.Context, _ *exec.Cmd, _ Invocation, _ *os.File, stdoutWrite, _ *os.File) (*startedProcess, error) {
			fd, err := unix.FcntlInt(stdoutWrite.Fd(), unix.F_DUPFD_CLOEXEC, 0)
			if err != nil {
				return nil, err
			}
			writerHold = os.NewFile(uintptr(fd), "detached-linux-v2-success-writer")
			if writerHold == nil {
				_ = unix.Close(fd)
				return nil, errors.New("wrap detached pipe writer")
			}
			return &startedProcess{
				pid: 42, identity: "linux:v2:gate",
				wait:              func() (int, error) { return 0, nil },
				renewLinuxV2Lease: func(context.Context, time.Duration, uint64) error { return nil },
				resume: func() error {
					_, err := stdoutWrite.Write([]byte("candidate-result"))
					return err
				},
				stopLinuxV2BeforeOutputDrain:         func() error { return nil },
				deferLinuxV2FinalizeUntilOutputDrain: true,
				recordLinuxV2CloneOutcome: func(value LinuxCloneRunOutcome) {
					outcome = value
					outcome.StopReceiptValid = true
					outcome.LeafRemovalProven = true
				},
				closeLinuxV2Containment: func() error {
					if !outcome.OutputTruncated || outcome.PromotionEligible() {
						return errors.New("clone with an external pipe writer was eligible for promotion")
					}
					finalizerCalled.Store(true)
					return nil
				},
			}, nil
		},
	}
	process, err := runner.Start(context.Background(), Invocation{
		Program: "/bin/true", LinuxV2Required: true, LinuxV2: &LinuxV2Invocation{},
		InitialLeaseDeadlineAt: time.Now().Add(time.Minute), InitialLeaseSequence: 1,
	}, SinkFunc(func(context.Context, Event) error { return nil }))
	if err != nil {
		if writerHold != nil {
			_ = writerHold.Close()
		}
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if writerHold != nil {
			_ = writerHold.Close()
		}
	}()
	result := process.Wait()
	if !result.OutputTruncated || result.Success() || !finalizerCalled.Load() || !outcome.StopReceiptValid || !outcome.LeafRemovalProven {
		t.Fatalf("result=%#v finalizerCalled=%t outcome=%#v", result, finalizerCalled.Load(), outcome)
	}
	select {
	case <-process.readersDone:
	default:
		t.Fatal("successful-stop drain timeout did not reap Runner-owned readers")
	}
	select {
	case <-process.deliveryDone:
	default:
		t.Fatal("successful-stop drain timeout did not finish output delivery")
	}
}

func TestLinuxCloneOutcomeIsObservedBeforePromotionFinalizer(t *testing.T) {
	var outcome *LinuxCloneRunOutcome
	var outputDelivered atomic.Bool
	var finalizerEligible atomic.Bool
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(_ context.Context, _ *exec.Cmd, _ Invocation, _ *os.File, stdoutWrite, _ *os.File) (*startedProcess, error) {
			return &startedProcess{
				pid: 42, identity: "linux:v2:gate",
				wait:              func() (int, error) { return 7, errors.New("native exited nonzero") },
				renewLinuxV2Lease: func(context.Context, time.Duration, uint64) error { return nil },
				resume: func() error {
					_, err := stdoutWrite.Write([]byte("candidate TaskResult already delivered"))
					return err
				},
				stopLinuxV2BeforeOutputDrain:         func() error { return nil },
				deferLinuxV2FinalizeUntilOutputDrain: true,
				recordLinuxV2CloneOutcome: func(value LinuxCloneRunOutcome) {
					copy := value
					outcome = &copy
				},
				closeLinuxV2Containment: func() error {
					if outcome == nil || !outputDelivered.Load() || outcome.ExitCode != 7 ||
						outcome.WaitError == nil || outcome.PromotionEligible() {
						return errors.New("clone finalizer lacked the drained-output backend failure outcome")
					}
					finalizerEligible.Store(outcome.PromotionEligible())
					return nil
				},
			}, nil
		},
	}
	process, err := runner.Start(context.Background(), Invocation{
		Program: "/bin/true", LinuxV2Required: true, LinuxV2: &LinuxV2Invocation{},
		InitialLeaseDeadlineAt: time.Now().Add(time.Minute), InitialLeaseSequence: 1,
	}, SinkFunc(func(_ context.Context, event Event) error {
		if event.Stream == Stdout && bytes.Contains(event.Data, []byte("candidate TaskResult")) {
			outputDelivered.Store(true)
		}
		return nil
	}))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	result := process.Wait()
	if result.ExitCode != 7 || result.WaitError == nil || !outputDelivered.Load() || finalizerEligible.Load() {
		t.Fatalf("result=%#v outputDelivered=%t finalizerPromotionEligible=%t outcome=%#v",
			result, outputDelivered.Load(), finalizerEligible.Load(), outcome)
	}
}

func TestLinuxCloneCancellationInvalidatesCandidateBeforeFinalizer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitRelease := make(chan struct{})
	terminationObserved := make(chan struct{}, 1)
	var outcome *LinuxCloneRunOutcome
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(_ context.Context, _ *exec.Cmd, _ Invocation, _ *os.File, _ *os.File, _ *os.File) (*startedProcess, error) {
			return &startedProcess{
				pid: 42, identity: "linux:v2:gate",
				wait: func() (int, error) {
					<-waitRelease
					return 0, nil
				},
				renewLinuxV2Lease:                    func(context.Context, time.Duration, uint64) error { return nil },
				stopLinuxV2BeforeOutputDrain:         func() error { return nil },
				deferLinuxV2FinalizeUntilOutputDrain: true,
				recordLinuxV2CloneOutcome: func(value LinuxCloneRunOutcome) {
					copy := value
					outcome = &copy
					// Termination is recorded before the configured grace elapses;
					// the fake hard-kill backend is intentionally not the barrier.
					if value.Terminated {
						select {
						case terminationObserved <- struct{}{}:
						default:
						}
					}
				},
				closeLinuxV2Containment: func() error {
					if outcome == nil || !outcome.Terminated || !outcome.RunContextCancelled || outcome.PromotionEligible() {
						return errors.New("clone finalizer accepted a cancelled run outcome")
					}
					return nil
				},
			}, nil
		},
	}
	process, err := runner.Start(ctx, Invocation{
		Program: "/bin/true", LinuxV2Required: true, LinuxV2: &LinuxV2Invocation{},
		InitialLeaseDeadlineAt: time.Now().Add(time.Minute), InitialLeaseSequence: 1,
	}, SinkFunc(func(context.Context, Event) error { return nil }))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cancel()
	select {
	case <-terminationObserved:
	case <-time.After(time.Second):
		t.Fatal("run-context cancellation was not captured in the clone outcome")
	}
	close(waitRelease)
	result := process.Wait()
	if !result.Terminated || result.Success() || outcome == nil || !outcome.RunContextCancelled || outcome.PromotionEligible() {
		t.Fatalf("result=%#v outcome=%#v", result, outcome)
	}
}

func TestLinuxCloneTerminateDeadlineDuringFinalizerDoesNotBlock(t *testing.T) {
	finalizerStarted := make(chan struct{})
	releaseFinalizer := make(chan struct{})
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		launchProcess: func(_ context.Context, _ *exec.Cmd, _ Invocation, _ *os.File, _ *os.File, _ *os.File) (*startedProcess, error) {
			return &startedProcess{
				pid: 42, identity: "linux:v2:gate",
				wait:                                 func() (int, error) { return 0, nil },
				renewLinuxV2Lease:                    func(context.Context, time.Duration, uint64) error { return nil },
				stopLinuxV2BeforeOutputDrain:         func() error { return nil },
				deferLinuxV2FinalizeUntilOutputDrain: true,
				closeLinuxV2Containment: func() error {
					close(finalizerStarted)
					<-releaseFinalizer
					return nil
				},
			}, nil
		},
	}
	process, err := runner.Start(context.Background(), Invocation{
		Program: "/bin/true", LinuxV2Required: true, LinuxV2: &LinuxV2Invocation{},
		InitialLeaseDeadlineAt: time.Now().Add(time.Minute), InitialLeaseSequence: 1,
	}, SinkFunc(func(context.Context, Event) error { return nil }))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitDone := make(chan Result, 1)
	go func() { waitDone <- process.Wait() }()
	select {
	case <-finalizerStarted:
	case <-time.After(time.Second):
		t.Fatal("clone finalizer did not reach its terminal decision")
	}
	terminationContext, cancelTermination := context.WithTimeout(context.Background(), 20*time.Millisecond)
	terminationErr := process.Terminate(terminationContext, 0)
	cancelTermination()
	if !errors.Is(terminationErr, context.DeadlineExceeded) {
		t.Fatalf("Terminate() during clone finalization = %v, want the caller's deadline", terminationErr)
	}
	close(releaseFinalizer)
	result := <-waitDone
	if result.Terminated || !result.Success() {
		t.Fatalf("deadline-limited Terminate() reclassified the completed clone run: %#v", result)
	}
}
