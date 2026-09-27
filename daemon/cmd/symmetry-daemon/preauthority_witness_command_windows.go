//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const preAuthorityWitnessArgument = "-preauthority-witness"

// The production binary owns this opt-in entrypoint so the supervisor helper
// launched by platform.AttachSuspendedProcessWithHandoff is the same binary,
// not a Go test executable or an in-process test double.
func init() {
	if len(os.Args) < 2 || os.Args[1] != preAuthorityWitnessArgument {
		return
	}
	err := runPreAuthorityWitness(os.Args[2:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "run pre-authority witness: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

type preAuthorityWitnessOptions struct {
	mode              string
	stateDir          string
	runID             string
	generation        int64
	sentinel          string
	readyFile         string
	resultFile        string
	crashAt           string
	wrongSecretMarker string
}

type preAuthorityWitnessReport struct {
	Mode               string `json:"mode"`
	Stage              string `json:"stage"`
	Status             string `json:"status"`
	RunID              string `json:"run_id"`
	Generation         int64  `json:"generation"`
	JobID              string `json:"job_id,omitempty"`
	PID                int    `json:"pid,omitempty"`
	HasProcessDetails  bool   `json:"has_process_details"`
	HasContainment     bool   `json:"has_containment"`
	HasPendingHandoff  bool   `json:"has_pending_handoff"`
	HasStopReceipt     bool   `json:"has_stop_receipt"`
	TargetPID          int    `json:"target_pid,omitempty"`
	TargetIdentity     string `json:"target_identity,omitempty"`
	SupervisorPID      int    `json:"supervisor_pid,omitempty"`
	SupervisorIdentity string `json:"supervisor_identity,omitempty"`
	Error              string `json:"error,omitempty"`
}

func runPreAuthorityWitness(arguments []string) error {
	options, err := parsePreAuthorityWitnessOptions(arguments)
	if err != nil {
		return err
	}
	key := state.RunKey{RunID: options.runID, Generation: options.generation}
	store, err := state.New(options.stateDir)
	if err != nil {
		return err
	}
	defer store.Close()

	switch options.mode {
	case "start":
		return runPreAuthorityWitnessStart(store, key, options)
	case "recover":
		return runPreAuthorityWitnessRecovery(store, key, options)
	default:
		return fmt.Errorf("unsupported witness mode %q", options.mode)
	}
}

func parsePreAuthorityWitnessOptions(arguments []string) (preAuthorityWitnessOptions, error) {
	options := preAuthorityWitnessOptions{mode: "start", generation: 1}
	flags := flag.NewFlagSet(preAuthorityWitnessArgument, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&options.mode, "mode", options.mode, "start or recover")
	flags.StringVar(&options.stateDir, "state-dir", "", "durable state directory")
	flags.StringVar(&options.runID, "run-id", "", "durable run identifier")
	flags.Int64Var(&options.generation, "generation", options.generation, "durable run generation")
	flags.StringVar(&options.sentinel, "sentinel", "", "suspended sentinel executable")
	flags.StringVar(&options.readyFile, "ready-file", "", "sentinel resume marker")
	flags.StringVar(&options.resultFile, "result-file", "", "machine-readable witness report")
	flags.StringVar(&options.crashAt, "crash-at", "", "hard-crash boundary")
	flags.StringVar(&options.wrongSecretMarker, "wrong-secret-marker", "", "opt-in wrong-secret first-client evidence marker")
	if err := flags.Parse(arguments); err != nil {
		return preAuthorityWitnessOptions{}, err
	}
	if options.stateDir == "" || options.runID == "" || options.generation <= 0 {
		return preAuthorityWitnessOptions{}, errors.New("state-dir, run-id, and positive generation are required")
	}
	if options.mode == "start" && (options.sentinel == "" || options.readyFile == "") {
		return preAuthorityWitnessOptions{}, errors.New("start mode requires sentinel and ready-file")
	}
	if options.mode != "start" && options.mode != "recover" {
		return preAuthorityWitnessOptions{}, fmt.Errorf("mode must be start or recover, got %q", options.mode)
	}
	return options, nil
}

func runPreAuthorityWitnessStart(store *state.Store, key state.RunKey, options preAuthorityWitnessOptions) error {
	journal, err := ensurePreAuthorityWitnessJournal(store, key, options.stateDir)
	if err != nil {
		return err
	}
	if journal.HasPendingContainment() || journal.HasProcessDetails() {
		return errors.New("witness journal already contains unresolved containment")
	}
	if options.wrongSecretMarker != "" {
		if err := os.MkdirAll(filepath.Dir(options.wrongSecretMarker), 0o700); err != nil {
			return fmt.Errorf("create wrong-secret witness marker directory: %w", err)
		}
		const wrongSecretMarkerEnv = "SYMMETRY_PREAUTHORITY_WRONG_SECRET_MARKER"
		const dropHelloResponseEnv = "SYMMETRY_PREAUTHORITY_DROP_FIRST_HELLO_RESPONSE"
		const helloRetryMarkerEnv = "SYMMETRY_PREAUTHORITY_HELLO_RETRY_MARKER"
		helloRetryMarker := options.wrongSecretMarker + ".hello-retry.json"
		previous, hadPrevious := os.LookupEnv(wrongSecretMarkerEnv)
		previousDrop, hadPreviousDrop := os.LookupEnv(dropHelloResponseEnv)
		previousRetryMarker, hadPreviousRetryMarker := os.LookupEnv(helloRetryMarkerEnv)
		if err := os.Setenv(wrongSecretMarkerEnv, options.wrongSecretMarker); err != nil {
			return fmt.Errorf("set wrong-secret witness marker environment: %w", err)
		}
		if err := os.Setenv(dropHelloResponseEnv, "1"); err != nil {
			return fmt.Errorf("set hello retry witness environment: %w", err)
		}
		if err := os.Setenv(helloRetryMarkerEnv, helloRetryMarker); err != nil {
			return fmt.Errorf("set hello retry witness marker environment: %w", err)
		}
		defer func() {
			if hadPrevious {
				_ = os.Setenv(wrongSecretMarkerEnv, previous)
			} else {
				_ = os.Unsetenv(wrongSecretMarkerEnv)
			}
			if hadPreviousDrop {
				_ = os.Setenv(dropHelloResponseEnv, previousDrop)
			} else {
				_ = os.Unsetenv(dropHelloResponseEnv)
			}
			if hadPreviousRetryMarker {
				_ = os.Setenv(helloRetryMarkerEnv, previousRetryMarker)
			} else {
				_ = os.Unsetenv(helloRetryMarkerEnv)
			}
		}()
	}

	process, err := platform.LaunchSuspended(platform.NativeLaunchSpec{
		Argv:     []string{options.sentinel, "-ready-file", options.readyFile},
		Headless: true,
	})
	if err != nil {
		return err
	}

	callbacks := platform.SupervisorHandoffCallbacks{
		Prepare: func(handoff authority.SupervisorHandoff) error {
			if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
				return err
			}
			journal, loadErr := store.LoadJournal(key)
			if loadErr != nil {
				return loadErr
			}
			writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "prepare", "reached", journal, nil))
			crashPreAuthorityWitness(options, "prepare", journal)
			return nil
		},
		Bind: func(expected authority.SupervisorHandoff, pid int, identity string) error {
			if _, err := store.BindSupervisorHandoff(key, expected, pid, identity); err != nil {
				return err
			}
			journal, loadErr := store.LoadJournal(key)
			if loadErr != nil {
				return loadErr
			}
			writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "bind", "reached", journal, nil))
			crashPreAuthorityWitness(options, "bind", journal)
			return nil
		},
	}

	containment, _, _, err := platform.AttachSuspendedProcessWithHandoff(context.Background(), process, callbacks)
	if err != nil {
		return err
	}
	if options.wrongSecretMarker != "" {
		if _, err := os.Stat(options.wrongSecretMarker); err != nil {
			return fmt.Errorf("wrong-secret first-client witness marker: %w", err)
		}
		if _, err := os.Stat(options.wrongSecretMarker + ".hello-retry.json"); err != nil {
			return fmt.Errorf("hello retry witness marker: %w", err)
		}
	}
	if containment == nil {
		return errors.New("durable containment attachment returned nil containment")
	}

	journal, err = store.LoadJournal(key)
	if err != nil {
		return err
	}
	if journal.ContainmentHandoff == nil {
		return errors.New("durable containment attachment did not leave a bound handoff")
	}
	bound := journal.ContainmentHandoff.Clone()
	committed, err := store.CommitSupervisorHandoff(key, bound, time.Now().UTC())
	if err != nil {
		return err
	}
	writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "commit", "reached", committed, nil))
	crashPreAuthorityWitness(options, "commit", committed)

	renewer, ok := containment.(platform.ContainmentLeaseRenewer)
	if !ok || !renewer.LeaseRenewalAvailable() {
		return errors.New("durable containment lease renewal is unavailable")
	}
	if err := renewer.RenewLease(2*time.Minute, 1); err != nil {
		return err
	}
	leaseExpiry := time.Now().UTC().Add(2 * time.Minute)
	if _, err := store.UpdateLeaseExpiry(key, leaseExpiry); err != nil {
		return err
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		return err
	}
	writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "lease", "reached", journal, nil))
	crashPreAuthorityWitness(options, "lease", journal)

	if err := process.Resume(); err != nil {
		return err
	}
	if err := waitForPreAuthorityWitnessFile(options.readyFile, 10*time.Second); err != nil {
		return err
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		return err
	}
	writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "resume", "reached", journal, nil))
	crashPreAuthorityWitness(options, "resume", journal)

	if err := containment.Close(); err != nil {
		return err
	}
	receiptProvider, ok := containment.(platform.ContainmentStopReceiptProvider)
	if !ok {
		return errors.New("durable containment stop receipt provider is unavailable")
	}
	receipt, ok := receiptProvider.ContainmentStopReceipt()
	if !ok || journal.ContainmentAuthority == nil || !receipt.ValidFor(*journal.ContainmentAuthority) {
		return errors.New("durable containment stop receipt is unavailable")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		return err
	}
	writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "stop", "reached", journal, nil))
	crashPreAuthorityWitness(options, "stop", journal)

	if _, err := store.RecordContainmentStopReceipt(key, journal.PID, journal.ProcessIdentity, receipt); err != nil {
		return err
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		return err
	}
	writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "receipt", "reached", journal, nil))
	crashPreAuthorityWitness(options, "receipt", journal)

	releaser, ok := containment.(platform.ContainmentStopReceiptReleaser)
	if !ok {
		return errors.New("durable containment stop receipt releaser is unavailable")
	}
	if err := releaser.ReleaseContainment(); err != nil {
		return err
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		return err
	}
	writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "release", "reached", journal, nil))
	crashPreAuthorityWitness(options, "release", journal)

	cleared, err := store.ClearProcessDetails(key, journal.PID, journal.ProcessIdentity)
	if err != nil {
		return err
	}
	clearReport := witnessReportFromJournal(options.mode, "clear", "reached", cleared, nil)
	setPreAuthorityWitnessJobEvidence(&clearReport, journal)
	writePreAuthorityWitnessReport(options, clearReport)
	// Keep the cleared journal as the state witness, but retain the exact
	// process binding needed to verify that clear did not leave a live target or
	// supervisor behind.
	crashPreAuthorityWitnessWithEvidence(options, "clear", cleared, &journal)
	if err := process.Close(); err != nil {
		return err
	}
	writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "complete", "completed", cleared, nil))
	return nil
}

func runPreAuthorityWitnessRecovery(store *state.Store, key state.RunKey, options preAuthorityWitnessOptions) error {
	journal, err := store.LoadJournal(key)
	if err != nil {
		return err
	}
	if journal.ContainmentHandoff != nil {
		handoff := journal.ContainmentHandoff.Clone()
		if handoff.SupervisorPID == 0 {
			proof, err := provePreAuthorityAbortWithRetry(handoff)
			if err != nil {
				return err
			}
			cleared, err := store.ClearSupervisorHandoffAfterAbort(key, handoff, proof)
			if err != nil {
				return err
			}
			writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "recover-abort", "recovered", cleared, nil))
			return nil
		}
		receipt, err := platform.RecoverPreparedSupervisor(handoff)
		if err != nil {
			// A crash after Bind but before the secret-bearing bootstrap is
			// allowed to leave no authenticated helper to recover. Only an exact
			// platform abort proof may clear that bound handoff in this case.
			proof, abortErr := provePreAuthorityAbortWithRetry(handoff)
			if abortErr != nil {
				return errors.Join(err, abortErr)
			}
			cleared, clearErr := store.ClearSupervisorHandoffAfterAbort(key, handoff, proof)
			if clearErr != nil {
				return errors.Join(err, clearErr)
			}
			writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "recover-abort", "recovered", cleared, nil))
			return nil
		}
		if _, err := store.RecordSupervisorHandoffStopReceipt(key, handoff, receipt); err != nil {
			return err
		}
		journal, err = store.LoadJournal(key)
		if err != nil {
			return err
		}
		if journal.ContainmentHandoff == nil {
			return errors.New("recovered handoff disappeared before release")
		}
		withReceipt := journal.ContainmentHandoff.Clone()
		proof, err := platform.ReleasePreparedSupervisor(withReceipt)
		if err != nil {
			return err
		}
		cleared, err := store.ClearSupervisorHandoff(key, withReceipt, proof)
		if err != nil {
			return err
		}
		writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "recover-release", "recovered", cleared, nil))
		return nil
	}
	if journal.ContainmentAuthority != nil {
		authorityValue := journal.ContainmentAuthority.Clone()
		receipt, err := platform.RecoverPersistedContainmentWithAuthority(journal.PID, journal.ProcessIdentity, &authorityValue)
		if err != nil {
			return err
		}
		if _, err := store.RecordContainmentStopReceipt(key, journal.PID, journal.ProcessIdentity, receipt); err != nil {
			return err
		}
		journal, err = store.LoadJournal(key)
		if err != nil {
			return err
		}
		if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil {
			return errors.New("recovered containment receipt is not durable")
		}
		if err := platform.ReleasePersistedContainmentWithAuthority(journal.PID, journal.ProcessIdentity, journal.ContainmentAuthority); err != nil {
			return err
		}
		cleared, err := store.ClearProcessDetails(key, journal.PID, journal.ProcessIdentity)
		if err != nil {
			return err
		}
		writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "recover-clear", "recovered", cleared, nil))
		return nil
	}
	writePreAuthorityWitnessReport(options, witnessReportFromJournal(options.mode, "recover", "already-clear", journal, nil))
	return nil
}

func provePreAuthorityAbortWithRetry(handoff authority.SupervisorHandoff) (authority.SupervisorHandoffAbortProof, error) {
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		proof, err := platform.ProvePreparedSupervisorAborted(handoff, func() error { return nil })
		if err == nil {
			return proof, nil
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("pre-authority abort proof timed out")
	}
	return authority.SupervisorHandoffAbortProof{}, fmt.Errorf("prove prepared supervisor abort: %w", lastErr)
}

func ensurePreAuthorityWitnessJournal(store *state.Store, key state.RunKey, stateDir string) (state.RunJournal, error) {
	journal, err := store.LoadJournal(key)
	if err == nil {
		return journal, nil
	}
	if !state.IsNotFound(err) {
		return state.RunJournal{}, err
	}
	now := time.Now().UTC()
	journal = state.RunJournal{
		RunID:               key.RunID,
		Generation:          key.Generation,
		RuntimeKey:          "preauthority-witness",
		RuntimeID:           "preauthority-witness-runtime",
		ClaimedRuntimeEpoch: 1,
		ClaimID:             "preauthority-witness-claim",
		LeaseToken:          "preauthority-witness-lease",
		LeaseExpiresAt:      now.Add(10 * time.Minute),
		LocalState:          "running",
		Work:                protocol.Work{Goal: "crash-safe process containment witness", AgentProfile: "sentinel", Workspace: "witness", Input: json.RawMessage(`{}`)},
		WorkspacePath:       filepath.Clean(stateDir),
		WorkspaceBindingKey: "preauthority-witness",
		LastEventSequence:   0,
	}
	if err := store.SaveJournal(journal); err != nil {
		return state.RunJournal{}, err
	}
	return journal, nil
}

func witnessReportFromJournal(mode, stage, status string, journal state.RunJournal, reportErr error) preAuthorityWitnessReport {
	report := preAuthorityWitnessReport{
		Mode:              mode,
		Stage:             stage,
		Status:            status,
		RunID:             journal.RunID,
		Generation:        journal.Generation,
		PID:               journal.PID,
		HasProcessDetails: journal.HasProcessDetails(),
		HasContainment:    journal.ContainmentAuthority != nil,
		HasPendingHandoff: journal.ContainmentHandoff != nil,
	}
	setPreAuthorityWitnessProcessEvidence(&report, journal)
	if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.StopReceipt != nil {
		report.HasStopReceipt = true
	}
	if reportErr != nil {
		report.Error = reportErr.Error()
	}
	return report
}

func writePreAuthorityWitnessReport(options preAuthorityWitnessOptions, report preAuthorityWitnessReport) {
	if options.resultFile == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(options.resultFile), 0o700); err != nil {
		return
	}
	contents, err := json.Marshal(report)
	if err != nil {
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(options.resultFile), ".preauthority-report-")
	if err != nil {
		return
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return
	}
	if err := temporary.Close(); err != nil {
		return
	}
	_ = os.Rename(temporaryPath, options.resultFile)
}

func crashPreAuthorityWitness(options preAuthorityWitnessOptions, stage string, journal state.RunJournal) {
	crashPreAuthorityWitnessWithEvidence(options, stage, journal, nil)
}

func crashPreAuthorityWitnessWithEvidence(options preAuthorityWitnessOptions, stage string, journal state.RunJournal, processEvidence *state.RunJournal) {
	if options.crashAt != stage {
		return
	}
	report := witnessReportFromJournal(options.mode, stage, "hard-crash", journal, nil)
	if processEvidence != nil {
		setPreAuthorityWitnessProcessEvidence(&report, *processEvidence)
	}
	writePreAuthorityWitnessReport(options, report)
	os.Exit(137)
}

func setPreAuthorityWitnessProcessEvidence(report *preAuthorityWitnessReport, journal state.RunJournal) {
	if report == nil {
		return
	}
	report.TargetPID = journal.PID
	report.TargetIdentity = journal.ProcessIdentity
	if journal.ContainmentAuthority != nil {
		report.TargetPID = journal.ContainmentAuthority.TargetPID
		report.TargetIdentity = journal.ContainmentAuthority.TargetIdentity
		report.SupervisorPID = journal.ContainmentAuthority.SupervisorPID
		report.SupervisorIdentity = journal.ContainmentAuthority.SupervisorIdentity
	} else if journal.ContainmentHandoff != nil {
		report.TargetPID = journal.ContainmentHandoff.TargetPID
		report.TargetIdentity = journal.ContainmentHandoff.TargetIdentity
		report.SupervisorPID = journal.ContainmentHandoff.SupervisorPID
		report.SupervisorIdentity = journal.ContainmentHandoff.SupervisorIdentity
	}
	setPreAuthorityWitnessJobEvidence(report, journal)
}

func setPreAuthorityWitnessJobEvidence(report *preAuthorityWitnessReport, journal state.RunJournal) {
	if report == nil {
		return
	}
	// JobID is a non-secret named Job identifier. Keep secret-bearing authority
	// fields out of the production witness report.
	if journal.ContainmentAuthority != nil {
		report.JobID = journal.ContainmentAuthority.JobID
	} else if journal.ContainmentHandoff != nil {
		report.JobID = journal.ContainmentHandoff.JobID
	}
}

func waitForPreAuthorityWitnessFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("sentinel did not publish %q within %s", path, timeout)
}
