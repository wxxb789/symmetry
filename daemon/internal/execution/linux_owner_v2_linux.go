//go:build linux && amd64

package execution

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const (
	linuxV2StopTimeout            = 30 * time.Second
	linuxV2ControlTimeout         = 30 * time.Second
	linuxV2WitnessStatusTimeout   = 2 * time.Second
	linuxV2AdmissionStatusTimeout = 2 * time.Second
)

type linuxV2NativeReleaser interface {
	Release(context.Context, linuxgate.NativeCommand, func(context.Context, authority.LinuxCloneCheckoutReceipt) error) error
}

type linuxV2ProcessOwner struct {
	mu sync.Mutex

	invocation     *LinuxV2Invocation
	runContext     context.Context
	parent         *platform.LinuxCgroupV2Parent
	leaf           *platform.LinuxCgroupV2Leaf
	anchor         *platform.LinuxOwnerAnchorClient
	anchorID       platform.LinuxOwnerAnchorIdentity
	grant          platform.LinuxOwnerAnchorGrant
	cloneGrant     *authority.LinuxCloneGrant
	helper         *platform.LinuxOwnerAnchorHelper
	prepared       *platform.LinuxOwnerHelperV2Prepared
	prepareFailure *platform.LinuxOwnerHelperV2PrepareError

	reservation               state.LinuxCgroupReservation
	reservationAttempted      bool
	launcherExit              *authority.LinuxCgroupExitProof
	gate                      *authority.LinuxCgroupGateIdentity
	noGateOutcome             *authority.LinuxCgroupGateProductionResult
	handoff                   *authority.SupervisorHandoff
	verifiedUserNamespace     platform.LinuxUserNamespaceIdentity
	rootUserNamespaceVerified bool
	releaseSucceeded          bool
	observedWitnessDrop       *LinuxV2WitnessDropStatus
	cloneCheckout             *authority.LinuxCloneCheckoutReceipt
	cloneRunOutcome           *LinuxCloneRunOutcome
	cloneOutcomeKnown         bool
	cloneProposed             bool
	cloneProposedCommit       string
	clonePromotion            *authority.LinuxClonePromotionReceipt
	clonePromotionDone        bool
	cloneRetained             bool
	cloneAbortDone            bool

	helperBound           bool
	helperStartAttempted  bool
	grantArmed            bool
	launcherExited        bool
	gateBound             bool
	handoffPreparedFlag   bool
	handoffBound          bool
	handoffCommitTried    bool
	journalCommitted      bool
	helperCommitAck       bool
	leaseAck              bool
	leaseSequence         uint64
	leaseRenewalInFlight  bool
	leaseRenewalUncertain bool
	leaseRenewalSequence  uint64
	leaseRenewalDone      chan struct{}
	stopRequested         bool
	releaseAttempted      bool

	stopMu                  sync.Mutex
	stopEvidence            *platform.LinuxOwnerHelperV2StopEvidence
	abortProof              *authority.LinuxCgroupReservationAbortProof
	helperFencedBeforeAbort bool
	normalRelease           *platform.LinuxCgroupV2Release
	preGateRelease          *platform.LinuxCgroupV2Release
	stopReceiptAcked        bool
	preGateReceiptAcked     bool
	clearDone               bool
	helperReleased          bool
	anchorRetired           bool
	ownerReleased           bool

	waitMu     sync.Mutex
	waitResult *platform.LinuxOwnerHelperV2Exit
}

type linuxV2Containment struct {
	owner *linuxV2ProcessOwner
}

func (containment *linuxV2Containment) Terminate(force bool) error {
	if !force {
		return errors.ErrUnsupported
	}
	if containment == nil || containment.owner == nil {
		return errors.New("Linux v2 process owner is unavailable")
	}
	return containment.owner.stopForTermination()
}

func (containment *linuxV2Containment) Close() error {
	if containment == nil || containment.owner == nil {
		return nil
	}
	return containment.owner.finalize()
}

func (containment *linuxV2Containment) LeaseRenewalAvailable() bool {
	if containment == nil || containment.owner == nil {
		return false
	}
	containment.owner.mu.Lock()
	defer containment.owner.mu.Unlock()
	return containment.owner.helperCommitAck && containment.owner.leaseAck &&
		!containment.owner.stopRequested && !containment.owner.ownerReleased
}

func (containment *linuxV2Containment) RenewLease(deadline time.Duration, sequence uint64) error {
	if containment == nil || containment.owner == nil {
		return errors.New("Linux v2 process owner is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), linuxV2ControlTimeout)
	defer cancel()
	return containment.owner.renewLease(ctx, deadline, sequence)
}

func launchLinuxV2Process(
	ctx context.Context,
	command *exec.Cmd,
	invocation Invocation,
	stdinRead, stdoutWrite, stderrWrite *os.File,
) (*startedProcess, error) {
	if ctx == nil || command == nil || !invocation.linuxV2Requested() || invocation.LinuxV2 == nil {
		return nil, errors.New("Linux v2 process launch grant is incomplete")
	}
	if err := validateLinuxV2Invocation(invocation.LinuxV2); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(command.Path) || !filepath.IsAbs(command.Dir) {
		return nil, errors.New("Linux v2 native executable and B workspace must be absolute paths")
	}
	native := linuxgate.NativeCommand{
		Path: command.Path, Args: command.Args, Env: command.Env, Dir: command.Dir,
		Stdin: stdinRead, Stdout: stdoutWrite, Stderr: stderrWrite,
	}
	if invocation.LinuxV2.CloneGrant != nil {
		grant := invocation.LinuxV2.CloneGrant.Clone()
		if err := validateLinuxCloneGrantForInvocation(grant, invocation.LinuxV2.RunKey, command.Dir); err != nil {
			return nil, err
		}
		native.Clone = &grant
	}
	if err := native.Validate(); err != nil {
		return nil, fmt.Errorf("validate native command and three Runner pipes before cgroup reservation: %w", err)
	}
	if err := validateLinuxV2ChildEnvironment(native.Env); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v2Invocation := *invocation.LinuxV2
	if invocation.LinuxV2.CloneGrant != nil {
		grant := invocation.LinuxV2.CloneGrant.Clone()
		v2Invocation.CloneGrant = &grant
	}
	owner := &linuxV2ProcessOwner{
		invocation: &v2Invocation, runContext: ctx,
		parent: v2Invocation.Parent,
		anchor: v2Invocation.Anchor,
	}
	if v2Invocation.CloneGrant != nil {
		grant := v2Invocation.CloneGrant.Clone()
		owner.cloneGrant = &grant
	}
	started := owner.startedProcess()
	if err := owner.reserveAndCreate(ctx); err != nil {
		if !owner.reservationAttempted {
			return nil, err
		}
		return started, err
	}
	if err := owner.startHelper(ctx); err != nil {
		return started, err
	}
	if err := owner.prepareGate(ctx, command); err != nil {
		if owner.gate != nil {
			started.pid = owner.gate.Process.PID
			started.identity = owner.gate.Process.Identity
		}
		return started, err
	}
	started.pid = owner.gate.Process.PID
	started.identity = owner.gate.Process.Identity
	if err := owner.bindHandoff(); err != nil {
		return started, err
	}
	started.containmentHandoff = cloneSupervisorHandoff(owner.handoff)
	started.containmentHandoffStartedAt = owner.gate.ObservedAt
	if owner.cloneGrant != nil {
		started.deferLinuxV2FinalizeUntilOutputDrain = true
		started.stopLinuxV2BeforeOutputDrain = owner.stopForTermination
		started.recordLinuxV2CloneOutcome = owner.recordLinuxV2CloneOutcome
	}
	started.resume = func() error {
		return owner.release(ctx, native)
	}
	started.commitSupervisorHandoff = owner.commitHandoff
	return started, nil
}

func (owner *linuxV2ProcessOwner) recordLinuxV2CloneOutcome(outcome LinuxCloneRunOutcome) {
	owner.mu.Lock()
	copy := outcome
	owner.cloneRunOutcome = &copy
	owner.mu.Unlock()
}

func validateLinuxV2ChildEnvironment(environment []string) error {
	var home string
	var foundHome bool
	var xdgHomes [4]string
	var seenXDG uint8
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if key == "HOME" {
			if foundHome {
				return errors.New("Linux v2 native child environment contains duplicate HOME entries")
			}
			home, foundHome = value, true
		}
		index := -1
		switch key {
		case "XDG_CONFIG_HOME":
			index = 0
		case "XDG_DATA_HOME":
			index = 1
		case "XDG_STATE_HOME":
			index = 2
		case "XDG_CACHE_HOME":
			index = 3
		}
		if index < 0 {
			continue
		}
		bit := uint8(1 << index)
		if seenXDG&bit != 0 {
			return fmt.Errorf("Linux v2 native child environment contains duplicate %s entries", key)
		}
		seenXDG |= bit
		if value == "" {
			continue
		}
		if !filepath.IsAbs(value) {
			return fmt.Errorf("Linux v2 native child %s must be an absolute B path", key)
		}
		xdgHomes[index] = value
	}
	if !foundHome || !filepath.IsAbs(home) || filepath.Clean(home) == filepath.Clean(os.Getenv("HOME")) {
		return errors.New("Linux v2 native child requires an explicit absolute B HOME distinct from the inherited A HOME")
	}
	cleanHome := filepath.Clean(home)
	for _, xdgHome := range xdgHomes {
		if xdgHome == "" {
			continue
		}
		relative, err := filepath.Rel(cleanHome, filepath.Clean(xdgHome))
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("Linux v2 native child XDG paths must remain within B HOME")
		}
	}
	return nil
}

func validateLinuxV2Invocation(invocation *LinuxV2Invocation) error {
	if invocation == nil || invocation.RunKey.RunID == "" || len(invocation.RunKey.RunID) > 256 || strings.IndexByte(invocation.RunKey.RunID, 0) >= 0 || invocation.RunKey.Generation <= 0 ||
		invocation.Fence.Generation != invocation.RunKey.Generation || invocation.Fence.RuntimeID == "" || len(invocation.Fence.RuntimeID) > 256 ||
		strings.IndexByte(invocation.Fence.RuntimeID, 0) >= 0 || invocation.Fence.RuntimeEpoch <= 0 || invocation.Fence.ClaimID == "" || len(invocation.Fence.ClaimID) > 256 ||
		strings.IndexByte(invocation.Fence.ClaimID, 0) >= 0 || invocation.Fence.LeaseToken == "" || len(invocation.Fence.LeaseToken) > 8192 || strings.IndexByte(invocation.Fence.LeaseToken, 0) >= 0 ||
		invocation.Parent == nil || invocation.Anchor == nil ||
		!filepath.IsAbs(invocation.RecoveryEndpoint) || filepath.Clean(invocation.RecoveryEndpoint) != invocation.RecoveryEndpoint ||
		strings.IndexByte(invocation.RecoveryEndpoint, 0) >= 0 || len(invocation.RecoveryEndpoint) >= 108 ||
		invocation.Callbacks.Reserve == nil || invocation.Callbacks.MarkCreateStarted == nil ||
		invocation.Callbacks.BindLeaf == nil || invocation.Callbacks.BindHelper == nil ||
		invocation.Callbacks.BindLauncher == nil || invocation.Callbacks.ArmLauncherGrant == nil ||
		invocation.Callbacks.RecordLauncherExit == nil || invocation.Callbacks.RecordHelperExit == nil ||
		invocation.Callbacks.BindGate == nil || invocation.Callbacks.PrepareHandoff == nil ||
		invocation.Callbacks.BindHandoffV2 == nil || invocation.Callbacks.CommitHandoff == nil ||
		invocation.Callbacks.RecordStopReceipt == nil || invocation.Callbacks.RecordPreGateStopReceipt == nil ||
		invocation.Callbacks.AbortReservation == nil || invocation.Callbacks.ClearHandoffV2 == nil ||
		invocation.Callbacks.ClearProcessDetailsV2 == nil || invocation.Callbacks.ClearReservationV2 == nil {
		return errors.New("Linux v2 invocation requires the exact RunKey/fence, pinned owner, recovery endpoint, and complete Store callbacks")
	}
	if invocation.CloneGrant == nil {
		return errors.New("Linux v2 native release requires the exact B-owned standalone clone grant")
	}
	if err := validateLinuxCloneGrantForInvocation(*invocation.CloneGrant, invocation.RunKey, invocation.CloneGrant.Intent.Target); err != nil {
		return err
	}
	if invocation.Callbacks.CheckNativeAdmission == nil {
		return errors.New("Linux v2 native release requires an App admission check")
	}
	if invocation.Callbacks.RecordLinuxCloneCheckoutReady == nil || invocation.Callbacks.LinuxCloneProposedCommit == nil ||
		invocation.Callbacks.RetainUnpromotedLinuxClone == nil || invocation.Callbacks.PromoteLinuxClone == nil ||
		invocation.Callbacks.RecordLinuxClonePromotion == nil ||
		invocation.Callbacks.FinalizeNoCheckoutLinuxCloneAfterRetirement == nil {
		return errors.New("Linux clone grant requires checkout, outcome, retention, promotion, receipt, and post-retirement abort callbacks")
	}
	return nil
}

func validateLinuxCloneGrantForInvocation(grant authority.LinuxCloneGrant, key state.RunKey, workspace string) error {
	if err := grant.Validate(); err != nil || grant.Intent.RunID != key.RunID || grant.Intent.Generation != key.Generation ||
		workspace != grant.Intent.Target {
		return errors.Join(errors.New("Linux clone grant differs from the exact RunKey or B workspace"), err)
	}
	return nil
}

func (owner *linuxV2ProcessOwner) startedProcess() *startedProcess {
	return &startedProcess{
		containment:              &linuxV2Containment{owner: owner},
		wait:                     owner.wait,
		kill:                     owner.stopForTermination,
		close:                    owner.closeAfterClear,
		duplicateLinuxV2Peer:     owner.duplicatePeer,
		linuxV2WitnessDropStatus: owner.witnessDropStatus,
		linuxV2LiveOwnerStatus:   owner.liveOwnerStatus,
		closeLinuxV2Containment:  owner.finalize,
		renewLinuxV2Lease:        owner.renewLease,
	}
}

func (owner *linuxV2ProcessOwner) reserveAndCreate(ctx context.Context) error {
	parentIdentity := owner.parent.Identity()
	if parentIdentity.Validate() != nil || owner.parent.OwnerUID() == 0 || uint32(os.Geteuid()) != owner.parent.OwnerUID() {
		return errors.New("Linux v2 delegated parent is not the exact unprivileged A-owned handle")
	}
	anchor, err := owner.anchor.Discover(ctx)
	if err != nil {
		return fmt.Errorf("discover exact Linux owner anchor: %w", err)
	}
	if anchor.Parent != parentIdentity || anchor.OwnerUID != owner.parent.OwnerUID() ||
		anchor.Process.PID == os.Getpid() || anchor.Process.Validate() != nil || anchor.ManagerPath != parentIdentity.Path {
		return errors.New("Linux owner anchor and continuously pinned delegated parent identities differ")
	}
	mirrorIdentity, err := platform.ProcessIdentity(os.Getpid())
	if err != nil {
		return fmt.Errorf("capture daemon mirror identity: %w", err)
	}
	mirror := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: mirrorIdentity}
	if mirror.Validate() != nil || mirror.Equal(anchor.Process) {
		return errors.New("Linux v2 daemon mirror identity is invalid or aliases the owner anchor")
	}
	launchToken, err := randomLinuxV2Token(authority.TokenBytes)
	if err != nil {
		return err
	}
	secret, err := randomLinuxV2Token(authority.SecretBytes)
	if err != nil {
		return err
	}
	pipeToken, err := randomLinuxV2Token(authority.TokenBytes)
	if err != nil {
		return err
	}
	jobID, err := randomLinuxV2Token(authority.TokenBytes)
	if err != nil {
		return err
	}
	leafName, err := randomLinuxV2Token(authority.TokenBytes)
	if err != nil {
		return err
	}
	owner.anchorID = anchor
	owner.reservation = state.LinuxCgroupReservation{
		Version:     state.LinuxCgroupReservationVersion,
		LaunchToken: launchToken, Secret: secret, PipeToken: pipeToken, JobID: jobID,
		OwnerUID: anchor.OwnerUID, LauncherUID: 0, RecoveryEndpoint: owner.invocation.RecoveryEndpoint,
		Parent: parentIdentity, LeafName: leafName, Anchor: anchor.Process,
		Mirror: &mirror,
	}
	owner.reservationAttempted = true
	if err := runLinuxV2JournalMutation(owner.invocation.Callbacks.Reserve, owner.reservation); err != nil {
		return fmt.Errorf("durably reserve exact Linux cgroup leaf: %w", err)
	}
	createIntent := owner.reservation.Clone()
	if err := runLinuxV2JournalMutation(owner.invocation.Callbacks.MarkCreateStarted, createIntent); err != nil {
		return fmt.Errorf("durably record Linux cgroup creation intent before mkdir: %w", err)
	}
	owner.reservation.CreateStarted = true
	leaf, err := owner.parent.CreateLeaf(leafName)
	if err != nil {
		return fmt.Errorf("create reserved Linux cgroup leaf through pinned parent: %w", err)
	}
	owner.leaf = leaf
	leafIdentity := leaf.Identity()
	if err := leaf.VerifyKillControl(); err != nil {
		return fmt.Errorf("verify A-owned run leaf cgroup.kill: %w", err)
	}
	if err := runLinuxV2JournalMutation(func(expected state.LinuxCgroupReservation) error {
		return owner.invocation.Callbacks.BindLeaf(expected, leafIdentity)
	}, owner.reservation); err != nil {
		return fmt.Errorf("durably bind measured Linux cgroup leaf: %w", err)
	}
	owner.reservation.LeafIdentity = &leafIdentity
	owner.grant = platform.LinuxOwnerAnchorGrant{
		ReservationVersion: state.LinuxCgroupReservationVersion,
		RunKey:             platform.LinuxOwnerAnchorRunKey{RunID: owner.invocation.RunKey.RunID, Generation: owner.invocation.RunKey.Generation},
		Fence:              owner.invocation.Fence, LaunchToken: owner.reservation.LaunchToken,
		Secret: owner.reservation.Secret, PipeToken: owner.reservation.PipeToken, JobID: owner.reservation.JobID,
		OwnerUID: owner.reservation.OwnerUID, LauncherUID: owner.reservation.LauncherUID,
		RecoveryEndpoint: owner.reservation.RecoveryEndpoint, CreateStarted: true,
		Anchor: owner.reservation.Anchor, Parent: owner.reservation.Parent, Leaf: leafIdentity,
	}
	if err := owner.grant.Validate(); err != nil {
		return fmt.Errorf("validate exact Linux owner launch grant: %w", err)
	}
	return nil
}

func (owner *linuxV2ProcessOwner) startHelper(ctx context.Context) error {
	leafPin, err := owner.leaf.DuplicateFD()
	if err != nil {
		return fmt.Errorf("duplicate daemon mirror leaf pin for owner anchor: %w", err)
	}
	defer leafPin.Close()
	owner.mu.Lock()
	owner.helperStartAttempted = true
	owner.mu.Unlock()
	helper, err := owner.anchor.StartHelper(ctx, owner.grant, int(leafPin.Fd()))
	if err != nil {
		// The exact RunKey map is the only permitted replay path after an
		// uncertain StartHelper response. Lookup can attach to, but never create,
		// the one existing helper.
		lookedUp, lookupErr := owner.anchor.LookupHelper(ctx, owner.grant)
		if lookupErr != nil {
			return errors.Join(fmt.Errorf("start Linux v2 owner helper: %w", err), fmt.Errorf("lookup exact RunKey helper after unknown response: %w", lookupErr))
		}
		helper = lookedUp
	}
	owner.helper = helper
	if helper == nil || helper.Control == nil || helper.PIDFD == nil || helper.LeafFD == nil ||
		helper.PID <= 1 || helper.Identity == "" {
		return errors.New("Linux owner anchor returned an incomplete helper/leaf handle; retain the run reservation")
	}
	helperIdentity := authority.LinuxCgroupProcessIdentity{PID: helper.PID, Identity: helper.Identity}
	if helperIdentity.Validate() != nil || helperIdentity.Equal(owner.reservation.Anchor) ||
		owner.reservation.Mirror != nil && helperIdentity.Equal(*owner.reservation.Mirror) {
		return errors.New("Linux owner helper identity aliases a distinct A owner")
	}
	if err := runLinuxV2JournalMutation(func(expected state.LinuxCgroupReservation) error {
		return owner.invocation.Callbacks.BindHelper(expected, helperIdentity)
	}, owner.reservation); err != nil {
		return fmt.Errorf("durably bind exact Linux v2 helper identity: %w", err)
	}
	owner.reservation.Helper = &helperIdentity
	if err := retryLinuxV2(func() error { return helper.Control.AcknowledgeHelper(ctx) }); err != nil {
		return fmt.Errorf("acknowledge Store-bound Linux v2 helper grant: %w", err)
	}
	owner.helperBound = true
	return nil
}

func (owner *linuxV2ProcessOwner) prepareGate(ctx context.Context, command *exec.Cmd) (resultErr error) {
	defer func() {
		if resultErr != nil {
			owner.captureWitnessDropStatus()
		}
	}()
	if owner.helper == nil || owner.helper.Control == nil || !owner.helperBound || command == nil || command.Path == "" {
		return errors.New("Linux v2 helper must be durably bound before gate preparation")
	}
	prepared, prepareErr := owner.helper.Control.Prepare(ctx, func(_ context.Context, launcher authority.LinuxCgroupProcessIdentity) error {
		return owner.bindAndArmLauncher(launcher)
	})
	if prepared != nil {
		owner.prepared = prepared
		owner.gate = cloneLinuxV2Gate(&prepared.Gate)
		proof := prepared.LauncherExit
		if err := owner.recordLauncherExit(proof); err != nil {
			return errors.Join(prepareErr, fmt.Errorf("durably record exact transient launcher wait: %w", err))
		}
		if err := owner.bindGate(); err != nil {
			return errors.Join(prepareErr, fmt.Errorf("durably bind exact parked A gate: %w", err))
		}
		if prepareErr != nil {
			return prepareErr
		}
		if owner.gate == nil || !prepared.Gate.Equal(*owner.gate) ||
			prepared.UserNamespace.Device == 0 || prepared.UserNamespace.Inode == 0 {
			return errors.New("Linux v2 prepared gate lacks its exact root-cross-checked pre-release user namespace")
		}
		// Helper Prepare returns a ready gate only after its A-side nsfs capture
		// matched the authenticated root launcher's nsfs identity. Preserve that
		// exact pre-release value; a mismatch leaves the gate parked and never
		// reaches RELEASE or the native peer adapter.
		owner.mu.Lock()
		owner.verifiedUserNamespace = prepared.UserNamespace
		owner.rootUserNamespaceVerified = true
		owner.mu.Unlock()
		return nil
	}
	var failure *platform.LinuxOwnerHelperV2PrepareError
	if errors.As(prepareErr, &failure) {
		owner.prepareFailure = failure
		if owner.grantArmed && owner.reservation.Launcher != nil && failure.Launcher != nil &&
			owner.reservation.Launcher.Equal(*failure.Launcher) && failure.LauncherExit != nil &&
			failure.LauncherExit.ValidForOwnedDescendant(*failure.Launcher, authority.LinuxCgroupProcessIdentity{PID: owner.helper.PID, Identity: owner.helper.Identity}, owner.anchorID.Process, nil) {
			if !owner.launcherExited {
				if err := owner.recordLauncherExit(*failure.LauncherExit); err != nil {
					return errors.Join(prepareErr, fmt.Errorf("durably record failed root launcher wait: %w", err))
				}
			}
		}
		if owner.grantArmed && failure.GateOutcome != nil && owner.reservation.Launcher != nil &&
			failure.Launcher != nil && owner.reservation.Launcher.Equal(*failure.Launcher) && failure.LauncherExit != nil {
			outcome := *failure.GateOutcome
			if outcome.ValidateFor(owner.reservation.LaunchToken, *failure.Launcher) == nil {
				owner.noGateOutcome = &outcome
			}
		}
		if owner.gate != nil && owner.launcherExited {
			if err := owner.bindGate(); err != nil {
				return errors.Join(prepareErr, fmt.Errorf("durably bind observed but unresolved A gate: %w", err))
			}
			if err := owner.bindHandoff(); err != nil {
				return errors.Join(prepareErr, fmt.Errorf("durably bind cleanup handoff for observed A gate: %w", err))
			}
		}
	}
	if prepareErr != nil && owner.gate == nil && owner.grantArmed && owner.helper != nil && owner.helper.Control != nil {
		if err := owner.recoverGateAfterPrepareError(ctx, failure); err != nil {
			prepareErr = errors.Join(prepareErr, fmt.Errorf("read back exact Linux v2 gate after Prepare error: %w", err))
		}
	}
	if prepareErr != nil && owner.gate != nil && owner.launcherExited {
		if err := owner.bindGate(); err != nil {
			return errors.Join(prepareErr, fmt.Errorf("durably bind observed Linux v2 gate after Prepare error: %w", err))
		}
		if err := owner.bindHandoff(); err != nil {
			return errors.Join(prepareErr, fmt.Errorf("durably bind cleanup handoff for observed A gate: %w", err))
		}
	}
	return prepareErr
}

func (owner *linuxV2ProcessOwner) recoverGateAfterPrepareError(ctx context.Context, failure *platform.LinuxOwnerHelperV2PrepareError) error {
	status, err := owner.helper.Control.Status(ctx)
	if err != nil {
		return err
	}
	defer status.Close()
	if status.Gate == nil || status.Gate.Validate() != nil || status.Launcher == nil || status.LauncherExit == nil || owner.launcherExit == nil ||
		owner.reservation.Launcher == nil || !status.Launcher.Equal(*owner.reservation.Launcher) ||
		!status.LauncherExit.Equal(*owner.launcherExit) {
		return errors.New("helper status does not carry the exact durably bound launcher and gate")
	}
	var pidfd *os.File
	if status.PIDFD != nil {
		pidfd = status.PIDFD
		status.PIDFD = nil
	} else if failure != nil && failure.PIDFD != nil && failure.Gate != nil && failure.Gate.Equal(*status.Gate) {
		pidfd = failure.PIDFD
		failure.PIDFD = nil
	} else {
		return errors.New("helper status has no independently transferred exact gate pidfd")
	}
	owner.gate = cloneLinuxV2Gate(status.Gate)
	var userNamespace platform.LinuxUserNamespaceIdentity
	if status.UserNamespace != nil {
		userNamespace = *status.UserNamespace
	}
	owner.prepared = &platform.LinuxOwnerHelperV2Prepared{
		Gate: *status.Gate, Launcher: *status.Launcher, LauncherExit: *status.LauncherExit,
		UserNamespace: userNamespace, PIDFD: pidfd,
	}
	return nil
}

// noGateOutcome is the root-launcher's authenticated clone3-never-produced
// result, retained separately until a helper stop and pinned-leaf receipt bind
// it to physical stop evidence.
func (owner *linuxV2ProcessOwner) bindAndArmLauncher(launcher authority.LinuxCgroupProcessIdentity) error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if !owner.helperBound || owner.reservation.Helper == nil || owner.reservation.Launcher != nil ||
		owner.reservation.LauncherGrantPossible || launcher.Validate() != nil {
		return errors.New("Linux root launcher is not the exact first producer for this bound helper")
	}
	if err := runLinuxV2JournalMutation(func(expected state.LinuxCgroupReservation) error {
		return owner.invocation.Callbacks.BindLauncher(expected, launcher)
	}, owner.reservation); err != nil {
		return fmt.Errorf("durably bind transient root launcher before its grant: %w", err)
	}
	copy := launcher
	owner.reservation.Launcher = &copy
	if err := runLinuxV2JournalMutation(owner.invocation.Callbacks.ArmLauncherGrant, owner.reservation); err != nil {
		return fmt.Errorf("durably arm root launcher grant before release: %w", err)
	}
	owner.reservation.LauncherGrantPossible = true
	owner.grantArmed = true
	return nil
}

func (owner *linuxV2ProcessOwner) recordLauncherExit(proof authority.LinuxCgroupExitProof) error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.launcherExited {
		if owner.launcherExit != nil && owner.launcherExit.Equal(proof) {
			return nil
		}
		return errors.New("Linux v2 launcher exit proof conflicts with the already recorded producer")
	}
	if owner.reservation.Launcher == nil || owner.reservation.Helper == nil ||
		!proof.ValidForOwnedDescendant(*owner.reservation.Launcher, *owner.reservation.Helper, owner.reservation.Anchor, owner.reservation.HelperExit) {
		return errors.New("Linux v2 launcher exit proof does not identify the exact durable launcher and helper")
	}
	if err := runLinuxV2JournalMutation(func(expected state.LinuxCgroupReservation) error {
		return owner.invocation.Callbacks.RecordLauncherExit(expected, proof)
	}, owner.reservation); err != nil {
		return err
	}
	copy := proof
	owner.reservation.LauncherExit = &copy
	owner.launcherExit = &copy
	owner.launcherExited = true
	return nil
}

func (owner *linuxV2ProcessOwner) bindGate() error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.gate == nil || owner.gate.Validate() != nil || owner.reservation.LauncherExit == nil ||
		owner.reservation.LeafIdentity == nil || !owner.gate.Cgroup.Equal(*owner.reservation.LeafIdentity) {
		return errors.New("Linux v2 gate lacks exact launcher-exit and pinned-leaf evidence")
	}
	if owner.gateBound {
		if owner.reservation.Gate != nil && owner.reservation.Gate.Equal(*owner.gate) {
			return nil
		}
		return errors.New("Linux v2 gate binding conflicts with its durable reservation")
	}
	if err := runLinuxV2JournalMutation(func(expected state.LinuxCgroupReservation) error {
		return owner.invocation.Callbacks.BindGate(expected, *owner.gate)
	}, owner.reservation); err != nil {
		return err
	}
	copy := *owner.gate
	owner.reservation.Gate = &copy
	owner.gateBound = true
	return nil
}

func (owner *linuxV2ProcessOwner) bindHandoff() error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if !owner.gateBound || owner.gate == nil || owner.reservation.Helper == nil || owner.reservation.Mirror == nil ||
		owner.reservation.Launcher == nil || owner.reservation.LauncherExit == nil {
		return errors.New("Linux v2 handoff requires a durably bound gate and every exact producer")
	}
	prepared := authority.SupervisorHandoff{
		Version:     authority.SupervisorHandoffVersionV2,
		LaunchToken: owner.reservation.LaunchToken, Secret: owner.reservation.Secret,
		OwnerKind:    authority.OwnerKindLinuxHelper,
		OwnerContext: owner.reservation.RecoveryEndpoint,
		TargetPID:    owner.gate.Process.PID, TargetIdentity: owner.gate.Process.Identity,
		OwnerUID: owner.reservation.OwnerUID, LauncherUID: owner.reservation.LauncherUID,
		PipeToken: owner.reservation.PipeToken, JobID: owner.reservation.JobID,
		TargetBirthKind: authority.LinuxCgroupTargetBirthTrustedAGate,
		CgroupIdentity:  cloneLinuxV2Cgroup(&owner.reservation.LeafIdentity),
		AnchorPID:       owner.reservation.Anchor.PID, AnchorIdentity: owner.reservation.Anchor.Identity,
		LauncherPID: owner.reservation.Launcher.PID, LauncherIdentity: owner.reservation.Launcher.Identity,
		LauncherExit: cloneLinuxV2Exit(owner.reservation.LauncherExit),
	}
	if err := prepared.Validate(); err != nil {
		return fmt.Errorf("validate exact v2 supervisor handoff: %w", err)
	}
	if !owner.handoffPreparedFlag {
		if err := runLinuxV2JournalMutation(owner.invocation.Callbacks.PrepareHandoff, prepared); err != nil {
			return fmt.Errorf("durably prepare exact Linux v2 handoff: %w", err)
		}
		owner.handoffPreparedFlag = true
	}
	helperIdentity := *owner.reservation.Helper
	mirrorIdentity := *owner.reservation.Mirror
	if !owner.handoffBound {
		if err := runLinuxV2JournalMutation(func(expected authority.SupervisorHandoff) error {
			return owner.invocation.Callbacks.BindHandoffV2(expected, helperIdentity, mirrorIdentity)
		}, prepared); err != nil {
			return fmt.Errorf("durably bind exact helper and daemon mirror to Linux v2 handoff: %w", err)
		}
		bound := prepared.Clone()
		bound.SupervisorPID = helperIdentity.PID
		bound.SupervisorIdentity = helperIdentity.Identity
		mirror := mirrorIdentity
		bound.Mirror = &mirror
		owner.handoff = &bound
		owner.handoffBound = true
	}
	return nil
}

func (owner *linuxV2ProcessOwner) commitHandoff(expected authority.SupervisorHandoff, startedAt time.Time) error {
	owner.mu.Lock()
	if !owner.handoffBound || owner.handoff == nil || !owner.handoff.Equal(expected) || owner.gate == nil || !startedAt.Equal(owner.gate.ObservedAt) {
		owner.mu.Unlock()
		return errors.New("Linux v2 durable handoff commit differs from the parked gate")
	}
	owner.handoffCommitTried = true
	journalCommitted := owner.journalCommitted
	prepared := owner.prepared
	owner.mu.Unlock()
	if !journalCommitted {
		if err := runLinuxV2JournalMutation(func(value authority.SupervisorHandoff) error {
			return owner.invocation.Callbacks.CommitHandoff(value, startedAt)
		}, expected); err != nil {
			return fmt.Errorf("durably commit Linux v2 handoff and process authority: %w", err)
		}
		owner.mu.Lock()
		owner.journalCommitted = true
		owner.mu.Unlock()
	}
	if prepared == nil || owner.helper == nil || owner.helper.Control == nil {
		return errors.New("Linux v2 helper commit acknowledgement handle is unavailable")
	}
	controlContext, cancel := context.WithTimeout(context.Background(), linuxV2ControlTimeout)
	defer cancel()
	if err := owner.helper.Control.Commit(controlContext, *prepared); err != nil {
		return fmt.Errorf("acknowledge durable Linux v2 gate commit to its helper: %w", err)
	}
	owner.mu.Lock()
	owner.helperCommitAck = true
	owner.mu.Unlock()
	return nil
}

func (owner *linuxV2ProcessOwner) renewLease(ctx context.Context, deadline time.Duration, sequence uint64) error {
	if ctx == nil || deadline <= 0 || sequence == 0 {
		return errors.New("Linux v2 helper lease requires a context, positive deadline, and sequence")
	}
	owner.mu.Lock()
	if !owner.helperCommitAck || owner.helper == nil || owner.helper.Control == nil ||
		(!owner.leaseAck && sequence != 1) || (owner.leaseAck && sequence != owner.leaseSequence+1) ||
		owner.stopRequested || owner.leaseRenewalInFlight || owner.leaseRenewalUncertain {
		owner.mu.Unlock()
		return errors.New("Linux v2 helper lease sequence is not the exact next resolved committed live-run renewal")
	}
	helper := owner.helper.Control
	helperIdentity := authority.LinuxCgroupProcessIdentity{PID: owner.helper.PID, Identity: owner.helper.Identity}
	leaseAckedOnEntry := owner.leaseAck
	done := make(chan struct{})
	owner.leaseRenewalInFlight = true
	owner.leaseRenewalSequence = sequence
	owner.leaseRenewalDone = done
	owner.mu.Unlock()
	if err := retryLinuxV2(func() error { return helper.RenewLease(ctx, deadline, sequence) }); err != nil {
		statusContext, cancel := context.WithTimeout(ctx, linuxV2AdmissionStatusTimeout)
		status, statusErr := helper.Status(statusContext)
		cancel()
		acknowledged := false
		if statusErr == nil {
			owner.mu.Lock()
			grant := owner.grant
			owner.mu.Unlock()
			acknowledged = linuxV2LeaseAckStatusMatches(status, grant, helperIdentity, sequence)
			_ = status.Close()
		}
		finishErr := owner.finishLeaseRenewal(done, sequence, acknowledged)
		if acknowledged && finishErr == nil {
			return nil
		}
		owner.captureWitnessDropStatus()
		if statusErr == nil && !acknowledged {
			statusErr = errors.New("fresh helper Status did not confirm the exact renewed lease sequence")
		}
		return errors.Join(fmt.Errorf("Linux v2 helper lease renewal response is unresolved: %w", err), statusErr, finishErr)
	}
	if err := owner.finishLeaseRenewal(done, sequence, true); err != nil {
		return fmt.Errorf("record acknowledged Linux v2 helper lease sequence: %w", err)
	}
	if sequence == 1 && owner.invocation.Callbacks.DropAckAfter != nil {
		if err := owner.invocation.Callbacks.DropAckAfter(LinuxV2AckStageLeaseArmed, leaseAckedOnEntry); err != nil {
			owner.captureWitnessDropStatus()
			return fmt.Errorf("initial Linux v2 lease ACK response was lost after helper acceptance: %w", err)
		}
	}
	return nil
}

func linuxV2LeaseAckStatusMatches(
	status platform.LinuxOwnerHelperV2Status,
	grant platform.LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
	sequence uint64,
) bool {
	return status.RunKey == grant.RunKey && status.Fence == grant.Fence && status.Helper.Equal(helper) &&
		status.LeaseSequence == sequence && linuxV2LeaseStatusCanReconcile(status)
}

func linuxV2LeaseStatusCanReconcile(status platform.LinuxOwnerHelperV2Status) bool {
	return status.State == "active" && status.CommitAcknowledged && status.LeaseLive &&
		!status.OwnerLost && !status.StopRequested && !status.StopAttempted && !status.ReleaseInFlight &&
		status.ReleaseAttempted == status.Released && status.StopEvidence == nil && status.Exit == nil && status.ErrorCode == ""
}

func (owner *linuxV2ProcessOwner) finishLeaseRenewal(done chan struct{}, sequence uint64, acknowledged bool) error {
	owner.mu.Lock()
	if done == nil || owner.leaseRenewalDone != done || !owner.leaseRenewalInFlight || owner.leaseRenewalSequence != sequence {
		owner.mu.Unlock()
		return errors.New("Linux v2 helper lease renewal ownership changed before settlement")
	}
	owner.leaseRenewalInFlight = false
	owner.leaseRenewalDone = nil
	if acknowledged {
		owner.leaseAck = true
		owner.leaseSequence = sequence
		owner.leaseRenewalUncertain = false
		owner.leaseRenewalSequence = 0
	} else {
		owner.leaseRenewalUncertain = true
	}
	owner.mu.Unlock()
	close(done)
	return nil
}

func (owner *linuxV2ProcessOwner) waitForLeaseRenewal(ctx context.Context) error {
	for {
		if err := lockLinuxV2OwnerStatusContext(ctx, owner); err != nil {
			return err
		}
		if !owner.leaseRenewalInFlight {
			owner.mu.Unlock()
			return nil
		}
		done := owner.leaseRenewalDone
		owner.mu.Unlock()
		if done == nil {
			return errors.New("Linux v2 lease renewal has no completion signal")
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

type linuxV2HelperStatusReader interface {
	Status(context.Context) (platform.LinuxOwnerHelperV2Status, error)
}

// linuxV2ReadHelperStatusWithPIDFD rejects a Status reply if the exact helper
// exited while the authenticated RPC was in flight.
func linuxV2ReadHelperStatusWithPIDFD(
	ctx context.Context,
	helperPIDFD *os.File,
	reader linuxV2HelperStatusReader,
) (platform.LinuxOwnerHelperV2Status, error) {
	if ctx == nil || helperPIDFD == nil || reader == nil {
		return platform.LinuxOwnerHelperV2Status{}, errors.New("Linux v2 helper Status pidfd fence is incomplete")
	}
	exited, err := linuxV2PIDFDExited(helperPIDFD)
	if err != nil {
		return platform.LinuxOwnerHelperV2Status{}, err
	}
	if exited {
		return platform.LinuxOwnerHelperV2Status{}, errors.New("exact Linux v2 helper pidfd exited before Status")
	}
	status, err := reader.Status(ctx)
	if err != nil {
		return platform.LinuxOwnerHelperV2Status{}, err
	}
	exited, err = linuxV2PIDFDExited(helperPIDFD)
	if err != nil || exited {
		_ = status.Close()
		if err != nil {
			return platform.LinuxOwnerHelperV2Status{}, fmt.Errorf("recheck exact helper pidfd after Status: %w", err)
		}
		return platform.LinuxOwnerHelperV2Status{}, errors.New("exact Linux v2 helper exited during Status")
	}
	return status, nil
}

func (owner *linuxV2ProcessOwner) readLiveOwnerHelperStatus(
	ctx context.Context,
	control *platform.LinuxOwnerHelperV2Client,
	helperPIDFD *os.File,
	grant platform.LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
) (platform.LinuxOwnerHelperV2Status, error) {
	for {
		if err := owner.waitForLeaseRenewal(ctx); err != nil {
			return platform.LinuxOwnerHelperV2Status{}, err
		}
		status, err := linuxV2ReadHelperStatusWithPIDFD(ctx, helperPIDFD, control)
		if err != nil {
			return platform.LinuxOwnerHelperV2Status{}, fmt.Errorf("read exact live-owner helper Status: %w", err)
		}
		if status.RunKey != grant.RunKey || status.Fence != grant.Fence || !status.Helper.Equal(helper) {
			_ = status.Close()
			return platform.LinuxOwnerHelperV2Status{}, errors.New("live-owner helper Status changed its exact RunKey, fence, or identity")
		}
		if err := lockLinuxV2OwnerStatusContext(ctx, owner); err != nil {
			_ = status.Close()
			return platform.LinuxOwnerHelperV2Status{}, err
		}
		currentHelper := owner.helper
		if currentHelper == nil || currentHelper.Control != control || currentHelper.PID != helper.PID || currentHelper.Identity != helper.Identity {
			owner.mu.Unlock()
			_ = status.Close()
			return platform.LinuxOwnerHelperV2Status{}, errors.New("live-owner helper changed during Status readback")
		}
		if owner.leaseRenewalInFlight {
			done := owner.leaseRenewalDone
			owner.mu.Unlock()
			_ = status.Close()
			if done == nil {
				return platform.LinuxOwnerHelperV2Status{}, errors.New("in-flight lease renewal has no completion signal")
			}
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return platform.LinuxOwnerHelperV2Status{}, ctx.Err()
			}
		}
		if owner.stopRequested || owner.ownerReleased {
			owner.mu.Unlock()
			return status, nil
		}
		leaseAck := owner.leaseAck
		leaseSequence := owner.leaseSequence
		uncertain := owner.leaseRenewalUncertain
		pendingSequence := owner.leaseRenewalSequence
		if uncertain {
			if pendingSequence > 0 && status.LeaseSequence == pendingSequence && linuxV2LeaseStatusCanReconcile(status) {
				owner.leaseAck = true
				owner.leaseSequence = pendingSequence
				owner.leaseRenewalUncertain = false
				owner.leaseRenewalSequence = 0
				owner.mu.Unlock()
				return status, nil
			}
			if leaseAck && status.LeaseSequence == leaseSequence && linuxV2LeaseStatusCanReconcile(status) {
				owner.mu.Unlock()
				_ = status.Close()
				if err := waitLinuxV2AdmissionStatusRetry(ctx); err != nil {
					return platform.LinuxOwnerHelperV2Status{}, fmt.Errorf("lease renewal readback remained unresolved: %w", err)
				}
				continue
			}
			owner.mu.Unlock()
			_ = status.Close()
			return platform.LinuxOwnerHelperV2Status{}, errors.New("fresh helper Status did not reconcile the exact in-flight lease sequence")
		}
		if leaseAck && status.LeaseSequence < leaseSequence {
			owner.mu.Unlock()
			_ = status.Close()
			if err := waitLinuxV2AdmissionStatusRetry(ctx); err != nil {
				return platform.LinuxOwnerHelperV2Status{}, fmt.Errorf("helper Status remained behind the last acknowledged lease sequence: %w", err)
			}
			continue
		}
		if status.LeaseSequence != leaseSequence {
			owner.mu.Unlock()
			_ = status.Close()
			return platform.LinuxOwnerHelperV2Status{}, errors.New("helper Status lease sequence differs from the last acknowledged owner sequence")
		}
		owner.mu.Unlock()
		return status, nil
	}
}

func waitLinuxV2AdmissionStatusRetry(ctx context.Context) error {
	timer := time.NewTimer(25 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (owner *linuxV2ProcessOwner) release(ctx context.Context, native linuxgate.NativeCommand) error {
	owner.mu.Lock()
	if owner.invocation == nil || owner.leaf == nil || !owner.releaseReadyLocked() {
		owner.mu.Unlock()
		return errors.New("Linux v2 RELEASE requires durable commit, helper commit ACK, initial lease ACK, and an unused gate")
	}
	if err := owner.leaf.VerifyKillControl(); err != nil {
		owner.mu.Unlock()
		return fmt.Errorf("verify exact Linux v2 leaf before RELEASE: %w", err)
	}
	releaseAckedOnEntry := owner.releaseSucceeded
	helper := owner.helper.Control
	checkNativeAdmission := owner.invocation.Callbacks.CheckNativeAdmission
	var onCheckoutReady func(context.Context, authority.LinuxCloneCheckoutReceipt) error
	if owner.cloneGrant != nil {
		onCheckoutReady = owner.recordLinuxCloneCheckoutReady
	}
	owner.mu.Unlock()
	if err := owner.releaseAfterAdmission(ctx, native, helper, onCheckoutReady, checkNativeAdmission); err != nil {
		owner.mu.Lock()
		releaseAttempted := owner.releaseAttempted
		owner.mu.Unlock()
		if releaseAttempted {
			owner.captureWitnessDropStatus()
			return fmt.Errorf("one-use Linux v2 RELEASE result is unresolved: %w", err)
		}
		return err
	}
	owner.mu.Lock()
	owner.releaseSucceeded = true
	owner.mu.Unlock()
	if owner.invocation.Callbacks.DropAckAfter != nil {
		if err := owner.invocation.Callbacks.DropAckAfter(LinuxV2AckStageNativeRelease, releaseAckedOnEntry); err != nil {
			owner.captureWitnessDropStatus()
			return fmt.Errorf("Linux v2 native RELEASE ACK response was lost after helper acceptance: %w", err)
		}
	}
	return nil
}

func (owner *linuxV2ProcessOwner) releaseReadyLocked() bool {
	return owner != nil && owner.handoffCommitTried && owner.journalCommitted && owner.helperCommitAck && owner.leaseAck &&
		!owner.leaseRenewalInFlight && !owner.leaseRenewalUncertain && !owner.releaseAttempted &&
		!owner.stopRequested && !owner.ownerReleased && owner.helper != nil && owner.helper.Control != nil
}

func (owner *linuxV2ProcessOwner) releaseAfterAdmission(
	ctx context.Context,
	native linuxgate.NativeCommand,
	helper linuxV2NativeReleaser,
	onCheckoutReady func(context.Context, authority.LinuxCloneCheckoutReceipt) error,
	checkNativeAdmission func() error,
) error {
	if ctx == nil || helper == nil || checkNativeAdmission == nil {
		return errors.New("Linux v2 native release is missing its context, helper, or App admission check")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	owner.mu.Lock()
	if !owner.releaseReadyLocked() {
		owner.mu.Unlock()
		return errors.New("Linux v2 release authority is not ready before the App admission check")
	}
	owner.mu.Unlock()
	if err := checkNativeAdmission(); err != nil {
		return fmt.Errorf("recheck cross-RunKey native admission before Linux v2 RELEASE: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	owner.mu.Lock()
	if !owner.releaseReadyLocked() {
		owner.mu.Unlock()
		return errors.New("Linux v2 release authority changed during the App admission check")
	}
	owner.releaseAttempted = true
	owner.mu.Unlock()
	// The daemon sends only the command and three Runner pipes. The helper
	// supplies its own retained read-only owner pipe and the exact monotonic
	// cutoff from the acknowledged lease to PreparedGate.Release; execution must
	// never synthesize either guard value.
	return helper.Release(ctx, native, onCheckoutReady)
}

func (owner *linuxV2ProcessOwner) recordLinuxCloneCheckoutReady(ctx context.Context, receipt authority.LinuxCloneCheckoutReceipt) error {
	if ctx == nil {
		return errors.New("Linux clone checkout ACK context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	owner.mu.Lock()
	grant := owner.cloneGrant
	if grant == nil {
		owner.mu.Unlock()
		return errors.New("Linux owner gate reported a clone checkout without a reserved clone grant")
	}
	if owner.cloneCheckout != nil {
		previous := *owner.cloneCheckout
		owner.mu.Unlock()
		if !previous.Equal(receipt) {
			return errors.New("Linux owner gate changed its exact clone checkout receipt")
		}
		return nil
	}
	grantCopy := grant.Clone()
	owner.mu.Unlock()
	if err := validateLinuxCloneCheckoutReceipt(grantCopy, receipt); err != nil {
		return err
	}
	if err := runLinuxV2MutationRetry(func() error {
		return owner.invocation.Callbacks.RecordLinuxCloneCheckoutReady(receipt.Clone())
	}); err != nil {
		return fmt.Errorf("persist exact B-owned Linux clone checkout before native exec ACK: %w", err)
	}
	owner.mu.Lock()
	copy := receipt.Clone()
	owner.cloneCheckout = &copy
	owner.mu.Unlock()
	return nil
}

func validateLinuxCloneCheckoutReceipt(grant authority.LinuxCloneGrant, receipt authority.LinuxCloneCheckoutReceipt) error {
	if err := grant.Validate(); err != nil {
		return fmt.Errorf("validate exact Linux clone RELEASE grant: %w", err)
	}
	intent := grant.Intent
	if err := receipt.Validate(); err != nil || receipt.RunID != intent.RunID || receipt.Generation != intent.Generation ||
		receipt.BindingKey != intent.BindingKey || receipt.ResourceID != intent.ResourceID || receipt.Target != intent.Target || receipt.Nonce != intent.Nonce ||
		receipt.SeedDigest != grant.Seed.Digest || receipt.BaseCommit != intent.BaseCommit {
		return errors.Join(errors.New("B checkout-ready receipt differs from the exact Linux clone grant"), err)
	}
	return nil
}

func (owner *linuxV2ProcessOwner) wait() (int, error) {
	owner.mu.Lock()
	if !owner.releaseAttempted || owner.helper == nil || owner.helper.Control == nil {
		owner.mu.Unlock()
		return -1, errors.New("Linux v2 gate did not receive a proven one-use RELEASE")
	}
	helper := owner.helper.Control
	owner.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := helper.Wait(ctx)
	if err != nil {
		return -1, fmt.Errorf("wait for exact helper-reaped Linux v2 process: %w", err)
	}
	if result.Process.Validate() != nil || result.Proof.Validate() != nil ||
		!result.Process.Equal(authority.LinuxCgroupProcessIdentity{PID: owner.processPID(), Identity: owner.processIdentity()}) ||
		!result.Proof.ValidForOwnedDescendant(result.Process,
			authority.LinuxCgroupProcessIdentity{PID: owner.helper.PID, Identity: owner.helper.Identity},
			owner.anchorID.Process, nil) || result.Exited && result.Signal != 0 || !result.Exited && result.Signal == 0 {
		return -1, errors.New("Linux v2 helper wait result does not prove exact gate exit and reap")
	}
	owner.waitMu.Lock()
	waitCopy := result
	owner.waitResult = &waitCopy
	owner.waitMu.Unlock()
	if result.Signal != 0 {
		return -1, linuxV2ExitStatusError(result)
	}
	return result.ExitCode, linuxV2ExitStatusError(result)
}

func (owner *linuxV2ProcessOwner) processPID() int {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.gate == nil {
		return 0
	}
	return owner.gate.Process.PID
}

func (owner *linuxV2ProcessOwner) processIdentity() string {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.gate == nil {
		return ""
	}
	return owner.gate.Process.Identity
}

func linuxV2ExitStatusError(result platform.LinuxOwnerHelperV2Exit) error {
	if result.Exited && result.Signal != 0 || !result.Exited && result.Signal == 0 {
		return errors.New("Linux v2 helper returned an inconsistent exact wait status")
	}
	if !result.Exited {
		if result.CoreDumped {
			return fmt.Errorf("Linux v2 native process terminated by signal %d and dumped core", result.Signal)
		}
		return fmt.Errorf("Linux v2 native process terminated by signal %d", result.Signal)
	}
	if result.ExitCode < 0 || result.ExitCode > 255 {
		return errors.New("Linux v2 helper returned an invalid native exit code")
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("Linux v2 native process exited with status %d", result.ExitCode)
	}
	return nil
}

func (owner *linuxV2ProcessOwner) stopForTermination() error {
	ctx, cancel := context.WithTimeout(context.Background(), linuxV2StopTimeout)
	defer cancel()
	_, err := owner.stop(ctx)
	return err
}

func (owner *linuxV2ProcessOwner) stop(ctx context.Context) (*platform.LinuxOwnerHelperV2StopEvidence, error) {
	owner.stopMu.Lock()
	defer owner.stopMu.Unlock()
	if owner.stopEvidence != nil {
		copy := cloneLinuxV2StopEvidence(owner.stopEvidence)
		return &copy, nil
	}
	owner.mu.Lock()
	if owner.ownerReleased {
		owner.mu.Unlock()
		return nil, nil
	}
	owner.stopRequested = true
	helper := owner.helper
	owner.mu.Unlock()
	if helper == nil || helper.Control == nil {
		return nil, errors.New("Linux v2 owner helper identity/control is unavailable; retain reservation and workspace")
	}
	evidence, firstErr := helper.Control.Stop(ctx)
	if firstErr != nil {
		owner.captureWitnessDropStatus()
		var err error
		evidence, err = helper.Control.Stop(ctx)
		if err != nil {
			helperIdentity := authority.LinuxCgroupProcessIdentity{PID: helper.PID, Identity: helper.Identity}
			exited, pollErr := linuxV2PIDFDExited(helper.PIDFD)
			if pollErr == nil && exited {
				mirrorEvidence, mirrorErr := owner.stopAsMirror(ctx, helperIdentity)
				if mirrorErr == nil {
					owner.stopEvidence = &mirrorEvidence
					return owner.stopEvidence, nil
				}
				return nil, errors.Join(fmt.Errorf("Linux v2 helper Stop failed: %w", errors.Join(firstErr, err)),
					fmt.Errorf("exact anchor mirror takeover failed: %w", mirrorErr))
			}
			if pollErr != nil {
				return nil, errors.Join(fmt.Errorf("Linux v2 helper Stop failed: %w", errors.Join(firstErr, err)),
					fmt.Errorf("probe exact helper pidfd for takeover: %w", pollErr))
			}
			return nil, fmt.Errorf("Linux v2 helper could not prove the exact run leaf stopped: %w", errors.Join(firstErr, err))
		}
	}
	copy := cloneLinuxV2StopEvidence(&evidence)
	owner.stopEvidence = &copy
	return owner.stopEvidence, nil
}

func (owner *linuxV2ProcessOwner) stopAsMirror(ctx context.Context, helper authority.LinuxCgroupProcessIdentity) (platform.LinuxOwnerHelperV2StopEvidence, error) {
	if owner.leaf == nil || owner.gate == nil || owner.reservation.Launcher == nil || owner.reservation.LauncherExit == nil ||
		owner.reservation.Helper == nil || !owner.reservation.Helper.Equal(helper) || owner.anchor == nil ||
		owner.anchorID.Process.Validate() != nil || owner.reservation.Mirror == nil || owner.grant.Validate() != nil {
		return platform.LinuxOwnerHelperV2StopEvidence{}, errors.New("Linux v2 mirror lacks the exact helper, gate, launcher, leaf, or anchor")
	}
	owner.mu.Lock()
	releaseAttempted := owner.releaseAttempted
	owner.mu.Unlock()
	reaped, err := owner.anchor.WaitReapRun(ctx, owner.grant, helper)
	if err != nil {
		return platform.LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("anchor could not prove run-wide exact wait/reap after helper death: %w", err)
	}
	anchorStop := reaped.Stop.Clone()
	if err := anchorStop.ValidateFor(owner.grant, helper, owner.anchorID.Process, reaped.Processes, true); err != nil {
		return platform.LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("anchor run-wide result lacks exact physical-stop evidence: %w", err)
	}
	if anchorStop.HelperExit == nil {
		return platform.LinuxOwnerHelperV2StopEvidence{}, errors.New("anchor run-wide stop evidence lacks the exact helper wait/reap")
	}
	helperExit := *cloneLinuxV2Exit(anchorStop.HelperExit)
	if err := owner.leaf.WaitEmpty(ctx); err != nil {
		return platform.LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("mirror could not independently recheck the exact stopped cgroup leaf: %w", err)
	}
	var gateEvidence *platform.LinuxOwnerAnchorReapEvidence
	for index := range reaped.Processes {
		item := &reaped.Processes[index]
		if item.ProcessRole == "gate" || item.ProcessRole == "native" {
			if !linuxV2MirrorGateReapRoleAllowed(item.Process, owner.gate.Process, item.ProcessRole,
				releaseAttempted, anchorStop.GateReleaseAuthorized) {
				return platform.LinuxOwnerHelperV2StopEvidence{}, errors.New("anchor reaped a gate/native identity outside the exact same-PID release transition")
			}
			if gateEvidence != nil {
				return platform.LinuxOwnerHelperV2StopEvidence{}, errors.New("anchor returned duplicate gate reaping evidence")
			}
			gateEvidence = item
		}
	}
	var gateExit authority.LinuxCgroupExitProof
	if gateEvidence != nil {
		gateExit = gateEvidence.ProcessExit
	} else {
		owner.waitMu.Lock()
		if owner.waitResult != nil {
			gateExit = owner.waitResult.Proof
		}
		owner.waitMu.Unlock()
	}
	if gateExit.Validate() != nil || !gateExit.ValidForOwnedDescendant(owner.gate.Process, helper, owner.anchorID.Process, &helperExit) {
		return platform.LinuxOwnerHelperV2StopEvidence{}, errors.New("no exact helper-owned or anchor-adopted gate wait/reap proof exists for mirror receipt")
	}
	if owner.reservation.HelperExit == nil {
		if err := runLinuxV2JournalMutation(func(expected state.LinuxCgroupReservation) error {
			return owner.invocation.Callbacks.RecordHelperExit(expected, helperExit)
		}, owner.reservation); err != nil {
			return platform.LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("durably record anchor's exact helper wait before mirror receipt: %w", err)
		}
		copy := *cloneLinuxV2Exit(&helperExit)
		owner.reservation.HelperExit = &copy
	} else if !owner.reservation.HelperExit.Equal(helperExit) {
		return platform.LinuxOwnerHelperV2StopEvidence{}, errors.New("anchor helper wait differs from the exact durable helper exit")
	}
	launcherExit := *owner.reservation.LauncherExit
	helperExitCopy := *cloneLinuxV2Exit(&helperExit)
	proof := authority.LinuxCgroupStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofMirror,
		Issuer: *owner.reservation.Mirror, Reaper: owner.anchorID.Process,
		PinnedLeafFD: anchorStop.PinnedLeafFD, CgroupKillAcknowledged: anchorStop.KillAcknowledged,
		CgroupPopulatedZero: anchorStop.CgroupPopulatedZero, NoMigrationObserved: anchorStop.NoMigrationObserved,
		ProducerFenced: anchorStop.ProducerFenced, DescendantsReaped: anchorStop.DescendantsReaped,
		LauncherExit: launcherExit, GateExit: gateExit, HelperExit: &helperExitCopy,
	}
	evidence := platform.LinuxOwnerHelperV2StopEvidence{
		Version: 1, RunKey: owner.grant.RunKey, Fence: owner.grant.Fence,
		LaunchToken: owner.grant.LaunchToken, Parent: owner.grant.Parent, Leaf: owner.grant.Leaf,
		Anchor: owner.grant.Anchor, Helper: helper, Launcher: *owner.reservation.Launcher,
		AnchorStop: &anchorStop, RunReapEvidence: reaped.Processes,
		Gate: *owner.gate, Proof: proof, DescendantCount: anchorStop.ReapCount,
		DescendantDigest: anchorStop.ReapDigest,
	}
	if err := evidence.ValidateMirrorFor(owner.grant, helper, *owner.reservation.Mirror, *owner.reservation.Launcher,
		launcherExit, *owner.gate); err != nil {
		return platform.LinuxOwnerHelperV2StopEvidence{}, fmt.Errorf("validate exact cgroup-v2 mirror physical-stop evidence: %w", err)
	}
	return evidence, nil
}

func linuxV2MirrorGateReapRoleAllowed(
	process authority.LinuxCgroupProcessIdentity,
	expected authority.LinuxCgroupProcessIdentity,
	role string,
	releaseAttempted bool,
	gateReleaseAuthorized bool,
) bool {
	if !process.Equal(expected) {
		return false
	}
	switch role {
	case "gate":
		// A failed RELEASE attempt may still leave the original A gate to reap.
		return true
	case "native":
		// The same PID is native only after both caller attempt and anchor auth.
		return releaseAttempted && gateReleaseAuthorized
	default:
		return false
	}
}

func linuxV2PIDFDExited(pidfd *os.File) (bool, error) {
	if pidfd == nil {
		return false, errors.New("exact helper pidfd is unavailable")
	}
	poll := []unix.PollFd{{Fd: int32(pidfd.Fd()), Events: unix.POLLIN}}
	for {
		poll[0].Revents = 0
		n, err := unix.Poll(poll, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return false, err
		}
		return n > 0 && poll[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0, nil
	}
}

func waitLinuxV2PIDFD(ctx context.Context, pidfd *os.File) error {
	if ctx == nil || pidfd == nil {
		return errors.New("exact helper pidfd wait is incomplete")
	}
	poll := []unix.PollFd{{Fd: int32(pidfd.Fd()), Events: unix.POLLIN}}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := unix.Poll(poll, 100)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return err
		}
		if n > 0 && poll[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0 {
			return nil
		}
	}
}

func (owner *linuxV2ProcessOwner) finalize() error {
	ctx, cancel := context.WithTimeout(context.Background(), linuxV2StopTimeout)
	defer cancel()
	if owner.gate == nil {
		if owner.noGateOutcome != nil {
			return owner.finalizePreGate(ctx)
		}
		owner.mu.Lock()
		helperStarted := owner.helperStartAttempted
		helperAvailable := owner.helper != nil && owner.helper.Control != nil
		owner.mu.Unlock()
		if helperStarted && helperAvailable {
			evidence, err := owner.stop(ctx)
			if err != nil {
				return err
			}
			if evidence.PreGate != nil && !evidence.PreGate.PrepareAttempted {
				return owner.finalizeNoPrepareAbort(ctx, evidence)
			}
			return errors.New("Linux v2 root launcher outcome is not authenticated as never-created; retain reservation and pinned owner")
		}
		if owner.canAbortWithoutProducer() {
			return owner.abortBeforeProducer(ctx)
		}
		return errors.New("Linux v2 producer or gate outcome is unresolved; retain journal, cgroup pins, and workspace")
	}
	if err := owner.ensureGateHandoff(); err != nil {
		return err
	}
	if err := owner.ensureCommitReadback(); err != nil {
		return err
	}
	evidence, err := owner.stop(ctx)
	if err != nil {
		return err
	}
	if evidence.PreGate != nil {
		return errors.New("Linux v2 helper reported a pre-gate stop for a durably gate-bound RunJournal")
	}
	owner.mu.Lock()
	handoff := cloneSupervisorHandoff(owner.handoff)
	helper := owner.helper
	receiptAcked := owner.stopReceiptAcked
	normalRelease := owner.normalRelease
	clearDone := owner.clearDone
	helperReleased := owner.helperReleased
	anchorRetired := owner.anchorRetired
	owner.mu.Unlock()
	if handoff == nil || evidence.Proof.ValidateFor(owner.supervisorForProof(evidence.Proof.LauncherExit)) != nil {
		return errors.New("Linux v2 stop proof does not match the exact bound handoff")
	}
	baseHandoff := handoff.Clone()
	var receipt *authority.StopReceipt
	receiptPersisted := baseHandoff.StopReceipt != nil
	if receiptPersisted {
		copy := *baseHandoff.StopReceipt
		receipt = &copy
		baseHandoff.StopReceipt = nil
	}
	candidate := linuxV2StopReceipt(baseHandoff, evidence.Proof)
	if receipt != nil && !receipt.Equal(candidate) {
		return errors.New("local Linux v2 stop receipt conflicts with the cached physical-stop evidence")
	}
	if receipt == nil {
		receipt = &candidate
	}
	if !receipt.ValidForHandoff(baseHandoff) {
		return errors.New("Linux v2 helper stop evidence did not produce an exact authority receipt")
	}
	mirrorStop := evidence.Proof.Kind == authority.LinuxCgroupStopProofMirror
	if !receiptPersisted {
		if err := runLinuxV2ReceiptMutation(owner.invocation.Callbacks.RecordStopReceipt, baseHandoff, *receipt); err != nil {
			return fmt.Errorf("persist exact Linux v2 physical-stop receipt: %w", err)
		}
		handoffWithReceipt := baseHandoff.Clone()
		receiptCopy := *receipt
		handoffWithReceipt.StopReceipt = &receiptCopy
		owner.mu.Lock()
		owner.handoff = &handoffWithReceipt
		owner.mu.Unlock()
		handoff = &handoffWithReceipt
	} else {
		handoff.StopReceipt = receipt
	}
	if !mirrorStop && !receiptAcked {
		var firstAckError error
		ackErr := retryLinuxV2(func() error {
			err := helper.Control.AcknowledgeStopReceipt(ctx, *receipt)
			if err != nil && firstAckError == nil {
				firstAckError = err
			}
			return err
		})
		if firstAckError != nil {
			owner.captureWitnessDropStatus()
		}
		if ackErr != nil {
			return fmt.Errorf("acknowledge durable Linux v2 stop receipt before release: %w", ackErr)
		}
		owner.mu.Lock()
		owner.stopReceiptAcked = true
		owner.mu.Unlock()
	}
	if normalRelease == nil {
		release, err := owner.removeAfterReceipt(ctx, *handoff, *receipt)
		if err != nil {
			return err
		}
		normalRelease = &release
		owner.mu.Lock()
		owner.normalRelease = normalRelease
		owner.mu.Unlock()
	}
	releaseProof, issued := normalRelease.Snapshot()
	if !issued || !releaseProof.ValidFor(*handoff) {
		return errors.New("Linux v2 platform release capability does not match the durable receipt")
	}
	if err := owner.promoteCloneAfterStop(ctx, *receipt, *normalRelease); err != nil {
		return err
	}
	if !clearDone {
		if err := owner.clearAfterReceipt(*handoff, *normalRelease); err != nil {
			return err
		}
		owner.mu.Lock()
		owner.clearDone = true
		owner.mu.Unlock()
	}
	if !mirrorStop && !helperReleased {
		if err := retryLinuxV2(func() error { return helper.Control.ReleaseAfterClear(ctx, releaseProof) }); err != nil {
			// A lost helper response is resolved only by the anchor's exact
			// receipt-bound retirement readback; no release is retried through a
			// replacement helper or numeric PID.
			if retireErr := owner.retireAnchorRun(ctx, *handoff, releaseProof); retireErr != nil {
				return errors.Join(fmt.Errorf("release Linux v2 helper only after opaque Store clear: %w", err),
					fmt.Errorf("read back exact anchor retirement after lost helper release ACK: %w", retireErr))
			}
			owner.mu.Lock()
			owner.helperReleased = true
			owner.anchorRetired = true
			owner.mu.Unlock()
			helperReleased = true
			anchorRetired = true
		} else {
			owner.mu.Lock()
			owner.helperReleased = true
			owner.mu.Unlock()
			helperReleased = true
		}
	}
	if !anchorRetired {
		if err := owner.retireAnchorRun(ctx, *handoff, releaseProof); err != nil {
			return err
		}
		owner.mu.Lock()
		owner.anchorRetired = true
		owner.mu.Unlock()
	}
	owner.mu.Lock()
	owner.ownerReleased = true
	owner.mu.Unlock()
	return nil
}

func (owner *linuxV2ProcessOwner) promoteCloneAfterStop(ctx context.Context, stopReceipt authority.StopReceipt, release platform.LinuxCgroupV2Release) error {
	owner.mu.Lock()
	grant := owner.cloneGrant
	if grant == nil {
		owner.mu.Unlock()
		return nil
	}
	grantCopy := grant.Clone()
	handoff := cloneSupervisorHandoff(owner.handoff)
	checkout := cloneLinuxV2CheckoutReceipt(owner.cloneCheckout)
	runOutcome := owner.cloneRunOutcome
	runContext := owner.runContext
	outcomeKnown := owner.cloneOutcomeKnown
	proposed := owner.cloneProposed
	proposedCommit := owner.cloneProposedCommit
	retained := owner.cloneRetained
	promotion := cloneLinuxV2PromotionReceipt(owner.clonePromotion)
	promotionDone := owner.clonePromotionDone
	owner.mu.Unlock()
	releaseProof, issued := release.Snapshot()
	if !issued || handoff == nil || !stopReceipt.ValidForHandoff(*handoff) || handoff.StopReceipt == nil ||
		!handoff.StopReceipt.Equal(stopReceipt) || !releaseProof.ValidFor(*handoff) {
		return errors.New("Linux clone promotion requires the exact durable stop receipt and opaque removed-leaf release")
	}
	if promotionDone {
		return nil
	}
	if runOutcome == nil {
		return errors.New("Linux clone promotion requires Runner's completed backend outcome")
	}
	promotionContext, cancelPromotion := context.WithCancel(ctx)
	stopRunCancellation := func() bool { return true }
	if runContext != nil {
		stopRunCancellation = context.AfterFunc(runContext, cancelPromotion)
	}
	defer func() {
		stopRunCancellation()
		cancelPromotion()
	}()
	outcome := *runOutcome
	outcome.StopReceiptValid = true
	outcome.LeafRemovalProven = true
	if runContext != nil && runContext.Err() != nil {
		outcome.RunContextCancelled = true
	}
	if checkout != nil {
		if err := validateLinuxCloneCheckoutReceipt(grantCopy, *checkout); err != nil {
			return fmt.Errorf("validate durable B checkout before clone promotion: %w", err)
		}
	}
	promotionAlreadyApplied := promotion != nil
	if promotionAlreadyApplied {
		expectedRef, refErr := authority.LinuxClonePromotionRef(grantCopy.Intent.RunID, grantCopy.Intent.Generation)
		if checkout == nil || promotion.Validate() != nil || promotion.Ref != expectedRef ||
			proposedCommit != "" && promotion.Commit != proposedCommit {
			return errors.Join(errors.New("cached Linux clone promotion differs from its exact checkout and RunKey"), refErr)
		}
		proposed, proposedCommit = true, promotion.Commit
	}
	if !outcomeKnown && !promotionAlreadyApplied {
		commit, candidate, err := owner.invocation.Callbacks.LinuxCloneProposedCommit(promotionContext, outcome)
		if err != nil {
			if runContext == nil || runContext.Err() == nil {
				return fmt.Errorf("read final Linux clone candidate commit: %w", err)
			}
			commit, candidate = "", false
		}
		if runContext != nil && runContext.Err() != nil {
			outcome.RunContextCancelled = true
			candidate = false
			commit = ""
		}
		if !outcome.PromotionEligible() && candidate {
			return errors.New("Linux clone candidate was proposed for a failed, cancelled, noisy, or unproven run")
		}
		if candidate && commit == "" || !candidate && commit != "" {
			return errors.New("Linux clone proposed-commit callback returned an inconsistent candidate state")
		}
		owner.mu.Lock()
		owner.cloneOutcomeKnown = true
		owner.cloneProposed = candidate
		owner.cloneProposedCommit = commit
		owner.mu.Unlock()
		proposed, proposedCommit = candidate, commit
	}
	if runContext != nil && runContext.Err() != nil {
		outcome.RunContextCancelled = true
	}
	if !outcome.PromotionEligible() && !promotionAlreadyApplied {
		proposed, proposedCommit = false, ""
		owner.mu.Lock()
		owner.cloneOutcomeKnown = true
		owner.cloneProposed = false
		owner.cloneProposedCommit = ""
		owner.mu.Unlock()
	}
	if checkout == nil {
		if proposed {
			return errors.New("Linux clone cannot propose a commit without a durable CheckoutReady receipt")
		}
		if !retained {
			if err := owner.retainUnpromotedClone(ctx); err != nil {
				return fmt.Errorf("retain Linux clone with no durable CheckoutReady receipt: %w", err)
			}
		}
		return nil
	}
	if !proposed && !promotionAlreadyApplied {
		if !retained {
			if err := owner.retainUnpromotedClone(ctx); err != nil {
				return fmt.Errorf("durably retain unpromoted B-owned Linux clone: %w", err)
			}
		}
		return nil
	}
	if proposedCommit == "" {
		return errors.New("Linux clone candidate has no commit identity")
	}
	if !promotionAlreadyApplied && runContext != nil && runContext.Err() != nil {
		owner.mu.Lock()
		owner.cloneOutcomeKnown = true
		owner.cloneProposed = false
		owner.cloneProposedCommit = ""
		owner.mu.Unlock()
		return owner.retainUnpromotedClone(ctx)
	}
	if promotion == nil {
		created, err := owner.invocation.Callbacks.PromoteLinuxClone(promotionContext, grantCopy, *checkout,
			proposedCommit, stopReceipt, release)
		if err != nil {
			return fmt.Errorf("validate and promote stopped B-owned Linux clone: %w", err)
		}
		expectedRef, refErr := authority.LinuxClonePromotionRef(grantCopy.Intent.RunID, grantCopy.Intent.Generation)
		if refErr != nil || created.Validate() != nil || created.Commit != proposedCommit || created.Ref != expectedRef {
			return errors.Join(errors.New("Linux clone promotion receipt differs from the exact proposed commit and RunKey ref"), refErr)
		}
		promotion = &created
		owner.mu.Lock()
		copy := created.Clone()
		owner.clonePromotion = &copy
		owner.mu.Unlock()
	}
	if err := runLinuxV2MutationRetry(func() error {
		return owner.invocation.Callbacks.RecordLinuxClonePromotion(promotion.Clone())
	}); err != nil {
		return fmt.Errorf("persist A-owned Linux clone promotion receipt: %w", err)
	}
	owner.mu.Lock()
	owner.clonePromotionDone = true
	owner.mu.Unlock()
	return nil
}

func (owner *linuxV2ProcessOwner) retainUnpromotedClone(ctx context.Context) error {
	owner.mu.Lock()
	if owner.cloneGrant == nil || owner.cloneRetained {
		owner.mu.Unlock()
		return nil
	}
	owner.mu.Unlock()
	if err := retryLinuxV2(func() error { return owner.invocation.Callbacks.RetainUnpromotedLinuxClone(ctx) }); err != nil {
		return err
	}
	owner.mu.Lock()
	owner.cloneRetained = true
	owner.mu.Unlock()
	return nil
}

func (owner *linuxV2ProcessOwner) recordHelperExitBeforeNoPrepareAbort(
	reservation state.LinuxCgroupReservation,
	helperExit authority.LinuxCgroupExitProof,
) (state.LinuxCgroupReservation, error) {
	if reservation.Helper == nil || !helperExit.ValidFor(*reservation.Helper, reservation.Anchor) {
		return state.LinuxCgroupReservation{}, errors.New("no-Prepare helper wait proof differs from the exact reserved helper and anchor")
	}
	if reservation.HelperExit != nil {
		if !reservation.HelperExit.Equal(helperExit) {
			return state.LinuxCgroupReservation{}, errors.New("no-Prepare helper wait differs from the durable reservation proof")
		}
		return reservation, nil
	}
	if err := runLinuxV2JournalMutation(func(expected state.LinuxCgroupReservation) error {
		return owner.invocation.Callbacks.RecordHelperExit(expected, helperExit)
	}, reservation); err != nil {
		return state.LinuxCgroupReservation{}, fmt.Errorf("durably record exact no-Prepare helper wait before leaf removal: %w", err)
	}
	copy := helperExit
	reservation.HelperExit = &copy
	owner.mu.Lock()
	if owner.reservation.HelperExit != nil && !owner.reservation.HelperExit.Equal(helperExit) {
		owner.mu.Unlock()
		return state.LinuxCgroupReservation{}, errors.New("cached no-Prepare helper wait differs from its durable proof")
	}
	owner.reservation.HelperExit = &copy
	owner.mu.Unlock()
	return reservation, nil
}

func (owner *linuxV2ProcessOwner) abortNoPrepareReservationAfterHelperExit(
	ctx context.Context,
	reservation state.LinuxCgroupReservation,
	helperExit authority.LinuxCgroupExitProof,
	cachedProof *authority.LinuxCgroupReservationAbortProof,
	removeLeaf func(context.Context, state.LinuxCgroupReservation, authority.LinuxCgroupExitProof) (authority.LinuxCgroupReservationAbortProof, error),
) (state.LinuxCgroupReservation, authority.LinuxCgroupReservationAbortProof, error) {
	reservation, err := owner.recordHelperExitBeforeNoPrepareAbort(reservation, helperExit)
	if err != nil {
		return state.LinuxCgroupReservation{}, authority.LinuxCgroupReservationAbortProof{}, err
	}
	var proof authority.LinuxCgroupReservationAbortProof
	if cachedProof != nil {
		proof = cachedProof.Clone()
	} else {
		if removeLeaf == nil {
			return state.LinuxCgroupReservation{}, authority.LinuxCgroupReservationAbortProof{}, errors.New("no-Prepare abort leaf remover is unavailable")
		}
		proof, err = removeLeaf(ctx, reservation.Clone(), helperExit)
		if err != nil {
			return state.LinuxCgroupReservation{}, authority.LinuxCgroupReservationAbortProof{}, err
		}
	}
	if reservation.Mirror == nil || !proof.ValidFor(reservation.LaunchToken, reservation.Parent, reservation.LeafName,
		reservation.CreateStarted, reservation.OwnerUID, reservation.Anchor, *reservation.Mirror,
		reservation.Helper, reservation.Launcher) {
		return state.LinuxCgroupReservation{}, authority.LinuxCgroupReservationAbortProof{}, errors.New("no-Prepare leaf removal proof differs from the helper-exit-bound reservation")
	}
	if cachedProof == nil {
		owner.mu.Lock()
		copy := proof.Clone()
		owner.abortProof = &copy
		owner.mu.Unlock()
	}
	if err := runLinuxV2MutationRetry(func() error {
		return owner.invocation.Callbacks.AbortReservation(reservation.Clone(), proof.Clone())
	}); err != nil {
		return state.LinuxCgroupReservation{}, authority.LinuxCgroupReservationAbortProof{}, err
	}
	return reservation, proof, nil
}

func (owner *linuxV2ProcessOwner) finalizeNoCheckoutCloneAfterRetirement(
	ctx context.Context,
	preGateProof *authority.LinuxCgroupPreGateReleaseProof,
	cgroupAbortProof *authority.LinuxCgroupReservationAbortProof,
) error {
	if ctx == nil {
		return errors.New("Linux clone abort finalization context is nil")
	}
	owner.mu.Lock()
	grant := owner.cloneGrant
	if grant == nil || owner.cloneAbortDone {
		owner.mu.Unlock()
		return nil
	}
	grantCopy := grant.Clone()
	reservation := owner.reservation.Clone()
	callback := owner.invocation.Callbacks.FinalizeNoCheckoutLinuxCloneAfterRetirement
	helperStarted := owner.helperStartAttempted
	helperReleased := owner.helperReleased
	anchorRetired := owner.anchorRetired
	ownerReleased := owner.ownerReleased
	owner.mu.Unlock()
	if (preGateProof == nil) == (cgroupAbortProof == nil) {
		return errors.New("Linux clone abort finalization requires exactly one pre-gate or cgroup-abort proof")
	}
	if err := grantCopy.Validate(); err != nil {
		return fmt.Errorf("validate exact no-checkout Linux clone grant: %w", err)
	}
	if callback == nil {
		return errors.New("Linux clone post-retirement abort callback is unavailable")
	}
	if !anchorRetired || helperStarted && !helperReleased || ownerReleased {
		return errors.New("Linux clone abort finalization requires exact helper/anchor retirement before owner close")
	}
	var preGateCopy *authority.LinuxCgroupPreGateReleaseProof
	var abortCopy *authority.LinuxCgroupReservationAbortProof
	if preGateProof != nil {
		if reservation.PreGateStopReceipt == nil || reservation.Gate != nil ||
			!preGateProof.ValidFor(*reservation.PreGateStopReceipt) {
			return errors.New("Linux clone abort pre-gate release proof differs from the exact never-produced receipt")
		}
		copy := preGateProof.Clone()
		preGateCopy = &copy
	} else {
		if reservation.Mirror == nil || reservation.AbortProof == nil || !reservation.AbortProof.Equal(*cgroupAbortProof) ||
			!cgroupAbortProof.ValidFor(reservation.LaunchToken, reservation.Parent, reservation.LeafName,
				reservation.CreateStarted, reservation.OwnerUID, reservation.Anchor, *reservation.Mirror,
				reservation.Helper, reservation.Launcher) {
			return errors.New("Linux clone cgroup-abort proof differs from the exact durable no-producer tombstone")
		}
		copy := cgroupAbortProof.Clone()
		abortCopy = &copy
	}
	intent := grantCopy.Intent.Clone()
	seed := grantCopy.Seed.Clone()
	if err := callback(ctx, intent, &seed, preGateCopy, abortCopy); err != nil {
		return fmt.Errorf("finalize exact no-checkout Linux clone after producer retirement: %w", err)
	}
	owner.mu.Lock()
	owner.cloneAbortDone = true
	owner.mu.Unlock()
	return nil
}

func (owner *linuxV2ProcessOwner) finalizeNoPrepareAbort(ctx context.Context, evidence *platform.LinuxOwnerHelperV2StopEvidence) error {
	if evidence == nil || evidence.PreGate == nil || evidence.PreGate.PrepareAttempted || !evidence.PreGate.NoProducerCanAct {
		return errors.New("Linux v2 no-prepare abort lacks exact helper producer-fence evidence")
	}
	owner.mu.Lock()
	reservation := owner.reservation.Clone()
	helper := owner.helper
	proofCached := owner.abortProof
	helperFenced := owner.helperFencedBeforeAbort
	owner.mu.Unlock()
	if reservation.AbortProof != nil {
		if proofCached == nil || !reservation.AbortProof.Equal(*proofCached) {
			return errors.New("Linux v2 no-Prepare abort tombstone differs from the exact cached removal proof")
		}
		reservation.AbortProof = nil
	}
	if reservation.LeafIdentity == nil || reservation.Mirror == nil || reservation.Helper == nil ||
		reservation.Launcher != nil || reservation.LauncherGrantPossible || reservation.Gate != nil ||
		helper == nil {
		return errors.New("Linux v2 no-prepare reservation contains a launcher, gate, or incomplete helper binding")
	}
	helperIdentity := *reservation.Helper
	if !helperIdentity.Equal(authority.LinuxCgroupProcessIdentity{PID: helper.PID, Identity: helper.Identity}) {
		return errors.New("Linux v2 no-prepare helper identity differs from the durable reservation")
	}
	var helperExit authority.LinuxCgroupExitProof
	if proofCached != nil {
		if proofCached.HelperExit == nil {
			return errors.New("cached no-Prepare cgroup abort lacks its exact helper wait proof")
		}
		helperExit = *proofCached.HelperExit
	} else {
		if !helperFenced {
			fenceEvidence, err := helper.Control.FenceBeforeAbort(ctx)
			if err == nil {
				if err := fenceEvidence.ValidateFor(owner.grant, helperIdentity); err != nil ||
					fenceEvidence.PrepareAttempted || !fenceEvidence.NoProducerCanAct {
					return errors.Join(errors.New("helper did not prove the exact no-Prepare abort boundary"), err)
				}
			} else if _, waitErr := owner.anchor.WaitHelper(ctx, owner.grant, helperIdentity); waitErr != nil {
				return errors.Join(fmt.Errorf("fence helper after exact no-Prepare stop before reservation abort: %w", err),
					fmt.Errorf("resolve lost fence ACK with exact anchor helper reap: %w", waitErr))
			}
			owner.mu.Lock()
			owner.helperFencedBeforeAbort = true
			owner.mu.Unlock()
		}
		var err error
		helperExit, err = owner.anchor.WaitHelper(ctx, owner.grant, helperIdentity)
		if err != nil {
			return fmt.Errorf("anchor could not reap exact no-Prepare helper before abort: %w", err)
		}
	}
	if !helperExit.ValidFor(helperIdentity, reservation.Anchor) {
		return errors.New("anchor wait result does not prove reaping the exact no-Prepare helper")
	}
	if reservation.HelperExit != nil && !reservation.HelperExit.Equal(helperExit) {
		return errors.New("anchor helper exit differs from its previously retained wait proof")
	}
	var err error
	var removeLeaf func(context.Context, state.LinuxCgroupReservation, authority.LinuxCgroupExitProof) (authority.LinuxCgroupReservationAbortProof, error)
	if proofCached == nil {
		removeLeaf = func(ctx context.Context, reservation state.LinuxCgroupReservation, helperExit authority.LinuxCgroupExitProof) (authority.LinuxCgroupReservationAbortProof, error) {
			helperExitCopy := helperExit
			authorization := platform.LinuxCgroupV2AbortAuthorization{
				LaunchToken: reservation.LaunchToken, OwnerUID: reservation.OwnerUID,
				ReservedLeafIdentity: *reservation.LeafIdentity, CreateStarted: reservation.CreateStarted,
				LauncherGrantPossible: false, Anchor: reservation.Anchor, Mirror: *reservation.Mirror,
				HelperWasCreated: true, Helper: helperIdentity, HelperExit: &helperExitCopy,
				CreatorFenced: true, NoProducerCanAct: true,
			}
			return owner.leaf.RemoveEmptyReservationAfterAbort(ctx, authorization)
		}
	}
	reservation, proof, err := owner.abortNoPrepareReservationAfterHelperExit(ctx, reservation, helperExit, proofCached, removeLeaf)
	if err != nil {
		return fmt.Errorf("complete exact no-Prepare helper-exit/leaf-abort Store sequence: %w", err)
	}
	if err := retryLinuxV2(func() error {
		return owner.anchor.RetireAbortedHelper(ctx, owner.grant.RunKey, owner.grant.Fence, helperIdentity, proof)
	}); err != nil {
		return fmt.Errorf("retire anchor helper only after durable no-Prepare abort: %w", err)
	}
	owner.mu.Lock()
	owner.reservation.AbortProof = &proof
	owner.clearDone = true
	owner.helperReleased = true
	owner.anchorRetired = true
	owner.mu.Unlock()
	if err := owner.finalizeNoCheckoutCloneAfterRetirement(ctx, nil, &proof); err != nil {
		return fmt.Errorf("finalize no-checkout Linux clone after exact aborted-helper retirement: %w", err)
	}
	owner.mu.Lock()
	owner.ownerReleased = true
	owner.mu.Unlock()
	return nil
}

func (owner *linuxV2ProcessOwner) ensureGateHandoff() error {
	owner.mu.Lock()
	gate := cloneLinuxV2Gate(owner.gate)
	launcherExit := cloneLinuxV2Exit(owner.launcherExit)
	gateBound := owner.gateBound
	owner.mu.Unlock()
	if gate == nil || launcherExit == nil {
		return errors.New("Linux v2 gate or exact launcher wait proof is missing")
	}
	if !gateBound {
		if err := owner.bindGate(); err != nil {
			return fmt.Errorf("replay exact Linux v2 gate bind before stop receipt: %w", err)
		}
	}
	owner.mu.Lock()
	handoffBound := owner.handoffBound
	owner.mu.Unlock()
	if !handoffBound {
		if err := owner.bindHandoff(); err != nil {
			return fmt.Errorf("replay exact Linux v2 handoff bind before stop receipt: %w", err)
		}
	}
	return nil
}

func (owner *linuxV2ProcessOwner) ensureCommitReadback() error {
	owner.mu.Lock()
	tried := owner.handoffCommitTried
	committed := owner.journalCommitted
	handoff := cloneSupervisorHandoff(owner.handoff)
	gate := cloneLinuxV2Gate(owner.gate)
	owner.mu.Unlock()
	if !tried || committed {
		return nil
	}
	if handoff == nil || gate == nil {
		return errors.New("Linux v2 handoff commit outcome is unknown without the exact gate and handoff")
	}
	if err := runLinuxV2MutationRetry(func() error {
		return owner.invocation.Callbacks.CommitHandoff(handoff.Clone(), gate.ObservedAt)
	}); err != nil {
		return fmt.Errorf("replay exact Linux v2 handoff commit after unknown acknowledgement: %w", err)
	}
	owner.mu.Lock()
	owner.journalCommitted = true
	owner.mu.Unlock()
	return nil
}

func (owner *linuxV2ProcessOwner) supervisorForProof(launcherExit authority.LinuxCgroupExitProof) authority.Supervisor {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.handoff == nil {
		return authority.Supervisor{}
	}
	value, err := owner.handoff.ToSupervisor()
	if err != nil {
		return authority.Supervisor{}
	}
	if value.LauncherExit == nil || !value.LauncherExit.Equal(launcherExit) {
		return authority.Supervisor{}
	}
	return value
}

func (owner *linuxV2ProcessOwner) removeAfterReceipt(ctx context.Context, handoff authority.SupervisorHandoff, receipt authority.StopReceipt) (platform.LinuxCgroupV2Release, error) {
	authorization := platform.LinuxCgroupV2RemovalAuthorization{Handoff: &handoff, Receipt: receipt}
	release, err := owner.leaf.RemoveAfterStopReceipt(ctx, authorization)
	if err == nil {
		return release, nil
	}
	completed, completeErr := owner.parent.CompleteRemovalAfterStopReceipt(ctx, *handoff.CgroupIdentity, authorization)
	if completeErr == nil {
		return completed, nil
	}
	return platform.LinuxCgroupV2Release{}, errors.Join(fmt.Errorf("remove exact receipt-authorized Linux v2 leaf: %w", err), fmt.Errorf("complete only receipt-backed pinned-parent absence: %w", completeErr))
}

func (owner *linuxV2ProcessOwner) clearAfterReceipt(handoff authority.SupervisorHandoff, release platform.LinuxCgroupV2Release) error {
	owner.mu.Lock()
	committed := owner.journalCommitted
	owner.mu.Unlock()
	if committed {
		if err := runLinuxV2MutationRetry(func() error {
			return owner.invocation.Callbacks.ClearProcessDetailsV2(handoff.TargetPID, handoff.TargetIdentity, release)
		}); err != nil {
			return fmt.Errorf("clear committed Linux v2 process with opaque leaf release: %w", err)
		}
		return nil
	}
	if err := runLinuxV2MutationRetry(func() error {
		return owner.invocation.Callbacks.ClearHandoffV2(handoff, release)
	}); err != nil {
		return fmt.Errorf("clear pending Linux v2 handoff with opaque leaf release: %w", err)
	}
	return nil
}

func (owner *linuxV2ProcessOwner) retireAnchorRun(ctx context.Context, handoff authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) error {
	// The anchor owns an independent helper/leaf pin. It may retire only after
	// State has durably cleared the exact receipt-bound RunJournal entry.
	helper := authority.LinuxCgroupProcessIdentity{PID: handoff.SupervisorPID, Identity: handoff.SupervisorIdentity}
	if err := retryLinuxV2(func() error { return owner.anchor.RetireHelper(ctx, owner.grant, helper, proof) }); err != nil {
		return fmt.Errorf("retire exact cleared Linux v2 anchor run: %w", err)
	}
	return nil
}

func (owner *linuxV2ProcessOwner) finalizePreGate(ctx context.Context) error {
	if !owner.grantArmed || owner.noGateOutcome == nil || owner.reservation.Launcher == nil || owner.reservation.LauncherExit == nil {
		return errors.New("Linux v2 pre-gate stop lacks an armed grant and authenticated never-created outcome")
	}
	evidence, err := owner.stop(ctx)
	if err != nil {
		return err
	}
	if evidence.PreGate == nil || evidence.PreGate.Proof == nil || evidence.PreGate.GateOutcome == nil || evidence.PreGate.LauncherExit == nil {
		return errors.New("Linux v2 helper returned no authenticated pre-gate physical-stop evidence")
	}
	preGate := evidence.PreGate
	if err := preGate.ValidateFor(owner.grant, authority.LinuxCgroupProcessIdentity{PID: owner.helper.PID, Identity: owner.helper.Identity}); err != nil {
		return fmt.Errorf("validate exact Linux v2 pre-gate helper evidence: %w", err)
	}
	if !preGate.GateOutcome.Equal(*owner.noGateOutcome) || !preGate.LauncherExit.Equal(*owner.reservation.LauncherExit) {
		return errors.New("Linux v2 pre-gate stop evidence differs from the durably armed launcher outcome")
	}
	candidate := authority.LinuxCgroupPreGateStopReceipt{
		Version:        authority.LinuxCgroupIdentityVersion,
		LaunchToken:    owner.reservation.LaunchToken,
		CgroupIdentity: *owner.reservation.LeafIdentity,
		OwnerUID:       owner.reservation.OwnerUID, LauncherUID: owner.reservation.LauncherUID,
		Anchor: owner.reservation.Anchor, Helper: *owner.reservation.Helper,
		Launcher: *owner.reservation.Launcher, Mirror: *owner.reservation.Mirror,
		Proof: *preGate.Proof,
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("construct exact authenticated Linux pre-gate stop receipt: %w", err)
	}
	var receipt authority.LinuxCgroupPreGateStopReceipt
	owner.mu.Lock()
	receiptPersisted := owner.reservation.PreGateStopReceipt != nil
	if receiptPersisted {
		receipt = owner.reservation.PreGateStopReceipt.Clone()
	}
	preGateAcked := owner.preGateReceiptAcked
	preGateRelease := owner.preGateRelease
	clearDone := owner.clearDone
	helperReleased := owner.helperReleased
	anchorRetired := owner.anchorRetired
	owner.mu.Unlock()
	if receiptPersisted {
		if !receipt.Equal(candidate) {
			return errors.New("cached Linux v2 pre-gate receipt differs from exact physical-stop evidence")
		}
	} else {
		receipt = candidate
		if err := runLinuxV2PreGateReceiptMutation(owner.invocation.Callbacks.RecordPreGateStopReceipt, owner.reservation, receipt); err != nil {
			return fmt.Errorf("persist authenticated Linux v2 pre-gate stop receipt: %w", err)
		}
		owner.mu.Lock()
		owner.reservation.PreGateStopReceipt = cloneLinuxV2PreGateReceipt(&receipt)
		owner.mu.Unlock()
	}
	if !preGateAcked {
		var firstAckError error
		ackErr := retryLinuxV2(func() error {
			err := owner.helper.Control.AcknowledgePreGateStopReceipt(ctx, receipt)
			if err != nil && firstAckError == nil {
				firstAckError = err
			}
			return err
		})
		if firstAckError != nil {
			owner.captureWitnessDropStatus()
		}
		if ackErr != nil {
			return fmt.Errorf("acknowledge durable Linux v2 pre-gate stop receipt: %w", ackErr)
		}
		owner.mu.Lock()
		owner.preGateReceiptAcked = true
		owner.mu.Unlock()
	}
	// Pre-gate release uses a distinct opaque proof and helper acknowledgement;
	// never pass the gate-bound handoff release method or a generic receipt.
	authorization := platform.LinuxCgroupV2PreGateRemovalAuthorization{
		Receipt: receipt, Secret: owner.reservation.Secret, LauncherGrantPossible: true,
	}
	if preGateRelease == nil {
		release, removeErr := owner.leaf.RemoveAfterPreGateStopReceipt(ctx, authorization)
		if removeErr != nil {
			release, removeErr = owner.parent.CompletePreGateRemovalAfterReceipt(ctx, *owner.reservation.LeafIdentity, authorization)
		}
		if removeErr != nil {
			return fmt.Errorf("remove authenticated no-gate Linux v2 leaf: %w", removeErr)
		}
		preGateRelease = &release
		owner.mu.Lock()
		owner.preGateRelease = preGateRelease
		owner.mu.Unlock()
	}
	if !clearDone {
		if err := runLinuxV2MutationRetry(func() error {
			return owner.invocation.Callbacks.ClearReservationV2(owner.reservation, *preGateRelease)
		}); err != nil {
			return fmt.Errorf("clear exact no-gate Linux v2 reservation with opaque release: %w", err)
		}
		owner.mu.Lock()
		owner.clearDone = true
		owner.mu.Unlock()
	}
	proof, issued := preGateRelease.SnapshotPreGate()
	if !issued || !proof.ValidFor(receipt) {
		return errors.New("Linux v2 pre-gate release capability differs from its receipt")
	}
	if !helperReleased {
		if err := retryLinuxV2(func() error { return owner.helper.Control.ReleaseAfterPreGateClear(ctx, proof) }); err != nil {
			if retireErr := owner.retireAnchorPreGate(ctx, proof); retireErr != nil {
				return errors.Join(fmt.Errorf("release no-gate Linux v2 helper after durable clear: %w", err),
					fmt.Errorf("read back exact anchor pre-gate retirement after lost helper release ACK: %w", retireErr))
			}
			owner.mu.Lock()
			owner.helperReleased = true
			owner.anchorRetired = true
			owner.mu.Unlock()
			helperReleased = true
			anchorRetired = true
		} else {
			owner.mu.Lock()
			owner.helperReleased = true
			owner.mu.Unlock()
			helperReleased = true
		}
	}
	if !anchorRetired {
		if err := owner.retireAnchorPreGate(ctx, proof); err != nil {
			return err
		}
		owner.mu.Lock()
		owner.anchorRetired = true
		owner.mu.Unlock()
	}
	if err := owner.finalizeNoCheckoutCloneAfterRetirement(ctx, &proof, nil); err != nil {
		return fmt.Errorf("finalize no-checkout Linux clone after exact pre-gate retirement: %w", err)
	}
	owner.mu.Lock()
	owner.ownerReleased = true
	owner.mu.Unlock()
	return nil
}

func (owner *linuxV2ProcessOwner) retireAnchorPreGate(ctx context.Context, proof authority.LinuxCgroupPreGateReleaseProof) error {
	if owner.helper == nil {
		return errors.New("Linux v2 pre-gate helper handle is unavailable")
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: owner.helper.PID, Identity: owner.helper.Identity}
	if err := retryLinuxV2(func() error { return owner.anchor.RetirePreGateHelper(ctx, owner.grant, helper, proof) }); err != nil {
		return fmt.Errorf("retire exact cleared Linux v2 pre-gate anchor run: %w", err)
	}
	return nil
}

func (owner *linuxV2ProcessOwner) canAbortWithoutProducer() bool {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return owner.leaf != nil && owner.reservation.LeafIdentity != nil &&
		owner.reservation.Helper == nil && owner.reservation.Launcher == nil &&
		!owner.reservation.LauncherGrantPossible && owner.helper == nil && !owner.helperStartAttempted
}

func (owner *linuxV2ProcessOwner) abortBeforeProducer(ctx context.Context) error {
	owner.mu.Lock()
	reservation := owner.reservation.Clone()
	leaf := owner.leaf
	anchor := owner.anchorID.Process
	cachedProof := owner.abortProof
	owner.mu.Unlock()
	if reservation.AbortProof != nil {
		if cachedProof == nil || !reservation.AbortProof.Equal(*cachedProof) {
			return errors.New("Linux v2 pre-effect abort tombstone differs from the exact cached removal proof")
		}
		reservation.AbortProof = nil
	}
	if reservation.LeafIdentity == nil || reservation.Mirror == nil {
		return errors.New("Linux v2 pre-effect abort is not at the exact reserved-leaf stage")
	}
	authorization := platform.LinuxCgroupV2AbortAuthorization{
		LaunchToken: reservation.LaunchToken, OwnerUID: reservation.OwnerUID,
		ReservedLeafIdentity: *reservation.LeafIdentity, CreateStarted: reservation.CreateStarted,
		LauncherGrantPossible: reservation.LauncherGrantPossible,
		Anchor:                anchor, Mirror: *reservation.Mirror,
		CreatorFenced: true, NoProducerCanAct: true,
	}
	var proof authority.LinuxCgroupReservationAbortProof
	if cachedProof != nil {
		proof = cachedProof.Clone()
	} else {
		measured, err := leaf.RemoveEmptyReservationAfterAbort(ctx, authorization)
		if err != nil {
			return fmt.Errorf("prove exact Linux v2 pre-effect leaf removal: %w", err)
		}
		proof = measured
		owner.mu.Lock()
		copy := measured.Clone()
		owner.abortProof = &copy
		owner.mu.Unlock()
	}
	if err := runLinuxV2MutationRetry(func() error {
		return owner.invocation.Callbacks.AbortReservation(reservation, proof)
	}); err != nil {
		return fmt.Errorf("persist exact Linux v2 no-producer abort tombstone: %w", err)
	}
	owner.mu.Lock()
	owner.reservation.AbortProof = &proof
	owner.clearDone = true
	owner.helperReleased = true
	// StartHelper was never attempted, so the anchor has no per-run helper
	// record to retire. The persisted exact abort proof is the no-producer
	// retirement fence for the clone cleanup callback below.
	owner.anchorRetired = true
	owner.mu.Unlock()
	if err := owner.finalizeNoCheckoutCloneAfterRetirement(ctx, nil, &proof); err != nil {
		return fmt.Errorf("finalize no-checkout Linux clone after exact no-producer abort: %w", err)
	}
	owner.mu.Lock()
	owner.ownerReleased = true
	owner.mu.Unlock()
	return nil
}

func (owner *linuxV2ProcessOwner) duplicatePeer() (*LinuxV2Peer, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	// rootUserNamespaceVerified is set only for a successful parked-gate
	// Prepare result, whose helper compared A's nsfs observation with the
	// authenticated root launcher's nsfs identity. Do not turn a merely nonzero
	// procfs namespace inode into native peer authority.
	prepared := owner.prepared
	gate := owner.gate
	reservationGate := owner.reservation.Gate
	if !owner.rootUserNamespaceVerified || !owner.releaseSucceeded || !owner.helperCommitAck || !owner.leaseAck ||
		owner.stopRequested || owner.ownerReleased || !owner.gateBound || !owner.handoffBound ||
		prepared == nil || prepared.PIDFD == nil || gate == nil || reservationGate == nil ||
		prepared.Gate.Validate() != nil || !prepared.Gate.Equal(*gate) || !reservationGate.Equal(*gate) ||
		gate.Process.Validate() != nil || prepared.UserNamespace.Device == 0 || prepared.UserNamespace.Inode == 0 ||
		prepared.UserNamespace != owner.verifiedUserNamespace {
		return nil, errors.ErrUnsupported
	}
	pidfd, err := unix.FcntlInt(prepared.PIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate root-cross-checked Linux v2 gate pidfd: %w", err)
	}
	file := os.NewFile(uintptr(pidfd), "linux-v2-gate-pidfd")
	if file == nil {
		_ = unix.Close(pidfd)
		return nil, errors.New("duplicate Linux v2 gate pidfd could not be wrapped as an owned file")
	}
	return &LinuxV2Peer{
		PID: gate.Process.PID, Identity: gate.Process.Identity,
		UserNamespace: owner.verifiedUserNamespace, PIDFD: file,
	}, nil
}

func (owner *linuxV2ProcessOwner) witnessDropStatus(ctx context.Context) (LinuxV2WitnessDropStatus, error) {
	if ctx == nil {
		return LinuxV2WitnessDropStatus{}, errors.New("Linux v2 helper witness status context is nil")
	}
	owner.mu.Lock()
	if owner.observedWitnessDrop != nil {
		cached := *owner.observedWitnessDrop
		owner.mu.Unlock()
		return cached, nil
	}
	helper := owner.helper
	grant := owner.grant
	var expectedHelper *authority.LinuxCgroupProcessIdentity
	if owner.reservation.Helper != nil {
		copy := *owner.reservation.Helper
		expectedHelper = &copy
	}
	owner.mu.Unlock()
	if helper == nil || helper.Control == nil || grant.Validate() != nil || expectedHelper == nil ||
		!expectedHelper.Equal(authority.LinuxCgroupProcessIdentity{PID: helper.PID, Identity: helper.Identity}) {
		return LinuxV2WitnessDropStatus{}, errors.ErrUnsupported
	}
	status, err := helper.Control.Status(ctx)
	if err != nil {
		return LinuxV2WitnessDropStatus{}, fmt.Errorf("read exact helper ACK-loss witness status: %w", err)
	}
	defer status.Close()
	if status.RunKey.RunID != grant.RunKey.RunID || status.RunKey.Generation != grant.RunKey.Generation ||
		status.Fence != grant.Fence || !status.Helper.Equal(*expectedHelper) {
		return LinuxV2WitnessDropStatus{}, errors.New("Linux owner helper witness status changed its exact RunKey, fence, or helper")
	}
	validOperation := status.WitnessDropOperation == "stop" || status.WitnessDropOperation == "ack-stop-receipt" ||
		status.WitnessDropOperation == "launcher-ready"
	if status.WitnessDropObserved && !validOperation || !status.WitnessDropObserved && status.WitnessDropOperation != "" {
		return LinuxV2WitnessDropStatus{}, errors.New("Linux owner helper returned an invalid ACK-loss witness operation")
	}
	result := LinuxV2WitnessDropStatus{
		RunKey: state.RunKey{RunID: status.RunKey.RunID, Generation: status.RunKey.Generation},
		Fence:  status.Fence, Helper: status.Helper,
		Observed: status.WitnessDropObserved, Operation: status.WitnessDropOperation,
	}
	if result.Observed {
		owner.mu.Lock()
		if owner.observedWitnessDrop == nil {
			copy := result
			owner.observedWitnessDrop = &copy
		} else if owner.observedWitnessDrop.RunKey != result.RunKey || owner.observedWitnessDrop.Fence != result.Fence ||
			!owner.observedWitnessDrop.Helper.Equal(result.Helper) ||
			owner.observedWitnessDrop.Operation != result.Operation {
			owner.mu.Unlock()
			return LinuxV2WitnessDropStatus{}, errors.New("Linux helper witness status changed its exact run operation")
		}
		owner.mu.Unlock()
	}
	return result, nil
}

func (owner *linuxV2ProcessOwner) liveOwnerStatus(ctx context.Context) (LinuxV2LiveOwnerStatus, error) {
	if owner == nil || ctx == nil {
		return LinuxV2LiveOwnerStatus{}, errors.New("Linux v2 live-owner status context is incomplete")
	}
	statusContext, cancel := context.WithTimeout(ctx, linuxV2AdmissionStatusTimeout)
	defer cancel()

	if err := lockLinuxV2OwnerStatusContext(statusContext, owner); err != nil {
		return LinuxV2LiveOwnerStatus{}, fmt.Errorf("lock exact Linux v2 owner status: %w", err)
	}
	helper := owner.helper
	if helper == nil || helper.Control == nil || helper.PIDFD == nil || owner.grant.Validate() != nil ||
		owner.reservation.Helper == nil {
		owner.mu.Unlock()
		return LinuxV2LiveOwnerStatus{}, errors.ErrUnsupported
	}
	helperIdentity := authority.LinuxCgroupProcessIdentity{PID: helper.PID, Identity: helper.Identity}
	if helperIdentity.Validate() != nil || !owner.reservation.Helper.Equal(helperIdentity) {
		owner.mu.Unlock()
		return LinuxV2LiveOwnerStatus{}, errors.New("Linux v2 live-owner helper differs from the exact reservation")
	}
	helperFD, err := unix.FcntlInt(helper.PIDFD.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		owner.mu.Unlock()
		return LinuxV2LiveOwnerStatus{}, fmt.Errorf("duplicate exact Linux owner helper pidfd: %w", err)
	}
	helperPIDFD := os.NewFile(uintptr(helperFD), "linux-v2-helper-live-owner-pidfd")
	if helperPIDFD == nil {
		_ = unix.Close(helperFD)
		owner.mu.Unlock()
		return LinuxV2LiveOwnerStatus{}, errors.New("wrap duplicated Linux owner helper pidfd")
	}
	helperControl := helper.Control
	grant := owner.grant
	expectedGate := cloneLinuxV2Gate(owner.gate)
	reservationGate := cloneLinuxV2Gate(owner.reservation.Gate)
	expectedUserNamespace := owner.verifiedUserNamespace
	rootUserNamespaceVerified := owner.rootUserNamespaceVerified
	owner.mu.Unlock()
	defer helperPIDFD.Close()
	var status platform.LinuxOwnerHelperV2Status
	statusOpen := false
	defer func() {
		if statusOpen {
			_ = status.Close()
		}
	}()
	for {
		var err error
		status, err = owner.readLiveOwnerHelperStatus(statusContext, helperControl, helperPIDFD, grant, helperIdentity)
		if err != nil {
			return LinuxV2LiveOwnerStatus{}, err
		}
		statusOpen = true
		if expectedGate == nil || reservationGate == nil || status.Gate == nil || status.Gate.Validate() != nil ||
			!status.Gate.Equal(*expectedGate) || !status.Gate.Equal(*reservationGate) || !rootUserNamespaceVerified ||
			status.UserNamespace == nil || *status.UserNamespace != expectedUserNamespace {
			return LinuxV2LiveOwnerStatus{}, errors.New("Linux owner helper Status differs from the exact gate or authenticated user namespace")
		}

		if err := lockLinuxV2OwnerStatusContext(statusContext, owner); err != nil {
			return LinuxV2LiveOwnerStatus{}, fmt.Errorf("recheck exact Linux v2 owner status: %w", err)
		}
		currentHelper := owner.helper
		if currentHelper == nil || currentHelper.Control != helperControl || currentHelper.PID != helperIdentity.PID ||
			currentHelper.Identity != helperIdentity.Identity {
			owner.mu.Unlock()
			return LinuxV2LiveOwnerStatus{}, errors.New("Linux owner helper changed while reading live-owner Status")
		}
		if owner.leaseRenewalInFlight || owner.leaseRenewalUncertain ||
			owner.leaseAck && status.LeaseSequence < owner.leaseSequence {
			done := owner.leaseRenewalDone
			inFlight := owner.leaseRenewalInFlight
			owner.mu.Unlock()
			_ = status.Close()
			statusOpen = false
			if inFlight {
				if done == nil {
					return LinuxV2LiveOwnerStatus{}, errors.New("in-flight lease renewal has no completion signal")
				}
				select {
				case <-done:
				case <-statusContext.Done():
					return LinuxV2LiveOwnerStatus{}, statusContext.Err()
				}
			} else if err := waitLinuxV2AdmissionStatusRetry(statusContext); err != nil {
				return LinuxV2LiveOwnerStatus{}, fmt.Errorf("lease Status did not settle at the last acknowledged sequence: %w", err)
			}
			continue
		}
		leaseAck := owner.leaseAck
		leaseSequence := owner.leaseSequence
		if status.LeaseSequence != leaseSequence {
			owner.mu.Unlock()
			return LinuxV2LiveOwnerStatus{}, errors.New("helper Status lease sequence differs from the last acknowledged owner sequence")
		}
		stopRequested := owner.stopRequested
		localReleaseAttempted := owner.releaseAttempted
		localReleaseSucceeded := owner.releaseSucceeded
		ownerReleased := owner.ownerReleased
		owner.mu.Unlock()

		targetExited := true
		if status.PIDFD != nil {
			targetExited, err = linuxV2PIDFDExited(status.PIDFD)
			if err != nil {
				return LinuxV2LiveOwnerStatus{}, fmt.Errorf("probe exact Linux owner target pidfd: %w", err)
			}
		}
		helperExitedAfterStatus, err := linuxV2PIDFDExited(helperPIDFD)
		if err != nil {
			return LinuxV2LiveOwnerStatus{}, fmt.Errorf("recheck exact Linux owner helper pidfd after Status: %w", err)
		}
		releaseUncertain := status.ReleaseInFlight || status.ReleaseAttempted && !status.Released ||
			localReleaseAttempted && !status.ReleaseAttempted || localReleaseSucceeded && !status.Released
		terminal := status.State == "stopped" || status.State == "recovery_only" || status.Exit != nil ||
			status.StopEvidence != nil || status.ErrorCode != "" || ownerReleased
		return LinuxV2LiveOwnerStatus{
			RunKey: state.RunKey{RunID: status.RunKey.RunID, Generation: status.RunKey.Generation},
			Fence:  status.Fence, Helper: status.Helper, Target: status.Gate.Process,
			HelperPIDFDLive: !helperExitedAfterStatus, TargetPIDFDLive: status.PIDFD != nil && !targetExited,
			LeaseSequence: leaseSequence,
			LeaseLive:     leaseAck && leaseSequence > 0 && status.LeaseLive && status.LeaseSequence == leaseSequence,
			OwnerLost:     status.OwnerLost || status.State == "recovery_only", CommitAcknowledged: status.CommitAcknowledged,
			StopRequested:    stopRequested || status.StopRequested,
			StopAttempted:    status.StopAttempted,
			ReleaseAttempted: status.ReleaseAttempted,
			ReleaseSucceeded: status.Released,
			ReleaseInFlight:  status.ReleaseInFlight, ReleaseUncertain: releaseUncertain, Terminal: terminal,
		}, nil
	}
}

func lockLinuxV2OwnerStatusContext(ctx context.Context, owner *linuxV2ProcessOwner) error {
	if ctx == nil || owner == nil {
		return errors.New("Linux v2 owner status lock is incomplete")
	}
	for !owner.mu.TryLock() {
		if err := ctx.Err(); err != nil {
			return err
		}
		time.Sleep(time.Millisecond)
	}
	if err := ctx.Err(); err != nil {
		owner.mu.Unlock()
		return err
	}
	return nil
}

func (owner *linuxV2ProcessOwner) captureWitnessDropStatus() {
	ctx, cancel := context.WithTimeout(context.Background(), linuxV2WitnessStatusTimeout)
	defer cancel()
	_, _ = owner.witnessDropStatus(ctx)
}

func (owner *linuxV2ProcessOwner) closeAfterClear() error {
	owner.mu.Lock()
	if !owner.ownerReleased || !owner.clearDone {
		owner.mu.Unlock()
		return errors.New("Linux v2 exact owner and all independent pins remain retained until receipt, removal, Store clear, and helper release")
	}
	helper := owner.helper
	prepared := owner.prepared
	failure := owner.prepareFailure
	leaf := owner.leaf
	owner.helper, owner.prepared, owner.prepareFailure, owner.leaf = nil, nil, nil, nil
	owner.mu.Unlock()
	var closeErr error
	if helper != nil {
		closeErr = errors.Join(closeErr, helper.Close())
	}
	if prepared != nil {
		closeErr = errors.Join(closeErr, prepared.Close())
	}
	if failure != nil {
		closeErr = errors.Join(closeErr, failure.Close())
	}
	if leaf != nil {
		closeErr = errors.Join(closeErr, leaf.Close())
	}
	return closeErr
}

func runLinuxV2JournalMutation[T interface{ Clone() T }](callback func(T) error, value T) error {
	return runLinuxV2MutationRetry(func() error { return callback(value.Clone()) })
}

func runLinuxV2MutationRetry(callback func() error) error {
	first := callback()
	if first == nil || errors.Is(first, ErrLinuxV2MutationAckLost) {
		return first
	}
	second := callback()
	if second == nil {
		return nil
	}
	return errors.Join(first, second)
}

func retryLinuxV2(callback func() error) error { return runLinuxV2MutationRetry(callback) }

func runLinuxV2ReceiptMutation(callback func(authority.SupervisorHandoff, authority.StopReceipt) error, handoff authority.SupervisorHandoff, receipt authority.StopReceipt) error {
	return runLinuxV2MutationRetry(func() error { return callback(handoff.Clone(), cloneLinuxV2StopReceiptValue(receipt)) })
}

func runLinuxV2PreGateReceiptMutation(callback func(state.LinuxCgroupReservation, authority.LinuxCgroupPreGateStopReceipt) error, reservation state.LinuxCgroupReservation, receipt authority.LinuxCgroupPreGateStopReceipt) error {
	return runLinuxV2MutationRetry(func() error { return callback(reservation.Clone(), receipt.Clone()) })
}

func randomLinuxV2Token(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate Linux v2 run authority: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func linuxV2StopReceipt(handoff authority.SupervisorHandoff, proof authority.LinuxCgroupStopProof) authority.StopReceipt {
	leaf := cloneLinuxV2Cgroup(&handoff.CgroupIdentity)
	copyProof := proof
	copyProof.LauncherExit = *cloneLinuxV2Exit(&proof.LauncherExit)
	copyProof.GateExit = *cloneLinuxV2Exit(&proof.GateExit)
	copyProof.HelperExit = cloneLinuxV2Exit(proof.HelperExit)
	return authority.StopReceipt{
		Version: authority.SupervisorVersionV2, Status: "stopped",
		OwnerKind: handoff.OwnerKind, OwnerContext: handoff.OwnerContext,
		TargetPID: handoff.TargetPID, TargetIdentity: handoff.TargetIdentity,
		OwnerUID: handoff.OwnerUID, LauncherUID: handoff.LauncherUID,
		PipeToken: handoff.PipeToken, JobID: handoff.JobID,
		SupervisorPID: handoff.SupervisorPID, SupervisorIdentity: handoff.SupervisorIdentity,
		ActiveProcesses: 0, TargetBirthKind: handoff.TargetBirthKind,
		CgroupIdentity: leaf, LinuxProof: &copyProof,
	}
}

func cloneLinuxV2Gate(value *authority.LinuxCgroupGateIdentity) *authority.LinuxCgroupGateIdentity {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneLinuxV2Exit(value *authority.LinuxCgroupExitProof) *authority.LinuxCgroupExitProof {
	if value == nil {
		return nil
	}
	copy := *value
	copy.HelperExit = cloneLinuxV2Exit(value.HelperExit)
	return &copy
}

func cloneLinuxV2Cgroup(value **authority.LinuxCgroupIdentity) *authority.LinuxCgroupIdentity {
	if value == nil || *value == nil {
		return nil
	}
	copy := **value
	return &copy
}

func cloneLinuxV2PreGateReceipt(value *authority.LinuxCgroupPreGateStopReceipt) *authority.LinuxCgroupPreGateStopReceipt {
	if value == nil {
		return nil
	}
	copy := value.Clone()
	return &copy
}

func cloneLinuxV2CheckoutReceipt(value *authority.LinuxCloneCheckoutReceipt) *authority.LinuxCloneCheckoutReceipt {
	if value == nil {
		return nil
	}
	copy := value.Clone()
	return &copy
}

func cloneLinuxV2PromotionReceipt(value *authority.LinuxClonePromotionReceipt) *authority.LinuxClonePromotionReceipt {
	if value == nil {
		return nil
	}
	copy := value.Clone()
	return &copy
}

func cloneLinuxV2StopReceiptValue(value authority.StopReceipt) authority.StopReceipt {
	cloned := value
	if value.CgroupIdentity != nil {
		identity := *value.CgroupIdentity
		cloned.CgroupIdentity = &identity
	}
	if value.LinuxProof != nil {
		proof := *value.LinuxProof
		proof.LauncherExit = *cloneLinuxV2Exit(&value.LinuxProof.LauncherExit)
		proof.GateExit = *cloneLinuxV2Exit(&value.LinuxProof.GateExit)
		proof.HelperExit = cloneLinuxV2Exit(value.LinuxProof.HelperExit)
		cloned.LinuxProof = &proof
	}
	return cloned
}

func cloneLinuxV2StopEvidence(value *platform.LinuxOwnerHelperV2StopEvidence) platform.LinuxOwnerHelperV2StopEvidence {
	if value == nil {
		return platform.LinuxOwnerHelperV2StopEvidence{}
	}
	copy := *value
	copy.Proof.LauncherExit = *cloneLinuxV2Exit(&value.Proof.LauncherExit)
	copy.Proof.GateExit = *cloneLinuxV2Exit(&value.Proof.GateExit)
	copy.Proof.HelperExit = cloneLinuxV2Exit(value.Proof.HelperExit)
	if value.AnchorStop != nil {
		anchorStop := value.AnchorStop.Clone()
		copy.AnchorStop = &anchorStop
	}
	if value.RunReapEvidence != nil {
		copy.RunReapEvidence = append([]platform.LinuxOwnerAnchorReapEvidence(nil), value.RunReapEvidence...)
		for index := range copy.RunReapEvidence {
			copy.RunReapEvidence[index].HelperExit = *cloneLinuxV2Exit(&value.RunReapEvidence[index].HelperExit)
			copy.RunReapEvidence[index].ProcessExit = *cloneLinuxV2Exit(&value.RunReapEvidence[index].ProcessExit)
		}
	}
	if value.PreGate != nil {
		preGate := *value.PreGate
		if value.PreGate.AnchorStop != nil {
			anchorStop := value.PreGate.AnchorStop.Clone()
			preGate.AnchorStop = &anchorStop
		}
		preGate.Launcher = cloneLinuxV2Process(&value.PreGate.Launcher)
		preGate.LauncherExit = cloneLinuxV2Exit(value.PreGate.LauncherExit)
		preGate.Proof = cloneLinuxV2PreGateProof(value.PreGate.Proof)
		copy.PreGate = &preGate
	}
	return copy
}

func cloneLinuxV2PreGateProof(value *authority.LinuxCgroupPreGateStopProof) *authority.LinuxCgroupPreGateStopProof {
	if value == nil {
		return nil
	}
	copy := *value
	copy.LauncherExit = *cloneLinuxV2Exit(&value.LauncherExit)
	copy.HelperExit = cloneLinuxV2Exit(value.HelperExit)
	return &copy
}

func cloneLinuxV2Process(value **authority.LinuxCgroupProcessIdentity) *authority.LinuxCgroupProcessIdentity {
	if value == nil || *value == nil {
		return nil
	}
	copy := **value
	return &copy
}
