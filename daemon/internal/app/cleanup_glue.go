package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const (
	linuxStopReceiptBarrierWitnessEnv = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS"
	linuxStopReceiptBarrierReachedEnv = "SYMMETRY_LINUX_SUPERVISOR_STOP_RECEIPT_BARRIER_REACHED_FILE"
	linuxStopReceiptBarrierReleaseEnv = "SYMMETRY_LINUX_SUPERVISOR_STOP_RECEIPT_BARRIER_RELEASE_FILE"
	linuxStopReceiptBarrierTimeout    = 30 * time.Second
	linuxStopReceiptBarrierPoll       = 25 * time.Millisecond
)

func (daemon *daemon) persistProcessAuthorityCallback(key state.RunKey) func(int, string, *authority.Supervisor) error {
	return func(pid int, identity string, value *authority.Supervisor) error {
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		if daemon.storeClosing || (daemon.store == nil && daemon.options.recordProcessAuthority == nil) {
			return errDaemonStoreClosing
		}
		if value == nil {
			return errors.New("persisted containment authority is nil")
		}
		persistErr := daemon.persistProcessAuthority(key, pid, identity, value.Clone())
		if persistErr == nil && daemon.options.processObserver != nil {
			daemon.options.processObserver(key, pid, identity, daemon.now())
		}
		return persistErr
	}
}

// persistProcessMarker owns the marker-only CAS boundary. A post-rename
// persistence error leaves the result unknown; an exact readback is only a
// reason to replay the same mutation and require a fresh successful barrier.
func (daemon *daemon) persistProcessMarker(key state.RunKey, pid int, identity string, startedAt time.Time) error {
	daemon.storeLifetimeMu.RLock()
	defer daemon.storeLifetimeMu.RUnlock()
	if daemon.storeClosing || (daemon.store == nil && daemon.options.recordProcess == nil) {
		return errDaemonStoreClosing
	}
	write := func() error {
		if daemon.options.recordProcess != nil {
			_, err := daemon.options.recordProcess(key, pid, identity, startedAt)
			return err
		}
		_, err := daemon.store.SetProcessDetails(key, pid, identity, startedAt)
		return err
	}
	if err := write(); err == nil {
		return nil
	} else {
		firstErr := err
		if daemon.store == nil {
			return firstErr
		}
		journal, readErr := daemon.store.LoadJournal(key)
		if readErr == nil && journal.PID == pid && journal.ProcessIdentity == identity && !journal.StartedAt.IsZero() {
			if replayErr := write(); replayErr == nil {
				return nil
			} else {
				return errors.Join(firstErr, fmt.Errorf("replay process marker write: %w", replayErr))
			}
		}
		if readErr != nil {
			return errors.Join(firstErr, fmt.Errorf("read back process marker: %w", readErr))
		}
		return firstErr
	}
}

func (daemon *daemon) markContainmentUnprovenCallback(key state.RunKey) func(int, string) error {
	return func(pid int, identity string) error {
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		mark := daemon.options.markContainmentUnproven
		if mark == nil {
			if daemon.storeClosing || daemon.store == nil {
				return errDaemonStoreClosing
			}
			mark = daemon.store.MarkContainmentUnproven
		} else if daemon.storeClosing {
			return errDaemonStoreClosing
		}
		_, err := mark(key, pid, identity)
		return err
	}
}

func (daemon *daemon) clearContainmentUnprovenCallback(key state.RunKey) func(int, string) error {
	return func(pid int, identity string) error {
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		clear := daemon.options.clearContainmentUnproven
		if clear == nil {
			if daemon.storeClosing || daemon.store == nil {
				return errDaemonStoreClosing
			}
			clear = daemon.store.ClearContainmentUnproven
		} else if daemon.storeClosing {
			return errDaemonStoreClosing
		}
		_, err := clear(key, pid, identity)
		return err
	}
}

func (daemon *daemon) persistContainmentStopReceiptCallback(key state.RunKey) func(int, string, authority.StopReceipt) error {
	return func(pid int, identity string, receipt authority.StopReceipt) error {
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		if daemon.storeClosing || (daemon.store == nil && daemon.options.recordContainmentStopReceipt == nil) {
			return errDaemonStoreClosing
		}
		if daemon.options.recordContainmentStopReceipt != nil {
			if _, err := daemon.options.recordContainmentStopReceipt(key, pid, identity, receipt); err != nil {
				return err
			}
		} else if _, err := daemon.store.RecordContainmentStopReceipt(key, pid, identity, receipt); err != nil {
			return err
		}
		if err := daemon.verifyDurableContainmentStopReceipt(key, pid, identity, receipt); err != nil {
			return err
		}
		return daemon.observeContainmentStop(key, pid, identity, receipt)
	}
}

// observeContainmentStop is a test-only external witness boundary. Callers
// invoke it only after the exact receipt mutation has returned successfully;
// an observer error therefore keeps the containment authority unreleased so a
// later recovery pass can retry the same receipt.
func (daemon *daemon) observeContainmentStop(key state.RunKey, pid int, identity string, receipt authority.StopReceipt) error {
	if daemon.options.containmentStopObserver != nil {
		if err := daemon.options.containmentStopObserver(key, pid, identity, receipt); err != nil {
			return fmt.Errorf("%w: %v", errContainmentStopObserver, err)
		}
	}
	return waitForLinuxStopReceiptBarrier(key, pid, identity, receipt)
}

// assembleLinuxWitnessMarkerFile writes a witness marker document into a
// temporary file next to its destination and returns the temporary path; the
// caller publishes it with one rename or link. Witness markers are read by
// polling harnesses that parse the first document they can see, so the
// document must never be observable while it is incomplete.
func assembleLinuxWitnessMarkerFile(path string, contents []byte) (string, error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".partial-*")
	if err != nil {
		return "", err
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
	if writeErr != nil {
		_ = os.Remove(temporaryPath)
		return "", writeErr
	}
	return temporaryPath, nil
}

// publishLinuxStopReceiptBarrierMarker publishes the stop-receipt barrier
// document as a single visible state change: the witness harness polls the
// reached path, reads the first non-empty document, and decodes it as the
// barrier JSON, so a create-then-write sequence would expose an empty or
// truncated marker to that decode. The destination must still be absent, which
// keeps the create-with-O_EXCL contract the barrier validates before it starts.
func publishLinuxStopReceiptBarrierMarker(path string, contents []byte) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("witness marker %q already exists", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporaryPath, err := assembleLinuxWitnessMarkerFile(path, contents)
	if err != nil {
		return err
	}
	if renameErr := os.Rename(temporaryPath, path); renameErr != nil {
		_ = os.Remove(temporaryPath)
		return renameErr
	}
	return nil
}

func waitForLinuxStopReceiptBarrier(key state.RunKey, pid int, identity string, receipt authority.StopReceipt) error {
	if os.Getenv(linuxStopReceiptBarrierWitnessEnv) != "1" {
		return nil
	}
	reachedPath, hasReached := os.LookupEnv(linuxStopReceiptBarrierReachedEnv)
	releasePath, hasRelease := os.LookupEnv(linuxStopReceiptBarrierReleaseEnv)
	if !hasReached && !hasRelease {
		return nil
	}
	reachedPath = strings.TrimSpace(reachedPath)
	releasePath = strings.TrimSpace(releasePath)
	if !hasReached || !hasRelease || reachedPath == "" || releasePath == "" ||
		!filepath.IsAbs(reachedPath) || !filepath.IsAbs(releasePath) || filepath.Clean(reachedPath) == filepath.Clean(releasePath) {
		return errors.New("Linux stop-receipt barrier requires distinct absolute reached and release paths")
	}
	if err := assertLinuxStopReceiptBarrierPathAbsent(reachedPath); err != nil {
		return fmt.Errorf("validate stop-receipt reached marker: %w", err)
	}
	if err := assertLinuxStopReceiptBarrierPathAbsent(releasePath); err != nil {
		return fmt.Errorf("validate stop-receipt release marker: %w", err)
	}
	contents, err := json.Marshal(struct {
		RunID      string                `json:"run_id"`
		Generation int64                 `json:"generation"`
		PID        int                   `json:"pid"`
		Identity   string                `json:"identity"`
		Receipt    authority.StopReceipt `json:"receipt"`
	}{RunID: key.RunID, Generation: key.Generation, PID: pid, Identity: identity, Receipt: receipt})
	if err != nil {
		return fmt.Errorf("encode Linux stop-receipt barrier marker: %w", err)
	}
	if err := publishLinuxStopReceiptBarrierMarker(reachedPath, contents); err != nil {
		return fmt.Errorf("create or publish Linux stop-receipt barrier marker: %w", err)
	}
	if err := syncLinuxStopReceiptBarrierDirectory(filepath.Dir(reachedPath)); err != nil {
		return fmt.Errorf("persist Linux stop-receipt barrier marker: %w", err)
	}
	deadline := time.NewTimer(linuxStopReceiptBarrierTimeout)
	defer deadline.Stop()
	poll := time.NewTicker(linuxStopReceiptBarrierPoll)
	defer poll.Stop()
	for {
		if info, err := os.Lstat(releasePath); err == nil {
			if !info.Mode().IsRegular() {
				return errors.New("Linux stop-receipt release marker is not a regular file")
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("observe Linux stop-receipt release marker: %w", err)
		}
		select {
		case <-deadline.C:
			return fmt.Errorf("Linux stop-receipt barrier release marker did not appear within %s", linuxStopReceiptBarrierTimeout)
		case <-poll.C:
		}
	}
}

func assertLinuxStopReceiptBarrierPathAbsent(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		return errors.New("path already exists")
	}
	return err
}

func syncLinuxStopReceiptBarrierDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func (daemon *daemon) verifyDurableContainmentStopReceipt(key state.RunKey, pid int, identity string, receipt authority.StopReceipt) error {
	if daemon.store == nil {
		return errors.New("state store is unavailable")
	}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return fmt.Errorf("read back containment stop receipt: %w", err)
	}
	if journal.ContainmentHandoff != nil {
		if journal.ContainmentHandoff.TargetPID != pid || journal.ContainmentHandoff.TargetIdentity != identity {
			return errors.New("containment stop receipt readback target mismatch")
		}
		if journal.ContainmentHandoff.StopReceipt != nil && journal.ContainmentHandoff.StopReceipt.Equal(receipt) {
			if !receipt.ValidForHandoff(*journal.ContainmentHandoff) {
				return errors.New("containment stop receipt readback is invalid")
			}
			return nil
		}
	} else {
		if journal.PID != pid || journal.ProcessIdentity != identity {
			return errors.New("containment stop receipt readback target mismatch")
		}
	}
	if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.StopReceipt != nil && journal.ContainmentAuthority.StopReceipt.Equal(receipt) {
		if !receipt.ValidFor(*journal.ContainmentAuthority) {
			return errors.New("containment stop receipt readback is invalid")
		}
		return nil
	}
	return errors.New("containment stop receipt was not durably retained")
}

// Supervisor handoff callbacks are the only app-owned bridge from execution's
// pre-authority lifecycle into the durable Store. Keep the store lifetime read
// lock across each complete CAS so daemon shutdown cannot close the Store while
// a runner is still deciding whether a write outcome is known.
func (daemon *daemon) prepareSupervisorHandoffCallback(key state.RunKey) func(authority.SupervisorHandoff) error {
	return func(handoff authority.SupervisorHandoff) error {
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		prepare := daemon.options.prepareSupervisorHandoff
		if prepare == nil {
			if daemon.storeClosing || daemon.store == nil {
				return errDaemonStoreClosing
			}
			prepare = daemon.store.PrepareSupervisorHandoff
		} else if daemon.storeClosing {
			return errDaemonStoreClosing
		}
		_, err := prepare(key, handoff.Clone())
		return err
	}
}

func (daemon *daemon) bindSupervisorHandoffCallback(key state.RunKey) func(authority.SupervisorHandoff, int, string) error {
	return func(expected authority.SupervisorHandoff, pid int, identity string) error {
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		bind := daemon.options.bindSupervisorHandoff
		if bind == nil {
			if daemon.storeClosing || daemon.store == nil {
				return errDaemonStoreClosing
			}
			bind = daemon.store.BindSupervisorHandoff
		} else if daemon.storeClosing {
			return errDaemonStoreClosing
		}
		_, err := bind(key, expected.Clone(), pid, identity)
		return err
	}
}

func (daemon *daemon) commitSupervisorHandoffCallback(key state.RunKey) func(authority.SupervisorHandoff, time.Time) error {
	return func(expected authority.SupervisorHandoff, startedAt time.Time) error {
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		commit := daemon.options.commitSupervisorHandoff
		if commit == nil {
			if daemon.storeClosing || daemon.store == nil {
				return errDaemonStoreClosing
			}
			commit = daemon.store.CommitSupervisorHandoff
		} else if daemon.storeClosing {
			return errDaemonStoreClosing
		}
		committed, err := commit(key, expected.Clone(), startedAt)
		if err != nil {
			return err
		}
		if daemon.options.processObserver != nil {
			daemon.options.processObserver(key, committed.PID, committed.ProcessIdentity, daemon.now())
		}
		return nil
	}
}

func (daemon *daemon) recordSupervisorHandoffStopReceiptCallback(key state.RunKey) func(authority.SupervisorHandoff, authority.StopReceipt) error {
	return func(expected authority.SupervisorHandoff, receipt authority.StopReceipt) error {
		return daemon.persistSupervisorHandoffStopReceipt(key, expected, receipt, true)
	}
}

func (daemon *daemon) clearSupervisorHandoffCallback(key state.RunKey) func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error {
	return func(expected authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) error {
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		clear := daemon.options.clearSupervisorHandoff
		if clear == nil {
			if daemon.storeClosing || daemon.store == nil {
				return errDaemonStoreClosing
			}
			clear = daemon.store.ClearSupervisorHandoff
		} else if daemon.storeClosing {
			return errDaemonStoreClosing
		}
		_, err := clear(key, expected.Clone(), proof)
		return err
	}
}

func (daemon *daemon) clearSupervisorHandoffAfterAbortCallback(key state.RunKey) func(authority.SupervisorHandoff, authority.SupervisorHandoffAbortProof) error {
	return func(expected authority.SupervisorHandoff, proof authority.SupervisorHandoffAbortProof) error {
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		clear := daemon.options.clearSupervisorHandoffAfterAbort
		if clear == nil {
			if daemon.storeClosing || daemon.store == nil {
				return errDaemonStoreClosing
			}
			clear = daemon.store.ClearSupervisorHandoffAfterAbort
		} else if daemon.storeClosing {
			return errDaemonStoreClosing
		}
		_, err := clear(key, expected.Clone(), proof)
		return err
	}
}

func (daemon *daemon) clearPersistedProcessDetails(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
	if daemon.store != nil {
		journal, err := daemon.store.LoadJournal(key)
		if err == nil && hasLinuxV2ContainmentState(journal) {
			if !journal.HasProcessDetails() && journal.PID == 0 && journal.ProcessIdentity == "" && journal.StartedAt.IsZero() &&
				journal.ContainmentCgroupReleaseProof != nil && journal.ContainmentCgroupReleaseProof.TargetPID == pid &&
				journal.ContainmentCgroupReleaseProof.TargetIdentity == identity {
				daemon.forgetRecoveredProcessStop(key)
				return journal, nil
			}
			return state.RunJournal{}, errors.New("Linux v2 process details require the exact opaque receipt-backed release callback")
		} else if err != nil && !state.IsNotFound(err) {
			return state.RunJournal{}, err
		}
	}
	if err := daemon.clearContainmentUnprovenCallback(key)(pid, identity); err != nil {
		return state.RunJournal{}, err
	}
	clear := daemon.options.clearProcessDetails
	if clear == nil {
		clear = daemon.store.ClearProcessDetails
	}
	journal, err := clear(key, pid, identity)
	if err == nil {
		daemon.forgetRecoveredProcessStop(key)
	}
	return journal, err
}

func hasLinuxV2ContainmentState(journal state.RunJournal) bool {
	return journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil ||
		journal.ContainmentCgroupPreGateReleaseProof != nil ||
		journal.ContainmentAuthority != nil && journal.ContainmentAuthority.Version == authority.SupervisorVersionV2 ||
		journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Version == authority.SupervisorHandoffVersionV2
}

func hasPendingLinuxV2Containment(journal state.RunJournal) bool {
	return journal.ContainmentAuthority != nil && journal.ContainmentAuthority.Version == authority.SupervisorVersionV2 ||
		journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Version == authority.SupervisorHandoffVersionV2 ||
		journal.ContainmentCgroup != nil && journal.ContainmentCgroup.AbortProof == nil
}
