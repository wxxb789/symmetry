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
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	maxStateFileBytes      = 1 << 20
	maxJournalFileBytes    = 4 << 20
	identityFileName       = "identity.json"
	enrollmentFileName     = "enrollment.json"
	runsDirectoryName      = "runs"
	goalUsageDirectoryName = "goal-usage"
	lockFileName           = ".symmetry-daemon.lock"
	journalFilePrefix      = "journal-"
	journalFileSuffix      = ".json"
	atomicTempPrefix       = ".symmetry-state-"
)

const (
	TerminalVerdictAccepted      = "accepted"
	TerminalVerdictOwnershipLost = "ownership_lost"
	TerminalVerdictGraceExpired  = "terminal_grace_expired"
)

// IsConclusiveTerminalVerdict reports whether the control plane can no longer
// accept delivery for the terminal run generation.
func IsConclusiveTerminalVerdict(verdict string) bool {
	return verdict == TerminalVerdictOwnershipLost || verdict == TerminalVerdictGraceExpired
}

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
	dir         string
	mu          sync.Mutex
	lock        *os.File
	closed      bool
	atomicWrite func(string, []byte) error
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
	RunID               string    `json:"run_id"`
	Generation          int64     `json:"generation"`
	RuntimeKey          string    `json:"runtime_key"`
	RuntimeID           string    `json:"runtime_id"`
	ClaimedRuntimeEpoch int64     `json:"claimed_runtime_epoch"`
	ClaimID             string    `json:"claim_id"`
	LeaseToken          string    `json:"lease_token"`
	LeaseExpiresAt      time.Time `json:"lease_expires_at"`
	LocalState          string    `json:"local_state"`
	TerminalPendingAt   time.Time `json:"terminal_pending_at,omitempty"`
	TerminalState       string    `json:"terminal_state,omitempty"`
	// TerminalTaskResultKind is the immutable semantic result kind carried by
	// the first completed transition. It survives transition delivery so local
	// cleanup can distinguish a candidate artifact from other successful work.
	TerminalTaskResultKind    protocol.TaskResultKind `json:"terminal_task_result_kind,omitempty"`
	TerminalIntentDigest      string                  `json:"terminal_intent_digest,omitempty"`
	TerminalCommandDigest     string                  `json:"terminal_command_digest,omitempty"`
	TerminalSettlementDigest  string                  `json:"terminal_settlement_digest,omitempty"`
	TerminalVerdict           string                  `json:"terminal_verdict,omitempty"`
	TerminalResolvedAt        time.Time               `json:"terminal_resolved_at,omitempty"`
	Work                      protocol.Work           `json:"work"`
	WorkspacePath             string                  `json:"workspace_path"`
	WorkspaceRecoveryRequired bool                    `json:"workspace_recovery_required,omitempty"`
	RetainWorkspace           bool                    `json:"retain_workspace,omitempty"`
	WorkspaceBindingKey       string                  `json:"workspace_binding_key"`
	PID                       int                     `json:"pid"`
	ProcessIdentity           string                  `json:"process_identity"`
	StartedAt                 time.Time               `json:"started_at"`
	// ContainmentAuthority is optional for backwards compatibility with
	// journals created before the independent Windows supervisor existed. A
	// process marker without it remains intentionally unrecoverable on Windows.
	ContainmentAuthority *authority.Supervisor `json:"containment_authority,omitempty"`
	// ContainmentHandoff is the durable pre-authority launch fence. It is
	// mutually exclusive with ContainmentAuthority and is cleared only by the
	// dedicated commit or release-proof mutations.
	ContainmentHandoff             *authority.SupervisorHandoff      `json:"containment_handoff,omitempty"`
	LastEventSequence              int64                             `json:"last_event_sequence"`
	PendingEvents                  []protocol.RunEvent               `json:"pending_events"`
	DroppedOutputChunks            int64                             `json:"dropped_output_chunks,omitempty"`
	DroppedOutputBytes             int64                             `json:"dropped_output_bytes,omitempty"`
	PendingTransitions             []protocol.StateTransitionRequest `json:"pending_transitions"`
	AttemptedTransitionIDs         []string                          `json:"attempted_transition_ids,omitempty"`
	PendingCommandAcknowledgements []protocol.CommandAcknowledgement `json:"pending_command_acknowledgements"`
	PendingGoalDeliveries          []GoalDelivery                    `json:"pending_goal_deliveries,omitempty"`
	DeliveredGoalDeliveries        []GoalDelivery                    `json:"delivered_goal_deliveries,omitempty"`
	RetiredGoalDeliveries          []GoalDeliveryRetirement          `json:"retired_goal_deliveries,omitempty"`
	GoalDeliveryEnabled            bool                              `json:"goal_delivery_enabled,omitempty"`
	NativeUsageObservation         *NativeUsageObservation           `json:"native_usage_observation,omitempty"`
	NativeUsageRecoveryRequired    bool                              `json:"native_usage_recovery_required,omitempty"`
	NativeUsageTerminalRecovery    *NativeUsageTerminalRecovery      `json:"native_usage_terminal_recovery,omitempty"`
	InputCommandIntent             *InputCommandIntent               `json:"input_command_intent,omitempty"`
	ControlCommandIntents          []ControlCommandIntent            `json:"control_command_intents,omitempty"`
	ProviderActionIntents          []ProviderActionIntent            `json:"provider_action_intents,omitempty"`
}

// NativeUsageTerminalRecovery preserves the exact terminal intent observed
// before native usage persistence failed. IDs remain generated by the outbox
// writer; the result payload and optional command outcome are immutable.
type NativeUsageTerminalRecovery struct {
	State          string          `json:"state"`
	Payload        json.RawMessage `json:"payload"`
	CommandID      string          `json:"command_id,omitempty"`
	CommandOutcome string          `json:"command_outcome,omitempty"`
}

// InputCommandIntent is the durable at-most-once record for one provide_input
// command in a waiting-for-input episode.
type InputCommandIntent struct {
	CommandID                string `json:"command_id"`
	PayloadDigest            string `json:"payload_digest"`
	RunningTransitionID      string `json:"running_transition_id"`
	AckID                    string `json:"ack_id"`
	EventSequenceBarrier     int64  `json:"event_sequence_barrier"`
	Outcome                  string `json:"outcome,omitempty"`
	AcknowledgementDelivered bool   `json:"acknowledgement_delivered,omitempty"`
}

// Key returns the journal's durable identity.
func (journal RunJournal) Key() RunKey {
	return RunKey{RunID: journal.RunID, Generation: journal.Generation}
}

// HasProcessDetails reports any retained process-ownership marker. A partial
// marker is not evidence that its process has stopped.
func (journal RunJournal) HasProcessDetails() bool {
	return journal.PID != 0 || journal.ProcessIdentity != "" || !journal.StartedAt.IsZero()
}

// HasPendingContainment reports a durable containment authority or an
// unresolved pre-authority handoff. It intentionally does not include the
// legacy process marker so HasProcessDetails retains its existing meaning.
func (journal RunJournal) HasPendingContainment() bool {
	return journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil
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

// CommandAcknowledgementRetired reports whether a command acknowledgement can
// no longer be delivered for journal's run generation.
func CommandAcknowledgementRetired(journal RunJournal) bool {
	return journal.LocalState == "cleanup_pending" || (journal.LocalState == "terminal_pending" && IsConclusiveTerminalVerdict(journal.TerminalVerdict))
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
		return nil, fmt.Errorf("secure state lock: %w", err)
	}
	store := &Store{dir: directory, lock: lock, atomicWrite: writeAtomic}
	if err := ensurePrivateDirectory(store.runsDir()); err != nil {
		_ = releaseStoreLock(lock)
		return nil, fmt.Errorf("create run journal directory: %w", err)
	}
	return store, nil
}

// SetAtomicWriterForTesting replaces the final journal-write primitive for a
// focused fault-injection test. Production callers must not use this seam.
func (store *Store) SetAtomicWriterForTesting(writer func(string, []byte) error) func() {
	store.mu.Lock()
	previous := store.atomicWrite
	if writer == nil {
		writer = writeAtomic
	}
	store.atomicWrite = writer
	store.mu.Unlock()
	return func() {
		store.mu.Lock()
		store.atomicWrite = previous
		store.mu.Unlock()
	}
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
	if err := store.saveJournalWithCapacityGuardLocked(journal); err != nil {
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
	if err := store.saveJournalWithCapacityGuardLocked(journal); err != nil {
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
	if current, err := store.loadJournalLocked(journal.Key()); err == nil {
		if current.TerminalTaskResultKind != journal.TerminalTaskResultKind {
			return errors.New("terminal task result kind is immutable")
		}
		if current.PID != journal.PID || current.ProcessIdentity != journal.ProcessIdentity || !current.StartedAt.Equal(journal.StartedAt) {
			return errors.New("process details require dedicated mutation")
		}
		if !sameContainmentAuthority(current.ContainmentAuthority, journal.ContainmentAuthority) {
			return errors.New("containment authority requires dedicated mutation")
		}
		if !sameContainmentHandoff(current.ContainmentHandoff, journal.ContainmentHandoff) {
			return errors.New("containment handoff requires dedicated mutation")
		}
		if (current.LocalState == "cleanup_pending") != (journal.LocalState == "cleanup_pending") {
			return errors.New("cleanup state requires dedicated transition")
		}
	} else if IsNotFound(err) {
		if journal.ContainmentAuthority != nil {
			return errors.New("containment authority requires dedicated mutation")
		}
		if journal.ContainmentHandoff != nil {
			return errors.New("containment handoff requires dedicated mutation")
		}
	} else {
		return err
	}
	// SaveJournal is the explicit recovery/import replacement seam. Normal
	// lossless mutations use saveJournalWithCapacityGuardLocked below; this
	// seam must remain able to import an already-existing unresolved journal so
	// recovery can classify it before ordinary mutations resume.
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

// MarkNativeUsageRecoveryRequired records that a native run reached a final
// outcome but its accounting delivery could not yet be made durable. The flag
// keeps stale local state from being treated as ordinary lease recovery; the
// outbox must persist a conservative terminal path before cleanup is allowed.
func (store *Store) MarkNativeUsageRecoveryRequired(key RunKey) (RunJournal, error) {
	return store.MarkNativeUsageRecoverySettlement(key, nil)
}

// MarkNativeUsageRecoverySettlement records the accounting barrier together
// with any exact terminal intent already observed by the live native owner.
func (store *Store) MarkNativeUsageRecoverySettlement(key RunKey, settlement *NativeUsageTerminalRecovery) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.NativeUsageRecoveryRequired = true
		return mergeNativeUsageTerminalRecovery(journal, settlement)
	})
}

// ClearNativeUsageRecoveryRequired releases the accounting recovery barrier
// only after the exact native usage body is durable in delivery history.
func (store *Store) ClearNativeUsageRecoveryRequired(key RunKey, usage protocol.Usage) (RunJournal, error) {
	if err := usage.Validate(); err != nil {
		return RunJournal{}, err
	}
	if usage.RunID != key.RunID {
		return RunJournal{}, errors.New("native usage recovery run ID does not match journal")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.NativeUsageRecoveryRequired {
			return nil
		}
		if !hasExactUsageDelivery(*journal, usage) {
			return errors.New("native usage recovery delivery is not durable")
		}
		journal.NativeUsageRecoveryRequired = false
		journal.NativeUsageTerminalRecovery = nil
		return nil
	})
}

func hasExactUsageDelivery(journal RunJournal, usage protocol.Usage) bool {
	delivery := GoalDelivery{Kind: GoalDeliveryUsage, DeliveryID: usage.UsageKey, Fence: journal.Fence(), Usage: &usage, Ready: true}
	if err := prepareGoalDelivery(journal.RunID, &delivery); err != nil {
		return false
	}
	for _, candidate := range journal.PendingGoalDeliveries {
		if candidate.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(candidate, delivery) {
			return true
		}
	}
	for _, candidate := range journal.DeliveredGoalDeliveries {
		if candidate.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(candidate, delivery) {
			return true
		}
	}
	for _, candidate := range journal.RetiredGoalDeliveries {
		if candidate.Delivery.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(candidate.Delivery, delivery) {
			return true
		}
	}
	return false
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
	journal, err := store.loadJournalLocked(key)
	if err != nil {
		return err
	}
	if journal.HasProcessDetails() {
		return errors.New("process stop evidence remains pending")
	}
	if journal.HasPendingContainment() {
		return errors.New("containment state remains pending")
	}
	if len(journal.PendingCommandAcknowledgements) != 0 {
		return errors.New("command acknowledgement remains pending")
	}
	if journal.HasPendingGoalDeliveries() {
		return errors.New("run journal has pending Goal delivery")
	}
	if journal.NativeUsageRecoveryRequired {
		return errors.New("native usage recovery remains pending")
	}
	if hasUnresolvedProviderActions(journal) {
		return errors.New("provider action outcome remains pending")
	}
	if journal.GoalDeliveryEnabled {
		if err := store.archiveLateGoalUsageLocked(journal); err != nil {
			return err
		}
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
		if err := store.readRunJournalJSONWithLimit(filepath.Join(store.runsDir(), entry.Name()), &journal, maxJournalFileBytes); err != nil {
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
		if (journal.LocalState == "cleanup_pending") != (localState == "cleanup_pending") {
			return errors.New("cleanup state requires dedicated transition")
		}
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
		if journal.ContainmentHandoff != nil {
			return errors.New("process details require supervisor handoff mutation")
		}
		if journal.LocalState == "cleanup_pending" {
			return errors.New("cannot register process details during cleanup")
		}
		if journal.HasProcessDetails() {
			if journal.PID != pid || journal.ProcessIdentity != identity {
				return errors.New("process details cannot replace an uncleared owner")
			}
			return nil
		}
		journal.PID = pid
		journal.ProcessIdentity = identity
		journal.StartedAt = startedAt
		return nil
	})
}

// SetProcessDetailsWithAuthority atomically persists the process marker and
// its independently releasable containment authority. The single journal
// mutation prevents recovery from observing a marker without the authority
// required to verify and release that process.
func (store *Store) SetProcessDetailsWithAuthority(key RunKey, pid int, identity string, startedAt time.Time, value authority.Supervisor) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) || startedAt.IsZero() {
		return RunJournal{}, errors.New("process details are invalid")
	}
	if err := value.Validate(); err != nil {
		return RunJournal{}, err
	}
	if value.TargetPID != pid || value.TargetIdentity != identity {
		return RunJournal{}, errors.New("containment authority target does not match process details")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if journal.ContainmentHandoff != nil {
			return errors.New("process details require supervisor handoff mutation")
		}
		if journal.LocalState == "cleanup_pending" {
			return errors.New("cannot register process details during cleanup")
		}
		if journal.HasProcessDetails() {
			if journal.PID != pid || journal.ProcessIdentity != identity {
				return errors.New("process details cannot replace an uncleared owner")
			}
		} else {
			journal.PID = pid
			journal.ProcessIdentity = identity
			journal.StartedAt = startedAt
		}
		if journal.ContainmentAuthority != nil {
			if journal.ContainmentAuthority.Equal(value) {
				return nil
			}
			// A retry after a stop-receipt write must not erase the receipt by
			// replaying the original authority without its terminal witness.
			existing := journal.ContainmentAuthority.Clone()
			existing.StopReceipt = nil
			candidate := value.Clone()
			candidate.StopReceipt = nil
			if existing.Equal(candidate) && journal.ContainmentAuthority.StopReceipt != nil && value.StopReceipt == nil {
				return nil
			}
			return errors.New("containment authority cannot replace an uncleared owner")
		}
		cloned := value.Clone()
		journal.ContainmentAuthority = &cloned
		return nil
	})
}

// SetContainmentAuthority persists the typed binding for an already persisted
// process marker. It never replaces an uncleared authority with a different
// launch, so a stale callback cannot retarget recovery.
func (store *Store) SetContainmentAuthority(key RunKey, pid int, identity string, value authority.Supervisor) (RunJournal, error) {
	if pid <= 0 || strings.TrimSpace(identity) == "" {
		return RunJournal{}, errors.New("process details are invalid")
	}
	if err := value.Validate(); err != nil {
		return RunJournal{}, err
	}
	if value.TargetPID != pid || value.TargetIdentity != identity {
		return RunJournal{}, errors.New("containment authority target does not match process details")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff != nil {
			return errors.New("containment authority requires supervisor handoff mutation")
		}
		if journal.PID != pid || journal.ProcessIdentity != identity || journal.StartedAt.IsZero() {
			return errors.New("containment authority requires a persisted process marker")
		}
		if journal.ContainmentAuthority != nil {
			if journal.ContainmentAuthority.Equal(value) {
				return nil
			}
			// A retry after a stop-receipt write must not erase the receipt by
			// replaying the original authority without its terminal witness. The
			// binding itself is still idempotent, so preserve the existing record
			// when only its receipt differs.
			existing := journal.ContainmentAuthority.Clone()
			existing.StopReceipt = nil
			candidate := value.Clone()
			candidate.StopReceipt = nil
			if existing.Equal(candidate) && journal.ContainmentAuthority.StopReceipt != nil && value.StopReceipt == nil {
				return nil
			}
			return errors.New("containment authority cannot replace an uncleared owner")
		}
		cloned := value.Clone()
		journal.ContainmentAuthority = &cloned
		return nil
	})
}

// PrepareSupervisorHandoff durably records the immutable launch binding before
// an independently running helper can outlive the daemon. Replaying the same
// launch preserves the record and performs a fresh persistence barrier so an
// unknown prior write is not upgraded by readback alone. A different launch
// cannot replace an unresolved record. Helper identity and stop receipt are
// advanced only by their dedicated CAS mutations.
func (store *Store) PrepareSupervisorHandoff(key RunKey, handoff authority.SupervisorHandoff) (RunJournal, error) {
	if err := handoff.Validate(); err != nil {
		return RunJournal{}, err
	}
	if handoff.StopReceipt != nil {
		return RunJournal{}, errors.New("supervisor handoff stop receipt requires dedicated mutation")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if journal.LocalState == "cleanup_pending" {
			return errors.New("cannot prepare supervisor handoff during cleanup")
		}
		if journal.HasProcessDetails() || journal.ContainmentAuthority != nil {
			return errors.New("supervisor handoff conflicts with committed containment")
		}
		if journal.ContainmentHandoff == nil {
			cloned := handoff.Clone()
			journal.ContainmentHandoff = &cloned
			return nil
		}
		current := journal.ContainmentHandoff
		if !current.SameLaunch(handoff) {
			return errors.New("supervisor handoff conflicts with journal")
		}
		if current.SupervisorPID != 0 && handoff.SupervisorPID != 0 &&
			(current.SupervisorPID != handoff.SupervisorPID || current.SupervisorIdentity != handoff.SupervisorIdentity) {
			return errors.New("supervisor handoff helper identity conflicts with journal")
		}
		if current.SupervisorPID == 0 && handoff.SupervisorPID != 0 {
			return errors.New("supervisor handoff helper identity requires dedicated bind")
		}
		// Replaying the pre-helper form must not erase a helper identity or a
		// receipt that a later phase durably recorded.
		return nil
	})
}

// BindSupervisorHandoff adds the exact helper PID/creation identity to a
// prepared handoff. The expected handoff is an exact CAS value, so a stale
// writer cannot retarget a retained helper. Exact replays perform a fresh
// persistence barrier after readback of an unknown prior write.
func (store *Store) BindSupervisorHandoff(key RunKey, expected authority.SupervisorHandoff, pid int, identity string) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if pid <= 0 || !validRequiredString(identity, 4096) {
		return RunJournal{}, errors.New("supervisor handoff helper identity is invalid")
	}
	if expected.StopReceipt != nil {
		return RunJournal{}, errors.New("supervisor handoff helper cannot be rebound after stop receipt")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff == nil {
			return errors.New("supervisor handoff is not pending")
		}
		current := journal.ContainmentHandoff
		if !current.Equal(expected) {
			// A write error after rename leaves the helper binding durable while
			// the caller still holds the pre-bind expected value. Replaying the
			// same binding is safe and must not be mistaken for retargeting.
			if expected.SupervisorPID == 0 && expected.SupervisorIdentity == "" && expected.StopReceipt == nil &&
				current.SameLaunch(expected) && current.SupervisorPID == pid && current.SupervisorIdentity == identity && current.StopReceipt == nil {
				return nil
			}
			return errors.New("supervisor handoff compare-and-set mismatch")
		}
		if current.SupervisorPID != 0 || current.SupervisorIdentity != "" {
			if current.SupervisorPID == pid && current.SupervisorIdentity == identity {
				return nil
			}
			return errors.New("supervisor handoff helper identity conflicts with journal")
		}
		current.SupervisorPID = pid
		current.SupervisorIdentity = identity
		return nil
	})
}

// CommitSupervisorHandoff atomically promotes an exact prepared handoff into
// the existing process marker plus Supervisor authority. The handoff is
// cleared in the same journal replacement, so recovery observes either the
// old pending record or the complete committed pair. Exact replays perform a
// fresh persistence barrier after readback of an unknown prior write.
func (store *Store) CommitSupervisorHandoff(key RunKey, expected authority.SupervisorHandoff, startedAt time.Time) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.StopReceipt != nil {
		return RunJournal{}, errors.New("supervisor handoff stop receipt cannot commit")
	}
	if startedAt.IsZero() {
		return RunJournal{}, errors.New("supervisor handoff start time is invalid")
	}
	value, err := expected.ToSupervisor()
	if err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff == nil {
			if journal.PID == value.TargetPID && journal.ProcessIdentity == value.TargetIdentity &&
				!journal.StartedAt.IsZero() && journal.ContainmentAuthority != nil {
				existing := journal.ContainmentAuthority.Clone()
				existing.StopReceipt = nil
				candidate := value.Clone()
				candidate.StopReceipt = nil
				if existing.Equal(candidate) {
					return nil
				}
			}
			return errors.New("supervisor handoff is not pending")
		}
		if !journal.ContainmentHandoff.Equal(expected) {
			return errors.New("supervisor handoff compare-and-set mismatch")
		}
		if journal.HasProcessDetails() || journal.ContainmentAuthority != nil {
			return errors.New("supervisor handoff conflicts with committed containment")
		}
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if journal.LocalState == "cleanup_pending" {
			return errors.New("cannot commit supervisor handoff during cleanup")
		}
		journal.PID = value.TargetPID
		journal.ProcessIdentity = value.TargetIdentity
		journal.StartedAt = startedAt
		cloned := value.Clone()
		journal.ContainmentAuthority = &cloned
		journal.ContainmentHandoff = nil
		return nil
	})
}

// RecordSupervisorHandoffStopReceipt records an exact positive stop witness
// while the handoff is pending. It also accepts the commit-unknown replay
// state, where the handoff rename succeeded before the caller observed an
// error, and records the witness on the committed authority instead. Every
// accepted replay performs a fresh journal write so a prior unknown result is
// never upgraded by readback alone.
func (store *Store) RecordSupervisorHandoffStopReceipt(key RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if !receipt.ValidForHandoff(expected) {
		return RunJournal{}, errors.New("supervisor handoff stop receipt does not match handoff")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff != nil {
			current := journal.ContainmentHandoff
			if !current.Equal(expected) {
				if !current.SameLaunch(expected) || current.SupervisorPID != expected.SupervisorPID || current.SupervisorIdentity != expected.SupervisorIdentity || current.StopReceipt == nil || *current.StopReceipt != receipt {
					return errors.New("supervisor handoff compare-and-set mismatch")
				}
				return nil
			}
			if current.StopReceipt != nil {
				if *current.StopReceipt == receipt {
					return nil
				}
				return errors.New("supervisor handoff stop receipt cannot replace an existing witness")
			}
			cloned := receipt
			current.StopReceipt = &cloned
			return nil
		}
		value, err := expected.ToSupervisor()
		if err != nil {
			return err
		}
		if journal.ContainmentAuthority == nil || journal.PID != value.TargetPID || journal.ProcessIdentity != value.TargetIdentity {
			return errors.New("supervisor handoff is not pending or committed")
		}
		existing := journal.ContainmentAuthority.Clone()
		existing.StopReceipt = nil
		candidate := value.Clone()
		candidate.StopReceipt = nil
		if !existing.Equal(candidate) {
			return errors.New("supervisor handoff committed authority conflicts with handoff")
		}
		if journal.ContainmentAuthority.StopReceipt != nil {
			if *journal.ContainmentAuthority.StopReceipt == receipt {
				return nil
			}
			return errors.New("containment stop receipt cannot replace an existing witness")
		}
		cloned := receipt
		journal.ContainmentAuthority.StopReceipt = &cloned
		return nil
	})
}

// ClearSupervisorHandoff removes a pending handoff only after the caller has
// supplied both an exact durable stop receipt and an exact platform release
// proof. A release proof without a receipt belongs to the separate abort path.
func (store *Store) ClearSupervisorHandoff(key RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.StopReceipt == nil {
		return RunJournal{}, errors.New("supervisor handoff stop receipt is required before release")
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	if !proof.ValidFor(expected) {
		return RunJournal{}, errors.New("supervisor handoff release proof does not match handoff")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff == nil {
			if journal.ContainmentAuthority == nil && !journal.HasProcessDetails() {
				return nil
			}
			return errors.New("supervisor handoff is not pending")
		}
		if !journal.ContainmentHandoff.Equal(expected) {
			return errors.New("supervisor handoff compare-and-set mismatch")
		}
		current := journal.ContainmentHandoff
		if !proof.ValidFor(*current) {
			return errors.New("supervisor handoff release proof does not match journal")
		}
		if current.StopReceipt == nil {
			return errors.New("supervisor handoff stop receipt is required before release")
		}
		if !current.StopReceipt.ValidForHandoff(*current) || *current.StopReceipt != *expected.StopReceipt {
			return errors.New("supervisor handoff stop receipt is invalid")
		}
		journal.ContainmentHandoff = nil
		return nil
	})
}

// ClearSupervisorHandoffAfterAbort clears a prepared handoff only when the
// exact current record is still pending and an independent platform proof
// identifies a definitive pre-authority abort. This path is intentionally
// separate from receipt/release cleanup: an abort proof is not a StopReceipt.
//
// Production callers may use this only while the app owns the Store lifetime
// lock, the old writer is quiesced, and the platform has proved the named Job
// and every exact target/helper process identity are absent. A caller boolean
// or a missing response is not an abort proof and must not be converted into
// one.
func (store *Store) ClearSupervisorHandoffAfterAbort(key RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffAbortProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.StopReceipt != nil {
		return RunJournal{}, errors.New("supervisor handoff abort cannot carry a stop receipt")
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	if !proof.ValidFor(expected) {
		return RunJournal{}, errors.New("supervisor handoff abort proof does not match handoff")
	}
	return store.mutateJournalIfChanged(key, func(journal *RunJournal) (bool, error) {
		if journal.ContainmentHandoff == nil {
			return false, errors.New("supervisor handoff is not pending")
		}
		if !journal.ContainmentHandoff.Equal(expected) {
			return false, errors.New("supervisor handoff compare-and-set mismatch")
		}
		current := journal.ContainmentHandoff
		if current.StopReceipt != nil {
			return false, errors.New("supervisor handoff abort cannot clear a durable stop receipt")
		}
		if journal.ContainmentAuthority != nil || journal.HasProcessDetails() {
			return false, errors.New("supervisor handoff abort conflicts with committed containment")
		}
		if !proof.ValidFor(*current) {
			return false, errors.New("supervisor handoff abort proof does not match journal")
		}
		journal.ContainmentHandoff = nil
		return true, nil
	})
}

// RecordContainmentStopReceipt durably records the exact positive helper
// witness before recovery is allowed to compare-and-clear the process marker.
// It is idempotent for the same authority and receipt, and rejects any
// replacement launch.
func (store *Store) RecordContainmentStopReceipt(key RunKey, pid int, identity string, receipt authority.StopReceipt) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff != nil {
			return errors.New("containment stop receipt requires supervisor handoff mutation")
		}
		if journal.PID != pid || journal.ProcessIdentity != identity || journal.ContainmentAuthority == nil {
			return errors.New("containment stop receipt has no matching authority")
		}
		if !receipt.ValidFor(*journal.ContainmentAuthority) {
			return errors.New("containment stop receipt does not match containment authority")
		}
		if journal.ContainmentAuthority.StopReceipt != nil {
			if *journal.ContainmentAuthority.StopReceipt == receipt {
				return nil
			}
			return errors.New("containment stop receipt cannot replace an existing witness")
		}
		cloned := receipt
		journal.ContainmentAuthority.StopReceipt = &cloned
		return nil
	})
}

// ClearProcessDetails records that the process identified by the expected PID
// and identity has been stopped. The compare-and-clear guard prevents recovery
// from erasing a newer process record if another owner replaced the process
// between termination and durable marker persistence. Replaying the same
// clear after the marker is already persisted is idempotent.
func (store *Store) ClearProcessDetails(key RunKey, pid int, identity string) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) {
		return RunJournal{}, errors.New("process details are invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff != nil {
			return errors.New("process details require supervisor handoff mutation")
		}
		if !journal.HasProcessDetails() {
			return nil
		}
		if journal.PID != pid || journal.ProcessIdentity != identity {
			return errors.New("persisted process details changed during termination")
		}
		journal.PID = 0
		journal.ProcessIdentity = ""
		journal.StartedAt = time.Time{}
		journal.ContainmentAuthority = nil
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

// QueueNextEvent assigns sequence under the same lock as control receipts, so
// concurrent stdin failures cannot invalidate an output event's sequence.
func (store *Store) QueueNextEvent(key RunKey, event protocol.RunEvent) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		event.Sequence = journal.LastEventSequence + 1
		return appendEvent(journal, event, "event is invalid")
	})
}

// QueueOutputEvent bounds only pending raw output. Dropped chunks are recorded
// durably without consuming an event sequence so later semantic events remain
// contiguous.
func (store *Store) QueueOutputEvent(key RunKey, event protocol.RunEvent, budget int) (RunJournal, bool, error) {
	dropped := false
	journal, err := store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if event.Kind != "output" || !validRequiredString(event.EventID, 4096) || event.OccurredAt.IsZero() || !validRawMessage(event.Payload) {
			return errors.New("event is invalid")
		}
		pending := 0
		for _, pendingEvent := range journal.PendingEvents {
			if pendingEvent.Kind == "output" {
				pending += len(pendingEvent.Payload)
			}
		}
		payloadBytes := len(event.Payload)
		if budget >= 0 && (pending > budget || payloadBytes > budget-pending) {
			journal.DroppedOutputChunks++
			journal.DroppedOutputBytes += int64(payloadBytes)
			dropped = true
			return nil
		}
		event.Sequence = journal.LastEventSequence + 1
		return appendEvent(journal, event, "event is invalid")
	})
	if err != nil {
		// The mutation is only a persisted drop after the atomic journal write
		// succeeds. In particular, do not report a drop when a capacity or write
		// failure rejected the counter update.
		dropped = false
	}
	return journal, dropped, err
}

// QueueOutputTruncatedMarker turns accumulated raw-output drops into one
// durable event while clearing the counters in the same journal mutation. It
// calls newEventID only after observing drops in the authoritative journal.
func (store *Store) QueueOutputTruncatedMarker(key RunKey, at time.Time, newEventID func() (string, error)) (RunJournal, bool, error) {
	appended := false
	journal, err := store.mutateJournal(key, func(journal *RunJournal) error {
		var err error
		appended, err = appendOutputTruncatedMarker(journal, at, newEventID)
		return err
	})
	return journal, appended, err
}

// MarkEventsDeliveredAndQueueOutputTruncatedMarker atomically confirms an
// accepted batch and records any preceding raw-output loss. A crash cannot
// otherwise leave loss counters with no pending event that can trigger a marker.
// newEventID is called inside the mutation only when the authoritative journal
// has drops to report, so a concurrent output drop cannot leave an accepted
// event pending because its caller used an older journal snapshot.
func (store *Store) MarkEventsDeliveredAndQueueOutputTruncatedMarker(key RunKey, eventIDs []string, at time.Time, newEventID func() (string, error)) (RunJournal, bool, error) {
	appended := false
	journal, err := store.mutateJournal(key, func(journal *RunJournal) error {
		journal.PendingEvents = removeEvents(journal.PendingEvents, eventIDs)
		var err error
		appended, err = appendOutputTruncatedMarker(journal, at, newEventID)
		return err
	})
	return journal, appended, err
}

func appendOutputTruncatedMarker(journal *RunJournal, at time.Time, newEventID func() (string, error)) (bool, error) {
	if !journal.hasClaimGrant() {
		return false, errors.New("journal has no claim grant")
	}
	if journal.DroppedOutputChunks == 0 {
		return false, nil
	}
	if newEventID == nil {
		return false, errors.New("output truncation marker ID generator is unavailable")
	}
	eventID, err := newEventID()
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(map[string]int64{
		"dropped_chunks": journal.DroppedOutputChunks,
		"dropped_bytes":  journal.DroppedOutputBytes,
	})
	if err != nil {
		return false, err
	}
	event := protocol.RunEvent{EventID: eventID, Kind: "output_truncated", Sequence: journal.LastEventSequence + 1, OccurredAt: at, Payload: payload}
	if err := appendEvent(journal, event, "event is invalid"); err != nil {
		return false, err
	}
	journal.DroppedOutputChunks = 0
	journal.DroppedOutputBytes = 0
	return true, nil
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
		if journal.LocalState == "paused" {
			event.Kind = "agent_event"
			return appendEvent(journal, event, "waiting event is invalid")
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
			if current.CommandID == intent.CommandID {
				if current.PayloadDigest == intent.PayloadDigest {
					return nil
				}
				return errors.New("input command conflicts with journal")
			}
			if !current.AcknowledgementDelivered || journal.LocalState != "waiting_for_input" {
				return errors.New("input command conflicts with journal")
			}
		}
		if journal.LocalState != "waiting_for_input" {
			return errors.New("journal is not waiting for input")
		}
		copy := intent
		copy.EventSequenceBarrier = journal.LastEventSequence
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
			payload, _ := json.Marshal(map[string]string{"command_id": intent.CommandID})
			prepared, err := prepareTransition(journal, protocol.StateTransitionRequest{TransitionID: intent.RunningTransitionID, State: "running", Payload: payload})
			if err != nil {
				return err
			}
			if !journal.HasPendingTransition(prepared.TransitionID) {
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

// QueueTerminalTransitionAndAcknowledgementAt atomically records an
// authoritative terminal transition and its command receipt.
func (store *Store) QueueTerminalTransitionAndAcknowledgementAt(key RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (RunJournal, error) {
	return store.queueTerminalTransitionAndAcknowledgementAt(key, transition, acknowledgement, pendingAt, "")
}

// QueueCancelledTransitionAndAcknowledgementAt atomically records the
// authoritative cancellation terminal transition and its command receipt.
func (store *Store) QueueCancelledTransitionAndAcknowledgementAt(key RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (RunJournal, error) {
	return store.queueTerminalTransitionAndAcknowledgementAt(key, transition, acknowledgement, pendingAt, "cancelled")
}

func (store *Store) queueTerminalTransitionAndAcknowledgementAt(key RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time, requiredState string) (RunJournal, error) {
	if pendingAt.IsZero() {
		return RunJournal{}, errors.New("terminal pending time is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := queueTerminalTransition(journal, transition, pendingAt); err != nil {
			return err
		}
		if requiredState != "" && journal.TerminalState != requiredState {
			return errors.New(requiredState + " terminal transition state is invalid")
		}
		if err := queueCommandAcknowledgement(journal, acknowledgement); err != nil {
			return err
		}
		commandDigest, err := terminalCommandDigest(*journal, acknowledgement)
		if err != nil {
			return err
		}
		if journal.TerminalCommandDigest != "" && journal.TerminalCommandDigest != commandDigest {
			return errors.New("terminal command intent conflicts with journal")
		}
		journal.TerminalCommandDigest = commandDigest
		digest, err := terminalSettlementDigest(*journal, transition, acknowledgement)
		if err != nil {
			return err
		}
		if journal.TerminalSettlementDigest != "" && journal.TerminalSettlementDigest != digest {
			return errors.New("terminal settlement conflicts with journal")
		}
		journal.TerminalSettlementDigest = digest
		return nil
	})
}

// TerminalIntentMatches verifies the terminal state and payload independently
// of the generated transition ID used for one delivery attempt.
func TerminalIntentMatches(journal RunJournal, stateName string, payload json.RawMessage) (bool, bool) {
	if journal.TerminalIntentDigest == "" {
		for _, transition := range journal.PendingTransitions {
			if !isTerminalTransitionState(transition.State) {
				continue
			}
			return transition.State == stateName && bytes.Equal(transition.Payload, payload), true
		}
		return false, journal.TerminalState != ""
	}
	digest, err := terminalIntentDigest(journal, stateName, payload)
	if err != nil {
		return false, true
	}
	return digest == journal.TerminalIntentDigest, true
}

// TerminalCommandIntentMatches verifies the terminal command and outcome
// independently of the generated acknowledgement ID.
func TerminalCommandIntentMatches(journal RunJournal, commandID, outcome string) (bool, bool) {
	if journal.TerminalCommandDigest == "" {
		for _, acknowledgement := range journal.PendingCommandAcknowledgements {
			if acknowledgement.CommandID != commandID {
				continue
			}
			return acknowledgement.Outcome == outcome, true
		}
		return false, false
	}
	digest, err := terminalCommandIntentDigest(journal, commandID, outcome)
	if err != nil {
		return false, true
	}
	return digest == journal.TerminalCommandDigest, true
}

// TerminalSettlementMatches verifies the exact terminal transition and command
// acknowledgement that were committed atomically, even after outbox delivery
// removes their pending bodies.
func TerminalSettlementMatches(journal RunJournal, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement) (bool, bool) {
	if journal.TerminalSettlementDigest == "" {
		return false, false
	}
	digest, err := terminalSettlementDigest(journal, transition, acknowledgement)
	if err != nil {
		return false, true
	}
	return digest == journal.TerminalSettlementDigest, true
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
// retires unreachable delivery state, and makes local cleanup eligible only
// after the process-ownership marker has been cleared by a verified stop.
func (store *Store) ResolveTerminalForCleanup(key RunKey, verdict string, resolvedAt time.Time) (RunJournal, error) {
	if !validConclusiveTerminalVerdict(verdict) || resolvedAt.IsZero() {
		return RunJournal{}, errors.New("terminal cleanup verdict is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LocalState == "cleanup_pending" {
			if journal.TerminalVerdict != verdict {
				return errors.New("terminal verdict conflicts with journal")
			}
			if journal.HasProcessDetails() || journal.HasPendingContainment() {
				journal.LocalState = "terminal_pending"
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
		settleUnresolvedProviderActions(journal, providerActionFailureTerminal)
		// Retention may have failed before mandatory fencing. Preserve the
		// durable supervisory evidence before retiring unreachable intents.
		if journal.TerminalState != "completed" && len(journal.ControlCommandIntents) != 0 {
			journal.RetainWorkspace = true
		}
		journal.PendingEvents = nil
		journal.PendingTransitions = nil
		journal.AttemptedTransitionIDs = nil
		journal.PendingCommandAcknowledgements = nil
		// Goal evidence and usage remain deliverable under their original fence
		// after a terminal verdict. Do not retire them with ordinary v1 outbox
		// state: their receiver has its own durable idempotency receipts.
		journal.InputCommandIntent = nil
		journal.ControlCommandIntents = nil
		if !journal.HasProcessDetails() && !journal.HasPendingContainment() {
			journal.LocalState = "cleanup_pending"
		}
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
		if journal.HasProcessDetails() || journal.HasPendingContainment() {
			return errors.New("process stop evidence remains pending")
		}
		if journal.LocalState == "cleanup_pending" {
			return nil
		}
		if journal.LocalState != "terminal_pending" || !validTerminalVerdict(journal.TerminalVerdict) {
			return errors.New("journal is not cleanup eligible")
		}
		if journal.NativeUsageRecoveryRequired {
			return errors.New("native usage recovery remains pending")
		}
		if journal.InputCommandIntent != nil && (journal.InputCommandIntent.Outcome == "" || !journal.InputCommandIntent.AcknowledgementDelivered) {
			return errors.New("input command receipt is not delivered")
		}
		if journal.TerminalVerdict == TerminalVerdictAccepted {
			for _, intent := range journal.ControlCommandIntents {
				if intent.Outcome == "" || !intent.AcknowledgementDelivered {
					return errors.New("control command receipt is not delivered")
				}
			}
			if len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0 || len(journal.PendingGoalDeliveries) != 0 {
				return errors.New("accepted terminal delivery is incomplete")
			}
		} else {
			journal.PendingTransitions = nil
			journal.PendingCommandAcknowledgements = nil
			journal.ControlCommandIntents = nil
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
		if journal.InputCommandIntent != nil && slices.Contains(acknowledgementIDs, journal.InputCommandIntent.AckID) {
			if journal.InputCommandIntent.Outcome == "" {
				return errors.New("input command acknowledgement is unresolved")
			}
			journal.InputCommandIntent.AcknowledgementDelivered = true
		}
		for index := range journal.ControlCommandIntents {
			intent := &journal.ControlCommandIntents[index]
			if slices.Contains(acknowledgementIDs, intent.AckID) {
				if intent.Outcome == "" {
					return errors.New("control command acknowledgement is unresolved")
				}
				intent.AcknowledgementDelivered = true
			}
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
	if err := store.saveJournalWithCapacityGuardLocked(journal); err != nil {
		return RunJournal{}, err
	}
	return journal, nil
}

func (store *Store) mutateJournalIfChanged(key RunKey, mutate func(*RunJournal) (bool, error)) (RunJournal, error) {
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
	changed, err := mutate(&journal)
	if err != nil {
		return RunJournal{}, err
	}
	if changed {
		if err := store.saveJournalWithCapacityGuardLocked(journal); err != nil {
			return RunJournal{}, err
		}
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
	intentDigest, err := terminalIntentDigest(*journal, prepared.State, prepared.Payload)
	if err != nil {
		return err
	}
	if journal.TerminalIntentDigest != "" && journal.TerminalIntentDigest != intentDigest {
		return errors.New("terminal intent conflicts with journal")
	}
	if existing, present := authoritativeTerminalTransition(journal); present {
		if existing == nil || !sameStateTransition(*existing, prepared) {
			return errors.New("terminal transition conflicts with journal")
		}
		journal.TerminalIntentDigest = intentDigest
		return nil
	}
	if err := settleUnresolvedInputCommand(journal); err != nil {
		return err
	}
	if err := settleUnresolvedControlCommands(journal); err != nil {
		return err
	}
	settleUnresolvedProviderActions(journal, providerActionFailureTerminal)
	if prepared.State == "cancelled" {
		journal.RetainWorkspace = true
		journal.PendingTransitions = []protocol.StateTransitionRequest{prepared}
		journal.AttemptedTransitionIDs = retainAttemptedTransitions(journal.AttemptedTransitionIDs, journal.PendingTransitions)
		journal.TerminalIntentDigest = intentDigest
		setTerminalPending(journal, pendingAt, prepared.State)
		return nil
	}
	if terminalState := pendingTerminalState(journal.PendingTransitions); terminalState != "" {
		setTerminalPending(journal, pendingAt, terminalState)
		return nil
	}
	if prepared.State == "completed" {
		kind, err := terminalTaskResultKind(prepared.Payload)
		if err != nil {
			return err
		}
		journal.TerminalTaskResultKind = kind
	}
	journal.PendingTransitions = append(journal.PendingTransitions, prepared)
	journal.TerminalIntentDigest = intentDigest
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
	if CommandAcknowledgementRetired(*journal) {
		return errors.New("command acknowledgement is no longer deliverable")
	}
	if acknowledgement.RunID == "" {
		acknowledgement.RunID = journal.RunID
	}
	if acknowledgement.RunID != journal.RunID || strings.TrimSpace(acknowledgement.CommandID) == "" || !validCommandAcknowledgementOutcome(acknowledgement.Outcome) || strings.TrimSpace(acknowledgement.AckID) == "" {
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

func terminalSettlementDigest(journal RunJournal, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement) (string, error) {
	preparedTransition, err := prepareTransition(&journal, transition)
	if err != nil {
		return "", err
	}
	if acknowledgement.RunID == "" {
		acknowledgement.RunID = journal.RunID
	}
	if isZeroFence(acknowledgement.Fence) {
		acknowledgement.Fence = journal.Fence()
	}
	if acknowledgement.RunID != journal.RunID || strings.TrimSpace(acknowledgement.CommandID) == "" || !validCommandAcknowledgementOutcome(acknowledgement.Outcome) || strings.TrimSpace(acknowledgement.AckID) == "" || !sameFence(acknowledgement.Fence, journal.Fence()) {
		return "", errors.New("command acknowledgement is invalid")
	}
	encoded, err := serializeJSONWithoutHTMLEscaping(struct {
		Transition      protocol.StateTransitionRequest `json:"transition"`
		Acknowledgement protocol.CommandAcknowledgement `json:"acknowledgement"`
	}{Transition: preparedTransition, Acknowledgement: acknowledgement})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func terminalIntentDigest(journal RunJournal, stateName string, payload json.RawMessage) (string, error) {
	if !isTerminalTransitionState(stateName) || !validRawMessage(payload) {
		return "", errors.New("terminal intent is invalid")
	}
	encoded, err := serializeJSONWithoutHTMLEscaping(struct {
		Fence   protocol.Fence  `json:"fence"`
		State   string          `json:"state"`
		Payload json.RawMessage `json:"payload"`
	}{Fence: journal.Fence(), State: stateName, Payload: payload})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func terminalCommandDigest(journal RunJournal, acknowledgement protocol.CommandAcknowledgement) (string, error) {
	if acknowledgement.RunID == "" {
		acknowledgement.RunID = journal.RunID
	}
	if isZeroFence(acknowledgement.Fence) {
		acknowledgement.Fence = journal.Fence()
	}
	if acknowledgement.RunID != journal.RunID || !sameFence(acknowledgement.Fence, journal.Fence()) {
		return "", errors.New("terminal command acknowledgement does not match journal")
	}
	return terminalCommandIntentDigest(journal, acknowledgement.CommandID, acknowledgement.Outcome)
}

func terminalCommandIntentDigest(journal RunJournal, commandID, outcome string) (string, error) {
	if !validRequiredString(commandID, 4096) || !validCommandAcknowledgementOutcome(outcome) {
		return "", errors.New("terminal command intent is invalid")
	}
	encoded, err := serializeJSONWithoutHTMLEscaping(struct {
		Fence     protocol.Fence `json:"fence"`
		RunID     string         `json:"run_id"`
		CommandID string         `json:"command_id"`
		Outcome   string         `json:"outcome"`
	}{Fence: journal.Fence(), RunID: journal.RunID, CommandID: commandID, Outcome: outcome})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validCommandAcknowledgementOutcome(outcome string) bool {
	switch outcome {
	case "applied", "rejected", "failed":
		return true
	default:
		return false
	}
}

func validInputCommandIntent(intent InputCommandIntent) bool {
	return validRequiredString(intent.CommandID, 4096) && len(intent.PayloadDigest) == sha256.Size*2 && validHex(intent.PayloadDigest) && validRequiredString(intent.RunningTransitionID, 4096) && validRequiredString(intent.AckID, 4096) && intent.EventSequenceBarrier >= 0 && validInputCommandOutcome(intent.Outcome) && (!intent.AcknowledgementDelivered || intent.Outcome != "")
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
	if err := store.readRunJournalJSONWithLimit(store.journalPath(key), &journal, maxJournalFileBytes); err != nil {
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
	data, err := serializeRunJournal(journal)
	if err != nil || len(data) > maxJournalFileBytes {
		return ErrProviderActionCapacity
	}
	writer := store.atomicWrite
	if writer == nil {
		writer = writeAtomic
	}
	if err := writer(store.journalPath(journal.Key()), data); err != nil {
		return errors.New("write run journal")
	}
	return nil
}

func (store *Store) saveJournalWithCapacityGuardLocked(journal RunJournal) error {
	if err := validateJournalSaveCapacity(journal); err != nil {
		return err
	}
	return store.saveJournalLocked(journal)
}

func (store *Store) readJSON(path string, resource string, destination any) error {
	return store.readJSONWithLimit(path, resource, destination, maxStateFileBytes)
}

func (store *Store) readJSONWithLimit(path string, resource string, destination any, limit int) error {
	return store.readJSONWithLimitAndTags(path, resource, destination, limit, nil)
}

func (store *Store) readRunJournalJSONWithLimit(path string, destination any, limit int) error {
	return store.readJSONWithLimitAndTags(path, "run journal", destination, limit, reflect.TypeOf(RunJournal{}))
}

func (store *Store) readJSONWithLimitAndTags(path string, resource string, destination any, limit int, rootType reflect.Type) error {
	data, err := readLimited(path, limit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &NotFoundError{Resource: resource}
		}
		return errors.New("read " + resource)
	}
	if err := protocol.RejectDuplicateJSONMembers(data); err != nil {
		return errors.New("decode " + resource)
	}
	if rootType != nil && rejectCaseInsensitiveJSONTagAliases(data, rootType) != nil {
		return errors.New("decode " + resource)
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

// rejectCaseInsensitiveJSONTagAliases closes encoding/json's compatibility
// behavior at the durable journal boundary. encoding/json accepts keys such as
// "PROVIDER_ACTION_INTENTS" for the exact "provider_action_intents" tag;
// durable authority must not let such an alias silently win over a later
// field. Unknown exact members remain the responsibility of
// Decoder.DisallowUnknownFields, while RawMessage and map fields stay open by
// design.
func rejectCaseInsensitiveJSONTagAliases(data []byte, rootType reflect.Type) error {
	var value json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return rejectJSONTagAliases(value, rootType)
}

func rejectJSONTagAliases(value json.RawMessage, valueType reflect.Type) error {
	valueType = dereferenceJSONTagType(valueType)
	if valueType == nil || valueType == reflect.TypeOf(json.RawMessage(nil)) {
		return nil
	}
	switch valueType.Kind() {
	case reflect.Array, reflect.Slice:
		var items []json.RawMessage
		if err := json.Unmarshal(value, &items); err != nil {
			return nil
		}
		for _, item := range items {
			if err := rejectJSONTagAliases(item, valueType.Elem()); err != nil {
				return err
			}
		}
	case reflect.Struct:
		fields := exactJSONTagFields(valueType)
		if len(fields) == 0 {
			return nil
		}
		var members map[string]json.RawMessage
		if err := json.Unmarshal(value, &members); err != nil {
			return nil
		}
		for member, child := range members {
			fieldType, exact := fields[member]
			if !exact {
				for tag := range fields {
					if strings.EqualFold(member, tag) {
						return fmt.Errorf("JSON member %q aliases exact tag %q", member, tag)
					}
				}
				continue
			}
			if err := rejectJSONTagAliases(child, fieldType); err != nil {
				return err
			}
		}
	}
	return nil
}

func dereferenceJSONTagType(valueType reflect.Type) reflect.Type {
	for valueType != nil && (valueType.Kind() == reflect.Pointer || valueType.Kind() == reflect.Interface) {
		valueType = valueType.Elem()
	}
	return valueType
}

func exactJSONTagFields(valueType reflect.Type) map[string]reflect.Type {
	valueType = dereferenceJSONTagType(valueType)
	if valueType == nil || valueType.Kind() != reflect.Struct {
		return nil
	}
	fields := make(map[string]reflect.Type)
	for index := 0; index < valueType.NumField(); index++ {
		field := valueType.Field(index)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if name == "" && field.Anonymous {
			nested := exactJSONTagFields(field.Type)
			if len(nested) != 0 {
				for nestedName, nestedType := range nested {
					fields[nestedName] = nestedType
				}
				continue
			}
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}

func (store *Store) writeJSON(path string, value any, resource string) error {
	return store.writeJSONWithLimit(path, value, resource, maxStateFileBytes)
}

func (store *Store) writeJSONWithLimit(path string, value any, resource string, limit int) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > limit {
		return errors.New("encode " + resource)
	}
	writer := store.atomicWrite
	if writer == nil {
		writer = writeAtomic
	}
	if err := writer(path, data); err != nil {
		return errors.New("write " + resource)
	}
	return nil
}

// serializeRunJournal is the sole durable representation used for run journal
// writes and byte-capacity checks. Other state files intentionally retain the
// default json.Marshal representation.
func serializeRunJournal(journal RunJournal) ([]byte, error) {
	return serializeJSONWithoutHTMLEscaping(journal)
}

func serializeJSONWithoutHTMLEscaping(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
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
	if err := renameStateFile(temporaryPath, path); err != nil {
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

func sameContainmentAuthority(left, right *authority.Supervisor) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameContainmentHandoff(left, right *authority.SupervisorHandoff) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func validateJournal(journal RunJournal) error {
	if err := validateKey(journal.Key()); err != nil {
		return err
	}
	if !validRequiredString(journal.RuntimeKey, 4096) || !validRequiredString(journal.RuntimeID, 4096) || journal.ClaimedRuntimeEpoch <= 0 || !validRequiredString(journal.ClaimID, 4096) || !validRequiredString(journal.LocalState, 256) || len(journal.WorkspacePath) > 32768 || !validRequiredString(journal.WorkspaceBindingKey, 4096) || journal.PID < 0 || len(journal.ProcessIdentity) > 4096 || journal.LastEventSequence < 0 {
		return errors.New("run journal is invalid")
	}
	if journal.PID == 0 && (!journal.StartedAt.IsZero() || journal.ProcessIdentity != "" || journal.ContainmentAuthority != nil) {
		return errors.New("run journal process details are invalid")
	}
	if journal.PID > 0 && (journal.StartedAt.IsZero() || !validRequiredString(journal.ProcessIdentity, 4096)) {
		return errors.New("run journal process details are invalid")
	}
	if journal.ContainmentAuthority != nil {
		if err := journal.ContainmentAuthority.Validate(); err != nil ||
			journal.ContainmentAuthority.TargetPID != journal.PID ||
			journal.ContainmentAuthority.TargetIdentity != journal.ProcessIdentity {
			return errors.New("run journal containment authority is invalid")
		}
	}
	if journal.ContainmentHandoff != nil {
		if journal.ContainmentAuthority != nil || journal.HasProcessDetails() || !journal.hasClaimGrant() {
			return errors.New("run journal containment handoff is invalid")
		}
		if err := journal.ContainmentHandoff.Validate(); err != nil {
			return errors.New("run journal containment handoff is invalid")
		}
	}
	if (strings.TrimSpace(journal.LeaseToken) == "") != journal.LeaseExpiresAt.IsZero() || len(journal.LeaseToken) > 65536 || !validWork(journal.Work) {
		return errors.New("run journal is invalid")
	}
	if !validTerminalState(journal) {
		return errors.New("run journal is invalid")
	}
	if journal.NativeUsageTerminalRecovery != nil && !journal.NativeUsageRecoveryRequired {
		return errors.New("run journal is invalid")
	}
	if journal.DroppedOutputChunks < 0 || journal.DroppedOutputBytes < 0 {
		return errors.New("run journal is invalid")
	}
	if !journal.hasClaimGrant() && (journal.PID != 0 || !journal.StartedAt.IsZero() || len(journal.PendingEvents) != 0 || journal.DroppedOutputChunks != 0 || journal.DroppedOutputBytes != 0 || len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0) {
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
	if err := validateGoalDeliveries(journal); err != nil {
		return err
	}
	if journal.NativeUsageObservation != nil {
		if err := journal.NativeUsageObservation.Validate(); err != nil {
			return err
		}
	}
	if intent := journal.InputCommandIntent; intent != nil {
		if !validInputCommandIntent(*intent) || intent.EventSequenceBarrier > journal.LastEventSequence {
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
	if err := validateControlCommandIntents(journal); err != nil {
		return err
	}
	if err := validateProviderActionIntents(journal); err != nil {
		return err
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
	if !validNativeUsageTerminalRecovery(journal.NativeUsageTerminalRecovery) {
		return false
	}
	if journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" {
		return !hasPendingTerminalTransition(journal.PendingTransitions) && journal.TerminalPendingAt.IsZero() && journal.TerminalState == "" && journal.TerminalTaskResultKind == "" && journal.TerminalIntentDigest == "" && journal.TerminalCommandDigest == "" && journal.TerminalSettlementDigest == "" && journal.TerminalVerdict == "" && journal.TerminalResolvedAt.IsZero()
	}
	if journal.TerminalPendingAt.IsZero() || !isTerminalTransitionState(journal.TerminalState) {
		return false
	}
	if journal.TerminalTaskResultKind != "" && !validTaskResultKind(journal.TerminalTaskResultKind) {
		return false
	}
	if journal.TerminalIntentDigest != "" && (len(journal.TerminalIntentDigest) != sha256.Size*2 || !validHex(journal.TerminalIntentDigest)) {
		return false
	}
	if journal.TerminalCommandDigest != "" && (len(journal.TerminalCommandDigest) != sha256.Size*2 || !validHex(journal.TerminalCommandDigest)) {
		return false
	}
	if journal.TerminalSettlementDigest != "" && (len(journal.TerminalSettlementDigest) != sha256.Size*2 || !validHex(journal.TerminalSettlementDigest)) {
		return false
	}
	if journal.LocalState == "cleanup_pending" {
		if !validTerminalVerdict(journal.TerminalVerdict) || journal.TerminalResolvedAt.IsZero() {
			return false
		}
		// Conclusive v1 terminal delivery may retire ordinary items while late
		// Goal usage/evidence remains retriable under its original fence.
		return len(journal.PendingEvents) == 0 && len(journal.PendingTransitions) == 0 && len(journal.PendingCommandAcknowledgements) == 0 && len(journal.AttemptedTransitionIDs) == 0
	}
	if journal.TerminalVerdict == "" {
		return journal.TerminalResolvedAt.IsZero()
	}
	return validTerminalVerdict(journal.TerminalVerdict) && !journal.TerminalResolvedAt.IsZero()
}

func terminalTaskResultKind(payload json.RawMessage) (protocol.TaskResultKind, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return "", errors.New("completed terminal payload is invalid")
	}
	rawResult, present := envelope["task_result"]
	if !present {
		return "", nil
	}
	var result struct {
		Kind protocol.TaskResultKind `json:"kind"`
	}
	if err := json.Unmarshal(rawResult, &result); err != nil || !validTaskResultKind(result.Kind) {
		return "", errors.New("completed terminal task result kind is invalid")
	}
	return result.Kind, nil
}

func validTaskResultKind(kind protocol.TaskResultKind) bool {
	switch kind {
	case protocol.TaskResultProgress,
		protocol.TaskResultCandidateCompletion,
		protocol.TaskResultBlocked,
		protocol.TaskResultRepairRequired,
		protocol.TaskResultReplanRequired,
		protocol.TaskResultFailed,
		protocol.TaskResultPlanProposed:
		return true
	default:
		return false
	}
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
	return IsConclusiveTerminalVerdict(verdict)
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
	return len(message) == 0 || (json.Valid(message) && protocol.RejectDuplicateJSONMembers(message) == nil)
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
	return slices.Contains(attempted, transitionID)
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
