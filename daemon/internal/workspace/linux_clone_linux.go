//go:build linux && amd64

package workspace

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"golang.org/x/sys/unix"
)

const (
	maxLinuxCloneBundleBytes      = authority.LinuxCloneMaxSeedBytes
	maxLinuxCloneObjectBytes      = int64(4 << 30)
	maxLinuxCloneObjectFiles      = 250_000
	maxLinuxCloneBundleHeaderSize = 64 << 10
	linuxCloneGitCommandTimeout   = 5 * time.Minute
	linuxCloneGitKillTimeout      = 5 * time.Second
)

var (
	errLinuxCloneBundleTooLarge     = errors.New("Linux clone seed bundle exceeds size limit")
	errLinuxCloneObjectsTooLarge    = errors.New("Linux clone object store exceeds size limit")
	errLinuxCloneUnsupportedObjects = errors.New("Linux clone object store layout is unsupported")
)

// ErrLinuxCloneProcessUnresolved means an A-side Git process group could not
// be proven stopped. Callers must retain the RunJournal and must not advance
// clone abort, promotion, or cleanup.
var ErrLinuxCloneProcessUnresolved = errors.New("Linux clone Git process-group stop is unresolved")

// PlanLinuxCloneIntent creates a no-effect intent proposal for the caller to
// durably reserve in State. Reservation, not this returned value, establishes
// authority. This method reads only local configuration; it does not inspect
// or mutate either repository or filesystem.
func (manager *Manager) PlanLinuxCloneIntent(bindingKey string, run RunRef, subject protocol.Subject) (authority.LinuxCloneIntent, error) {
	if manager == nil {
		return authority.LinuxCloneIntent{}, errors.New("workspace manager is nil")
	}
	if err := validateRun(run); err != nil {
		return authority.LinuxCloneIntent{}, err
	}
	if err := subject.Validate(); err != nil {
		return authority.LinuxCloneIntent{}, fmt.Errorf("validate Linux clone base subject: %w", err)
	}
	if !validFingerprintBindingKey(bindingKey) {
		return authority.LinuxCloneIntent{}, errors.New("workspace binding key is invalid")
	}
	binding, ok := manager.bindings[bindingKey]
	if !ok {
		return authority.LinuxCloneIntent{}, fmt.Errorf("workspace binding %q is not configured", bindingKey)
	}
	if binding.Policy != config.WorkspacePolicyGitCloneV2 {
		return authority.LinuxCloneIntent{}, fmt.Errorf("workspace binding %q does not use git_clone_v2", bindingKey)
	}
	if !canonicalLinuxClonePath(binding.Repository) || !canonicalLinuxClonePath(binding.Root) || !canonicalLinuxClonePath(binding.SeedRoot) {
		return authority.LinuxCloneIntent{}, errors.New("Linux clone binding paths must be canonical absolute paths")
	}

	nonce := make([]byte, authority.LinuxCloneNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return authority.LinuxCloneIntent{}, fmt.Errorf("generate Linux clone nonce: %w", err)
	}
	seedPath := filepath.Join(binding.SeedRoot, "binding-"+bindingKey, "run-"+run.RunID, fmt.Sprintf("generation-%d.bundle", run.Generation))
	target := linuxCloneTargetPath(binding.Root, bindingKey, run)
	intent := authority.LinuxCloneIntent{
		Version:        authority.LinuxCloneVersion,
		RunID:          run.RunID,
		Generation:     run.Generation,
		BindingKey:     bindingKey,
		ResourceID:     subject.ResourceID,
		BaseCommit:     subject.Commit,
		BaseTreeDigest: subject.TreeDigest,
		Nonce:          hex.EncodeToString(nonce),
		SeedPath:       seedPath,
		Target:         target,
	}
	if err := intent.Validate(); err != nil {
		return authority.LinuxCloneIntent{}, fmt.Errorf("construct Linux clone intent: %w", err)
	}
	return intent, nil
}

// PrepareLinuxCloneSeed compare-creates the reserved A-owned base ref and
// publishes its bounded, immutable, single-ref Git bundle. The caller must
// have durably reserved intent before invoking this method; this package never
// writes State.
func (manager *Manager) PrepareLinuxCloneSeed(ctx context.Context, intent authority.LinuxCloneIntent) (authority.LinuxCloneSeedReceipt, error) {
	if manager == nil {
		return authority.LinuxCloneSeedReceipt{}, errors.New("workspace manager is nil")
	}
	if ctx == nil {
		return authority.LinuxCloneSeedReceipt{}, errors.New("Linux clone seed context is nil")
	}
	if err := ctx.Err(); err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	binding, repository, err := manager.linuxCloneBinding(intent)
	if err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	if err := verifyLinuxCloneSource(ctx, repository); err != nil {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("validate A-owned Git source: %w", err)
	}
	if err := verifyLinuxCloneBase(ctx, repository, intent); err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	baseRef, err := authority.LinuxCloneBaseRef(intent.RunID, intent.Generation)
	if err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, baseRef); err != nil {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("A-owned base ref lock is unresolved: %w", err)
	}
	if err := ensureLinuxCloneBaseRef(ctx, repository, baseRef, intent.BaseCommit); err != nil {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("pin A-owned Linux clone base ref: %w", err)
	}
	parentFD, name, err := openLinuxCloneSeedParent(binding.SeedRoot, intent.SeedPath, true)
	if err != nil {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("open A-owned Linux clone seed parent: %w", err)
	}
	defer unix.Close(parentFD)

	if receipt, err := readLinuxCloneSeed(ctx, repository, parentFD, name, intent, baseRef, nil); err == nil {
		if err := freezeLinuxCloneSeed(parentFD, name, receipt); err != nil {
			return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("freeze existing Linux clone seed: %w", err)
		}
		if err := unix.Fsync(parentFD); err != nil {
			return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("sync existing Linux clone seed parent: %w", err)
		}
		return readLinuxCloneSeed(ctx, repository, parentFD, name, intent, baseRef, expectedLinuxCloneSeedDigest(&receipt))
	} else if !errors.Is(err, os.ErrNotExist) {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("verify existing Linux clone seed: %w", err)
	}

	temporary, temporaryName, err := createLinuxCloneSeedTemp(parentFD)
	if err != nil {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("create Linux clone seed temporary: %w", err)
	}
	temporaryOpen := true
	removeTemporary := true
	defer func() {
		if temporaryOpen {
			_ = temporary.Close()
		}
		if removeTemporary {
			_ = unix.Unlinkat(parentFD, temporaryName, 0)
		}
	}()
	temporaryPath := filepath.Join(filepath.Dir(intent.SeedPath), temporaryName)
	digest, size, err := writeLinuxCloneBundle(ctx, repository, baseRef, temporary)
	if err != nil {
		if errors.Is(err, ErrLinuxCloneProcessUnresolved) {
			removeTemporary = false
		}
		return authority.LinuxCloneSeedReceipt{}, err
	}
	if err := temporary.Sync(); err != nil {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("sync Linux clone seed bytes: %w", err)
	}
	if err := verifyLinuxCloneBundle(ctx, repository, temporaryPath, intent, baseRef); err != nil {
		if errors.Is(err, ErrLinuxCloneProcessUnresolved) {
			removeTemporary = false
		}
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("verify generated Linux clone bundle: %w", err)
	}
	if size <= 0 || size > maxLinuxCloneBundleBytes {
		return authority.LinuxCloneSeedReceipt{}, errLinuxCloneBundleTooLarge
	}
	if digest == "" {
		return authority.LinuxCloneSeedReceipt{}, errors.New("Linux clone bundle digest is unavailable")
	}

	if err := unix.Linkat(parentFD, temporaryName, parentFD, name, 0); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("publish immutable Linux clone seed: %w", err)
		}
		if err := temporary.Close(); err != nil {
			return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("close unpublished Linux clone seed temporary: %w", err)
		}
		temporaryOpen = false
		if receipt, verifyErr := readLinuxCloneSeed(ctx, repository, parentFD, name, intent, baseRef, nil); verifyErr == nil {
			if err := freezeLinuxCloneSeed(parentFD, name, receipt); err != nil {
				return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("freeze concurrent Linux clone seed: %w", err)
			}
			if err := unix.Fsync(parentFD); err != nil {
				return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("sync replayed Linux clone seed parent: %w", err)
			}
			return readLinuxCloneSeed(ctx, repository, parentFD, name, intent, baseRef, expectedLinuxCloneSeedDigest(&receipt))
		} else {
			return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("verify concurrent Linux clone seed publication: %w", verifyErr)
		}
	}
	if err := unix.Fchmod(int(temporary.Fd()), 0o440); err != nil {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("make published Linux clone seed immutable: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("sync immutable Linux clone seed: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("close Linux clone seed temporary: %w", err)
	}
	temporaryOpen = false
	if err := unix.Fsync(parentFD); err != nil {
		return authority.LinuxCloneSeedReceipt{}, fmt.Errorf("sync Linux clone seed publication: %w", err)
	}
	return readLinuxCloneSeed(ctx, repository, parentFD, name, intent, baseRef, &linuxCloneExpectedDigest{digest: digest, size: size})
}

// PrepareLinuxClone creates only A-owned Prepared metadata. In particular it
// does not stat or create Intent.Target, and it does not run Git in that path.
func (manager *Manager) PrepareLinuxClone(ctx context.Context, grant authority.LinuxCloneGrant) (Prepared, error) {
	if manager == nil {
		return Prepared{}, errors.New("workspace manager is nil")
	}
	if ctx == nil {
		return Prepared{}, errors.New("Linux clone prepare context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Prepared{}, err
	}
	if err := grant.Validate(); err != nil {
		return Prepared{}, err
	}
	binding, repository, err := manager.linuxCloneBinding(grant.Intent)
	if err != nil {
		return Prepared{}, err
	}
	if err := verifyLinuxCloneGrant(ctx, repository, binding, grant); err != nil {
		return Prepared{}, err
	}
	fingerprint, err := linuxCloneFingerprint(grant)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{
		Path:                  grant.Intent.Target,
		BindingKey:            grant.Intent.BindingKey,
		Run:                   RunRef{RunID: grant.Intent.RunID, Generation: grant.Intent.Generation},
		linuxClone:            true,
		linuxCloneFingerprint: fingerprint,
		linuxCloneTarget:      grant.Intent.Target,
		linuxCloneBindingKey:  grant.Intent.BindingKey,
		linuxCloneRun:         RunRef{RunID: grant.Intent.RunID, Generation: grant.Intent.Generation},
		policy:                binding.Cleanup,
		repository:            repository,
		root:                  binding.Root,
	}, nil
}

// ValidateLinuxCloneCheckout verifies the gate-produced checkout receipt
// against the reserved grant and a no-follow stat of the exact target inode.
// It never consults B's .git metadata or runs Git against the target.
func (manager *Manager) ValidateLinuxCloneCheckout(grant authority.LinuxCloneGrant, receipt authority.LinuxCloneCheckoutReceipt) error {
	if manager == nil {
		return errors.New("workspace manager is nil")
	}
	if err := grant.Validate(); err != nil {
		return err
	}
	if err := validateLinuxCloneCheckoutReceipt(grant, receipt); err != nil {
		return err
	}
	binding, _, err := manager.linuxCloneBinding(grant.Intent)
	if err != nil {
		return err
	}
	target, err := openLinuxCloneTarget(grant.Intent, receipt, binding.Root)
	if err != nil {
		return fmt.Errorf("verify B-owned Linux clone target: %w", err)
	}
	return target.close()
}

// PromoteLinuxClone imports a proposed commit only after an issued normal v2
// release proves stopped-and-removed containment. The App must first bind that
// release to this RunKey/fence with Store readback; the release itself carries
// no RunKey. Candidate bytes are read only through the checkout-receipt inode.
func (manager *Manager) PromoteLinuxClone(ctx context.Context, grant authority.LinuxCloneGrant, checkout authority.LinuxCloneCheckoutReceipt, proposedCommit string, release platform.LinuxCgroupV2Release) (result authority.LinuxClonePromotionReceipt, resultErr error) {
	if manager == nil {
		return authority.LinuxClonePromotionReceipt{}, errors.New("workspace manager is nil")
	}
	if ctx == nil {
		return authority.LinuxClonePromotionReceipt{}, errors.New("Linux clone promotion context is nil")
	}
	if err := ctx.Err(); err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if err := grant.Validate(); err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if err := validateLinuxCloneCheckoutReceipt(grant, checkout); err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if err := validateLinuxCloneRelease(release); err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if !validLinuxCloneObjectID(proposedCommit) || len(proposedCommit) != len(grant.Intent.BaseCommit) {
		return authority.LinuxClonePromotionReceipt{}, errors.New("proposed Linux clone commit is invalid")
	}
	binding, repository, err := manager.linuxCloneBinding(grant.Intent)
	if err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if err := verifyLinuxCloneGrant(ctx, repository, binding, grant); err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	target, err := openLinuxCloneTarget(grant.Intent, checkout, binding.Root)
	if err != nil {
		return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("open stopped Linux clone target: %w", err)
	}
	defer target.close()

	quarantine, err := createLinuxCloneQuarantine(ctx, target, filepath.Dir(grant.Intent.SeedPath), len(grant.Intent.BaseCommit))
	if err != nil {
		return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("copy stopped Linux clone objects to A-only quarantine: %w", err)
	}
	removeQuarantine := true
	defer func() {
		if !removeQuarantine {
			return
		}
		if err := os.RemoveAll(quarantine); err != nil {
			result = authority.LinuxClonePromotionReceipt{}
			resultErr = errors.Join(resultErr, fmt.Errorf("remove A-only Linux clone quarantine: %w", err))
		}
	}()
	if err := verifyLinuxCloneTargetStillBound(target); err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if err := verifyLinuxCloneCandidate(ctx, quarantine, grant.Intent.BaseCommit, proposedCommit); err != nil {
		if errors.Is(err, ErrLinuxCloneProcessUnresolved) {
			removeQuarantine = false
		}
		return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("validate proposed Linux clone commit: %w", err)
	}
	treeDigest, err := linuxCloneTreeDigest(ctx, quarantine, proposedCommit)
	if err != nil {
		if errors.Is(err, ErrLinuxCloneProcessUnresolved) {
			removeQuarantine = false
		}
		return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("digest proposed Linux clone tree: %w", err)
	}
	if err := verifyLinuxCloneTargetStillBound(target); err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if err := importLinuxCloneCandidate(ctx, repository, quarantine, proposedCommit); err != nil {
		if errors.Is(err, ErrLinuxCloneProcessUnresolved) {
			removeQuarantine = false
		}
		return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("import validated Linux clone objects into A source: %w", err)
	}
	receipt, err := promoteLinuxCloneRef(ctx, repository, grant.Intent, proposedCommit, treeDigest)
	if err != nil {
		if errors.Is(err, ErrLinuxCloneProcessUnresolved) {
			removeQuarantine = false
		}
		return authority.LinuxClonePromotionReceipt{}, err
	}
	return receipt, nil
}

// CleanupLinuxClone removes only a promoted B-owned clone whose exact State
// readback proves terminal cleanup eligibility. The caller separately checks
// retained Goal-session ownership before passing that readback.
func (manager *Manager) CleanupLinuxClone(ctx context.Context, grant authority.LinuxCloneGrant, checkout authority.LinuxCloneCheckoutReceipt, journal state.RunJournal) error {
	if manager == nil {
		return errors.New("workspace manager is nil")
	}
	if ctx == nil {
		return errors.New("Linux clone cleanup context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := grant.Validate(); err != nil {
		return err
	}
	if err := validateLinuxCloneCheckoutReceipt(grant, checkout); err != nil {
		return err
	}
	if err := validateLinuxCloneCleanupJournal(grant, checkout, journal); err != nil {
		return err
	}
	binding, repository, err := manager.linuxCloneBinding(grant.Intent)
	if err != nil {
		return err
	}
	if err := verifyLinuxCloneGrant(ctx, repository, binding, grant); err != nil {
		return err
	}
	promotion := *journal.LinuxClonePromotionReceipt
	if err := verifyLinuxClonePromotion(ctx, repository, grant.Intent, promotion); err != nil {
		return err
	}
	target, err := openLinuxCloneTarget(grant.Intent, checkout, binding.Root)
	if errors.Is(err, os.ErrNotExist) {
		if err := verifyLinuxCloneTargetAbsentUnderRoot(ctx, binding.Root, grant.Intent.Target); err != nil {
			return err
		}
		rootFD, _, err := openLinuxCloneDirPath(binding.Root)
		if err != nil {
			return fmt.Errorf("reopen B clone root for absent cleanup readback: %w", err)
		}
		if err := unix.Fsync(rootFD); err != nil {
			_ = unix.Close(rootFD)
			return fmt.Errorf("sync already absent Linux clone root: %w", err)
		}
		if err := unix.Close(rootFD); err != nil {
			return err
		}
		return verifyLinuxCloneTargetAbsentUnderRoot(ctx, binding.Root, grant.Intent.Target)
	}
	if err != nil {
		return fmt.Errorf("open authorized Linux clone target for cleanup: %w", err)
	}
	defer target.close()
	if err := removeLinuxCloneTarget(ctx, target); err != nil {
		return fmt.Errorf("remove authorized Linux clone target: %w", err)
	}
	return nil
}

// AbortLinuxCloneBeforeCheckout returns a no-target cleanup proof only after
// the caller has durably fenced every possible clone producer in State. The
// optional seed and at most one cgroup proof must be exact Store readbacks.
// A normal release proves stopped containment, not that B never ran; this
// method never writes State.
func (manager *Manager) AbortLinuxCloneBeforeCheckout(ctx context.Context, intent authority.LinuxCloneIntent, seedReceipt *authority.LinuxCloneSeedReceipt, preGateRelease *authority.LinuxCgroupPreGateReleaseProof, cgroupAbortProof *authority.LinuxCgroupReservationAbortProof, normalRelease *authority.SupervisorHandoffReleaseProof) (authority.LinuxCloneAbortProof, error) {
	if manager == nil {
		return authority.LinuxCloneAbortProof{}, errors.New("workspace manager is nil")
	}
	if ctx == nil {
		return authority.LinuxCloneAbortProof{}, errors.New("Linux clone abort context is nil")
	}
	if err := ctx.Err(); err != nil {
		return authority.LinuxCloneAbortProof{}, err
	}
	binding, repository, err := manager.linuxCloneBinding(intent)
	if err != nil {
		return authority.LinuxCloneAbortProof{}, err
	}
	if seedReceipt != nil && (seedReceipt.Validate() != nil || seedReceipt.BaseCommit != intent.BaseCommit) {
		return authority.LinuxCloneAbortProof{}, errors.New("Store-bound Linux clone seed receipt differs from the abort intent")
	}
	if preGateRelease != nil && preGateRelease.Validate() != nil {
		return authority.LinuxCloneAbortProof{}, errors.New("Store-bound pre-gate release proof is invalid")
	}
	if cgroupAbortProof != nil && cgroupAbortProof.Validate() != nil {
		return authority.LinuxCloneAbortProof{}, errors.New("Store-bound cgroup abort proof is invalid")
	}
	if normalRelease != nil && (normalRelease.Validate() != nil || normalRelease.Version != authority.SupervisorHandoffVersionV2 ||
		normalRelease.Status != "released" || normalRelease.StopReceipt == nil ||
		(normalRelease.LeafDisposition != authority.LinuxCgroupLeafRemoved && normalRelease.LeafDisposition != authority.LinuxCgroupLeafAbsentAfterReceipt)) {
		return authority.LinuxCloneAbortProof{}, errors.New("Store-bound normal Linux v2 release proof is invalid")
	}
	proofInputs := 0
	for _, present := range []bool{preGateRelease != nil, cgroupAbortProof != nil, normalRelease != nil} {
		if present {
			proofInputs++
		}
	}
	if proofInputs > 1 {
		return authority.LinuxCloneAbortProof{}, errors.New("Linux clone abort containment proofs are mutually exclusive")
	}
	if err := verifyLinuxCloneSource(ctx, repository); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("validate A-owned Git source for abort: %w", err)
	}
	if err := verifyLinuxCloneTargetAbsentUnderRoot(ctx, binding.Root, intent.Target); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("verify B target absent before clone abort: %w", err)
	}
	baseRef, err := authority.LinuxCloneBaseRef(intent.RunID, intent.Generation)
	if err != nil {
		return authority.LinuxCloneAbortProof{}, err
	}
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, baseRef); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("A-owned base ref lock is unresolved before abort: %w", err)
	}
	promotionRef, err := authority.LinuxClonePromotionRef(intent.RunID, intent.Generation)
	if err != nil {
		return authority.LinuxCloneAbortProof{}, err
	}
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, promotionRef); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("A-owned promotion ref lock is unresolved before abort: %w", err)
	}
	if err := requireLinuxCloneRefAbsent(ctx, repository, promotionRef); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("refuse precheckout abort with a promotion ref: %w", err)
	}
	if value, err := linuxCloneRefValue(ctx, repository, baseRef); err == nil {
		if value != intent.BaseCommit {
			return authority.LinuxCloneAbortProof{}, errors.New("refuse to remove a base ref that differs from the abort intent")
		}
		if err := verifyLinuxCloneRef(ctx, repository, baseRef, intent.BaseCommit); err != nil {
			return authority.LinuxCloneAbortProof{}, fmt.Errorf("verify A-owned base ref before abort: %w", err)
		}
	} else if !errors.Is(err, errLinuxCloneRefMissing) {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("read A-owned base ref before abort: %w", err)
	}
	if err := removeLinuxCloneSeedForAbort(ctx, repository, binding.SeedRoot, intent, baseRef, seedReceipt); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("remove exact A-owned Linux clone seed: %w", err)
	}
	if err := removeLinuxCloneBaseRefForAbort(ctx, repository, baseRef, intent.BaseCommit); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("remove exact A-owned Linux clone base ref: %w", err)
	}
	if err := requireLinuxCloneRefAbsent(ctx, repository, baseRef); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("verify A-owned base ref absent after abort: %w", err)
	}
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, baseRef); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("verify A-owned base ref lock absent after abort: %w", err)
	}
	if err := requireLinuxCloneRefAbsent(ctx, repository, promotionRef); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("verify A-owned promotion ref absent after abort: %w", err)
	}
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, promotionRef); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("verify A-owned promotion ref lock absent after abort: %w", err)
	}
	if err := verifyLinuxCloneTargetAbsentUnderRoot(ctx, binding.Root, intent.Target); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("verify B target absent after clone abort: %w", err)
	}
	proof := authority.LinuxCloneAbortProof{
		Version: authority.LinuxCloneVersion, Intent: intent.Clone(),
		SeedReceipt: cloneLinuxCloneSeedReceipt(seedReceipt),
		BaseRef:     baseRef, PromotionRef: promotionRef,
		TargetAbsent: true, SeedAbsent: true, BaseRefAbsent: true, PromotionRefAbsent: true,
	}
	if preGateRelease != nil {
		release := preGateRelease.Clone()
		proof.PreGateReleaseProof = &release
	}
	if cgroupAbortProof != nil {
		abortProof := cgroupAbortProof.Clone()
		proof.CgroupAbortProof = &abortProof
	}
	if normalRelease != nil {
		release := normalRelease.Clone()
		proof.NormalReleaseProof = &release
	}
	if err := proof.Validate(); err != nil {
		return authority.LinuxCloneAbortProof{}, fmt.Errorf("validate Linux clone no-target proof: %w", err)
	}
	return proof, nil
}

func cloneLinuxCloneSeedReceipt(receipt *authority.LinuxCloneSeedReceipt) *authority.LinuxCloneSeedReceipt {
	if receipt == nil {
		return nil
	}
	cloned := receipt.Clone()
	return &cloned
}

func requireLinuxCloneRefAbsent(ctx context.Context, repository, ref string) error {
	_, err := linuxCloneRefValue(ctx, repository, ref)
	if errors.Is(err, errLinuxCloneRefMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("Linux clone ref is still present")
}

func removeLinuxCloneBaseRefForAbort(ctx context.Context, repository, ref, commit string) error {
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, ref); err != nil {
		return err
	}
	value, err := linuxCloneRefValue(ctx, repository, ref)
	if errors.Is(err, errLinuxCloneRefMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	if value != commit {
		return errors.New("refuse to delete a base ref with a different object ID")
	}
	if err := verifyLinuxCloneRef(ctx, repository, ref, commit); err != nil {
		return err
	}
	_, deleteErr := linuxCloneGitOutput(ctx, repository, 4096, errors.New("Git ref deletion output exceeds limit"), "update-ref", "--no-deref", "-d", ref, commit)
	if errors.Is(deleteErr, ErrLinuxCloneProcessUnresolved) {
		return deleteErr
	}
	if err := requireLinuxCloneRefAbsent(ctx, repository, ref); err == nil {
		return requireLinuxCloneRefLockAbsent(ctx, repository, ref)
	} else if deleteErr != nil {
		return fmt.Errorf("delete exact A-owned base ref: %w; readback: %v", deleteErr, err)
	}
	return fmt.Errorf("A-owned base ref remains after delete readback: %w", err)
}

func removeLinuxCloneSeedForAbort(ctx context.Context, repository, seedRoot string, intent authority.LinuxCloneIntent, baseRef string, expected *authority.LinuxCloneSeedReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if expected != nil && (expected.Validate() != nil || expected.BaseCommit != intent.BaseCommit) {
		return errors.New("Store-bound Linux clone seed receipt is invalid")
	}
	rootFD, _, err := openLinuxCloneOwnedDirPath(seedRoot)
	if err != nil {
		return fmt.Errorf("open A-owned seed root for abort: %w", err)
	}
	_ = unix.Close(rootFD)
	parentFD, name, err := openLinuxCloneSeedParent(seedRoot, intent.SeedPath, false)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	entries, err := readLinuxCloneDirNames(ctx, parentFD)
	if err != nil {
		return fmt.Errorf("inspect A-owned seed directory for in-flight build: %w", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry, ".clone-seed-") || strings.HasPrefix(entry, ".symmetry-clone-quarantine-") ||
			strings.HasPrefix(entry, ".symmetry-clone-template-") {
			return errors.New("A-side Linux clone temporary remains; no-target abort is unresolved")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var namedBefore unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &namedBefore, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return verifyLinuxCloneSeedAbsent(parentFD, name)
		}
		return err
	}
	receipt, err := readLinuxCloneSeed(ctx, repository, parentFD, name, intent, baseRef, expectedLinuxCloneSeedDigest(expected))
	if err != nil {
		return fmt.Errorf("verify A-owned seed before abort removal: %w", err)
	}
	if uint64(namedBefore.Dev) != receipt.Device || uint64(namedBefore.Ino) != receipt.Inode || namedBefore.Size != receipt.Size {
		return errors.New("A-owned seed name changed before abort removal")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, name, 0); err != nil {
		if absentErr := verifyLinuxCloneSeedAbsent(parentFD, name); absentErr != nil {
			return fmt.Errorf("unlink A-owned seed: %w; readback: %v", err, absentErr)
		}
	}
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync A-owned seed removal: %w", err)
	}
	return verifyLinuxCloneSeedAbsent(parentFD, name)
}

func expectedLinuxCloneSeedDigest(receipt *authority.LinuxCloneSeedReceipt) *linuxCloneExpectedDigest {
	if receipt == nil {
		return nil
	}
	return &linuxCloneExpectedDigest{
		digest: receipt.Digest, size: receipt.Size, device: receipt.Device, inode: receipt.Inode, checkIdentity: true,
	}
}

func verifyLinuxCloneSeedAbsent(parentFD int, name string) error {
	var info unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		var readback unix.Stat_t
		if readbackErr := unix.Fstatat(parentFD, name, &readback, unix.AT_SYMLINK_NOFOLLOW); errors.Is(readbackErr, unix.ENOENT) {
			return nil
		} else if readbackErr != nil {
			return readbackErr
		}
		return errors.New("Linux clone seed reappeared during absence readback")
	} else if err != nil {
		return err
	}
	return errors.New("Linux clone seed remains after abort removal")
}

func (manager *Manager) linuxCloneBinding(intent authority.LinuxCloneIntent) (config.Workspace, string, error) {
	if err := intent.Validate(); err != nil {
		return config.Workspace{}, "", err
	}
	binding, ok := manager.bindings[intent.BindingKey]
	if !ok || binding.Policy != config.WorkspacePolicyGitCloneV2 {
		return config.Workspace{}, "", errors.New("Linux clone intent does not match a configured git_clone_v2 binding")
	}
	if !canonicalLinuxClonePath(binding.Repository) || !canonicalLinuxClonePath(binding.Root) || !canonicalLinuxClonePath(binding.SeedRoot) {
		return config.Workspace{}, "", errors.New("Linux clone binding paths are not canonical absolute paths")
	}
	if intent.Target != linuxCloneTargetPath(binding.Root, intent.BindingKey, RunRef{RunID: intent.RunID, Generation: intent.Generation}) {
		return config.Workspace{}, "", errors.New("Linux clone target differs from the configured per-run target")
	}
	expectedSeed := filepath.Join(binding.SeedRoot, "binding-"+intent.BindingKey, "run-"+intent.RunID, fmt.Sprintf("generation-%d.bundle", intent.Generation))
	if intent.SeedPath != expectedSeed {
		return config.Workspace{}, "", errors.New("Linux clone seed path differs from the configured per-run seed")
	}
	if !within(binding.SeedRoot, intent.SeedPath) || intent.SeedPath == binding.SeedRoot {
		return config.Workspace{}, "", errors.New("Linux clone seed path escapes the configured A-owned seed root")
	}
	if linuxClonePathsOverlap(binding.Repository, binding.Root) || linuxClonePathsOverlap(binding.Repository, binding.SeedRoot) ||
		linuxClonePathsOverlap(binding.Root, binding.SeedRoot) {
		return config.Workspace{}, "", errors.New("Linux clone repository, target root, and seed root overlap")
	}
	return binding, binding.Repository, nil
}

func linuxCloneTargetPath(root, bindingKey string, run RunRef) string {
	return filepath.Join(root, "binding-"+bindingKey, "run-"+run.RunID, fmt.Sprintf("generation-%d", run.Generation))
}

func canonicalLinuxClonePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

func linuxClonePathsOverlap(left, right string) bool {
	return left == right || within(left, right) || within(right, left)
}

func linuxCloneFingerprint(grant authority.LinuxCloneGrant) (string, error) {
	identity := struct {
		Version int                             `json:"version"`
		Intent  authority.LinuxCloneIntent      `json:"intent"`
		Seed    authority.LinuxCloneSeedReceipt `json:"seed"`
	}{Version: authority.LinuxCloneVersion, Intent: grant.Intent, Seed: grant.Seed}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("encode Linux clone fingerprint identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func verifyLinuxCloneGrant(ctx context.Context, repository string, binding config.Workspace, grant authority.LinuxCloneGrant) error {
	if err := grant.Validate(); err != nil {
		return err
	}
	if err := verifyLinuxCloneSource(ctx, repository); err != nil {
		return fmt.Errorf("validate A-owned Git source: %w", err)
	}
	if err := verifyLinuxCloneBase(ctx, repository, grant.Intent); err != nil {
		return err
	}
	baseRef, err := authority.LinuxCloneBaseRef(grant.Intent.RunID, grant.Intent.Generation)
	if err != nil {
		return err
	}
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, baseRef); err != nil {
		return fmt.Errorf("A-owned base ref lock is unresolved: %w", err)
	}
	if err := verifyLinuxCloneRef(ctx, repository, baseRef, grant.Intent.BaseCommit); err != nil {
		return fmt.Errorf("verify pinned Linux clone base ref: %w", err)
	}
	parentFD, name, err := openLinuxCloneSeedParent(binding.SeedRoot, grant.Intent.SeedPath, false)
	if err != nil {
		return fmt.Errorf("open A-owned Linux clone seed parent: %w", err)
	}
	defer unix.Close(parentFD)
	if _, err := readLinuxCloneSeed(ctx, repository, parentFD, name, grant.Intent, baseRef, expectedLinuxCloneSeedDigest(&grant.Seed)); err != nil {
		return fmt.Errorf("verify journal-bound Linux clone seed: %w", err)
	}
	return nil
}

func verifyLinuxCloneSource(ctx context.Context, repository string) error {
	fd, info, err := openLinuxCloneOwnedDirPath(repository)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if uint32(info.Uid) != uint32(os.Geteuid()) || info.Mode&0o022 != 0 {
		return errors.New("configured Git source must be A-owned and not group/world writable")
	}
	common, err := linuxCloneCommonDirectory(ctx, repository)
	if err != nil {
		return err
	}
	gitFD, gitInfo, err := openLinuxCloneOwnedDirPath(common)
	if err != nil {
		return fmt.Errorf("open A-owned Git common directory: %w", err)
	}
	defer unix.Close(gitFD)
	if uint32(gitInfo.Uid) != uint32(os.Geteuid()) || gitInfo.Mode&0o022 != 0 {
		return errors.New("Git common directory must be A-owned and not group/world writable")
	}
	if err := verifyLinuxCloneOwnedGitEntry(gitFD, "config", unix.S_IFREG); err != nil {
		return fmt.Errorf("validate A-owned Git config: %w", err)
	}
	if err := verifyLinuxCloneOwnedGitEntry(gitFD, "HEAD", unix.S_IFREG); err != nil {
		return fmt.Errorf("validate A-owned Git HEAD: %w", err)
	}
	if err := verifyLinuxCloneOptionalOwnedGitEntry(gitFD, "hooks", unix.S_IFDIR); err != nil {
		return fmt.Errorf("validate A-owned Git hooks: %w", err)
	}
	if err := verifyLinuxCloneOptionalOwnedGitEntry(gitFD, "packed-refs", unix.S_IFREG); err != nil {
		return fmt.Errorf("validate A-owned packed refs: %w", err)
	}
	if err := rejectLinuxCloneEntry(gitFD, "shallow"); err != nil {
		return fmt.Errorf("A-owned Git source must contain complete history: %w", err)
	}
	objectsFD, err := openLinuxCloneDirAt(gitFD, "objects")
	if err != nil {
		return fmt.Errorf("open A-owned Git object directory: %w", err)
	}
	var objectsInfo unix.Stat_t
	if err := unix.Fstat(objectsFD, &objectsInfo); err != nil {
		_ = unix.Close(objectsFD)
		return err
	}
	if uint32(objectsInfo.Uid) != uint32(os.Geteuid()) || objectsInfo.Mode&0o022 != 0 {
		_ = unix.Close(objectsFD)
		return errors.New("Git object directory must be A-owned and not group/world writable")
	}
	if infoFD, err := openLinuxCloneDirAt(objectsFD, "info"); err == nil {
		if err := rejectLinuxCloneEntry(infoFD, "alternates"); err != nil {
			_ = unix.Close(infoFD)
			_ = unix.Close(objectsFD)
			return fmt.Errorf("A-owned Git source cannot use object alternates: %w", err)
		}
		if err := rejectLinuxCloneEntry(infoFD, "http-alternates"); err != nil {
			_ = unix.Close(infoFD)
			_ = unix.Close(objectsFD)
			return fmt.Errorf("A-owned Git source cannot use HTTP object alternates: %w", err)
		}
		_ = unix.Close(infoFD)
	} else if !errors.Is(err, unix.ENOENT) {
		_ = unix.Close(objectsFD)
		return fmt.Errorf("inspect A-owned Git object info directory: %w", err)
	}
	_ = unix.Close(objectsFD)
	return nil
}

func linuxCloneCommonDirectory(ctx context.Context, repository string) (string, error) {
	commonOutput, err := linuxCloneGitOutput(ctx, repository, 4096, errors.New("Git directory output exceeds limit"), "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("resolve A-owned Git common directory: %w", err)
	}
	common := strings.TrimSpace(string(commonOutput))
	if common == "" || strings.ContainsAny(common, "\r\n\x00") {
		return "", errors.New("Git returned an invalid common directory")
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(repository, common)
	}
	return filepath.Clean(common), nil
}

func requireLinuxCloneRefLockAbsent(ctx context.Context, repository, ref string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	common, err := linuxCloneCommonDirectory(ctx, repository)
	if err != nil {
		return err
	}
	commonFD, _, err := openLinuxCloneOwnedDirPath(common)
	if err != nil {
		return err
	}
	defer unix.Close(commonFD)
	var reftable unix.Stat_t
	if err := unix.Fstatat(commonFD, "reftable", &reftable, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return errors.New("Linux clone A source uses unsupported reftable ref storage")
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	if err := linuxCloneLockEntryAbsent(commonFD, "packed-refs.lock"); err != nil {
		return err
	}
	if !strings.HasPrefix(ref, "refs/") {
		return errors.New("Linux clone ref is not a full refs/ name")
	}
	components := strings.Split(strings.TrimPrefix(ref, "refs/"), "/")
	if len(components) < 2 {
		return errors.New("Linux clone ref path is incomplete")
	}
	opened := []int{commonFD}
	defer func() {
		for _, fd := range opened[1:] {
			_ = unix.Close(fd)
		}
	}()
	parentFD := commonFD
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." || component == ".." {
			return errors.New("Linux clone ref path contains an invalid component")
		}
		next, openErr := openLinuxCloneDirAt(parentFD, component)
		if errors.Is(openErr, unix.ENOENT) {
			return nil
		}
		if openErr != nil {
			return fmt.Errorf("open A-owned Git ref directory: %w", openErr)
		}
		var info unix.Stat_t
		if err := unix.Fstat(next, &info); err != nil {
			_ = unix.Close(next)
			return err
		}
		if uint32(info.Uid) != uint32(os.Geteuid()) || info.Mode&0o022 != 0 {
			_ = unix.Close(next)
			return errors.New("Git ref directory must be A-owned and not group/world writable")
		}
		opened = append(opened, next)
		parentFD = next
	}
	return linuxCloneLockEntryAbsent(parentFD, components[len(components)-1]+".lock")
}

func linuxCloneLockEntryAbsent(directoryFD int, name string) error {
	var info unix.Stat_t
	err := unix.Fstatat(directoryFD, name, &info, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("Git lock entry %q remains unresolved", name)
}

func verifyLinuxCloneOwnedGitEntry(directoryFD int, name string, kind uint32) error {
	var info unix.Stat_t
	if err := unix.Fstatat(directoryFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if info.Mode&unix.S_IFMT != kind || uint32(info.Uid) != uint32(os.Geteuid()) || info.Mode&0o022 != 0 {
		return errors.New("Git metadata entry is not an A-owned supported file or directory")
	}
	return nil
}

func verifyLinuxCloneOptionalOwnedGitEntry(directoryFD int, name string, kind uint32) error {
	var info unix.Stat_t
	if err := unix.Fstatat(directoryFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	if info.Mode&unix.S_IFMT != kind || uint32(info.Uid) != uint32(os.Geteuid()) || info.Mode&0o022 != 0 {
		return errors.New("Git metadata entry is not an A-owned supported directory")
	}
	return nil
}

func verifyLinuxCloneBase(ctx context.Context, repository string, intent authority.LinuxCloneIntent) error {
	if err := verifyLinuxCloneObjectFormat(ctx, repository, len(intent.BaseCommit)); err != nil {
		return err
	}
	resolved, err := linuxCloneGitOutput(ctx, repository, 4096, errors.New("Git object ID output exceeds limit"), "rev-parse", "--verify", intent.BaseCommit+"^{commit}")
	if err != nil {
		return fmt.Errorf("verify admitted Linux clone base commit: %w", err)
	}
	if strings.TrimSpace(string(resolved)) != intent.BaseCommit {
		return errors.New("A-owned source resolved the admitted base commit to a different object")
	}
	digest, err := linuxCloneTreeDigest(ctx, repository, intent.BaseCommit)
	if err != nil {
		return fmt.Errorf("digest admitted Linux clone base tree: %w", err)
	}
	if digest != intent.BaseTreeDigest {
		return fmt.Errorf("admitted base tree digest mismatch: expected %s, got %s", intent.BaseTreeDigest, digest)
	}
	return nil
}

func verifyLinuxCloneObjectFormat(ctx context.Context, repository string, objectIDLength int) error {
	output, err := linuxCloneGitOutput(ctx, repository, 128, errors.New("Git object format output exceeds limit"), "rev-parse", "--show-object-format")
	if err != nil {
		return fmt.Errorf("read A-owned Git object format: %w", err)
	}
	want := "sha1"
	if objectIDLength == 64 {
		want = "sha256"
	}
	if strings.TrimSpace(string(output)) != want {
		return fmt.Errorf("A-owned Git object format does not match %s commit IDs", want)
	}
	return nil
}

func ensureLinuxCloneBaseRef(ctx context.Context, repository, ref, commit string) error {
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, ref); err != nil {
		return err
	}
	if err := verifyLinuxCloneRef(ctx, repository, ref, commit); err == nil {
		return nil
	} else if !errors.Is(err, errLinuxCloneRefMissing) {
		return err
	}
	zero := strings.Repeat("0", len(commit))
	_, updateErr := linuxCloneGitOutput(ctx, repository, 4096, errors.New("Git ref output exceeds limit"), "update-ref", "--no-deref", ref, commit, zero)
	if updateErr == nil {
		if err := requireLinuxCloneRefLockAbsent(ctx, repository, ref); err != nil {
			return err
		}
		return verifyLinuxCloneRef(ctx, repository, ref, commit)
	}
	if errors.Is(updateErr, ErrLinuxCloneProcessUnresolved) {
		return updateErr
	}
	// A lost update-ref response is resolved only by an exact direct-ref readback.
	if err := verifyLinuxCloneRef(ctx, repository, ref, commit); err == nil {
		return requireLinuxCloneRefLockAbsent(ctx, repository, ref)
	}
	return fmt.Errorf("compare-create A-owned base ref: %w", updateErr)
}

var errLinuxCloneRefMissing = errors.New("Linux clone ref is missing")

func linuxCloneRefValue(ctx context.Context, repository, ref string) (string, error) {
	if _, err := linuxCloneGitOutput(ctx, repository, 4096, errors.New("Git symbolic-ref output exceeds limit"), "symbolic-ref", "--quiet", ref); err == nil {
		return "", errors.New("Linux clone ref must be a direct ref, not a symbolic ref")
	} else if exitCode, ok := linuxCloneExitCode(err); !ok || exitCode != 1 {
		return "", fmt.Errorf("inspect Linux clone ref type: %w", err)
	}
	output, err := linuxCloneGitOutput(ctx, repository, 4096, errors.New("Git ref output exceeds limit"), "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		if exitCode, ok := linuxCloneExitCode(err); ok && exitCode == 1 {
			return "", errLinuxCloneRefMissing
		}
		return "", err
	}
	value := strings.TrimSpace(string(output))
	if !validLinuxCloneObjectID(value) {
		return "", errors.New("Linux clone ref returned an invalid object ID")
	}
	return value, nil
}

func verifyLinuxCloneRef(ctx context.Context, repository, ref, commit string) error {
	value, err := linuxCloneRefValue(ctx, repository, ref)
	if err != nil {
		return err
	}
	if value != commit {
		return errors.New("Linux clone ref does not point to its exact expected commit")
	}
	typeOutput, err := linuxCloneGitOutput(ctx, repository, 128, errors.New("Git object type output exceeds limit"), "cat-file", "-t", commit)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(typeOutput)) != "commit" {
		return errors.New("Linux clone ref target is not a commit object")
	}
	return nil
}

func verifyLinuxCloneBundle(ctx context.Context, repository, bundlePath string, intent authority.LinuxCloneIntent, ref string) error {
	file, err := openLinuxCloneBundle(bundlePath)
	if err != nil {
		return err
	}
	if err := verifyLinuxCloneBundleHeader(file, intent.BaseCommit, ref); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := linuxCloneGitOutput(ctx, repository, 64<<10, errors.New("Git bundle verification output exceeds limit"), "bundle", "verify", bundlePath); err != nil {
		return fmt.Errorf("git bundle verify: %w", err)
	}
	listed, err := linuxCloneGitOutput(ctx, repository, 4096, errors.New("Git bundle ref output exceeds limit"), "bundle", "list-heads", bundlePath)
	if err != nil {
		return fmt.Errorf("git bundle list-heads: %w", err)
	}
	if strings.TrimSpace(string(listed)) != intent.BaseCommit+" "+ref {
		return errors.New("Linux clone bundle does not advertise exactly the admitted named ref and OID")
	}
	return nil
}

func verifyLinuxCloneBundleHeader(file *os.File, commit, ref string) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(io.LimitReader(file, maxLinuxCloneBundleHeaderSize))
	magic, err := reader.ReadString('\n')
	if err != nil || (magic != "# v2 git bundle\n" && magic != "# v3 git bundle\n") {
		return errors.New("Linux clone bundle has an unsupported header")
	}
	var found bool
	var objectFormatSeen bool
	for consumed := len(magic); consumed < maxLinuxCloneBundleHeaderSize; {
		line, err := reader.ReadString('\n')
		consumed += len(line)
		if err != nil {
			return errors.New("Linux clone bundle header is truncated")
		}
		if line == "\n" {
			if !found {
				return errors.New("Linux clone bundle has no named ref")
			}
			if len(commit) == 64 && !objectFormatSeen {
				return errors.New("SHA-256 Linux clone bundle is missing its object-format header")
			}
			return nil
		}
		if strings.HasPrefix(line, "-") {
			return errors.New("Linux clone bundle must contain complete history without prerequisites")
		}
		if strings.HasPrefix(line, "@") {
			if objectFormatSeen {
				return errors.New("Linux clone bundle has duplicate object-format headers")
			}
			want := "@object-format=sha1\n"
			if len(commit) == 64 {
				want = "@object-format=sha256\n"
			}
			if line != want {
				return errors.New("Linux clone bundle object format does not match the admitted commit")
			}
			objectFormatSeen = true
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != commit || fields[1] != ref || found {
			return errors.New("Linux clone bundle must contain exactly the admitted named ref and OID")
		}
		found = true
	}
	return errors.New("Linux clone bundle header exceeds size limit")
}

func validLinuxCloneObjectID(value string) bool {
	if (len(value) != 40 && len(value) != 64) || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded)*2 == len(value)
}

func validateLinuxCloneCheckoutReceipt(grant authority.LinuxCloneGrant, receipt authority.LinuxCloneCheckoutReceipt) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	intent, seed := grant.Intent, grant.Seed
	if receipt.Version != intent.Version || receipt.RunID != intent.RunID || receipt.Generation != intent.Generation ||
		receipt.BindingKey != intent.BindingKey || receipt.ResourceID != intent.ResourceID || receipt.Target != intent.Target || receipt.Nonce != intent.Nonce ||
		receipt.SeedDigest != seed.Digest || receipt.BaseCommit != intent.BaseCommit {
		return errors.New("Linux clone checkout receipt does not match the reserved grant")
	}
	return nil
}

func validateLinuxCloneRelease(release platform.LinuxCgroupV2Release) error {
	proof, issued := release.Snapshot()
	if !issued || proof.Validate() != nil || proof.Version != authority.SupervisorHandoffVersionV2 ||
		proof.Status != "released" || proof.OwnerKind != authority.OwnerKindLinuxHelper || proof.StopReceipt == nil ||
		proof.CgroupIdentity == nil || (proof.LeafDisposition != authority.LinuxCgroupLeafRemoved && proof.LeafDisposition != authority.LinuxCgroupLeafAbsentAfterReceipt) {
		return errors.New("Linux clone promotion requires an issued normal v2 stop-and-release proof")
	}
	return nil
}

func validateLinuxCloneCleanupJournal(grant authority.LinuxCloneGrant, checkout authority.LinuxCloneCheckoutReceipt, journal state.RunJournal) error {
	key := state.RunKey{RunID: grant.Intent.RunID, Generation: grant.Intent.Generation}
	fence := journal.Fence()
	if fence.Generation != key.Generation || fence.RuntimeID == "" || fence.RuntimeEpoch <= 0 || fence.ClaimID == "" || fence.LeaseToken == "" {
		return errors.New("RunJournal clone cleanup fence is incomplete")
	}
	if !journal.LinuxCloneCleanupEligible(key, fence) {
		return errors.New("RunJournal is not durably eligible for Linux clone cleanup")
	}
	if journal.WorkspacePath != grant.Intent.Target || journal.WorkspaceBindingKey != grant.Intent.BindingKey ||
		journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(grant.Intent) ||
		journal.LinuxCloneSeedReceipt == nil || !journal.LinuxCloneSeedReceipt.Equal(grant.Seed) ||
		journal.LinuxCloneCheckoutReceipt == nil || !journal.LinuxCloneCheckoutReceipt.Equal(checkout) ||
		journal.LinuxClonePromotionReceipt == nil || journal.ContainmentCgroupPreGateReleaseProof != nil {
		return errors.New("RunJournal clone authority does not match cleanup grant and checkout")
	}
	if err := journal.LinuxClonePromotionReceipt.Validate(); err != nil {
		return fmt.Errorf("validate durable Linux clone promotion receipt: %w", err)
	}
	return nil
}

func verifyLinuxClonePromotion(ctx context.Context, repository string, intent authority.LinuxCloneIntent, receipt authority.LinuxClonePromotionReceipt) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	if len(receipt.Commit) != len(intent.BaseCommit) {
		return errors.New("promoted Linux clone commit uses a different object format from the base")
	}
	expectedRef, err := authority.LinuxClonePromotionRef(intent.RunID, intent.Generation)
	if err != nil {
		return err
	}
	if receipt.Ref != expectedRef {
		return errors.New("Linux clone promotion receipt names a different RunKey ref")
	}
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, receipt.Ref); err != nil {
		return fmt.Errorf("promoted Linux clone ref lock is unresolved: %w", err)
	}
	if err := verifyLinuxCloneRef(ctx, repository, receipt.Ref, receipt.Commit); err != nil {
		return fmt.Errorf("verify promoted Linux clone ref: %w", err)
	}
	digest, err := linuxCloneTreeDigest(ctx, repository, receipt.Commit)
	if err != nil {
		return err
	}
	if digest != receipt.TreeDigest {
		return errors.New("promoted Linux clone tree digest differs from its State receipt")
	}
	return nil
}

func promoteLinuxCloneRef(ctx context.Context, repository string, intent authority.LinuxCloneIntent, commit, treeDigest string) (authority.LinuxClonePromotionReceipt, error) {
	ref, err := authority.LinuxClonePromotionRef(intent.RunID, intent.Generation)
	if err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, ref); err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if err := verifyLinuxCloneRef(ctx, repository, ref, commit); err == nil {
		digest, digestErr := linuxCloneTreeDigest(ctx, repository, commit)
		if digestErr != nil {
			return authority.LinuxClonePromotionReceipt{}, digestErr
		}
		if digest != treeDigest {
			return authority.LinuxClonePromotionReceipt{}, errors.New("replayed promotion ref has a different verified tree digest")
		}
		return authority.LinuxClonePromotionReceipt{Version: authority.LinuxCloneVersion, Commit: commit, TreeDigest: digest, Ref: ref}, nil
	} else if !errors.Is(err, errLinuxCloneRefMissing) {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	zero := strings.Repeat("0", len(commit))
	_, updateErr := linuxCloneGitOutput(ctx, repository, 4096, errors.New("Git ref output exceeds limit"), "update-ref", "--no-deref", ref, commit, zero)
	if updateErr != nil {
		if errors.Is(updateErr, ErrLinuxCloneProcessUnresolved) {
			return authority.LinuxClonePromotionReceipt{}, updateErr
		}
		if err := verifyLinuxCloneRef(ctx, repository, ref, commit); err != nil {
			return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("compare-create A-owned Linux clone promotion ref: %w", updateErr)
		}
	}
	if err := requireLinuxCloneRefLockAbsent(ctx, repository, ref); err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if err := verifyLinuxCloneRef(ctx, repository, ref, commit); err != nil {
		return authority.LinuxClonePromotionReceipt{}, fmt.Errorf("read back A-owned Linux clone promotion ref: %w", err)
	}
	digest, err := linuxCloneTreeDigest(ctx, repository, commit)
	if err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	if digest != treeDigest {
		return authority.LinuxClonePromotionReceipt{}, errors.New("A source tree changed while promoting Linux clone commit")
	}
	receipt := authority.LinuxClonePromotionReceipt{Version: authority.LinuxCloneVersion, Commit: commit, TreeDigest: digest, Ref: ref}
	if err := receipt.Validate(); err != nil {
		return authority.LinuxClonePromotionReceipt{}, err
	}
	return receipt, nil
}

func importLinuxCloneCandidate(ctx context.Context, repository, quarantine, commit string) error {
	ref := "refs/heads/symmetry/clone-v2/candidate"
	zero := strings.Repeat("0", len(commit))
	if _, err := linuxCloneGitOutput(ctx, quarantine, 4096, errors.New("Git ref output exceeds limit"), "update-ref", "--no-deref", ref, commit, zero); err != nil {
		return fmt.Errorf("pin candidate in A-only quarantine: %w", err)
	}
	_, err := linuxCloneGitOutput(ctx, repository, 64<<10, errors.New("Git fetch output exceeds limit"),
		"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--no-auto-maintenance", quarantine, ref)
	if err != nil {
		return err
	}
	resolved, err := linuxCloneGitOutput(ctx, repository, 4096, errors.New("Git object ID output exceeds limit"), "rev-parse", "--verify", commit+"^{commit}")
	if err != nil || strings.TrimSpace(string(resolved)) != commit {
		return fmt.Errorf("verify imported Linux clone commit: %w", linuxCloneErrorOr(err, errors.New("source resolved a different commit")))
	}
	return nil
}

func verifyLinuxCloneCandidate(ctx context.Context, quarantine, baseCommit, proposedCommit string) error {
	resolved, err := linuxCloneGitOutput(ctx, quarantine, 4096, errors.New("Git object ID output exceeds limit"), "rev-parse", "--verify", proposedCommit+"^{commit}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(resolved)) != proposedCommit {
		return errors.New("quarantine resolved a different proposed commit")
	}
	typeOutput, err := linuxCloneGitOutput(ctx, quarantine, 128, errors.New("Git object type output exceeds limit"), "cat-file", "-t", proposedCommit)
	if err != nil || strings.TrimSpace(string(typeOutput)) != "commit" {
		return fmt.Errorf("proposed object is not a commit: %w", linuxCloneErrorOr(err, errors.New("object type differs")))
	}
	if _, err := linuxCloneGitOutput(ctx, quarantine, 128<<10, errors.New("Git fsck output exceeds limit"), "fsck", "--full", "--strict", "--no-reflogs", proposedCommit); err != nil {
		return fmt.Errorf("verify complete candidate object graph: %w", err)
	}
	_, err = linuxCloneGitOutput(ctx, quarantine, 128, errors.New("Git ancestry output exceeds limit"), "merge-base", "--is-ancestor", baseCommit, proposedCommit)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if exitCode, ok := linuxCloneExitCode(err); ok && exitCode == 1 {
			return errors.New("proposed Linux clone commit is not descended from its admitted base")
		}
		return fmt.Errorf("verify admitted base ancestry: %w", err)
	}
	return nil
}

func linuxCloneTreeDigest(ctx context.Context, repository, commit string) (string, error) {
	return gitTreeDigestWithReaders(ctx, repository, commit, linuxCloneGitOutput, linuxCloneGitBlobContent)
}

func linuxCloneGitBlobContent(ctx context.Context, repository, objectID string) ([]byte, error) {
	return linuxCloneGitOutput(ctx, repository, maxSubjectArtifactBlobBytes, ErrSubjectArtifactTooLarge, "cat-file", "blob", objectID)
}

func linuxCloneGitOutput(ctx context.Context, repository string, maxBytes int, limitErr error, arguments ...string) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("Linux clone Git output limit must be positive")
	}
	commandContext, cancel := context.WithTimeout(ctx, linuxCloneGitCommandTimeout)
	defer cancel()
	command := linuxCloneGitCommand(commandContext, repository, arguments...)
	var output bytes.Buffer
	if _, err := runLinuxCloneGitStdout(commandContext, command, int64(maxBytes), limitErr, &output); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func linuxCloneGitCommand(ctx context.Context, repository string, arguments ...string) *exec.Cmd {
	commandArguments := []string{"--no-replace-objects", "-c", "core.hooksPath=/dev/null", "-c", "core.fsync=all", "-C", repository}
	commandArguments = append(commandArguments, arguments...)
	command := exec.CommandContext(ctx, "/usr/bin/git", commandArguments...)
	command.WaitDelay = linuxCloneGitKillTimeout
	var cancelOnce sync.Once
	var cancelErr error
	command.Cancel = func() error {
		cancelOnce.Do(func() {
			if command.Process == nil {
				cancelErr = os.ErrProcessDone
				return
			}
			identity, err := platform.ProcessIdentity(command.Process.Pid)
			if err != nil {
				cancelErr = fmt.Errorf("%w: capture Git process identity: %v", ErrLinuxCloneProcessUnresolved, err)
				return
			}
			stopContext, stopCancel := context.WithTimeout(context.Background(), linuxCloneGitKillTimeout)
			defer stopCancel()
			if err := platform.TerminateProcessGroup(stopContext, command.Process, identity); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, unix.ESRCH) {
				cancelErr = fmt.Errorf("%w: %v", ErrLinuxCloneProcessUnresolved, err)
			}
		})
		return cancelErr
	}
	command.Env = []string{
		"HOME=/nonexistent",
		"PATH=/usr/bin:/bin",
		"LC_ALL=C",
		"LANG=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0=/dev/null",
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_ATTR_NOSYSTEM=1",
	}
	return command
}

func runLinuxCloneGitStdout(ctx context.Context, command *exec.Cmd, maxBytes int64, limitErr error, destination io.Writer) (int64, error) {
	if ctx == nil || command == nil || destination == nil || maxBytes <= 0 || limitErr == nil {
		return 0, errors.New("Linux clone Git output operation is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := platform.ConfigureProcess(command); err != nil {
		return 0, fmt.Errorf("configure exact Git process group: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return 0, err
	}
	command.Stderr = io.Discard
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		return 0, fmt.Errorf("configure headless Git process: %w", err)
	}
	if err := command.Start(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, ctxErr
		}
		return 0, err
	}
	type copyResult struct {
		size int64
		err  error
	}
	copied := make(chan copyResult, 1)
	go func() {
		size, copyErr := io.Copy(destination, io.LimitReader(stdout, maxBytes+1))
		copied <- copyResult{size: size, err: copyErr}
	}()
	var result copyResult
	select {
	case result = <-copied:
	case <-ctx.Done():
		terminateErr := command.Cancel()
		if terminateErr != nil && command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = stdout.Close()
		result = <-copied
		waitErr := command.Wait()
		return result.size, errors.Join(ErrLinuxCloneProcessUnresolved, ctx.Err(), terminateErr, waitErr)
	}
	overLimit := result.size > maxBytes
	if overLimit || result.err != nil {
		terminateErr := command.Cancel()
		if terminateErr != nil && command.Process != nil {
			_ = command.Process.Kill()
			_ = stdout.Close()
		}
		waitErr := command.Wait()
		failure := result.err
		if overLimit {
			failure = fmt.Errorf("%w: output exceeded %d bytes", limitErr, maxBytes)
		}
		return result.size, errors.Join(ErrLinuxCloneProcessUnresolved, ctx.Err(), failure, terminateErr, waitErr)
	}
	waitErr := command.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		terminateErr := command.Cancel()
		return result.size, errors.Join(ErrLinuxCloneProcessUnresolved, ctxErr, terminateErr, waitErr)
	}
	if waitErr != nil {
		return result.size, waitErr
	}
	return result.size, nil
}

func writeLinuxCloneBundle(ctx context.Context, repository, ref string, destination *os.File) (string, int64, error) {
	if ctx == nil || destination == nil {
		return "", 0, errors.New("Linux clone bundle writer is incomplete")
	}
	commandContext, cancel := context.WithTimeout(ctx, linuxCloneGitCommandTimeout)
	defer cancel()
	command := linuxCloneGitCommand(commandContext, repository, "bundle", "create", "--version=3", "-", ref)
	hasher := sha256.New()
	size, err := runLinuxCloneGitStdout(commandContext, command, maxLinuxCloneBundleBytes, errLinuxCloneBundleTooLarge, io.MultiWriter(destination, hasher))
	if err != nil {
		return "", size, err
	}
	if size == 0 {
		return "", size, errors.New("git bundle create returned no data")
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func linuxCloneExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	return exitErr.ExitCode(), true
}

func linuxCloneErrorOr(actual, fallback error) error {
	if actual != nil {
		return actual
	}
	return fallback
}

type linuxCloneExpectedDigest struct {
	digest        string
	size          int64
	device        uint64
	inode         uint64
	checkIdentity bool
}

func openLinuxCloneBundle(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func readLinuxCloneSeed(ctx context.Context, repository string, parentFD int, name string, intent authority.LinuxCloneIntent, ref string, expected *linuxCloneExpectedDigest) (authority.LinuxCloneSeedReceipt, error) {
	if err := ctx.Err(); err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	fd, err := openLinuxCloneFileAt(parentFD, name, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || uint32(before.Uid) != uint32(os.Geteuid()) || before.Mode&0o022 != 0 || before.Mode&0o007 != 0 ||
		(before.Mode&0o200 != 0 && before.Mode&0o070 != 0) || expected != nil && before.Mode&0o222 != 0 ||
		before.Size <= 0 || before.Size > maxLinuxCloneBundleBytes {
		return authority.LinuxCloneSeedReceipt{}, errors.New("Linux clone seed must be an A-owned, private-or-read-only, bounded regular file")
	}
	if expected != nil && (expected.checkIdentity && (uint64(before.Dev) != expected.device || uint64(before.Ino) != expected.inode) || before.Size != expected.size) {
		return authority.LinuxCloneSeedReceipt{}, errors.New("Linux clone seed inode or size differs from the journal receipt")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	hasher := sha256.New()
	size, err := io.Copy(hasher, io.LimitReader(linuxCloneContextReader{ctx: ctx, reader: file}, maxLinuxCloneBundleBytes+1))
	if err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	if size != before.Size || size > maxLinuxCloneBundleBytes {
		return authority.LinuxCloneSeedReceipt{}, errors.New("Linux clone seed readback size changed")
	}
	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if expected != nil && digest != expected.digest {
		return authority.LinuxCloneSeedReceipt{}, errors.New("Linux clone seed digest differs from the journal receipt")
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || !sameLinuxCloneFileIdentity(&before, &after) {
		return authority.LinuxCloneSeedReceipt{}, errors.New("Linux clone seed changed during exact readback")
	}
	var named unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameLinuxCloneFileIdentity(&before, &named) {
		return authority.LinuxCloneSeedReceipt{}, errors.New("Linux clone seed name changed during exact readback")
	}
	path := filepath.Join(filepath.Dir(intent.SeedPath), name)
	if err := verifyLinuxCloneBundle(ctx, repository, path, intent, ref); err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	receipt := authority.LinuxCloneSeedReceipt{
		Version:    authority.LinuxCloneVersion,
		BaseCommit: intent.BaseCommit,
		Digest:     digest,
		Size:       size,
		Device:     uint64(before.Dev),
		Inode:      uint64(before.Ino),
	}
	if err := receipt.Validate(); err != nil {
		return authority.LinuxCloneSeedReceipt{}, err
	}
	if expected != nil && (receipt.Digest != expected.digest || receipt.Size != expected.size ||
		expected.checkIdentity && (receipt.Device != expected.device || receipt.Inode != expected.inode)) {
		return authority.LinuxCloneSeedReceipt{}, errors.New("Linux clone seed receipt readback differs")
	}
	return receipt, nil
}

func freezeLinuxCloneSeed(parentFD int, name string, receipt authority.LinuxCloneSeedReceipt) error {
	fd, err := openLinuxCloneFileAt(parentFD, name, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || uint32(before.Uid) != uint32(os.Geteuid()) ||
		uint64(before.Dev) != receipt.Device || uint64(before.Ino) != receipt.Inode || before.Size != receipt.Size ||
		before.Mode&0o022 != 0 || before.Mode&0o007 != 0 || (before.Mode&0o200 != 0 && before.Mode&0o070 != 0) {
		return errors.New("Linux clone seed changed before immutable publication")
	}
	if err := unix.Fchmod(fd, 0o440); err != nil {
		return fmt.Errorf("protect B-readable Linux clone seed: %w", err)
	}
	if err := unix.Fsync(fd); err != nil {
		return fmt.Errorf("sync immutable Linux clone seed: %w", err)
	}
	var after, named unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || !sameLinuxCloneFileIdentity(&before, &after) || after.Mode&0o222 != 0 {
		return errors.New("Linux clone seed changed while making it immutable")
	}
	if err := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameLinuxCloneFileIdentity(&after, &named) {
		return errors.New("Linux clone seed path changed while making it immutable")
	}
	return nil
}

func sameLinuxCloneFileIdentity(left, right *unix.Stat_t) bool {
	return left != nil && right != nil && left.Dev == right.Dev && left.Ino == right.Ino && left.Mode&unix.S_IFMT == right.Mode&unix.S_IFMT && left.Size == right.Size
}
