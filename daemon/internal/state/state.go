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
	"runtime"
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

// LinuxCgroupReservation is the single RunJournal's v2 run-leaf creation and
// producer-fencing record. Its key/fence are inherited from the enclosing
// journal. Parent and leaf name are immutable reservation intent; matching
// path/inode fields never recreate authority after all live pins are lost.
// AbortProof is an exact pre-effect no-target tombstone, not a stop receipt;
// retaining it makes lost abort-write responses replayable without making the
// aborted leaf a pending containment owner.
type LinuxCgroupReservation struct {
	Version          int                                 `json:"version"`
	LaunchToken      string                              `json:"launch_token"`
	Secret           string                              `json:"secret"`
	PipeToken        string                              `json:"pipe_token"`
	JobID            string                              `json:"job_id"`
	OwnerUID         uint32                              `json:"owner_uid"`
	LauncherUID      uint32                              `json:"launcher_uid"`
	RecoveryEndpoint string                              `json:"recovery_endpoint"`
	Parent           authority.LinuxCgroupParentIdentity `json:"parent"`
	LeafName         string                              `json:"leaf_name"`
	CreateStarted    bool                                `json:"create_started,omitempty"`
	// LauncherGrantPossible is set and fsynced before the grant can reach the
	// root launcher. Once true, the no-target abort path is permanently closed.
	LauncherGrantPossible bool                                        `json:"launcher_grant_possible,omitempty"`
	LeafIdentity          *authority.LinuxCgroupIdentity              `json:"leaf_identity,omitempty"`
	Anchor                authority.LinuxCgroupProcessIdentity        `json:"anchor"`
	Helper                *authority.LinuxCgroupProcessIdentity       `json:"helper,omitempty"`
	Launcher              *authority.LinuxCgroupProcessIdentity       `json:"launcher,omitempty"`
	LauncherExit          *authority.LinuxCgroupExitProof             `json:"launcher_exit,omitempty"`
	Gate                  *authority.LinuxCgroupGateIdentity          `json:"gate,omitempty"`
	HelperExit            *authority.LinuxCgroupExitProof             `json:"helper_exit,omitempty"`
	Mirror                *authority.LinuxCgroupProcessIdentity       `json:"mirror,omitempty"`
	AbortProof            *authority.LinuxCgroupReservationAbortProof `json:"abort_proof,omitempty"`
	PreGateStopReceipt    *authority.LinuxCgroupPreGateStopReceipt    `json:"pre_gate_stop_receipt,omitempty"`
}

const LinuxCgroupReservationVersion = 2

func (value LinuxCgroupReservation) Validate() error {
	if value.Version != LinuxCgroupReservationVersion || !validStateLowerHex(value.LaunchToken, authority.TokenBytes*2) ||
		!validStateLowerHex(value.Secret, authority.SecretBytes*2) || !validStateLowerHex(value.PipeToken, authority.TokenBytes*2) ||
		!validStateLowerHex(value.JobID, authority.TokenBytes*2) || !validRequiredString(value.RecoveryEndpoint, 4096) ||
		value.OwnerUID == 0 || value.LauncherUID != 0 || strings.IndexByte(value.RecoveryEndpoint, 0) >= 0 || value.Parent.Validate() != nil ||
		!validStateLowerHex(value.LeafName, authority.TokenBytes*2) || value.Anchor.Validate() != nil ||
		value.Mirror == nil || value.Mirror.Validate() != nil {
		return errors.New("Linux cgroup reservation is invalid")
	}
	if value.LeafIdentity != nil {
		if !value.CreateStarted || value.LeafIdentity.Validate() != nil ||
			!value.LeafIdentity.Parent.Equal(value.Parent) || value.LeafIdentity.LeafName != value.LeafName {
			return errors.New("Linux cgroup reservation leaf identity is invalid")
		}
	}
	if value.Helper != nil && (value.LeafIdentity == nil || value.Helper.Validate() != nil) {
		return errors.New("Linux cgroup helper identity is invalid")
	}
	if value.Helper != nil && value.Helper.Equal(value.Anchor) {
		return errors.New("Linux cgroup helper aliases the anchor process")
	}
	if value.Launcher != nil && (value.Helper == nil || value.Launcher.Validate() != nil) {
		return errors.New("Linux cgroup launcher identity is invalid")
	}
	if value.LauncherGrantPossible && value.Launcher == nil {
		return errors.New("Linux cgroup launcher grant has no bound launcher")
	}
	if value.Launcher != nil && (value.Launcher.Equal(value.Anchor) || value.Launcher.Equal(*value.Helper)) {
		return errors.New("Linux cgroup launcher aliases another producer")
	}
	if value.LauncherExit != nil && (value.Launcher == nil ||
		value.Helper == nil || !value.LauncherExit.ValidForOwnedDescendant(*value.Launcher, *value.Helper, value.Anchor, value.HelperExit)) {
		return errors.New("Linux cgroup launcher exit proof is invalid")
	}
	if value.Gate != nil && (!value.LauncherGrantPossible || value.LauncherExit == nil || value.LeafIdentity == nil ||
		value.Gate.Validate() != nil || value.Gate.HostUID != value.OwnerUID || !value.Gate.Cgroup.Equal(*value.LeafIdentity)) {
		return errors.New("Linux cgroup gate identity is invalid")
	}
	if value.Gate != nil && (value.Gate.Process.Equal(value.Anchor) || value.Gate.Process.Equal(*value.Helper) ||
		value.Gate.Process.Equal(*value.Launcher)) {
		return errors.New("Linux cgroup gate aliases another producer")
	}
	if value.HelperExit != nil && (value.Helper == nil || !value.HelperExit.ValidFor(*value.Helper, value.Anchor)) {
		return errors.New("Linux cgroup helper exit proof is invalid")
	}
	if value.Mirror.Equal(value.Anchor) || (value.Helper != nil && value.Mirror.Equal(*value.Helper)) ||
		(value.Launcher != nil && value.Mirror.Equal(*value.Launcher)) || (value.Gate != nil && value.Mirror.Equal(value.Gate.Process)) {
		return errors.New("Linux cgroup mirror aliases another producer")
	}
	if value.AbortProof != nil && !linuxCgroupReservationAbortMatches(value, *value.AbortProof) {
		return errors.New("Linux cgroup reservation abort proof is invalid")
	}
	if value.PreGateStopReceipt != nil && !linuxCgroupPreGateReceiptMatchesReservation(value, *value.PreGateStopReceipt) {
		return errors.New("Linux pre-gate cgroup stop receipt is invalid")
	}
	if value.AbortProof != nil && value.PreGateStopReceipt != nil {
		return errors.New("Linux cgroup reservation cannot have both abort and physical-stop proof")
	}
	if value.AbortProof != nil && (value.HelperExit != nil && value.AbortProof.HelperExit == nil ||
		value.LauncherExit != nil && value.AbortProof.LauncherExit == nil) {
		return errors.New("Linux cgroup aborted reservation lost producer-exit evidence")
	}
	if !value.CreateStarted && (value.LeafIdentity != nil || value.Helper != nil || value.Launcher != nil ||
		value.LauncherExit != nil || value.Gate != nil || value.HelperExit != nil ||
		value.AbortProof != nil && value.AbortProof.LeafDisposition != authority.LinuxCgroupAbortLeafNeverCreated) {
		return errors.New("Linux cgroup reservation has effects before creation intent")
	}
	return nil
}

func (value LinuxCgroupReservation) ValidateWithoutPreGateReceipt() error {
	cloned := value.Clone()
	cloned.PreGateStopReceipt = nil
	return cloned.Validate()
}

func (value LinuxCgroupReservation) Clone() LinuxCgroupReservation {
	cloned := value
	if value.LeafIdentity != nil {
		leaf := *value.LeafIdentity
		cloned.LeafIdentity = &leaf
	}
	if value.Helper != nil {
		helper := *value.Helper
		cloned.Helper = &helper
	}
	if value.Launcher != nil {
		launcher := *value.Launcher
		cloned.Launcher = &launcher
	}
	if value.LauncherExit != nil {
		proof := *value.LauncherExit
		cloned.LauncherExit = &proof
	}
	if value.Gate != nil {
		gate := *value.Gate
		cloned.Gate = &gate
	}
	if value.HelperExit != nil {
		proof := *value.HelperExit
		cloned.HelperExit = &proof
	}
	if value.Mirror != nil {
		mirror := *value.Mirror
		cloned.Mirror = &mirror
	}
	if value.AbortProof != nil {
		proof := value.AbortProof.Clone()
		cloned.AbortProof = &proof
	}
	if value.PreGateStopReceipt != nil {
		receipt := value.PreGateStopReceipt.Clone()
		cloned.PreGateStopReceipt = &receipt
	}
	return cloned
}

func (value LinuxCgroupReservation) Equal(other LinuxCgroupReservation) bool {
	return value.Version == other.Version && value.LaunchToken == other.LaunchToken && value.Secret == other.Secret &&
		value.PipeToken == other.PipeToken && value.JobID == other.JobID && value.RecoveryEndpoint == other.RecoveryEndpoint &&
		value.OwnerUID == other.OwnerUID && value.LauncherUID == other.LauncherUID &&
		value.Parent.Equal(other.Parent) && value.LeafName == other.LeafName && value.CreateStarted == other.CreateStarted &&
		value.LauncherGrantPossible == other.LauncherGrantPossible &&
		equalAuthorityCgroupIdentity(value.LeafIdentity, other.LeafIdentity) &&
		value.Anchor.Equal(other.Anchor) && equalAuthorityProcess(value.Helper, other.Helper) &&
		equalAuthorityProcess(value.Launcher, other.Launcher) && equalAuthorityExit(value.LauncherExit, other.LauncherExit) &&
		equalAuthorityGate(value.Gate, other.Gate) && equalAuthorityExit(value.HelperExit, other.HelperExit) &&
		equalAuthorityProcess(value.Mirror, other.Mirror) && equalAuthorityAbortProof(value.AbortProof, other.AbortProof) &&
		equalAuthorityPreGateReceipt(value.PreGateStopReceipt, other.PreGateStopReceipt)
}

func validStateLowerHex(value string, length int) bool {
	return len(value) == length && value == strings.ToLower(value) && validHex(value)
}

func equalAuthorityCgroupIdentity(left, right *authority.LinuxCgroupIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalAuthorityProcess(left, right *authority.LinuxCgroupProcessIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalAuthorityExit(left, right *authority.LinuxCgroupExitProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalAuthorityGate(left, right *authority.LinuxCgroupGateIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalAuthorityAbortProof(left, right *authority.LinuxCgroupReservationAbortProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalAuthorityPreGateReceipt(left, right *authority.LinuxCgroupPreGateStopReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func exactCgroupMutationStage(journal *RunJournal, expected, after LinuxCgroupReservation) (bool, error) {
	if !journal.hasClaimGrant() || journal.LocalState == "cleanup_pending" || journal.ContainmentCgroup == nil ||
		expected.AbortProof != nil || expected.PreGateStopReceipt != nil {
		return false, errors.New("Linux cgroup reservation is not mutable")
	}
	if err := after.Validate(); err != nil {
		return false, err
	}
	current := journal.ContainmentCgroup
	if current.Equal(after) {
		return true, nil
	}
	if current.Equal(expected) {
		return false, nil
	}
	return false, errors.New("Linux cgroup reservation compare-and-set mismatch")
}

func (store *Store) ensureLinuxCgroupLeafAvailableLocked(key RunKey, reservation LinuxCgroupReservation) error {
	entries, err := os.ReadDir(store.runsDir())
	if err != nil {
		return errors.New("list run journals before cgroup reservation")
	}
	for _, entry := range entries {
		if entry.IsDir() || !isJournalFile(entry.Name()) {
			continue
		}
		var existing RunJournal
		path := filepath.Join(store.runsDir(), entry.Name())
		if err := store.readRunJournalJSONWithLimit(path, &existing, maxJournalFileBytes); err != nil ||
			validateJournal(existing) != nil || filepath.Base(store.journalPath(existing.Key())) != entry.Name() {
			return errors.New("cannot establish unique Linux cgroup leaf reservation")
		}
		if existing.Key() == key || existing.ContainmentCgroup == nil {
			continue
		}
		other := existing.ContainmentCgroup
		if other.Parent.Path == reservation.Parent.Path && other.LeafName == reservation.LeafName {
			return errors.New("Linux cgroup leaf path is already reserved")
		}
	}
	return nil
}

func (store *Store) ensureLinuxClonePathsAvailableLocked(key RunKey, intent authority.LinuxCloneIntent) error {
	entries, err := os.ReadDir(store.runsDir())
	if err != nil {
		return errors.New("list run journals before Linux clone reservation")
	}
	for _, entry := range entries {
		if entry.IsDir() || !isJournalFile(entry.Name()) {
			continue
		}
		var existing RunJournal
		path := filepath.Join(store.runsDir(), entry.Name())
		if err := store.readRunJournalJSONWithLimit(path, &existing, maxJournalFileBytes); err != nil ||
			validateJournal(existing) != nil || filepath.Base(store.journalPath(existing.Key())) != entry.Name() {
			return errors.New("cannot establish unique Linux clone paths")
		}
		if existing.Key() == key || existing.LinuxCloneIntent == nil ||
			existing.LinuxCloneAbortProof != nil && !existing.LinuxCloneAbortPending {
			continue
		}
		other := *existing.LinuxCloneIntent
		if linuxClonePathAliases(intent.SeedPath, other.SeedPath) ||
			linuxClonePathAliases(intent.Target, other.Target) ||
			linuxClonePathAliases(intent.SeedPath, other.Target) ||
			linuxClonePathAliases(intent.Target, other.SeedPath) {
			return errors.New("Linux clone seed or target path is already reserved")
		}
	}
	return nil
}

func linuxClonePathAliases(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
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
	// LinuxCloneIntent and the stage receipts are additive v2 clone authority
	// in this existing run journal. Each advances only through its dedicated
	// RunKey/fence CAS; checkout presence never clears process containment.
	LinuxCloneIntent           *authority.LinuxCloneIntent           `json:"linux_clone_intent,omitempty"`
	LinuxCloneSeedReceipt      *authority.LinuxCloneSeedReceipt      `json:"linux_clone_seed_receipt,omitempty"`
	LinuxCloneCheckoutReceipt  *authority.LinuxCloneCheckoutReceipt  `json:"linux_clone_checkout_receipt,omitempty"`
	LinuxClonePromotionReceipt *authority.LinuxClonePromotionReceipt `json:"linux_clone_promotion_receipt,omitempty"`
	// LinuxCloneAbortPending is written before A-side no-checkout cleanup and
	// fences every later gate/checkout stage. LinuxCloneAbortProof is the exact
	// absence tombstone written only after A verifies target, seed and refs gone.
	LinuxCloneAbortPending bool                            `json:"linux_clone_abort_pending,omitempty"`
	LinuxCloneAbortProof   *authority.LinuxCloneAbortProof `json:"linux_clone_abort_proof,omitempty"`
	PID                    int                             `json:"pid"`
	ProcessIdentity        string                          `json:"process_identity"`
	StartedAt              time.Time                       `json:"started_at"`
	// ContainmentAuthority is optional for backwards compatibility with
	// journals created before the independent Windows supervisor existed. A
	// process marker without it remains intentionally unrecoverable on Windows.
	ContainmentAuthority *authority.Supervisor `json:"containment_authority,omitempty"`
	// ContainmentHandoff is the durable pre-authority launch fence. It is
	// mutually exclusive with ContainmentAuthority and is cleared only by the
	// dedicated commit or release-proof mutations.
	ContainmentHandoff *authority.SupervisorHandoff `json:"containment_handoff,omitempty"`
	// ContainmentCgroup is the additive v2 Linux run-leaf reservation and
	// producer ledger. It remains in this same RunJournal from pre-mkdir reserve
	// through receipt-backed release, including while PID is zero.
	ContainmentCgroup *LinuxCgroupReservation `json:"containment_cgroup,omitempty"`
	// ContainmentCgroupReleaseProof is the exact post-removal proof retained in
	// this same RunJournal after atomic v2 clear. It is a replay tombstone, not
	// pending containment; only the exact successful clear is acknowledged.
	ContainmentCgroupReleaseProof *authority.SupervisorHandoffReleaseProof `json:"containment_cgroup_release_proof,omitempty"`
	// ContainmentCgroupPreGateReleaseProof is the matching receipt-bound
	// tombstone for a gate-never-produced, PID-zero reservation clear.
	ContainmentCgroupPreGateReleaseProof *authority.LinuxCgroupPreGateReleaseProof `json:"containment_cgroup_pre_gate_release_proof,omitempty"`
	// ContainmentScans is the additive durable pre-scan/result ledger for Linux
	// helper and daemon-mirror observations. Missing is valid for journals
	// written before scan fencing existed; bound legacy Linux owners then remain
	// unresolved rather than being upgraded from readback or process absence.
	ContainmentScans *ContainmentScanJournal `json:"containment_scans,omitempty"`
	// ContainmentUnproven records that a local stop attempt could not prove the
	// complete owned process boundary. Recovery must retain the marker until a
	// later positive stop proof clears this flag explicitly.
	ContainmentUnproven            bool                              `json:"containment_unproven,omitempty"`
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

// ContainmentScanProgress tracks one scanner's monotonic durable scan fence.
// PendingSequence remains set through CleanResultSequence until the scanner
// receives the durable clean-result ACK and retires that exact fence.
// SealedSequence is a one-way quiescence proof for the exact final clean
// sequence; zero (including a missing legacy field) is never sealed.
// UnprovenSequence is sticky for the lifetime of the process authority.
type ContainmentScanProgress struct {
	PendingSequence     uint64 `json:"pending_sequence,omitempty"`
	CleanResultSequence uint64 `json:"clean_result_sequence,omitempty"`
	CompletedSequence   uint64 `json:"completed_sequence,omitempty"`
	SealedSequence      uint64 `json:"sealed_sequence,omitempty"`
	UnprovenSequence    uint64 `json:"unproven_sequence,omitempty"`
	FailureKind         string `json:"failure_kind,omitempty"`
}

// ContainmentScanJournal binds helper and volatile mirror scan progress to the
// exact Linux launch and one mirror generation recorded by this RunJournal's
// run/generation key.
type ContainmentScanJournal struct {
	Version            int                     `json:"version"`
	LaunchToken        string                  `json:"launch_token"`
	TargetPID          int                     `json:"target_pid"`
	TargetIdentity     string                  `json:"target_identity"`
	SupervisorPID      int                     `json:"supervisor_pid"`
	SupervisorIdentity string                  `json:"supervisor_identity"`
	MirrorIdentity     string                  `json:"mirror_identity"`
	Helper             ContainmentScanProgress `json:"helper"`
	Mirror             ContainmentScanProgress `json:"mirror"`
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

// HasPendingContainment reports a durable containment authority, handoff, scan
// ledger, or v2 leaf reservation. It intentionally does not include the legacy
// process marker so HasProcessDetails retains its existing meaning.
func (journal RunJournal) HasPendingContainment() bool {
	return journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
		journal.ContainmentScans != nil || journal.ContainmentCgroup != nil && journal.ContainmentCgroup.AbortProof == nil
}

// LinuxCloneCleanupEligible reports whether the B-owned clone can be removed
// without using checkout presence as containment or artifact proof. The caller
// must still check retained GoalSession ownership and any in-memory retention
// decision; this state-local predicate covers only durable RunJournal gates.
// An exact no-target abort proof is the exception to RetainWorkspace because
// it establishes that no clone, seed or run-keyed source ref remains.
func (journal RunJournal) LinuxCloneCleanupEligible(key RunKey, fence protocol.Fence) bool {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" || journal.Key() != key || journal.Fence() != fence ||
		(journal.LocalState != "cleanup_pending" && journal.LocalState != "stale") ||
		journal.LocalState == "stale" && journal.TerminalState == "" && journal.LeaseExpiresAt.After(time.Now()) ||
		journal.HasProcessDetails() || journal.HasPendingContainment() ||
		len(journal.PendingCommandAcknowledgements) != 0 || journal.HasPendingGoalDeliveries() ||
		journal.NativeUsageRecoveryRequired || len(journal.PendingTransitions) != 0 || len(journal.PendingEvents) != 0 ||
		journal.LinuxCloneIntent == nil || linuxCloneArtifactReceiptRequired(journal) || validateLinuxCloneJournal(journal) != nil {
		return false
	}
	if journal.LinuxCloneAbortProof != nil {
		return !journal.RetainWorkspace && !journal.LinuxCloneAbortPending && journal.LinuxCloneCheckoutReceipt == nil && journal.LinuxClonePromotionReceipt == nil &&
			journal.WorkspacePath == "" && !journal.WorkspaceRecoveryRequired
	}
	return !journal.LinuxCloneAbortPending && !journal.RetainWorkspace && journal.TerminalState != "cancelled" &&
		journal.LinuxCloneSeedReceipt != nil && journal.LinuxCloneCheckoutReceipt != nil && journal.LinuxClonePromotionReceipt != nil &&
		journal.WorkspacePath == journal.LinuxCloneIntent.Target && journal.WorkspaceBindingKey == journal.LinuxCloneIntent.BindingKey &&
		journal.ContainmentCgroupReleaseProof != nil && journal.ContainmentCgroupReleaseProof.Validate() == nil &&
		journal.ContainmentCgroupReleaseProof.Version == authority.SupervisorHandoffVersionV2
}

func linuxCloneArtifactReceiptRequired(journal RunJournal) bool {
	if !journal.GoalDeliveryEnabled || journal.TerminalState != "completed" {
		return false
	}
	admission, present, err := protocol.ParseAdmissionInput(journal.Work.Input)
	if err != nil || !present {
		return true
	}
	if admission.Purpose != protocol.AdmissionPurposeImplement {
		return false
	}
	switch journal.TerminalTaskResultKind {
	case protocol.TaskResultCandidateCompletion:
		for _, delivery := range journal.DeliveredGoalDeliveries {
			if delivery.Kind == GoalDeliveryEvidence && delivery.Evidence != nil && delivery.Evidence.Kind == protocol.EvidenceArtifact {
				return false
			}
		}
		return true
	case protocol.TaskResultProgress, protocol.TaskResultBlocked, protocol.TaskResultRepairRequired,
		protocol.TaskResultReplanRequired, protocol.TaskResultFailed, protocol.TaskResultPlanProposed:
		return false
	default:
		return true
	}
}

// ContainmentObservationUnresolved reports whether bound Linux scan evidence
// is pending, sticky-unproven, incomplete, absent, or clean-but-unsealed. A
// periodic clean scan is not a terminal witness; only exact durable seals for
// both scanner owners permit stop-receipt recovery. Missing legacy fields stay
// unsealed and cannot be upgraded from process absence.
func (journal RunJournal) ContainmentObservationUnresolved() bool {
	if journal.ContainmentCgroup != nil {
		if journal.ContainmentCgroup.AbortProof != nil {
			return false
		}
		if journal.ContainmentCgroup.PreGateStopReceipt != nil &&
			linuxCgroupPreGateReceiptMatchesReservation(*journal.ContainmentCgroup, *journal.ContainmentCgroup.PreGateStopReceipt) {
			return false
		}
		if authorityValue := journal.ContainmentAuthority; authorityValue != nil &&
			authorityValue.Version == authority.SupervisorVersionV2 && authorityValue.StopReceipt != nil &&
			authorityValue.StopReceipt.ValidFor(*authorityValue) {
			return false
		}
		if handoff := journal.ContainmentHandoff; handoff != nil &&
			handoff.Version == authority.SupervisorHandoffVersionV2 && handoff.StopReceipt != nil &&
			handoff.StopReceipt.ValidForHandoff(*handoff) {
			return false
		}
		return true
	}
	if journal.ContainmentUnproven {
		return true
	}
	if journal.ContainmentScans != nil {
		return !journal.ContainmentScans.sealed()
	}
	if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.OwnerKind == authority.OwnerKindLinuxHelper {
		return true
	}
	if handoff := journal.ContainmentHandoff; handoff != nil && handoff.OwnerKind == authority.OwnerKindLinuxHelper && handoff.SupervisorPID > 0 {
		return true
	}
	return false
}

// RecoveryHelperScanSealEligible reports whether an authenticated live helper
// can complete exactly the next terminal sequence during daemon-loss recovery.
// It never permits recovery to seal a vanished mirror or advance pending scan
// evidence.
func (journal RunJournal) RecoveryHelperScanSealEligible() bool {
	value := journal.ContainmentAuthority
	scans := journal.ContainmentScans
	if journal.ContainmentCgroup != nil || journal.ContainmentUnproven || value == nil || value.OwnerKind != authority.OwnerKindLinuxHelper ||
		value.StopReceipt != nil || scans == nil || !scans.matchesAuthority(*value) || !scans.Mirror.sealed() {
		return false
	}
	progress := scans.Helper
	return progress.CompletedSequence > 0 && progress.CompletedSequence != ^uint64(0) && progress.PendingSequence == 0 && progress.CleanResultSequence == 0 &&
		progress.SealedSequence == 0 && progress.UnprovenSequence == 0 && progress.FailureKind == ""
}

func (scans ContainmentScanJournal) clean() bool {
	return scans.Helper.PendingSequence == 0 && scans.Helper.CleanResultSequence == 0 && scans.Helper.UnprovenSequence == 0 && scans.Helper.CompletedSequence > 0 &&
		scans.Mirror.PendingSequence == 0 && scans.Mirror.CleanResultSequence == 0 && scans.Mirror.UnprovenSequence == 0 && scans.Mirror.CompletedSequence > 0
}

func (progress ContainmentScanProgress) sealed() bool {
	return progress.CompletedSequence > 0 && progress.SealedSequence == progress.CompletedSequence &&
		progress.PendingSequence == 0 && progress.CleanResultSequence == 0 && progress.UnprovenSequence == 0 && progress.FailureKind == ""
}

func (scans ContainmentScanJournal) sealed() bool {
	return scans.Helper.sealed() && scans.Mirror.sealed()
}

func (scans ContainmentScanJournal) matchesHandoff(handoff authority.SupervisorHandoff) bool {
	return scans.Version == 1 && scans.LaunchToken == handoff.LaunchToken && scans.TargetPID == handoff.TargetPID &&
		scans.TargetIdentity == handoff.TargetIdentity && scans.SupervisorPID == handoff.SupervisorPID &&
		scans.SupervisorIdentity == handoff.SupervisorIdentity
}

func (scans ContainmentScanJournal) matchesAuthority(value authority.Supervisor) bool {
	return scans.Version == 1 && scans.LaunchToken == value.LaunchToken && scans.TargetPID == value.TargetPID &&
		scans.TargetIdentity == value.TargetIdentity && scans.SupervisorPID == value.SupervisorPID &&
		scans.SupervisorIdentity == value.SupervisorIdentity
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

// ReserveLinuxCloneIntent durably reserves the exact RunKey, current admitted
// Subject, seed path and B-owned target before the workspace service performs
// any Git/ref/bundle or checkout side effect.
func (store *Store) ReserveLinuxCloneIntent(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := intent.Validate(); err != nil || intent.RunID != key.RunID || intent.Generation != key.Generation {
		return RunJournal{}, errors.New("Linux clone intent does not match RunKey")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if err := store.ensureLinuxClonePathsAvailableLocked(key, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent != nil {
			if !journal.LinuxCloneIntent.Equal(intent) {
				return errors.New("Linux clone intent cannot be replaced")
			}
			if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
				return errors.New("Linux clone intent is fenced for pre-checkout abort")
			}
			return nil
		}
		if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" || journal.LocalState == "stale" ||
			journal.HasProcessDetails() || journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil ||
			journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("Linux clone intent must be reserved before process containment or terminal cleanup")
		}
		if journal.WorkspacePath != "" {
			return errors.New("Linux clone intent cannot adopt a pre-existing workspace path")
		}
		copy := intent.Clone()
		journal.LinuxCloneIntent = &copy
		journal.WorkspacePath = intent.Target
		journal.WorkspaceBindingKey = intent.BindingKey
		journal.WorkspaceRecoveryRequired = true
		return nil
	})
}

// BindLinuxCloneSeed records the A-measured immutable seed bundle receipt only
// after its exact reserved intent has been persisted.
func (store *Store) BindLinuxCloneSeed(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, seed authority.LinuxCloneSeedReceipt) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := seed.Validate(); err != nil || seed.BaseCommit != intent.BaseCommit {
		return RunJournal{}, errors.New("Linux clone seed receipt does not match its intent")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) {
			return errors.New("Linux clone seed intent is not durably reserved")
		}
		if journal.LinuxCloneSeedReceipt != nil {
			if !journal.LinuxCloneSeedReceipt.Equal(seed) {
				return errors.New("Linux clone seed receipt cannot be changed")
			}
			return nil
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil || !linuxCloneStageCanAdvance(*journal) {
			return errors.New("Linux clone seed cannot advance after abort or terminal fencing")
		}
		copy := seed.Clone()
		journal.LinuxCloneSeedReceipt = &copy
		return nil
	})
}

// RecordLinuxCloneCheckoutReady binds the B-created target inode to the
// reserved intent and seed. The initial write is legal only after the v2
// handoff is durably committed; the trusted same-PID gate calls it after its
// existing initial-lease-ACK/RELEASE barrier and before native exec.
func (store *Store) RecordLinuxCloneCheckoutReady(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, receipt authority.LinuxCloneCheckoutReceipt) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := receipt.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) || journal.LinuxCloneSeedReceipt == nil {
			return errors.New("Linux clone checkout is missing its reserved intent or seed")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("Linux clone checkout is fenced for pre-checkout abort")
		}
		if !linuxCloneCheckoutMatches(intent, *journal.LinuxCloneSeedReceipt, receipt) {
			return errors.New("Linux clone checkout receipt does not match its intent and seed")
		}
		if journal.LinuxCloneCheckoutReceipt != nil {
			if !journal.LinuxCloneCheckoutReceipt.Equal(receipt) {
				return errors.New("Linux clone checkout receipt cannot be changed")
			}
			return nil
		}
		if !linuxCloneStageCanAdvance(*journal) {
			return errors.New("Linux clone checkout cannot advance after terminal fencing")
		}
		if !linuxCloneCommittedGateReady(*journal) {
			return errors.New("Linux clone checkout requires committed v2 gate authority")
		}
		copy := receipt.Clone()
		journal.LinuxCloneCheckoutReceipt = &copy
		return nil
	})
}

// RecordLinuxClonePromotion records only an A-validated candidate receipt for
// the deterministic run-keyed A-owned promotion ref, after durable physical
// stop proof. It never clears or advances containment state.
func (store *Store) RecordLinuxClonePromotion(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, receipt authority.LinuxClonePromotionReceipt) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := receipt.Validate(); err != nil {
		return RunJournal{}, err
	}
	ref, err := authority.LinuxClonePromotionRef(key.RunID, key.Generation)
	if err != nil || receipt.Ref != ref {
		return RunJournal{}, errors.New("Linux clone promotion ref does not match RunKey")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) ||
			journal.LinuxCloneSeedReceipt == nil || journal.LinuxCloneCheckoutReceipt == nil {
			return errors.New("Linux clone promotion is missing its reserved checkout evidence")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("Linux clone promotion is fenced for pre-checkout abort")
		}
		if !linuxCloneStopProven(*journal) {
			return errors.New("Linux clone promotion requires durable v2 physical-stop proof")
		}
		if journal.LinuxClonePromotionReceipt != nil {
			if !journal.LinuxClonePromotionReceipt.Equal(receipt) {
				return errors.New("Linux clone promotion receipt cannot be changed")
			}
			return nil
		}
		if !linuxCloneStageCanAdvance(*journal) {
			return errors.New("Linux clone promotion cannot advance after terminal fencing")
		}
		copy := receipt.Clone()
		journal.LinuxClonePromotionReceipt = &copy
		return nil
	})
}

// FenceLinuxCloneBeforeCheckout closes every B-side launch path before A
// removes a reserved seed or source ref. Replaying the same fence only adds a
// fresh persistence barrier; an existing checkout cannot enter this stage.
func (store *Store) FenceLinuxCloneBeforeCheckout(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := intent.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) ||
			journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionReceipt != nil {
			return errors.New("Linux clone is not eligible for pre-checkout abort fencing")
		}
		if journal.LinuxCloneAbortProof != nil {
			if journal.LinuxCloneAbortProof.Intent.Equal(intent) && !journal.LinuxCloneAbortPending {
				return nil
			}
			return errors.New("Linux clone abort fence conflicts with completed proof")
		}
		if journal.LinuxCloneAbortPending {
			return nil
		}
		if !linuxCloneAbortFenceAvailable(*journal) {
			return errors.New("Linux clone abort fence requires no producer or a durable no-gate abort proof")
		}
		journal.LinuxCloneAbortPending = true
		journal.WorkspacePath = intent.Target
		journal.WorkspaceBindingKey = intent.BindingKey
		journal.WorkspaceRecoveryRequired = true
		return nil
	})
}

// CompleteLinuxCloneAbort records exact A-side proof that the target, seed and
// run-keyed refs are absent after FenceLinuxCloneBeforeCheckout. It does not
// create checkout, promotion, or physical-stop evidence.
func (store *Store) CompleteLinuxCloneAbort(key RunKey, fence protocol.Fence, intent authority.LinuxCloneIntent, proof authority.LinuxCloneAbortProof) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(intent) || !proof.Intent.Equal(intent) ||
			journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionReceipt != nil {
			return errors.New("Linux clone abort proof does not match a pre-checkout intent")
		}
		if journal.LinuxCloneAbortProof != nil {
			if !journal.LinuxCloneAbortProof.Equal(proof) || journal.LinuxCloneAbortPending {
				return errors.New("Linux clone abort proof replay conflicts with journal")
			}
			return nil
		}
		if !journal.LinuxCloneAbortPending {
			return errors.New("Linux clone abort completion has no durable creator fence")
		}
		if journal.LinuxCloneSeedReceipt != nil &&
			(proof.SeedReceipt == nil || !journal.LinuxCloneSeedReceipt.Equal(*proof.SeedReceipt)) {
			return errors.New("Linux clone abort proof does not preserve the bound seed receipt")
		}
		if !linuxCloneAbortHistoryMatches(*journal, &proof) {
			return errors.New("Linux clone abort proof does not match durable no-gate history")
		}
		copy := proof.Clone()
		journal.LinuxCloneAbortProof = &copy
		journal.LinuxCloneAbortPending = false
		journal.WorkspacePath = ""
		journal.WorkspaceRecoveryRequired = false
		return nil
	})
}

// ClearLinuxCloneNoTargetRetention releases a retained-workspace barrier only
// after App has independently checked that no retained GoalSession or other
// retention owner remains. The exact Store-bound absence proof and RunKey fence
// make the mutation replayable; ordinary checkout/promotion retention cannot
// be cleared through this path.
func (store *Store) ClearLinuxCloneNoTargetRetention(key RunKey, fence protocol.Fence, proof authority.LinuxCloneAbortProof) (RunJournal, error) {
	if err := requireLinuxClonePlatform(); err != nil {
		return RunJournal{}, err
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := requireLinuxCloneJournalFence(*journal, fence, proof.Intent); err != nil {
			return err
		}
		if journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(proof.Intent) ||
			journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof == nil ||
			!journal.LinuxCloneAbortProof.Equal(proof) || journal.LinuxCloneCheckoutReceipt != nil ||
			journal.LinuxClonePromotionReceipt != nil || journal.WorkspacePath != "" || journal.WorkspaceRecoveryRequired ||
			!linuxCloneAbortHistoryMatches(*journal, &proof) {
			return errors.New("Linux clone retention clear lacks exact no-target abort proof")
		}
		journal.RetainWorkspace = false
		return nil
	})
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
		if !sameContainmentCgroupReleaseProof(current.ContainmentCgroupReleaseProof, journal.ContainmentCgroupReleaseProof) {
			return errors.New("v2 cgroup release proof requires dedicated clear mutation")
		}
		if !sameContainmentPreGateReleaseProof(current.ContainmentCgroupPreGateReleaseProof, journal.ContainmentCgroupPreGateReleaseProof) {
			return errors.New("v2 pre-gate release proof requires dedicated clear mutation")
		}
		if !sameLinuxCloneIntent(current.LinuxCloneIntent, journal.LinuxCloneIntent) ||
			!sameLinuxCloneSeedReceipt(current.LinuxCloneSeedReceipt, journal.LinuxCloneSeedReceipt) ||
			!sameLinuxCloneCheckoutReceipt(current.LinuxCloneCheckoutReceipt, journal.LinuxCloneCheckoutReceipt) ||
			!sameLinuxClonePromotionReceipt(current.LinuxClonePromotionReceipt, journal.LinuxClonePromotionReceipt) ||
			current.LinuxCloneAbortPending != journal.LinuxCloneAbortPending ||
			!sameLinuxCloneAbortProof(current.LinuxCloneAbortProof, journal.LinuxCloneAbortProof) {
			return errors.New("Linux clone authority requires dedicated mutation")
		}
		if current.ContainmentCgroup != nil || journal.ContainmentCgroup != nil ||
			current.LinuxCloneIntent != nil || journal.LinuxCloneIntent != nil {
			if current.RuntimeKey != journal.RuntimeKey || current.RuntimeID != journal.RuntimeID ||
				current.ClaimedRuntimeEpoch != journal.ClaimedRuntimeEpoch || current.ClaimID != journal.ClaimID ||
				current.LeaseToken != journal.LeaseToken || !current.LeaseExpiresAt.Equal(journal.LeaseExpiresAt) {
				return errors.New("run fence requires dedicated mutation while v2 authority is reserved")
			}
		}
		if current.TerminalTaskResultKind != journal.TerminalTaskResultKind {
			return errors.New("terminal task result kind is immutable")
		}
		if current.PID != journal.PID || current.ProcessIdentity != journal.ProcessIdentity || !current.StartedAt.Equal(journal.StartedAt) {
			return errors.New("process details require dedicated mutation")
		}
		if current.ContainmentUnproven != journal.ContainmentUnproven {
			return errors.New("containment uncertainty requires dedicated mutation")
		}
		if !sameContainmentAuthority(current.ContainmentAuthority, journal.ContainmentAuthority) {
			return errors.New("containment authority requires dedicated mutation")
		}
		if !sameContainmentHandoff(current.ContainmentHandoff, journal.ContainmentHandoff) {
			return errors.New("containment handoff requires dedicated mutation")
		}
		if !sameContainmentCgroup(current.ContainmentCgroup, journal.ContainmentCgroup) {
			return errors.New("v2 cgroup reservation requires dedicated mutation")
		}
		if !sameContainmentScans(current.ContainmentScans, journal.ContainmentScans) {
			return errors.New("containment scan progress requires dedicated mutation")
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
		if journal.ContainmentScans != nil {
			return errors.New("containment scan progress requires dedicated mutation")
		}
		if journal.ContainmentCgroup != nil {
			return errors.New("v2 cgroup reservation requires dedicated mutation")
		}
		if journal.ContainmentCgroupReleaseProof != nil {
			return errors.New("v2 cgroup release proof requires dedicated clear mutation")
		}
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("v2 pre-gate release proof requires dedicated clear mutation")
		}
		if journal.LinuxCloneIntent != nil || journal.LinuxCloneSeedReceipt != nil ||
			journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionReceipt != nil ||
			journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("Linux clone authority requires dedicated mutation")
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
	if journal.LinuxCloneIntent != nil && !journal.LinuxCloneCleanupEligible(key, journal.Fence()) {
		return errors.New("Linux clone journal is not eligible for workspace deletion")
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
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("process launch is fenced by Linux clone abort")
		}
		if journal.LinuxCloneIntent != nil {
			return errors.New("Linux clone run requires v2 cgroup process registration")
		}
		if journal.ContainmentCgroup != nil {
			return errors.New("process details require v2 cgroup handoff mutation")
		}
		if journal.ContainmentCgroupReleaseProof != nil {
			return errors.New("process details cannot be recreated after v2 containment release")
		}
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("process details cannot be recreated after v2 pre-gate release")
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
	if value.Version == authority.SupervisorVersionV2 {
		return RunJournal{}, errors.New("v2 containment authority requires supervisor handoff commit")
	}
	if value.TargetPID != pid || value.TargetIdentity != identity {
		return RunJournal{}, errors.New("containment authority target does not match process details")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("process launch is fenced by Linux clone abort")
		}
		if journal.LinuxCloneIntent != nil {
			return errors.New("Linux clone run requires v2 cgroup process registration")
		}
		if journal.ContainmentHandoff != nil {
			return errors.New("process details require supervisor handoff mutation")
		}
		if journal.ContainmentCgroup != nil {
			return errors.New("process details require v2 cgroup handoff mutation")
		}
		if journal.ContainmentCgroupReleaseProof != nil {
			return errors.New("process details cannot be recreated after v2 containment release")
		}
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("process details cannot be recreated after v2 pre-gate release")
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

// MarkContainmentUnproven retains an exact process marker when local cleanup
// cannot prove that the complete owned boundary stopped. It is idempotent for
// the same process identity and survives daemon restart.
func (store *Store) MarkContainmentUnproven(key RunKey, pid int, identity string) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) {
		return RunJournal{}, errors.New("process details are invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.PID != pid || journal.ProcessIdentity != identity || !journal.HasProcessDetails() {
			return errors.New("containment uncertainty requires the exact process marker")
		}
		journal.ContainmentUnproven = true
		return nil
	})
}

// ClearContainmentUnproven clears a prior uncertainty marker only after the
// caller has completed a fresh identity-bound stop proof.
func (store *Store) ClearContainmentUnproven(key RunKey, pid int, identity string) (RunJournal, error) {
	if pid <= 0 || !validRequiredString(identity, 4096) {
		return RunJournal{}, errors.New("process details are invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentCgroup != nil {
			return errors.New("v2 containment uncertainty clears only with an exact stop receipt")
		}
		if !journal.HasProcessDetails() {
			return nil
		}
		if journal.PID != pid || journal.ProcessIdentity != identity {
			return errors.New("persisted process details changed during containment proof")
		}
		if journal.ContainmentScans != nil && !journal.ContainmentScans.clean() {
			return errors.New("containment scan uncertainty is sticky")
		}
		if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.OwnerKind == authority.OwnerKindLinuxHelper && journal.ContainmentScans == nil {
			return errors.New("Linux containment scan fence is missing")
		}
		journal.ContainmentUnproven = false
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
	if value.Version == authority.SupervisorVersionV2 {
		return RunJournal{}, errors.New("v2 containment authority requires supervisor handoff commit")
	}
	if value.TargetPID != pid || value.TargetIdentity != identity {
		return RunJournal{}, errors.New("containment authority target does not match process details")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("process launch is fenced by Linux clone abort")
		}
		if journal.LinuxCloneIntent != nil {
			return errors.New("Linux clone run requires v2 cgroup process registration")
		}
		if journal.ContainmentCgroup != nil {
			return errors.New("v2 containment authority requires supervisor handoff commit")
		}
		if journal.ContainmentCgroupReleaseProof != nil {
			return errors.New("v2 containment authority cannot be recreated after release")
		}
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("v2 pre-gate containment authority cannot be recreated after release")
		}
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

// ReserveLinuxCgroup durably reserves the immutable run leaf before any mkdir
// or producer launch. Exact retries write the same journal again, including a
// fresh filesystem persistence barrier after an unknown earlier result.
func (store *Store) ReserveLinuxCgroup(key RunKey, reservation LinuxCgroupReservation) (RunJournal, error) {
	if err := reservation.Validate(); err != nil {
		return RunJournal{}, err
	}
	if reservation.CreateStarted || reservation.LeafIdentity != nil || reservation.Helper != nil ||
		reservation.Launcher != nil || reservation.LauncherExit != nil || reservation.Gate != nil ||
		reservation.HelperExit != nil || reservation.AbortProof != nil || reservation.PreGateStopReceipt != nil ||
		reservation.Mirror == nil {
		return RunJournal{}, errors.New("Linux cgroup reservation must precede creation")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("Linux cgroup reservation is fenced by clone abort")
		}
		if journal.LocalState == "cleanup_pending" || journal.HasProcessDetails() ||
			journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil ||
			journal.ContainmentScans != nil || journal.ContainmentUnproven || journal.ContainmentCgroupReleaseProof != nil ||
			journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("Linux cgroup reservation conflicts with existing containment")
		}
		if journal.ContainmentCgroup != nil {
			if journal.ContainmentCgroup.Equal(reservation) {
				return nil
			}
			return errors.New("Linux cgroup reservation conflicts with journal")
		}
		if err := store.ensureLinuxCgroupLeafAvailableLocked(key, reservation); err != nil {
			return err
		}
		cloned := reservation.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// MarkLinuxCgroupCreateStarted commits the before-mkdir creation intent.
// Replaying its exact pre-state after an unknown write performs another fsync
// barrier; no later stage can be inferred from the flag alone. The reserved
// daemon mirror identity remains present here; it is not a cgroup producer
// grant and does not make the leaf exist.
func (store *Store) MarkLinuxCgroupCreateStarted(key RunKey, expected LinuxCgroupReservation) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.CreateStarted || expected.LeafIdentity != nil || expected.Helper != nil ||
		expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil ||
		expected.HelperExit != nil {
		return RunJournal{}, errors.New("Linux cgroup creation intent expected value is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		after := expected.Clone()
		after.CreateStarted = true
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// BindLinuxCgroupLeaf records the first measured leaf object identity after a
// successful durable creation intent. It never adopts an already-bound or
// path-reopened leaf with a different object identity. The caller must invoke
// it only while the measured directory is still pinned through uninterrupted
// A-owned FD provenance; matching a path/stat after all pins are lost is not
// evidence that this reservation created that object.
func (store *Store) BindLinuxCgroupLeaf(key RunKey, expected LinuxCgroupReservation, measured authority.LinuxCgroupIdentity) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := measured.Validate(); err != nil {
		return RunJournal{}, err
	}
	if !expected.CreateStarted || measured.LeafName != expected.LeafName || !measured.Parent.Equal(expected.Parent) {
		return RunJournal{}, errors.New("measured Linux cgroup leaf does not match reservation")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if expected.LeafIdentity != nil || expected.Helper != nil || expected.Launcher != nil || expected.Gate != nil {
			return errors.New("Linux cgroup leaf must bind before its producers")
		}
		leaf := measured
		after := expected.Clone()
		after.LeafIdentity = &leaf
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// BindLinuxCgroupHelper records the exact UID-A helper identity after the
// daemon and helper have independently pinned the measured leaf.
func (store *Store) BindLinuxCgroupHelper(key RunKey, expected LinuxCgroupReservation, helper authority.LinuxCgroupProcessIdentity) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := helper.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if expected.LeafIdentity == nil || expected.Helper != nil || expected.Launcher != nil || expected.Gate != nil ||
			expected.AbortProof != nil || expected.PreGateStopReceipt != nil || journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil {
			return errors.New("Linux cgroup helper bind is out of order")
		}
		bound := helper
		after := expected.Clone()
		after.Helper = &bound
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// BindLinuxCgroupLauncher records the exact root-launcher process before that
// producer can receive or act on a run grant.
func (store *Store) BindLinuxCgroupLauncher(key RunKey, expected LinuxCgroupReservation, launcher authority.LinuxCgroupProcessIdentity) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := launcher.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if expected.LeafIdentity == nil || expected.Helper == nil || expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil ||
			journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil {
			return errors.New("Linux cgroup launcher bind is out of order")
		}
		bound := launcher
		after := expected.Clone()
		after.Launcher = &bound
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// ArmLinuxCgroupLauncherGrant durably records that an external grant may now
// reach the exact bound launcher. Callers must replay this exact CAS and
// obtain a successful persistence barrier before sending the grant; after this
// point the no-target abort path is closed even if the launcher later exits.
func (store *Store) ArmLinuxCgroupLauncherGrant(key RunKey, expected LinuxCgroupReservation) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Launcher == nil || expected.LauncherGrantPossible || expected.LauncherExit != nil || expected.Gate != nil ||
		expected.AbortProof != nil {
		return RunJournal{}, errors.New("Linux cgroup launcher grant expected value is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil || journal.ContainmentCgroup == nil {
			return errors.New("Linux cgroup launcher grant has no pending reservation")
		}
		after := expected.Clone()
		after.LauncherGrantPossible = true
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// RecordLinuxCgroupLauncherExit persists the anchor's exact wait/reap witness
// before gate binding or handoff preparation may advance.
func (store *Store) RecordLinuxCgroupLauncherExit(key RunKey, expected LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if expected.Launcher == nil || expected.Helper == nil || expected.LauncherExit != nil ||
			!proof.ValidForOwnedDescendant(*expected.Launcher, *expected.Helper, expected.Anchor, expected.HelperExit) {
			return errors.New("Linux cgroup launcher exit proof does not match producer")
		}
		copy := proof
		after := expected.Clone()
		after.LauncherExit = &copy
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// BindLinuxCgroupGate records the trusted A gate born in the bound leaf after
// the exact root-launcher wait has been durably acknowledged.
func (store *Store) BindLinuxCgroupGate(key RunKey, expected LinuxCgroupReservation, gate authority.LinuxCgroupGateIdentity) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := gate.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if expected.LeafIdentity == nil || expected.Helper == nil ||
			expected.LauncherExit == nil || expected.Gate != nil ||
			!gate.Cgroup.Equal(*expected.LeafIdentity) ||
			journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil {
			return errors.New("Linux cgroup gate bind is out of order")
		}
		copy := gate
		after := expected.Clone()
		after.Gate = &copy
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// RecordLinuxCgroupHelperExit stores the anchor's exact helper wait/reap proof
// for a permitted mirror-takeover receipt. It does not itself prove cgroup stop.
func (store *Store) RecordLinuxCgroupHelperExit(key RunKey, expected LinuxCgroupReservation, proof authority.LinuxCgroupExitProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.StopReceipt != nil ||
			journal.ContainmentAuthority != nil && journal.ContainmentAuthority.StopReceipt != nil {
			return errors.New("Linux cgroup helper exit cannot follow a durable stop receipt")
		}
		if expected.Helper == nil || expected.HelperExit != nil || !proof.ValidFor(*expected.Helper, expected.Anchor) {
			return errors.New("Linux cgroup helper exit proof does not match producer")
		}
		copy := proof
		after := expected.Clone()
		after.HelperExit = &copy
		replayed, err := exactCgroupMutationStage(journal, expected, after)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		cloned := after.Clone()
		journal.ContainmentCgroup = &cloned
		return nil
	})
}

// AbortLinuxCgroupReservation records a replayable no-target abort only after
// an independent caller proves the exact parent unchanged, every producer
// fenced, no launcher grant possible and either never-created or exact empty
// leaf removal. It never creates a stop receipt. The tombstone is not pending
// containment but remains until journal deletion so an unknown write can be
// replayed with the same proof and a fresh persistence barrier.
func (store *Store) AbortLinuxCgroupReservation(key RunKey, expected LinuxCgroupReservation, proof authority.LinuxCgroupReservationAbortProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := proof.Validate(); err != nil {
		return RunJournal{}, err
	}
	if !proof.ValidFor(expected.LaunchToken, expected.Parent, expected.LeafName, expected.CreateStarted,
		expected.OwnerUID, expected.Anchor, *expected.Mirror, expected.Helper, expected.Launcher) ||
		expected.AbortProof != nil || expected.PreGateStopReceipt != nil || expected.Gate != nil {
		return RunJournal{}, errors.New("Linux cgroup reservation abort proof does not match a pre-effect owner")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		return applyLinuxCgroupReservationAbort(journal, expected, proof)
	})
}

// AbortNeverCreatedLinuxCgroupReservation records a no-target tombstone when
// the durable before-mkdir CAS was never advanced. The exclusive open Store
// lock and exact RunKey/fence prove that no compliant creator could have
// proceeded past CreateStarted=false; this is not a filesystem observation,
// physical-stop receipt, or authority to remove a cgroup leaf.
func (store *Store) AbortNeverCreatedLinuxCgroupReservation(key RunKey, fence protocol.Fence, expected LinuxCgroupReservation) (RunJournal, error) {
	if err := expected.Validate(); err != nil || expected.CreateStarted || expected.LeafIdentity != nil ||
		expected.Helper != nil || expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil ||
		expected.HelperExit != nil || expected.AbortProof != nil || expected.PreGateStopReceipt != nil ||
		expected.LauncherGrantPossible || expected.Mirror == nil {
		return RunJournal{}, errors.New("Linux cgroup reservation is not a never-created state")
	}
	proof := authority.LinuxCgroupReservationAbortProof{
		Version: authority.LinuxCgroupAbortProofVersion, LaunchToken: expected.LaunchToken,
		Parent: expected.Parent, LeafName: expected.LeafName, CreateStarted: false,
		LeafDisposition: authority.LinuxCgroupAbortLeafNeverCreated, OwnerUID: expected.OwnerUID,
		Anchor: expected.Anchor, Mirror: *expected.Mirror, Witness: expected.Anchor,
		CreatorFenced: true, NoProducerCanAct: true, ParentUnchanged: true, LeafAbsent: true,
		LeafNeverAppeared: false,
	}
	if err := proof.Validate(); err != nil || !linuxCgroupReservationAbortMatches(expected, proof) {
		return RunJournal{}, errors.New("Linux cgroup never-created tombstone is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.Fence() != fence || !journal.hasClaimGrant() {
			return errors.New("Linux cgroup never-created abort has a stale RunKey fence")
		}
		return applyLinuxCgroupReservationAbort(journal, expected, proof)
	})
}

func applyLinuxCgroupReservationAbort(journal *RunJournal, expected LinuxCgroupReservation, proof authority.LinuxCgroupReservationAbortProof) error {
	if journal.ContainmentCgroup == nil {
		return errors.New("Linux cgroup reservation abort compare-and-set mismatch")
	}
	current := journal.ContainmentCgroup
	if current.AbortProof != nil {
		previous := current.Clone()
		previous.AbortProof = nil
		if previous.Equal(expected) && current.AbortProof.Equal(proof) &&
			journal.ContainmentAuthority == nil && journal.ContainmentHandoff == nil &&
			journal.ContainmentScans == nil && !journal.HasProcessDetails() && !journal.ContainmentUnproven {
			return nil
		}
		return errors.New("Linux cgroup reservation abort replay conflicts with journal")
	}
	if !current.Equal(expected) || journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil ||
		journal.ContainmentScans != nil || journal.HasProcessDetails() || journal.ContainmentUnproven {
		return errors.New("Linux cgroup reservation abort compare-and-set mismatch")
	}
	if !linuxCgroupReservationAbortMatches(*current, proof) {
		return errors.New("Linux cgroup reservation abort has a live or unidentified producer")
	}
	copy := proof.Clone()
	current.AbortProof = &copy
	return nil
}

// PrepareSupervisorHandoff durably records the immutable launch binding before
// an independently running helper can outlive the daemon. Replaying the same
// launch preserves the record and performs a fresh persistence barrier so an
// unknown prior write is not upgraded by readback alone. A different launch
// cannot replace an unresolved record. Helper identity, scan progress, and stop
// receipt are advanced only by their dedicated CAS mutations.
func (store *Store) PrepareSupervisorHandoff(key RunKey, handoff authority.SupervisorHandoff) (RunJournal, error) {
	if err := handoff.Validate(); err != nil {
		return RunJournal{}, err
	}
	if handoff.Version == authority.SupervisorHandoffVersionV2 {
		return store.prepareLinuxCgroupSupervisorHandoff(key, handoff)
	}
	if handoff.ScanUpdate != nil {
		return store.recordSupervisorScanUpdate(key, handoff)
	}
	if handoff.StopReceipt != nil {
		return RunJournal{}, errors.New("supervisor handoff stop receipt requires dedicated mutation")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if journal.LinuxCloneIntent != nil {
			return errors.New("Linux clone run requires v2 cgroup supervisor handoff")
		}
		if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("supervisor handoff is fenced by Linux clone abort")
		}
		if journal.LocalState == "cleanup_pending" {
			return errors.New("cannot prepare supervisor handoff during cleanup")
		}
		if journal.HasProcessDetails() || journal.ContainmentAuthority != nil {
			return errors.New("supervisor handoff conflicts with committed containment")
		}
		if journal.ContainmentCgroupReleaseProof != nil {
			return errors.New("supervisor handoff cannot be recreated after v2 containment release")
		}
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("supervisor handoff cannot be recreated after v2 pre-gate release")
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

func (store *Store) prepareLinuxCgroupSupervisorHandoff(key RunKey, handoff authority.SupervisorHandoff) (RunJournal, error) {
	if handoff.Version != authority.SupervisorHandoffVersionV2 || handoff.SupervisorPID != 0 ||
		handoff.SupervisorIdentity != "" || handoff.Mirror != nil || handoff.StopReceipt != nil {
		return RunJournal{}, errors.New("Linux cgroup supervisor handoff prepare value is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() || journal.LocalState == "cleanup_pending" || journal.HasProcessDetails() ||
			journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil ||
			journal.ContainmentAuthority != nil || journal.ContainmentScans != nil || journal.ContainmentUnproven ||
			journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil {
			return errors.New("Linux cgroup supervisor handoff conflicts with journal")
		}
		if journal.ContainmentCgroup == nil ||
			!linuxCgroupReservationMatchesHandoff(*journal.ContainmentCgroup, handoff, false) {
			return errors.New("Linux cgroup supervisor handoff does not match a complete leaf reservation")
		}
		if journal.ContainmentHandoff == nil {
			cloned := handoff.Clone()
			journal.ContainmentHandoff = &cloned
			return nil
		}
		current := journal.ContainmentHandoff
		if !current.Equal(handoff) || current.StopReceipt != nil ||
			!linuxCgroupReservationMatchesHandoff(*journal.ContainmentCgroup, *current, current.SupervisorPID > 0) {
			return errors.New("Linux cgroup supervisor handoff conflicts with journal")
		}
		return nil
	})
}

// BindSupervisorHandoffV2 binds the reservation's already durable helper and
// the exact daemon mirror identity in the prepared v2 handoff. It intentionally
// initializes no v1 process-group scan ledger.
func (store *Store) BindSupervisorHandoffV2(key RunKey, expected authority.SupervisorHandoff, helper authority.LinuxCgroupProcessIdentity, mirror authority.LinuxCgroupProcessIdentity) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Version != authority.SupervisorHandoffVersionV2 || expected.SupervisorPID != 0 ||
		expected.SupervisorIdentity != "" || expected.Mirror != nil || expected.StopReceipt != nil {
		return RunJournal{}, errors.New("Linux cgroup supervisor bind expected value is invalid")
	}
	if err := helper.Validate(); err != nil {
		return RunJournal{}, err
	}
	if err := mirror.Validate(); err != nil {
		return RunJournal{}, err
	}
	after := expected.Clone()
	after.SupervisorPID = helper.PID
	after.SupervisorIdentity = helper.Identity
	after.Mirror = &mirror
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() || journal.LocalState == "cleanup_pending" || journal.ContainmentScans != nil ||
			journal.ContainmentAuthority != nil || journal.HasProcessDetails() || journal.ContainmentUnproven ||
			journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Helper == nil ||
			!journal.ContainmentCgroup.Helper.Equal(helper) ||
			journal.ContainmentCgroup.Gate == nil || journal.ContainmentCgroup.Mirror == nil ||
			!journal.ContainmentCgroup.Mirror.Equal(mirror) {
			return errors.New("Linux cgroup supervisor bind has no matching producer reservation")
		}
		current := journal.ContainmentHandoff
		if current == nil || !current.SameLaunch(expected) || current.StopReceipt != nil {
			return errors.New("Linux cgroup supervisor handoff compare-and-set mismatch")
		}
		if current.Equal(after) {
			if journal.ContainmentCgroup.Mirror == nil || !journal.ContainmentCgroup.Mirror.Equal(mirror) {
				return errors.New("Linux cgroup mirror identity conflicts with reservation")
			}
			return nil
		}
		if !current.Equal(expected) {
			return errors.New("Linux cgroup supervisor handoff compare-and-set mismatch")
		}
		cloned := after.Clone()
		journal.ContainmentHandoff = &cloned
		return nil
	})
}

func (store *Store) recordSupervisorScanUpdate(key RunKey, request authority.SupervisorHandoff) (RunJournal, error) {
	update := request.ScanUpdate
	if update == nil {
		return RunJournal{}, errors.New("supervisor scan update is missing")
	}
	expected := request.Clone()
	expected.ScanUpdate = nil
	expected.SupervisorPID = update.SupervisorPID
	expected.SupervisorIdentity = update.SupervisorIdentity
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LocalState == "cleanup_pending" {
			return errors.New("Linux containment scan update arrived during cleanup")
		}
		if journal.ContainmentUnproven && update.Phase != authority.SupervisorScanPhaseUnproven {
			return errors.New("Linux containment observation is already unresolved")
		}
		if journal.ContainmentHandoff != nil {
			current := journal.ContainmentHandoff
			if current.StopReceipt != nil || !current.Equal(expected) {
				return errors.New("supervisor scan update handoff identity mismatch")
			}
		} else {
			value, err := expected.ToSupervisor()
			if err != nil {
				return err
			}
			current := journal.ContainmentAuthority
			if current == nil || current.StopReceipt != nil || journal.PID != value.TargetPID || journal.ProcessIdentity != value.TargetIdentity {
				return errors.New("supervisor scan update has no active containment authority")
			}
			currentBinding := current.Clone()
			currentBinding.StopReceipt = nil
			if !currentBinding.Equal(value) {
				return errors.New("supervisor scan update authority identity mismatch")
			}
		}
		scans := journal.ContainmentScans
		if scans == nil || !scans.matchesHandoff(expected) || scans.MirrorIdentity != update.MirrorIdentity {
			return errors.New("supervisor scan update fence is missing or mismatched")
		}
		progress := &scans.Helper
		if update.Owner == authority.SupervisorScanOwnerMirror {
			progress = &scans.Mirror
		}
		switch update.Phase {
		case authority.SupervisorScanPhaseBegin:
			if progress.SealedSequence != 0 || progress.UnprovenSequence != 0 || progress.PendingSequence != 0 && progress.PendingSequence != update.Sequence {
				return errors.New("supervisor scan begin conflicts with unresolved sequence")
			}
			if progress.PendingSequence == update.Sequence {
				if progress.CleanResultSequence != 0 {
					return errors.New("supervisor scan is awaiting fence retirement")
				}
				return nil
			}
			if progress.CompletedSequence == ^uint64(0) || update.Sequence != progress.CompletedSequence+1 {
				return errors.New("supervisor scan begin sequence is stale or out of order")
			}
			progress.PendingSequence = update.Sequence
		case authority.SupervisorScanPhaseClean:
			if progress.SealedSequence != 0 || progress.UnprovenSequence != 0 || journal.ContainmentUnproven {
				return errors.New("supervisor scan cannot clear unresolved observation")
			}
			if progress.PendingSequence == update.Sequence {
				if progress.CleanResultSequence != 0 && progress.CleanResultSequence != update.Sequence {
					return errors.New("supervisor clean scan result conflicts with pending sequence")
				}
				progress.CleanResultSequence = update.Sequence
				return nil
			}
			if progress.PendingSequence == 0 && progress.CleanResultSequence == 0 && progress.CompletedSequence == update.Sequence {
				return nil
			}
			return errors.New("supervisor clean scan sequence is not pending")
		case authority.SupervisorScanPhaseRetire:
			if progress.SealedSequence != 0 || progress.UnprovenSequence != 0 || journal.ContainmentUnproven {
				return errors.New("supervisor scan fence cannot retire unresolved observation")
			}
			if progress.PendingSequence == update.Sequence && progress.CleanResultSequence == update.Sequence {
				progress.PendingSequence = 0
				progress.CleanResultSequence = 0
				progress.CompletedSequence = update.Sequence
				return nil
			}
			if progress.PendingSequence == 0 && progress.CleanResultSequence == 0 && progress.CompletedSequence == update.Sequence {
				return nil
			}
			return errors.New("supervisor scan result is not durably ready for retirement")
		case authority.SupervisorScanPhaseSeal:
			if progress.SealedSequence == update.Sequence && progress.sealed() {
				return nil
			}
			if progress.SealedSequence != 0 || progress.PendingSequence != 0 || progress.CleanResultSequence != 0 ||
				progress.UnprovenSequence != 0 || journal.ContainmentUnproven || progress.CompletedSequence == 0 ||
				progress.CompletedSequence != update.Sequence || progress.FailureKind != "" {
				return errors.New("supervisor scan seal does not match a final clean sequence")
			}
			progress.SealedSequence = update.Sequence
		case authority.SupervisorScanPhaseUnproven:
			if progress.UnprovenSequence == update.Sequence && progress.FailureKind == update.FailureKind {
				return nil
			}
			preScanBeginFailure := update.FailureKind == "pre_scan_ack_failure" && progress.PendingSequence == 0 &&
				progress.CleanResultSequence == 0 && progress.SealedSequence == 0 && progress.CompletedSequence != ^uint64(0) &&
				update.Sequence == progress.CompletedSequence+1
			cleanRetireFailure := progress.PendingSequence == 0 && progress.CompletedSequence == update.Sequence &&
				update.FailureKind == "clean_retire_ack_failure"
			if progress.SealedSequence != 0 || progress.UnprovenSequence != 0 ||
				(progress.PendingSequence != update.Sequence && !cleanRetireFailure && !preScanBeginFailure) {
				return errors.New("supervisor unproven scan sequence is not pending")
			}
			progress.PendingSequence = 0
			progress.CleanResultSequence = 0
			progress.UnprovenSequence = update.Sequence
			progress.FailureKind = update.FailureKind
			journal.ContainmentUnproven = true
		case authority.SupervisorScanPhaseAssertClean:
			if update.Sequence != progress.CompletedSequence || journal.ContainmentUnproven || !scans.clean() {
				return errors.New("Linux containment scan evidence is unresolved")
			}
		case authority.SupervisorScanPhaseAssertSealed:
			if update.Sequence != progress.SealedSequence || journal.ContainmentUnproven || !progress.sealed() {
				return errors.New("Linux containment scan owner seal is unresolved")
			}
		case authority.SupervisorScanPhaseAssertAllSealed:
			if update.Sequence != progress.SealedSequence || journal.ContainmentUnproven || !scans.sealed() {
				return errors.New("Linux containment scan seals are unresolved")
			}
		default:
			return errors.New("supervisor scan update phase is invalid")
		}
		return nil
	})
}

// BindSupervisorHandoff adds the exact helper PID/creation identity and the
// first helper/mirror scan fences to a prepared handoff. The expected handoff
// is an exact CAS value, so a stale writer cannot retarget a retained helper.
// Exact replays perform a fresh persistence barrier after readback of an
// unknown prior write.
func (store *Store) BindSupervisorHandoff(key RunKey, expected authority.SupervisorHandoff, pid int, identity string) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Version == authority.SupervisorHandoffVersionV2 {
		return RunJournal{}, errors.New("v2 handoff requires BindSupervisorHandoffV2")
	}
	scanUpdate := expected.ScanUpdate
	baseExpected := expected.Clone()
	baseExpected.ScanUpdate = nil
	if expected.OwnerKind == authority.OwnerKindLinuxHelper {
		if scanUpdate == nil || scanUpdate.Phase != authority.SupervisorScanPhaseInitialize ||
			scanUpdate.Owner != authority.SupervisorScanOwnerMirror || scanUpdate.Sequence != 1 || scanUpdate.MirrorIdentity == "" {
			return RunJournal{}, errors.New("Linux supervisor bind requires an exact mirror scan identity")
		}
	} else if scanUpdate != nil {
		return RunJournal{}, errors.New("non-Linux supervisor bind cannot carry scan state")
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
		if !current.Equal(baseExpected) {
			// A write error after rename leaves the helper binding durable while
			// the caller still holds the pre-bind expected value. Replaying the
			// same binding is safe and must not be mistaken for retargeting.
			if baseExpected.SupervisorPID == 0 && baseExpected.SupervisorIdentity == "" && baseExpected.StopReceipt == nil &&
				current.SameLaunch(baseExpected) && current.SupervisorPID == pid && current.SupervisorIdentity == identity && current.StopReceipt == nil {
				if current.OwnerKind == authority.OwnerKindLinuxHelper &&
					(journal.ContainmentScans == nil || !journal.ContainmentScans.matchesHandoff(*current) || journal.ContainmentScans.MirrorIdentity != scanUpdate.MirrorIdentity) {
					return errors.New("Linux supervisor bind scan identity conflicts with journal")
				}
				return nil
			}
			return errors.New("supervisor handoff compare-and-set mismatch")
		}
		if current.SupervisorPID != 0 || current.SupervisorIdentity != "" {
			if current.SupervisorPID == pid && current.SupervisorIdentity == identity {
				if current.OwnerKind == authority.OwnerKindLinuxHelper &&
					(journal.ContainmentScans == nil || !journal.ContainmentScans.matchesHandoff(*current) || journal.ContainmentScans.MirrorIdentity != scanUpdate.MirrorIdentity) {
					return errors.New("Linux supervisor handoff scan fence is missing or mismatched")
				}
				return nil
			}
			return errors.New("supervisor handoff helper identity conflicts with journal")
		}
		if current.OwnerKind == authority.OwnerKindLinuxHelper {
			if journal.ContainmentUnproven || journal.ContainmentScans != nil {
				return errors.New("Linux supervisor handoff cannot bind over unresolved scan state")
			}
			scans := ContainmentScanJournal{
				Version: 1, LaunchToken: current.LaunchToken, TargetPID: current.TargetPID,
				TargetIdentity: current.TargetIdentity, SupervisorPID: pid, SupervisorIdentity: identity,
				MirrorIdentity: scanUpdate.MirrorIdentity,
				Helper:         ContainmentScanProgress{PendingSequence: 1},
				Mirror:         ContainmentScanProgress{PendingSequence: 1},
			}
			journal.ContainmentScans = &scans
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
		if value.Version == authority.SupervisorVersionV2 {
			if journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Gate == nil ||
				!journal.ContainmentCgroup.Gate.Process.Equal(authority.LinuxCgroupProcessIdentity{PID: value.TargetPID, Identity: value.TargetIdentity}) ||
				!startedAt.Equal(journal.ContainmentCgroup.Gate.ObservedAt) {
				return errors.New("v2 supervisor handoff commit time must match durable gate observation")
			}
		}
		if journal.ContainmentHandoff == nil {
			if journal.PID == value.TargetPID && journal.ProcessIdentity == value.TargetIdentity &&
				!journal.StartedAt.IsZero() && journal.ContainmentAuthority != nil {
				if value.Version == authority.SupervisorVersionV2 && !journal.StartedAt.Equal(startedAt) {
					return errors.New("v2 supervisor handoff commit timestamp changed during replay")
				}
				if value.Version == authority.SupervisorVersionV2 && journal.ContainmentAuthority.StopReceipt != nil {
					return errors.New("v2 handoff commit replay must precede receipt advancement")
				}
				existing := journal.ContainmentAuthority.Clone()
				existing.StopReceipt = nil
				candidate := value.Clone()
				candidate.StopReceipt = nil
				if existing.Equal(candidate) {
					if value.Version == authority.SupervisorVersionV2 {
						if journal.ContainmentUnproven || journal.ContainmentCgroup == nil ||
							!linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *journal.ContainmentAuthority) {
							return errors.New("v2 committed authority has no matching cgroup reservation")
						}
					} else {
						if err := requireLinuxScansCleanForAuthority(*journal, *journal.ContainmentAuthority); err != nil {
							return err
						}
						if journal.ContainmentUnproven {
							return errors.New("Linux supervisor initial scan observation is unresolved")
						}
					}
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
		if value.OwnerKind == authority.OwnerKindLinuxHelper {
			if value.Version == authority.SupervisorVersionV2 {
				if journal.ContainmentUnproven || journal.ContainmentScans != nil || journal.ContainmentCgroup == nil ||
					journal.ContainmentCgroup.HelperExit != nil ||
					!linuxCgroupReservationMatchesHandoff(*journal.ContainmentCgroup, expected, true) {
					return errors.New("Linux cgroup initial producer fences are unresolved")
				}
			} else if journal.ContainmentUnproven || journal.ContainmentScans == nil ||
				!journal.ContainmentScans.matchesHandoff(expected) || !journal.ContainmentScans.clean() {
				return errors.New("Linux supervisor initial scan fences are unresolved")
			}
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
	if expected.Version == authority.SupervisorHandoffVersionV2 {
		return store.recordLinuxCgroupSupervisorStopReceipt(key, expected, receipt)
	}
	if expected.SupervisorPID <= 0 || expected.SupervisorIdentity == "" {
		return RunJournal{}, errors.New("supervisor handoff stop receipt requires a bound helper")
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
				if journal.ContainmentUnproven {
					return errors.New("supervisor handoff scan observation is unresolved")
				}
				if err := requireLinuxScansSealedForHandoff(*journal, *current); err != nil {
					return err
				}
				return nil
			}
			if journal.ContainmentUnproven {
				return errors.New("supervisor handoff scan observation is unresolved")
			}
			if err := requireLinuxScansSealedForHandoff(*journal, *current); err != nil {
				return err
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
		if journal.ContainmentUnproven {
			return errors.New("supervisor handoff scan observation is unresolved")
		}
		if err := requireLinuxScansSealedForAuthority(*journal, *journal.ContainmentAuthority); err != nil {
			return err
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

func (store *Store) recordLinuxCgroupSupervisorStopReceipt(key RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt) (RunJournal, error) {
	if expected.SupervisorPID <= 0 || expected.SupervisorIdentity == "" || expected.Mirror == nil ||
		!receipt.ValidForHandoff(expected) {
		return RunJournal{}, errors.New("v2 supervisor stop receipt does not match handoff")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.ContainmentCgroup == nil || journal.ContainmentScans != nil ||
			!linuxCgroupReceiptMatchesReservation(*journal.ContainmentCgroup, receipt) {
			return errors.New("v2 stop receipt has no matching cgroup producer reservation")
		}
		if journal.ContainmentHandoff != nil {
			current := journal.ContainmentHandoff
			if !sameBoundV2Handoff(current, &expected) {
				return errors.New("v2 supervisor handoff compare-and-set mismatch")
			}
			if current.StopReceipt != nil {
				if current.StopReceipt.Equal(receipt) {
					return nil
				}
				return errors.New("v2 supervisor stop receipt cannot replace an existing witness")
			}
			if !current.Equal(expected) {
				return errors.New("v2 supervisor handoff compare-and-set mismatch")
			}
			copy := receipt
			copy.CgroupIdentity = cloneStateCgroupIdentity(receipt.CgroupIdentity)
			copy.LinuxProof = cloneStateStopProof(receipt.LinuxProof)
			current.StopReceipt = &copy
			journal.ContainmentUnproven = false
			return nil
		}
		value, err := expected.ToSupervisor()
		if err != nil {
			return err
		}
		current := journal.ContainmentAuthority
		if current == nil || current.Version != authority.SupervisorVersionV2 ||
			journal.PID != value.TargetPID || journal.ProcessIdentity != value.TargetIdentity {
			return errors.New("v2 supervisor handoff is not pending or committed")
		}
		candidate := value.Clone()
		candidate.StopReceipt = nil
		currentBinding := current.Clone()
		currentBinding.StopReceipt = nil
		if !currentBinding.Equal(candidate) || !linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *current) {
			return errors.New("v2 committed authority conflicts with handoff")
		}
		if current.StopReceipt != nil {
			if current.StopReceipt.Equal(receipt) {
				return nil
			}
			return errors.New("v2 supervisor stop receipt cannot replace an existing witness")
		}
		copy := receipt
		copy.CgroupIdentity = cloneStateCgroupIdentity(receipt.CgroupIdentity)
		copy.LinuxProof = cloneStateStopProof(receipt.LinuxProof)
		current.StopReceipt = &copy
		journal.ContainmentUnproven = false
		return nil
	})
}

// ClearSupervisorHandoff preserves the v1 release path. V2 Linux handoffs are
// rejected here and must use the Linux-only opaque platform release API.
func (store *Store) ClearSupervisorHandoff(key RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.Version == authority.SupervisorHandoffVersionV2 {
		return RunJournal{}, errors.New("v2 supervisor handoff release requires a platform cgroup release")
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
		if journal.ContainmentUnproven {
			return errors.New("supervisor handoff has unresolved containment observation")
		}
		if err := requireLinuxScansSealedForHandoff(*journal, expected); err != nil {
			return err
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
		journal.ContainmentScans = nil
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
	if expected.Version == authority.SupervisorHandoffVersionV2 {
		return RunJournal{}, errors.New("v2 cgroup handoff cannot use the v1 pre-authority abort path")
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
		if journal.ContainmentUnproven {
			return false, errors.New("supervisor handoff has unresolved containment observation")
		}
		if expected.OwnerKind == authority.OwnerKindLinuxHelper {
			if expected.SupervisorPID == 0 {
				if journal.ContainmentScans != nil {
					return false, errors.New("unbound Linux handoff unexpectedly has scan state")
				}
			} else if journal.ContainmentScans == nil || !journal.ContainmentScans.matchesHandoff(expected) ||
				!initialPrecommitLinuxScansAbortable(*journal.ContainmentScans) {
				return false, errors.New("Linux supervisor pre-commit scan state is unresolved")
			}
		}
		journal.ContainmentHandoff = nil
		journal.ContainmentScans = nil
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
		if journal.ContainmentAuthority.Version == authority.SupervisorVersionV2 {
			current := journal.ContainmentAuthority
			if journal.ContainmentCgroup == nil || journal.ContainmentScans != nil ||
				!linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *current) ||
				!linuxCgroupReceiptMatchesReservation(*journal.ContainmentCgroup, receipt) ||
				!receipt.ValidFor(*current) {
				return errors.New("v2 containment stop receipt does not match exact cgroup authority")
			}
			if current.StopReceipt != nil {
				if current.StopReceipt.Equal(receipt) {
					return nil
				}
				return errors.New("v2 containment stop receipt cannot replace an existing witness")
			}
			copy := receipt
			copy.CgroupIdentity = cloneStateCgroupIdentity(receipt.CgroupIdentity)
			copy.LinuxProof = cloneStateStopProof(receipt.LinuxProof)
			current.StopReceipt = &copy
			journal.ContainmentUnproven = false
			return nil
		}
		if journal.ContainmentCgroup != nil {
			return errors.New("v1 containment receipt cannot act on a v2 cgroup reservation")
		}
		if journal.ContainmentUnproven {
			return errors.New("containment scan observation is unresolved")
		}
		if err := requireLinuxScansSealedForAuthority(*journal, *journal.ContainmentAuthority); err != nil {
			return err
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

// RecordSupervisorRecoveryScanSeal persists a helper's authenticated
// owner-loss terminal scan after a daemon restart. The caller may use this only
// after the exact helper recovery endpoint has attested a clean, joined
// terminal scan; the mirror must already have its own durable seal. A pending
// or unproven journal can never be advanced by this recovery-only mutation.
// Replaying the exact proof performs a fresh journal write barrier.
func (store *Store) RecordSupervisorRecoveryScanSeal(key RunKey, expected authority.Supervisor, sequence uint64) (RunJournal, error) {
	if err := expected.Validate(); err != nil {
		return RunJournal{}, err
	}
	if expected.OwnerKind != authority.OwnerKindLinuxHelper || expected.StopReceipt != nil || sequence == 0 {
		return RunJournal{}, errors.New("Linux helper recovery scan seal identity is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		current := journal.ContainmentAuthority
		if current == nil || journal.PID != expected.TargetPID || journal.ProcessIdentity != expected.TargetIdentity || current.StopReceipt != nil {
			return errors.New("Linux helper recovery seal has no active matching authority")
		}
		currentBinding := current.Clone()
		currentBinding.StopReceipt = nil
		candidate := expected.Clone()
		candidate.StopReceipt = nil
		if !currentBinding.Equal(candidate) {
			return errors.New("Linux helper recovery seal authority identity mismatch")
		}
		scans := journal.ContainmentScans
		if scans == nil || !scans.matchesAuthority(expected) {
			return errors.New("Linux helper recovery seal scan fence is missing or mismatched")
		}
		if journal.ContainmentUnproven || !scans.Mirror.sealed() {
			return errors.New("Linux helper recovery seal requires a durable clean mirror seal")
		}
		progress := &scans.Helper
		if progress.SealedSequence == sequence && progress.sealed() {
			return nil
		}
		if progress.CompletedSequence == 0 || progress.CompletedSequence == ^uint64(0) || sequence != progress.CompletedSequence+1 ||
			progress.PendingSequence != 0 || progress.CleanResultSequence != 0 || progress.SealedSequence != 0 ||
			progress.UnprovenSequence != 0 || progress.FailureKind != "" {
			return errors.New("Linux helper recovery scan proof is not the exact next clean sequence")
		}
		progress.CompletedSequence = sequence
		progress.SealedSequence = sequence
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
		if journal.ContainmentCgroup != nil || journal.ContainmentAuthority != nil && journal.ContainmentAuthority.Version == authority.SupervisorVersionV2 {
			return errors.New("v2 process details require ClearProcessDetailsV2")
		}
		if journal.ContainmentHandoff != nil {
			return errors.New("process details require supervisor handoff mutation")
		}
		if !journal.HasProcessDetails() {
			return nil
		}
		if journal.PID != pid || journal.ProcessIdentity != identity {
			return errors.New("persisted process details changed during termination")
		}
		if journal.ContainmentUnproven {
			return errors.New("containment stop proof is unresolved")
		}
		if journal.ContainmentAuthority != nil {
			if err := requireLinuxScansSealedForAuthority(*journal, *journal.ContainmentAuthority); err != nil {
				return err
			}
			if journal.ContainmentAuthority.StopReceipt == nil || !journal.ContainmentAuthority.StopReceipt.ValidFor(*journal.ContainmentAuthority) {
				return errors.New("containment stop receipt is required before clearing process details")
			}
		}
		journal.PID = 0
		journal.ProcessIdentity = ""
		journal.StartedAt = time.Time{}
		journal.ContainmentAuthority = nil
		journal.ContainmentScans = nil
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

func requireLinuxClonePlatform() error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return errors.New("Linux clone-v2 mutations are supported only on Linux/amd64")
	}
	return nil
}

func requireLinuxCloneJournalFence(journal RunJournal, fence protocol.Fence, intent authority.LinuxCloneIntent) error {
	if intent.Validate() != nil || intent.RunID != journal.RunID || intent.Generation != journal.Generation ||
		journal.Fence() != fence || !journal.hasClaimGrant() {
		return errors.New("Linux clone RunKey or fence is stale")
	}
	if intent.BindingKey != journal.WorkspaceBindingKey {
		return errors.New("Linux clone binding key does not match the journal")
	}
	admission, present, err := protocol.ParseAdmissionInput(journal.Work.Input)
	if err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID ||
		admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest {
		return errors.New("Linux clone intent does not match the current admitted Subject")
	}
	return nil
}

func linuxCloneStageCanAdvance(journal RunJournal) bool {
	return journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" && journal.LocalState != "stale" &&
		!journal.LinuxCloneAbortPending && journal.LinuxCloneAbortProof == nil
}

func linuxCloneCheckoutMatches(intent authority.LinuxCloneIntent, seed authority.LinuxCloneSeedReceipt, receipt authority.LinuxCloneCheckoutReceipt) bool {
	return receipt.Validate() == nil && receipt.RunID == intent.RunID && receipt.Generation == intent.Generation &&
		receipt.BindingKey == intent.BindingKey && receipt.ResourceID == intent.ResourceID &&
		receipt.Target == intent.Target && receipt.Nonce == intent.Nonce &&
		receipt.SeedDigest == seed.Digest && receipt.BaseCommit == intent.BaseCommit && seed.BaseCommit == intent.BaseCommit
}

func linuxCloneCommittedGateReady(journal RunJournal) bool {
	if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" || journal.LocalState == "stale" ||
		!journal.HasProcessDetails() || journal.ContainmentUnproven || journal.ContainmentHandoff != nil ||
		journal.ContainmentCgroup == nil || journal.ContainmentCgroup.Gate == nil || journal.ContainmentAuthority == nil {
		return false
	}
	owner := journal.ContainmentAuthority
	gate := journal.ContainmentCgroup.Gate
	return owner.Version == authority.SupervisorVersionV2 && owner.Validate() == nil && owner.StopReceipt == nil &&
		linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *owner) &&
		owner.TargetPID == journal.PID && owner.TargetIdentity == journal.ProcessIdentity &&
		gate.Process.PID == journal.PID && gate.Process.Identity == journal.ProcessIdentity
}

func linuxCloneStopProven(journal RunJournal) bool {
	if owner := journal.ContainmentAuthority; owner != nil && owner.Version == authority.SupervisorVersionV2 &&
		owner.StopReceipt != nil && owner.StopReceipt.ValidFor(*owner) {
		return true
	}
	release := journal.ContainmentCgroupReleaseProof
	return release != nil && release.Version == authority.SupervisorHandoffVersionV2 && release.Validate() == nil
}

func validateLinuxCloneJournal(journal RunJournal) error {
	intent := journal.LinuxCloneIntent
	if intent == nil {
		if journal.LinuxCloneSeedReceipt != nil || journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionReceipt != nil ||
			journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
			return errors.New("run journal has Linux clone evidence without an intent")
		}
		return nil
	}
	if intent.Validate() != nil || intent.RunID != journal.RunID || intent.Generation != journal.Generation ||
		intent.BindingKey != journal.WorkspaceBindingKey || !journal.hasClaimGrant() {
		return errors.New("run journal Linux clone intent is invalid")
	}
	if err := requireLinuxCloneJournalFenceSubject(journal, *intent); err != nil {
		return errors.New("run journal Linux clone intent does not match admitted Subject")
	}
	seed := journal.LinuxCloneSeedReceipt
	if seed != nil && (seed.Validate() != nil || seed.BaseCommit != intent.BaseCommit) {
		return errors.New("run journal Linux clone seed receipt is invalid")
	}
	if journal.LinuxCloneAbortPending && journal.LinuxCloneAbortProof != nil {
		return errors.New("run journal Linux clone abort is both pending and complete")
	}
	if journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil {
		if journal.LinuxCloneCheckoutReceipt != nil || journal.LinuxClonePromotionReceipt != nil {
			return errors.New("run journal Linux clone abort state is invalid")
		}
		if journal.LinuxCloneAbortPending {
			if !linuxCloneAbortFenceAvailable(journal) {
				return errors.New("run journal Linux clone abort lost its producer fence")
			}
			if journal.WorkspacePath != intent.Target || !journal.WorkspaceRecoveryRequired {
				return errors.New("run journal pending clone abort lost its reserved target")
			}
			return nil
		}
		proof := journal.LinuxCloneAbortProof
		if proof.Validate() != nil || !proof.Intent.Equal(*intent) || journal.WorkspacePath != "" || journal.WorkspaceRecoveryRequired ||
			!linuxCloneAbortHistoryMatches(journal, proof) {
			return errors.New("run journal Linux clone abort proof is invalid")
		}
		if seed != nil && (proof.SeedReceipt == nil || !seed.Equal(*proof.SeedReceipt)) {
			return errors.New("run journal Linux clone abort proof lost its bound seed receipt")
		}
		return nil
	}
	if journal.WorkspacePath != intent.Target {
		return errors.New("run journal Linux clone target path changed")
	}
	checkout := journal.LinuxCloneCheckoutReceipt
	if checkout == nil {
		if journal.LinuxClonePromotionReceipt != nil {
			return errors.New("run journal Linux clone promotion has no checkout receipt")
		}
		return nil
	}
	if !linuxCloneCheckoutMatches(*intent, *seed, *checkout) {
		return errors.New("run journal Linux clone checkout receipt is invalid")
	}
	promotion := journal.LinuxClonePromotionReceipt
	if promotion == nil {
		return nil
	}
	ref, err := authority.LinuxClonePromotionRef(journal.RunID, journal.Generation)
	if promotion.Validate() != nil || err != nil || promotion.Ref != ref || !linuxCloneStopProven(journal) {
		return errors.New("run journal Linux clone promotion receipt is invalid")
	}
	return nil
}

func linuxCloneAbortHistoryMatches(journal RunJournal, proof *authority.LinuxCloneAbortProof) bool {
	if !linuxCloneAbortNoActiveOwner(journal) {
		return false
	}
	if proof == nil {
		return journal.ContainmentCgroup == nil && journal.ContainmentCgroupReleaseProof == nil &&
			journal.ContainmentCgroupPreGateReleaseProof == nil
	}
	if proof.CgroupAbortProof != nil {
		return proof.PreGateReleaseProof == nil && proof.NormalReleaseProof == nil && journal.ContainmentCgroup != nil &&
			journal.ContainmentCgroup.AbortProof != nil && journal.ContainmentCgroup.AbortProof.Equal(*proof.CgroupAbortProof) &&
			linuxCgroupReservationAbortMatches(*journal.ContainmentCgroup, *proof.CgroupAbortProof) &&
			journal.ContainmentCgroupReleaseProof == nil && journal.ContainmentCgroupPreGateReleaseProof == nil
	}
	if proof.PreGateReleaseProof != nil {
		return proof.NormalReleaseProof == nil && journal.ContainmentCgroup == nil && journal.ContainmentCgroupReleaseProof == nil &&
			journal.ContainmentCgroupPreGateReleaseProof != nil &&
			journal.ContainmentCgroupPreGateReleaseProof.Equal(*proof.PreGateReleaseProof)
	}
	if proof.NormalReleaseProof != nil {
		return journal.ContainmentCgroup == nil && journal.ContainmentCgroupPreGateReleaseProof == nil &&
			journal.ContainmentCgroupReleaseProof != nil && journal.ContainmentCgroupReleaseProof.Equal(*proof.NormalReleaseProof) &&
			proof.NormalReleaseProof.Validate() == nil && proof.NormalReleaseProof.Version == authority.SupervisorHandoffVersionV2 &&
			proof.NormalReleaseProof.StopReceipt != nil
	}
	return journal.ContainmentCgroup == nil && journal.ContainmentCgroupReleaseProof == nil &&
		journal.ContainmentCgroupPreGateReleaseProof == nil
}

func linuxCloneAbortFenceAvailable(journal RunJournal) bool {
	if !linuxCloneAbortNoActiveOwner(journal) {
		return false
	}
	if release := journal.ContainmentCgroupReleaseProof; release != nil {
		return journal.ContainmentCgroup == nil && journal.ContainmentCgroupPreGateReleaseProof == nil &&
			release.Validate() == nil && release.Version == authority.SupervisorHandoffVersionV2 && release.StopReceipt != nil
	}
	if reservation := journal.ContainmentCgroup; reservation != nil {
		proof := reservation.AbortProof
		return proof != nil && reservation.PreGateStopReceipt == nil && reservation.Gate == nil &&
			linuxCgroupReservationAbortMatches(*reservation, *proof) && journal.ContainmentCgroupPreGateReleaseProof == nil
	}
	if proof := journal.ContainmentCgroupPreGateReleaseProof; proof != nil {
		return proof.Version == authority.SupervisorHandoffVersionV2 && proof.Validate() == nil
	}
	return true
}

func linuxCloneAbortNoActiveOwner(journal RunJournal) bool {
	return !journal.HasProcessDetails() && journal.ContainmentAuthority == nil && journal.ContainmentHandoff == nil &&
		journal.ContainmentScans == nil && !journal.ContainmentUnproven && !journal.HasPendingContainment()
}

func requireLinuxCloneJournalFenceSubject(journal RunJournal, intent authority.LinuxCloneIntent) error {
	admission, present, err := protocol.ParseAdmissionInput(journal.Work.Input)
	if err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID ||
		admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest {
		return errors.New("admitted Subject does not match Linux clone intent")
	}
	return nil
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

func sameContainmentCgroup(left, right *LinuxCgroupReservation) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneIntent(left, right *authority.LinuxCloneIntent) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneSeedReceipt(left, right *authority.LinuxCloneSeedReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneCheckoutReceipt(left, right *authority.LinuxCloneCheckoutReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxClonePromotionReceipt(left, right *authority.LinuxClonePromotionReceipt) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameLinuxCloneAbortProof(left, right *authority.LinuxCloneAbortProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameContainmentCgroupReleaseProof(left, right *authority.SupervisorHandoffReleaseProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameContainmentPreGateReleaseProof(left, right *authority.LinuxCgroupPreGateReleaseProof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func cloneStateCgroupIdentity(value *authority.LinuxCgroupIdentity) *authority.LinuxCgroupIdentity {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStateStopProof(value *authority.LinuxCgroupStopProof) *authority.LinuxCgroupStopProof {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.LauncherExit.HelperExit = cloneJournalExitProof(value.LauncherExit.HelperExit)
	cloned.GateExit.HelperExit = cloneJournalExitProof(value.GateExit.HelperExit)
	cloned.HelperExit = cloneJournalExitProof(value.HelperExit)
	return &cloned
}

func cloneJournalExitProof(value *authority.LinuxCgroupExitProof) *authority.LinuxCgroupExitProof {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.HelperExit = cloneJournalExitProof(value.HelperExit)
	return &cloned
}

func sameBoundV2Handoff(left, right *authority.SupervisorHandoff) bool {
	return left != nil && right != nil && left.Version == authority.SupervisorHandoffVersionV2 &&
		right.Version == authority.SupervisorHandoffVersionV2 && left.SameLaunch(*right) &&
		left.SupervisorPID == right.SupervisorPID && left.SupervisorIdentity == right.SupervisorIdentity &&
		equalAuthorityProcess(left.Mirror, right.Mirror)
}

func linuxCgroupReservationMatchesHandoff(reservation LinuxCgroupReservation, handoff authority.SupervisorHandoff, requireBound bool) bool {
	if reservation.Validate() != nil || reservation.PreGateStopReceipt != nil || handoff.Version != authority.SupervisorHandoffVersionV2 ||
		reservation.LeafIdentity == nil || reservation.Helper == nil || reservation.LauncherExit == nil || reservation.Gate == nil ||
		!reservation.LauncherGrantPossible ||
		handoff.LaunchToken != reservation.LaunchToken || handoff.Secret != reservation.Secret ||
		handoff.PipeToken != reservation.PipeToken || handoff.JobID != reservation.JobID ||
		handoff.OwnerKind != authority.OwnerKindLinuxHelper || handoff.OwnerContext != reservation.RecoveryEndpoint ||
		handoff.OwnerUID != reservation.OwnerUID || handoff.LauncherUID != reservation.LauncherUID ||
		handoff.TargetBirthKind != reservation.Gate.BirthKind || handoff.TargetPID != reservation.Gate.Process.PID ||
		handoff.TargetIdentity != reservation.Gate.Process.Identity || handoff.CgroupIdentity == nil ||
		!handoff.CgroupIdentity.Equal(*reservation.LeafIdentity) || handoff.AnchorPID != reservation.Anchor.PID ||
		handoff.AnchorIdentity != reservation.Anchor.Identity || handoff.LauncherPID != reservation.Launcher.PID ||
		handoff.LauncherIdentity != reservation.Launcher.Identity || handoff.LauncherExit == nil ||
		!handoff.LauncherExit.Equal(*reservation.LauncherExit) {
		return false
	}
	if handoff.SupervisorPID == 0 {
		return !requireBound && handoff.SupervisorIdentity == "" && handoff.Mirror == nil && reservation.Mirror != nil
	}
	return requireBound && handoff.SupervisorPID == reservation.Helper.PID &&
		handoff.SupervisorIdentity == reservation.Helper.Identity && reservation.Mirror != nil &&
		equalAuthorityProcess(handoff.Mirror, reservation.Mirror)
}

func linuxCgroupReservationMatchesAuthority(reservation LinuxCgroupReservation, value authority.Supervisor) bool {
	return reservation.Validate() == nil && reservation.AbortProof == nil && reservation.PreGateStopReceipt == nil &&
		reservation.LauncherGrantPossible && value.Version == authority.SupervisorVersionV2 &&
		reservation.LeafIdentity != nil && reservation.Helper != nil && reservation.Launcher != nil &&
		reservation.LauncherExit != nil && reservation.Gate != nil && reservation.Mirror != nil &&
		value.LaunchToken == reservation.LaunchToken && value.Secret == reservation.Secret &&
		value.PipeToken == reservation.PipeToken && value.JobID == reservation.JobID &&
		value.OwnerKind == authority.OwnerKindLinuxHelper && value.OwnerContext == reservation.RecoveryEndpoint &&
		value.OwnerUID == reservation.OwnerUID && value.LauncherUID == reservation.LauncherUID &&
		value.TargetBirthKind == reservation.Gate.BirthKind && value.TargetPID == reservation.Gate.Process.PID &&
		value.TargetIdentity == reservation.Gate.Process.Identity && value.CgroupIdentity != nil &&
		value.CgroupIdentity.Equal(*reservation.LeafIdentity) && value.AnchorPID == reservation.Anchor.PID &&
		value.AnchorIdentity == reservation.Anchor.Identity && value.LauncherPID == reservation.Launcher.PID &&
		value.LauncherIdentity == reservation.Launcher.Identity && value.LauncherExit != nil &&
		value.LauncherExit.Equal(*reservation.LauncherExit) && value.SupervisorPID == reservation.Helper.PID &&
		value.SupervisorIdentity == reservation.Helper.Identity && equalAuthorityProcess(value.Mirror, reservation.Mirror)
}

func linuxCgroupReceiptMatchesReservation(reservation LinuxCgroupReservation, receipt authority.StopReceipt) bool {
	if reservation.Validate() != nil || reservation.AbortProof != nil || reservation.PreGateStopReceipt != nil ||
		!reservation.LauncherGrantPossible || reservation.LeafIdentity == nil || reservation.Helper == nil ||
		reservation.Launcher == nil || reservation.LauncherExit == nil || reservation.Gate == nil || reservation.Mirror == nil {
		return false
	}
	value := authority.Supervisor{
		Version:            authority.SupervisorVersionV2,
		Secret:             reservation.Secret,
		LaunchToken:        reservation.LaunchToken,
		OwnerKind:          authority.OwnerKindLinuxHelper,
		OwnerContext:       reservation.RecoveryEndpoint,
		TargetPID:          reservation.Gate.Process.PID,
		TargetIdentity:     reservation.Gate.Process.Identity,
		OwnerUID:           reservation.OwnerUID,
		LauncherUID:        reservation.LauncherUID,
		PipeToken:          reservation.PipeToken,
		JobID:              reservation.JobID,
		SupervisorPID:      reservation.Helper.PID,
		SupervisorIdentity: reservation.Helper.Identity,
		TargetBirthKind:    reservation.Gate.BirthKind,
		CgroupIdentity:     reservation.LeafIdentity,
		AnchorPID:          reservation.Anchor.PID,
		AnchorIdentity:     reservation.Anchor.Identity,
		LauncherPID:        reservation.Launcher.PID,
		LauncherIdentity:   reservation.Launcher.Identity,
		LauncherExit:       reservation.LauncherExit,
		Mirror:             reservation.Mirror,
	}
	if value.Validate() != nil || !receipt.ValidFor(value) || receipt.LinuxProof == nil {
		return false
	}
	if receipt.LinuxProof.Kind == authority.LinuxCgroupStopProofMirror {
		return reservation.HelperExit != nil && receipt.LinuxProof.HelperExit != nil &&
			receipt.LinuxProof.HelperExit.Equal(*reservation.HelperExit)
	}
	return receipt.LinuxProof.Kind == authority.LinuxCgroupStopProofHelper && reservation.HelperExit == nil
}

func linuxCgroupReservationAbortMatches(reservation LinuxCgroupReservation, proof authority.LinuxCgroupReservationAbortProof) bool {
	if reservation.Version != LinuxCgroupReservationVersion || proof.Validate() != nil || reservation.LauncherGrantPossible ||
		!proof.ValidFor(reservation.LaunchToken, reservation.Parent, reservation.LeafName, reservation.CreateStarted,
			reservation.OwnerUID, reservation.Anchor, *reservation.Mirror, reservation.Helper, reservation.Launcher) ||
		reservation.AbortProof != nil && !reservation.AbortProof.Equal(proof) ||
		reservation.Gate != nil || reservation.PreGateStopReceipt != nil {
		return false
	}
	switch proof.LeafDisposition {
	case authority.LinuxCgroupAbortLeafNeverCreated:
		return reservation.LeafIdentity == nil && reservation.Helper == nil && reservation.HelperExit == nil &&
			reservation.Launcher == nil && reservation.LauncherExit == nil && proof.LeafIdentity == nil &&
			proof.LauncherExit == nil && !proof.LauncherGrantPossible
	case authority.LinuxCgroupAbortLeafRemovedEmpty, authority.LinuxCgroupAbortLeafAbsentAfterRmdir:
		if reservation.LeafIdentity == nil || proof.LeafIdentity == nil || !reservation.LeafIdentity.Equal(*proof.LeafIdentity) ||
			!proof.LeafEmpty || !proof.LeafAbsent || proof.LauncherGrantPossible {
			return false
		}
		if reservation.Launcher == nil {
			if reservation.LauncherExit != nil || proof.LauncherExit != nil {
				return false
			}
		} else if reservation.LauncherExit == nil || proof.LauncherExit == nil ||
			!reservation.LauncherExit.Equal(*proof.LauncherExit) ||
			!proof.LauncherExit.ValidForOwnedDescendant(*reservation.Launcher, *reservation.Helper, reservation.Anchor, reservation.HelperExit) {
			return false
		}
		if reservation.Helper == nil {
			return reservation.HelperExit == nil && proof.HelperExit == nil
		}
		return reservation.HelperExit != nil && proof.HelperExit != nil &&
			reservation.HelperExit.Equal(*proof.HelperExit) &&
			proof.HelperExit.ValidFor(*reservation.Helper, reservation.Anchor)
	default:
		return false
	}
}

func linuxCgroupPreGateReceiptMatchesReservation(reservation LinuxCgroupReservation, receipt authority.LinuxCgroupPreGateStopReceipt) bool {
	return reservation.Version == LinuxCgroupReservationVersion && reservation.ValidateWithoutPreGateReceipt() == nil &&
		reservation.CreateStarted && reservation.LauncherGrantPossible && reservation.LeafIdentity != nil &&
		reservation.Helper != nil && reservation.Launcher != nil && reservation.LauncherExit != nil &&
		reservation.Mirror != nil && reservation.Gate == nil && reservation.AbortProof == nil &&
		(reservation.PreGateStopReceipt == nil || reservation.PreGateStopReceipt.Equal(receipt)) &&
		receipt.ValidFor(reservation.LaunchToken, *reservation.LeafIdentity, reservation.OwnerUID, reservation.LauncherUID,
			reservation.Anchor, *reservation.Helper, *reservation.Launcher, *reservation.Mirror, *reservation.LauncherExit, reservation.HelperExit)
}

func sameContainmentScans(left, right *ContainmentScanJournal) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func requireLinuxScansCleanForHandoff(journal RunJournal, handoff authority.SupervisorHandoff) error {
	if handoff.OwnerKind != authority.OwnerKindLinuxHelper {
		return nil
	}
	if handoff.SupervisorPID <= 0 || handoff.SupervisorIdentity == "" || journal.ContainmentScans == nil ||
		!journal.ContainmentScans.matchesHandoff(handoff) {
		return errors.New("Linux supervisor scan fence is missing or mismatched")
	}
	if journal.ContainmentUnproven || !journal.ContainmentScans.clean() {
		return errors.New("Linux supervisor scan evidence is unresolved")
	}
	return nil
}

func initialPrecommitLinuxScansAbortable(scans ContainmentScanJournal) bool {
	return initialPrecommitLinuxScanProgressAbortable(scans.Helper) && initialPrecommitLinuxScanProgressAbortable(scans.Mirror)
}

func initialPrecommitLinuxScanProgressAbortable(progress ContainmentScanProgress) bool {
	if progress.UnprovenSequence != 0 || progress.FailureKind != "" || progress.CompletedSequence > 1 ||
		progress.PendingSequence > 1 || progress.CleanResultSequence > 1 || progress.SealedSequence > 1 {
		return false
	}
	if progress.CompletedSequence == 0 {
		return progress.PendingSequence == 1 && progress.SealedSequence == 0
	}
	return progress.PendingSequence == 0 && progress.CleanResultSequence == 0 &&
		(progress.SealedSequence == 0 || progress.SealedSequence == progress.CompletedSequence)
}

func requireLinuxScansCleanForAuthority(journal RunJournal, value authority.Supervisor) error {
	if value.OwnerKind != authority.OwnerKindLinuxHelper {
		return nil
	}
	if journal.ContainmentScans == nil || !journal.ContainmentScans.matchesAuthority(value) {
		return errors.New("Linux supervisor scan fence is missing or mismatched")
	}
	if journal.ContainmentUnproven || !journal.ContainmentScans.clean() {
		return errors.New("Linux supervisor scan evidence is unresolved")
	}
	return nil
}

func requireLinuxScansSealedForHandoff(journal RunJournal, handoff authority.SupervisorHandoff) error {
	if handoff.OwnerKind != authority.OwnerKindLinuxHelper {
		return nil
	}
	if handoff.SupervisorPID <= 0 || handoff.SupervisorIdentity == "" || journal.ContainmentScans == nil ||
		!journal.ContainmentScans.matchesHandoff(handoff) {
		return errors.New("Linux supervisor scan fence is missing or mismatched")
	}
	if journal.ContainmentUnproven || !journal.ContainmentScans.sealed() {
		return errors.New("Linux supervisor scan seals are unresolved")
	}
	return nil
}

func requireLinuxScansSealedForAuthority(journal RunJournal, value authority.Supervisor) error {
	if value.OwnerKind != authority.OwnerKindLinuxHelper {
		return nil
	}
	if journal.ContainmentScans == nil || !journal.ContainmentScans.matchesAuthority(value) {
		return errors.New("Linux supervisor scan fence is missing or mismatched")
	}
	if journal.ContainmentUnproven || !journal.ContainmentScans.sealed() {
		return errors.New("Linux supervisor scan seals are unresolved")
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
	if journal.PID == 0 && (!journal.StartedAt.IsZero() || journal.ProcessIdentity != "" || journal.ContainmentAuthority != nil) {
		return errors.New("run journal process details are invalid")
	}
	if journal.PID > 0 && (journal.StartedAt.IsZero() || !validRequiredString(journal.ProcessIdentity, 4096)) {
		return errors.New("run journal process details are invalid")
	}
	if journal.ContainmentUnproven && !journal.HasProcessDetails() && journal.ContainmentHandoff == nil &&
		journal.ContainmentScans == nil && journal.ContainmentCgroup == nil {
		return errors.New("run journal containment uncertainty is invalid")
	}
	if journal.ContainmentAuthority != nil {
		if err := journal.ContainmentAuthority.Validate(); err != nil ||
			journal.ContainmentAuthority.TargetPID != journal.PID ||
			journal.ContainmentAuthority.TargetIdentity != journal.ProcessIdentity {
			return errors.New("run journal containment authority is invalid")
		}
		if journal.ContainmentAuthority.Version == authority.SupervisorVersionV2 {
			if journal.ContainmentCgroup == nil || journal.ContainmentScans != nil ||
				journal.ContainmentCgroupReleaseProof != nil ||
				!linuxCgroupReservationMatchesAuthority(*journal.ContainmentCgroup, *journal.ContainmentAuthority) ||
				journal.ContainmentCgroup.Gate == nil || !journal.StartedAt.Equal(journal.ContainmentCgroup.Gate.ObservedAt) ||
				journal.ContainmentAuthority.StopReceipt != nil && journal.ContainmentUnproven {
				return errors.New("run journal v2 containment authority is invalid")
			}
		} else if journal.ContainmentCgroup != nil {
			return errors.New("v1 containment authority cannot use a v2 cgroup reservation")
		}
	}
	if journal.ContainmentHandoff != nil {
		if journal.ContainmentAuthority != nil || journal.HasProcessDetails() || !journal.hasClaimGrant() {
			return errors.New("run journal containment handoff is invalid")
		}
		if err := journal.ContainmentHandoff.Validate(); err != nil {
			return errors.New("run journal containment handoff is invalid")
		}
		if journal.ContainmentHandoff.Version == authority.SupervisorHandoffVersionV2 {
			if journal.ContainmentCgroup == nil || journal.ContainmentScans != nil ||
				journal.ContainmentCgroupReleaseProof != nil ||
				!linuxCgroupReservationMatchesHandoff(*journal.ContainmentCgroup, *journal.ContainmentHandoff, journal.ContainmentHandoff.SupervisorPID > 0) ||
				journal.ContainmentHandoff.StopReceipt != nil && journal.ContainmentUnproven {
				return errors.New("run journal v2 containment handoff is invalid")
			}
		} else if journal.ContainmentCgroup != nil {
			return errors.New("v1 containment handoff cannot use a v2 cgroup reservation")
		}
	}
	if journal.ContainmentCgroup != nil {
		reservation := journal.ContainmentCgroup
		if reservation.Validate() != nil || !journal.hasClaimGrant() || journal.ContainmentScans != nil {
			return errors.New("run journal v2 cgroup reservation is invalid")
		}
		if reservation.AbortProof != nil && (journal.ContainmentUnproven || journal.ContainmentAuthority != nil ||
			journal.ContainmentHandoff != nil || journal.HasProcessDetails()) {
			return errors.New("run journal aborted cgroup reservation has active owner state")
		}
		if reservation.PreGateStopReceipt != nil && (journal.ContainmentUnproven || journal.ContainmentAuthority != nil ||
			journal.ContainmentHandoff != nil || journal.HasProcessDetails()) {
			return errors.New("run journal pre-gate cgroup receipt has active owner state")
		}
		if journal.ContainmentAuthority == nil && journal.ContainmentHandoff == nil {
			if journal.HasProcessDetails() {
				return errors.New("uncommitted v2 cgroup reservation has process authority")
			}
		}
		if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.Version != authority.SupervisorVersionV2 {
			return errors.New("v2 cgroup reservation cannot accompany a v1 authority")
		}
		if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Version != authority.SupervisorHandoffVersionV2 {
			return errors.New("v2 cgroup reservation cannot accompany a v1 handoff")
		}
	}
	if journal.ContainmentScans != nil {
		scans := journal.ContainmentScans
		if journal.ContainmentCgroup != nil || scans.Version != 1 || len(scans.LaunchToken) != authority.TokenBytes*2 || !validHex(scans.LaunchToken) ||
			len(scans.MirrorIdentity) != authority.TokenBytes*2 || !validHex(scans.MirrorIdentity) || scans.TargetPID <= 0 ||
			!validRequiredString(scans.TargetIdentity, 4096) || scans.SupervisorPID <= 0 ||
			!validRequiredString(scans.SupervisorIdentity, 4096) ||
			!validContainmentScanProgress(scans.Helper) || !validContainmentScanProgress(scans.Mirror) {
			return errors.New("run journal containment scan state is invalid")
		}
		if journal.ContainmentHandoff != nil {
			handoff := journal.ContainmentHandoff
			if handoff.OwnerKind != authority.OwnerKindLinuxHelper || handoff.SupervisorPID <= 0 || !scans.matchesHandoff(*handoff) {
				return errors.New("run journal containment scan handoff is invalid")
			}
		} else if journal.ContainmentAuthority != nil {
			value := journal.ContainmentAuthority
			if value.OwnerKind != authority.OwnerKindLinuxHelper || !scans.matchesAuthority(*value) {
				return errors.New("run journal containment scan authority is invalid")
			}
		} else {
			return errors.New("run journal containment scan state has no Linux owner")
		}
		if (scans.Helper.UnprovenSequence != 0 || scans.Mirror.UnprovenSequence != 0) && !journal.ContainmentUnproven {
			return errors.New("run journal containment scan failure is not sticky")
		}
	}
	if releaseProof := journal.ContainmentCgroupReleaseProof; releaseProof != nil {
		if releaseProof.Validate() != nil || releaseProof.Version != authority.SupervisorHandoffVersionV2 ||
			journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil || journal.ContainmentCgroup != nil ||
			journal.ContainmentScans != nil || journal.ContainmentCgroupPreGateReleaseProof != nil ||
			journal.HasProcessDetails() || journal.PID != 0 ||
			journal.ProcessIdentity != "" || !journal.StartedAt.IsZero() || journal.ContainmentUnproven {
			return errors.New("run journal v2 cgroup release proof is invalid")
		}
	}
	if releaseProof := journal.ContainmentCgroupPreGateReleaseProof; releaseProof != nil {
		if releaseProof.Validate() != nil || journal.ContainmentCgroupReleaseProof != nil ||
			journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil || journal.ContainmentCgroup != nil ||
			journal.ContainmentScans != nil || journal.HasProcessDetails() || journal.PID != 0 ||
			journal.ProcessIdentity != "" || !journal.StartedAt.IsZero() || journal.ContainmentUnproven {
			return errors.New("run journal pre-gate cgroup release proof is invalid")
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
	if !journal.hasClaimGrant() && (journal.PID != 0 || !journal.StartedAt.IsZero() || journal.ContainmentCgroup != nil ||
		journal.LinuxCloneIntent != nil || journal.LinuxCloneSeedReceipt != nil || journal.LinuxCloneCheckoutReceipt != nil ||
		journal.LinuxClonePromotionReceipt != nil || journal.LinuxCloneAbortPending || journal.LinuxCloneAbortProof != nil ||
		len(journal.PendingEvents) != 0 || journal.DroppedOutputChunks != 0 ||
		journal.DroppedOutputBytes != 0 || len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0) {
		return errors.New("run journal is invalid")
	}
	if err := validateLinuxCloneJournal(journal); err != nil {
		return err
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

func validContainmentScanProgress(progress ContainmentScanProgress) bool {
	if progress.SealedSequence != 0 &&
		(progress.CompletedSequence == 0 || progress.SealedSequence != progress.CompletedSequence ||
			progress.PendingSequence != 0 || progress.CleanResultSequence != 0 || progress.UnprovenSequence != 0 || progress.FailureKind != "") {
		return false
	}
	if progress.PendingSequence != 0 {
		if progress.UnprovenSequence != 0 || progress.CompletedSequence == ^uint64(0) ||
			progress.PendingSequence != progress.CompletedSequence+1 {
			return false
		}
	}
	if progress.CleanResultSequence != 0 &&
		(progress.PendingSequence != progress.CleanResultSequence || progress.CompletedSequence == ^uint64(0) ||
			progress.CleanResultSequence != progress.CompletedSequence+1 || progress.UnprovenSequence != 0) {
		return false
	}
	if progress.UnprovenSequence == 0 {
		return progress.FailureKind == ""
	}
	if progress.PendingSequence != 0 || progress.CleanResultSequence != 0 || !validRequiredString(progress.FailureKind, 128) {
		return false
	}
	return progress.CompletedSequence != ^uint64(0) && progress.UnprovenSequence == progress.CompletedSequence+1 ||
		progress.UnprovenSequence == progress.CompletedSequence && progress.FailureKind == "clean_retire_ack_failure"
}

func validRequiredString(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit
}

func validWork(work protocol.Work) bool {
	return len(work.Goal) <= 65536 && len(work.AgentProfile) <= 4096 && len(work.Workspace) <= 4096 && validRawMessage(work.Input)
}

func sameWork(left, right protocol.Work) bool {
	return left.Goal == right.Goal && left.AgentProfile == right.AgentProfile && left.Workspace == right.Workspace &&
		bytes.Equal(left.Input, right.Input)
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
