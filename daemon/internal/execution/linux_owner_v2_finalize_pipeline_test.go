//go:build linux && amd64

package execution

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

// These tests execute the real close paths (finalize / finalizePreGate) on a
// hand-built owner. No native helper, cgroup leaf or anchor exists in a CI
// process, so the owner carries two doubles from linuxV2CloseSeams: the helper
// control channel and the opaque platform release capability snapshot. Every
// other guard runs for real, including the issued/valid capability checks, the
// receipt reconcile and the durable mutation order.

var errLinuxV2CloseReceiptWrite = errors.New("stop receipt write failed")

const (
	linuxV2CloseLaunchToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	linuxV2ClosePipeToken   = "cccccccccccccccccccccccccccccccc"
	linuxV2CloseJobID       = "dddddddddddddddddddddddddddddddd"
	linuxV2CloseRecovery    = "/run/symmetry/close-recovery.sock"
)

// linuxV2CloseHarness is the CI executable environment of one close path: the
// coherent identity set the production validators accept, the recorded
// close-path call order and the doubles for the helper control channel and the
// opaque release capability.
type linuxV2CloseHarness struct {
	t *testing.T

	mutex  sync.Mutex
	events []string

	control *linuxV2CloseTestControl

	anchor   authority.LinuxCgroupProcessIdentity
	helper   authority.LinuxCgroupProcessIdentity
	launcher authority.LinuxCgroupProcessIdentity
	target   authority.LinuxCgroupProcessIdentity
	mirror   authority.LinuxCgroupProcessIdentity

	parent authority.LinuxCgroupParentIdentity
	leaf   authority.LinuxCgroupIdentity

	helperLauncherExit authority.LinuxCgroupExitProof
	anchorLauncherExit authority.LinuxCgroupExitProof
	helperExit         authority.LinuxCgroupExitProof

	ownerReceipt        authority.StopReceipt
	ownerReleaseProof   authority.SupervisorHandoffReleaseProof
	preGateReceipt      authority.LinuxCgroupPreGateStopReceipt
	preGateReleaseProof authority.LinuxCgroupPreGateReleaseProof

	// receiptAttempts counts every RecordStopReceipt invocation; receiptFailures
	// fails that many initial attempts and receiptAckLost makes the first
	// attempt report the never-replayable ACK-loss sentinel instead.
	receiptAttempts int
	receiptFailures int
	receiptAckLost  bool
}

func newLinuxV2CloseHarness(t *testing.T) *linuxV2CloseHarness {
	t.Helper()
	harness := &linuxV2CloseHarness{t: t}
	harness.control = &linuxV2CloseTestControl{harness: harness}
	harness.anchor = authority.LinuxCgroupProcessIdentity{PID: 10, Identity: "anchor:10:start"}
	harness.helper = authority.LinuxCgroupProcessIdentity{PID: 20, Identity: "helper:20:start"}
	harness.launcher = authority.LinuxCgroupProcessIdentity{PID: 30, Identity: "launcher:30:start"}
	harness.target = authority.LinuxCgroupProcessIdentity{PID: 40, Identity: "gate:40:start"}
	harness.mirror = authority.LinuxCgroupProcessIdentity{PID: 50, Identity: "daemon:50:start"}
	harness.parent = authority.LinuxCgroupParentIdentity{
		BootID: "00000000-0000-4000-8000-000000000001", MountID: 42, Device: 84, Inode: 126,
		Path: "/system.slice/symmetry.slice/delegated",
	}
	harness.leaf = authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: harness.parent,
		LeafName: strings.Repeat("e", authority.TokenBytes*2), LeafDevice: 168, LeafInode: 210,
	}
	harness.helperExit = authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: harness.helper,
		Witness: harness.anchor, Waited: true, Reaped: true,
	}
	harness.helperLauncherExit = authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: harness.launcher,
		Witness: harness.helper, Waited: true, Reaped: true,
	}
	adoptedHelperExit := harness.helperExit
	harness.anchorLauncherExit = authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: harness.launcher,
		Witness: harness.anchor, Waited: true, Reaped: true, HelperExit: &adoptedHelperExit,
	}
	return harness
}

func (harness *linuxV2CloseHarness) record(event string) {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	harness.events = append(harness.events, event)
}

func (harness *linuxV2CloseHarness) order() []string {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	return slices.Clone(harness.events)
}

func (harness *linuxV2CloseHarness) newHandoff(launcherExit authority.LinuxCgroupExitProof) authority.SupervisorHandoff {
	harness.t.Helper()
	leaf := harness.leaf
	mirror := harness.mirror
	handoff := authority.SupervisorHandoff{
		Version:     authority.SupervisorHandoffVersionV2,
		LaunchToken: linuxV2CloseLaunchToken, Secret: strings.Repeat("b", authority.SecretBytes*2),
		OwnerKind: authority.OwnerKindLinuxHelper, OwnerContext: linuxV2CloseRecovery,
		TargetPID: harness.target.PID, TargetIdentity: harness.target.Identity,
		OwnerUID: 1000, LauncherUID: 0,
		PipeToken: linuxV2ClosePipeToken, JobID: linuxV2CloseJobID,
		TargetBirthKind: authority.LinuxCgroupTargetBirthTrustedAGate,
		CgroupIdentity:  &leaf,
		AnchorPID:       harness.anchor.PID, AnchorIdentity: harness.anchor.Identity,
		LauncherPID: harness.launcher.PID, LauncherIdentity: harness.launcher.Identity,
		LauncherExit:  &launcherExit,
		SupervisorPID: harness.helper.PID, SupervisorIdentity: harness.helper.Identity,
		Mirror: &mirror,
	}
	if err := handoff.Validate(); err != nil {
		harness.t.Fatalf("close handoff fixture is invalid: %v", err)
	}
	return handoff
}

func (harness *linuxV2CloseHarness) helperStopProof(handoff authority.SupervisorHandoff) authority.LinuxCgroupStopProof {
	harness.t.Helper()
	proof := authority.LinuxCgroupStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
		Issuer: harness.helper, Reaper: harness.helper,
		PinnedLeafFD: true, CgroupKillAcknowledged: true, CgroupPopulatedZero: true,
		NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
		LauncherExit: harness.helperLauncherExit,
		GateExit: authority.LinuxCgroupExitProof{
			Version: authority.LinuxCgroupIdentityVersion, Process: harness.target,
			Witness: harness.helper, Waited: true, Reaped: true,
		},
	}
	harness.assertStopProof(handoff, proof)
	return proof
}

func (harness *linuxV2CloseHarness) mirrorStopProof(handoff authority.SupervisorHandoff) authority.LinuxCgroupStopProof {
	harness.t.Helper()
	mirrorHelperExit := harness.helperExit
	gateHelperExit := harness.helperExit
	proof := authority.LinuxCgroupStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofMirror,
		Issuer: harness.mirror, Reaper: harness.anchor,
		PinnedLeafFD: true, CgroupKillAcknowledged: true, CgroupPopulatedZero: true,
		NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
		LauncherExit: harness.anchorLauncherExit,
		GateExit: authority.LinuxCgroupExitProof{
			Version: authority.LinuxCgroupIdentityVersion, Process: harness.target,
			Witness: harness.anchor, Waited: true, Reaped: true, HelperExit: &gateHelperExit,
		},
		HelperExit: &mirrorHelperExit,
	}
	harness.assertStopProof(handoff, proof)
	return proof
}

func (harness *linuxV2CloseHarness) assertStopProof(handoff authority.SupervisorHandoff, proof authority.LinuxCgroupStopProof) {
	harness.t.Helper()
	supervisor, err := handoff.ToSupervisor()
	if err != nil {
		harness.t.Fatalf("close handoff fixture has no supervisor projection: %v", err)
	}
	if err := proof.ValidateFor(supervisor); err != nil {
		harness.t.Fatalf("close stop proof fixture is invalid: %v", err)
	}
}

// bindOwnerReleaseFixture derives the durable receipt and the opaque release
// capability of one gate-path fixture with the production constructor, so the
// pipeline's own reconcile and claim guards accept them.
func (harness *linuxV2CloseHarness) bindOwnerReleaseFixture(handoff authority.SupervisorHandoff, proof authority.LinuxCgroupStopProof) {
	harness.t.Helper()
	base := handoff.Clone()
	base.StopReceipt = nil
	receipt := linuxV2StopReceipt(base, proof)
	if !receipt.ValidForHandoff(base) {
		harness.t.Fatal("close receipt fixture is invalid for its handoff")
	}
	withReceipt := base.Clone()
	withReceipt.StopReceipt = &receipt
	harness.ownerReceipt = receipt
	harness.ownerReleaseProof = authority.SupervisorHandoffReleaseProof{
		Version: authority.SupervisorHandoffVersionV2, Status: "released",
		LaunchToken: base.LaunchToken, OwnerKind: base.OwnerKind, OwnerContext: base.OwnerContext,
		TargetPID: base.TargetPID, TargetIdentity: base.TargetIdentity,
		OwnerUID: base.OwnerUID, LauncherUID: base.LauncherUID,
		PipeToken: base.PipeToken, JobID: base.JobID,
		SupervisorPID: base.SupervisorPID, SupervisorIdentity: base.SupervisorIdentity,
		TargetBirthKind: base.TargetBirthKind, CgroupIdentity: base.CgroupIdentity,
		AnchorPID: base.AnchorPID, AnchorIdentity: base.AnchorIdentity,
		LauncherPID: base.LauncherPID, LauncherIdentity: base.LauncherIdentity,
		LauncherExit: base.LauncherExit, Mirror: base.Mirror,
		LeafDisposition: authority.LinuxCgroupLeafRemoved, StopReceipt: &receipt,
	}
	if err := harness.ownerReleaseProof.Validate(); err != nil {
		harness.t.Fatalf("close release proof fixture is invalid: %v", err)
	}
	if !harness.ownerReleaseProof.ValidFor(withReceipt) {
		harness.t.Fatal("close release proof fixture does not bind its exact receipt")
	}
}

func (harness *linuxV2CloseHarness) nextReceiptAttempt() int {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	harness.receiptAttempts++
	return harness.receiptAttempts
}

func (harness *linuxV2CloseHarness) gateCallbacks() LinuxV2JournalCallbacks {
	return LinuxV2JournalCallbacks{
		RecordStopReceipt: func(handoff authority.SupervisorHandoff, receipt authority.StopReceipt) error {
			attempt := harness.nextReceiptAttempt()
			harness.record("receipt")
			if attempt == 1 {
				// Model a callback that mutates its arguments. Every attempt
				// must receive its own clone, so a retry may not observe this.
				handoff.LauncherExit.Reaped = false
				receipt.CgroupIdentity.LeafName = strings.Repeat("f", authority.TokenBytes*2)
			}
			if attempt == 1 && harness.receiptAckLost {
				return errors.Join(errors.New("StageReplay dropped ACK after readback"), ErrLinuxV2MutationAckLost)
			}
			if attempt <= harness.receiptFailures {
				return errLinuxV2CloseReceiptWrite
			}
			if attempt > 1 {
				harness.assertPristineReceiptInput(attempt, handoff, receipt)
			}
			return nil
		},
		ClearProcessDetailsV2: func(int, string, platform.LinuxCgroupV2Release) error {
			harness.record("clear")
			return nil
		},
		ClearHandoffV2: func(authority.SupervisorHandoff, platform.LinuxCgroupV2Release) error {
			harness.record("clear")
			return nil
		},
	}
}

func (harness *linuxV2CloseHarness) assertPristineReceiptInput(attempt int, handoff authority.SupervisorHandoff, receipt authority.StopReceipt) {
	harness.t.Helper()
	if handoff.LauncherExit == nil || !handoff.LauncherExit.Reaped {
		harness.t.Fatalf("stop receipt attempt %d observed a mutated handoff clone", attempt)
	}
	if receipt.CgroupIdentity == nil || receipt.CgroupIdentity.LeafName != harness.leaf.LeafName {
		harness.t.Fatalf("stop receipt attempt %d observed a mutated receipt clone", attempt)
	}
}

// assertPreGateClearInput pins the pre-gate clear input: the reservation the
// close sends must carry the just-persisted pre-gate stop receipt, never a
// reservation without its durable receipt.
func (harness *linuxV2CloseHarness) assertPreGateClearInput(expected state.LinuxCgroupReservation) {
	harness.t.Helper()
	if expected.PreGateStopReceipt == nil {
		harness.t.Fatal("pre-gate ClearReservationV2 carried a reservation without the just-persisted stop receipt")
	}
	if !expected.PreGateStopReceipt.Equal(harness.preGateReceipt) {
		harness.t.Fatal("pre-gate ClearReservationV2 carried a stop receipt that differs from the just-persisted one")
	}
}

// linuxV2CloseTestControl doubles the helper control channel of the close
// pipeline and records every call in the harness order.
type linuxV2CloseTestControl struct {
	harness *linuxV2CloseHarness
	ackErr  error

	// releaseErr fails the release acknowledgement of either close path; the
	// pipeline then resolves the lost response through the anchor readback.
	releaseErr error
}

func (control *linuxV2CloseTestControl) AcknowledgeStopReceipt(context.Context, authority.StopReceipt) error {
	control.harness.record("ack")
	return control.ackErr
}

func (control *linuxV2CloseTestControl) ReleaseAfterClear(context.Context, authority.SupervisorHandoffReleaseProof) error {
	control.harness.record("release")
	return control.releaseErr
}

func (control *linuxV2CloseTestControl) AcknowledgePreGateStopReceipt(context.Context, authority.LinuxCgroupPreGateStopReceipt) error {
	control.harness.record("ack")
	return control.ackErr
}

func (control *linuxV2CloseTestControl) ReleaseAfterPreGateClear(context.Context, authority.LinuxCgroupPreGateReleaseProof) error {
	control.harness.record("release")
	return control.releaseErr
}

// newLinuxV2CloseGateOwner builds the gate-path owner the real finalize
// validates: a durably bound handoff with measured physical-stop evidence, the
// opaque release already removed, and the anchor already retired.
func newLinuxV2CloseGateOwner(t *testing.T, harness *linuxV2CloseHarness, mirror bool) *linuxV2ProcessOwner {
	t.Helper()
	launcherExit := harness.helperLauncherExit
	if mirror {
		launcherExit = harness.anchorLauncherExit
	}
	handoff := harness.newHandoff(launcherExit)
	var proof authority.LinuxCgroupStopProof
	if mirror {
		proof = harness.mirrorStopProof(handoff)
	} else {
		proof = harness.helperStopProof(handoff)
	}
	harness.bindOwnerReleaseFixture(handoff, proof)
	handoffCopy := handoff
	launcherExitCopy := launcherExit
	gate := authority.LinuxCgroupGateIdentity{
		Process: harness.target, HostUID: 1000,
		ObservedAt: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		BirthKind:  authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: harness.leaf,
		BornInLeaf: true, TrustedImage: true,
	}
	evidence := &platform.LinuxOwnerHelperV2StopEvidence{
		Version: 1, RunKey: platform.LinuxOwnerAnchorRunKey{RunID: "run-close-gate", Generation: 1},
		LaunchToken: handoff.LaunchToken, Parent: harness.parent, Leaf: harness.leaf,
		Anchor: harness.anchor, Helper: harness.helper, Launcher: harness.launcher,
		Gate: gate, Proof: proof, DescendantDigest: strings.Repeat("0", 64),
	}
	return &linuxV2ProcessOwner{
		invocation: &LinuxV2Invocation{Callbacks: harness.gateCallbacks()},
		gate:       &gate, gateBound: true, handoffBound: true, journalCommitted: true,
		handoff: &handoffCopy, launcherExit: &launcherExitCopy, stopEvidence: evidence,
		normalRelease: &platform.LinuxCgroupV2Release{}, anchorRetired: true,
		closeSeams: linuxV2CloseSeams{
			helperControl: harness.control,
			ownerReleaseSnapshot: func(platform.LinuxCgroupV2Release) (authority.SupervisorHandoffReleaseProof, bool) {
				return harness.ownerReleaseProof, true
			},
		},
	}
}

// newLinuxV2ClosePreGateOwner builds the pre-gate owner the real finalizePreGate
// validates: an armed never-created reservation with authenticated pre-gate
// stop evidence, the pre-gate leaf already removed, and the anchor already
// retired.
func newLinuxV2ClosePreGateOwner(t *testing.T, harness *linuxV2CloseHarness) *linuxV2ProcessOwner {
	t.Helper()
	grant := platform.LinuxOwnerAnchorGrant{
		ReservationVersion: state.LinuxCgroupReservationVersion,
		RunKey:             platform.LinuxOwnerAnchorRunKey{RunID: "run-close-pregate", Generation: 1},
		Fence: protocol.Fence{
			Generation: 1, RuntimeID: "runtime", RuntimeEpoch: 1, ClaimID: "claim", LeaseToken: "lease",
		},
		LaunchToken: linuxV2CloseLaunchToken, Secret: strings.Repeat("b", authority.SecretBytes*2),
		PipeToken: linuxV2ClosePipeToken, JobID: linuxV2CloseJobID,
		OwnerUID: 1000, LauncherUID: 0, RecoveryEndpoint: linuxV2CloseRecovery,
		CreateStarted: true, Anchor: harness.anchor, Parent: harness.parent, Leaf: harness.leaf,
	}
	authenticator, err := linuxgate.SignGateNeverProducedAuthenticator(grant.Secret, grant.LaunchToken, harness.launcher)
	if err != nil {
		t.Fatalf("sign no-gate launcher outcome: %v", err)
	}
	gateOutcome := authority.LinuxCgroupGateProductionResult{
		Version: authority.LinuxCgroupIdentityVersion, LaunchToken: grant.LaunchToken,
		Launcher: harness.launcher, Outcome: authority.LinuxCgroupGateOutcomeNeverCreated,
		Authenticator: authenticator,
	}
	launcher := harness.launcher
	launcherExit := harness.helperLauncherExit
	fenceLauncherExit := launcherExit.Clone()
	producerFence := platform.LinuxOwnerAnchorProducerFence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Helper: harness.helper, StopLatched: true, LeaseStopped: true, PrepareAttempted: true,
		PrepareQuiesced: true, ReleaseQuiesced: true, LeafMembershipVerified: true,
		Launcher: &launcher, LauncherExit: &fenceLauncherExit, GateOutcome: &gateOutcome,
	}
	fence := producerFence
	anchorStop := platform.LinuxOwnerAnchorStopEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Anchor: grant.Anchor, Helper: harness.helper,
		ProducerFence: &fence, PinnedLeafFD: true, KillAttempted: true, KillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, ProducerFenced: true,
		Launcher: &launcher, GateOutcome: &gateOutcome,
	}
	if err := fence.ValidateFor(grant, harness.helper); err != nil {
		t.Fatalf("anchor producer fence fixture is invalid: %v", err)
	}
	if err := anchorStop.ValidateFor(grant, harness.helper, grant.Anchor, nil, false); err != nil {
		t.Fatalf("anchor stop evidence fixture is invalid: %v", err)
	}
	preGateProof := authority.LinuxCgroupPreGateStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
		Issuer: harness.helper, Reaper: harness.helper,
		PinnedLeafFD: true, CgroupKillAcknowledged: true, CgroupPopulatedZero: true,
		NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
		GateOutcome: gateOutcome, LauncherExit: harness.helperLauncherExit,
	}
	preGate := &platform.LinuxOwnerHelperV2PreGateStopEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Anchor: grant.Anchor, Helper: harness.helper,
		AnchorStop: &anchorStop, PrepareAttempted: true,
		Launcher: &launcher, LauncherExit: &fenceLauncherExit, GateOutcome: &gateOutcome, Proof: &preGateProof,
		NoProducerCanAct: true, PinnedLeafFD: true, CgroupKillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, DescendantsReaped: true,
		DescendantDigest: strings.Repeat("0", 64),
	}
	if err := preGate.ValidateFor(grant, harness.helper); err != nil {
		t.Fatalf("pre-gate stop evidence fixture is invalid: %v", err)
	}
	leafIdentity := harness.leaf
	helper := harness.helper
	reservationLauncherExit := launcherExit.Clone()
	reservation := state.LinuxCgroupReservation{
		Version:     state.LinuxCgroupReservationVersion,
		LaunchToken: linuxV2CloseLaunchToken, Secret: grant.Secret,
		PipeToken: linuxV2ClosePipeToken, JobID: linuxV2CloseJobID,
		OwnerUID: 1000, LauncherUID: 0, RecoveryEndpoint: linuxV2CloseRecovery,
		Parent: harness.parent, LeafName: harness.leaf.LeafName,
		CreateStarted: true, LauncherGrantPossible: true,
		LeafIdentity: &leafIdentity, Anchor: harness.anchor, Helper: &helper,
		Launcher: &launcher, LauncherExit: &reservationLauncherExit, Mirror: &harness.mirror,
	}
	receipt := authority.LinuxCgroupPreGateStopReceipt{
		Version: authority.LinuxCgroupIdentityVersion, LaunchToken: reservation.LaunchToken,
		CgroupIdentity: harness.leaf, OwnerUID: reservation.OwnerUID, LauncherUID: reservation.LauncherUID,
		Anchor: reservation.Anchor, Helper: *reservation.Helper,
		Launcher: *reservation.Launcher, Mirror: *reservation.Mirror, Proof: preGateProof,
	}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("pre-gate receipt fixture is invalid: %v", err)
	}
	harness.preGateReceipt = receipt
	harness.preGateReleaseProof = authority.LinuxCgroupPreGateReleaseProof{
		Version: authority.LinuxCgroupIdentityVersion, Receipt: receipt,
		LeafDisposition: authority.LinuxCgroupLeafRemoved,
	}
	if err := harness.preGateReleaseProof.Validate(); err != nil {
		t.Fatalf("pre-gate release proof fixture is invalid: %v", err)
	}
	noGateOutcome := gateOutcome
	helperHandle := &platform.LinuxOwnerAnchorHelper{PID: harness.helper.PID, Identity: harness.helper.Identity}
	return &linuxV2ProcessOwner{
		invocation: &LinuxV2Invocation{Callbacks: LinuxV2JournalCallbacks{
			RecordPreGateStopReceipt: func(state.LinuxCgroupReservation, authority.LinuxCgroupPreGateStopReceipt) error {
				harness.record("receipt")
				return nil
			},
			ClearReservationV2: func(expected state.LinuxCgroupReservation, _ platform.LinuxCgroupV2Release) error {
				harness.assertPreGateClearInput(expected)
				harness.record("clear")
				return nil
			},
		}},
		grant: grant, grantArmed: true, noGateOutcome: &noGateOutcome,
		reservation: reservation, helper: helperHandle,
		stopEvidence: &platform.LinuxOwnerHelperV2StopEvidence{
			Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
			Parent: grant.Parent, Leaf: grant.Leaf, Anchor: grant.Anchor, Helper: harness.helper,
			PreGate: preGate, DescendantDigest: strings.Repeat("0", 64),
		},
		preGateRelease: &platform.LinuxCgroupV2Release{}, anchorRetired: true,
		closeSeams: linuxV2CloseSeams{
			helperControl: harness.control,
			preGateReleaseSnapshot: func(platform.LinuxCgroupV2Release) (authority.LinuxCgroupPreGateReleaseProof, bool) {
				return harness.preGateReleaseProof, true
			},
		},
	}
}

func linuxV2CloseEventIndex(t *testing.T, order []string, event string) int {
	t.Helper()
	index := slices.Index(order, event)
	if index < 0 {
		t.Fatalf("close order %#v omitted the %q step", order, event)
	}
	return index
}

func TestLinuxV2FinalizeClearFollowsDurableReceiptAndAck(t *testing.T) {
	harness := newLinuxV2CloseHarness(t)
	owner := newLinuxV2CloseGateOwner(t, harness, false)
	if err := owner.finalize(); err != nil {
		t.Fatalf("finalize() error = %v, want the receipt-bound close to complete", err)
	}
	order := harness.order()
	receipt := linuxV2CloseEventIndex(t, order, "receipt")
	ack := linuxV2CloseEventIndex(t, order, "ack")
	clear := linuxV2CloseEventIndex(t, order, "clear")
	if receipt >= ack || ack >= clear {
		t.Fatalf("close order = %#v, want the durable receipt and its acknowledgement before the Store clear", order)
	}
	owner.mu.Lock()
	released := owner.ownerReleased
	publishedReceipt := owner.handoff != nil && owner.handoff.StopReceipt != nil
	owner.mu.Unlock()
	if !released || !publishedReceipt {
		t.Fatalf("close state released=%t publishedReceipt=%t, want a settled owner with its durable receipt", released, publishedReceipt)
	}
}

func TestLinuxV2FinalizeReleasesHelperOnlyAfterClear(t *testing.T) {
	harness := newLinuxV2CloseHarness(t)
	owner := newLinuxV2CloseGateOwner(t, harness, false)
	if err := owner.finalize(); err != nil {
		t.Fatalf("finalize() error = %v, want the receipt-bound close to complete", err)
	}
	order := harness.order()
	if clear, release := linuxV2CloseEventIndex(t, order, "clear"), linuxV2CloseEventIndex(t, order, "release"); clear >= release {
		t.Fatalf("close order = %#v, want the helper released only after the durable Store clear", order)
	}
	owner.mu.Lock()
	helperReleased := owner.helperReleased
	owner.mu.Unlock()
	if !helperReleased {
		t.Fatal("close pipeline cleared the journal without releasing the exact helper")
	}
}

func TestLinuxV2FinalizeReusesDurableReceiptWithoutRewriting(t *testing.T) {
	harness := newLinuxV2CloseHarness(t)
	owner := newLinuxV2CloseGateOwner(t, harness, false)
	receipt := harness.ownerReceipt
	owner.mu.Lock()
	owner.handoff.StopReceipt = &receipt
	owner.mu.Unlock()
	if err := owner.finalize(); err != nil {
		t.Fatalf("finalize() error = %v, want the cached receipt to be reused", err)
	}
	order := harness.order()
	if slices.Contains(order, "receipt") {
		t.Fatalf("close order = %#v, want no receipt rewrite for an already durable receipt", order)
	}
	if !slices.Equal(order, []string{"ack", "clear", "release"}) {
		t.Fatalf("close order = %#v, want acknowledgement, clear and release", order)
	}
}

func TestLinuxV2FinalizeMirrorStopSkipsStopReceiptAck(t *testing.T) {
	helperHarness := newLinuxV2CloseHarness(t)
	helperOwner := newLinuxV2CloseGateOwner(t, helperHarness, false)
	if err := helperOwner.finalize(); err != nil {
		t.Fatalf("helper-kind finalize() error = %v", err)
	}
	helperOrder := helperHarness.order()
	if !slices.Contains(helperOrder, "ack") {
		t.Fatalf("helper-kind close order = %#v, want the durable stop receipt acknowledged", helperOrder)
	}

	mirrorHarness := newLinuxV2CloseHarness(t)
	mirrorOwner := newLinuxV2CloseGateOwner(t, mirrorHarness, true)
	if err := mirrorOwner.finalize(); err != nil {
		t.Fatalf("mirror-kind finalize() error = %v", err)
	}
	mirrorOrder := mirrorHarness.order()
	if slices.Contains(mirrorOrder, "ack") {
		t.Fatalf("mirror close order = %#v, want no acknowledgement without a bound helper", mirrorOrder)
	}
	if !slices.Equal(mirrorOrder, []string{"receipt", "clear"}) {
		t.Fatalf("mirror close order = %#v, want the receipt persisted and cleared without a helper release", mirrorOrder)
	}
}

func TestLinuxV2FinalizePromotionHopStaysBeforeClear(t *testing.T) {
	// The hop itself is out of scope and revalidates the opaque capability, so
	// CI pins its position from both sides: with a valid claim the sequence
	// stops on the hop before any clear, and with an unissued claim it stops at
	// the claim before the hop.
	hopHarness := newLinuxV2CloseHarness(t)
	hopOwner := newLinuxV2CloseGateOwner(t, hopHarness, false)
	hopOwner.cloneGrant = &authority.LinuxCloneGrant{}
	err := hopOwner.finalize()
	const hopError = "Linux clone promotion requires the exact durable stop receipt and opaque removed-leaf release"
	if err == nil || !strings.Contains(err.Error(), hopError) {
		t.Fatalf("finalize() error = %v, want the promotion hop to stop the sequence", err)
	}
	hopOrder := hopHarness.order()
	if slices.Contains(hopOrder, "clear") || slices.Contains(hopOrder, "release") {
		t.Fatalf("close order = %#v, want the hop before the durable clear and the helper release", hopOrder)
	}

	claimHarness := newLinuxV2CloseHarness(t)
	claimOwner := newLinuxV2CloseGateOwner(t, claimHarness, false)
	claimOwner.cloneGrant = &authority.LinuxCloneGrant{}
	claimOwner.closeSeams.ownerReleaseSnapshot = func(platform.LinuxCgroupV2Release) (authority.SupervisorHandoffReleaseProof, bool) {
		return authority.SupervisorHandoffReleaseProof{}, false
	}
	err = claimOwner.finalize()
	const claimError = "Linux v2 platform release capability does not match the durable receipt"
	if err == nil || err.Error() != claimError {
		t.Fatalf("finalize() error = %v, want the unissued capability claim to stop before the hop", err)
	}
	if order := claimHarness.order(); !slices.Equal(order, []string{"receipt", "ack"}) {
		t.Fatalf("close order = %#v, want the durable receipt and its acknowledgement only", order)
	}
}

func TestLinuxV2FinalizeMutationAttemptsSeeFreshClones(t *testing.T) {
	harness := newLinuxV2CloseHarness(t)
	harness.receiptFailures = 1
	owner := newLinuxV2CloseGateOwner(t, harness, false)
	if err := owner.finalize(); err != nil {
		t.Fatalf("finalize() error = %v, want the failed receipt write retried once", err)
	}
	if harness.receiptAttempts != 2 {
		t.Fatalf("stop receipt attempts = %d, want the single allowed retry", harness.receiptAttempts)
	}
	if !slices.Equal(harness.order(), []string{"receipt", "receipt", "ack", "clear", "release"}) {
		t.Fatalf("close order = %#v, want the retried receipt before its acknowledgement", harness.order())
	}
}

func TestLinuxV2FinalizeDoesNotReplayLostReceiptAck(t *testing.T) {
	harness := newLinuxV2CloseHarness(t)
	harness.receiptAckLost = true
	owner := newLinuxV2CloseGateOwner(t, harness, false)
	err := owner.finalize()
	if !errors.Is(err, ErrLinuxV2MutationAckLost) {
		t.Fatalf("finalize() error = %v, want the response-loss sentinel", err)
	}
	if harness.receiptAttempts != 1 {
		t.Fatalf("stop receipt attempts = %d, want no replay after a lost acknowledgement", harness.receiptAttempts)
	}
	if order := harness.order(); !slices.Equal(order, []string{"receipt"}) {
		t.Fatalf("close order = %#v, want the sequence stopped at the lost receipt acknowledgement", order)
	}
}

func TestLinuxV2FinalizeReconcilesCachedReceiptBeforeAnyMutation(t *testing.T) {
	harness := newLinuxV2CloseHarness(t)
	owner := newLinuxV2ClosePreGateOwner(t, harness)
	// The cached receipt came from an earlier physical-stop measurement: it is
	// shape-valid but differs from the exact evidence measured now.
	conflicting := harness.preGateReceipt
	conflicting.Proof.GateOutcome.Authenticator = strings.Repeat("a", 64)
	owner.mu.Lock()
	owner.reservation.PreGateStopReceipt = &conflicting
	owner.mu.Unlock()
	err := owner.finalizePreGate(context.Background())
	const reconcileError = "cached Linux v2 pre-gate receipt differs from exact physical-stop evidence"
	if err == nil || err.Error() != reconcileError {
		t.Fatalf("finalizePreGate() error = %v, want the conflicting cached receipt rejected", err)
	}
	if order := harness.order(); len(order) != 0 {
		t.Fatalf("close order = %#v, want no mutation before the receipt reconcile", order)
	}
	owner.mu.Lock()
	released := owner.ownerReleased
	owner.mu.Unlock()
	if released {
		t.Fatal("conflicting cached receipt settled the owner")
	}
}

func TestLinuxV2FinalizeStopsBeforeClearWhenReceiptAckFails(t *testing.T) {
	harness := newLinuxV2CloseHarness(t)
	wantErr := errors.New("helper refused the stop receipt acknowledgement")
	harness.control.ackErr = wantErr
	owner := newLinuxV2CloseGateOwner(t, harness, false)
	err := owner.finalize()
	const ackError = "acknowledge durable Linux v2 stop receipt before release"
	if err == nil || !strings.Contains(err.Error(), ackError) || !errors.Is(err, wantErr) {
		t.Fatalf("finalize() error = %v, want the failed acknowledgement to stop the close", err)
	}
	if order := harness.order(); !slices.Equal(order, []string{"receipt", "ack", "ack"}) {
		t.Fatalf("close order = %#v, want the retried acknowledgement and no mutation after it", order)
	}
	owner.mu.Lock()
	released := owner.ownerReleased
	owner.mu.Unlock()
	if released {
		t.Fatal("failed receipt acknowledgement settled the owner")
	}
}

func TestLinuxV2FinalizeResumesFromAlreadyAppliedSteps(t *testing.T) {
	harness := newLinuxV2CloseHarness(t)
	owner := newLinuxV2CloseGateOwner(t, harness, false)
	owner.mu.Lock()
	owner.clearDone = true
	owner.helperReleased = true
	owner.mu.Unlock()
	if err := owner.finalize(); err != nil {
		t.Fatalf("finalize() error = %v, want the already applied steps to be skipped", err)
	}
	if order := harness.order(); !slices.Equal(order, []string{"receipt", "ack"}) {
		t.Fatalf("close order = %#v, want no repeated clear or release", order)
	}
	owner.mu.Lock()
	released := owner.ownerReleased
	owner.mu.Unlock()
	if !released {
		t.Fatal("resumed close did not settle the owner")
	}
}

func TestLinuxV2FinalizePreGateReleasesHelperOnlyAfterClear(t *testing.T) {
	harness := newLinuxV2CloseHarness(t)
	owner := newLinuxV2ClosePreGateOwner(t, harness)
	if err := owner.finalizePreGate(context.Background()); err != nil {
		t.Fatalf("finalizePreGate() error = %v, want the pre-gate close to complete", err)
	}
	order := harness.order()
	if !slices.Equal(order, []string{"receipt", "ack", "clear", "release"}) {
		t.Fatalf("pre-gate close order = %#v, want the receipt, its acknowledgement, the clear and the release", order)
	}
	if clear, release := linuxV2CloseEventIndex(t, order, "clear"), linuxV2CloseEventIndex(t, order, "release"); clear >= release {
		t.Fatalf("pre-gate close order = %#v, want the helper released only after the durable clear", order)
	}
	owner.mu.Lock()
	released := owner.ownerReleased
	helperReleased := owner.helperReleased
	receiptAcked := owner.preGateReceiptAcked
	persisted := owner.reservation.PreGateStopReceipt != nil
	owner.mu.Unlock()
	if !released || !helperReleased || !receiptAcked || !persisted {
		t.Fatalf("pre-gate close state released=%t helperReleased=%t acked=%t persisted=%t, want all four",
			released, helperReleased, receiptAcked, persisted)
	}
}

func TestLinuxV2FinalizePreGateClaimsCapabilityOnlyAfterClear(t *testing.T) {
	harness := newLinuxV2CloseHarness(t)
	owner := newLinuxV2ClosePreGateOwner(t, harness)
	// The capability is not issued yet: a pipeline that claimed it before the
	// durable clear would stop on the claim error instead of the clear failure.
	owner.closeSeams.preGateReleaseSnapshot = func(platform.LinuxCgroupV2Release) (authority.LinuxCgroupPreGateReleaseProof, bool) {
		return authority.LinuxCgroupPreGateReleaseProof{}, false
	}
	wantErr := errors.New("reservation clear failed")
	owner.invocation.Callbacks.ClearReservationV2 = func(state.LinuxCgroupReservation, platform.LinuxCgroupV2Release) error {
		harness.record("clear")
		return wantErr
	}
	err := owner.finalizePreGate(context.Background())
	const clearError = "clear exact no-gate Linux v2 reservation with opaque release"
	if err == nil || !strings.Contains(err.Error(), clearError) || !errors.Is(err, wantErr) {
		t.Fatalf("finalizePreGate() error = %v, want the failed durable clear before any capability claim", err)
	}
	if order := harness.order(); !slices.Equal(order, []string{"receipt", "ack", "clear", "clear"}) {
		t.Fatalf("pre-gate close order = %#v, want the retried clear and no claim before it", order)
	}
	owner.mu.Lock()
	released := owner.ownerReleased
	owner.mu.Unlock()
	if released {
		t.Fatal("failed pre-gate clear settled the owner")
	}

	// The claim is also pinned from the other side: with the durable clear
	// succeeding, the unissued capability must stop the sequence on the claim
	// error after [receipt, ack, clear] and before any release.
	claimHarness := newLinuxV2CloseHarness(t)
	claimOwner := newLinuxV2ClosePreGateOwner(t, claimHarness)
	claimOwner.closeSeams.preGateReleaseSnapshot = func(platform.LinuxCgroupV2Release) (authority.LinuxCgroupPreGateReleaseProof, bool) {
		return authority.LinuxCgroupPreGateReleaseProof{}, false
	}
	err = claimOwner.finalizePreGate(context.Background())
	const unissuedClaimError = "Linux v2 pre-gate release capability differs from its receipt"
	if err == nil || err.Error() != unissuedClaimError {
		t.Fatalf("finalizePreGate() error = %v, want the unissued capability claim to stop after the durable clear", err)
	}
	if order := claimHarness.order(); !slices.Equal(order, []string{"receipt", "ack", "clear"}) {
		t.Fatalf("pre-gate close order = %#v, want the durable clear before the capability claim and no release", order)
	}
	assertLinuxV2CloseUnsettled(t, "pre-gate", claimOwner)
}

func TestLinuxV2FinalizeRejectsCapabilityBoundToAnotherReceipt(t *testing.T) {
	// The seam reports an issued capability whose proof binds a different
	// receipt: the real ValidFor half of the claim guard must stop the
	// sequence on the claim error, before the hop on the gate path and after
	// the durable clear on the pre-gate path.
	gateHarness := newLinuxV2CloseHarness(t)
	gateOwner := newLinuxV2CloseGateOwner(t, gateHarness, false)
	foreignGateProof := gateHarness.ownerReleaseProof.Clone()
	foreignGateProof.LaunchToken = strings.Repeat("f", authority.TokenBytes*2)
	gateOwner.closeSeams.ownerReleaseSnapshot = func(platform.LinuxCgroupV2Release) (authority.SupervisorHandoffReleaseProof, bool) {
		return foreignGateProof, true
	}
	err := gateOwner.finalize()
	const gateClaimError = "Linux v2 platform release capability does not match the durable receipt"
	if err == nil || err.Error() != gateClaimError {
		t.Fatalf("finalize() error = %v, want the capability claim bound to another receipt rejected", err)
	}
	if order := gateHarness.order(); !slices.Equal(order, []string{"receipt", "ack"}) {
		t.Fatalf("close order = %#v, want the durable receipt and its acknowledgement only", order)
	}
	assertLinuxV2CloseUnsettled(t, "gate", gateOwner)

	preGateHarness := newLinuxV2CloseHarness(t)
	preGateOwner := newLinuxV2ClosePreGateOwner(t, preGateHarness)
	foreignPreGateProof := preGateHarness.preGateReleaseProof.Clone()
	foreignPreGateProof.Receipt.LaunchToken = strings.Repeat("f", authority.TokenBytes*2)
	preGateOwner.closeSeams.preGateReleaseSnapshot = func(platform.LinuxCgroupV2Release) (authority.LinuxCgroupPreGateReleaseProof, bool) {
		return foreignPreGateProof, true
	}
	err = preGateOwner.finalizePreGate(context.Background())
	const preGateClaimError = "Linux v2 pre-gate release capability differs from its receipt"
	if err == nil || err.Error() != preGateClaimError {
		t.Fatalf("finalizePreGate() error = %v, want the capability claim bound to another receipt rejected", err)
	}
	if order := preGateHarness.order(); !slices.Equal(order, []string{"receipt", "ack", "clear"}) {
		t.Fatalf("pre-gate close order = %#v, want the durable clear before the capability claim and no release", order)
	}
	assertLinuxV2CloseUnsettled(t, "pre-gate", preGateOwner)
}

func TestLinuxV2FinalizeJoinsLostReleaseAckRetirementReadback(t *testing.T) {
	// The release acknowledgement is lost and the anchor readback that would
	// resolve it fails too (no anchor client exists in a CI process), so the
	// joined error must carry the release wrap first and the readback wrap
	// second, each with its exact base text.
	releaseErr := errors.New("helper response lost after the durable clear")

	harness := newLinuxV2CloseHarness(t)
	harness.control.releaseErr = releaseErr
	owner := newLinuxV2CloseGateOwner(t, harness, false)
	err := owner.finalize()
	assertLinuxV2CloseJoin(t, err, releaseErr,
		"release Linux v2 helper only after opaque Store clear",
		"read back exact anchor retirement after lost helper release ACK")
	if order := harness.order(); !slices.Equal(order, []string{"receipt", "ack", "clear", "release", "release"}) {
		t.Fatalf("close order = %#v, want the retried release and no step after the failed readback", order)
	}
	assertLinuxV2CloseUnsettled(t, "gate", owner)

	preGateHarness := newLinuxV2CloseHarness(t)
	preGateHarness.control.releaseErr = releaseErr
	preGateOwner := newLinuxV2ClosePreGateOwner(t, preGateHarness)
	err = preGateOwner.finalizePreGate(context.Background())
	assertLinuxV2CloseJoin(t, err, releaseErr,
		"release no-gate Linux v2 helper after durable clear",
		"read back exact anchor pre-gate retirement after lost helper release ACK")
	if order := preGateHarness.order(); !slices.Equal(order, []string{"receipt", "ack", "clear", "release", "release"}) {
		t.Fatalf("pre-gate close order = %#v, want the retried release and no step after the failed readback", order)
	}
	assertLinuxV2CloseUnsettled(t, "pre-gate", preGateOwner)
}

func assertLinuxV2CloseJoin(t *testing.T, err, releaseErr error, releaseText, readbackText string) {
	t.Helper()
	if err == nil || !errors.Is(err, releaseErr) {
		t.Fatalf("close error = %v, want the lost release acknowledgement preserved", err)
	}
	message := err.Error()
	releaseIndex := strings.Index(message, releaseText)
	readbackIndex := strings.Index(message, readbackText)
	if releaseIndex < 0 || readbackIndex < 0 {
		t.Fatalf("close error = %q, want both the %q and the %q wrap", message, releaseText, readbackText)
	}
	if releaseIndex > readbackIndex {
		t.Fatalf("close error = %q, want the release wrap before the readback wrap", message)
	}
}

func assertLinuxV2CloseUnsettled(t *testing.T, path string, owner *linuxV2ProcessOwner) {
	t.Helper()
	owner.mu.Lock()
	released := owner.ownerReleased
	helperReleased := owner.helperReleased
	owner.mu.Unlock()
	if released || helperReleased {
		t.Fatalf("%s owner settled after the failed release readback (released=%t helperReleased=%t)", path, released, helperReleased)
	}
}
