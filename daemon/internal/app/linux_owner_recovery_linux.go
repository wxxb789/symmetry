//go:build linux && amd64

package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

func init() {
	containmentOnlyRecovery = runLinuxContainmentOnlyRecovery
	recoverLinuxV2ContainmentOwner = recoverLinuxV2OwnerJournal
	linuxCloneAbortRecovery = recoverLinuxCloneAbortJournal
	linuxCloneTargetDeleterForRun = linuxCloneTargetRemovalDeleter
	linuxCloneCleanupPersistMutation = func(
		daemon *daemon,
		store *state.Store,
		key state.RunKey,
		fence protocol.Fence,
		stage string,
		write func() (state.RunJournal, error),
		matches func(state.RunJournal) bool,
	) error {
		if daemon == nil {
			return errors.New("Linux clone cleanup persistence has no App owner")
		}
		return daemon.persistLinuxV2Mutation(store, key, fence, stage, write, matches)
	}
}

func linuxCloneTargetRemovalDeleter(
	daemon *daemon,
	grant authority.LinuxCloneGrant,
	cleanup authority.LinuxCloneCleanupIntent,
	priorRemoval *authority.LinuxCloneTargetRemovalProof,
) (workspace.LinuxCloneTargetDeleter, error) {
	if daemon == nil || grant.Validate() != nil || cleanup.Validate() != nil ||
		!grant.Intent.Equal(cleanup.Intent) || !grant.Seed.Equal(cleanup.SeedReceipt) {
		return nil, errors.New("Linux clone cleanup anchor callback lacks the exact Begin-cleanup grant")
	}
	if priorRemoval != nil && (priorRemoval.Validate() != nil || !priorRemoval.Intent.Equal(cleanup) ||
		priorRemoval.Disposition != authority.LinuxCloneTargetDispositionRemoved) {
		return nil, errors.New("Linux clone cleanup prior proof differs from the exact persisted target-removal receipt")
	}
	owner, err := daemon.linuxV2OwnerForRecovery()
	if err != nil {
		return nil, err
	}
	anchor := owner.anchor
	if anchor == nil {
		return nil, errors.New("Linux clone cleanup has no authenticated owner anchor")
	}
	anchorIdentity := authority.LinuxCgroupProcessIdentity{
		PID: cleanup.NormalReleaseProof.AnchorPID, Identity: cleanup.NormalReleaseProof.AnchorIdentity,
	}
	return func(
		ctx context.Context,
		rootFD, targetFD int,
		callbackGrant authority.LinuxCloneGrant,
		callbackCleanup authority.LinuxCloneCleanupIntent,
		callbackPrior *authority.LinuxCloneTargetRemovalProof,
	) (authority.LinuxCloneTargetRemovalProof, error) {
		if ctx == nil || !callbackGrant.Intent.Equal(grant.Intent) || !callbackGrant.Seed.Equal(grant.Seed) ||
			!callbackCleanup.Equal(cleanup) || anchorIdentity.Validate() != nil ||
			(priorRemoval == nil && (targetFD < 0 || callbackPrior != nil)) ||
			(priorRemoval != nil && (targetFD >= 0 || callbackPrior == nil || !callbackPrior.Equal(*priorRemoval))) {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("Workspace clone removal changed its exact Begin-cleanup authority")
		}
		proof, err := anchor.RemoveBCloneAfterReceipt(ctx, rootFD, targetFD, callbackGrant, callbackCleanup, callbackPrior)
		if err != nil {
			return authority.LinuxCloneTargetRemovalProof{}, err
		}
		if proof.Validate() != nil || !proof.Intent.Equal(cleanup) ||
			!proof.RootDeleterExit.ValidFor(proof.RootDeleter, anchorIdentity) {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("owner anchor returned no exact NormalReleaseProof-bound target-removal witness")
		}
		if priorRemoval != nil && !proof.Equal(*priorRemoval) ||
			priorRemoval == nil && proof.Disposition != authority.LinuxCloneTargetDispositionRemoved {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("owner anchor target-removal proof differs from its fresh/prior proof mode")
		}
		return proof.Clone(), nil
	}, nil
}

func recoverLinuxCloneAbortJournal(ctx context.Context, daemon *daemon, journal state.RunJournal) error {
	if ctx == nil || daemon == nil || journal.LinuxCloneIntent == nil {
		return errors.New("Linux clone abort recovery lacks its exact context, daemon, or intent")
	}
	fence := linuxV2FenceFromJournal(journal)
	if !linuxV2JournalMatchesFence(journal, journal.Key(), fence) {
		return errors.New("Linux clone abort journal has no exact claim fence")
	}
	return daemon.abortLinuxV2CloneBeforeCheckout(ctx, journal.Key(), fence, *journal.LinuxCloneIntent)
}

func recoverLinuxV2OwnerJournal(ctx context.Context, daemon *daemon, journal state.RunJournal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !hasLinuxV2ContainmentState(journal) {
		return nil
	}
	if !hasPendingLinuxV2Containment(journal) {
		if journal.HasProcessDetails() || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
			journal.ContainmentScans != nil || journal.ContainmentUnproven {
			return errors.New("released Linux v2 tombstone still has a live process marker or authority")
		}
		if journal.ContainmentCgroup != nil && journal.ContainmentCgroup.AbortProof != nil {
			return retireLinuxV2AbortTombstone(ctx, daemon, journal, *journal.ContainmentCgroup, *journal.ContainmentCgroup.AbortProof)
		}
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			return retireLinuxV2PreGateClearedTombstone(ctx, daemon, journal, *journal.ContainmentCgroupPreGateReleaseProof)
		}
		if journal.ContainmentCgroupReleaseProof != nil {
			if err := daemon.reconcileLinuxClonePromotion(ctx, journal, platform.LinuxCgroupV2Release{}); err != nil {
				return fmt.Errorf("reconcile promotion from the durable Linux v2 release tombstone: %w", err)
			}
			return retireLinuxV2ClearedTombstone(ctx, daemon, journal, *journal.ContainmentCgroupReleaseProof)
		}
		return errors.New("Linux v2 release tombstone cannot be reconciled until the anchor exposes its exact post-clear retirement proof")
	}
	if journal.ContainmentCgroup == nil || journal.ContainmentCgroup.AbortProof != nil ||
		journal.ContainmentScans != nil || journal.ContainmentUnproven {
		return errors.New("Linux v2 journal lacks a complete recoverable cgroup reservation")
	}
	reservation := journal.ContainmentCgroup.Clone()
	if !reservation.CreateStarted {
		fence := linuxV2FenceFromJournal(journal)
		if !linuxV2JournalMatchesFence(journal, journal.Key(), fence) {
			return errors.New("never-created Linux reservation has no exact claim fence")
		}
		return daemon.abortNeverCreatedLinuxV2Reservation(journal.Key(), fence, reservation)
	}
	if !reservation.CreateStarted || reservation.LeafIdentity == nil || reservation.Helper == nil ||
		reservation.Mirror == nil || reservation.RecoveryEndpoint == "" {
		return errors.New("Linux v2 reservation has no pinned leaf/helper identity for recovery")
	}
	fence := linuxV2FenceFromJournal(journal)
	if !linuxV2JournalMatchesFence(journal, journal.Key(), fence) {
		return errors.New("Linux v2 journal claim fence is incomplete")
	}
	grant := linuxV2GrantFromJournal(journal.Key(), fence, reservation)
	if err := grant.Validate(); err != nil {
		return fmt.Errorf("validate exact Linux v2 anchor grant from RunJournal: %w", err)
	}
	owner, err := daemon.linuxV2OwnerForRecovery()
	if err != nil {
		return err
	}
	anchor, err := owner.anchor.Discover(ctx)
	if err != nil {
		return fmt.Errorf("discover exact Linux v2 recovery anchor: %w", err)
	}
	if !anchor.Process.Equal(reservation.Anchor) || !anchor.Parent.Equal(reservation.Parent) || anchor.OwnerUID != reservation.OwnerUID {
		return errors.New("Linux v2 recovery anchor or delegated parent differs from the durable reservation")
	}
	helper, lookupErr := owner.anchor.LookupHelper(ctx, grant)
	if lookupErr != nil {
		return fmt.Errorf("lookup exact RunKey-bound Linux owner helper: %w", lookupErr)
	}
	defer helper.Close()
	if helper.Identity != reservation.Helper.Identity || helper.PID != reservation.Helper.PID {
		return errors.New("Linux owner anchor returned a helper that differs from the durable reservation")
	}
	if helper.ParentFD == nil || helper.LeafFD == nil {
		return errors.New("anchor did not transfer both continuous v2 parent and leaf pins")
	}
	parent, err := platform.AdoptLinuxCgroupV2Parent(int(helper.ParentFD.Fd()), reservation.Parent)
	if err != nil {
		return fmt.Errorf("adopt anchor-transferred pinned Linux cgroup parent: %w", err)
	}
	defer parent.Close()
	callbacks := daemon.linuxV2JournalCallbacks(journal.Key(), fence)
	if reservation.PreGateStopReceipt != nil {
		return recoverLinuxV2PreGateStop(ctx, daemon, parent, owner.anchor, helper, grant, reservation, callbacks)
	}
	if (journal.ContainmentHandoff == nil) == (journal.ContainmentAuthority == nil) {
		if journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil {
			return errors.New("Linux v2 reservation has conflicting pending and committed owners")
		}
		noLauncherGrant := !reservation.LauncherGrantPossible && reservation.Launcher == nil &&
			reservation.LauncherExit == nil && reservation.Gate == nil
		if noLauncherGrant {
			var evidence *platform.LinuxOwnerHelperV2StopEvidence
			if helper.Control != nil && helper.HelperExit == nil && reservation.HelperExit == nil {
				measured, err := helper.Control.Stop(ctx)
				if err != nil {
					return fmt.Errorf("stop exact pre-launcher Linux v2 helper: %w", err)
				}
				if measured.PreGate == nil || measured.PreGate.PrepareAttempted {
					return errors.New("pre-launcher helper stop lacks the exact no-Prepare producer fence")
				}
				if err := measured.PreGate.ValidateFor(grant, *reservation.Helper); err != nil {
					return fmt.Errorf("validate exact pre-launcher helper stop evidence: %w", err)
				}
				evidence = &measured
			}
			return recoverLinuxV2NoPrepareAbort(ctx, daemon, parent, owner.anchor, helper, grant, journal, reservation, evidence, callbacks)
		}
		if !reservation.LauncherGrantPossible || reservation.Launcher == nil {
			return errors.New("pre-handoff Linux v2 owner lacks the exact launcher-granted reservation")
		}
		if helper.Control == nil || helper.HelperExit != nil || reservation.HelperExit != nil {
			reaped, err := owner.anchor.WaitReapRun(ctx, grant, *reservation.Helper)
			if err != nil {
				return fmt.Errorf("anchor could not stop and reap the exact unhanded RunKey after helper death: %w", err)
			}
			facts, err := linuxV2AnchorReapFactsForReservation(grant, reservation, reaped)
			if err != nil {
				return err
			}
			if helper.HelperExit != nil && !helper.HelperExit.Equal(facts.helperExit) {
				return errors.New("anchor run-reap helper exit differs from the exact RunKey lookup")
			}
			if facts.gate == nil {
				if reaped.Stop.GateOutcome == nil {
					return errors.New("dead pre-handoff helper left neither an exact gate nor authenticated gate-never-produced outcome")
				}
				journal, reservation, err = bindRecoveredV2AnchorPreGateStopReceipt(
					ctx, daemon, journal, reservation, grant, reaped, callbacks,
				)
				if err != nil {
					return err
				}
				return recoverLinuxV2PreGateStop(ctx, daemon, parent, owner.anchor, helper, grant, reservation, callbacks)
			}
			journal, reservation, err = bindRecoveredV2AnchorGate(
				ctx, daemon, journal, reservation, grant, reaped, callbacks,
			)
			if err != nil {
				return err
			}
			return recoverLinuxV2GateStop(
				ctx, daemon, parent, owner.anchor, helper, grant, journal, reservation, callbacks, nil, &reaped,
			)
		}
		journal, reservation, evidence, preGate, err := bindRecoveredV2UnhandedHelperStop(ctx, daemon, journal, reservation, grant, helper.Control, callbacks)
		if err != nil {
			return err
		}
		if preGate {
			return recoverLinuxV2PreGateStop(ctx, daemon, parent, owner.anchor, helper, grant, reservation, callbacks)
		}
		return recoverLinuxV2GateStop(ctx, daemon, parent, owner.anchor, helper, grant, journal, reservation, callbacks, &evidence, nil)
	}
	return recoverLinuxV2GateStop(ctx, daemon, parent, owner.anchor, helper, grant, journal, reservation, callbacks, nil, nil)
}

func bindRecoveredV2UnhandedHelperStop(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	grant platform.LinuxOwnerAnchorGrant,
	control linuxV2RecoveryHelperControl,
	callbacks execution.LinuxV2JournalCallbacks,
) (state.RunJournal, state.LinuxCgroupReservation, platform.LinuxOwnerHelperV2StopEvidence, bool, error) {
	if ctx == nil || control == nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, platform.LinuxOwnerHelperV2StopEvidence{}, false,
			errors.New("unhanded Linux v2 recovery has no authenticated helper Stop interface")
	}
	evidence, err := control.Stop(ctx)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, platform.LinuxOwnerHelperV2StopEvidence{}, false,
			fmt.Errorf("stop exact pre-handoff Linux v2 helper: %w", err)
	}
	journal, reservation, preGate, err := bindRecoveredV2UnhandedStopEvidence(ctx, daemon, journal, reservation, grant, evidence, callbacks)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, platform.LinuxOwnerHelperV2StopEvidence{}, false, err
	}
	return journal, reservation, evidence, preGate, nil
}

func bindRecoveredV2UnhandedStopEvidence(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	grant platform.LinuxOwnerAnchorGrant,
	evidence platform.LinuxOwnerHelperV2StopEvidence,
	callbacks execution.LinuxV2JournalCallbacks,
) (state.RunJournal, state.LinuxCgroupReservation, bool, error) {
	if reservation.Helper == nil || reservation.Launcher == nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, false, errors.New("unhanded helper stop lacks exact durable helper/launcher identity")
	}
	if evidence.PreGate != nil {
		if err := evidence.ValidateFor(grant, *reservation.Helper, authority.LinuxCgroupProcessIdentity{}, authority.LinuxCgroupGateIdentity{}); err != nil {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, false, fmt.Errorf("validate exact pre-handoff Stop envelope: %w", err)
		}
		if err := evidence.PreGate.ValidateFor(grant, *reservation.Helper); err != nil {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, false, fmt.Errorf("validate exact pre-handoff helper stop evidence: %w", err)
		}
		if !evidence.PreGate.PrepareAttempted || evidence.PreGate.Launcher == nil || evidence.PreGate.LauncherExit == nil ||
			!evidence.PreGate.Launcher.Equal(*reservation.Launcher) {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, false, errors.New("authenticated gate-never-produced evidence lacks its exact armed launcher identity")
		}
		updatedJournal, updatedReservation, err := bindRecoveredV2PreGateStopReceipt(ctx, daemon, journal, reservation, grant, evidence, callbacks)
		return updatedJournal, updatedReservation, true, err
	}
	updatedJournal, updatedReservation, err := bindRecoveredV2Gate(ctx, daemon, journal, reservation, evidence, callbacks)
	return updatedJournal, updatedReservation, false, err
}

func retireLinuxV2ClearedTombstone(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	proof authority.SupervisorHandoffReleaseProof,
) error {
	if ctx == nil || daemon == nil || proof.Validate() != nil ||
		proof.Version != authority.SupervisorHandoffVersionV2 || proof.OwnerKind != authority.OwnerKindLinuxHelper ||
		proof.CgroupIdentity == nil || proof.StopReceipt == nil ||
		journal.ContainmentCgroup != nil || journal.ContainmentCgroupPreGateReleaseProof != nil ||
		journal.HasProcessDetails() || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
		journal.ContainmentScans != nil || journal.ContainmentUnproven {
		return errors.New("Linux v2 normal release tombstone is not exact or still has an active owner")
	}
	fence := linuxV2FenceFromJournal(journal)
	if !linuxV2JournalMatchesFence(journal, journal.Key(), fence) {
		return errors.New("Linux v2 normal release tombstone has no exact claim fence")
	}
	owner, err := daemon.linuxV2OwnerForRecovery()
	if err != nil {
		return err
	}
	identity, err := owner.anchor.Discover(ctx)
	if err != nil {
		return fmt.Errorf("discover anchor for cleared Linux v2 run retirement: %w", err)
	}
	if !identity.Process.Equal(authority.LinuxCgroupProcessIdentity{PID: proof.AnchorPID, Identity: proof.AnchorIdentity}) ||
		!identity.Parent.Equal(proof.CgroupIdentity.Parent) || identity.OwnerUID != proof.OwnerUID {
		return errors.New("cleared Linux v2 run anchor differs from its exact release tombstone")
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: proof.SupervisorPID, Identity: proof.SupervisorIdentity}
	key := platform.LinuxOwnerAnchorRunKey{RunID: journal.RunID, Generation: journal.Generation}
	if err := owner.anchor.RetireClearedRunWithProof(ctx, key, fence, helper, proof); err != nil {
		return fmt.Errorf("retire exact cleared Linux v2 run after durable release: %w", err)
	}
	return nil
}

func retireLinuxV2PreGateClearedTombstone(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	proof authority.LinuxCgroupPreGateReleaseProof,
) error {
	if ctx == nil || daemon == nil || proof.Validate() != nil ||
		journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil ||
		journal.HasProcessDetails() || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
		journal.ContainmentScans != nil || journal.ContainmentUnproven {
		return errors.New("Linux v2 pre-gate release tombstone is not exact or still has an active owner")
	}
	fence := linuxV2FenceFromJournal(journal)
	if !linuxV2JournalMatchesFence(journal, journal.Key(), fence) {
		return errors.New("Linux v2 pre-gate release tombstone has no exact claim fence")
	}
	owner, err := daemon.linuxV2OwnerForRecovery()
	if err != nil {
		return err
	}
	identity, err := owner.anchor.Discover(ctx)
	if err != nil {
		return fmt.Errorf("discover anchor for cleared Linux v2 pre-gate retirement: %w", err)
	}
	if !identity.Process.Equal(proof.Receipt.Anchor) || !identity.Parent.Equal(proof.Receipt.CgroupIdentity.Parent) ||
		identity.OwnerUID != proof.Receipt.OwnerUID {
		return errors.New("cleared Linux v2 pre-gate anchor differs from its exact receipt")
	}
	key := platform.LinuxOwnerAnchorRunKey{RunID: journal.RunID, Generation: journal.Generation}
	if err := owner.anchor.RetirePreGateClearedRunWithProof(ctx, key, fence, proof.Receipt.Helper, proof); err != nil {
		return fmt.Errorf("retire exact cleared Linux v2 pre-gate run: %w", err)
	}
	return nil
}

func recoverLinuxV2NoPrepareAbort(
	ctx context.Context,
	daemon *daemon,
	parent *platform.LinuxCgroupV2Parent,
	anchor *platform.LinuxOwnerAnchorClient,
	helper *platform.LinuxOwnerAnchorHelper,
	grant platform.LinuxOwnerAnchorGrant,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	stopEvidence *platform.LinuxOwnerHelperV2StopEvidence,
	callbacks execution.LinuxV2JournalCallbacks,
) error {
	if ctx == nil || daemon == nil || parent == nil || anchor == nil || helper == nil ||
		reservation.Validate() != nil || reservation.Helper == nil || reservation.LeafIdentity == nil || reservation.Mirror == nil ||
		!reservation.CreateStarted || reservation.LauncherGrantPossible || reservation.Launcher != nil ||
		reservation.LauncherExit != nil || reservation.Gate != nil || reservation.PreGateStopReceipt != nil ||
		reservation.AbortProof != nil || helper.LeafFD == nil {
		return errors.New("Linux no-Prepare abort lacks the exact bound helper-only reservation")
	}
	helperIdentity := *reservation.Helper
	if !helperIdentity.Equal(authority.LinuxCgroupProcessIdentity{PID: helper.PID, Identity: helper.Identity}) ||
		grant.RunKey != (platform.LinuxOwnerAnchorRunKey{RunID: journal.RunID, Generation: journal.Generation}) ||
		grant.Fence != linuxV2FenceFromJournal(journal) || !grant.Anchor.Equal(reservation.Anchor) ||
		!grant.Parent.Equal(reservation.Parent) || !grant.Leaf.Equal(*reservation.LeafIdentity) ||
		parent.OwnerUID() != reservation.OwnerUID || !parent.Identity().Equal(reservation.Parent) {
		return errors.New("Linux no-Prepare abort helper or grant differs from the exact RunJournal")
	}
	var control linuxV2RecoveryHelperControl
	if helper.Control != nil && helper.HelperExit == nil && reservation.HelperExit == nil {
		control = helper.Control
	}
	journal, reservation, helperExit, err := persistRecoveredV2NoPrepareHelperExit(
		ctx, daemon, control, anchor, grant, journal, reservation, stopEvidence, callbacks,
	)
	if err != nil {
		return err
	}
	if helper.HelperExit != nil && !helper.HelperExit.Equal(helperExit) {
		return errors.New("anchor helper exit differs from its previously retained wait proof")
	}
	leaf, err := parent.AdoptLeaf(reservation.LeafIdentity.LeafName, int(helper.LeafFD.Fd()), *reservation.LeafIdentity)
	if err != nil {
		return fmt.Errorf("adopt exact no-Prepare reserved leaf pin: %w", err)
	}
	defer leaf.Close()
	helperExitCopy := helperExit
	authorization := platform.LinuxCgroupV2AbortAuthorization{
		LaunchToken: reservation.LaunchToken, OwnerUID: reservation.OwnerUID,
		ReservedLeafIdentity: *reservation.LeafIdentity, CreateStarted: true,
		LauncherGrantPossible: false, Anchor: reservation.Anchor, Mirror: *reservation.Mirror,
		HelperWasCreated: true, Helper: helperIdentity, HelperExit: &helperExitCopy,
		CreatorFenced: true, NoProducerCanAct: true,
	}
	proof, err := leaf.RemoveEmptyReservationAfterAbort(ctx, authorization)
	if err != nil {
		return fmt.Errorf("remove only the exact empty pre-launcher leaf after helper reap: %w", err)
	}
	if err := callbacks.AbortReservation(reservation, proof); err != nil {
		return fmt.Errorf("persist exact pre-launcher cgroup abort proof: %w", err)
	}
	latest, err := daemon.store.LoadJournal(journal.Key())
	if err != nil || !linuxV2JournalMatchesFence(latest, journal.Key(), grant.Fence) ||
		latest.ContainmentCgroup == nil || latest.ContainmentCgroup.AbortProof == nil ||
		!latest.ContainmentCgroup.AbortProof.Equal(proof) || latest.HasProcessDetails() ||
		latest.ContainmentAuthority != nil || latest.ContainmentHandoff != nil {
		return fmt.Errorf("read back exact pre-launcher cgroup abort proof: %w", appErrorOr(err, "abort proof is missing"))
	}
	if err := anchor.RetireAbortedHelper(ctx, grant.RunKey, grant.Fence, helperIdentity, proof); err != nil {
		return fmt.Errorf("retire exact pre-launcher helper after durable no-producer abort: %w", err)
	}
	return nil
}

func bindRecoveredV2PreGateStopReceipt(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	grant platform.LinuxOwnerAnchorGrant,
	evidence platform.LinuxOwnerHelperV2StopEvidence,
	callbacks execution.LinuxV2JournalCallbacks,
) (state.RunJournal, state.LinuxCgroupReservation, error) {
	if ctx == nil || daemon == nil || reservation.Helper == nil || reservation.Launcher == nil ||
		!reservation.LauncherGrantPossible || reservation.Gate != nil || evidence.PreGate == nil ||
		!evidence.PreGate.PrepareAttempted || evidence.PreGate.Launcher == nil || evidence.PreGate.LauncherExit == nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("recovered pre-gate evidence lacks the exact armed launcher")
	}
	if err := evidence.ValidateFor(grant, *reservation.Helper, authority.LinuxCgroupProcessIdentity{}, authority.LinuxCgroupGateIdentity{}); err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("validate measured pre-gate helper stop evidence: %w", err)
	}
	if !evidence.PreGate.Launcher.Equal(*reservation.Launcher) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("measured pre-gate launcher differs from the durable reservation")
	}
	updatedJournal, updatedReservation := journal, reservation
	var err error
	if evidence.PreGate.Proof != nil && evidence.PreGate.Proof.HelperExit != nil {
		updatedJournal, updatedReservation, err = persistRecoveredV2HelperExit(
			ctx, daemon, journal, reservation, *evidence.PreGate.Proof.HelperExit, callbacks,
		)
		if err != nil {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("persist exact helper wait before pre-gate receipt: %w", err)
		}
		journal, reservation = updatedJournal, updatedReservation
	}
	updatedJournal, updatedReservation, err = persistRecoveredV2LauncherExit(
		ctx, daemon, journal, reservation, *evidence.PreGate.LauncherExit, callbacks,
	)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("persist authenticated pre-gate launcher wait before receipt: %w", err)
	}
	receipt, err := linuxV2PreGateReceipt(updatedReservation, grant, *updatedReservation.Helper, evidence)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, err
	}
	if updatedReservation.PreGateStopReceipt != nil {
		if !updatedReservation.PreGateStopReceipt.Equal(receipt) {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("recovered pre-gate receipt differs from its existing State binding")
		}
	} else {
		if err := callbacks.RecordPreGateStopReceipt(withoutLinuxV2PreGateReceipt(updatedReservation), receipt); err != nil {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("persist authenticated recovered pre-gate receipt: %w", err)
		}
		updatedJournal, err = daemon.store.LoadJournal(journal.Key())
		if err != nil || !linuxV2JournalMatchesFence(updatedJournal, journal.Key(), grant.Fence) ||
			updatedJournal.ContainmentCgroup == nil || updatedJournal.ContainmentCgroup.PreGateStopReceipt == nil ||
			!updatedJournal.ContainmentCgroup.PreGateStopReceipt.Equal(receipt) {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("read back recovered pre-gate receipt: %w", appErrorOr(err, "reservation or receipt is missing"))
		}
		updatedReservation = updatedJournal.ContainmentCgroup.Clone()
	}
	return updatedJournal, updatedReservation, nil
}

func sameLinuxV2NoPrepareEvidence(left, right *platform.LinuxOwnerHelperV2PreGateStopEvidence) bool {
	return left != nil && right != nil && left.Version == right.Version && left.RunKey == right.RunKey &&
		left.Fence == right.Fence && left.LaunchToken == right.LaunchToken &&
		left.Parent.Equal(right.Parent) && left.Leaf.Equal(right.Leaf) && left.Anchor.Equal(right.Anchor) &&
		left.Helper.Equal(right.Helper) && !left.PrepareAttempted && !right.PrepareAttempted &&
		left.AnchorStop != nil && right.AnchorStop != nil && sameLinuxOwnerAnchorStopObservation(*left.AnchorStop, *right.AnchorStop) &&
		left.Launcher == nil && right.Launcher == nil && left.LauncherExit == nil && right.LauncherExit == nil &&
		left.GateOutcome == nil && right.GateOutcome == nil && left.Proof == nil && right.Proof == nil &&
		left.NoProducerCanAct == right.NoProducerCanAct && left.PinnedLeafFD == right.PinnedLeafFD &&
		left.CgroupKillAcknowledged == right.CgroupKillAcknowledged && left.CgroupPopulatedZero == right.CgroupPopulatedZero &&
		left.NoMigrationObserved == right.NoMigrationObserved && left.DescendantsReaped == right.DescendantsReaped &&
		left.DescendantCount == right.DescendantCount && left.DescendantDigest == right.DescendantDigest
}

func sameLinuxOwnerAnchorStopObservation(left, right platform.LinuxOwnerAnchorStopEvidence) bool {
	return left.Version == right.Version && left.RunKey == right.RunKey && left.Fence == right.Fence &&
		left.LaunchToken == right.LaunchToken && left.Parent.Equal(right.Parent) && left.Leaf.Equal(right.Leaf) &&
		left.Anchor.Equal(right.Anchor) && left.Helper.Equal(right.Helper) &&
		left.ProducerFence != nil && right.ProducerFence != nil && left.ProducerFence.Equal(*right.ProducerFence) &&
		left.GateReleaseAuthorized == right.GateReleaseAuthorized &&
		left.PinnedLeafFD == right.PinnedLeafFD && left.KillAttempted == right.KillAttempted &&
		left.KillAcknowledged == right.KillAcknowledged && left.CgroupPopulatedZero == right.CgroupPopulatedZero &&
		left.NoMigrationObserved == right.NoMigrationObserved && left.ProducerFenced == right.ProducerFenced
}

type linuxV2RecoveryHelperControl interface {
	Stop(context.Context) (platform.LinuxOwnerHelperV2StopEvidence, error)
	FenceBeforeAbort(context.Context) (platform.LinuxOwnerHelperV2PreGateStopEvidence, error)
}

type linuxV2RecoveryRunReaper interface {
	WaitReapRun(context.Context, platform.LinuxOwnerAnchorGrant, authority.LinuxCgroupProcessIdentity) (platform.LinuxOwnerAnchorRunReapResult, error)
}

func persistRecoveredV2HelperExit(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	proof authority.LinuxCgroupExitProof,
	callbacks execution.LinuxV2JournalCallbacks,
) (state.RunJournal, state.LinuxCgroupReservation, error) {
	if ctx == nil || daemon == nil || reservation.Helper == nil ||
		proof.Validate() != nil || !proof.ValidFor(*reservation.Helper, reservation.Anchor) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("anchor helper wait/reap proof differs from the exact durable reservation")
	}
	if reservation.HelperExit != nil {
		if !reservation.HelperExit.Equal(proof) {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("anchor helper wait/reap proof differs from the durable reservation")
		}
		return journal, reservation, nil
	}
	if err := callbacks.RecordHelperExit(reservation, proof); err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("persist exact helper wait/reap before receipt or leaf removal: %w", err)
	}
	fence := linuxV2FenceFromJournal(journal)
	latest, err := daemon.store.LoadJournal(journal.Key())
	if err != nil || !linuxV2JournalMatchesFence(latest, journal.Key(), fence) || latest.ContainmentCgroup == nil ||
		latest.ContainmentCgroup.HelperExit == nil || !latest.ContainmentCgroup.HelperExit.Equal(proof) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("read back exact helper wait/reap: %w", appErrorOr(err, "reservation or helper exit is missing"))
	}
	return latest, latest.ContainmentCgroup.Clone(), nil
}

func persistRecoveredV2NoPrepareHelperExit(
	ctx context.Context,
	daemon *daemon,
	control linuxV2RecoveryHelperControl,
	anchor linuxV2RecoveryRunReaper,
	grant platform.LinuxOwnerAnchorGrant,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	stopEvidence *platform.LinuxOwnerHelperV2StopEvidence,
	callbacks execution.LinuxV2JournalCallbacks,
) (state.RunJournal, state.LinuxCgroupReservation, authority.LinuxCgroupExitProof, error) {
	if ctx == nil || daemon == nil || anchor == nil || reservation.Helper == nil || reservation.Mirror == nil ||
		!reservation.CreateStarted || reservation.LauncherGrantPossible || reservation.Launcher != nil ||
		reservation.LauncherExit != nil || reservation.Gate != nil || reservation.PreGateStopReceipt != nil ||
		reservation.AbortProof != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{}, errors.New("no-Prepare helper wait lacks an exact helper-only reservation")
	}
	helperIdentity := *reservation.Helper
	if stopEvidence == nil && control != nil && reservation.HelperExit == nil {
		measured, err := control.Stop(ctx)
		if err != nil {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{},
				fmt.Errorf("stop exact pre-launcher helper before abort: %w", err)
		}
		stopEvidence = &measured
	}
	if stopEvidence != nil && (stopEvidence.PreGate == nil || stopEvidence.PreGate.PrepareAttempted ||
		stopEvidence.ValidateFor(grant, helperIdentity, authority.LinuxCgroupProcessIdentity{}, authority.LinuxCgroupGateIdentity{}) != nil) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{}, errors.New("no-Prepare helper Stop result differs from the exact helper grant")
	}
	var fenceErr error
	if control != nil {
		fenced, err := control.FenceBeforeAbort(ctx)
		fenceErr = err
		if err == nil {
			if fenced.PrepareAttempted || fenced.ValidateFor(grant, helperIdentity) != nil {
				return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{}, errors.New("helper fence-before-abort differs from the exact no-Prepare reservation")
			}
			if stopEvidence != nil && !sameLinuxV2NoPrepareEvidence(stopEvidence.PreGate, &fenced) {
				return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{}, errors.New("helper no-Prepare fence differs from its exact measured Stop result")
			}
		}
	}
	reaped, err := anchor.WaitReapRun(ctx, grant, helperIdentity)
	if err != nil {
		if fenceErr != nil {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{}, errors.Join(
				fmt.Errorf("fence helper before no-Prepare abort: %w", fenceErr), fmt.Errorf("wait/reap exact RunKey-owned processes: %w", err))
		}
		return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{}, fmt.Errorf("wait/reap exact no-Prepare RunKey-owned processes: %w", err)
	}
	facts, factsErr := linuxV2AnchorReapFactsForReservation(grant, reservation, reaped)
	if factsErr != nil || reaped.Stop.Launcher != nil || reaped.Stop.Gate != nil || reaped.Stop.GateOutcome != nil ||
		facts.launcherExit != nil || facts.gate != nil || facts.gateExit != nil ||
		reaped.Stop.ProducerFence != nil && (reaped.Stop.ProducerFence.PrepareAttempted ||
			reaped.Stop.ProducerFence.ReleaseAttempted || reaped.Stop.ProducerFence.Released) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{},
			errors.Join(errors.New("anchor RunReap result contains a producer or lacks the exact no-Prepare stop"), factsErr)
	}
	if stopEvidence != nil && (stopEvidence.PreGate == nil || stopEvidence.PreGate.AnchorStop == nil ||
		!sameLinuxOwnerAnchorStopObservation(*stopEvidence.PreGate.AnchorStop, reaped.Stop)) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{}, errors.New("anchor RunReap physical stop differs from the helper's exact no-Prepare Stop response")
	}
	helperExit := facts.helperExit.Clone()
	if !helperExit.ValidFor(helperIdentity, reservation.Anchor) ||
		reservation.HelperExit != nil && !reservation.HelperExit.Equal(helperExit) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{}, errors.New("anchor RunReap helper exit differs from the exact no-Prepare reservation")
	}
	updatedJournal, updatedReservation, err := persistRecoveredV2HelperExit(ctx, daemon, journal, reservation, helperExit, callbacks)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, authority.LinuxCgroupExitProof{}, err
	}
	return updatedJournal, updatedReservation, helperExit, nil
}

func (daemon *daemon) abortNeverCreatedLinuxV2Reservation(key state.RunKey, fence protocol.Fence, expected state.LinuxCgroupReservation) error {
	if fence.Generation != key.Generation || fence.RuntimeID == "" || fence.ClaimID == "" || fence.LeaseToken == "" {
		return errors.New("never-created Linux reservation has no exact claim fence")
	}
	if err := expected.Validate(); err != nil || expected.CreateStarted || expected.LauncherGrantPossible ||
		expected.LeafIdentity != nil || expected.Helper != nil || expected.Launcher != nil || expected.LauncherExit != nil ||
		expected.Gate != nil || expected.HelperExit != nil || expected.AbortProof != nil || expected.PreGateStopReceipt != nil ||
		expected.Mirror == nil {
		return errors.New("Linux reservation is not an exact never-created state")
	}
	return daemon.persistLinuxV2Mutation(daemon.store, key, fence, "abort never-created cgroup reservation", func() (state.RunJournal, error) {
		return daemon.store.AbortNeverCreatedLinuxCgroupReservation(key, fence, expected)
	}, func(journal state.RunJournal) bool {
		if journal.ContainmentCgroup == nil || journal.ContainmentCgroup.AbortProof == nil {
			return false
		}
		current := journal.ContainmentCgroup.Clone()
		proof := current.AbortProof.Clone()
		current.AbortProof = nil
		return current.Equal(expected) && proof.LeafDisposition == authority.LinuxCgroupAbortLeafNeverCreated &&
			proof.ValidFor(expected.LaunchToken, expected.Parent, expected.LeafName, false, expected.OwnerUID,
				expected.Anchor, *expected.Mirror, nil, nil)
	})
}

func retireLinuxV2AbortTombstone(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	proof authority.LinuxCgroupReservationAbortProof,
) error {
	if err := reservation.Validate(); err != nil || proof.Validate() != nil || reservation.AbortProof == nil ||
		!reservation.AbortProof.Equal(proof) || !proof.ValidFor(reservation.LaunchToken, reservation.Parent,
		reservation.LeafName, reservation.CreateStarted, reservation.OwnerUID, reservation.Anchor,
		*reservation.Mirror, reservation.Helper, reservation.Launcher) {
		return errors.New("Linux v2 abort tombstone does not match the exact reservation")
	}
	if proof.Helper == nil {
		if proof.Launcher != nil {
			return errors.New("Linux v2 abort tombstone has a launcher without its helper")
		}
		return nil
	}
	owner, err := daemon.linuxV2OwnerForRecovery()
	if err != nil {
		return err
	}
	identity, err := owner.anchor.Discover(ctx)
	if err != nil {
		return fmt.Errorf("discover anchor for aborted Linux v2 helper retirement: %w", err)
	}
	if !identity.Process.Equal(reservation.Anchor) || !identity.Parent.Equal(reservation.Parent) || identity.OwnerUID != reservation.OwnerUID {
		return errors.New("aborted Linux v2 helper anchor differs from its exact journal tombstone")
	}
	fence := linuxV2FenceFromJournal(journal)
	if !linuxV2JournalMatchesFence(journal, journal.Key(), fence) {
		return errors.New("aborted Linux v2 helper tombstone has no exact claim fence")
	}
	key := platform.LinuxOwnerAnchorRunKey{RunID: journal.RunID, Generation: journal.Generation}
	if err := owner.anchor.RetireAbortedHelper(ctx, key, fence, *proof.Helper, proof); err != nil {
		return fmt.Errorf("retire exact aborted Linux v2 helper: %w", err)
	}
	return nil
}

func (daemon *daemon) linuxV2OwnerForRecovery() (*linuxV2OwnerResources, error) {
	daemon.linuxV2OwnerInitMu.Lock()
	defer daemon.linuxV2OwnerInitMu.Unlock()
	if daemon.linuxV2Owner != nil {
		owner, ok := daemon.linuxV2Owner.(*linuxV2OwnerResources)
		if !ok {
			return nil, errors.New("Linux v2 recovery cannot use a non-platform test owner")
		}
		return owner, nil
	}
	factory := daemon.options.linuxV2NativeOwnerFactory
	if factory == nil {
		factory = defaultLinuxV2NativeOwnerFactory
	}
	created, err := factory(daemon.config)
	if err != nil {
		return nil, fmt.Errorf("initialize Linux v2 recovery anchor and delegated parent: %w", err)
	}
	owner, ok := created.(*linuxV2OwnerResources)
	if !ok || owner == nil {
		if created != nil {
			_ = created.Close()
		}
		return nil, errors.New("Linux v2 recovery requires the platform-pinned owner runtime")
	}
	daemon.linuxV2Owner = owner
	return owner, nil
}

func bindRecoveredV2Gate(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	evidence platform.LinuxOwnerHelperV2StopEvidence,
	callbacks execution.LinuxV2JournalCallbacks,
) (state.RunJournal, state.LinuxCgroupReservation, error) {
	if err := ctx.Err(); err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, err
	}
	if reservation.Helper == nil || reservation.Launcher == nil || reservation.LeafIdentity == nil || reservation.Mirror == nil ||
		!evidence.Launcher.Equal(*reservation.Launcher) || !evidence.Gate.Cgroup.Equal(*reservation.LeafIdentity) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("helper stop evidence cannot bind a nonmatching pre-handoff launcher or gate")
	}
	if err := evidence.ValidateFor(
		linuxV2GrantFromJournal(journal.Key(), linuxV2FenceFromJournal(journal), reservation),
		*reservation.Helper, *reservation.Launcher, evidence.Gate,
	); err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("validate measured pre-handoff gate stop evidence: %w", err)
	}
	if evidence.AnchorStop == nil || linuxV2AnchorReportsGateRelease(*evidence.AnchorStop) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("pre-handoff helper attempted B RELEASE before a durable handoff commit")
	}
	updatedJournal, updatedReservation, err := persistRecoveredV2LauncherExit(ctx, daemon, journal, reservation, evidence.Proof.LauncherExit, callbacks)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("persist exact root-launcher wait before recovery bind: %w", err)
	}
	journal, reservation = updatedJournal, updatedReservation
	if reservation.Gate == nil {
		if err := callbacks.BindGate(reservation, evidence.Gate); err != nil {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("bind helper-observed A gate before stop receipt: %w", err)
		}
		var err error
		journal, err = daemon.store.LoadJournal(journal.Key())
		if err != nil || journal.ContainmentCgroup == nil {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("read back helper-observed A gate: %w", appErrorOr(err, "reservation is missing"))
		}
		reservation = journal.ContainmentCgroup.Clone()
	}
	if reservation.Gate == nil || !reservation.Gate.Equal(evidence.Gate) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("helper stop gate differs from the durable reservation")
	}
	handoff, err := linuxV2HandoffFromReservation(reservation)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, err
	}
	if err := callbacks.PrepareHandoff(handoff); err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("persist measured pre-authority handoff: %w", err)
	}
	if err := callbacks.BindHandoffV2(handoff, *reservation.Helper, *reservation.Mirror); err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("bind exact v2 handoff owners: %w", err)
	}
	journal, err = daemon.store.LoadJournal(journal.Key())
	if err != nil || journal.ContainmentCgroup == nil || journal.ContainmentHandoff == nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("read back bound v2 handoff: %w", appErrorOr(err, "handoff is missing"))
	}
	return journal, journal.ContainmentCgroup.Clone(), nil
}

func bindRecoveredV2AnchorGate(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	grant platform.LinuxOwnerAnchorGrant,
	reaped platform.LinuxOwnerAnchorRunReapResult,
	callbacks execution.LinuxV2JournalCallbacks,
) (state.RunJournal, state.LinuxCgroupReservation, error) {
	if journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil || reservation.Launcher == nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("anchor gate recovery is not for an unhanded exact launcher reservation")
	}
	facts, err := linuxV2AnchorReapFactsForReservation(grant, reservation, reaped)
	if err != nil || facts.gate == nil || facts.gateExit == nil || facts.launcherExit == nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.Join(errors.New("anchor run-reap evidence cannot bind the exact pre-handoff gate"), err)
	}
	if linuxV2AnchorReportsGateRelease(reaped.Stop) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("pre-handoff anchor evidence reports B RELEASE before a durable handoff commit")
	}
	journal, reservation, err = persistRecoveredV2HelperExit(ctx, daemon, journal, reservation, facts.helperExit, callbacks)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, err
	}
	journal, reservation, err = persistRecoveredV2LauncherExit(ctx, daemon, journal, reservation, *facts.launcherExit, callbacks)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("persist anchor-observed launcher exit before gate bind: %w", err)
	}
	if reservation.Gate == nil {
		if err := callbacks.BindGate(reservation, *facts.gate); err != nil {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("bind anchor-observed A gate: %w", err)
		}
		journal, err = daemon.store.LoadJournal(journal.Key())
		if err != nil || journal.ContainmentCgroup == nil {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("read back anchor-observed A gate: %w", appErrorOr(err, "reservation is missing"))
		}
		reservation = journal.ContainmentCgroup.Clone()
	}
	if reservation.Gate == nil || !reservation.Gate.Equal(*facts.gate) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("anchor run-reap gate differs from the durable reservation")
	}
	handoff, err := linuxV2HandoffFromReservation(reservation)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, err
	}
	if err := callbacks.PrepareHandoff(handoff); err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("persist anchor-measured pre-authority handoff: %w", err)
	}
	if err := callbacks.BindHandoffV2(handoff, *reservation.Helper, *reservation.Mirror); err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("bind exact anchor-measured v2 handoff owners: %w", err)
	}
	journal, err = daemon.store.LoadJournal(journal.Key())
	if err != nil || journal.ContainmentCgroup == nil || journal.ContainmentHandoff == nil ||
		journal.ContainmentCgroup.Gate == nil || !journal.ContainmentCgroup.Gate.Equal(*facts.gate) ||
		journal.ContainmentCgroup.LauncherExit == nil || !journal.ContainmentCgroup.LauncherExit.Equal(*facts.launcherExit) ||
		journal.ContainmentCgroup.HelperExit == nil || !journal.ContainmentCgroup.HelperExit.Equal(facts.helperExit) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("read back exact anchor-measured v2 handoff: %w", appErrorOr(err, "handoff or producer proofs are missing"))
	}
	return journal, journal.ContainmentCgroup.Clone(), nil
}

func bindRecoveredV2AnchorPreGateStopReceipt(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	grant platform.LinuxOwnerAnchorGrant,
	reaped platform.LinuxOwnerAnchorRunReapResult,
	callbacks execution.LinuxV2JournalCallbacks,
) (state.RunJournal, state.LinuxCgroupReservation, error) {
	facts, err := linuxV2AnchorReapFactsForReservation(grant, reservation, reaped)
	if err != nil || !reservation.LauncherGrantPossible || reservation.Launcher == nil ||
		facts.gate != nil || facts.gateExit != nil || reaped.Stop.GateOutcome == nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.Join(errors.New("anchor run-reap evidence is not the exact launcher-granted no-gate path"), err)
	}
	journal, reservation, err = persistRecoveredV2HelperExit(ctx, daemon, journal, reservation, facts.helperExit, callbacks)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, err
	}
	if facts.launcherExit == nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("anchor no-gate result lacks its exact launcher wait/reap proof")
	}
	journal, reservation, err = persistRecoveredV2LauncherExit(ctx, daemon, journal, reservation, *facts.launcherExit, callbacks)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("persist anchor no-gate launcher wait before receipt: %w", err)
	}
	receipt, err := linuxV2PreGateReceiptFromAnchorReap(grant, reservation, reaped, facts)
	if err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, err
	}
	if reservation.PreGateStopReceipt != nil {
		if !reservation.PreGateStopReceipt.Equal(receipt) {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("anchor no-gate receipt differs from the durable reservation")
		}
	} else if err := callbacks.RecordPreGateStopReceipt(reservation, receipt); err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("persist anchor-observed authenticated no-gate receipt: %w", err)
	}
	latest, err := daemon.store.LoadJournal(journal.Key())
	if err != nil || !linuxV2JournalMatchesFence(latest, journal.Key(), grant.Fence) ||
		latest.ContainmentCgroup == nil || latest.ContainmentCgroup.PreGateStopReceipt == nil ||
		!latest.ContainmentCgroup.PreGateStopReceipt.Equal(receipt) || latest.PID != 0 || latest.HasProcessDetails() ||
		latest.ContainmentAuthority != nil || latest.ContainmentHandoff != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("read back exact anchor no-gate stop receipt: %w", appErrorOr(err, "reservation or receipt is missing"))
	}
	return latest, latest.ContainmentCgroup.Clone(), nil
}

func persistRecoveredV2LauncherExit(
	ctx context.Context,
	daemon *daemon,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	proof authority.LinuxCgroupExitProof,
	callbacks execution.LinuxV2JournalCallbacks,
) (state.RunJournal, state.LinuxCgroupReservation, error) {
	if ctx == nil || daemon == nil || reservation.Helper == nil || reservation.Launcher == nil ||
		proof.Validate() != nil || !proof.ValidForOwnedDescendant(*reservation.Launcher, *reservation.Helper, reservation.Anchor, reservation.HelperExit) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("measured launcher exit does not match the exact durable helper/launcher identities")
	}
	if reservation.LauncherExit != nil {
		if !reservation.LauncherExit.Equal(proof) {
			return state.RunJournal{}, state.LinuxCgroupReservation{}, errors.New("measured launcher exit differs from the durable reservation")
		}
		return journal, reservation, nil
	}
	if err := callbacks.RecordLauncherExit(reservation, proof); err != nil {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("record exact recovered launcher wait: %w", err)
	}
	fence := linuxV2FenceFromJournal(journal)
	latest, err := daemon.store.LoadJournal(journal.Key())
	if err != nil || !linuxV2JournalMatchesFence(latest, journal.Key(), fence) || latest.ContainmentCgroup == nil ||
		latest.ContainmentCgroup.LauncherExit == nil || !latest.ContainmentCgroup.LauncherExit.Equal(proof) {
		return state.RunJournal{}, state.LinuxCgroupReservation{}, fmt.Errorf("read back exact recovered launcher wait: %w", appErrorOr(err, "reservation or proof is missing"))
	}
	return latest, latest.ContainmentCgroup.Clone(), nil
}

type linuxV2AnchorReapFacts struct {
	helperExit   authority.LinuxCgroupExitProof
	launcherExit *authority.LinuxCgroupExitProof
	gate         *authority.LinuxCgroupGateIdentity
	gateExit     *authority.LinuxCgroupExitProof
}

func linuxV2AnchorReapFactsForReservation(
	grant platform.LinuxOwnerAnchorGrant,
	reservation state.LinuxCgroupReservation,
	reaped platform.LinuxOwnerAnchorRunReapResult,
) (linuxV2AnchorReapFacts, error) {
	if grant.Validate() != nil || reservation.Validate() != nil || reservation.Helper == nil ||
		reservation.LeafIdentity == nil || reservation.Mirror == nil ||
		grant.LaunchToken != reservation.LaunchToken || grant.Secret != reservation.Secret ||
		grant.PipeToken != reservation.PipeToken || grant.JobID != reservation.JobID ||
		grant.RecoveryEndpoint != reservation.RecoveryEndpoint || !grant.Parent.Equal(reservation.Parent) ||
		!grant.Leaf.Equal(*reservation.LeafIdentity) || !grant.Anchor.Equal(reservation.Anchor) ||
		grant.OwnerUID != reservation.OwnerUID || grant.LauncherUID != reservation.LauncherUID {
		return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap request differs from the exact durable reservation")
	}
	stop := reaped.Stop
	// WaitReapRun returns only after the platform client validates the sorted
	// process set and its digest. Bind that authenticated result to State here
	// without serializing and hashing the full set a second time.
	if stop.RunKey != grant.RunKey || stop.Fence != grant.Fence || stop.LaunchToken != grant.LaunchToken ||
		!stop.Parent.Equal(grant.Parent) || !stop.Leaf.Equal(grant.Leaf) ||
		!stop.Anchor.Equal(grant.Anchor) || !stop.Helper.Equal(*reservation.Helper) ||
		!stop.PinnedLeafFD || !stop.KillAttempted || !stop.KillAcknowledged || !stop.CgroupPopulatedZero ||
		!stop.NoMigrationObserved || !stop.ProducerFenced || !stop.DescendantsReaped ||
		stop.ReapCount != uint64(len(reaped.Processes)) || stop.HelperExit == nil ||
		!stop.HelperExit.ValidFor(*reservation.Helper, reservation.Anchor) ||
		reservation.HelperExit != nil && !reservation.HelperExit.Equal(*stop.HelperExit) {
		return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap helper exit differs from the exact durable reservation")
	}
	facts := linuxV2AnchorReapFacts{helperExit: stop.HelperExit.Clone()}
	if stop.Launcher != nil && (reservation.Launcher == nil || !stop.Launcher.Equal(*reservation.Launcher)) {
		return linuxV2AnchorReapFacts{}, errors.New("anchor stop evidence launcher differs from the durable reservation")
	}
	if stop.Gate != nil {
		if err := validateLinuxV2AnchorGate(*stop.Gate, reservation); err != nil {
			return linuxV2AnchorReapFacts{}, err
		}
		if reservation.Gate != nil && !reservation.Gate.Equal(*stop.Gate) {
			return linuxV2AnchorReapFacts{}, errors.New("anchor stop evidence gate differs from the durable reservation")
		}
		gate := *stop.Gate
		facts.gate = &gate
	}
	if stop.ProducerFence != nil {
		producer := stop.ProducerFence
		if reservation.Launcher != nil && producer.Launcher != nil && !producer.Launcher.Equal(*reservation.Launcher) ||
			reservation.LauncherExit != nil && producer.LauncherExit != nil && !reservation.LauncherExit.Equal(*producer.LauncherExit) ||
			reservation.Gate != nil && producer.Gate != nil && !reservation.Gate.Equal(*producer.Gate) {
			return linuxV2AnchorReapFacts{}, errors.New("anchor producer fence differs from the exact durable launcher or gate")
		}
		if producer.LauncherExit != nil {
			proof := producer.LauncherExit.Clone()
			facts.launcherExit = &proof
		}
	}
	if reservation.LauncherExit != nil {
		if facts.launcherExit != nil && !reservation.LauncherExit.Equal(*facts.launcherExit) {
			return linuxV2AnchorReapFacts{}, errors.New("anchor producer fence launcher wait differs from its durable receipt")
		}
		proof := reservation.LauncherExit.Clone()
		facts.launcherExit = &proof
	}
	if stop.GateOutcome != nil {
		if reservation.Launcher == nil || stop.Gate != nil ||
			stop.GateOutcome.ValidateFor(reservation.LaunchToken, *reservation.Launcher) != nil {
			return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap no-gate result differs from the exact launcher")
		}
	}
	if reservation.Gate != nil {
		if facts.gate != nil && !facts.gate.Equal(*reservation.Gate) {
			return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap gate differs from its durable binding")
		}
		gate := *reservation.Gate
		facts.gate = &gate
	}
	var launcherReap *authority.LinuxCgroupExitProof
	var gateReap *authority.LinuxCgroupExitProof
	for index := range reaped.Processes {
		process := &reaped.Processes[index]
		if reservation.Launcher != nil && process.Process.Equal(*reservation.Launcher) && process.ProcessRole != "launcher" {
			return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap set mislabeled the exact launcher")
		}
		if facts.gate != nil && process.Process.Equal(facts.gate.Process) &&
			process.ProcessRole != "gate" && process.ProcessRole != "native" {
			return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap set mislabeled the exact gate")
		}
		switch process.ProcessRole {
		case "launcher":
			if reservation.Launcher == nil || !process.Process.Equal(*reservation.Launcher) || launcherReap != nil {
				return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap set contains a launcher outside the exact reservation")
			}
			proof := process.ProcessExit.Clone()
			launcherReap = &proof
		case "gate", "native":
			if facts.gate == nil || !process.Process.Equal(facts.gate.Process) || gateReap != nil {
				return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap set contains a gate/native process outside the exact gate")
			}
			if process.ProcessRole == "native" && (!stop.GateReleaseAuthorized || stop.Gate == nil ||
				!stop.Gate.Equal(*facts.gate)) {
				return linuxV2AnchorReapFacts{}, errors.New("anchor native-process reap lacks the exact gate and release authorization")
			}
			proof := process.ProcessExit.Clone()
			gateReap = &proof
		case "descendant":
			if facts.gate != nil && process.Process.Equal(facts.gate.Process) {
				return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap set mislabeled the exact gate as a descendant")
			}
		}
	}
	if reservation.Launcher != nil {
		if launcherReap != nil {
			if !launcherReap.ValidForOwnedDescendant(*reservation.Launcher, *reservation.Helper, reservation.Anchor, &facts.helperExit) {
				return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap launcher exit differs from the exact producer")
			}
			if facts.launcherExit == nil {
				facts.launcherExit = launcherReap
			}
		}
		if facts.launcherExit == nil || !facts.launcherExit.ValidForOwnedDescendant(*reservation.Launcher, *reservation.Helper, reservation.Anchor, &facts.helperExit) {
			return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap result lacks the exact launcher wait needed for recovery")
		}
	} else if facts.launcherExit != nil || launcherReap != nil || facts.gate != nil || stop.GateOutcome != nil {
		return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap result invented a producer for a helper-only reservation")
	}
	if facts.gate != nil {
		if stop.GateOutcome != nil || gateReap == nil ||
			!gateReap.ValidForOwnedDescendant(facts.gate.Process, *reservation.Helper, reservation.Anchor, &facts.helperExit) {
			return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap result lacks the exact gate wait/reap proof")
		}
		facts.gateExit = gateReap
	} else if gateReap != nil {
		return linuxV2AnchorReapFacts{}, errors.New("anchor run-reap gate exit has no exact gate identity")
	}
	return facts, nil
}

func validateLinuxV2AnchorGate(gate authority.LinuxCgroupGateIdentity, reservation state.LinuxCgroupReservation) error {
	if gate.Validate() != nil || reservation.LeafIdentity == nil || gate.HostUID != reservation.OwnerUID ||
		gate.BirthKind != authority.LinuxCgroupTargetBirthTrustedAGate || !gate.BornInLeaf || !gate.TrustedImage ||
		!gate.Cgroup.Equal(*reservation.LeafIdentity) {
		return errors.New("anchor gate differs from the exact trusted A gate in the reserved leaf")
	}
	return nil
}

func linuxV2MirrorStopProofFromAnchorReap(
	reservation state.LinuxCgroupReservation,
	reaped platform.LinuxOwnerAnchorRunReapResult,
	facts linuxV2AnchorReapFacts,
) (authority.LinuxCgroupStopProof, error) {
	if reservation.Helper == nil || reservation.Launcher == nil || reservation.LauncherExit == nil ||
		reservation.Gate == nil || reservation.Mirror == nil || facts.gateExit == nil ||
		facts.helperExit.Validate() != nil || reaped.Stop.HelperExit == nil || !reaped.Stop.HelperExit.Equal(facts.helperExit) {
		return authority.LinuxCgroupStopProof{}, errors.New("anchor mirror stop lacks the exact gate, launcher, helper, or wait evidence")
	}
	helperExit := facts.helperExit.Clone()
	proof := authority.LinuxCgroupStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofMirror,
		Issuer: *reservation.Mirror, Reaper: reservation.Anchor,
		PinnedLeafFD: reaped.Stop.PinnedLeafFD, CgroupKillAcknowledged: reaped.Stop.KillAcknowledged,
		CgroupPopulatedZero: reaped.Stop.CgroupPopulatedZero, NoMigrationObserved: reaped.Stop.NoMigrationObserved,
		ProducerFenced: reaped.Stop.ProducerFenced, DescendantsReaped: reaped.Stop.DescendantsReaped,
		LauncherExit: reservation.LauncherExit.Clone(), GateExit: facts.gateExit.Clone(),
		HelperExit: &helperExit,
	}
	handoff, err := linuxV2HandoffFromReservation(reservation)
	if err != nil {
		return authority.LinuxCgroupStopProof{}, err
	}
	handoff.SupervisorPID = reservation.Helper.PID
	handoff.SupervisorIdentity = reservation.Helper.Identity
	mirror := *reservation.Mirror
	handoff.Mirror = &mirror
	value, err := handoff.ToSupervisor()
	if err != nil {
		return authority.LinuxCgroupStopProof{}, fmt.Errorf("convert exact durable mirror handoff: %w", err)
	}
	if err := proof.ValidateFor(value); err != nil {
		return authority.LinuxCgroupStopProof{}, fmt.Errorf("validate exact anchor mirror stop proof: %w", err)
	}
	return proof, nil
}

// linuxV2AnchorReportsGateRelease is checked before accepting B/native evidence
// while the durable RunJournal has no committed v2 Authority. GateReleaseAuthorized
// is anchor state for the helper-death case where no helper ProducerFence survived.
func linuxV2AnchorReportsGateRelease(stop platform.LinuxOwnerAnchorStopEvidence) bool {
	return stop.GateReleaseAuthorized ||
		stop.ProducerFence != nil && (stop.ProducerFence.ReleaseAttempted || stop.ProducerFence.Released)
}

func linuxV2PreGateReceiptFromAnchorReap(
	grant platform.LinuxOwnerAnchorGrant,
	reservation state.LinuxCgroupReservation,
	reaped platform.LinuxOwnerAnchorRunReapResult,
	facts linuxV2AnchorReapFacts,
) (authority.LinuxCgroupPreGateStopReceipt, error) {
	stop := reaped.Stop
	if reservation.Helper == nil || reservation.Mirror == nil || reservation.Launcher == nil ||
		reservation.LauncherExit == nil || reservation.LeafIdentity == nil || reservation.Gate != nil ||
		!reservation.LauncherGrantPossible || stop.Gate != nil || stop.GateOutcome == nil ||
		facts.gate != nil || facts.gateExit != nil || facts.launcherExit == nil ||
		!facts.launcherExit.Equal(*reservation.LauncherExit) || facts.helperExit.Validate() != nil {
		return authority.LinuxCgroupPreGateStopReceipt{}, errors.New("anchor no-gate stop lacks the exact launcher MAC, helper, or reserved leaf")
	}
	if stop.Launcher != nil && !stop.Launcher.Equal(*reservation.Launcher) ||
		stop.GateOutcome.ValidateFor(reservation.LaunchToken, *reservation.Launcher) != nil {
		return authority.LinuxCgroupPreGateStopReceipt{}, errors.New("anchor no-gate stop differs from the exact launcher identity")
	}
	helperExit := facts.helperExit.Clone()
	proof := authority.LinuxCgroupPreGateStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofMirror,
		Issuer: *reservation.Mirror, Reaper: reservation.Anchor,
		PinnedLeafFD: stop.PinnedLeafFD, CgroupKillAcknowledged: stop.KillAcknowledged,
		CgroupPopulatedZero: stop.CgroupPopulatedZero, NoMigrationObserved: stop.NoMigrationObserved,
		ProducerFenced: stop.ProducerFenced, DescendantsReaped: stop.DescendantsReaped,
		LauncherExit: reservation.LauncherExit.Clone(), HelperExit: &helperExit,
		GateOutcome: *stop.GateOutcome,
	}
	receipt := authority.LinuxCgroupPreGateStopReceipt{
		Version: authority.LinuxCgroupIdentityVersion, LaunchToken: reservation.LaunchToken,
		CgroupIdentity: *reservation.LeafIdentity, OwnerUID: reservation.OwnerUID, LauncherUID: reservation.LauncherUID,
		Anchor: reservation.Anchor, Helper: *reservation.Helper, Launcher: *reservation.Launcher,
		Mirror: *reservation.Mirror, Proof: proof,
	}
	if err := receipt.Validate(); err != nil {
		return authority.LinuxCgroupPreGateStopReceipt{}, fmt.Errorf("validate anchor mirror pre-gate receipt: %w", err)
	}
	if !receipt.ValidFor(
		grant.LaunchToken, grant.Leaf, grant.OwnerUID, grant.LauncherUID,
		grant.Anchor, *reservation.Helper, *reservation.Launcher, *reservation.Mirror,
		*reservation.LauncherExit, &facts.helperExit,
	) {
		return authority.LinuxCgroupPreGateStopReceipt{}, errors.New("anchor mirror pre-gate receipt differs from the exact reservation and helper wait")
	}
	return receipt, nil
}

func linuxV2HandoffFromReservation(reservation state.LinuxCgroupReservation) (authority.SupervisorHandoff, error) {
	if reservation.Validate() != nil || reservation.Helper == nil || reservation.Launcher == nil ||
		reservation.LauncherExit == nil || reservation.Gate == nil || reservation.LeafIdentity == nil {
		return authority.SupervisorHandoff{}, errors.New("Linux v2 reservation is incomplete for handoff reconstruction")
	}
	gate := reservation.Gate
	leaf := *reservation.LeafIdentity
	handoff := authority.SupervisorHandoff{
		Version: authority.SupervisorHandoffVersionV2, LaunchToken: reservation.LaunchToken, Secret: reservation.Secret,
		OwnerKind: authority.OwnerKindLinuxHelper, OwnerContext: reservation.RecoveryEndpoint,
		TargetPID: gate.Process.PID, TargetIdentity: gate.Process.Identity,
		OwnerUID: reservation.OwnerUID, LauncherUID: reservation.LauncherUID,
		PipeToken: reservation.PipeToken, JobID: reservation.JobID,
		TargetBirthKind: gate.BirthKind, CgroupIdentity: &leaf,
		AnchorPID: reservation.Anchor.PID, AnchorIdentity: reservation.Anchor.Identity,
		LauncherPID: reservation.Launcher.PID, LauncherIdentity: reservation.Launcher.Identity,
		LauncherExit: reservation.LauncherExit,
	}
	if err := handoff.Validate(); err != nil {
		return authority.SupervisorHandoff{}, fmt.Errorf("validate reconstructed v2 handoff: %w", err)
	}
	return handoff, nil
}

func recoverLinuxV2GateStop(
	ctx context.Context,
	daemon *daemon,
	parent *platform.LinuxCgroupV2Parent,
	anchor *platform.LinuxOwnerAnchorClient,
	helper *platform.LinuxOwnerAnchorHelper,
	grant platform.LinuxOwnerAnchorGrant,
	journal state.RunJournal,
	reservation state.LinuxCgroupReservation,
	callbacks execution.LinuxV2JournalCallbacks,
	observed *platform.LinuxOwnerHelperV2StopEvidence,
	reaped *platform.LinuxOwnerAnchorRunReapResult,
) error {
	handoff, supervisor, receipt, err := linuxV2JournalOwner(journal)
	if err != nil {
		return err
	}
	if receipt == nil {
		var stopProof authority.LinuxCgroupStopProof
		if reaped != nil || helper.HelperExit != nil || reservation.HelperExit != nil || helper.Control == nil {
			if reaped == nil {
				measured, waitErr := anchor.WaitReapRun(ctx, grant, *reservation.Helper)
				if waitErr != nil {
					return fmt.Errorf("anchor could not stop and reap the exact v2 RunKey after helper death: %w", waitErr)
				}
				reaped = &measured
			}
			facts, factsErr := linuxV2AnchorReapFactsForReservation(grant, reservation, *reaped)
			if factsErr != nil {
				return factsErr
			}
			if facts.gate == nil || facts.gateExit == nil || reservation.Gate == nil || !facts.gate.Equal(*reservation.Gate) {
				return errors.New("anchor run-reap result lacks the exact durable Linux v2 gate")
			}
			if supervisor == nil && linuxV2AnchorReportsGateRelease(reaped.Stop) {
				return errors.New("pending handoff cannot authorize a reported B RELEASE attempt")
			}
			if helper.HelperExit != nil && !helper.HelperExit.Equal(facts.helperExit) {
				return errors.New("anchor run-reap helper exit differs from the exact RunKey lookup")
			}
			journal, reservation, err = persistRecoveredV2HelperExit(ctx, daemon, journal, reservation, facts.helperExit, callbacks)
			if err != nil {
				return err
			}
			stopProof, err = linuxV2MirrorStopProofFromAnchorReap(reservation, *reaped, facts)
			if err != nil {
				return err
			}
		} else {
			if observed == nil {
				var evidence platform.LinuxOwnerHelperV2StopEvidence
				evidence, err = helper.Control.Stop(ctx)
				if err != nil {
					return fmt.Errorf("request exact helper stop proof: %w", err)
				}
				observed = &evidence
			}
			if observed.PreGate != nil {
				return errors.New("pre-gate stop evidence requires the separate no-target receipt path")
			}
			if reservation.Launcher == nil || reservation.Gate == nil {
				return errors.New("Linux v2 stop evidence has no durable launcher and gate identity")
			}
			if err := observed.ValidateFor(grant, *reservation.Helper, *reservation.Launcher, *reservation.Gate); err != nil {
				return fmt.Errorf("validate exact helper physical stop observations: %w", err)
			}
			if supervisor == nil && (observed.AnchorStop == nil || linuxV2AnchorReportsGateRelease(*observed.AnchorStop)) {
				return errors.New("pending handoff cannot authorize a helper-reported B RELEASE attempt")
			}
			stopProof = observed.Proof
		}
		var value authority.Supervisor
		if supervisor != nil {
			value = supervisor.Clone()
			value.StopReceipt = nil
		} else {
			value, err = handoff.ToSupervisor()
			if err != nil {
				return fmt.Errorf("convert exact Linux v2 handoff before stop receipt: %w", err)
			}
		}
		stopReceipt := linuxV2StopReceipt(value, stopProof)
		if !stopReceipt.ValidFor(value) {
			return errors.New("measured physical stop observations do not form the exact durable v2 receipt")
		}
		if err := callbacks.RecordStopReceipt(handoff, stopReceipt); err != nil {
			return fmt.Errorf("persist observed Linux v2 stop receipt: %w", err)
		}
		receipt = &stopReceipt
	}
	if receipt == nil || handoff.StopReceipt != nil && !handoff.StopReceipt.Equal(*receipt) ||
		supervisor != nil && !receipt.ValidFor(*supervisor) ||
		supervisor == nil && !receipt.ValidForHandoff(handoff) {
		return errors.New("durable Linux v2 stop receipt differs from the exact owner")
	}
	if err := callbacks.RecordStopReceipt(handoff, *receipt); err != nil {
		return fmt.Errorf("replay exact durable Linux v2 stop receipt: %w", err)
	}
	latest, err := daemon.store.LoadJournal(journal.Key())
	if err != nil {
		return fmt.Errorf("read back durable Linux v2 stop receipt: %w", err)
	}
	if supervisor != nil {
		if latest.ContainmentAuthority == nil || latest.ContainmentAuthority.StopReceipt == nil || !latest.ContainmentAuthority.StopReceipt.Equal(*receipt) {
			return errors.New("committed Linux v2 stop receipt readback differs from the exact authority")
		}
		supervisorCopy := latest.ContainmentAuthority.Clone()
		supervisor = &supervisorCopy
	} else {
		if latest.ContainmentHandoff == nil || latest.ContainmentHandoff.StopReceipt == nil || !latest.ContainmentHandoff.StopReceipt.Equal(*receipt) {
			return errors.New("pending Linux v2 stop receipt readback differs from the exact handoff")
		}
		handoff = latest.ContainmentHandoff.Clone()
	}
	if helper.Control != nil && helper.HelperExit == nil && reaped == nil {
		if err := helper.Control.AcknowledgeStopReceipt(ctx, *receipt); err != nil {
			return fmt.Errorf("acknowledge durable Linux v2 stop receipt to helper: %w", err)
		}
	}
	identity := *reservation.LeafIdentity
	removal := platform.LinuxCgroupV2RemovalAuthorization{Handoff: &handoff, Receipt: *receipt}
	if supervisor != nil {
		removal.Handoff = nil
		removal.Supervisor = supervisor
	}
	leaf, leafErr := parent.AdoptLeaf(identity.LeafName, int(helper.LeafFD.Fd()), identity)
	var release platform.LinuxCgroupV2Release
	if leafErr == nil {
		release, leafErr = leaf.RemoveAfterStopReceipt(ctx, removal)
		_ = leaf.Close()
	}
	if leafErr != nil {
		release, err = parent.CompleteRemovalAfterStopReceipt(ctx, identity, removal)
		if err != nil {
			return errors.Join(fmt.Errorf("remove exact stopped Linux v2 leaf: %w", leafErr), fmt.Errorf("complete only receipt-backed exact absence: %w", err))
		}
	}
	proof, issued := release.Snapshot()
	if !issued {
		return errors.New("Linux cgroup platform returned no opaque post-removal release")
	}
	latest, err = daemon.store.LoadJournal(journal.Key())
	if err != nil {
		return fmt.Errorf("reload RunJournal before clone promotion reconciliation: %w", err)
	}
	if err := daemon.reconcileLinuxClonePromotion(ctx, latest, release); err != nil {
		return fmt.Errorf("reconcile exact Linux clone promotion before containment clear: %w", err)
	}
	if supervisor != nil {
		if err := callbacks.ClearProcessDetailsV2(supervisor.TargetPID, supervisor.TargetIdentity, release); err != nil {
			return fmt.Errorf("clear committed Linux v2 process after physical removal: %w", err)
		}
	} else {
		if err := callbacks.ClearHandoffV2(handoff, release); err != nil {
			return fmt.Errorf("clear pending Linux v2 handoff after physical removal: %w", err)
		}
	}
	if helper.Control != nil {
		if err := helper.Control.ReleaseAfterClear(ctx, proof); err != nil {
			return fmt.Errorf("release helper after durable v2 clear: %w", err)
		}
	}
	if anchor == nil {
		return errors.New("Linux v2 helper retirement lacks the authenticated owner anchor")
	}
	if err := anchor.RetireHelper(ctx, grant, *reservation.Helper, proof); err != nil {
		return fmt.Errorf("retire exact Linux v2 helper after release: %w", err)
	}
	return nil
}

func recoverLinuxV2PreGateStop(
	ctx context.Context,
	daemon *daemon,
	parent *platform.LinuxCgroupV2Parent,
	anchor *platform.LinuxOwnerAnchorClient,
	helper *platform.LinuxOwnerAnchorHelper,
	grant platform.LinuxOwnerAnchorGrant,
	reservation state.LinuxCgroupReservation,
	callbacks execution.LinuxV2JournalCallbacks,
) error {
	receipt := reservation.PreGateStopReceipt.Clone()
	if receipt.Validate() != nil || reservation.LeafIdentity == nil || reservation.Helper == nil {
		return errors.New("durable Linux pre-gate receipt has no exact reserved leaf/helper")
	}
	if err := callbacks.RecordPreGateStopReceipt(withoutLinuxV2PreGateReceipt(reservation), receipt); err != nil {
		return fmt.Errorf("replay exact Linux pre-gate physical receipt: %w", err)
	}
	if helper.Control != nil && helper.HelperExit == nil {
		if err := helper.Control.AcknowledgePreGateStopReceipt(ctx, receipt); err != nil {
			return fmt.Errorf("acknowledge durable Linux pre-gate receipt: %w", err)
		}
	}
	identity := *reservation.LeafIdentity
	authorization := platform.LinuxCgroupV2PreGateRemovalAuthorization{
		Receipt: receipt, Secret: reservation.Secret, LauncherGrantPossible: reservation.LauncherGrantPossible,
	}
	leaf, leafErr := parent.AdoptLeaf(identity.LeafName, int(helper.LeafFD.Fd()), identity)
	var release platform.LinuxCgroupV2Release
	if leafErr == nil {
		release, leafErr = leaf.RemoveAfterPreGateStopReceipt(ctx, authorization)
		_ = leaf.Close()
	}
	if leafErr != nil {
		var completionErr error
		release, completionErr = parent.CompletePreGateRemovalAfterReceipt(ctx, identity, authorization)
		if completionErr != nil {
			return errors.Join(fmt.Errorf("remove exact receipt-stopped pre-gate leaf: %w", leafErr), fmt.Errorf("complete only receipt-backed pre-gate absence: %w", completionErr))
		}
	}
	proof, issued := release.SnapshotPreGate()
	if !issued || !proof.ValidFor(receipt) {
		return errors.New("Linux platform returned no exact opaque pre-gate release")
	}
	latest, err := daemon.store.LoadJournal(state.RunKey{RunID: grant.RunKey.RunID, Generation: grant.RunKey.Generation})
	if err != nil || latest.ContainmentCgroup == nil {
		return fmt.Errorf("read exact pre-gate reservation before clear: %w", appErrorOr(err, "reservation is absent"))
	}
	if err := callbacks.ClearReservationV2(*latest.ContainmentCgroup, release); err != nil {
		return fmt.Errorf("clear receipt-released pre-gate reservation: %w", err)
	}
	if helper.Control != nil && helper.HelperExit == nil {
		if err := helper.Control.ReleaseAfterPreGateClear(ctx, proof); err != nil {
			return fmt.Errorf("release helper after durable pre-gate clear: %w", err)
		}
	}
	if anchor == nil {
		return errors.New("pre-gate helper retirement lacks the authenticated owner anchor")
	}
	if err := anchor.RetirePreGateHelper(ctx, grant, *reservation.Helper, proof); err != nil {
		return fmt.Errorf("retire pre-gate helper after release: %w", err)
	}
	return nil
}

func linuxV2PreGateReceipt(
	reservation state.LinuxCgroupReservation,
	grant platform.LinuxOwnerAnchorGrant,
	helper authority.LinuxCgroupProcessIdentity,
	evidence platform.LinuxOwnerHelperV2StopEvidence,
) (authority.LinuxCgroupPreGateStopReceipt, error) {
	preGate := evidence.PreGate
	if preGate == nil || preGate.ValidateFor(grant, helper) != nil || !preGate.PrepareAttempted ||
		preGate.Launcher == nil || preGate.LauncherExit == nil || preGate.Proof == nil ||
		reservation.LeafIdentity == nil || !reservation.LauncherGrantPossible || reservation.Launcher == nil ||
		reservation.LauncherExit == nil || reservation.Helper == nil {
		return authority.LinuxCgroupPreGateStopReceipt{}, errors.New("no-gate evidence is not eligible for a launcher-granted pre-gate stop receipt")
	}
	if !preGate.Launcher.Equal(*reservation.Launcher) || !preGate.LauncherExit.Equal(*reservation.LauncherExit) ||
		preGate.Proof.GateOutcome.ValidateFor(reservation.LaunchToken, *reservation.Launcher) != nil {
		return authority.LinuxCgroupPreGateStopReceipt{}, errors.New("no-gate evidence differs from the exact durable launcher result")
	}
	receipt := authority.LinuxCgroupPreGateStopReceipt{
		Version: authority.LinuxCgroupIdentityVersion, LaunchToken: reservation.LaunchToken,
		CgroupIdentity: *reservation.LeafIdentity, OwnerUID: reservation.OwnerUID, LauncherUID: reservation.LauncherUID,
		Anchor: reservation.Anchor, Helper: helper, Launcher: *reservation.Launcher, Mirror: *reservation.Mirror,
		Proof: *preGate.Proof,
	}
	var helperExit *authority.LinuxCgroupExitProof
	if reservation.HelperExit != nil {
		copy := reservation.HelperExit.Clone()
		helperExit = &copy
	}
	if err := receipt.Validate(); err != nil {
		return authority.LinuxCgroupPreGateStopReceipt{}, fmt.Errorf("validate exact Linux pre-gate receipt from helper evidence: %w", err)
	}
	if !receipt.ValidFor(
		grant.LaunchToken, *reservation.LeafIdentity, reservation.OwnerUID, reservation.LauncherUID,
		reservation.Anchor, helper, *reservation.Launcher, *reservation.Mirror, *reservation.LauncherExit, helperExit,
	) {
		return authority.LinuxCgroupPreGateStopReceipt{}, errors.New("exact Linux pre-gate receipt differs from its State helper-exit binding")
	}
	return receipt, nil
}

func withoutLinuxV2PreGateReceipt(reservation state.LinuxCgroupReservation) state.LinuxCgroupReservation {
	copy := reservation.Clone()
	copy.PreGateStopReceipt = nil
	return copy
}

func linuxV2JournalOwner(journal state.RunJournal) (authority.SupervisorHandoff, *authority.Supervisor, *authority.StopReceipt, error) {
	if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Version == authority.SupervisorHandoffVersionV2 && journal.ContainmentAuthority == nil {
		handoff := journal.ContainmentHandoff.Clone()
		if handoff.StopReceipt == nil {
			return handoff, nil, nil, nil
		}
		receipt := *handoff.StopReceipt
		return handoff, nil, &receipt, nil
	}
	if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.Version != authority.SupervisorVersionV2 || journal.ContainmentHandoff != nil {
		return authority.SupervisorHandoff{}, nil, nil, errors.New("Linux v2 journal has no exact handoff/authority owner")
	}
	supervisor := journal.ContainmentAuthority.Clone()
	var receipt *authority.StopReceipt
	if supervisor.StopReceipt != nil {
		copy := cloneLinuxV2StopReceipt(*supervisor.StopReceipt)
		receipt = &copy
		supervisor.StopReceipt = nil
	}
	handoff := linuxV2HandoffFromSupervisor(supervisor)
	return handoff, &supervisor, receipt, nil
}

func linuxV2HandoffFromSupervisor(value authority.Supervisor) authority.SupervisorHandoff {
	value = value.Clone()
	return authority.SupervisorHandoff{
		Version:     authority.SupervisorHandoffVersionV2,
		LaunchToken: value.LaunchToken, Secret: value.Secret, OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext,
		TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, OwnerUID: value.OwnerUID, LauncherUID: value.LauncherUID,
		PipeToken: value.PipeToken, JobID: value.JobID, SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity,
		TargetBirthKind: value.TargetBirthKind, CgroupIdentity: value.CgroupIdentity,
		AnchorPID: value.AnchorPID, AnchorIdentity: value.AnchorIdentity, LauncherPID: value.LauncherPID,
		LauncherIdentity: value.LauncherIdentity, LauncherExit: value.LauncherExit, Mirror: value.Mirror,
		CreatorSessionID: value.CreatorSessionID,
	}
}

func linuxV2FenceFromJournal(journal state.RunJournal) protocol.Fence {
	return protocol.Fence{
		RuntimeID: journal.RuntimeID, RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation: journal.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken,
	}
}

func linuxV2GrantFromJournal(key state.RunKey, fence protocol.Fence, reservation state.LinuxCgroupReservation) platform.LinuxOwnerAnchorGrant {
	var leaf authority.LinuxCgroupIdentity
	if reservation.LeafIdentity != nil {
		leaf = *reservation.LeafIdentity
	}
	return platform.LinuxOwnerAnchorGrant{
		ReservationVersion: state.LinuxCgroupReservationVersion,
		RunKey:             platform.LinuxOwnerAnchorRunKey{RunID: key.RunID, Generation: key.Generation}, Fence: fence,
		LaunchToken: reservation.LaunchToken, Secret: reservation.Secret, PipeToken: reservation.PipeToken,
		JobID: reservation.JobID, OwnerUID: reservation.OwnerUID, LauncherUID: reservation.LauncherUID,
		RecoveryEndpoint: reservation.RecoveryEndpoint, CreateStarted: reservation.CreateStarted,
		Anchor: reservation.Anchor, Parent: reservation.Parent, Leaf: leaf,
	}
}

func linuxV2StopReceipt(value authority.Supervisor, proof authority.LinuxCgroupStopProof) authority.StopReceipt {
	return authority.StopReceipt{
		Version: authority.SupervisorVersionV2, Status: "stopped", OwnerKind: value.OwnerKind,
		OwnerContext: value.OwnerContext, TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity,
		OwnerUID: value.OwnerUID, LauncherUID: value.LauncherUID, PipeToken: value.PipeToken, JobID: value.JobID,
		SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, ActiveProcesses: 0,
		TargetBirthKind: value.TargetBirthKind, CgroupIdentity: value.CgroupIdentity, LinuxProof: &proof,
	}
}

func runLinuxContainmentOnlyRecovery(ctx context.Context, value config.Config) (result error) {
	if ctx == nil {
		return errors.New("Linux containment-only recovery requires a context and StateDir")
	}
	stateDir, err := canonicalLinuxOwnerStateDir(value.StateDir)
	if err != nil {
		return fmt.Errorf("normalize Linux containment-only StateDir against root working directory: %w", err)
	}
	value.StateDir = stateDir
	if value.StateDir == "" || !filepath.IsAbs(value.StateDir) || filepath.Clean(value.StateDir) != value.StateDir {
		return errors.New("Linux containment-only recovery requires a context and StateDir")
	}
	ctx, cancel := context.WithTimeout(ctx, value.CleanupTimeout())
	defer cancel()
	store, err := state.New(value.StateDir)
	if err != nil {
		return fmt.Errorf("acquire exclusive StateDir Store lock for recovery-only command: %w", err)
	}
	recovery := &daemon{
		config: value,
		store:  store,
		options: options{
			store: store, clock: time.Now,
			terminatePersist: func(pid int, identity string) error {
				return terminatePersistedProcessWithContext(ctx, pid, identity)
			},
			terminatePersistAuthority:             terminatePersistedProcessWithAuthority,
			terminatePersistAuthorityWithScanSeal: terminatePersistedProcessWithAuthorityScanSeal,
		},
		recoveryOnly:       true,
		background:         ctx,
		running:            make(map[state.RunKey]*runningRun),
		recoveredStops:     make(map[state.RunKey]recoveredProcessStop),
		retainedWorkspaces: make(map[state.RunKey]struct{}),
	}
	defer func() {
		recovery.close()
		result = errors.Join(result, store.Close())
	}()
	journals, err := store.ListJournals()
	if err != nil {
		return fmt.Errorf("read all RunJournals under recovery-only lock: %w", err)
	}
	return recoverContainmentOnlyJournals(ctx, recovery, journals)
}

// recoverContainmentOnlyJournals performs only physical-containment recovery
// and durable proof/clear mutations. The caller must already own the Store's
// exclusive StateDir lock.
func recoverContainmentOnlyJournals(ctx context.Context, recovery *daemon, journals []state.RunJournal) error {
	var unresolved error
	for _, journal := range journals {
		if err := ctx.Err(); err != nil {
			return errors.Join(unresolved, err)
		}
		switch {
		case hasLinuxV2ContainmentState(journal):
			if err := recoverLinuxV2OwnerJournal(ctx, recovery, journal); err != nil {
				unresolved = errors.Join(unresolved, fmt.Errorf("recover v2 owner %s/%d: %w", journal.RunID, journal.Generation, err))
			}
		case journal.ContainmentHandoff != nil:
			if err := recovery.recoverPersistedSupervisorHandoff(journal.Key(), *journal.ContainmentHandoff); err != nil {
				unresolved = errors.Join(unresolved, fmt.Errorf("recover v1 handoff %s/%d: %w", journal.RunID, journal.Generation, err))
			}
		case journal.HasProcessDetails():
			if err := recovery.stopPersistedProcess(ctx, journal); err != nil {
				unresolved = errors.Join(unresolved, fmt.Errorf("stop legacy process %s/%d: %w", journal.RunID, journal.Generation, err))
				continue
			}
			if _, err := recovery.clearPersistedProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity); err != nil {
				unresolved = errors.Join(unresolved, fmt.Errorf("clear legacy process %s/%d after proof: %w", journal.RunID, journal.Generation, err))
			}
		case journal.HasPendingContainment() || journal.ContainmentUnproven:
			unresolved = errors.Join(unresolved, fmt.Errorf("legacy containment %s/%d has no exact process identity", journal.RunID, journal.Generation))
		}
	}
	return unresolved
}
