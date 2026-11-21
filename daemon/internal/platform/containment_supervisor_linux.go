//go:build linux

package platform

// Linux containment is deliberately implemented as a helper-first launch.
// The daemon never starts the target itself: the helper owns the target's
// ptrace state, process-group monitor, and reaping boundary.  The daemon keeps
// only a duplicate pidfd as a volatile emergency mirror.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

const (
	linuxSupervisorOpLaunch  = "launch"
	linuxSupervisorOpHello   = "hello"
	linuxSupervisorOpBind    = "bind"
	linuxSupervisorOpCommit  = "commit"
	linuxSupervisorOpResume  = "resume"
	linuxSupervisorOpStop    = "stop"
	linuxSupervisorOpClose   = "close"
	linuxSupervisorOpAbort   = "abort"
	linuxSupervisorOpRenew   = "renew"
	linuxSupervisorOpRelease = "release"
	linuxSupervisorOpWait    = "wait"
	linuxSupervisorOpScan    = "scan_update"

	linuxSupervisorOwnerKind                  = authority.OwnerKindLinuxHelper
	linuxSupervisorTimeout                    = 5 * time.Second
	linuxSupervisorScanFailureChildren        = "children_after_initial"
	linuxSupervisorScanPendingChildren        = "children_after_initial_pending"
	linuxSupervisorScanPauseAfterClean        = "pause_after_clean_scan"
	linuxSupervisorScanSealAfterClean         = "seal_after_clean_scan"
	linuxSupervisorReleaseBarrierTimeout      = 30 * time.Second
	linuxSupervisorReleaseBarrierPollInterval = 25 * time.Millisecond
)

var (
	ErrLinuxSupervisorNotCommitted                  = errors.New("Linux supervisor authority is not committed")
	ErrLinuxSupervisorOwnerLost                     = errors.New("Linux supervisor owner was lost")
	ErrLinuxSupervisorHelperExited                  = errors.New("Linux supervisor helper exited")
	ErrLinuxSupervisorStopUnproven                  = errors.New("Linux supervisor stop is unproven")
	ErrLinuxSupervisorReleased                      = errors.New("Linux supervisor containment was released")
	ErrLinuxSupervisorLeaseExpired                  = errors.New("Linux supervisor lease expired")
	ErrLinuxSupervisorLeaseUnarmed                  = errors.New("Linux supervisor lease is not armed")
	errLinuxSupervisorInjectedDescendantScanFailure = errors.New("injected Linux supervisor descendant scan failure")
	errLinuxSupervisorControlWriteLost              = errors.New("Linux supervisor control request write failed")

	linuxSupervisorLstat                   = os.Lstat
	linuxSupervisorProcessIdentity         = ProcessIdentity
	linuxSupervisorProveProcessGroupAbsent = ProvePersistedProcessGroupAbsent
)

type linuxSupervisorHelloAnchorWitness struct {
	TargetPID       int    `json:"target_pid"`
	TargetIdentity  string `json:"target_identity"`
	TargetPGRP      int64  `json:"target_pgrp"`
	TargetSession   int64  `json:"target_session"`
	TargetStartTime uint64 `json:"target_start_time"`
}

type linuxSupervisorHelloSessionWitness struct {
	Actual             linuxSupervisorHelloAnchorWitness `json:"actual"`
	Transmitted        linuxSupervisorHelloAnchorWitness `json:"transmitted"`
	SupervisorPID      int                               `json:"supervisor_pid"`
	SupervisorIdentity string                            `json:"supervisor_identity"`
}

type linuxSupervisorStopReceiptSource uint8

const (
	linuxSupervisorStopReceiptUnknown linuxSupervisorStopReceiptSource = iota
	linuxSupervisorStopReceiptFromHelper
	linuxSupervisorStopReceiptFromAbsenceProof
)

type linuxSupervisorScanFailureInjection struct {
	triggerPath     string
	firedPath       string
	releasePath     string
	sealOwner       string
	pending         bool
	pauseAfterClean bool
	sealAfterClean  bool
}

func linuxSupervisorScanFailureInjectionFromEnv() (linuxSupervisorScanFailureInjection, bool) {
	witness := linuxSupervisorWitnessConfigFromEnv()
	if strings.TrimSpace(witness.productionWitness) != "1" ||
		strings.TrimSpace(witness.scanFailureMode) == "" {
		return linuxSupervisorScanFailureInjection{}, false
	}
	mode := strings.TrimSpace(witness.scanFailureMode)
	if mode != linuxSupervisorScanFailureChildren && mode != linuxSupervisorScanPendingChildren &&
		mode != linuxSupervisorScanPauseAfterClean && mode != linuxSupervisorScanSealAfterClean {
		return linuxSupervisorScanFailureInjection{}, false
	}
	triggerPath := strings.TrimSpace(witness.scanFailureTriggerPath)
	firedPath := strings.TrimSpace(witness.scanFailureFiredPath)
	if triggerPath == "" || firedPath == "" || !filepath.IsAbs(triggerPath) || !filepath.IsAbs(firedPath) {
		return linuxSupervisorScanFailureInjection{}, false
	}
	releasePath := strings.TrimSpace(witness.scanPendingReleasePath)
	if (mode == linuxSupervisorScanPendingChildren || mode == linuxSupervisorScanPauseAfterClean || mode == linuxSupervisorScanSealAfterClean) &&
		(releasePath == "" || !filepath.IsAbs(releasePath)) {
		return linuxSupervisorScanFailureInjection{}, false
	}
	sealOwner := ""
	if mode == linuxSupervisorScanSealAfterClean {
		sealOwner = strings.TrimSpace(witness.scanSealOwner)
		if sealOwner == "" {
			sealOwner = "both"
		}
		if sealOwner != "both" && sealOwner != authority.SupervisorScanOwnerHelper && sealOwner != authority.SupervisorScanOwnerMirror {
			return linuxSupervisorScanFailureInjection{}, false
		}
	}
	return linuxSupervisorScanFailureInjection{
		triggerPath: triggerPath, firedPath: firedPath, releasePath: releasePath, sealOwner: sealOwner,
		pending:         mode == linuxSupervisorScanPendingChildren,
		pauseAfterClean: mode == linuxSupervisorScanPauseAfterClean,
		sealAfterClean:  mode == linuxSupervisorScanSealAfterClean,
	}, true
}

func validateLinuxSupervisorScanFailureEnv() error {
	witness := linuxSupervisorWitnessConfigFromEnv()
	if witness.productionWitness != "1" {
		return nil
	}
	mode := strings.TrimSpace(witness.scanFailureMode)
	if mode == "" {
		return nil
	}
	if mode != linuxSupervisorScanFailureChildren && mode != linuxSupervisorScanPendingChildren &&
		mode != linuxSupervisorScanPauseAfterClean && mode != linuxSupervisorScanSealAfterClean {
		return fmt.Errorf("unsupported Linux supervisor scan-failure mode %q", mode)
	}
	triggerPath := strings.TrimSpace(witness.scanFailureTriggerPath)
	firedPath := strings.TrimSpace(witness.scanFailureFiredPath)
	if triggerPath == "" || firedPath == "" || !filepath.IsAbs(triggerPath) || !filepath.IsAbs(firedPath) {
		return errors.New("Linux supervisor scan-failure injection requires absolute trigger and fired marker paths")
	}
	if mode == linuxSupervisorScanPendingChildren || mode == linuxSupervisorScanPauseAfterClean || mode == linuxSupervisorScanSealAfterClean {
		releasePath := strings.TrimSpace(witness.scanPendingReleasePath)
		if releasePath == "" || !filepath.IsAbs(releasePath) {
			return errors.New("Linux supervisor scan barrier requires an absolute release marker path")
		}
	}
	if mode == linuxSupervisorScanSealAfterClean {
		owner := strings.TrimSpace(witness.scanSealOwner)
		if owner != "" && owner != "both" && owner != authority.SupervisorScanOwnerHelper && owner != authority.SupervisorScanOwnerMirror {
			return fmt.Errorf("unsupported Linux supervisor scan seal owner %q", owner)
		}
	}
	return nil
}

type linuxSupervisorHelloSessionInjection struct {
	firedPath   string
	releasePath string
}

func linuxSupervisorHelloSessionInjectionFromEnv() (linuxSupervisorHelloSessionInjection, bool, error) {
	witness := linuxSupervisorWitnessConfigFromEnv()
	if witness.productionWitness != "1" {
		return linuxSupervisorHelloSessionInjection{}, false, nil
	}
	mode := strings.TrimSpace(witness.helloSessionInjection)
	if mode == "" {
		return linuxSupervisorHelloSessionInjection{}, false, nil
	}
	if mode != "1" {
		return linuxSupervisorHelloSessionInjection{}, false, fmt.Errorf("unsupported Linux supervisor wrong-session hello injection mode %q", mode)
	}
	firedPath := strings.TrimSpace(witness.helloSessionFiredPath)
	if firedPath == "" || !filepath.IsAbs(firedPath) {
		return linuxSupervisorHelloSessionInjection{}, false, errors.New("Linux supervisor wrong-session hello injection requires an absolute fired marker path")
	}
	releasePath := strings.TrimSpace(witness.helloSessionReleasePath)
	if releasePath != "" && !filepath.IsAbs(releasePath) {
		return linuxSupervisorHelloSessionInjection{}, false, errors.New("Linux supervisor wrong-session hello barrier requires an absolute release marker path")
	}
	return linuxSupervisorHelloSessionInjection{firedPath: firedPath, releasePath: releasePath}, true, nil
}

func wrongLinuxSupervisorHelloSession(anchor LinuxProcessGroupAnchor) (int64, error) {
	session := anchor.Session + 1
	if session <= 0 {
		session = anchor.Session - 1
	}
	if session == int64(anchor.PID) {
		session++
	}
	if session <= 0 || session == anchor.Session || session == int64(anchor.PID) {
		return 0, errors.New("cannot construct a plausible wrong Linux supervisor target session")
	}
	return session, nil
}

// publishLinuxSupervisorWitnessFile publishes a helper-side witness document as
// one visible state change: readers poll these paths and accept the first
// successful read, so a document must never be observable while it is
// incomplete. The previous create-then-write sequence exposed an empty file
// first, and the wrong-session test decoded that intermediate state as
// `decode wrong-session helper witness: unexpected end of JSON input` (CI run
// 44133750246). The document is assembled in a temporary file in the
// destination directory and renamed into place; the destination must still be
// absent, which keeps the create-with-O_EXCL contract of witness paths that
// this package validates as absent before a run.
func publishLinuxSupervisorWitnessFile(path string, contents []byte) error {
	if err := validateLinuxSupervisorWitnessMarkerParent(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("witness path %q already exists", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".partial-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	written, writeErr := temporary.Write(contents)
	if writeErr == nil && written != len(contents) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = temporary.Sync()
	}
	if closeErr := temporary.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr == nil {
		writeErr = os.Rename(temporaryPath, path)
	}
	if writeErr != nil {
		_ = os.Remove(temporaryPath)
		return writeErr
	}
	return nil
}

func writeLinuxSupervisorHelloSessionWitness(path string, actual, transmitted linuxSupervisorHello) error {
	witness := linuxSupervisorHelloSessionWitness{
		Actual: linuxSupervisorHelloAnchorWitness{
			TargetPID: actual.TargetPID, TargetIdentity: actual.TargetIdentity,
			TargetPGRP: actual.TargetPGRP, TargetSession: actual.TargetSession,
			TargetStartTime: actual.TargetStartTime,
		},
		Transmitted: linuxSupervisorHelloAnchorWitness{
			TargetPID: transmitted.TargetPID, TargetIdentity: transmitted.TargetIdentity,
			TargetPGRP: transmitted.TargetPGRP, TargetSession: transmitted.TargetSession,
			TargetStartTime: transmitted.TargetStartTime,
		},
		SupervisorPID: transmitted.SupervisorPID, SupervisorIdentity: transmitted.SupervisorIdentity,
	}
	contents, err := json.Marshal(witness)
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	return publishLinuxSupervisorWitnessFile(path, contents)
}

func waitForLinuxSupervisorHelloSessionRelease(owner *os.File, releasePath string) error {
	deadline := time.Now().Add(linuxSupervisorReleaseBarrierTimeout)
	ownerPoll := []unix.PollFd{{Fd: int32(owner.Fd()), Events: unix.POLLHUP | unix.POLLERR}}
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("wait for Linux supervisor wrong-session release marker: %w", context.DeadlineExceeded)
		}
		pollFor := remaining
		if pollFor > 25*time.Millisecond {
			pollFor = 25 * time.Millisecond
		}
		milliseconds := int((pollFor + time.Millisecond - 1) / time.Millisecond)
		ownerPoll[0].Revents = 0
		count, err := unix.Poll(ownerPoll, milliseconds)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return fmt.Errorf("watch Linux supervisor owner during wrong-session barrier: %w", err)
		}
		if count > 0 && ownerPoll[0].Revents&(unix.POLLHUP|unix.POLLERR) != 0 {
			return io.EOF
		}
		if _, err := os.Stat(releasePath); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read Linux supervisor wrong-session release marker: %w", err)
		}
	}
}

type linuxSupervisorOwnerLossProofBarrier struct {
	reachedPath string
	releasePath string
	resultPath  string
}

type linuxSupervisorOwnerRecoveryAuthenticatedMarker struct {
	path string
}

func linuxSupervisorOwnerRecoveryAuthenticatedMarkerFromEnv() (linuxSupervisorOwnerRecoveryAuthenticatedMarker, bool, error) {
	witness := linuxSupervisorWitnessConfigFromEnv()
	if !witness.ownerRecoveryAuthenticatedSet {
		return linuxSupervisorOwnerRecoveryAuthenticatedMarker{}, false, nil
	}
	if witness.productionWitness != "1" {
		return linuxSupervisorOwnerRecoveryAuthenticatedMarker{}, false, errors.New("Linux supervisor authenticated owner-recovery marker requires the production witness flag")
	}
	path := strings.TrimSpace(witness.ownerRecoveryAuthenticatedPath)
	if path == "" || !filepath.IsAbs(path) {
		return linuxSupervisorOwnerRecoveryAuthenticatedMarker{}, false, errors.New("Linux supervisor authenticated owner-recovery marker requires a non-empty absolute file path")
	}
	if err := assertLinuxSupervisorBarrierPathAbsent(path); err != nil {
		return linuxSupervisorOwnerRecoveryAuthenticatedMarker{}, false, fmt.Errorf("Linux supervisor authenticated owner-recovery marker path is not absent: %w", err)
	}
	return linuxSupervisorOwnerRecoveryAuthenticatedMarker{path: path}, true, nil
}

// writeLinuxSupervisorOwnerRecoveryAuthenticatedMarker records test evidence
// only; it never grants recovery authority.
func writeLinuxSupervisorOwnerRecoveryAuthenticatedMarker(marker linuxSupervisorOwnerRecoveryAuthenticatedMarker, operation string, targetPID, helperPID int, proofCategory string) error {
	if marker.path == "" {
		return nil
	}
	switch operation {
	case linuxSupervisorOpStop, linuxSupervisorOpClose, linuxSupervisorOpAbort, linuxSupervisorOpRelease:
	default:
		return errors.New("Linux supervisor authenticated owner-recovery marker operation is invalid")
	}
	if targetPID <= 0 || helperPID <= 1 {
		return errors.New("Linux supervisor authenticated owner-recovery marker process identity is invalid")
	}
	if proofCategory != "identity_or_anchor_mismatch" && proofCategory != "other_error" {
		return errors.New("Linux supervisor authenticated owner-recovery marker has no latched owner-loss proof category")
	}
	contents := fmt.Sprintf("operation=%s\ntarget_pid=%d\nhelper_pid=%d\nowner_loss_proof_category=%s\n", operation, targetPID, helperPID, proofCategory)
	if err := writeLinuxSupervisorAbsentWitnessMarker(marker.path, contents); err != nil {
		return fmt.Errorf("publish Linux supervisor authenticated owner-recovery marker: %w", err)
	}
	return nil
}

func linuxSupervisorOwnerLossProofBarrierFromEnv() (linuxSupervisorOwnerLossProofBarrier, bool, error) {
	witness := linuxSupervisorWitnessConfigFromEnv()
	if witness.productionWitness != "1" {
		return linuxSupervisorOwnerLossProofBarrier{}, false, nil
	}
	var barrier linuxSupervisorOwnerLossProofBarrier
	values := []struct {
		raw        string
		configured bool
		path       *string
	}{
		{witness.ownerLossProofReachedPath, witness.ownerLossProofReachedSet, &barrier.reachedPath},
		{witness.ownerLossProofReleasePath, witness.ownerLossProofReleaseSet, &barrier.releasePath},
		{witness.ownerLossProofResultPath, witness.ownerLossProofResultSet, &barrier.resultPath},
	}
	configured := 0
	for _, value := range values {
		if value.configured {
			configured++
			*value.path = strings.TrimSpace(value.raw)
		}
	}
	if configured == 0 {
		return linuxSupervisorOwnerLossProofBarrier{}, false, nil
	}
	if configured != len(values) {
		return linuxSupervisorOwnerLossProofBarrier{}, false, errors.New("Linux supervisor owner-loss proof barrier requires all reached, release, and result marker paths")
	}
	paths := []string{barrier.reachedPath, barrier.releasePath, barrier.resultPath}
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) {
			return linuxSupervisorOwnerLossProofBarrier{}, false, errors.New("Linux supervisor owner-loss proof barrier requires non-empty absolute marker paths")
		}
	}
	for left := range paths {
		for right := left + 1; right < len(paths); right++ {
			if filepath.Clean(paths[left]) == filepath.Clean(paths[right]) {
				return linuxSupervisorOwnerLossProofBarrier{}, false, errors.New("Linux supervisor owner-loss proof barrier marker paths must be distinct")
			}
		}
	}
	for _, path := range paths {
		if err := assertLinuxSupervisorBarrierPathAbsent(path); err != nil {
			return linuxSupervisorOwnerLossProofBarrier{}, false, fmt.Errorf("Linux supervisor owner-loss proof barrier marker path is not absent: %w", err)
		}
	}
	return barrier, true, nil
}

func validateLinuxSupervisorOwnerLossProofBarrierEnv() error {
	_, _, err := linuxSupervisorOwnerLossProofBarrierFromEnv()
	return err
}

func waitForLinuxSupervisorOwnerLossProofBarrier(barrier linuxSupervisorOwnerLossProofBarrier) error {
	if err := writeLinuxSupervisorAbsentWitnessMarker(barrier.reachedPath, "post_reap_pre_proof\n"); err != nil {
		return fmt.Errorf("publish Linux supervisor owner-loss proof barrier: %w", err)
	}
	deadline := time.NewTimer(linuxSupervisorReleaseBarrierTimeout)
	defer deadline.Stop()
	poll := time.NewTicker(linuxSupervisorReleaseBarrierPollInterval)
	defer poll.Stop()
	for {
		if _, err := os.Lstat(barrier.releasePath); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("observe Linux supervisor owner-loss proof release marker: %w", err)
		}
		select {
		case <-deadline.C:
			return fmt.Errorf("Linux supervisor owner-loss proof barrier was not released within %s", linuxSupervisorReleaseBarrierTimeout)
		case <-poll.C:
		}
	}
}

func writeLinuxSupervisorOwnerLossProofResult(barrier linuxSupervisorOwnerLossProofBarrier, proofErr error) error {
	contents := "proof_attempted\n"
	if category := linuxSupervisorOwnerLossProofResultCategory(proofErr); category != "" {
		contents += "category=" + category + "\n"
	}
	if err := writeLinuxSupervisorAbsentWitnessMarker(barrier.resultPath, contents); err != nil {
		return fmt.Errorf("publish Linux supervisor owner-loss proof result: %w", err)
	}
	return nil
}

func linuxSupervisorOwnerLossProofResultCategory(proofErr error) string {
	if proofErr == nil {
		return ""
	}
	message := strings.ToLower(proofErr.Error())
	if strings.Contains(message, "identity") || strings.Contains(message, "anchor") || strings.Contains(message, "fence") || strings.Contains(message, "reused") {
		return "identity_or_anchor_mismatch"
	}
	return "other_error"
}

func writeLinuxSupervisorAbsentWitnessMarker(path, contents string) error {
	return publishLinuxSupervisorWitnessFile(path, []byte(contents))
}

func installLinuxSupervisorScanFailureInjectionAfterInitial(group *processGroup, targetPID int, targetIdentity string) func() {
	return installLinuxSupervisorScanInjection(group, targetPID, targetIdentity, true, authority.SupervisorScanOwnerHelper, false)
}

func installLinuxSupervisorCleanScanBarrier(group *processGroup, targetPID int, targetIdentity, owner string) func() {
	return installLinuxSupervisorScanInjection(group, targetPID, targetIdentity, true, owner, true)
}

func installLinuxSupervisorScanInjection(group *processGroup, targetPID int, targetIdentity string, skipInitial bool, owner string, barrierOnly bool) func() {
	injection, enabled := linuxSupervisorScanFailureInjectionFromEnv()
	if !enabled || group == nil || targetPID <= 0 || targetIdentity == "" {
		return func() {}
	}
	if barrierOnly && !injection.pauseAfterClean && !injection.sealAfterClean {
		return func() {}
	}
	if injection.sealAfterClean && injection.sealOwner != "both" && injection.sealOwner != owner {
		return func() {}
	}

	group.mutex.Lock()
	realStat := group.monitorReadStat
	realChildren := group.monitorReadChildren
	realAfterClean := group.afterCleanScan
	realSealAfterClean := group.sealAfterCleanScan
	realAfterSeal := group.afterDescendantScanSeal
	realSealCleanScanForTesting := group.sealCleanScanForTesting
	if realStat == nil {
		realStat = readLinuxProcessStat
	}
	if realChildren == nil {
		realChildren = readLinuxProcessChildren
	}
	var fired atomic.Bool
	var initialChildrenObserved atomic.Bool
	if !skipInitial {
		initialChildrenObserved.Store(true)
	}
	var restored atomic.Bool
	var injectedStat func(int) (linuxProcessStat, error)
	var injectedChildren func(int) ([]int, error)
	restore := func() {
		if !restored.CompareAndSwap(false, true) {
			return
		}
		group.mutex.Lock()
		group.monitorReadStat = realStat
		group.monitorReadChildren = realChildren
		group.afterCleanScan = realAfterClean
		group.sealAfterCleanScan = realSealAfterClean
		group.afterDescendantScanSeal = realAfterSeal
		group.sealCleanScanForTesting = realSealCleanScanForTesting
		group.mutex.Unlock()
	}
	if injection.pauseAfterClean {
		var firstRetire atomic.Bool
		var barrierFired atomic.Bool
		if !skipInitial {
			firstRetire.Store(true)
		}
		group.afterCleanScan = func(sequence uint64) bool {
			if realAfterClean != nil && realAfterClean(sequence) {
				return true
			}
			if !firstRetire.Swap(true) {
				return false
			}
			if _, err := os.Lstat(injection.triggerPath); err != nil || !barrierFired.CompareAndSwap(false, true) {
				return false
			}
			reachedPath := injection.firedPath + "." + owner
			if err := writeLinuxSupervisorScanWitnessMarker(reachedPath, fmt.Sprintf("owner=%s\nsequence=%d\n", owner, sequence)); err != nil {
				return false
			}
			for {
				select {
				case <-group.monitorStop:
					return true
				default:
				}
				if _, err := os.Lstat(injection.releasePath); err == nil {
					return true
				} else if !errors.Is(err, os.ErrNotExist) {
					return true
				}
				timer := time.NewTimer(linuxSupervisorReleaseBarrierPollInterval)
				select {
				case <-group.monitorStop:
					timer.Stop()
					return true
				case <-timer.C:
				}
			}
		}
		group.mutex.Unlock()
		return restore
	}
	if injection.sealAfterClean {
		var firstRetire atomic.Bool
		var barrierFired atomic.Bool
		if !skipInitial {
			firstRetire.Store(true)
		}
		group.sealAfterCleanScan = func(sequence uint64) bool {
			if realSealAfterClean != nil && realSealAfterClean(sequence) {
				return true
			}
			if !firstRetire.Swap(true) {
				return false
			}
			if _, err := os.Lstat(injection.triggerPath); err != nil || !barrierFired.CompareAndSwap(false, true) {
				return false
			}
			for {
				select {
				case <-group.monitorStop:
					return false
				default:
				}
				if _, err := os.Lstat(injection.releasePath); err == nil {
					return true
				} else if !errors.Is(err, os.ErrNotExist) {
					return true
				}
				timer := time.NewTimer(linuxSupervisorReleaseBarrierPollInterval)
				select {
				case <-group.monitorStop:
					timer.Stop()
					return false
				case <-timer.C:
				}
			}
		}
		group.afterDescendantScanSeal = func(sequence uint64) {
			path := injection.firedPath + "." + owner
			_ = writeLinuxSupervisorScanWitnessMarker(path, fmt.Sprintf("owner=%s\nsequence=%d\n", owner, sequence))
		}
		group.sealCleanScanForTesting = true
		group.mutex.Unlock()
		return restore
	}
	maybeInject := func(pid int) error {
		if pid != targetPID {
			return nil
		}
		if _, err := os.Stat(injection.triggerPath); err != nil {
			return nil
		}
		actualIdentity, identityErr := readProcessIdentity(pid)
		if identityErr != nil || actualIdentity != targetIdentity || !fired.CompareAndSwap(false, true) {
			return nil
		}
		markerErr := writeLinuxSupervisorScanWitnessMarker(injection.firedPath, "fired\n")
		if markerErr != nil {
			restore()
			return fmt.Errorf("%w: write fired marker: %v", errLinuxSupervisorInjectedDescendantScanFailure, markerErr)
		}
		if injection.pending {
			for {
				if _, err := os.Lstat(injection.releasePath); err == nil {
					break
				} else if !errors.Is(err, os.ErrNotExist) {
					restore()
					return fmt.Errorf("%w: observe pending-scan release marker: %v", errLinuxSupervisorInjectedDescendantScanFailure, err)
				}
				timer := time.NewTimer(linuxSupervisorReleaseBarrierPollInterval)
				<-timer.C
			}
		}
		restore()
		return errLinuxSupervisorInjectedDescendantScanFailure
	}
	injectedStat = func(pid int) (linuxProcessStat, error) {
		return realStat(pid)
	}
	injectedChildren = func(pid int) ([]int, error) {
		if pid == targetPID && !initialChildrenObserved.Swap(true) {
			return realChildren(pid)
		}
		if err := maybeInject(pid); err != nil {
			return nil, err
		}
		return realChildren(pid)
	}
	group.monitorReadStat = injectedStat
	group.monitorReadChildren = injectedChildren
	group.mutex.Unlock()
	return restore
}

func channelClosedV2(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (anchor LinuxProcessGroupAnchor) private() linuxProcessGroupAnchor {
	return linuxProcessGroupAnchor{pid: anchor.PID, pgrp: anchor.PGRP, session: anchor.Session, startTime: anchor.StartTime}
}

func assertLinuxSupervisorBarrierPathAbsent(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		return errors.New("path already exists")
	}
	return err
}

func writeLinuxSupervisorResponseV2(writer *os.File, request linuxSupervisorRequest, status string, target *LinuxPtraceProcess, launch linuxSupervisorLaunch, active uint32, message string) error {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	return writeLinuxSupervisorFrame(writer, linuxSupervisorResponse{Version: linuxSupervisorProtocolVersion, Operation: request.Operation, Status: status, LaunchToken: launch.LaunchToken, Sequence: request.Sequence, TargetPID: target.PID(), TargetIdentity: target.Identity(), OwnerKind: linuxSupervisorOwnerKind, OwnerContext: launch.OwnerContext, SupervisorPID: os.Getpid(), SupervisorIdentity: identity, Token: launch.PipeToken, JobID: launch.JobID, ActiveProcesses: active, Error: message})
}

func mustLinuxSupervisorIdentity() string {
	identity, _ := ProcessIdentity(os.Getpid())
	return identity
}

func dialLinuxSupervisorRecovery(value authority.Supervisor) (*net.UnixConn, error) {
	endpoint, err := linuxSupervisorEndpoint(value.OwnerContext)
	if err != nil {
		return nil, err
	}
	connection, err := net.DialTimeout("unix", endpoint, linuxSupervisorTimeout)
	if err != nil {
		return nil, sanitizeLinuxSupervisorEndpointError(err)
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		_ = connection.Close()
		return nil, errors.New("Linux supervisor recovery connection is not Unix")
	}
	return unixConnection, nil
}

func exchangeLinuxSupervisorRecovery(connection *net.UnixConn, request linuxSupervisorRequest, value authority.Supervisor) (linuxSupervisorRecoveryResponse, error) {
	if err := connection.SetDeadline(time.Now().Add(linuxSupervisorTimeout)); err != nil {
		return linuxSupervisorRecoveryResponse{}, fmt.Errorf("set Linux supervisor recovery deadline: %w", sanitizeLinuxSupervisorEndpointError(err))
	}
	defer connection.SetDeadline(time.Time{})
	if err := verifyLinuxSupervisorPeerBeforeSecret(connection, value.SupervisorPID, value.SupervisorIdentity); err != nil {
		return linuxSupervisorRecoveryResponse{}, err
	}
	if err := writeLinuxSupervisorFrame(connection, request); err != nil {
		return linuxSupervisorRecoveryResponse{}, fmt.Errorf("send Linux supervisor recovery request: %w", sanitizeLinuxSupervisorEndpointError(err))
	}
	var response linuxSupervisorRecoveryResponse
	if err := readLinuxSupervisorFrame(bufio.NewReader(connection), &response); err != nil {
		return linuxSupervisorRecoveryResponse{}, fmt.Errorf("read Linux supervisor recovery response: %w", sanitizeLinuxSupervisorEndpointError(err))
	}
	if response.Version != linuxSupervisorProtocolVersion || response.Operation != request.Operation || response.LaunchToken != value.LaunchToken || response.TargetPID != value.TargetPID || response.TargetIdentity != value.TargetIdentity || response.OwnerKind != value.OwnerKind || response.OwnerContext != value.OwnerContext || response.SupervisorPID != value.SupervisorPID || response.SupervisorIdentity != value.SupervisorIdentity || response.Token != value.PipeToken || response.JobID != value.JobID {
		return linuxSupervisorRecoveryResponse{}, errors.New("Linux supervisor recovery response identity mismatch")
	}
	if response.Status == "error" {
		if response.Error == "" {
			return linuxSupervisorRecoveryResponse{}, errors.New("Linux supervisor recovery returned an unspecified error")
		}
		return linuxSupervisorRecoveryResponse{}, errors.New(response.Error)
	}
	if err := verifyLinuxSupervisorPeer(connection, response.SupervisorPID, response.SupervisorIdentity); err != nil {
		return linuxSupervisorRecoveryResponse{}, err
	}
	actual, err := ProcessIdentity(response.SupervisorPID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return linuxSupervisorRecoveryResponse{}, fmt.Errorf("Linux supervisor recovery helper identity is not exact: %v", err)
	}
	if err == nil && actual != response.SupervisorIdentity {
		return linuxSupervisorRecoveryResponse{}, fmt.Errorf("Linux supervisor recovery helper identity is not exact: got %q, want %q", actual, response.SupervisorIdentity)
	}
	return response, nil
}

func verifyLinuxSupervisorPeer(connection *net.UnixConn, expectedPID int, expectedIdentity string) error {
	if connection == nil || expectedPID <= 0 || expectedIdentity == "" {
		return errors.New("Linux supervisor peer identity is incomplete")
	}
	credentials, err := linuxSupervisorPeerCredentials(connection)
	if err != nil {
		return err
	}
	if credentials == nil || int(credentials.Pid) != expectedPID {
		return fmt.Errorf("Linux supervisor peer PID mismatch: got %d, want %d", credentialsPID(credentials), expectedPID)
	}
	actual, err := ProcessIdentity(expectedPID)
	if err != nil {
		// A legitimate release response may race the helper's final exit. The
		// socket peer credentials are already bound to the exact PID; absence
		// after that response is the expected terminal state.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read Linux supervisor peer process identity: %w", err)
	}
	if actual != expectedIdentity {
		return fmt.Errorf("Linux supervisor peer identity mismatch: got %q, want %q", actual, expectedIdentity)
	}
	return nil
}

func verifyLinuxSupervisorPeerBeforeSecret(connection *net.UnixConn, expectedPID int, expectedIdentity string) error {
	if connection == nil || expectedPID <= 0 || expectedIdentity == "" {
		return errors.New("Linux supervisor peer identity is incomplete")
	}
	credentials, err := linuxSupervisorPeerCredentials(connection)
	if err != nil {
		return err
	}
	if int(credentials.Pid) != expectedPID {
		return fmt.Errorf("Linux supervisor peer PID mismatch: got %d, want %d", credentialsPID(credentials), expectedPID)
	}
	actual, err := ProcessIdentity(expectedPID)
	if err != nil {
		return fmt.Errorf("read Linux supervisor peer process identity: %w", err)
	}
	if actual != expectedIdentity {
		return fmt.Errorf("Linux supervisor peer identity mismatch: got %q, want %q", actual, expectedIdentity)
	}
	return nil
}

func verifyLinuxSupervisorUnboundPeer(connection *net.UnixConn, ownerContext string) error {
	credentials, err := linuxSupervisorPeerCredentials(connection)
	if err != nil {
		return err
	}
	if credentials.Pid <= 0 {
		return errors.New("Linux supervisor unbound peer PID is invalid")
	}
	expectedIdentity, err := linuxSupervisorHelperIdentity(ownerContext)
	if err != nil {
		return err
	}
	actual, err := linuxSupervisorProcessIdentity(int(credentials.Pid))
	if err != nil {
		return fmt.Errorf("read Linux supervisor unbound peer identity: %w", err)
	}
	if actual != expectedIdentity {
		return fmt.Errorf("Linux supervisor unbound peer identity mismatch: got %q, want %q", actual, expectedIdentity)
	}
	return nil
}

func linuxSupervisorHelperIdentity(ownerContext string) (string, error) {
	marker := "|helper="
	start := strings.LastIndex(ownerContext, marker)
	if start < 0 {
		return "", errors.New("Linux supervisor owner context has no helper identity fence")
	}
	value := ownerContext[start+len(marker):]
	if end := strings.Index(value, "|endpoint="); end >= 0 {
		value = value[:end]
	}
	value = strings.TrimSpace(value)
	if value == "" || len(value) >= 4096 {
		return "", errors.New("Linux supervisor helper identity fence is invalid")
	}
	return value, nil
}

func linuxSupervisorPeerCredentials(connection *net.UnixConn) (*unix.Ucred, error) {
	if connection == nil {
		return nil, errors.New("Linux supervisor peer connection is nil")
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("capture Linux supervisor peer credentials: %w", err)
	}
	var credentials *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, fmt.Errorf("read Linux supervisor peer credentials: %w", err)
	}
	if controlErr != nil {
		return nil, fmt.Errorf("read Linux supervisor peer credentials: %w", controlErr)
	}
	if credentials == nil {
		return nil, errors.New("Linux supervisor peer credentials are unavailable")
	}
	return credentials, nil
}

func credentialsPID(credentials *unix.Ucred) int {
	if credentials == nil {
		return 0
	}
	return int(credentials.Pid)
}

// sanitizeLinuxSupervisorEndpointError preserves the typed cause and operation
// while removing the random recovery socket path from OS error formatting.
func sanitizeLinuxSupervisorEndpointError(err error) error {
	switch typed := err.(type) {
	case *net.OpError:
		copy := *typed
		copy.Source = nil
		copy.Addr = nil
		copy.Err = sanitizeLinuxSupervisorEndpointError(typed.Err)
		return &copy
	case *os.PathError:
		copy := *typed
		copy.Path = "Linux supervisor recovery endpoint"
		copy.Err = sanitizeLinuxSupervisorEndpointError(typed.Err)
		return &copy
	case *os.SyscallError:
		copy := *typed
		copy.Err = sanitizeLinuxSupervisorEndpointError(typed.Err)
		return &copy
	default:
		return err
	}
}

// linuxSupervisorEndpointMax is sizeof(sun_path); the path must also fit its
// terminating NUL.
const linuxSupervisorEndpointMax = 108

// linuxSupervisorEndpointPath places the recovery socket in the first writable
// directory whose path fits sun_path. A long or read-only TMPDIR, or an image
// without /tmp, would otherwise fail only inside the helper, after the target
// is launched.
func linuxSupervisorEndpointPath(jobID string, directories ...string) (string, error) {
	name := "symmetry-linux-supervisor-" + jobID + ".sock"
	for _, directory := range directories {
		endpoint := filepath.Join(directory, name)
		if !filepath.IsAbs(endpoint) || len(endpoint) >= linuxSupervisorEndpointMax {
			continue
		}
		probe, err := os.CreateTemp(directory, ".symmetry-endpoint-probe-*")
		if err != nil {
			continue
		}
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		return endpoint, nil
	}
	return "", fmt.Errorf("Linux supervisor recovery endpoint needs a writable directory with a path shorter than %d bytes; tried %q", linuxSupervisorEndpointMax-len(name)-1, directories)
}

func linuxSupervisorEndpoint(ownerContext string) (string, error) {
	marker := "|endpoint="
	index := strings.LastIndex(ownerContext, marker)
	if index < 0 {
		return "", errors.New("Linux supervisor owner context has no recovery endpoint")
	}
	endpoint := strings.TrimSpace(ownerContext[index+len(marker):])
	if endpoint == "" || len(endpoint) >= linuxSupervisorEndpointMax || filepath.IsAbs(endpoint) == false {
		return "", errors.New("Linux supervisor recovery endpoint is invalid")
	}
	return endpoint, nil
}

func linuxSupervisorValidHexV2(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func linuxSupervisorSecretMatchesV2(secret, digest string) bool {
	computed := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(computed[:]) == digest
}

func linuxSupervisorRequestMatchesV2(request linuxSupervisorRequest, launch linuxSupervisorLaunch, target *LinuxPtraceProcess) bool {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil || request.Version != linuxSupervisorProtocolVersion || request.LaunchToken != launch.LaunchToken || request.TargetPID != target.PID() || request.TargetIdentity != target.Identity() || request.OwnerKind != linuxSupervisorOwnerKind || request.OwnerContext != launch.OwnerContext || request.SupervisorPID != os.Getpid() || request.SupervisorIdentity != identity || request.Token != launch.PipeToken || request.JobID != launch.JobID {
		return false
	}
	return request.Operation != linuxSupervisorOpLaunch && request.Operation != linuxSupervisorOpHello
}

func linuxSupervisorUnboundAbortRequestMatches(request linuxSupervisorRequest, launch linuxSupervisorLaunch, target *LinuxPtraceProcess) bool {
	return request.Version == linuxSupervisorProtocolVersion && request.Operation == linuxSupervisorOpAbort && request.LaunchToken == launch.LaunchToken && request.TargetPID == target.PID() && request.TargetIdentity == target.Identity() && request.OwnerKind == linuxSupervisorOwnerKind && request.OwnerContext == launch.OwnerContext && request.SupervisorPID == 0 && request.SupervisorIdentity == "" && request.Token == launch.PipeToken && request.JobID == launch.JobID && request.Secret != "" && linuxSupervisorSecretMatchesV2(request.Secret, launch.SecretDigest)
}
