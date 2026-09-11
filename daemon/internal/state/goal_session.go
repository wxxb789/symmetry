package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	goalSessionsDirectoryName       = "sessions"
	goalSessionFilePrefix           = "session-"
	goalSessionFileSuffix           = ".json"
	goalSessionLineageFilePrefix    = "lineage-"
	goalSessionLineageFileSuffix    = ".json"
	goalSessionSchemaVersion        = 1
	goalSessionLineageSchemaVersion = 1
	goalSessionConsumedStopLimit    = 4096
)

const (
	// Goal session lifecycle states mirror the durable harness_sessions state
	// values in docs/design/data.md.
	GoalSessionStateAvailable   = "available"
	GoalSessionStateBusy        = "busy"
	GoalSessionStateUnavailable = "unavailable"
	GoalSessionStateClosed      = "closed"

	// Launch states are local recovery facts. They are deliberately separate
	// from SessionState, which is the state exposed to the control plane.
	GoalSessionLaunchStateIntent    = "intent"
	GoalSessionLaunchStateLaunching = "launching"
	GoalSessionLaunchStateAttached  = "attached"
	GoalSessionLaunchStateUncertain = "uncertain"
	GoalSessionLaunchStateClosed    = "closed"

	GoalSessionModeFresh   = "fresh"
	GoalSessionModeResume  = "resume"
	GoalSessionModeHandoff = "handoff"
)

const (
	// GoalSessionLineageStateFree means that no local launch reservation remains
	// for the lineage. Every other state is a barrier for a replacement launch.
	GoalSessionLineageStateFree      GoalSessionLineageState = "free"
	GoalSessionLineageStateReserved  GoalSessionLineageState = "reserved"
	GoalSessionLineageStateLaunching GoalSessionLineageState = "launching"
	GoalSessionLineageStateAttached  GoalSessionLineageState = "attached"
	GoalSessionLineageStateUncertain GoalSessionLineageState = "uncertain"
)

var (
	// ErrGoalSessionConflict means that an idempotent launch replay contains
	// different identity or launch data, or that another active session already
	// owns the requested local handle.
	ErrGoalSessionConflict = errors.New("goal session journal conflict")
	// ErrGoalSessionUncertain means that native launch effects cannot be proved
	// from the local journal. Callers must reconcile before starting again.
	ErrGoalSessionUncertain = errors.New("goal session launch is uncertain")
	// ErrGoalSessionOwnerMismatch means that a session is being used by a
	// different machine/runtime/daemon owner.
	ErrGoalSessionOwnerMismatch = errors.New("goal session owner mismatch")
	// ErrGoalSessionVersionMismatch means that the installed native harness or
	// adapter does not match the version recorded at launch.
	ErrGoalSessionVersionMismatch = errors.New("goal session version mismatch")
	// ErrGoalSessionWorkspaceMismatch means that a session is being resumed in
	// a different machine-local workspace.
	ErrGoalSessionWorkspaceMismatch = errors.New("goal session workspace mismatch")
	// ErrGoalSessionClosed means that a closed session cannot be resumed.
	ErrGoalSessionClosed = errors.New("goal session is closed")
	// ErrGoalSessionNotAttached means that a native identity has not yet been
	// durably recorded for the requested resume operation.
	ErrGoalSessionNotAttached = errors.New("goal session native handle is not attached")
	// ErrGoalSessionCompatibilityIncomplete means that a reconciliation attempt
	// did not provide an exact recorded identity. Reconciliation never treats
	// zero values as wildcards.
	ErrGoalSessionCompatibilityIncomplete = errors.New("goal session compatibility is incomplete")
)

// GoalSessionKey identifies a local native session without exposing the raw
// native handle to the control plane. A goal may own more than one session.
type GoalSessionKey struct {
	GoalID        string
	LocalHandleID string
}

// GoalSessionLineageKey is the identity shared by retries of one Goal task.
// GoalRevision, RunID and Generation are deliberately excluded: a new
// revision or fenced run must not bypass an unresolved native launch from the
// same Goal/WorkItem/Task lineage.
type GoalSessionLineageKey struct {
	GoalID     string `json:"goal_id"`
	WorkItemID string `json:"work_item_id,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
}

// GoalSessionLineageState describes the strongest local recovery fact found
// for a lineage. The state is derived from durable journals, not caller input.
type GoalSessionLineageState string

// GoalSessionLineageStatus is the durable launch barrier returned to admission
// code. SessionKeys contains every currently blocking local session, sorted by
// stable key. An empty/free status is the only result that permits a new
// lineage launch.
type GoalSessionLineageStatus struct {
	Lineage     GoalSessionLineageKey
	State       GoalSessionLineageState
	Blocking    bool
	SessionKeys []GoalSessionKey
}

// BlocksNewLaunch reports whether a new run/generation would risk duplicating
// an external native effect for this lineage.
func (status GoalSessionLineageStatus) BlocksNewLaunch() bool {
	return status.Blocking
}

type goalSessionLineageIndex struct {
	SchemaVersion int                       `json:"schema_version"`
	Lineage       GoalSessionLineageKey     `json:"lineage"`
	Dirty         bool                      `json:"dirty,omitempty"`
	Entries       []goalSessionLineageEntry `json:"entries"`
}

type goalSessionLineageEntry struct {
	SessionKey GoalSessionKey          `json:"session_key"`
	State      GoalSessionLineageState `json:"state"`
	UpdatedAt  time.Time               `json:"updated_at"`
}

// GoalSessionLaunchIntent is the exact admission and local ownership data
// persisted before starting a native session. It contains no native session
// ID or native filename; those are written only after native creation.
type GoalSessionLaunchIntent struct {
	LaunchIntentID string `json:"launch_intent_id"`
	GoalID         string `json:"goal_id"`
	GoalRevision   int64  `json:"goal_revision"`
	WorkItemID     string `json:"work_item_id,omitempty"`
	TaskID         string `json:"task_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	Generation     int64  `json:"generation,omitempty"`
	AdmissionID    string `json:"admission_id,omitempty"`
	// HandoffSourceRunID records the completed Run whose accepted context is
	// continuing in a newly created local native session. It is provenance,
	// never a native-session handle, and only applies to handoff admissions.
	HandoffSourceRunID     string `json:"handoff_source_run_id,omitempty"`
	LocalHandleID          string `json:"local_handle_id"`
	BindingID              string `json:"binding_id,omitempty"`
	OwnerID                string `json:"owner_id,omitempty"`
	MachineID              string `json:"machine_id,omitempty"`
	RuntimeID              string `json:"runtime_id,omitempty"`
	RuntimeEpoch           int64  `json:"runtime_epoch,omitempty"`
	DaemonInstanceID       string `json:"daemon_instance_id,omitempty"`
	HarnessKind            string `json:"harness_kind"`
	HarnessVersion         string `json:"harness_version"`
	AdapterVersion         string `json:"adapter_version"`
	AdapterProtocolVersion int    `json:"adapter_protocol_version"`
	WorkspaceFingerprint   string `json:"workspace_fingerprint"`
	WorkspacePath          string `json:"workspace_path,omitempty"`
	// WorkspaceOwnerRunKey permanently identifies the Run that materialized the
	// retained worktree. Resume changes the current execution Run, never this
	// original workspace owner.
	WorkspaceOwnerRunKey WorkspaceOwnerRunKey `json:"workspace_owner_run_key,omitempty"`
	// RepositoryResourceID is the canonical Control repository identity bound to
	// the workspace fingerprint. It is required for a retained native resume.
	RepositoryResourceID string `json:"repository_resource_id,omitempty"`
	SessionMode          string `json:"session_mode"`
}

// WorkspaceOwnerRunKey identifies the immutable original owner of a retained
// workspace. It deliberately remains separate from GoalSessionLaunchIntent's
// current RunID and Generation, which change on a later resume admission.
type WorkspaceOwnerRunKey struct {
	RunID      string `json:"run_id,omitempty"`
	Generation int64  `json:"generation,omitempty"`
}

// GoalSessionJournal is a machine-local recovery record for one native Goal
// session. NativeSessionID and NativeSessionFilename are intentionally local
// fields: callers must use ControlProjection when building a control request.
// They are never part of the control-plane projection.
type GoalSessionJournal struct {
	GoalSessionLaunchIntent
	SchemaVersion              int       `json:"schema_version"`
	SessionState               string    `json:"session_state"`
	LaunchState                string    `json:"launch_state"`
	LaunchAttempted            bool      `json:"launch_attempted,omitempty"`
	LaunchAttemptedAt          time.Time `json:"launch_attempted_at,omitempty"`
	RecoveryRequired           bool      `json:"recovery_required,omitempty"`
	NativeSessionID            string    `json:"native_session_id,omitempty"`
	NativeSessionFilename      string    `json:"native_session_filename,omitempty"`
	ControlSessionID           string    `json:"control_session_id,omitempty"`
	ControlAttachmentReceiptID string    `json:"control_attachment_receipt_id,omitempty"`
	// ControlAttachmentLineage preserves the first verified Control attachment
	// while current attachment fields rotate for each resumed Run.
	ControlAttachmentLineage *GoalSessionControlAttachmentLineage `json:"control_attachment_lineage,omitempty"`
	// ResumeSourceStopCertificate is the exact available certificate consumed by
	// the current resume Run. It makes a retry prove the same predecessor rather
	// than treating a matching target alone as an idempotency key.
	ResumeSourceStopCertificate *GoalSessionStopCertificate `json:"resume_source_stop_certificate,omitempty"`
	// ConsumedStopCertificates retains every predecessor stop certificate that
	// a later rebind consumed. Delayed delivery replays may acknowledge only an
	// exact member of this history and cannot mutate the current Run.
	ConsumedStopCertificates *GoalSessionConsumedStopCertificateHistory `json:"consumed_stop_certificates,omitempty"`
	// ResumeStartPending proves a rebind has completed but no native Start
	// boundary has yet been crossed. It survives attach Ready/readback crashes;
	// MarkGoalSessionResumeStartAttempted clears it immediately before Start.
	ResumeStartPending bool `json:"resume_start_pending,omitempty"`
	// ResumeNativeStopKnown proves that the current retained pi turn is known
	// stopped locally even if its new Control attachment has not been read back
	// yet. It is not a Control release receipt.
	ResumeNativeStopKnown bool `json:"resume_native_stop_known,omitempty"`
	// ResumeTerminalReason records a known terminal classification before the
	// Run terminal outbox is written. It only applies to a proven stopped
	// retained resume and prevents restart recovery from weakening it.
	ResumeTerminalReason string                      `json:"resume_terminal_reason,omitempty"`
	StopCertificate      *GoalSessionStopCertificate `json:"stop_certificate,omitempty"`
	UncertainReason      string                      `json:"uncertain_reason,omitempty"`
	CreatedAt            time.Time                   `json:"created_at"`
	UpdatedAt            time.Time                   `json:"updated_at"`
}

// GoalSessionControlAttachmentLineage is the immutable first fenced Control
// attachment for a retained native session. It survives resume rebinds so a
// later Run cannot substitute a different Control session as its provenance.
type GoalSessionControlAttachmentLineage struct {
	Original GoalSessionControlAttachment `json:"original"`
	Current  GoalSessionControlAttachment `json:"current"`
}

// GoalSessionControlAttachment records one immutable Control attachment
// receipt. Original never changes; Current advances only after a new resume
// Run's attachment has been durably persisted.
type GoalSessionControlAttachment struct {
	RunID       string `json:"run_id"`
	Generation  int64  `json:"generation"`
	TaskID      string `json:"task_id,omitempty"`
	AdmissionID string `json:"admission_id,omitempty"`
	SessionID   string `json:"session_id"`
	BindingID   string `json:"binding_id"`
	ReceiptID   string `json:"receipt_id"`
}

// GoalSessionStopCertificate is the durable local proof that Control accepted
// a precise stopped attachment. It survives deletion of the completed Run
// journal, which intentionally does not own retained-session lifecycle.
type GoalSessionStopCertificate struct {
	RunID          string `json:"run_id"`
	Generation     int64  `json:"generation"`
	SessionID      string `json:"session_id"`
	LocalHandleID  string `json:"local_handle_id"`
	BindingID      string `json:"binding_id"`
	DeliveryDigest string `json:"delivery_digest"`
	ReceiptID      string `json:"receipt_id"`
}

// GoalSessionConsumedStopCertificateHistory is append-only predecessor receipt
// provenance. It is intentionally retained after later stop cycles because an
// old RunJournal outbox can replay after a newer Run has completed.
type GoalSessionConsumedStopCertificateHistory struct {
	Certificates []GoalSessionStopCertificate `json:"certificates"`
}

// GoalSessionHandle is the local native identity obtained after a successful
// native launch. It must not be serialized into control-plane payloads.
type GoalSessionHandle struct {
	NativeSessionID       string
	NativeSessionFilename string
}

// GoalSessionCompatibility is the set of values that must match before a
// retained native session can be inspected or explicitly reattached.
type GoalSessionCompatibility struct {
	OwnerID                string
	MachineID              string
	RuntimeID              string
	RuntimeEpoch           int64
	DaemonInstanceID       string
	HarnessKind            string
	HarnessVersion         string
	AdapterVersion         string
	AdapterProtocolVersion int
	WorkspaceFingerprint   string
	RepositoryResourceID   string
}

// GoalSessionResumeRebind is the complete CAS input for moving a stopped,
// retained native session to a newly admitted execution. ExactStopCertificate
// identifies the completed execution; the target fields identify the only Run
// that may consume it.
type GoalSessionResumeRebind struct {
	RunID                string
	Generation           int64
	TaskID               string
	AdmissionID          string
	BindingID            string
	ExactStopCertificate GoalSessionStopCertificate
	Compatibility        GoalSessionCompatibility
}

// GoalSessionControlProjection is safe to put on the control wire. In
// particular it contains local_handle_id but no raw native ID or filename.
type GoalSessionControlProjection struct {
	GoalID                 string `json:"goal_id"`
	GoalRevision           int64  `json:"goal_revision"`
	WorkItemID             string `json:"work_item_id,omitempty"`
	TaskID                 string `json:"task_id,omitempty"`
	RunID                  string `json:"run_id,omitempty"`
	Generation             int64  `json:"generation,omitempty"`
	AdmissionID            string `json:"admission_id,omitempty"`
	LocalHandleID          string `json:"local_handle_id"`
	OwnerID                string `json:"owner_id,omitempty"`
	MachineID              string `json:"machine_id,omitempty"`
	RuntimeID              string `json:"runtime_id,omitempty"`
	RuntimeEpoch           int64  `json:"runtime_epoch,omitempty"`
	HarnessKind            string `json:"harness_kind"`
	HarnessVersion         string `json:"harness_version"`
	AdapterVersion         string `json:"adapter_version"`
	AdapterProtocolVersion int    `json:"adapter_protocol_version"`
	WorkspaceFingerprint   string `json:"workspace_fingerprint"`
	SessionState           string `json:"session_state"`
	LaunchState            string `json:"launch_state"`
	RecoveryRequired       bool   `json:"recovery_required,omitempty"`
}

// Key returns the stable key used for the session journal filename.
func (intent GoalSessionLaunchIntent) Key() GoalSessionKey {
	return GoalSessionKey{GoalID: intent.GoalID, LocalHandleID: intent.LocalHandleID}
}

// LineageKey returns the retry identity for this Goal session intent. It does
// not include the fenced run generation because retries across generations are
// exactly the case the unresolved-launch barrier protects.
func (intent GoalSessionLaunchIntent) LineageKey() GoalSessionLineageKey {
	return GoalSessionLineageKey{GoalID: intent.GoalID, WorkItemID: intent.WorkItemID, TaskID: intent.TaskID}
}

// Key returns the stable key used for the session journal filename.
func (journal GoalSessionJournal) Key() GoalSessionKey {
	return journal.GoalSessionLaunchIntent.Key()
}

// LineageKey returns the retry identity recorded in the journal.
func (journal GoalSessionJournal) LineageKey() GoalSessionLineageKey {
	return journal.GoalSessionLaunchIntent.LineageKey()
}

// Intent returns the launch identity without any post-launch native fields.
func (journal GoalSessionJournal) Intent() GoalSessionLaunchIntent {
	return journal.GoalSessionLaunchIntent
}

// Compatibility returns the recorded owner, adapter and workspace binding for
// an explicit resume or reconciliation check.
func (journal GoalSessionJournal) Compatibility() GoalSessionCompatibility {
	return GoalSessionCompatibility{
		OwnerID: journal.OwnerID, MachineID: journal.MachineID, RuntimeID: journal.RuntimeID,
		RuntimeEpoch: journal.RuntimeEpoch, DaemonInstanceID: journal.DaemonInstanceID,
		HarnessKind: journal.HarnessKind, HarnessVersion: journal.HarnessVersion,
		AdapterVersion: journal.AdapterVersion, AdapterProtocolVersion: journal.AdapterProtocolVersion,
		WorkspaceFingerprint: journal.WorkspaceFingerprint, RepositoryResourceID: journal.RepositoryResourceID,
	}
}

// NeedsReconciliation reports whether starting a replacement could duplicate
// an unverified native launch.
func (journal GoalSessionJournal) NeedsReconciliation() bool {
	return journal.RecoveryRequired || (journal.LaunchAttempted && journal.LaunchState != GoalSessionLaunchStateAttached && journal.LaunchState != GoalSessionLaunchStateClosed)
}

// HasVerifiedControlAttachment reports whether the attachment has the
// immutable Control receipt required for release or native-session reuse.
func (journal GoalSessionJournal) HasVerifiedControlAttachment() bool {
	return journal.LaunchState == GoalSessionLaunchStateAttached && journal.NativeSessionID != "" &&
		validGoalSessionUUID(journal.ControlSessionID) && validGoalSessionUUID(journal.BindingID) &&
		validGoalSessionUUID(journal.ControlAttachmentReceiptID) && journal.ControlAttachmentLineage != nil &&
		validGoalSessionControlAttachmentLineage(*journal.ControlAttachmentLineage) &&
		matchesGoalSessionControlAttachment(journal.ControlAttachmentLineage.Current, journal)
}

// IsLegacyControlAttachment identifies a journal written before attachment
// receipts existed. It remains readable for recovery, but cannot resume or
// release until readback supplies the immutable receipt.
func (journal GoalSessionJournal) IsLegacyControlAttachment() bool {
	return journal.LaunchState == GoalSessionLaunchStateAttached && journal.NativeSessionID != "" &&
		validGoalSessionUUID(journal.ControlSessionID) && validGoalSessionUUID(journal.BindingID) &&
		journal.ControlAttachmentLineage == nil
}

// IsUncertainLaunch reports whether the record explicitly represents an
// uncertain native launch.
func (journal GoalSessionJournal) IsUncertainLaunch() bool {
	return journal.LaunchState == GoalSessionLaunchStateUncertain || journal.NeedsReconciliation()
}

// HasKnownRetainedResumeNativeStop reports a local proof that the current
// retained resume turn stopped. It intentionally says nothing about whether
// Control accepted the corresponding stop receipt.
func (journal GoalSessionJournal) HasKnownRetainedResumeNativeStop() bool {
	return journal.ResumeNativeStopKnown && journal.SessionMode == GoalSessionModeResume &&
		journal.ResumeSourceStopCertificate != nil && journal.StopCertificate == nil &&
		journal.SessionState == GoalSessionStateUnavailable && journal.LaunchState == GoalSessionLaunchStateAttached &&
		journal.NativeSessionID != "" && !journal.NeedsReconciliation()
}

// ControlProjection strips all machine-local native identity from a journal.
func (journal GoalSessionJournal) ControlProjection() GoalSessionControlProjection {
	return GoalSessionControlProjection{
		GoalID: journal.GoalID, GoalRevision: journal.GoalRevision, WorkItemID: journal.WorkItemID,
		TaskID: journal.TaskID, RunID: journal.RunID, Generation: journal.Generation,
		AdmissionID: journal.AdmissionID, LocalHandleID: journal.LocalHandleID,
		OwnerID: journal.OwnerID, MachineID: journal.MachineID, RuntimeID: journal.RuntimeID,
		RuntimeEpoch: journal.RuntimeEpoch, HarnessKind: journal.HarnessKind,
		HarnessVersion: journal.HarnessVersion, AdapterVersion: journal.AdapterVersion,
		AdapterProtocolVersion: journal.AdapterProtocolVersion,
		WorkspaceFingerprint:   journal.WorkspaceFingerprint, SessionState: journal.SessionState,
		LaunchState: journal.LaunchState, RecoveryRequired: journal.NeedsReconciliation(),
	}
}

// NewGoalSessionLocalHandleID returns a UUID suitable for local_handle_id.
func NewGoalSessionLocalHandleID() (string, error) {
	return NewDaemonInstanceID()
}

// SaveGoalSessionLaunchIntent persists the launch intent before native launch.
// Replaying the exact same intent is idempotent. Any changed replay is a
// conflict, including an attempted or uncertain launch, so a caller cannot
// silently start a duplicate native session.
func (store *Store) SaveGoalSessionLaunchIntent(intent GoalSessionLaunchIntent) (GoalSessionJournal, error) {
	if err := normalizeGoalSessionIntent(&intent); err != nil {
		return GoalSessionJournal{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return GoalSessionJournal{}, err
	}
	path := store.goalSessionPath(intent.Key())
	existing, err := store.loadGoalSessionPathLocked(path)
	if err == nil {
		if sameGoalSessionIntent(existing.Intent(), intent) {
			if existing.NeedsReconciliation() {
				return existing, ErrGoalSessionUncertain
			}
			return existing, nil
		}
		return GoalSessionJournal{}, ErrGoalSessionConflict
	}
	if !IsNotFound(err) {
		return GoalSessionJournal{}, err
	}
	if err := store.ensureLocalHandleAvailableLocked(intent); err != nil {
		return GoalSessionJournal{}, err
	}
	now := time.Now().UTC()
	journal := GoalSessionJournal{
		GoalSessionLaunchIntent: intent,
		SchemaVersion:           goalSessionSchemaVersion,
		SessionState:            GoalSessionStateBusy,
		LaunchState:             GoalSessionLaunchStateIntent,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if err := validateGoalSessionJournal(journal); err != nil {
		return GoalSessionJournal{}, err
	}
	if err := store.beginGoalSessionLineageMutationLocked(intent.LineageKey()); err != nil {
		return GoalSessionJournal{}, err
	}
	if err := store.saveGoalSessionJournalLocked(journal); err != nil {
		// Leave the index dirty if the write outcome is not provable. The next
		// query will rebuild from the authoritative journal set and fail closed
		// if anything was actually persisted.
		return GoalSessionJournal{}, err
	}
	// The lineage index is a derived acceleration structure. The journal above
	// is the commit point; a refresh failure leaves a durable dirty marker and is
	// repaired by the next query instead of making callers retry a committed
	// launch intent as if it had not been saved.
	_ = store.refreshGoalSessionLineageIndexLocked(intent.LineageKey())
	return journal, nil
}

// LoadGoalSession loads one local native session journal.
func (store *Store) LoadGoalSession(key GoalSessionKey) (GoalSessionJournal, error) {
	if err := validateGoalSessionKey(key); err != nil {
		return GoalSessionJournal{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return GoalSessionJournal{}, err
	}
	return store.loadGoalSessionPathLocked(store.goalSessionPath(key))
}

// LoadGoalSessionByHandle finds a session by local_handle_id without exposing
// the raw native identity to the caller's control-plane code.
func (store *Store) LoadGoalSessionByHandle(localHandleID string) (GoalSessionJournal, error) {
	if !validRequiredString(localHandleID, 4096) {
		return GoalSessionJournal{}, errors.New("local handle ID is invalid")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return GoalSessionJournal{}, err
	}
	journals, err := store.listGoalSessionsLocked()
	if err != nil {
		return GoalSessionJournal{}, err
	}
	for _, journal := range journals {
		if journal.LocalHandleID == localHandleID {
			return journal, nil
		}
	}
	return GoalSessionJournal{}, &NotFoundError{Resource: "goal session journal"}
}

// LoadGoalSessionByControlSessionID finds the local retained-session record
// for one Control harness session. A duplicate mapping is a local integrity
// conflict: callers must not choose an arbitrary native handle.
func (store *Store) LoadGoalSessionByControlSessionID(controlSessionID string) (GoalSessionJournal, error) {
	if !validGoalSessionUUID(controlSessionID) {
		return GoalSessionJournal{}, errors.New("Control session ID is invalid")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return GoalSessionJournal{}, err
	}
	journals, err := store.listGoalSessionsLocked()
	if err != nil {
		return GoalSessionJournal{}, err
	}
	var match *GoalSessionJournal
	for _, journal := range journals {
		if journal.ControlSessionID != controlSessionID {
			continue
		}
		if match != nil {
			return GoalSessionJournal{}, ErrGoalSessionConflict
		}
		candidate := journal
		match = &candidate
	}
	if match == nil {
		return GoalSessionJournal{}, &NotFoundError{Resource: "goal session journal"}
	}
	return *match, nil
}

// ListGoalSessions loads all authoritative native session journals and fails
// closed on malformed records. Temporary files owned by this package are
// removed, matching ListJournals behavior.
func (store *Store) ListGoalSessions() ([]GoalSessionJournal, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return nil, err
	}
	return store.listGoalSessionsLocked()
}

// CheckGoalSessionLineage returns the durable local barrier for one
// Goal/WorkItem/Task lineage. The query is backed by a per-lineage index after
// its first rebuild, so admission does not need to scan every session journal
// on each retry. A dirty or missing index is rebuilt from the journals before
// the result is returned; any rebuild uncertainty fails closed.
func (store *Store) CheckGoalSessionLineage(lineage GoalSessionLineageKey) (GoalSessionLineageStatus, error) {
	if err := validateGoalSessionLineageKey(lineage); err != nil {
		return GoalSessionLineageStatus{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return GoalSessionLineageStatus{}, err
	}
	return store.checkGoalSessionLineageLocked(lineage)
}

// EnsureGoalSessionLineageAvailable is the admission guard for a new local
// session. It maps the indexed status to the same typed errors used by launch
// intent persistence, while callers that need diagnostics can use
// CheckGoalSessionLineage directly.
func (store *Store) EnsureGoalSessionLineageAvailable(lineage GoalSessionLineageKey) error {
	status, err := store.CheckGoalSessionLineage(lineage)
	if err != nil {
		return err
	}
	if !status.BlocksNewLaunch() {
		return nil
	}
	if status.State == GoalSessionLineageStateUncertain || status.State == GoalSessionLineageStateLaunching {
		return ErrGoalSessionUncertain
	}
	return ErrGoalSessionConflict
}

// MarkGoalSessionLaunchStarted records the point after which a crash may have
// left a native session alive without a persisted native handle.
func (store *Store) MarkGoalSessionLaunchStarted(key GoalSessionKey, at ...time.Time) (GoalSessionJournal, error) {
	when := time.Now().UTC()
	if len(at) > 1 {
		return GoalSessionJournal{}, errors.New("launch start time is invalid")
	}
	if len(at) == 1 {
		when = at[0].UTC()
	}
	if when.IsZero() {
		return GoalSessionJournal{}, errors.New("launch start time is invalid")
	}
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if journal.LaunchState == GoalSessionLaunchStateClosed || journal.SessionState == GoalSessionStateClosed {
			return ErrGoalSessionClosed
		}
		if journal.NeedsReconciliation() {
			return ErrGoalSessionUncertain
		}
		if journal.LaunchState == GoalSessionLaunchStateAttached {
			return errors.New("goal session native handle is already attached")
		}
		journal.LaunchState = GoalSessionLaunchStateLaunching
		journal.SessionState = GoalSessionStateBusy
		journal.LaunchAttempted = true
		journal.LaunchAttemptedAt = when
		journal.RecoveryRequired = true
		journal.UpdatedAt = when
		return nil
	})
}

// AbortGoalSessionLaunchBeforeNativeStart records a known local abort before
// Adapter.Start was invoked. It is deliberately unavailable once a native
// handle exists or recovery is required; those cases are unknown external
// effects and must remain reconcilable rather than being closed optimistically.
func (store *Store) AbortGoalSessionLaunchBeforeNativeStart(key GoalSessionKey) (GoalSessionJournal, error) {
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if journal.LaunchState == GoalSessionLaunchStateClosed && journal.SessionState == GoalSessionStateClosed {
			return nil
		}
		if journal.LaunchState == GoalSessionLaunchStateIntent && !journal.LaunchAttempted && !journal.RecoveryRequired && journal.NativeSessionID == "" && journal.NativeSessionFilename == "" {
			journal.LaunchState = GoalSessionLaunchStateClosed
			journal.SessionState = GoalSessionStateClosed
			journal.RecoveryRequired = false
			journal.UncertainReason = ""
			journal.UpdatedAt = time.Now().UTC()
			return nil
		}
		if journal.LaunchState != GoalSessionLaunchStateLaunching || !journal.LaunchAttempted || journal.NativeSessionID != "" || journal.NativeSessionFilename != "" {
			return ErrGoalSessionUncertain
		}
		journal.LaunchState = GoalSessionLaunchStateClosed
		journal.SessionState = GoalSessionStateClosed
		journal.RecoveryRequired = false
		journal.UncertainReason = ""
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// PersistGoalSessionHandle durably binds local_handle_id to the native
// identity. The optional compatibility argument is checked before the write.
func (store *Store) PersistGoalSessionHandle(key GoalSessionKey, handle GoalSessionHandle, expected ...GoalSessionCompatibility) (GoalSessionJournal, error) {
	if err := validateGoalSessionHandle(handle); err != nil {
		return GoalSessionJournal{}, err
	}
	if len(expected) > 1 {
		return GoalSessionJournal{}, errors.New("goal session compatibility is invalid")
	}
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if len(expected) == 1 {
			if err := compareGoalSessionIdentity(*journal, expected[0]); err != nil {
				return err
			}
		}
		if journal.LaunchState == GoalSessionLaunchStateClosed || journal.SessionState == GoalSessionStateClosed {
			return ErrGoalSessionClosed
		}
		if journal.LaunchState == GoalSessionLaunchStateUncertain || (journal.NeedsReconciliation() && journal.LaunchState != GoalSessionLaunchStateLaunching) {
			return ErrGoalSessionUncertain
		}
		if journal.LaunchState == GoalSessionLaunchStateAttached {
			if journal.NativeSessionID == handle.NativeSessionID && journal.NativeSessionFilename == handle.NativeSessionFilename {
				return nil
			}
			return ErrGoalSessionConflict
		}
		journal.NativeSessionID = handle.NativeSessionID
		journal.NativeSessionFilename = handle.NativeSessionFilename
		journal.LaunchState = GoalSessionLaunchStateAttached
		journal.LaunchAttempted = true
		if journal.LaunchAttemptedAt.IsZero() {
			journal.LaunchAttemptedAt = time.Now().UTC()
		}
		journal.RecoveryRequired = false
		journal.UncertainReason = ""
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// PersistGoalSessionControlAttachment durably records the public Control
// session identity returned by the attach receipt. It is a barrier: callers
// must not acknowledge the attach outbox item until this mapping is on disk.
func (store *Store) PersistGoalSessionControlAttachment(key GoalSessionKey, controlSessionID, bindingID, attachmentReceiptID string, serverIssuedBinding bool) (GoalSessionJournal, error) {
	if !validGoalSessionUUID(controlSessionID) || !validGoalSessionUUID(bindingID) || !validGoalSessionUUID(attachmentReceiptID) {
		return GoalSessionJournal{}, errors.New("Goal session control attachment is invalid")
	}
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if journal.NeedsReconciliation() {
			return ErrGoalSessionUncertain
		}
		if journal.LaunchState != GoalSessionLaunchStateAttached || journal.NativeSessionID == "" {
			return ErrGoalSessionNotAttached
		}
		if journal.BindingID != "" && journal.BindingID != bindingID {
			return ErrGoalSessionConflict
		}
		if journal.BindingID == "" && !serverIssuedBinding {
			return ErrGoalSessionConflict
		}
		if journal.ControlSessionID != "" && journal.ControlSessionID != controlSessionID {
			return ErrGoalSessionConflict
		}
		if journal.ControlAttachmentReceiptID != "" && journal.ControlAttachmentReceiptID != attachmentReceiptID {
			return ErrGoalSessionConflict
		}
		if journal.ControlAttachmentLineage != nil {
			lineage := *journal.ControlAttachmentLineage
			if lineage.Original.SessionID != controlSessionID {
				return ErrGoalSessionConflict
			}
			lineage.Current = goalSessionControlAttachment(*journal, controlSessionID, bindingID, attachmentReceiptID)
			journal.ControlAttachmentLineage = &lineage
		} else {
			attachment := goalSessionControlAttachment(*journal, controlSessionID, bindingID, attachmentReceiptID)
			journal.ControlAttachmentLineage = &GoalSessionControlAttachmentLineage{
				Original: attachment,
				Current:  attachment,
			}
		}
		journal.ControlSessionID = controlSessionID
		journal.ControlAttachmentReceiptID = attachmentReceiptID
		journal.BindingID = bindingID
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// RebindGoalSessionForResume atomically consumes one exact stopped attachment
// and binds the retained native session to a newly admitted resume Run. The
// caller must subsequently persist that Run's new fenced Control attachment
// before it may start a native turn. Replaying the same target before another
// state transition is idempotent; every other concurrent or stale request
// fails closed.
func (store *Store) RebindGoalSessionForResume(key GoalSessionKey, rebind GoalSessionResumeRebind) (GoalSessionJournal, error) {
	if err := validateGoalSessionKey(key); err != nil {
		return GoalSessionJournal{}, err
	}
	if err := validateGoalSessionResumeRebind(rebind); err != nil {
		return GoalSessionJournal{}, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return GoalSessionJournal{}, err
	}
	journal, err := store.loadGoalSessionPathLocked(store.goalSessionPath(key))
	if err != nil {
		return GoalSessionJournal{}, err
	}
	if journal.resumeTargetConflictsWithConsumedHistory(rebind) {
		return GoalSessionJournal{}, ErrGoalSessionConflict
	}
	if sameGoalSessionResumeTarget(journal, rebind) {
		if err := compareGoalSessionResumeCompatibility(journal, rebind.Compatibility); err != nil {
			return GoalSessionJournal{}, err
		}
		return journal, nil
	}
	if err := validateGoalSessionResumeSource(journal, rebind); err != nil {
		return GoalSessionJournal{}, err
	}

	previousLineage := journal.LineageKey()
	targetLineage := GoalSessionLineageKey{GoalID: journal.GoalID, WorkItemID: journal.WorkItemID, TaskID: rebind.TaskID}
	if targetLineage != previousLineage {
		status, err := store.checkGoalSessionLineageLocked(targetLineage)
		if err != nil {
			return GoalSessionJournal{}, err
		}
		if status.BlocksNewLaunch() {
			if status.State == GoalSessionLineageStateUncertain || status.State == GoalSessionLineageStateLaunching {
				return GoalSessionJournal{}, ErrGoalSessionUncertain
			}
			return GoalSessionJournal{}, ErrGoalSessionConflict
		}
	}

	// Mark every affected derived index dirty before publishing the authoritative
	// journal. A crash between these writes is conservatively rebuilt from the
	// journal set on the next lineage check.
	if err := store.beginGoalSessionLineageMutationLocked(previousLineage); err != nil {
		return GoalSessionJournal{}, err
	}
	if targetLineage != previousLineage {
		if err := store.beginGoalSessionLineageMutationLocked(targetLineage); err != nil {
			return GoalSessionJournal{}, err
		}
	}

	journal.RunID = rebind.RunID
	journal.Generation = rebind.Generation
	journal.TaskID = rebind.TaskID
	journal.AdmissionID = rebind.AdmissionID
	journal.BindingID = rebind.BindingID
	journal.SessionMode = GoalSessionModeResume
	journal.HandoffSourceRunID = ""
	journal.ControlAttachmentReceiptID = ""
	sourceCertificate := rebind.ExactStopCertificate
	journal.ResumeSourceStopCertificate = &sourceCertificate
	if err := journal.appendConsumedStopCertificate(sourceCertificate); err != nil {
		return GoalSessionJournal{}, err
	}
	journal.ResumeStartPending = true
	journal.ResumeNativeStopKnown = false
	journal.ResumeTerminalReason = ""
	journal.StopCertificate = nil
	journal.SessionState = GoalSessionStateBusy
	journal.LaunchState = GoalSessionLaunchStateAttached
	journal.LaunchAttempted = true
	journal.RecoveryRequired = false
	journal.UncertainReason = ""
	journal.UpdatedAt = time.Now().UTC()
	if err := validateGoalSessionJournal(journal); err != nil {
		return GoalSessionJournal{}, err
	}
	if err := store.saveGoalSessionJournalLocked(journal); err != nil {
		return GoalSessionJournal{}, err
	}
	_ = store.refreshGoalSessionLineageIndexLocked(previousLineage)
	if targetLineage != previousLineage {
		_ = store.refreshGoalSessionLineageIndexLocked(targetLineage)
	}
	return journal, nil
}

// MarkGoalSessionResumeStartAttempted closes the durable pre-start resume
// window immediately before the caller invokes adapter.Start. If the process
// stops after this mutation, recovery must conservatively treat native resume
// as potentially started rather than compensating with the predecessor stop.
func (store *Store) MarkGoalSessionResumeStartAttempted(key GoalSessionKey) (GoalSessionJournal, error) {
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if journal.LaunchState == GoalSessionLaunchStateClosed || journal.SessionState == GoalSessionStateClosed {
			return ErrGoalSessionClosed
		}
		if journal.NeedsReconciliation() {
			return ErrGoalSessionUncertain
		}
		if journal.SessionMode != GoalSessionModeResume || journal.ResumeSourceStopCertificate == nil || journal.StopCertificate != nil || journal.ResumeNativeStopKnown ||
			journal.SessionState != GoalSessionStateBusy || journal.LaunchState != GoalSessionLaunchStateAttached || journal.NativeSessionID == "" {
			return ErrGoalSessionConflict
		}
		if !journal.ResumeStartPending {
			return nil
		}
		journal.ResumeStartPending = false
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// MarkGoalSessionResumeNativeStopped persists a known local native stop for a
// rebound retained session. Unlike MarkGoalSessionStoppedPending, it does not
// require the current Control attachment receipt: callers may need to record a
// typed native resume rejection before readback/attachment can succeed. The
// later Control stop receipt remains mandatory before availability.
func (store *Store) MarkGoalSessionResumeNativeStopped(key GoalSessionKey) (GoalSessionJournal, error) {
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if journal.LaunchState == GoalSessionLaunchStateClosed || journal.SessionState == GoalSessionStateClosed {
			return ErrGoalSessionClosed
		}
		if journal.NeedsReconciliation() {
			return ErrGoalSessionUncertain
		}
		if journal.SessionMode != GoalSessionModeResume || journal.ResumeSourceStopCertificate == nil || journal.StopCertificate != nil ||
			journal.LaunchState != GoalSessionLaunchStateAttached || journal.NativeSessionID == "" ||
			(journal.SessionState != GoalSessionStateBusy && journal.SessionState != GoalSessionStateUnavailable) {
			return ErrGoalSessionConflict
		}
		if journal.ResumeNativeStopKnown {
			return nil
		}
		journal.SessionState = GoalSessionStateUnavailable
		journal.ResumeStartPending = false
		journal.ResumeNativeStopKnown = true
		journal.ResumeTerminalReason = ""
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// MarkGoalSessionResumeRejected persists a known local pi resume rejection
// before the caller queues the Run terminal transition. It shares the proven
// native-stop state with MarkGoalSessionResumeNativeStopped but carries the
// only terminal classification that can safely survive that cross-file gap.
func (store *Store) MarkGoalSessionResumeRejected(key GoalSessionKey) (GoalSessionJournal, error) {
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if journal.LaunchState == GoalSessionLaunchStateClosed || journal.SessionState == GoalSessionStateClosed {
			return ErrGoalSessionClosed
		}
		if journal.NeedsReconciliation() {
			return ErrGoalSessionUncertain
		}
		if journal.SessionMode != GoalSessionModeResume || journal.ResumeSourceStopCertificate == nil || journal.StopCertificate != nil ||
			journal.LaunchState != GoalSessionLaunchStateAttached || journal.NativeSessionID == "" ||
			(journal.SessionState != GoalSessionStateBusy && journal.SessionState != GoalSessionStateUnavailable) {
			return ErrGoalSessionConflict
		}
		if journal.ResumeNativeStopKnown {
			if journal.ResumeTerminalReason == "resume_rejected" {
				return nil
			}
			if journal.ResumeTerminalReason != "" {
				return ErrGoalSessionConflict
			}
			// A generic native-stop witness may have been persisted before the
			// adapter returned its typed rejection. The typed result is the
			// stronger classification and may safely upgrade that empty marker.
			journal.ResumeTerminalReason = "resume_rejected"
			journal.UpdatedAt = time.Now().UTC()
			return nil
		}
		journal.SessionState = GoalSessionStateUnavailable
		journal.ResumeStartPending = false
		journal.ResumeNativeStopKnown = true
		journal.ResumeTerminalReason = "resume_rejected"
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// MarkGoalSessionStoppedPending records known native termination without
// discarding the native resume identity. A separate fenced Control receipt is
// required before the journal becomes locally available for a later resume.
func (store *Store) MarkGoalSessionStoppedPending(key GoalSessionKey) (GoalSessionJournal, error) {
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if journal.NeedsReconciliation() {
			return ErrGoalSessionUncertain
		}
		if journal.LaunchState != GoalSessionLaunchStateAttached || journal.NativeSessionID == "" {
			return ErrGoalSessionNotAttached
		}
		if journal.IsLegacyControlAttachment() || !journal.HasVerifiedControlAttachment() {
			return ErrGoalSessionCompatibilityIncomplete
		}
		if journal.ResumeStartPending {
			return ErrGoalSessionConflict
		}
		if !validGoalSessionUUID(journal.ControlSessionID) || !validGoalSessionUUID(journal.BindingID) {
			return ErrGoalSessionConflict
		}
		if journal.SessionState != GoalSessionStateBusy && journal.SessionState != GoalSessionStateUnavailable {
			return ErrGoalSessionConflict
		}
		journal.SessionState = GoalSessionStateUnavailable
		if journal.SessionMode == GoalSessionModeResume {
			journal.ResumeNativeStopKnown = true
		}
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// MarkGoalSessionAvailable acknowledges the matching Control stop receipt.
// It deliberately retains the local native session handle and workspace for
// explicit resume; permanent destruction remains CloseGoalSession's job.
func (store *Store) MarkGoalSessionAvailable(key GoalSessionKey, certificate GoalSessionStopCertificate) (GoalSessionJournal, error) {
	if !validGoalSessionStopCertificate(certificate) {
		return GoalSessionJournal{}, errors.New("Goal session availability receipt is invalid")
	}
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if consumed, exact := journal.consumedStopCertificate(certificate); consumed {
			if exact {
				// The predecessor receipt was already consumed by a durable rebind.
				// Returning success lets its late outbox replay retire without changing
				// the current Run's binding or availability state.
				return nil
			}
			return ErrGoalSessionConflict
		}
		if journal.NeedsReconciliation() {
			return ErrGoalSessionUncertain
		}
		if journal.IsLegacyControlAttachment() || !journal.HasVerifiedControlAttachment() {
			return ErrGoalSessionCompatibilityIncomplete
		}
		if journal.LaunchState != GoalSessionLaunchStateAttached || journal.NativeSessionID == "" ||
			journal.ControlSessionID != certificate.SessionID || journal.BindingID != certificate.BindingID ||
			journal.RunID != certificate.RunID || journal.Generation != certificate.Generation ||
			journal.LocalHandleID != certificate.LocalHandleID {
			return ErrGoalSessionConflict
		}
		if journal.StopCertificate != nil {
			if *journal.StopCertificate == certificate {
				return nil
			}
			return ErrGoalSessionConflict
		}
		if journal.SessionState != GoalSessionStateUnavailable && journal.SessionState != GoalSessionStateAvailable {
			return ErrGoalSessionConflict
		}
		if journal.ResumeSourceStopCertificate != nil && journal.ConsumedStopCertificates == nil {
			if err := journal.appendConsumedStopCertificate(*journal.ResumeSourceStopCertificate); err != nil {
				return err
			}
		}
		journal.SessionState = GoalSessionStateAvailable
		certificateCopy := certificate
		journal.StopCertificate = &certificateCopy
		// The current Run is now terminally released. Its predecessor certificate
		// was only an idempotency witness while the rebind remained in flight;
		// retaining it would incorrectly block the next valid resume cycle.
		journal.ResumeSourceStopCertificate = nil
		journal.ResumeStartPending = false
		journal.ResumeNativeStopKnown = false
		journal.ResumeTerminalReason = ""
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// AttachGoalSession is an alias for the explicit post-launch persistence
// operation. It never reattaches merely because a journal exists.
func (store *Store) AttachGoalSession(key GoalSessionKey, handle GoalSessionHandle, expected ...GoalSessionCompatibility) (GoalSessionJournal, error) {
	return store.PersistGoalSessionHandle(key, handle, expected...)
}

// MarkGoalSessionUncertain makes an unknown native outcome explicit and
// durable. It clears any retained native handle so an attached session cannot
// be re-used after its state becomes uncertain.
func (store *Store) MarkGoalSessionUncertain(key GoalSessionKey, reason string) (GoalSessionJournal, error) {
	if !validRequiredString(reason, 4096) {
		return GoalSessionJournal{}, errors.New("uncertain launch reason is invalid")
	}
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if journal.LaunchState == GoalSessionLaunchStateClosed || journal.SessionState == GoalSessionStateClosed {
			return ErrGoalSessionClosed
		}
		// SaveGoalSessionLaunchIntent is durably written before the native
		// launch boundary. If recovery observes this pre-boundary state, the
		// native effect is known not to have been attempted; close it rather
		// than manufacturing an uncertain external outcome.
		if journal.LaunchState == GoalSessionLaunchStateIntent && !journal.LaunchAttempted && !journal.RecoveryRequired && journal.NativeSessionID == "" && journal.NativeSessionFilename == "" {
			journal.LaunchState = GoalSessionLaunchStateClosed
			journal.SessionState = GoalSessionStateClosed
			journal.RecoveryRequired = false
			journal.UncertainReason = ""
			journal.UpdatedAt = time.Now().UTC()
			return nil
		}
		journal.LaunchState = GoalSessionLaunchStateUncertain
		journal.SessionState = GoalSessionStateUnavailable
		journal.LaunchAttempted = true
		if journal.LaunchAttemptedAt.IsZero() {
			journal.LaunchAttemptedAt = time.Now().UTC()
		}
		journal.NativeSessionID = ""
		journal.NativeSessionFilename = ""
		journal.ControlSessionID = ""
		journal.ControlAttachmentReceiptID = ""
		journal.StopCertificate = nil
		journal.ResumeStartPending = false
		journal.ResumeNativeStopKnown = false
		journal.ResumeTerminalReason = ""
		journal.RecoveryRequired = true
		journal.UncertainReason = reason
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// ResolveGoalSessionUncertain explicitly records a reconciled native handle.
// The caller must provide the expected owner/version/workspace identity; no
// automatic re-attach path exists.
func (store *Store) ResolveGoalSessionUncertain(key GoalSessionKey, handle GoalSessionHandle, expected GoalSessionCompatibility) (GoalSessionJournal, error) {
	if err := validateGoalSessionHandle(handle); err != nil {
		return GoalSessionJournal{}, err
	}
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if err := compareGoalSessionIdentityStrict(*journal, expected); err != nil {
			return err
		}
		if !journal.NeedsReconciliation() {
			return errors.New("goal session is not awaiting reconciliation")
		}
		journal.NativeSessionID = handle.NativeSessionID
		journal.NativeSessionFilename = handle.NativeSessionFilename
		journal.LaunchState = GoalSessionLaunchStateAttached
		journal.SessionState = GoalSessionStateBusy
		journal.ResumeStartPending = false
		journal.ResumeNativeStopKnown = false
		journal.ResumeTerminalReason = ""
		journal.RecoveryRequired = false
		journal.UncertainReason = ""
		journal.LaunchAttempted = true
		if journal.LaunchAttemptedAt.IsZero() {
			journal.LaunchAttemptedAt = time.Now().UTC()
		}
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// ResolveGoalSessionUncertainStopped closes an unresolved launch only after a
// caller has explicitly reconciled the native side and established that it is
// stopped or was never created. This is the no-handle recovery path; it is the
// only operation, besides a known pre-native abort, that releases an
// unresolved lineage barrier without attaching a native identity.
func (store *Store) ResolveGoalSessionUncertainStopped(key GoalSessionKey, expected GoalSessionCompatibility) (GoalSessionJournal, error) {
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if err := compareGoalSessionIdentityStrict(*journal, expected); err != nil {
			return err
		}
		if !journal.NeedsReconciliation() {
			if journal.LaunchState == GoalSessionLaunchStateClosed && journal.SessionState == GoalSessionStateClosed {
				return nil
			}
			return errors.New("goal session is not awaiting reconciliation")
		}
		journal.NativeSessionID = ""
		journal.NativeSessionFilename = ""
		journal.ControlSessionID = ""
		journal.ControlAttachmentReceiptID = ""
		journal.StopCertificate = nil
		journal.ResumeStartPending = false
		journal.ResumeNativeStopKnown = false
		journal.ResumeTerminalReason = ""
		journal.SessionState = GoalSessionStateClosed
		journal.LaunchState = GoalSessionLaunchStateClosed
		journal.RecoveryRequired = false
		journal.UncertainReason = ""
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// CloseGoalSession records that an attached native process is known to have
// stopped. It retains the launch identity for audit but removes the private
// native handle, which cannot be valid after close. Callers must reconcile an
// uncertain launch instead of closing or deleting its journal.
func (store *Store) CloseGoalSession(key GoalSessionKey) (GoalSessionJournal, error) {
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if journal.LaunchState == GoalSessionLaunchStateClosed && journal.SessionState == GoalSessionStateClosed {
			return nil
		}
		if journal.NeedsReconciliation() {
			return ErrGoalSessionUncertain
		}
		if journal.LaunchState != GoalSessionLaunchStateAttached || journal.NativeSessionID == "" {
			return ErrGoalSessionNotAttached
		}
		journal.NativeSessionID = ""
		journal.NativeSessionFilename = ""
		journal.ControlSessionID = ""
		journal.ControlAttachmentReceiptID = ""
		journal.StopCertificate = nil
		journal.ResumeStartPending = false
		journal.ResumeNativeStopKnown = false
		journal.ResumeTerminalReason = ""
		journal.SessionState = GoalSessionStateClosed
		journal.LaunchState = GoalSessionLaunchStateClosed
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// SetGoalSessionState persists the control-facing session state. Closing is
// routed through CloseGoalSession so an attached native handle cannot remain
// in a closed journal.
func (store *Store) SetGoalSessionState(key GoalSessionKey, sessionState string) (GoalSessionJournal, error) {
	if !validGoalSessionState(sessionState) {
		return GoalSessionJournal{}, errors.New("goal session state is invalid")
	}
	if sessionState == GoalSessionStateClosed {
		return store.CloseGoalSession(key)
	}
	if sessionState == GoalSessionStateAvailable {
		return GoalSessionJournal{}, errors.New("available Goal session state requires a stop certificate")
	}
	return store.mutateGoalSession(key, func(journal *GoalSessionJournal) error {
		if journal.LaunchState == GoalSessionLaunchStateClosed || journal.SessionState == GoalSessionStateClosed {
			return ErrGoalSessionClosed
		}
		if journal.ResumeStartPending && sessionState != GoalSessionStateBusy {
			return ErrGoalSessionConflict
		}
		journal.SessionState = sessionState
		journal.UpdatedAt = time.Now().UTC()
		return nil
	})
}

// CheckGoalSessionCompatibility verifies the exact local ownership and
// version/workspace binding required for an explicit resume or reconciliation.
func (store *Store) CheckGoalSessionCompatibility(key GoalSessionKey, expected GoalSessionCompatibility) error {
	journal, err := store.LoadGoalSession(key)
	if err != nil {
		return err
	}
	return compareGoalSessionCompatibility(journal, expected)
}

// DeleteGoalSession removes only a closed, fully resolved session journal. An
// uncertain record must remain available for reconciliation and cannot be
// removed by a normal cleanup path.
func (store *Store) DeleteGoalSession(key GoalSessionKey) error {
	if err := validateGoalSessionKey(key); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return err
	}
	journal, err := store.loadGoalSessionPathLocked(store.goalSessionPath(key))
	if err != nil {
		return err
	}
	if journal.NeedsReconciliation() {
		return ErrGoalSessionUncertain
	}
	if journal.SessionState != GoalSessionStateClosed && journal.LaunchState != GoalSessionLaunchStateClosed {
		return errors.New("goal session is not closed")
	}
	lineage := journal.LineageKey()
	if err := store.beginGoalSessionLineageMutationLocked(lineage); err != nil {
		return err
	}
	if err := os.Remove(store.goalSessionPath(key)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &NotFoundError{Resource: "goal session journal"}
		}
		return errors.New("delete goal session journal")
	}
	if err := syncDirectory(store.goalSessionsDir()); err != nil {
		return err
	}
	// The journal deletion is already durably committed. The lineage index is
	// derived and rebuildable, so a refresh failure must not make a successful
	// cleanup look uncommitted to the caller.
	_ = store.refreshGoalSessionLineageIndexLocked(lineage)
	return nil
}

// mutateGoalSession performs a serialized read-modify-write update using the
// same atomic fsync+rename primitive as legacy RunJournal.
func (store *Store) mutateGoalSession(key GoalSessionKey, mutate func(*GoalSessionJournal) error) (GoalSessionJournal, error) {
	if err := validateGoalSessionKey(key); err != nil {
		return GoalSessionJournal{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return GoalSessionJournal{}, err
	}
	journal, err := store.loadGoalSessionPathLocked(store.goalSessionPath(key))
	if err != nil {
		return GoalSessionJournal{}, err
	}
	if err := mutate(&journal); err != nil {
		return GoalSessionJournal{}, err
	}
	if err := validateGoalSessionJournal(journal); err != nil {
		return GoalSessionJournal{}, err
	}
	if err := store.beginGoalSessionLineageMutationLocked(journal.LineageKey()); err != nil {
		return GoalSessionJournal{}, err
	}
	if err := store.saveGoalSessionJournalLocked(journal); err != nil {
		return GoalSessionJournal{}, err
	}
	// The session journal is the authoritative commit. A failed derived-index
	// refresh leaves the dirty marker for the next query to rebuild and must not
	// make a successful state transition look uncommitted to the caller.
	_ = store.refreshGoalSessionLineageIndexLocked(journal.LineageKey())
	return journal, nil
}

func (store *Store) loadGoalSessionPathLocked(path string) (GoalSessionJournal, error) {
	var journal GoalSessionJournal
	if err := store.readJSONWithLimit(path, "goal session journal", &journal, maxJournalFileBytes); err != nil {
		return GoalSessionJournal{}, err
	}
	if err := validateGoalSessionJournal(journal); err != nil {
		return GoalSessionJournal{}, errors.New("invalid goal session journal")
	}
	if filepath.Base(path) != filepath.Base(store.goalSessionPath(journal.Key())) {
		return GoalSessionJournal{}, errors.New("goal session journal does not match requested key")
	}
	return journal, nil
}

func (store *Store) saveGoalSessionJournalLocked(journal GoalSessionJournal) error {
	if err := validateGoalSessionJournal(journal); err != nil {
		return err
	}
	if err := store.ensureGoalSessionsDirLocked(); err != nil {
		return fmt.Errorf("create goal session journal directory: %w", err)
	}
	return store.writeJSONWithLimit(store.goalSessionPath(journal.Key()), journal, "goal session journal", maxJournalFileBytes)
}

func (store *Store) checkGoalSessionLineageLocked(lineage GoalSessionLineageKey) (GoalSessionLineageStatus, error) {
	index, err := store.loadGoalSessionLineageIndexLocked(lineage)
	if IsNotFound(err) || (err == nil && index.Dirty) {
		if err := store.refreshGoalSessionLineageIndexLocked(lineage); err != nil {
			return GoalSessionLineageStatus{}, err
		}
		index, err = store.loadGoalSessionLineageIndexLocked(lineage)
	}
	if err != nil {
		return GoalSessionLineageStatus{}, err
	}
	return goalSessionLineageStatus(index), nil
}

// beginGoalSessionLineageMutationLocked writes a durable dirty marker before
// changing a session journal. If the process stops between the journal write
// and index refresh, a later query will scan journals rather than trusting a
// possibly incomplete snapshot.
func (store *Store) beginGoalSessionLineageMutationLocked(lineage GoalSessionLineageKey) error {
	if err := validateGoalSessionLineageKey(lineage); err != nil {
		return err
	}
	index := goalSessionLineageIndex{
		SchemaVersion: goalSessionLineageSchemaVersion,
		Lineage:       lineage,
		Dirty:         true,
		Entries:       []goalSessionLineageEntry{},
	}
	return store.saveGoalSessionLineageIndexLocked(index)
}

// refreshGoalSessionLineageIndexLocked rebuilds one index from authoritative
// session journals and publishes it clean only after the scan completes.
func (store *Store) refreshGoalSessionLineageIndexLocked(lineage GoalSessionLineageKey) error {
	if err := validateGoalSessionLineageKey(lineage); err != nil {
		return err
	}
	journals, err := store.listGoalSessionsLocked()
	if err != nil {
		return err
	}
	entries := make([]goalSessionLineageEntry, 0)
	for _, journal := range journals {
		if journal.LineageKey() != lineage || goalSessionLineageStateForJournal(journal) == GoalSessionLineageStateFree {
			continue
		}
		entries = append(entries, goalSessionLineageEntry{
			SessionKey: journal.Key(),
			State:      goalSessionLineageStateForJournal(journal),
			UpdatedAt:  journal.UpdatedAt,
		})
	}
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].SessionKey.GoalID == entries[right].SessionKey.GoalID {
			return entries[left].SessionKey.LocalHandleID < entries[right].SessionKey.LocalHandleID
		}
		return entries[left].SessionKey.GoalID < entries[right].SessionKey.GoalID
	})
	return store.saveGoalSessionLineageIndexLocked(goalSessionLineageIndex{
		SchemaVersion: goalSessionLineageSchemaVersion,
		Lineage:       lineage,
		Entries:       entries,
	})
}

func (store *Store) loadGoalSessionLineageIndexLocked(lineage GoalSessionLineageKey) (goalSessionLineageIndex, error) {
	if err := validateGoalSessionLineageKey(lineage); err != nil {
		return goalSessionLineageIndex{}, err
	}
	var index goalSessionLineageIndex
	path := store.goalSessionLineagePath(lineage)
	if err := store.readJSONWithLimit(path, "goal session lineage index", &index, maxJournalFileBytes); err != nil {
		return goalSessionLineageIndex{}, err
	}
	if err := validateGoalSessionLineageIndex(index); err != nil {
		return goalSessionLineageIndex{}, errors.New("invalid goal session lineage index")
	}
	if index.Lineage != lineage || filepath.Base(path) != filepath.Base(store.goalSessionLineagePath(index.Lineage)) {
		return goalSessionLineageIndex{}, errors.New("goal session lineage index does not match requested lineage")
	}
	return index, nil
}

func (store *Store) saveGoalSessionLineageIndexLocked(index goalSessionLineageIndex) error {
	if err := validateGoalSessionLineageIndex(index); err != nil {
		return err
	}
	if err := store.ensureGoalSessionsDirLocked(); err != nil {
		return fmt.Errorf("create goal session lineage index directory: %w", err)
	}
	return store.writeJSONWithLimit(store.goalSessionLineagePath(index.Lineage), index, "goal session lineage index", maxJournalFileBytes)
}

func goalSessionLineageStatus(index goalSessionLineageIndex) GoalSessionLineageStatus {
	status := GoalSessionLineageStatus{Lineage: index.Lineage, State: GoalSessionLineageStateFree}
	if len(index.Entries) == 0 {
		return status
	}
	status.Blocking = true
	status.SessionKeys = make([]GoalSessionKey, 0, len(index.Entries))
	for _, entry := range index.Entries {
		status.SessionKeys = append(status.SessionKeys, entry.SessionKey)
		if lineageStateRank(entry.State) > lineageStateRank(status.State) {
			status.State = entry.State
		}
	}
	sort.Slice(status.SessionKeys, func(left, right int) bool {
		if status.SessionKeys[left].GoalID == status.SessionKeys[right].GoalID {
			return status.SessionKeys[left].LocalHandleID < status.SessionKeys[right].LocalHandleID
		}
		return status.SessionKeys[left].GoalID < status.SessionKeys[right].GoalID
	})
	return status
}

func goalSessionLineageStateForJournal(journal GoalSessionJournal) GoalSessionLineageState {
	if journal.LaunchState == GoalSessionLaunchStateClosed && journal.SessionState == GoalSessionStateClosed {
		return GoalSessionLineageStateFree
	}
	if journal.LaunchState == GoalSessionLaunchStateUncertain || journal.NeedsReconciliation() {
		return GoalSessionLineageStateUncertain
	}
	switch journal.LaunchState {
	case GoalSessionLaunchStateIntent:
		return GoalSessionLineageStateReserved
	case GoalSessionLaunchStateLaunching:
		return GoalSessionLineageStateLaunching
	case GoalSessionLaunchStateAttached:
		return GoalSessionLineageStateAttached
	default:
		// validateGoalSessionJournal rejects this state. Keep the helper
		// conservative if it is ever called before validation.
		return GoalSessionLineageStateUncertain
	}
}

func lineageStateRank(state GoalSessionLineageState) int {
	switch state {
	case GoalSessionLineageStateReserved:
		return 1
	case GoalSessionLineageStateAttached:
		return 2
	case GoalSessionLineageStateLaunching:
		return 3
	case GoalSessionLineageStateUncertain:
		return 4
	default:
		return 0
	}
}

func validateGoalSessionLineageIndex(index goalSessionLineageIndex) error {
	if index.SchemaVersion != goalSessionLineageSchemaVersion || validateGoalSessionLineageKey(index.Lineage) != nil || len(index.Entries) > 4096 {
		return errors.New("goal session lineage index is invalid")
	}
	seen := make(map[GoalSessionKey]struct{}, len(index.Entries))
	for _, entry := range index.Entries {
		if err := validateGoalSessionKey(entry.SessionKey); err != nil || !validGoalSessionLineageState(entry.State) || entry.State == GoalSessionLineageStateFree || entry.UpdatedAt.IsZero() {
			return errors.New("goal session lineage index is invalid")
		}
		if _, exists := seen[entry.SessionKey]; exists {
			return errors.New("goal session lineage index is invalid")
		}
		seen[entry.SessionKey] = struct{}{}
	}
	return nil
}

func (store *Store) listGoalSessionsLocked() ([]GoalSessionJournal, error) {
	if _, err := os.Stat(store.goalSessionsDir()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []GoalSessionJournal{}, nil
		}
		return nil, errors.New("list goal session journal directory")
	}
	entries, err := os.ReadDir(store.goalSessionsDir())
	if err != nil {
		return nil, errors.New("list goal session journal directory")
	}
	if err := removeOwnedGoalSessionTemps(store.goalSessionsDir(), entries); err != nil {
		return nil, err
	}
	result := make([]GoalSessionJournal, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isGoalSessionFile(entry.Name()) {
			continue
		}
		journal, err := store.loadGoalSessionPathLocked(filepath.Join(store.goalSessionsDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		result = append(result, journal)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].GoalID == result[right].GoalID {
			return result[left].LocalHandleID < result[right].LocalHandleID
		}
		return result[left].GoalID < result[right].GoalID
	})
	return result, nil
}

func (store *Store) ensureGoalSessionsDirLocked() error {
	return ensurePrivateDirectory(store.goalSessionsDir())
}

func (store *Store) goalSessionsDir() string {
	return filepath.Join(store.dir, goalSessionsDirectoryName)
}

func (store *Store) goalSessionPath(key GoalSessionKey) string {
	input := key.GoalID + "\x00" + key.LocalHandleID
	digest := sha256.Sum256([]byte(input))
	return filepath.Join(store.goalSessionsDir(), goalSessionFilePrefix+hex.EncodeToString(digest[:])+goalSessionFileSuffix)
}

func (store *Store) goalSessionLineagePath(lineage GoalSessionLineageKey) string {
	input := lineage.GoalID + "\x00" + lineage.WorkItemID + "\x00" + lineage.TaskID
	digest := sha256.Sum256([]byte(input))
	return filepath.Join(store.goalSessionsDir(), goalSessionLineageFilePrefix+hex.EncodeToString(digest[:])+goalSessionLineageFileSuffix)
}

func (store *Store) ensureLocalHandleAvailableLocked(intent GoalSessionLaunchIntent) error {
	status, err := store.checkGoalSessionLineageLocked(intent.LineageKey())
	if err != nil {
		return err
	}
	if status.BlocksNewLaunch() {
		if status.State == GoalSessionLineageStateUncertain || status.State == GoalSessionLineageStateLaunching {
			return ErrGoalSessionUncertain
		}
		return ErrGoalSessionConflict
	}
	journals, err := store.listGoalSessionsLocked()
	if err != nil {
		return err
	}
	for _, existing := range journals {
		if existing.LocalHandleID == intent.LocalHandleID && existing.Key() != intent.Key() {
			return ErrGoalSessionConflict
		}
		if existing.GoalID == intent.GoalID && existing.WorkItemID == intent.WorkItemID && existing.TaskID == intent.TaskID && existing.RunID == intent.RunID && existing.Generation == intent.Generation && existing.LaunchState != GoalSessionLaunchStateClosed && existing.SessionState != GoalSessionStateClosed {
			if existing.NeedsReconciliation() {
				return ErrGoalSessionUncertain
			}
			return ErrGoalSessionConflict
		}
	}
	return nil
}

func normalizeGoalSessionIntent(intent *GoalSessionLaunchIntent) error {
	if intent.LaunchIntentID == "" {
		id, err := NewGoalSessionLocalHandleID()
		if err != nil {
			return err
		}
		intent.LaunchIntentID = id
	}
	if intent.LocalHandleID == "" {
		id, err := NewGoalSessionLocalHandleID()
		if err != nil {
			return err
		}
		intent.LocalHandleID = id
	}
	if intent.SessionMode == "" {
		intent.SessionMode = GoalSessionModeFresh
	}
	if intent.WorkspaceOwnerRunKey == (WorkspaceOwnerRunKey{}) && intent.RunID != "" && intent.Generation > 0 {
		intent.WorkspaceOwnerRunKey = WorkspaceOwnerRunKey{RunID: intent.RunID, Generation: intent.Generation}
	}
	return validateGoalSessionIntent(*intent)
}

func validateGoalSessionKey(key GoalSessionKey) error {
	if !validRequiredString(key.GoalID, 4096) || !validRequiredString(key.LocalHandleID, 4096) {
		return errors.New("goal session key is invalid")
	}
	return nil
}

func validateGoalSessionLineageKey(lineage GoalSessionLineageKey) error {
	if !validRequiredString(lineage.GoalID, 4096) || len(lineage.WorkItemID) > 4096 || len(lineage.TaskID) > 4096 {
		return errors.New("goal session lineage key is invalid")
	}
	return nil
}

func validateGoalSessionIntent(intent GoalSessionLaunchIntent) error {
	if err := validateGoalSessionKey(intent.Key()); err != nil || !validRequiredString(intent.LaunchIntentID, 4096) || intent.GoalRevision <= 0 || intent.Generation < 0 || len(intent.WorkItemID) > 4096 || len(intent.TaskID) > 4096 || len(intent.RunID) > 4096 || len(intent.AdmissionID) > 4096 || len(intent.HandoffSourceRunID) > 36 || len(intent.WorkspacePath) > 32768 {
		return errors.New("goal session launch intent is invalid")
	}
	if intent.RunID != "" && intent.Generation <= 0 {
		return errors.New("goal session generation is invalid")
	}
	if len(intent.OwnerID) > 4096 || len(intent.MachineID) > 4096 || len(intent.RuntimeID) > 4096 || len(intent.DaemonInstanceID) > 4096 || (intent.OwnerID == "" && intent.MachineID == "" && intent.RuntimeID == "" && intent.DaemonInstanceID == "") {
		return errors.New("goal session owner is invalid")
	}
	if intent.RuntimeEpoch < 0 || !validRequiredString(intent.HarnessKind, 4096) || !validRequiredString(intent.HarnessVersion, 4096) || !validRequiredString(intent.AdapterVersion, 4096) || intent.AdapterProtocolVersion <= 0 || !validRequiredString(intent.WorkspaceFingerprint, 4096) || !validGoalSessionMode(intent.SessionMode) {
		return errors.New("goal session launch identity is invalid")
	}
	if intent.RepositoryResourceID != "" && !validGoalSessionUUID(intent.RepositoryResourceID) {
		return errors.New("goal session repository resource ID is invalid")
	}
	if intent.WorkspaceOwnerRunKey != (WorkspaceOwnerRunKey{}) && !validWorkspaceOwnerRunKey(intent.WorkspaceOwnerRunKey) {
		return errors.New("goal session workspace owner Run key is invalid")
	}
	if intent.BindingID != "" && !validGoalSessionUUID(intent.BindingID) {
		return errors.New("goal session binding ID is invalid")
	}
	switch intent.SessionMode {
	case GoalSessionModeHandoff:
		if !validGoalSessionUUID(intent.HandoffSourceRunID) {
			return errors.New("goal session handoff source Run ID is invalid")
		}
	case GoalSessionModeFresh, GoalSessionModeResume:
		if intent.HandoffSourceRunID != "" {
			return errors.New("goal session handoff source Run ID is invalid")
		}
	}
	return nil
}

func validateGoalSessionJournal(journal GoalSessionJournal) error {
	// Wall-clock time can move backwards across a restart or an injected
	// recovery timestamp; ordering is not a journal-integrity invariant.
	if journal.SchemaVersion != goalSessionSchemaVersion || journal.CreatedAt.IsZero() || journal.UpdatedAt.IsZero() {
		return errors.New("goal session journal is invalid")
	}
	if err := validateGoalSessionIntent(journal.Intent()); err != nil {
		return err
	}
	if !validGoalSessionState(journal.SessionState) || !validGoalSessionLaunchState(journal.LaunchState) || len(journal.NativeSessionID) > 65536 || len(journal.NativeSessionFilename) > 32768 || len(journal.UncertainReason) > 4096 || (journal.ControlSessionID != "" && !validGoalSessionUUID(journal.ControlSessionID)) || (journal.ControlAttachmentReceiptID != "" && !validGoalSessionUUID(journal.ControlAttachmentReceiptID)) {
		return errors.New("goal session journal is invalid")
	}
	if journal.LaunchAttemptedAt.IsZero() != !journal.LaunchAttempted {
		return errors.New("goal session launch attempt metadata is invalid")
	}
	if journal.LaunchState == GoalSessionLaunchStateIntent && journal.LaunchAttempted {
		return errors.New("goal session launch state is invalid")
	}
	if journal.LaunchState == GoalSessionLaunchStateLaunching && !journal.LaunchAttempted {
		return errors.New("goal session launch state is invalid")
	}
	if journal.LaunchState == GoalSessionLaunchStateAttached && (!journal.LaunchAttempted || journal.NativeSessionID == "") {
		return errors.New("goal session native handle is invalid")
	}
	if journal.LaunchState == GoalSessionLaunchStateUncertain && !journal.NeedsReconciliation() {
		return errors.New("goal session uncertainty is invalid")
	}
	if journal.LaunchState == GoalSessionLaunchStateClosed && journal.SessionState != GoalSessionStateClosed {
		return errors.New("goal session closed state is invalid")
	}
	if journal.RecoveryRequired != journal.NeedsReconciliation() {
		return errors.New("goal session recovery marker is invalid")
	}
	if journal.LaunchState == GoalSessionLaunchStateAttached && journal.UncertainReason != "" {
		return errors.New("goal session uncertainty reason is invalid")
	}
	if journal.LaunchState != GoalSessionLaunchStateAttached && (journal.NativeSessionID != "" || journal.NativeSessionFilename != "") {
		return errors.New("goal session native handle is invalid")
	}
	if (journal.ControlSessionID != "" || journal.ControlAttachmentReceiptID != "") && (journal.LaunchState != GoalSessionLaunchStateAttached || journal.BindingID == "" || journal.ControlSessionID == "") {
		return errors.New("goal session control attachment is invalid")
	}
	if journal.ControlAttachmentLineage != nil && !validGoalSessionControlAttachmentLineage(*journal.ControlAttachmentLineage) {
		return errors.New("goal session control attachment lineage is invalid")
	}
	if journal.ControlAttachmentLineage != nil && journal.ControlSessionID != "" &&
		(journal.ControlAttachmentLineage.Original.SessionID != journal.ControlSessionID || journal.ControlAttachmentLineage.Current.SessionID != journal.ControlSessionID) {
		return errors.New("goal session control attachment lineage is invalid")
	}
	if journal.ResumeSourceStopCertificate != nil && !validGoalSessionStopCertificate(*journal.ResumeSourceStopCertificate) {
		return errors.New("goal session resume source receipt is invalid")
	}
	if journal.ConsumedStopCertificates != nil && !validGoalSessionConsumedStopCertificateHistory(*journal.ConsumedStopCertificates) {
		return errors.New("goal session consumed stop receipt history is invalid")
	}
	if journal.ResumeSourceStopCertificate != nil && journal.ConsumedStopCertificates != nil && !journal.ConsumedStopCertificates.containsExact(*journal.ResumeSourceStopCertificate) {
		return errors.New("goal session resume source receipt history is invalid")
	}
	if journal.ResumeStartPending && (journal.SessionMode != GoalSessionModeResume || journal.ResumeSourceStopCertificate == nil || journal.StopCertificate != nil ||
		journal.SessionState != GoalSessionStateBusy || journal.LaunchState != GoalSessionLaunchStateAttached || journal.NativeSessionID == "") {
		return errors.New("goal session resume pre-start state is invalid")
	}
	if journal.ResumeNativeStopKnown && !journal.HasKnownRetainedResumeNativeStop() {
		return errors.New("goal session retained native stop state is invalid")
	}
	if journal.ResumeTerminalReason != "" && (!journal.HasKnownRetainedResumeNativeStop() || journal.ResumeTerminalReason != "resume_rejected") {
		return errors.New("goal session retained resume terminal reason is invalid")
	}
	if journal.StopCertificate != nil && (!validGoalSessionStopCertificate(*journal.StopCertificate) || journal.SessionState != GoalSessionStateAvailable || journal.ControlSessionID != journal.StopCertificate.SessionID || journal.BindingID != journal.StopCertificate.BindingID || journal.RunID != journal.StopCertificate.RunID || journal.Generation != journal.StopCertificate.Generation || journal.LocalHandleID != journal.StopCertificate.LocalHandleID) {
		return errors.New("goal session stop receipt is invalid")
	}
	return nil
}

func validGoalSessionStopCertificate(certificate GoalSessionStopCertificate) bool {
	return validRequiredString(certificate.RunID, 4096) && certificate.Generation > 0 &&
		validGoalSessionUUID(certificate.SessionID) && validGoalSessionUUID(certificate.LocalHandleID) &&
		validGoalSessionUUID(certificate.BindingID) && validGoalDeliveryDigest(certificate.DeliveryDigest) &&
		validGoalSessionUUID(certificate.ReceiptID)
}

func validGoalSessionConsumedStopCertificateHistory(history GoalSessionConsumedStopCertificateHistory) bool {
	if len(history.Certificates) == 0 || len(history.Certificates) > goalSessionConsumedStopLimit {
		return false
	}
	seen := make(map[GoalSessionStopCertificate]struct{}, len(history.Certificates))
	for _, certificate := range history.Certificates {
		if !validGoalSessionStopCertificate(certificate) {
			return false
		}
		if _, exists := seen[certificate]; exists {
			return false
		}
		seen[certificate] = struct{}{}
	}
	return true
}

func validWorkspaceOwnerRunKey(key WorkspaceOwnerRunKey) bool {
	return validRequiredString(key.RunID, 4096) && key.Generation > 0
}

func validGoalSessionControlAttachmentLineage(lineage GoalSessionControlAttachmentLineage) bool {
	return validGoalSessionControlAttachment(lineage.Original) && validGoalSessionControlAttachment(lineage.Current) &&
		lineage.Original.SessionID == lineage.Current.SessionID
}

func validGoalSessionControlAttachment(attachment GoalSessionControlAttachment) bool {
	return validRequiredString(attachment.RunID, 4096) && attachment.Generation > 0 &&
		validRequiredString(attachment.TaskID, 4096) && validRequiredString(attachment.AdmissionID, 4096) &&
		validGoalSessionUUID(attachment.SessionID) && validGoalSessionUUID(attachment.BindingID) &&
		validGoalSessionUUID(attachment.ReceiptID)
}

func goalSessionControlAttachment(journal GoalSessionJournal, sessionID, bindingID, receiptID string) GoalSessionControlAttachment {
	return GoalSessionControlAttachment{
		RunID: journal.RunID, Generation: journal.Generation, TaskID: journal.TaskID, AdmissionID: journal.AdmissionID,
		SessionID: sessionID, BindingID: bindingID, ReceiptID: receiptID,
	}
}

func matchesGoalSessionControlAttachment(attachment GoalSessionControlAttachment, journal GoalSessionJournal) bool {
	return attachment.RunID == journal.RunID && attachment.Generation == journal.Generation &&
		attachment.TaskID == journal.TaskID && attachment.AdmissionID == journal.AdmissionID &&
		attachment.SessionID == journal.ControlSessionID && attachment.BindingID == journal.BindingID &&
		attachment.ReceiptID == journal.ControlAttachmentReceiptID
}

func validateGoalSessionResumeRebind(rebind GoalSessionResumeRebind) error {
	if !validRequiredString(rebind.RunID, 4096) || rebind.Generation <= 0 ||
		!validRequiredString(rebind.TaskID, 4096) || !validRequiredString(rebind.AdmissionID, 4096) ||
		!validGoalSessionUUID(rebind.BindingID) || !validGoalSessionStopCertificate(rebind.ExactStopCertificate) {
		return errors.New("goal session resume rebind is invalid")
	}
	if !completeGoalSessionResumeCompatibility(rebind.Compatibility) {
		return ErrGoalSessionCompatibilityIncomplete
	}
	if rebind.RunID == rebind.ExactStopCertificate.RunID || rebind.BindingID == rebind.ExactStopCertificate.BindingID {
		return ErrGoalSessionConflict
	}
	return nil
}

func completeGoalSessionResumeCompatibility(value GoalSessionCompatibility) bool {
	return validRequiredString(value.MachineID, 4096) && validRequiredString(value.RuntimeID, 4096) &&
		validRequiredString(value.HarnessKind, 4096) &&
		validRequiredString(value.HarnessVersion, 4096) && validRequiredString(value.AdapterVersion, 4096) &&
		value.AdapterProtocolVersion > 0 && validRequiredString(value.WorkspaceFingerprint, 4096) &&
		validGoalSessionUUID(value.RepositoryResourceID)
}

func sameGoalSessionResumeTarget(journal GoalSessionJournal, rebind GoalSessionResumeRebind) bool {
	return journal.RunID == rebind.RunID && journal.Generation == rebind.Generation &&
		journal.TaskID == rebind.TaskID && journal.AdmissionID == rebind.AdmissionID &&
		journal.BindingID == rebind.BindingID && journal.SessionMode == GoalSessionModeResume &&
		journal.SessionState == GoalSessionStateBusy && journal.LaunchState == GoalSessionLaunchStateAttached &&
		journal.StopCertificate == nil && journal.ControlAttachmentReceiptID == "" &&
		journal.ResumeSourceStopCertificate != nil && *journal.ResumeSourceStopCertificate == rebind.ExactStopCertificate &&
		journal.ResumeStartPending && journal.NativeSessionID != "" && !journal.NeedsReconciliation()
}

func validateGoalSessionResumeSource(journal GoalSessionJournal, rebind GoalSessionResumeRebind) error {
	if journal.LaunchState == GoalSessionLaunchStateClosed || journal.SessionState == GoalSessionStateClosed {
		return ErrGoalSessionClosed
	}
	if journal.NeedsReconciliation() {
		return ErrGoalSessionUncertain
	}
	if journal.ResumeSourceStopCertificate != nil {
		// A prior resume CAS has already consumed an available certificate. The
		// exact target replay was handled above; every other request is a stale
		// concurrent consumer, not an incomplete source record.
		return ErrGoalSessionConflict
	}
	if journal.SessionState != GoalSessionStateAvailable || journal.LaunchState != GoalSessionLaunchStateAttached ||
		journal.NativeSessionID == "" || !journal.HasVerifiedControlAttachment() ||
		journal.StopCertificate == nil || !validWorkspaceOwnerRunKey(journal.WorkspaceOwnerRunKey) ||
		journal.RepositoryResourceID == "" {
		return ErrGoalSessionCompatibilityIncomplete
	}
	if *journal.StopCertificate != rebind.ExactStopCertificate {
		return ErrGoalSessionConflict
	}
	if err := compareGoalSessionResumeCompatibility(journal, rebind.Compatibility); err != nil {
		return err
	}
	return nil
}

func compareGoalSessionResumeCompatibility(journal GoalSessionJournal, expected GoalSessionCompatibility) error {
	if !completeGoalSessionResumeCompatibility(expected) || !completeGoalSessionResumeCompatibility(journal.Compatibility()) {
		return ErrGoalSessionCompatibilityIncomplete
	}
	if journal.MachineID != expected.MachineID || journal.RuntimeID != expected.RuntimeID {
		return ErrGoalSessionOwnerMismatch
	}
	if journal.HarnessKind != expected.HarnessKind || journal.HarnessVersion != expected.HarnessVersion ||
		journal.AdapterVersion != expected.AdapterVersion || journal.AdapterProtocolVersion != expected.AdapterProtocolVersion {
		return ErrGoalSessionVersionMismatch
	}
	if journal.WorkspaceFingerprint != expected.WorkspaceFingerprint || journal.RepositoryResourceID != expected.RepositoryResourceID {
		return ErrGoalSessionWorkspaceMismatch
	}
	return nil
}

func sameGoalSessionCertificateExecution(left, right GoalSessionStopCertificate) bool {
	return left.RunID == right.RunID && left.Generation == right.Generation &&
		left.SessionID == right.SessionID && left.LocalHandleID == right.LocalHandleID
}

func (journal *GoalSessionJournal) appendConsumedStopCertificate(certificate GoalSessionStopCertificate) error {
	if !validGoalSessionStopCertificate(certificate) {
		return errors.New("goal session consumed stop receipt is invalid")
	}
	history := GoalSessionConsumedStopCertificateHistory{}
	if journal.ConsumedStopCertificates != nil {
		history.Certificates = append(history.Certificates, journal.ConsumedStopCertificates.Certificates...)
	}
	for _, existing := range history.Certificates {
		if existing == certificate {
			return ErrGoalSessionConflict
		}
	}
	if len(history.Certificates) >= goalSessionConsumedStopLimit {
		return ErrGoalSessionConflict
	}
	history.Certificates = append(history.Certificates, certificate)
	journal.ConsumedStopCertificates = &history
	return nil
}

func (history GoalSessionConsumedStopCertificateHistory) containsExact(certificate GoalSessionStopCertificate) bool {
	for _, existing := range history.Certificates {
		if existing == certificate {
			return true
		}
	}
	return false
}

// consumedStopCertificate reports whether the certificate refers to a known
// consumed predecessor and whether it is an exact receipt match. A same
// execution with changed receipt data is never safe to acknowledge.
func (journal GoalSessionJournal) consumedStopCertificate(certificate GoalSessionStopCertificate) (bool, bool) {
	if journal.ConsumedStopCertificates != nil {
		for _, existing := range journal.ConsumedStopCertificates.Certificates {
			if existing == certificate {
				return true, true
			}
			if sameGoalSessionCertificateExecution(existing, certificate) {
				return true, false
			}
		}
	}
	// Journals written by the first retained-resume implementation can contain
	// ResumeSourceStopCertificate without the later durable history. Preserve an
	// exact predecessor ACK until that current Run reaches availability, where
	// MarkGoalSessionAvailable promotes it into history before clearing source.
	if journal.ConsumedStopCertificates == nil && journal.ResumeSourceStopCertificate != nil {
		if *journal.ResumeSourceStopCertificate == certificate {
			return true, true
		}
		if sameGoalSessionCertificateExecution(*journal.ResumeSourceStopCertificate, certificate) {
			return true, false
		}
	}
	return false, false
}

func (journal GoalSessionJournal) resumeTargetConflictsWithConsumedHistory(rebind GoalSessionResumeRebind) bool {
	if journal.ConsumedStopCertificates == nil {
		return false
	}
	for _, certificate := range journal.ConsumedStopCertificates.Certificates {
		if rebind.RunID == certificate.RunID || rebind.BindingID == certificate.BindingID {
			return true
		}
	}
	return false
}

func validateGoalSessionHandle(handle GoalSessionHandle) error {
	if !validRequiredString(handle.NativeSessionID, 65536) || len(handle.NativeSessionFilename) > 32768 {
		return errors.New("goal session native handle is invalid")
	}
	return nil
}

func validGoalSessionState(value string) bool {
	switch value {
	case GoalSessionStateAvailable, GoalSessionStateBusy, GoalSessionStateUnavailable, GoalSessionStateClosed:
		return true
	default:
		return false
	}
}

func validGoalSessionLaunchState(value string) bool {
	switch value {
	case GoalSessionLaunchStateIntent, GoalSessionLaunchStateLaunching, GoalSessionLaunchStateAttached, GoalSessionLaunchStateUncertain, GoalSessionLaunchStateClosed:
		return true
	default:
		return false
	}
}

func validGoalSessionLineageState(value GoalSessionLineageState) bool {
	switch value {
	case GoalSessionLineageStateReserved, GoalSessionLineageStateLaunching, GoalSessionLineageStateAttached, GoalSessionLineageStateUncertain:
		return true
	default:
		return false
	}
}

func validGoalSessionMode(value string) bool {
	switch value {
	case GoalSessionModeFresh, GoalSessionModeResume, GoalSessionModeHandoff:
		return true
	default:
		return false
	}
}

func validGoalSessionUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return value[14] >= '1' && value[14] <= '5' && strings.ContainsRune("89ab", rune(value[19]))
}

func sameGoalSessionIntent(left, right GoalSessionLaunchIntent) bool {
	return left == right
}

func compareGoalSessionCompatibility(journal GoalSessionJournal, expected GoalSessionCompatibility) error {
	if err := compareGoalSessionIdentity(journal, expected); err != nil {
		return err
	}
	if journal.LaunchState == GoalSessionLaunchStateClosed || journal.SessionState == GoalSessionStateClosed {
		return ErrGoalSessionClosed
	}
	if journal.NeedsReconciliation() {
		return ErrGoalSessionUncertain
	}
	if journal.IsLegacyControlAttachment() || !journal.HasVerifiedControlAttachment() {
		return ErrGoalSessionCompatibilityIncomplete
	}
	if journal.LaunchState != GoalSessionLaunchStateAttached || journal.NativeSessionID == "" {
		return ErrGoalSessionNotAttached
	}
	return nil
}

func compareGoalSessionIdentity(journal GoalSessionJournal, expected GoalSessionCompatibility) error {
	if expected.OwnerID != "" && journal.OwnerID != expected.OwnerID {
		return ErrGoalSessionOwnerMismatch
	}
	if expected.MachineID != "" && journal.MachineID != expected.MachineID {
		return ErrGoalSessionOwnerMismatch
	}
	if expected.RuntimeID != "" && journal.RuntimeID != expected.RuntimeID {
		return ErrGoalSessionOwnerMismatch
	}
	if expected.RuntimeEpoch > 0 && journal.RuntimeEpoch != expected.RuntimeEpoch {
		return ErrGoalSessionOwnerMismatch
	}
	if expected.DaemonInstanceID != "" && journal.DaemonInstanceID != expected.DaemonInstanceID {
		return ErrGoalSessionOwnerMismatch
	}
	if expected.HarnessKind != "" && journal.HarnessKind != expected.HarnessKind || expected.HarnessVersion != "" && journal.HarnessVersion != expected.HarnessVersion || expected.AdapterVersion != "" && journal.AdapterVersion != expected.AdapterVersion || expected.AdapterProtocolVersion > 0 && journal.AdapterProtocolVersion != expected.AdapterProtocolVersion {
		return ErrGoalSessionVersionMismatch
	}
	if expected.WorkspaceFingerprint != "" && journal.WorkspaceFingerprint != expected.WorkspaceFingerprint {
		return ErrGoalSessionWorkspaceMismatch
	}
	if expected.RepositoryResourceID != "" && journal.RepositoryResourceID != expected.RepositoryResourceID {
		return ErrGoalSessionWorkspaceMismatch
	}
	return nil
}

// compareGoalSessionIdentityStrict is used only for reconciliation operations
// that can release or reattach an unresolved external effect. Unlike the
// ordinary compatibility check, every recorded field is compared exactly;
// zero values are never interpreted as caller wildcards.
func compareGoalSessionIdentityStrict(journal GoalSessionJournal, expected GoalSessionCompatibility) error {
	recorded := journal.Compatibility()
	if expected == (GoalSessionCompatibility{}) ||
		(recorded.OwnerID != "" && expected.OwnerID == "") ||
		(recorded.MachineID != "" && expected.MachineID == "") ||
		(recorded.RuntimeID != "" && expected.RuntimeID == "") ||
		(recorded.RuntimeEpoch > 0 && expected.RuntimeEpoch <= 0) ||
		(recorded.DaemonInstanceID != "" && expected.DaemonInstanceID == "") ||
		(recorded.HarnessKind != "" && expected.HarnessKind == "") ||
		(recorded.HarnessVersion != "" && expected.HarnessVersion == "") ||
		(recorded.AdapterVersion != "" && expected.AdapterVersion == "") ||
		(recorded.AdapterProtocolVersion > 0 && expected.AdapterProtocolVersion <= 0) ||
		(recorded.WorkspaceFingerprint != "" && expected.WorkspaceFingerprint == "") ||
		(recorded.RepositoryResourceID != "" && expected.RepositoryResourceID == "") {
		return ErrGoalSessionCompatibilityIncomplete
	}
	if expected.OwnerID != recorded.OwnerID || expected.MachineID != recorded.MachineID || expected.RuntimeID != recorded.RuntimeID || expected.RuntimeEpoch != recorded.RuntimeEpoch || expected.DaemonInstanceID != recorded.DaemonInstanceID {
		return ErrGoalSessionOwnerMismatch
	}
	if expected.HarnessKind != recorded.HarnessKind || expected.HarnessVersion != recorded.HarnessVersion || expected.AdapterVersion != recorded.AdapterVersion || expected.AdapterProtocolVersion != recorded.AdapterProtocolVersion {
		return ErrGoalSessionVersionMismatch
	}
	if expected.WorkspaceFingerprint != recorded.WorkspaceFingerprint || expected.RepositoryResourceID != recorded.RepositoryResourceID {
		return ErrGoalSessionWorkspaceMismatch
	}
	return nil
}

func removeOwnedGoalSessionTemps(directory string, entries []os.DirEntry) error {
	for _, entry := range entries {
		if entry.IsDir() || !isOwnedTemp(entry.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("remove goal session temporary file")
		}
	}
	return nil
}

func isGoalSessionFile(name string) bool {
	if !strings.HasPrefix(name, goalSessionFilePrefix) || !strings.HasSuffix(name, goalSessionFileSuffix) {
		return false
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(name, goalSessionFilePrefix), goalSessionFileSuffix)
	if len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func isGoalSessionLineageFile(name string) bool {
	if !strings.HasPrefix(name, goalSessionLineageFilePrefix) || !strings.HasSuffix(name, goalSessionLineageFileSuffix) {
		return false
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(name, goalSessionLineageFilePrefix), goalSessionLineageFileSuffix)
	if len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}
