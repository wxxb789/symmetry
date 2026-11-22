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

// Takes the store lifetime read lock and returns it as release for the caller to defer: the lock
// must span the complete CAS, and the lazy store fallback binds inside the guard.
func selectStoreWrite[Fn any](daemon *daemon, fromStore func(journalStore) Fn) (Fn, func(), error) {
	daemon.storeLifetimeMu.RLock()
	release := daemon.storeLifetimeMu.RUnlock
	if !daemon.storeClosing && daemon.store != nil {
		return fromStore(daemon.store), release, nil
	}
	release()
	var refused Fn
	return refused, nil, errDaemonStoreClosing
}

func (daemon *daemon) persistProcessAuthorityCallback(key state.RunKey) func(int, string, *authority.Supervisor) error {
	return func(pid int, identity string, value *authority.Supervisor) error {
		// Write selection and the unconditional single retry live in persistProcessAuthority
		// because recovery shares them; the refusal predicate must match selectStoreWrite.
		daemon.storeLifetimeMu.RLock()
		defer daemon.storeLifetimeMu.RUnlock()
		if daemon.storeClosing || daemon.store == nil {
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

// Marker-only CAS boundary: a post-rename persistence error leaves the result unknown, and
// an exact readback only replays the same mutation under a fresh successful barrier.
func (daemon *daemon) persistProcessMarker(key state.RunKey, pid int, identity string, startedAt time.Time) error {
	daemon.storeLifetimeMu.RLock()
	defer daemon.storeLifetimeMu.RUnlock()
	if daemon.storeClosing || daemon.store == nil {
		return errDaemonStoreClosing
	}
	write := func() error {
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
		mark, release, err := selectStoreWrite(daemon, func(store journalStore) func(state.RunKey, int, string) (state.RunJournal, error) {
			return store.MarkContainmentUnproven
		})
		if err != nil {
			return err
		}
		defer release()
		_, err = mark(key, pid, identity)
		return err
	}
}

func (daemon *daemon) clearContainmentUnprovenCallback(key state.RunKey) func(int, string) error {
	return func(pid int, identity string) error {
		clear, release, err := selectStoreWrite(daemon, func(store journalStore) func(state.RunKey, int, string) (state.RunJournal, error) {
			return store.ClearContainmentUnproven
		})
		if err != nil {
			return err
		}
		defer release()
		_, err = clear(key, pid, identity)
		return err
	}
}

func (daemon *daemon) persistContainmentStopReceiptCallback(key state.RunKey) func(int, string, authority.StopReceipt) error {
	return func(pid int, identity string, receipt authority.StopReceipt) error {
		record, release, err := selectStoreWrite(daemon, func(store journalStore) func(state.RunKey, int, string, authority.StopReceipt) (state.RunJournal, error) {
			return store.RecordContainmentStopReceipt
		})
		if err != nil {
			return err
		}
		defer release()
		if _, err := record(key, pid, identity, receipt); err != nil {
			return err
		}
		if err := daemon.verifyDurableContainmentStopReceipt(key, pid, identity, receipt); err != nil {
			return err
		}
		return daemon.observeContainmentStop(key, pid, identity, receipt)
	}
}

// Test-only external witness boundary: callers invoke it only after the exact receipt
// mutation returned, so an observer error keeps the containment authority unreleased.
func (daemon *daemon) observeContainmentStop(key state.RunKey, pid int, identity string, receipt authority.StopReceipt) error {
	if daemon.options.containmentStopObserver != nil {
		if err := daemon.options.containmentStopObserver(key, pid, identity, receipt); err != nil {
			return fmt.Errorf("%w: %v", errContainmentStopObserver, err)
		}
	}
	return waitForLinuxStopReceiptBarrier(key, pid, identity, receipt)
}

// The caller publishes the temp file with one rename or link: polling harnesses decode
// the first document they can see, so it must never be observable incomplete.
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

// Publishes the barrier marker as one visible state change (destination still absent):
// a create-then-write sequence would expose an empty or truncated marker to the decode.
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

// The only app-owned bridge from execution's pre-authority lifecycle into the Store: keep
// the store lifetime read lock across each CAS, or shutdown can close it mid-decision.
func (daemon *daemon) prepareSupervisorHandoffCallback(key state.RunKey) func(authority.SupervisorHandoff) error {
	return func(handoff authority.SupervisorHandoff) error {
		prepare, release, err := selectStoreWrite(daemon, func(store journalStore) func(state.RunKey, authority.SupervisorHandoff) (state.RunJournal, error) {
			return store.PrepareSupervisorHandoff
		})
		if err != nil {
			return err
		}
		defer release()
		_, err = prepare(key, handoff.Clone())
		return err
	}
}

func (daemon *daemon) bindSupervisorHandoffCallback(key state.RunKey) func(authority.SupervisorHandoff, int, string) error {
	return func(expected authority.SupervisorHandoff, pid int, identity string) error {
		bind, release, err := selectStoreWrite(daemon, func(store journalStore) func(state.RunKey, authority.SupervisorHandoff, int, string) (state.RunJournal, error) {
			return store.BindSupervisorHandoff
		})
		if err != nil {
			return err
		}
		defer release()
		_, err = bind(key, expected.Clone(), pid, identity)
		return err
	}
}

func (daemon *daemon) commitSupervisorHandoffCallback(key state.RunKey) func(authority.SupervisorHandoff, time.Time) error {
	return func(expected authority.SupervisorHandoff, startedAt time.Time) error {
		commit, release, err := selectStoreWrite(daemon, func(store journalStore) func(state.RunKey, authority.SupervisorHandoff, time.Time) (state.RunJournal, error) {
			return store.CommitSupervisorHandoff
		})
		if err != nil {
			return err
		}
		defer release()
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
		clear, release, err := selectStoreWrite(daemon, func(store journalStore) func(state.RunKey, authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) (state.RunJournal, error) {
			return store.ClearSupervisorHandoff
		})
		if err != nil {
			return err
		}
		defer release()
		_, err = clear(key, expected.Clone(), proof)
		return err
	}
}

func (daemon *daemon) clearSupervisorHandoffAfterAbortCallback(key state.RunKey) func(authority.SupervisorHandoff, authority.SupervisorHandoffAbortProof) error {
	return func(expected authority.SupervisorHandoff, proof authority.SupervisorHandoffAbortProof) error {
		clear, release, err := selectStoreWrite(daemon, func(store journalStore) func(state.RunKey, authority.SupervisorHandoff, authority.SupervisorHandoffAbortProof) (state.RunJournal, error) {
			return store.ClearSupervisorHandoffAfterAbort
		})
		if err != nil {
			return err
		}
		defer release()
		_, err = clear(key, expected.Clone(), proof)
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
	journal, err := daemon.store.ClearProcessDetails(key, pid, identity)
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
