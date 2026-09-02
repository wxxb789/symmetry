// Package state persists machine-local daemon recovery state.
package state

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
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
	maxStateFileBytes = 1 << 20
	identityFileName  = "identity.json"
	runsDirectoryName = "runs"
	lockFileName      = ".symmetry-daemon.lock"
	journalFilePrefix = "journal-"
	journalFileSuffix = ".json"
	atomicTempPrefix  = ".symmetry-state-"
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
	Work                           protocol.Work                     `json:"work"`
	WorkspacePath                  string                            `json:"workspace_path"`
	WorkspaceBindingKey            string                            `json:"workspace_binding_key"`
	PID                            int                               `json:"pid"`
	ProcessIdentity                string                            `json:"process_identity"`
	StartedAt                      time.Time                         `json:"started_at"`
	LastEventSequence              int64                             `json:"last_event_sequence"`
	PendingEvents                  []protocol.RunEvent               `json:"pending_events"`
	PendingTransitions             []protocol.StateTransitionRequest `json:"pending_transitions"`
	PendingCommandAcknowledgements []protocol.CommandAcknowledgement `json:"pending_command_acknowledgements"`
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
	if err := store.removeOwnedTempsLocked(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(store.runsDir())
	if err != nil {
		return nil, errors.New("list run journals")
	}
	result := make([]RunJournal, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isJournalFile(entry.Name()) {
			continue
		}
		var journal RunJournal
		if err := store.readJSON(filepath.Join(store.runsDir(), entry.Name()), "run journal", &journal); err != nil {
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

// QueueEvent durably enqueues an idempotent event before its HTTP request.
func (store *Store) QueueEvent(key RunKey, event protocol.RunEvent) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.Kind) == "" || event.Sequence != journal.LastEventSequence+1 || event.OccurredAt.IsZero() || !validRawMessage(event.Payload) {
			return errors.New("event is invalid")
		}
		journal.PendingEvents = append(journal.PendingEvents, event)
		journal.LastEventSequence = event.Sequence
		return nil
	})
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
		return queueTransition(journal, transition)
	})
}

// QueueTerminalTransition atomically makes a terminal transition durable and
// marks the local process terminal-pending before any HTTP request is sent.
func (store *Store) QueueTerminalTransition(key RunKey, transition protocol.StateTransitionRequest) (RunJournal, error) {
	if !isTerminalTransitionState(transition.State) {
		return RunJournal{}, errors.New("terminal transition state is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := queueTransition(journal, transition); err != nil {
			return err
		}
		journal.LocalState = "terminal_pending"
		return nil
	})
}

// MarkTransitionsDelivered removes successfully applied transitions by ID.
func (store *Store) MarkTransitionsDelivered(key RunKey, transitionIDs []string) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.PendingTransitions = removeTransitions(journal.PendingTransitions, transitionIDs)
		return nil
	})
}

// QueueCommandAcknowledgement durably queues an acknowledgement before its HTTP
// request. A zero fence is populated from the current journal.
func (store *Store) QueueCommandAcknowledgement(key RunKey, acknowledgement protocol.CommandAcknowledgement) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
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
		journal.PendingCommandAcknowledgements = append(journal.PendingCommandAcknowledgements, acknowledgement)
		return nil
	})
}

// MarkCommandAcknowledgementsDelivered removes acknowledgements confirmed by the
// control plane, keyed by their caller-generated ack IDs.
func (store *Store) MarkCommandAcknowledgementsDelivered(key RunKey, acknowledgementIDs []string) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.PendingCommandAcknowledgements = removeAcknowledgements(journal.PendingCommandAcknowledgements, acknowledgementIDs)
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
	if !journal.hasClaimGrant() {
		return errors.New("journal has no claim grant")
	}
	if strings.TrimSpace(transition.TransitionID) == "" || strings.TrimSpace(transition.State) == "" || !validRawMessage(transition.Payload) {
		return errors.New("state transition is invalid")
	}
	if isZeroFence(transition.Fence) {
		transition.Fence = journal.Fence()
	}
	if !sameFence(transition.Fence, journal.Fence()) {
		return errors.New("state transition fence does not match journal")
	}
	journal.PendingTransitions = append(journal.PendingTransitions, transition)
	return nil
}

func isTerminalTransitionState(state string) bool {
	switch state {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (store *Store) loadJournalLocked(key RunKey) (RunJournal, error) {
	var journal RunJournal
	if err := store.readJSON(store.journalPath(key), "run journal", &journal); err != nil {
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
	return store.writeJSON(store.journalPath(journal.Key()), journal, "run journal")
}

func (store *Store) readJSON(path string, resource string, destination any) error {
	data, err := readLimited(path)
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
	data, err := json.Marshal(value)
	if err != nil || len(data) > maxStateFileBytes {
		return errors.New("encode " + resource)
	}
	if err := writeAtomic(path, data); err != nil {
		return errors.New("write " + resource)
	}
	return nil
}

func (store *Store) removeOwnedTempsLocked() error {
	entries, err := os.ReadDir(store.runsDir())
	if err != nil {
		return errors.New("list run journal directory")
	}
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

func readLimited(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxStateFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxStateFileBytes {
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
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if !validAcknowledgement(acknowledgement, journal) {
			return errors.New("run journal is invalid")
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
