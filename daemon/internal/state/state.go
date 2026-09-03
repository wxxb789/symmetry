// Package state persists machine-local daemon recovery state.
package state

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	maxStateFileBytes   = 1 << 20
	maxJournalFileBytes = 4 << 20
	identityFileName    = "identity.json"
	enrollmentFileName  = "enrollment.json"
	runsDirectoryName   = "runs"
	lockFileName        = ".symmetry-daemon.lock"
	journalFilePrefix   = "journal-"
	journalFileSuffix   = ".json"
	atomicTempPrefix    = ".symmetry-state-"
)

const (
	TerminalVerdictAccepted      = "accepted"
	TerminalVerdictOwnershipLost = "ownership_lost"
	TerminalVerdictGraceExpired  = "terminal_grace_expired"
)

// ErrStoreInUse indicates that another daemon process currently owns this
// state directory.
var ErrStoreInUse = errors.New("state directory is already in use")

var (
	applyDirectorySecurity = secureDirectory
	applyFileSecurity      = secureFile
)

// Store owns state rooted in one configured machine-local directory.
// Its methods are serialized so related read-modify-write operations cannot
// overwrite each other within a daemon process.
type Store struct {
	dir    string
	mu     sync.Mutex
	lock   *os.File
	closed bool
}

// MachineIdentity is the durable result of machine enrollment.
type MachineIdentity struct {
	MachineID    string `json:"machine_id"`
	MachineToken string `json:"machine_token"`
}

// EnrollmentIntent is the durable replay identity for first enrollment.
type EnrollmentIntent struct {
	MachineName    string `json:"machine_name"`
	MachineToken   string `json:"machine_token"`
	IdempotencyKey string `json:"idempotency_key"`
}

// RunKey uniquely identifies one fenced execution generation.
type RunKey struct {
	RunID      string
	Generation int64
}

// ClaimIntent contains the durable information that must be saved before a
// claim request can be sent. ClaimID is deliberately caller supplied so an
// uncertain HTTP result can be retried idempotently after a restart.
type ClaimIntent struct {
	Key                 RunKey
	RuntimeKey          string
	RuntimeID           string
	RuntimeEpoch        int64
	ClaimID             string
	LocalState          string
	Work                protocol.Work
	WorkspacePath       string
	WorkspaceBindingKey string
}

// RunJournal is the complete durable local recovery record for one run
// generation. It contains only opaque protocol state and machine-local process
// information; coding-agent and repository credentials never belong here.
type RunJournal struct {
	RunID                          string                            `json:"run_id"`
	Generation                     int64                             `json:"generation"`
	RuntimeKey                     string                            `json:"runtime_key"`
	RuntimeID                      string                            `json:"runtime_id"`
	ClaimedRuntimeEpoch            int64                             `json:"claimed_runtime_epoch"`
	ClaimID                        string                            `json:"claim_id"`
	LeaseToken                     string                            `json:"lease_token"`
	LeaseExpiresAt                 time.Time                         `json:"lease_expires_at"`
	LocalState                     string                            `json:"local_state"`
	TerminalPendingAt              time.Time                         `json:"terminal_pending_at,omitempty"`
	TerminalState                  string                            `json:"terminal_state,omitempty"`
	TerminalVerdict                string                            `json:"terminal_verdict,omitempty"`
	TerminalResolvedAt             time.Time                         `json:"terminal_resolved_at,omitempty"`
	Work                           protocol.Work                     `json:"work"`
	WorkspacePath                  string                            `json:"workspace_path"`
	WorkspaceRecoveryRequired      bool                              `json:"workspace_recovery_required,omitempty"`
	WorkspaceBindingKey            string                            `json:"workspace_binding_key"`
	PID                            int                               `json:"pid"`
	ProcessIdentity                string                            `json:"process_identity"`
	StartedAt                      time.Time                         `json:"started_at"`
	LastEventSequence              int64                             `json:"last_event_sequence"`
	PendingEvents                  []protocol.RunEvent               `json:"pending_events"`
	PendingTransitions             []protocol.StateTransitionRequest `json:"pending_transitions"`
	AttemptedTransitionIDs         []string                          `json:"attempted_transition_ids,omitempty"`
	PendingCommandAcknowledgements []protocol.CommandAcknowledgement `json:"pending_command_acknowledgements"`
	InputCommandIntent             *InputCommandIntent               `json:"input_command_intent,omitempty"`
}

// InputCommandIntent is the durable at-most-once record for one provide_input
// command in a waiting-for-input episode.
type InputCommandIntent struct {
	CommandID                string `json:"command_id"`
	PayloadDigest            string `json:"payload_digest"`
	RunningTransitionID      string `json:"running_transition_id"`
	AckID                    string `json:"ack_id"`
	Outcome                  string `json:"outcome,omitempty"`
	AcknowledgementDelivered bool   `json:"acknowledgement_delivered,omitempty"`
}

// Key returns the journal's durable identity.
func (journal RunJournal) Key() RunKey {
	return RunKey{RunID: journal.RunID, Generation: journal.Generation}
}

// Fence returns the fencing data currently held by the journal.
func (journal RunJournal) Fence() protocol.Fence {
	return protocol.Fence{
		RuntimeID:    journal.RuntimeID,
		RuntimeEpoch: journal.ClaimedRuntimeEpoch,
		Generation:   journal.Generation,
		ClaimID:      journal.ClaimID,
		LeaseToken:   journal.LeaseToken,
	}
}

// NotFoundError distinguishes absent local durable state from malformed state.
type NotFoundError struct {
	Resource string
}

func (err *NotFoundError) Error() string {
	return err.Resource + " not found"
}

// IsNotFound reports whether err represents an absent state record.
func IsNotFound(err error) bool {
	var target *NotFoundError
	return errors.As(err, &target)
}

// New opens a machine-local state root, creating private directories as needed.
func New(directory string) (*Store, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, errors.New("state directory must not be empty")
	}
	if err := ensurePrivateDirectory(directory); err != nil {
		return nil, fmt.Errorf("initialize state directory: %w", err)
	}
	lockPath := filepath.Join(directory, lockFileName)
	lock, err := acquireStoreLock(lockPath)
	if err != nil {
		return nil, err
	}
	if err := applyFileSecurity(lockPath); err != nil {
		_ = releaseStoreLock(lock)
		return nil, errors.New("secure state lock")
	}
	store := &Store{dir: directory, lock: lock}
	if err := ensurePrivateDirectory(store.runsDir()); err != nil {
		_ = releaseStoreLock(lock)
		return nil, errors.New("create run journal directory")
	}
	return store, nil
}

// Close releases the state directory's cross-process exclusive lock. It is
// idempotent and marks the store unusable for further reads or writes.
func (store *Store) Close() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return nil
	}
	store.closed = true
	if store.lock == nil {
		return nil
	}
	lock := store.lock
	store.lock = nil
	if err := releaseStoreLock(lock); err != nil {
		return errors.New("release state directory lock")
	}
	return nil
}

// NewDaemonInstanceID returns a newly generated RFC 4122 version 4 UUID for a
// single daemon process lifetime.
func NewDaemonInstanceID() (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(rand.Reader, value[:]); err != nil {
		return "", errors.New("generate daemon instance ID")
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

// NewMachineToken returns a 256-bit URL-safe opaque credential.
func NewMachineToken() (string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", errors.New("generate machine token")
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// LoadEnrollmentIntent returns the durable first-enrollment replay request.
func (store *Store) LoadEnrollmentIntent() (EnrollmentIntent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return EnrollmentIntent{}, err
	}
	var intent EnrollmentIntent
	if err := store.readJSON(store.enrollmentPath(), "enrollment intent", &intent); err != nil {
		return EnrollmentIntent{}, err
	}
	if err := validateEnrollmentIntent(intent); err != nil {
		return EnrollmentIntent{}, errors.New("invalid enrollment intent")
	}
	return intent, nil
}

// SaveEnrollmentIntent atomically persists the exact request used for retries.
func (store *Store) SaveEnrollmentIntent(intent EnrollmentIntent) error {
	if err := validateEnrollmentIntent(intent); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return err
	}
	return store.writeJSON(store.enrollmentPath(), intent, "enrollment intent")
}

// DeleteEnrollmentIntent removes a completed or superseded enrollment request.
func (store *Store) DeleteEnrollmentIntent() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return err
	}
	if err := os.Remove(store.enrollmentPath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.New("remove enrollment intent")
	}
	return syncDirectory(store.dir)
}

// LoadIdentity returns the enrolled machine credentials. An absent enrollment
// returns a typed NotFoundError.
func (store *Store) LoadIdentity() (MachineIdentity, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return MachineIdentity{}, err
	}

	var identity MachineIdentity
	if err := store.readJSON(store.identityPath(), "identity", &identity); err != nil {
		return MachineIdentity{}, err
	}
	if err := validateIdentity(identity); err != nil {
		return MachineIdentity{}, errors.New("invalid identity record")
	}
	return identity, nil
}

// SaveIdentity atomically replaces the enrolled machine credentials.
func (store *Store) SaveIdentity(identity MachineIdentity) error {
	if err := validateIdentity(identity); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return err
	}
	return store.writeJSON(store.identityPath(), identity, "identity")
}

// SaveClaimIntent creates a run journal before the claim HTTP request. If the
// same intent is already present, it is returned unchanged for idempotent retry.
func (store *Store) SaveClaimIntent(intent ClaimIntent) (RunJournal, error) {
	if err := validateKey(intent.Key); err != nil {
		return RunJournal{}, err
	}
	if !validRequiredString(intent.WorkspaceBindingKey, 4096) {
		return RunJournal{}, errors.New("workspace binding key is invalid")
	}
	journal := RunJournal{
		RunID:               intent.Key.RunID,
		Generation:          intent.Key.Generation,
		RuntimeKey:          intent.RuntimeKey,
		RuntimeID:           intent.RuntimeID,
		ClaimedRuntimeEpoch: intent.RuntimeEpoch,
		ClaimID:             intent.ClaimID,
		LocalState:          intent.LocalState,
		Work:                intent.Work,
		WorkspacePath:       intent.WorkspacePath,
		WorkspaceBindingKey: intent.WorkspaceBindingKey,
	}
	if journal.LocalState == "" {
		journal.LocalState = "claiming"
	}
	if err := validateJournal(journal); err != nil {
		return RunJournal{}, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return RunJournal{}, err
	}
	existing, err := store.loadJournalLocked(intent.Key)
	if err == nil {
		if existing.ClaimID != intent.ClaimID || existing.RuntimeID != intent.RuntimeID || existing.ClaimedRuntimeEpoch != intent.RuntimeEpoch {
			return RunJournal{}, errors.New("claim intent conflicts with existing journal")
		}
		return existing, nil
	}
	if !IsNotFound(err) {
		return RunJournal{}, err
	}
	if err := store.saveJournalLocked(journal); err != nil {
		return RunJournal{}, err
	}
	return journal, nil
}

// SaveClaimGrant persists a validated control-plane claim response before a
// worker process can be launched.
func (store *Store) SaveClaimGrant(key RunKey, grant protocol.ClaimResponse) (RunJournal, error) {
	if err := validateKey(key); err != nil {
		return RunJournal{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return RunJournal{}, err
	}
	journal, err := store.loadJournalLocked(key)
	if err != nil {
		return RunJournal{}, err
	}
	if grant.RunID != key.RunID || grant.Generation != key.Generation || grant.ClaimID != journal.ClaimID ||
		strings.TrimSpace(grant.LeaseToken) == "" || grant.LeaseExpiresAt.IsZero() {
		return RunJournal{}, errors.New("claim grant does not match journal")
	}
	journal.LeaseToken = grant.LeaseToken
	journal.LeaseExpiresAt = grant.LeaseExpiresAt
	journal.Work = grant.Work
	journal.LocalState = "claimed"
	if err := store.saveJournalLocked(journal); err != nil {
		return RunJournal{}, err
	}
	return journal, nil
}

// LoadJournal returns exactly one run generation journal.
func (store *Store) LoadJournal(key RunKey) (RunJournal, error) {
	if err := validateKey(key); err != nil {
		return RunJournal{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return RunJournal{}, err
	}
	return store.loadJournalLocked(key)
}

// SaveJournal atomically replaces a complete run journal. It is intended for
// recovery imports and carefully coordinated callers; normal callers should
// prefer the mutation helpers below.
func (store *Store) SaveJournal(journal RunJournal) error {
	if err := validateJournal(journal); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return err
	}
	return store.saveJournalLocked(journal)
}

// MarkWorkspaceRecoveryRequired records that workspace preparation may have
// begun, even when its resolved path has not yet been persisted.
func (store *Store) MarkWorkspaceRecoveryRequired(key RunKey) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.WorkspaceRecoveryRequired = true
		return nil
	})
}

// SetWorkspacePath atomically records a prepared workspace path without
// overwriting concurrently queued terminal delivery state.
func (store *Store) SetWorkspacePath(key RunKey, path string) (RunJournal, error) {
	if path == "" || len(path) > 32768 {
		return RunJournal{}, errors.New("workspace path is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.WorkspacePath = path
		return nil
	})
}

// DeleteJournal removes only the requested run generation journal.
func (store *Store) DeleteJournal(key RunKey) error {
	if err := validateKey(key); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return err
	}
	if err := os.Remove(store.journalPath(key)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &NotFoundError{Resource: "run journal"}
		}
		return errors.New("delete run journal")
	}
	return syncDirectory(store.runsDir())
}

// ListJournals loads every authoritative journal. It cleans only temporary
// files created by this package and fails closed on a corrupt journal.
func (store *Store) ListJournals() ([]RunJournal, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(store.runsDir())
	if err != nil {
		return nil, errors.New("list run journal directory")
	}
	if err := store.removeOwnedTempsLocked(entries); err != nil {
		return nil, err
	}
	result := make([]RunJournal, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isJournalFile(entry.Name()) {
			continue
		}
		var journal RunJournal
		if err := store.readJSONWithLimit(filepath.Join(store.runsDir(), entry.Name()), "run journal", &journal, maxJournalFileBytes); err != nil {
			return nil, err
		}
		if err := validateJournal(journal); err != nil {
			return nil, errors.New("invalid run journal")
		}
		if filepath.Base(store.journalPath(journal.Key())) != entry.Name() {
			return nil, errors.New("run journal file does not match its key")
		}
		result = append(result, journal)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].RunID == result[right].RunID {
			return result[left].Generation < result[right].Generation
		}
		return result[left].RunID < result[right].RunID
	})
	return result, nil
}

// SetLocalState persists a local lifecycle state transition.
func (store *Store) SetLocalState(key RunKey, localState string) (RunJournal, error) {
	if strings.TrimSpace(localState) == "" || len(localState) > 256 {
		return RunJournal{}, errors.New("local state is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.LocalState = localState
		return nil
	})
}

// SetProcessDetails persists an execution process identity that recovery code
// can verify before acting on a retained PID.
func (store *Store) SetProcessDetails(key RunKey, pid int, identity string, startedAt time.Time) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) || startedAt.IsZero() {
		return RunJournal{}, errors.New("process details are invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		journal.PID = pid
		journal.ProcessIdentity = identity
		journal.StartedAt = startedAt
		return nil
	})
}

// UpdateLeaseExpiry stores a successful lease renewal.
func (store *Store) UpdateLeaseExpiry(key RunKey, expiry time.Time) (RunJournal, error) {
	if expiry.IsZero() {
		return RunJournal{}, errors.New("lease expiry is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LeaseToken == "" {
			return errors.New("journal has no claim grant")
		}
		journal.LeaseExpiresAt = expiry
		return nil
	})
}

// AdvanceLeaseExpiry persists expiry only when it extends the current lease.
func (store *Store) AdvanceLeaseExpiry(key RunKey, expiry time.Time) (RunJournal, error) {
	if expiry.IsZero() {
		return RunJournal{}, errors.New("lease expiry is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LeaseToken == "" {
			return errors.New("journal has no claim grant")
		}
		if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" {
			return nil
		}
		if expiry.After(journal.LeaseExpiresAt) {
			journal.LeaseExpiresAt = expiry
		}
		return nil
	})
}

// QueueEvent durably enqueues an idempotent event before its HTTP request.
func (store *Store) QueueEvent(key RunKey, event protocol.RunEvent) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		return appendEvent(journal, event, "event is invalid")
	})
}

// QueueWaitingForInput atomically records a waiting event and the associated
// lifecycle transition. Repeated waiting records refresh an undelivered
// transition's payload without creating a second transition.
func (store *Store) QueueWaitingForInput(key RunKey, event protocol.RunEvent, transition protocol.StateTransitionRequest) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		event.Sequence = journal.LastEventSequence + 1
		if event.Kind != "waiting_for_input" {
			return errors.New("waiting event is invalid")
		}
		if journal.LocalState == "terminal_pending" || hasPendingTerminalTransition(journal.PendingTransitions) {
			return appendEvent(journal, event, "waiting event is invalid")
		}
		prepared, err := prepareTransition(journal, transition)
		if err != nil {
			return err
		}
		if prepared.State != "waiting_for_input" {
			return errors.New("waiting transition state is invalid")
		}
		if err := appendEvent(journal, event, "waiting event is invalid"); err != nil {
			return err
		}
		if len(journal.PendingTransitions) == 0 {
			if journal.LocalState != "waiting_for_input" {
				journal.PendingTransitions = append(journal.PendingTransitions, prepared)
			}
		} else if last := len(journal.PendingTransitions) - 1; journal.PendingTransitions[last].State == "waiting_for_input" {
			if !hasAttemptedTransition(journal.AttemptedTransitionIDs, journal.PendingTransitions[last].TransitionID) {
				journal.PendingTransitions[last].Payload = prepared.Payload
			}
		} else {
			journal.PendingTransitions = append(journal.PendingTransitions, prepared)
		}
		journal.LocalState = "waiting_for_input"
		return nil
	})
}

func appendEvent(journal *RunJournal, event protocol.RunEvent, invalidMessage string) error {
	if !journal.hasClaimGrant() {
		return errors.New("journal has no claim grant")
	}
	if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.Kind) == "" || event.Sequence != journal.LastEventSequence+1 || event.OccurredAt.IsZero() || !validRawMessage(event.Payload) {
		return errors.New(invalidMessage)
	}
	journal.PendingEvents = append(journal.PendingEvents, event)
	journal.LastEventSequence = event.Sequence
	return nil
}

// MarkEventsDelivered removes successfully appended events by their stable IDs.
func (store *Store) MarkEventsDelivered(key RunKey, eventIDs []string) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.PendingEvents = removeEvents(journal.PendingEvents, eventIDs)
		return nil
	})
}

// QueueTransition durably enqueues a fenced transition before its HTTP request.
func (store *Store) QueueTransition(key RunKey, transition protocol.StateTransitionRequest) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if isTerminalTransitionState(transition.State) {
			return errors.New("terminal transition requires terminal queue")
		}
		return queueTransition(journal, transition)
	})
}

// QueueRunningTransition atomically queues a running transition and records
// the corresponding local lifecycle state.
func (store *Store) QueueRunningTransition(key RunKey, transition protocol.StateTransitionRequest) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		prepared, err := prepareTransition(journal, transition)
		if err != nil {
			return err
		}
		if prepared.State != "running" {
			return errors.New("running transition state is invalid")
		}
		if journal.LocalState == "terminal_pending" || hasPendingTerminalTransition(journal.PendingTransitions) {
			return errors.New("running transition follows terminal transition")
		}
		journal.PendingTransitions = append(journal.PendingTransitions, prepared)
		journal.LocalState = "running"
		return nil
	})
}

// PrepareProvideInput durably records a provide_input command before the
// process receives stdin. It reports whether this call created a new intent.
func (store *Store) PrepareProvideInput(key RunKey, intent InputCommandIntent) (RunJournal, bool, error) {
	if !validInputCommandIntent(intent) || intent.Outcome != "" || intent.AcknowledgementDelivered {
		return RunJournal{}, false, errors.New("input command intent is invalid")
	}
	created := false
	journal, err := store.mutateJournal(key, func(journal *RunJournal) error {
		if current := journal.InputCommandIntent; current != nil {
			if current.CommandID == intent.CommandID && current.PayloadDigest == intent.PayloadDigest {
				return nil
			}
			if !current.AcknowledgementDelivered || journal.LocalState != "waiting_for_input" {
				return errors.New("input command conflicts with journal")
			}
		}
		if journal.LocalState != "waiting_for_input" {
			return errors.New("journal is not waiting for input")
		}
		copy := intent
		journal.InputCommandIntent = &copy
		created = true
		return nil
	})
	return journal, created, err
}

// CompleteProvideInput records a previously prepared input outcome and queues
// the required control-plane receipt in the same durable mutation.
func (store *Store) CompleteProvideInput(key RunKey, commandID, payloadDigest, outcome string) (RunJournal, error) {
	if !validInputCommandOutcome(outcome) {
		return RunJournal{}, errors.New("input command outcome is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		intent := journal.InputCommandIntent
		if intent == nil || intent.CommandID != commandID || intent.PayloadDigest != payloadDigest {
			return errors.New("input command intent does not match journal")
		}
		if intent.Outcome != "" && intent.Outcome != outcome {
			return nil
		}
		if intent.Outcome == "" {
			intent.Outcome = outcome
		}
		if intent.AcknowledgementDelivered {
			return nil
		}
		if outcome == "applied" && journal.LocalState == "waiting_for_input" {
			prepared, err := prepareTransition(journal, protocol.StateTransitionRequest{TransitionID: intent.RunningTransitionID, State: "running", Payload: json.RawMessage(`{}`)})
			if err != nil {
				return err
			}
			if !hasTransition(journal.PendingTransitions, prepared.TransitionID) {
				journal.PendingTransitions = append(journal.PendingTransitions, prepared)
			}
			journal.LocalState = "running"
		}
		return queueCommandAcknowledgement(journal, protocol.CommandAcknowledgement{RunID: journal.RunID, CommandID: intent.CommandID, Outcome: intent.Outcome, AckID: intent.AckID})
	})
}

// FailUnresolvedProvideInput records conservative recovery for an input that
// may have reached stdin before the daemon stopped.
func (store *Store) FailUnresolvedProvideInput(key RunKey, transition protocol.StateTransitionRequest, pendingAt time.Time) (RunJournal, error) {
	if pendingAt.IsZero() {
		return RunJournal{}, errors.New("terminal pending time is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.InputCommandIntent == nil || journal.InputCommandIntent.Outcome != "" {
			return nil
		}
		if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" {
			return settleUnresolvedInputCommand(journal)
		}
		return queueTerminalTransition(journal, transition, pendingAt)
	})
}

// QueueTerminalTransition atomically makes a terminal transition durable and
// marks the local process terminal-pending before any HTTP request is sent.
func (store *Store) QueueTerminalTransition(key RunKey, transition protocol.StateTransitionRequest) (RunJournal, error) {
	return store.QueueTerminalTransitionAt(key, transition, time.Now().UTC())
}

// QueueTerminalTransitionAt atomically makes a terminal transition durable,
// records the first terminal-pending time, and marks the local process
// terminal-pending before any HTTP request is sent.
func (store *Store) QueueTerminalTransitionAt(key RunKey, transition protocol.StateTransitionRequest, pendingAt time.Time) (RunJournal, error) {
	if pendingAt.IsZero() {
		return RunJournal{}, errors.New("terminal pending time is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		return queueTerminalTransition(journal, transition, pendingAt)
	})
}

// QueueCancelledTransitionAndAcknowledgementAt atomically records the
// authoritative cancellation terminal transition and its command receipt.
func (store *Store) QueueCancelledTransitionAndAcknowledgementAt(key RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (RunJournal, error) {
	if pendingAt.IsZero() {
		return RunJournal{}, errors.New("terminal pending time is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := queueTerminalTransition(journal, transition, pendingAt); err != nil {
			return err
		}
		if journal.TerminalState != "cancelled" {
			return errors.New("cancelled terminal transition state is invalid")
		}
		return queueCommandAcknowledgement(journal, acknowledgement)
	})
}

// ResolveTerminal durably records a terminal control-plane verdict. Repeating
// the same verdict is idempotent; conflicting terminal outcomes are rejected.
func (store *Store) ResolveTerminal(key RunKey, verdict string, resolvedAt time.Time) (RunJournal, error) {
	if !validTerminalVerdict(verdict) || resolvedAt.IsZero() {
		return RunJournal{}, errors.New("terminal verdict is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LocalState != "terminal_pending" {
			return errors.New("journal is not terminal pending")
		}
		if journal.TerminalVerdict != "" {
			if journal.TerminalVerdict != verdict {
				return errors.New("terminal verdict conflicts with journal")
			}
			return nil
		}
		if journal.TerminalPendingAt.IsZero() || !isTerminalTransitionState(journal.TerminalState) {
			return errors.New("journal terminal state is invalid")
		}
		journal.TerminalVerdict = verdict
		journal.TerminalResolvedAt = resolvedAt
		return nil
	})
}

// ResolveTerminalForCleanup atomically records a conclusive terminal rejection,
// retires unreachable delivery state, and makes local cleanup eligible.
func (store *Store) ResolveTerminalForCleanup(key RunKey, verdict string, resolvedAt time.Time) (RunJournal, error) {
	if !validConclusiveTerminalVerdict(verdict) || resolvedAt.IsZero() {
		return RunJournal{}, errors.New("terminal cleanup verdict is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LocalState == "cleanup_pending" {
			if journal.TerminalVerdict != verdict {
				return errors.New("terminal verdict conflicts with journal")
			}
			return nil
		}
		if journal.LocalState != "terminal_pending" {
			return errors.New("journal is not terminal pending")
		}
		if journal.TerminalVerdict != "" && journal.TerminalVerdict != verdict {
			return errors.New("terminal verdict conflicts with journal")
		}
		if journal.TerminalPendingAt.IsZero() || !isTerminalTransitionState(journal.TerminalState) {
			return errors.New("journal terminal state is invalid")
		}
		if journal.TerminalVerdict == "" {
			journal.TerminalVerdict = verdict
			journal.TerminalResolvedAt = resolvedAt
		}
		journal.PendingEvents = nil
		journal.PendingTransitions = nil
		journal.AttemptedTransitionIDs = nil
		journal.PendingCommandAcknowledgements = nil
		journal.InputCommandIntent = nil
		journal.LocalState = "cleanup_pending"
		return nil
	})
}

// EnterCleanupPending records that terminal delivery has reached a conclusive
// outcome and local workspace cleanup is the only remaining daemon action.
// The accepted path requires every transition and command acknowledgement to
// be delivered first, while conclusive rejections retire unreachable delivery
// work.
func (store *Store) EnterCleanupPending(key RunKey) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LocalState == "cleanup_pending" {
			return nil
		}
		if journal.LocalState != "terminal_pending" || !validTerminalVerdict(journal.TerminalVerdict) {
			return errors.New("journal is not cleanup eligible")
		}
		if journal.InputCommandIntent != nil && (journal.InputCommandIntent.Outcome == "" || !journal.InputCommandIntent.AcknowledgementDelivered) {
			return errors.New("input command receipt is not delivered")
		}
		if journal.TerminalVerdict == TerminalVerdictAccepted {
			if len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0 {
				return errors.New("accepted terminal delivery is incomplete")
			}
		} else {
			journal.PendingTransitions = nil
			journal.PendingCommandAcknowledgements = nil
		}
		journal.PendingEvents = nil
		journal.AttemptedTransitionIDs = nil
		journal.LocalState = "cleanup_pending"
		return nil
	})
}

func setTerminalPending(journal *RunJournal, pendingAt time.Time, terminalState string) {
	journal.LocalState = "terminal_pending"
	if journal.TerminalPendingAt.IsZero() {
		journal.TerminalPendingAt = pendingAt
	}
	journal.TerminalState = terminalState
}

// MarkTransitionsDelivered removes successfully applied transitions by ID.
func (store *Store) MarkTransitionsDelivered(key RunKey, transitionIDs []string) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.PendingTransitions = removeTransitions(journal.PendingTransitions, transitionIDs)
		journal.AttemptedTransitionIDs = retainAttemptedTransitions(journal.AttemptedTransitionIDs, journal.PendingTransitions)
		return nil
	})
}

// MarkTransitionAttempted durably freezes a pending transition before its HTTP
// request is sent. Repeating the marker for the same pending transition is a
// no-op.
func (store *Store) MarkTransitionAttempted(key RunKey, transitionID string) (RunJournal, error) {
	if !validRequiredString(transitionID, 4096) {
		return RunJournal{}, errors.New("transition ID is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.HasPendingTransition(transitionID) {
			return errors.New("transition is not pending")
		}
		if !hasAttemptedTransition(journal.AttemptedTransitionIDs, transitionID) {
			journal.AttemptedTransitionIDs = append(journal.AttemptedTransitionIDs, transitionID)
		}
		return nil
	})
}

// QueueCommandAcknowledgement durably queues an acknowledgement before its HTTP
// request. A zero fence is populated from the current journal.
func (store *Store) QueueCommandAcknowledgement(key RunKey, acknowledgement protocol.CommandAcknowledgement) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		return queueCommandAcknowledgement(journal, acknowledgement)
	})
}

// MarkCommandAcknowledgementsDelivered removes acknowledgements confirmed by the
// control plane, keyed by their caller-generated ack IDs.
func (store *Store) MarkCommandAcknowledgementsDelivered(key RunKey, acknowledgementIDs []string) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.PendingCommandAcknowledgements = removeAcknowledgements(journal.PendingCommandAcknowledgements, acknowledgementIDs)
		if journal.InputCommandIntent != nil && containsString(acknowledgementIDs, journal.InputCommandIntent.AckID) {
			if journal.InputCommandIntent.Outcome == "" {
				return errors.New("input command acknowledgement is unresolved")
			}
			journal.InputCommandIntent.AcknowledgementDelivered = true
		}
		return nil
	})
}

func (store *Store) mutateJournal(key RunKey, mutate func(*RunJournal) error) (RunJournal, error) {
	if err := validateKey(key); err != nil {
		return RunJournal{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return RunJournal{}, err
	}
	journal, err := store.loadJournalLocked(key)
	if err != nil {
		return RunJournal{}, err
	}
	if err := mutate(&journal); err != nil {
		return RunJournal{}, err
	}
	if err := store.saveJournalLocked(journal); err != nil {
		return RunJournal{}, err
	}
	return journal, nil
}

func queueTransition(journal *RunJournal, transition protocol.StateTransitionRequest) error {
	prepared, err := prepareTransition(journal, transition)
	if err != nil {
		return err
	}
	journal.PendingTransitions = append(journal.PendingTransitions, prepared)
	return nil
}

func prepareTransition(journal *RunJournal, transition protocol.StateTransitionRequest) (protocol.StateTransitionRequest, error) {
	if !journal.hasClaimGrant() {
		return protocol.StateTransitionRequest{}, errors.New("journal has no claim grant")
	}
	if strings.TrimSpace(transition.TransitionID) == "" || strings.TrimSpace(transition.State) == "" || !validRawMessage(transition.Payload) {
		return protocol.StateTransitionRequest{}, errors.New("state transition is invalid")
	}
	if isZeroFence(transition.Fence) {
		transition.Fence = journal.Fence()
	}
	if !sameFence(transition.Fence, journal.Fence()) {
		return protocol.StateTransitionRequest{}, errors.New("state transition fence does not match journal")
	}
	return transition, nil
}

func queueTerminalTransition(journal *RunJournal, transition protocol.StateTransitionRequest, pendingAt time.Time) error {
	if err := settleUnresolvedInputCommand(journal); err != nil {
		return err
	}
	if journal.TerminalVerdict != "" {
		return errors.New("terminal verdict is already recorded")
	}
	prepared, err := prepareTransition(journal, transition)
	if err != nil {
		return err
	}
	if !isTerminalTransitionState(prepared.State) {
		return errors.New("terminal transition state is invalid")
	}
	if prepared.State == "cancelled" {
		journal.PendingTransitions = []protocol.StateTransitionRequest{prepared}
		journal.AttemptedTransitionIDs = retainAttemptedTransitions(journal.AttemptedTransitionIDs, journal.PendingTransitions)
		setTerminalPending(journal, pendingAt, prepared.State)
		return nil
	}
	if hasPendingTerminalTransition(journal.PendingTransitions) {
		setTerminalPending(journal, pendingAt, pendingTerminalState(journal.PendingTransitions))
		return nil
	}
	journal.PendingTransitions = append(journal.PendingTransitions, prepared)
	setTerminalPending(journal, pendingAt, prepared.State)
	return nil
}

func settleUnresolvedInputCommand(journal *RunJournal) error {
	intent := journal.InputCommandIntent
	if intent == nil || intent.Outcome != "" {
		return nil
	}
	intent.Outcome = "failed"
	return queueCommandAcknowledgement(journal, protocol.CommandAcknowledgement{RunID: journal.RunID, CommandID: intent.CommandID, Outcome: intent.Outcome, AckID: intent.AckID})
}

func queueCommandAcknowledgement(journal *RunJournal, acknowledgement protocol.CommandAcknowledgement) error {
	if !journal.hasClaimGrant() {
		return errors.New("journal has no claim grant")
	}
	if journal.LocalState == "cleanup_pending" || (journal.LocalState == "terminal_pending" && (journal.TerminalVerdict == TerminalVerdictOwnershipLost || journal.TerminalVerdict == TerminalVerdictGraceExpired)) {
		return errors.New("command acknowledgement is no longer deliverable")
	}
	if acknowledgement.RunID == "" {
		acknowledgement.RunID = journal.RunID
	}
	if acknowledgement.RunID != journal.RunID || strings.TrimSpace(acknowledgement.CommandID) == "" || strings.TrimSpace(acknowledgement.Outcome) == "" || strings.TrimSpace(acknowledgement.AckID) == "" {
		return errors.New("command acknowledgement is invalid")
	}
	if isZeroFence(acknowledgement.Fence) {
		acknowledgement.Fence = journal.Fence()
	}
	if !sameFence(acknowledgement.Fence, journal.Fence()) {
		return errors.New("command acknowledgement fence does not match journal")
	}
	for _, pending := range journal.PendingCommandAcknowledgements {
		if pending.CommandID != acknowledgement.CommandID {
			continue
		}
		if pending.Outcome == acknowledgement.Outcome {
			return nil
		}
		return errors.New("command acknowledgement outcome conflicts with pending acknowledgement")
	}
	journal.PendingCommandAcknowledgements = append(journal.PendingCommandAcknowledgements, acknowledgement)
	return nil
}

func hasTransition(transitions []protocol.StateTransitionRequest, transitionID string) bool {
	for _, transition := range transitions {
		if transition.TransitionID == transitionID {
			return true
		}
	}
	return false
}

func validInputCommandIntent(intent InputCommandIntent) bool {
	return validRequiredString(intent.CommandID, 4096) && len(intent.PayloadDigest) == sha256.Size*2 && validHex(intent.PayloadDigest) && validRequiredString(intent.RunningTransitionID, 4096) && validRequiredString(intent.AckID, 4096) && validInputCommandOutcome(intent.Outcome) && (!intent.AcknowledgementDelivered || intent.Outcome != "")
}

func validInputCommandOutcome(outcome string) bool {
	switch outcome {
	case "", "applied", "failed":
		return true
	default:
		return false
	}
}

func validHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func isTerminalTransitionState(state string) bool {
	switch state {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func hasPendingTerminalTransition(transitions []protocol.StateTransitionRequest) bool {
	for _, transition := range transitions {
		if isTerminalTransitionState(transition.State) {
			return true
		}
	}
	return false
}

func pendingTerminalState(transitions []protocol.StateTransitionRequest) string {
	for _, transition := range transitions {
		if isTerminalTransitionState(transition.State) {
			return transition.State
		}
	}
	return ""
}

func (store *Store) loadJournalLocked(key RunKey) (RunJournal, error) {
	var journal RunJournal
	if err := store.readJSONWithLimit(store.journalPath(key), "run journal", &journal, maxJournalFileBytes); err != nil {
		return RunJournal{}, err
	}
	if err := validateJournal(journal); err != nil {
		return RunJournal{}, errors.New("invalid run journal")
	}
	if journal.Key() != key {
		return RunJournal{}, errors.New("run journal does not match requested key")
	}
	return journal, nil
}

func (store *Store) ensureOpenLocked() error {
	if store.closed || store.lock == nil {
		return errors.New("state store is closed")
	}
	return nil
}

func (journal RunJournal) hasClaimGrant() bool {
	return strings.TrimSpace(journal.LeaseToken) != "" && !journal.LeaseExpiresAt.IsZero()
}

func (store *Store) saveJournalLocked(journal RunJournal) error {
	if err := validateJournal(journal); err != nil {
		return err
	}
	return store.writeJSONWithLimit(store.journalPath(journal.Key()), journal, "run journal", maxJournalFileBytes)
}

func (store *Store) readJSON(path string, resource string, destination any) error {
	return store.readJSONWithLimit(path, resource, destination, maxStateFileBytes)
}

func (store *Store) readJSONWithLimit(path string, resource string, destination any, limit int) error {
	data, err := readLimited(path, limit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &NotFoundError{Resource: resource}
		}
		return errors.New("read " + resource)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("decode " + resource)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("decode " + resource)
	}
	return nil
}

func (store *Store) writeJSON(path string, value any, resource string) error {
	return store.writeJSONWithLimit(path, value, resource, maxStateFileBytes)
}

func (store *Store) writeJSONWithLimit(path string, value any, resource string, limit int) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > limit {
		return errors.New("encode " + resource)
	}
	if err := writeAtomic(path, data); err != nil {
		return errors.New("write " + resource)
	}
	return nil
}

func (store *Store) removeOwnedTempsLocked(entries []os.DirEntry) error {
	for _, entry := range entries {
		if entry.IsDir() || !isOwnedTemp(entry.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(store.runsDir(), entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("remove run journal temporary file")
		}
	}
	return nil
}

func (store *Store) identityPath() string {
	return filepath.Join(store.dir, identityFileName)
}

func (store *Store) enrollmentPath() string {
	return filepath.Join(store.dir, enrollmentFileName)
}

func (store *Store) runsDir() string {
	return filepath.Join(store.dir, runsDirectoryName)
}

func (store *Store) journalPath(key RunKey) string {
	input := key.RunID + "\x00" + strconv.FormatInt(key.Generation, 10)
	digest := sha256.Sum256([]byte(input))
	return filepath.Join(store.runsDir(), journalFilePrefix+hex.EncodeToString(digest[:])+journalFileSuffix)
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return applyDirectorySecurity(path)
}

func readLimited(path string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("state file exceeds size limit")
	}
	return data, nil
}

func writeAtomic(path string, data []byte) (err error) {
	directory := filepath.Dir(path)
	var temporary *os.File
	var temporaryPath string
	for range 16 {
		suffix, randomErr := randomHex(16)
		if randomErr != nil {
			return randomErr
		}
		temporaryPath = filepath.Join(directory, atomicTempPrefix+suffix+".tmp")
		temporary, err = os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if temporary == nil {
		return errors.New("create temporary state file")
	}
	defer func() {
		if temporary != nil {
			_ = temporary.Close()
		}
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := applyFileSecurity(temporaryPath); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	temporary = nil
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	temporaryPath = ""
	return syncDirectory(directory)
}

func randomHex(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func isJournalFile(name string) bool {
	if !strings.HasPrefix(name, journalFilePrefix) || !strings.HasSuffix(name, journalFileSuffix) {
		return false
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(name, journalFilePrefix), journalFileSuffix)
	if len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func isOwnedTemp(name string) bool {
	if !strings.HasPrefix(name, atomicTempPrefix) || !strings.HasSuffix(name, ".tmp") {
		return false
	}
	suffix := strings.TrimSuffix(strings.TrimPrefix(name, atomicTempPrefix), ".tmp")
	if len(suffix) != 32 {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}

func validateIdentity(identity MachineIdentity) error {
	if strings.TrimSpace(identity.MachineID) == "" || strings.TrimSpace(identity.MachineToken) == "" || len(identity.MachineID) > 4096 || len(identity.MachineToken) > 65536 {
		return errors.New("machine identity is invalid")
	}
	return nil
}

func validateEnrollmentIntent(intent EnrollmentIntent) error {
	if !validRequiredString(intent.MachineName, 4096) || !validRequiredString(intent.MachineToken, 65536) || !validRequiredString(intent.IdempotencyKey, 4096) {
		return errors.New("enrollment intent is invalid")
	}
	return nil
}

func validateKey(key RunKey) error {
	if strings.TrimSpace(key.RunID) == "" || len(key.RunID) > 4096 || key.Generation <= 0 {
		return errors.New("run key is invalid")
	}
	return nil
}

func validateJournal(journal RunJournal) error {
	if err := validateKey(journal.Key()); err != nil {
		return err
	}
	if !validRequiredString(journal.RuntimeKey, 4096) || !validRequiredString(journal.RuntimeID, 4096) || journal.ClaimedRuntimeEpoch <= 0 || !validRequiredString(journal.ClaimID, 4096) || !validRequiredString(journal.LocalState, 256) || len(journal.WorkspacePath) > 32768 || !validRequiredString(journal.WorkspaceBindingKey, 4096) || journal.PID < 0 || len(journal.ProcessIdentity) > 4096 || journal.LastEventSequence < 0 {
		return errors.New("run journal is invalid")
	}
	if journal.PID == 0 && (!journal.StartedAt.IsZero() || journal.ProcessIdentity != "") {
		return errors.New("run journal process details are invalid")
	}
	if journal.PID > 0 && (journal.StartedAt.IsZero() || !validRequiredString(journal.ProcessIdentity, 4096)) {
		return errors.New("run journal process details are invalid")
	}
	if (strings.TrimSpace(journal.LeaseToken) == "") != journal.LeaseExpiresAt.IsZero() || len(journal.LeaseToken) > 65536 || !validWork(journal.Work) {
		return errors.New("run journal is invalid")
	}
	if !validTerminalState(journal) {
		return errors.New("run journal is invalid")
	}
	if !journal.hasClaimGrant() && (journal.PID != 0 || !journal.StartedAt.IsZero() || len(journal.PendingEvents) != 0 || len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0) {
		return errors.New("run journal is invalid")
	}
	lastSequence := int64(0)
	for _, event := range journal.PendingEvents {
		if !validEvent(event) || event.Sequence <= lastSequence || event.Sequence > journal.LastEventSequence {
			return errors.New("run journal is invalid")
		}
		lastSequence = event.Sequence
	}
	for _, transition := range journal.PendingTransitions {
		if !validTransition(transition) || !sameFence(transition.Fence, journal.Fence()) {
			return errors.New("run journal is invalid")
		}
	}
	if !validAttemptedTransitions(journal.AttemptedTransitionIDs, journal.PendingTransitions) {
		return errors.New("run journal is invalid")
	}
	commandIDs := make(map[string]struct{}, len(journal.PendingCommandAcknowledgements))
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if !validAcknowledgement(acknowledgement, journal) {
			return errors.New("run journal is invalid")
		}
		if _, exists := commandIDs[acknowledgement.CommandID]; exists {
			return errors.New("run journal is invalid")
		}
		commandIDs[acknowledgement.CommandID] = struct{}{}
	}
	if intent := journal.InputCommandIntent; intent != nil {
		if !validInputCommandIntent(*intent) {
			return errors.New("run journal input command intent is invalid")
		}
		pending := false
		for _, acknowledgement := range journal.PendingCommandAcknowledgements {
			if acknowledgement.AckID == intent.AckID {
				pending = acknowledgement.CommandID == intent.CommandID && acknowledgement.Outcome == intent.Outcome
				break
			}
		}
		if intent.Outcome == "" && pending {
			return errors.New("unresolved input command has an acknowledgement")
		}
		if intent.Outcome != "" && intent.AcknowledgementDelivered == pending {
			return errors.New("input command acknowledgement delivery is invalid")
		}
	}
	return nil
}

func validRequiredString(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit
}

func validWork(work protocol.Work) bool {
	return len(work.Goal) <= 65536 && len(work.AgentProfile) <= 4096 && len(work.Workspace) <= 4096 && validRawMessage(work.Input)
}

func validEvent(event protocol.RunEvent) bool {
	return validRequiredString(event.EventID, 4096) && event.Sequence > 0 && validRequiredString(event.Kind, 256) && !event.OccurredAt.IsZero() && validRawMessage(event.Payload)
}

func validTransition(transition protocol.StateTransitionRequest) bool {
	return validRequiredString(transition.TransitionID, 4096) && validRequiredString(transition.State, 256) && validRawMessage(transition.Payload)
}

func validTerminalState(journal RunJournal) bool {
	if journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" {
		return !hasPendingTerminalTransition(journal.PendingTransitions) && journal.TerminalPendingAt.IsZero() && journal.TerminalState == "" && journal.TerminalVerdict == "" && journal.TerminalResolvedAt.IsZero()
	}
	if journal.TerminalPendingAt.IsZero() || !isTerminalTransitionState(journal.TerminalState) {
		return false
	}
	if journal.LocalState == "cleanup_pending" {
		if !validTerminalVerdict(journal.TerminalVerdict) || journal.TerminalResolvedAt.IsZero() {
			return false
		}
		return len(journal.PendingEvents) == 0 && len(journal.PendingTransitions) == 0 && len(journal.PendingCommandAcknowledgements) == 0 && len(journal.AttemptedTransitionIDs) == 0
	}
	if journal.TerminalVerdict == "" {
		return journal.TerminalResolvedAt.IsZero()
	}
	return validTerminalVerdict(journal.TerminalVerdict) && !journal.TerminalResolvedAt.IsZero()
}

func validTerminalVerdict(verdict string) bool {
	switch verdict {
	case TerminalVerdictAccepted, TerminalVerdictOwnershipLost, TerminalVerdictGraceExpired:
		return true
	default:
		return false
	}
}

func validConclusiveTerminalVerdict(verdict string) bool {
	return verdict == TerminalVerdictOwnershipLost || verdict == TerminalVerdictGraceExpired
}

func validAttemptedTransitions(attempted []string, pending []protocol.StateTransitionRequest) bool {
	pendingIDs := make(map[string]struct{}, len(pending))
	for _, transition := range pending {
		pendingIDs[transition.TransitionID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(attempted))
	for _, transitionID := range attempted {
		if !validRequiredString(transitionID, 4096) {
			return false
		}
		if _, exists := pendingIDs[transitionID]; !exists {
			return false
		}
		if _, duplicate := seen[transitionID]; duplicate {
			return false
		}
		seen[transitionID] = struct{}{}
	}
	return true
}

func validAcknowledgement(acknowledgement protocol.CommandAcknowledgement, journal RunJournal) bool {
	return acknowledgement.RunID == journal.RunID && validRequiredString(acknowledgement.CommandID, 4096) && validRequiredString(acknowledgement.Outcome, 256) && validRequiredString(acknowledgement.AckID, 4096) && sameFence(acknowledgement.Fence, journal.Fence())
}

func validRawMessage(message json.RawMessage) bool {
	return len(message) == 0 || json.Valid(message)
}

func isZeroFence(fence protocol.Fence) bool {
	return fence == (protocol.Fence{})
}

func sameFence(left, right protocol.Fence) bool {
	return left == right
}

func removeEvents(events []protocol.RunEvent, ids []string) []protocol.RunEvent {
	if len(ids) == 0 {
		return events
	}
	remove := makeStringSet(ids)
	result := events[:0]
	for _, event := range events {
		if !remove[event.EventID] {
			result = append(result, event)
		}
	}
	return result
}

func removeTransitions(transitions []protocol.StateTransitionRequest, ids []string) []protocol.StateTransitionRequest {
	if len(ids) == 0 {
		return transitions
	}
	remove := makeStringSet(ids)
	result := transitions[:0]
	for _, transition := range transitions {
		if !remove[transition.TransitionID] {
			result = append(result, transition)
		}
	}
	return result
}

// HasPendingTransition reports whether transitionID is still queued.
func (journal RunJournal) HasPendingTransition(transitionID string) bool {
	return hasPendingTransition(journal.PendingTransitions, transitionID)
}

func hasPendingTransition(transitions []protocol.StateTransitionRequest, transitionID string) bool {
	for _, transition := range transitions {
		if transition.TransitionID == transitionID {
			return true
		}
	}
	return false
}

func hasAttemptedTransition(attempted []string, transitionID string) bool {
	for _, attemptedID := range attempted {
		if attemptedID == transitionID {
			return true
		}
	}
	return false
}

func retainAttemptedTransitions(attempted []string, pending []protocol.StateTransitionRequest) []string {
	if len(attempted) == 0 {
		return nil
	}
	result := attempted[:0]
	for _, transitionID := range attempted {
		if hasPendingTransition(pending, transitionID) {
			result = append(result, transitionID)
		}
	}
	return result
}

func removeAcknowledgements(acknowledgements []protocol.CommandAcknowledgement, ids []string) []protocol.CommandAcknowledgement {
	if len(ids) == 0 {
		return acknowledgements
	}
	remove := makeStringSet(ids)
	result := acknowledgements[:0]
	for _, acknowledgement := range acknowledgements {
		if !remove[acknowledgement.AckID] {
			result = append(result, acknowledgement)
		}
	}
	return result
}

func makeStringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
