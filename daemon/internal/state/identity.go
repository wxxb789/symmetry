package state

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

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

// NewDaemonInstanceID returns a newly generated RFC 4122 version 4 UUID for a
// single daemon process lifetime.
func NewDaemonInstanceID() (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(rand.Reader, value[:]); err != nil {
		return "", fmt.Errorf("generate daemon instance ID: %w", err)
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
		return "", fmt.Errorf("generate machine token: %w", err)
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
		return fmt.Errorf("remove enrollment intent: %w", err)
	}
	return syncDirectory(store.dir)
}

// LoadIdentity returns the enrolled machine credentials. An absent enrollment
// returns a typed not-found error.
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
	if journal.ContainmentCgroup != nil || journal.LinuxCloneIntent != nil {
		if journal.LeaseToken != grant.LeaseToken || !journal.LeaseExpiresAt.Equal(grant.LeaseExpiresAt) || !sameWork(journal.Work, grant.Work) {
			return RunJournal{}, errors.New("claim grant cannot replace a v2 run authority fence")
		}
		if err := store.saveJournalWithCapacityGuardLocked(journal); err != nil {
			return RunJournal{}, err
		}
		return journal, nil
	}
	if journal.ContainmentCgroupReleaseProof != nil {
		return RunJournal{}, errors.New("claim grant cannot change after v2 containment release")
	}
	if journal.ContainmentCgroupPreGateReleaseProof != nil {
		return RunJournal{}, errors.New("claim grant cannot change after v2 pre-gate release")
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

func (store *Store) identityPath() string {
	return filepath.Join(store.dir, identityFileName)
}

func (store *Store) enrollmentPath() string {
	return filepath.Join(store.dir, enrollmentFileName)
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
