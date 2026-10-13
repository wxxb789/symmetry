//go:build linux && amd64

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/opencode"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const (
	LinuxV2NativeOwnerWitnessEnvironment = "SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS"
	linuxV2NativeOwnerWitnessRoot        = "/opt/symmetry-owner-test"
	// This is the sole witness data path outside the fixture root. Config must
	// match the root-installed OwnerConfig and App pins its exact private inode.
	linuxV2NativeOwnerWitnessStateDir   = "/var/lib/symmetry-v2"
	linuxV2NativeWitnessRequestMax      = 1 << 20
	linuxV2NativeOwnerWitnessTimeoutMax = 30
	linuxV2NativeOwnerWitnessPoll       = 25 * time.Millisecond
	linuxV2NativeOwnerWitnessHookEnv    = "SYMMETRY_LINUX_V2_NATIVE_HOOK_MARKER_ROOT"
)

const (
	linuxV2NativeWitnessReserveStage     = "reservation_durable"
	linuxV2NativeWitnessCreateStage      = "create_started"
	linuxV2NativeWitnessLeafStage        = "leaf_bound"
	linuxV2NativeWitnessHelperStage      = "helper_bound"
	linuxV2NativeWitnessLauncherStage    = "launcher_bound"
	linuxV2NativeWitnessGateStage        = "gate_bound"
	linuxV2NativeWitnessPrepareStage     = "handoff_prepared"
	linuxV2NativeWitnessCommitStage      = "handoff_committed"
	linuxV2NativeWitnessLeaseStage       = "lease_armed"
	linuxV2NativeWitnessCheckoutStage    = "checkout_ready"
	linuxV2NativeWitnessReleaseStage     = "native_release"
	linuxV2NativeWitnessTurnStage        = "native_turn_started"
	linuxV2NativeWitnessStopReceiptStage = "stop_receipt_durable"
	linuxV2NativeWitnessLeafRemovedStage = "leaf_removed"
	linuxV2NativeWitnessClearedStage     = "journal_cleared"
	linuxV2NativeWitnessChildStage       = "detached_child_live"
)

var (
	errLinuxV2NativeWitnessReplayTargetMissing = errors.New("replay-required owner witness RunKey is absent")
)

type linuxV2NativeWitnessNativeReleaseState string

const (
	linuxV2NativeWitnessNativeReleaseNotReleased linuxV2NativeWitnessNativeReleaseState = "not_released"
	linuxV2NativeWitnessNativeReleaseReleased    linuxV2NativeWitnessNativeReleaseState = "released"
	linuxV2NativeWitnessNativeReleaseUnknown     linuxV2NativeWitnessNativeReleaseState = "unknown"
)

func linuxV2NativeWitnessStageReleaseState(stage string) (linuxV2NativeWitnessNativeReleaseState, bool) {
	switch stage {
	case linuxV2NativeWitnessReserveStage, linuxV2NativeWitnessCreateStage, linuxV2NativeWitnessLeafStage,
		linuxV2NativeWitnessHelperStage, linuxV2NativeWitnessLauncherStage, linuxV2NativeWitnessGateStage,
		linuxV2NativeWitnessPrepareStage, linuxV2NativeWitnessCommitStage, linuxV2NativeWitnessLeaseStage,
		linuxV2NativeWitnessCheckoutStage:
		return linuxV2NativeWitnessNativeReleaseNotReleased, true
	case linuxV2NativeWitnessReleaseStage, linuxV2NativeWitnessTurnStage,
		linuxV2NativeWitnessChildStage:
		return linuxV2NativeWitnessNativeReleaseReleased, true
	case linuxV2NativeWitnessStopReceiptStage, linuxV2NativeWitnessLeafRemovedStage, linuxV2NativeWitnessClearedStage:
		return linuxV2NativeWitnessNativeReleaseUnknown, false
	default:
		return linuxV2NativeWitnessNativeReleaseUnknown, false
	}
}

func linuxV2NativeWitnessValidReleaseState(value linuxV2NativeWitnessNativeReleaseState) bool {
	return value == linuxV2NativeWitnessNativeReleaseNotReleased || value == linuxV2NativeWitnessNativeReleaseReleased ||
		value == linuxV2NativeWitnessNativeReleaseUnknown
}

type linuxV2NativeWitnessRunKey struct {
	RunID      string `json:"run_id"`
	Generation int64  `json:"generation"`
}

type linuxV2NativeWitnessBarrier struct {
	Directory      string `json:"directory"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	HoldAfter      string `json:"hold_after"`
	ReleaseFile    string `json:"release_file,omitempty"`
}

type linuxV2NativeWitnessRequest struct {
	Version        int                         `json:"version"`
	Case           string                      `json:"case"`
	Harness        string                      `json:"harness"`
	RunKey         linuxV2NativeWitnessRunKey  `json:"run_key"`
	ReplayRequired bool                        `json:"replay_required,omitempty"`
	Fence          protocol.Fence              `json:"fence"`
	Claim          protocol.ClaimResponse      `json:"-"`
	Subject        protocol.Subject            `json:"subject"`
	Barrier        linuxV2NativeWitnessBarrier `json:"barrier"`
	DropAckAfter   string                      `json:"drop_ack_after,omitempty"`
}

type linuxV2NativeWitnessFenceSummary struct {
	RuntimeID    string `json:"runtime_id"`
	RuntimeEpoch int64  `json:"runtime_epoch"`
	Generation   int64  `json:"generation"`
	ClaimID      string `json:"claim_id"`
}

type linuxV2NativeWitnessProcess struct {
	PID      int    `json:"pid,omitempty"`
	Identity string `json:"identity,omitempty"`
}

type linuxV2NativeWitnessStages struct {
	ReservationDurable bool `json:"reservation_durable"`
	CreateStarted      bool `json:"create_started"`
	LeafBound          bool `json:"leaf_bound"`
	HelperBound        bool `json:"helper_bound"`
	LauncherBound      bool `json:"launcher_bound"`
	GateBound          bool `json:"gate_bound"`
	HandoffPrepared    bool `json:"handoff_prepared"`
	HandoffCommitted   bool `json:"handoff_committed"`
	InitialLeaseAcked  bool `json:"initial_lease_acked"`
	CheckoutReady      bool `json:"checkout_ready"`
	NativeReleased     bool `json:"native_released"`
	NativeTurnStarted  bool `json:"native_turn_started"`
	StopReceiptDurable bool `json:"stop_receipt_durable"`
	LeafRemoved        bool `json:"leaf_removed"`
	JournalCleared     bool `json:"journal_cleared"`
	DetachedChildLive  bool `json:"detached_child_live"`
}

type linuxV2NativeWitnessTarget struct {
	PID              int                           `json:"pid,omitempty"`
	Identity         string                        `json:"identity,omitempty"`
	PIDFDVerified    bool                          `json:"pidfd_verified"`
	HostUID          uint32                        `json:"host_uid,omitempty"`
	UserNamespace    linuxV2NativeWitnessNamespace `json:"user_namespace"`
	NetworkNamespace linuxV2NativeWitnessNamespace `json:"network_namespace"`
	SamePIDExec      bool                          `json:"same_pid_exec"`
}

type linuxV2NativeWitnessNamespace struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type linuxV2NativeWitnessOwners struct {
	Anchor   linuxV2NativeWitnessProcess `json:"anchor"`
	Helper   linuxV2NativeWitnessProcess `json:"helper"`
	Launcher linuxV2NativeWitnessProcess `json:"launcher"`
	Gate     linuxV2NativeWitnessProcess `json:"gate"`
	Reaper   linuxV2NativeWitnessProcess `json:"reaper"`
}

type linuxV2NativeWitnessLeaf struct {
	Version       int    `json:"version,omitempty"`
	ParentMountID uint64 `json:"parent_mount_id,omitempty"`
	ParentDevice  uint64 `json:"parent_device,omitempty"`
	ParentInode   uint64 `json:"parent_inode,omitempty"`
	Device        uint64 `json:"device,omitempty"`
	Inode         uint64 `json:"inode,omitempty"`
	NameSHA256    string `json:"name_sha256,omitempty"`
	Removed       bool   `json:"removed"`
}

type linuxV2NativeWitnessClone struct {
	IntentSHA256   string `json:"intent_sha256,omitempty"`
	SeedDigest     string `json:"seed_digest,omitempty"`
	CheckoutReady  bool   `json:"checkout_ready"`
	CheckoutDevice uint64 `json:"checkout_device,omitempty"`
	CheckoutInode  uint64 `json:"checkout_inode,omitempty"`
}

type linuxV2NativeWitnessExit struct {
	PID      int    `json:"pid,omitempty"`
	Identity string `json:"identity,omitempty"`
	Waited   bool   `json:"waited"`
	Reaped   bool   `json:"reaped"`
}

type linuxV2NativeWitnessStop struct {
	ReceiptVersion      int                         `json:"receipt_version,omitempty"`
	ReceiptStatus       string                      `json:"receipt_status,omitempty"`
	ProofKind           string                      `json:"proof_kind,omitempty"`
	Issuer              linuxV2NativeWitnessProcess `json:"issuer"`
	Reaper              linuxV2NativeWitnessProcess `json:"reaper"`
	KillAcknowledged    bool                        `json:"kill_acknowledged"`
	PopulatedZero       bool                        `json:"populated_zero"`
	NoMigrationObserved bool                        `json:"no_migration_observed"`
	ProducerFenced      bool                        `json:"producer_fenced"`
	DescendantsReaped   bool                        `json:"descendants_reaped"`
	LauncherExit        linuxV2NativeWitnessExit    `json:"launcher_exit"`
	GateExit            linuxV2NativeWitnessExit    `json:"gate_exit"`
	HelperExit          *linuxV2NativeWitnessExit   `json:"helper_exit,omitempty"`
}

type linuxV2NativeWitnessRelease struct {
	Version               int    `json:"version,omitempty"`
	LeafDisposition       string `json:"leaf_disposition,omitempty"`
	OpaquePlatformRelease bool   `json:"opaque_platform_release"`
	LeafRemoved           bool   `json:"leaf_removed"`
	ClearPersisted        bool   `json:"clear_persisted"`
	AnchorRetired         bool   `json:"anchor_retired"`
}

type linuxV2NativeWitnessProviderAction struct {
	Attempted      bool   `json:"attempted"`
	CompletedCount int    `json:"completed_count"`
	Outcome        string `json:"outcome,omitempty"`
}

type linuxV2NativeWitnessHealth struct {
	Attempted                bool                          `json:"attempted"`
	Healthy                  bool                          `json:"healthy"`
	PeerVerified             bool                          `json:"peer_verified"`
	PeerPID                  int                           `json:"peer_pid,omitempty"`
	PeerIdentity             string                        `json:"peer_identity,omitempty"`
	NetworkNamespaceVerified bool                          `json:"network_namespace_verified"`
	NetworkNamespace         linuxV2NativeWitnessNamespace `json:"network_namespace"`
}

type linuxV2NativeWitnessDetachedChild struct {
	PID                    int    `json:"pid"`
	Identity               string `json:"identity"`
	ParentPID              int    `json:"parent_pid"`
	ProcessGroup           int    `json:"process_group"`
	SessionID              int    `json:"session_id"`
	PIDFDVerified          bool   `json:"pidfd_verified"`
	LeafMembershipVerified bool   `json:"leaf_membership_verified"`
	AnchorReaped           bool   `json:"anchor_reaped"`
}

type linuxV2NativeWitnessSummary struct {
	Version                int                                    `json:"version"`
	Case                   string                                 `json:"case"`
	Harness                string                                 `json:"harness"`
	NativeVersion          string                                 `json:"native_version"`
	InstalledOwnerVerified bool                                   `json:"installed_owner_verified"`
	NativeReleaseState     linuxV2NativeWitnessNativeReleaseState `json:"native_release_state"`
	Outcome                string                                 `json:"outcome"`
	RecoveryState          string                                 `json:"recovery_state,omitempty"`
	ResponseLostAfter      string                                 `json:"response_lost_after,omitempty"`
	HelperDropObserved     bool                                   `json:"helper_drop_observed"`
	HelperDropOperation    string                                 `json:"helper_drop_operation,omitempty"`
	JournalUnproven        bool                                   `json:"journal_unproven"`
	NoTargetAbort          bool                                   `json:"no_target_abort"`
	FailureCategory        string                                 `json:"failure_category"`
	RunKey                 linuxV2NativeWitnessRunKey             `json:"run_key"`
	Fence                  linuxV2NativeWitnessFenceSummary       `json:"fence"`
	Subject                protocol.Subject                       `json:"subject"`
	Stages                 linuxV2NativeWitnessStages             `json:"stages"`
	Target                 linuxV2NativeWitnessTarget             `json:"target"`
	Owners                 linuxV2NativeWitnessOwners             `json:"owners"`
	Leaf                   linuxV2NativeWitnessLeaf               `json:"leaf"`
	Clone                  linuxV2NativeWitnessClone              `json:"clone"`
	Stop                   linuxV2NativeWitnessStop               `json:"stop"`
	Release                linuxV2NativeWitnessRelease            `json:"release"`
	ProviderAction         linuxV2NativeWitnessProviderAction     `json:"provider_action"`
	Health                 linuxV2NativeWitnessHealth             `json:"health"`
	DetachedChild          *linuxV2NativeWitnessDetachedChild     `json:"detached_child,omitempty"`
}

type linuxV2NativeWitnessStageMarker struct {
	Version            int                                    `json:"version"`
	Case               string                                 `json:"case"`
	Stage              string                                 `json:"stage"`
	HoldAfter          string                                 `json:"hold_after"`
	DropAckAfter       string                                 `json:"drop_ack_after,omitempty"`
	NativeReleaseState linuxV2NativeWitnessNativeReleaseState `json:"native_release_state"`
	RunKey             linuxV2NativeWitnessRunKey             `json:"run_key"`
	Target             linuxV2NativeWitnessTarget             `json:"target"`
	Owners             linuxV2NativeWitnessOwners             `json:"owners"`
	Leaf               linuxV2NativeWitnessLeaf               `json:"leaf"`
	Stop               linuxV2NativeWitnessStop               `json:"stop"`
	Release            linuxV2NativeWitnessRelease            `json:"release"`
	DetachedChild      *linuxV2NativeWitnessDetachedChild     `json:"detached_child,omitempty"`
}

type linuxV2NativeWitnessRun struct {
	mu                   sync.Mutex
	ctx                  context.Context
	request              linuxV2NativeWitnessRequest
	config               config.Config
	ownerConfig          linuxgate.OwnerConfig
	barrier              *linuxV2NativeWitnessBarriers
	stages               linuxV2NativeWitnessStages
	summary              linuxV2NativeWitnessSummary
	process              Process
	peer                 *execution.LinuxV2Peer
	prepared             workspace.Prepared
	grant                authority.LinuxCloneGrant
	cloneWorkspace       linuxV2CloneWorkspace
	child                *linuxV2NativeWitnessDetachedChild
	childPIDFD           *os.File
	nativeEnvironment    []string
	runnerBarrierRestore func()
	runnerBarrierErr     error
}

// RunLinuxV2NativeOwnerWitness is an opt-in production-boundary test path. It
// bypasses Control registration and scheduling, but uses the installed shared
// StateDir and normal App claim/clone CAS, Linux v2 Runner, pinned adapters,
// peer checks, and stop receipt/release path. Fixture inputs and workspaces
// remain restricted to the fixed test root.
func RunLinuxV2NativeOwnerWitness(ctx context.Context, configPath, requestPath string, output io.Writer) (resultErr error) {
	if os.Getenv(LinuxV2NativeOwnerWitnessEnvironment) != "1" {
		return errors.New("Linux v2 native owner witness is disabled")
	}
	if ctx == nil || output == nil {
		return errors.New("Linux v2 native owner witness requires a context and output writer")
	}
	if err := validateLinuxV2NativeWitnessPath(configPath); err != nil {
		return fmt.Errorf("validate owner witness config path: %w", err)
	}
	if err := validateLinuxV2NativeWitnessPath(requestPath); err != nil {
		return fmt.Errorf("validate owner witness request path: %w", err)
	}
	ownerConfig, err := linuxgate.ReadInstalledOwnerConfig()
	if err != nil {
		return fmt.Errorf("read trusted Linux A/B owner config: %w", err)
	}
	if err := validateLinuxV2NativeWitnessInstalledOwner(ownerConfig, uint32(os.Getuid()), uint32(os.Geteuid()), uint32(os.Getgid()), uint32(os.Getegid())); err != nil {
		return err
	}
	configBytes, err := readLinuxV2NativeWitnessFile(configPath, ownerConfig, false)
	if err != nil {
		return fmt.Errorf("read root-controlled owner witness config: %w", err)
	}
	value, err := decodeLinuxV2NativeWitnessConfig(configBytes)
	if err != nil {
		return err
	}
	// Never take this primary group from JSON or the ambient environment.
	if err := populateTrustedLinuxWitnessPiExtensionGroup(&value, ownerConfig); err != nil {
		return err
	}
	if err := validateLinuxV2NativeWitnessConfig(value, ownerConfig); err != nil {
		return err
	}
	requestBytes, err := readLinuxV2NativeWitnessFile(requestPath, ownerConfig, true)
	if err != nil {
		return fmt.Errorf("read root-controlled owner witness request: %w", err)
	}
	defer clear(requestBytes)
	request, err := decodeLinuxV2NativeWitnessRequest(requestBytes)
	if err != nil {
		return err
	}
	if err := validateLinuxV2NativeWitnessRequest(request, value, ownerConfig); err != nil {
		return err
	}
	if err := validateLinuxV2NativeWitnessStateDirectory(value.StateDir, ownerConfig); err != nil {
		return err
	}
	barrier, err := newLinuxV2NativeWitnessBarriers(request, ownerConfig)
	if err != nil {
		return err
	}
	witness := &linuxV2NativeWitnessRun{
		ctx: ctx, request: request, config: value, ownerConfig: ownerConfig, barrier: barrier,
		summary: linuxV2NativeWitnessSummary{
			Version: 1, Case: request.Case, Harness: request.Harness,
			NativeVersion:          value.Runtime.HarnessVersion,
			InstalledOwnerVerified: true,
			NativeReleaseState:     linuxV2NativeWitnessNativeReleaseUnknown,
			RunKey:                 request.RunKey, Subject: request.Subject, Outcome: "unresolved",
			Fence: linuxV2NativeWitnessFenceSummary{
				RuntimeID: request.Fence.RuntimeID, RuntimeEpoch: request.Fence.RuntimeEpoch,
				Generation: request.Fence.Generation, ClaimID: request.Fence.ClaimID,
			},
		},
	}
	defer func() {
		if witness.childPIDFD != nil {
			resultErr = errors.Join(resultErr, witness.childPIDFD.Close())
			witness.childPIDFD = nil
		}
		if witness.peer != nil {
			resultErr = errors.Join(resultErr, witness.peer.Close())
			witness.peer = nil
		}
	}()

	store, err := state.New(value.StateDir)
	if err != nil {
		return fmt.Errorf("open private owner witness Store: %w", err)
	}
	storeOpen := true
	defer func() {
		if storeOpen {
			resultErr = errors.Join(resultErr, store.Close())
		}
	}()
	journals, err := store.ListJournals()
	if err != nil {
		return fmt.Errorf("list isolated owner witness journals: %w", err)
	}
	key := state.RunKey{RunID: request.RunKey.RunID, Generation: request.RunKey.Generation}
	selectedJournal, existing, err := selectLinuxV2NativeWitnessJournal(journals, request)
	if err != nil {
		return err
	}
	if existing {
		journal, err := store.LoadJournal(key)
		if err != nil {
			return fmt.Errorf("reload selected owner witness RunJournal under the Store lock: %w", err)
		}
		if err := verifyLinuxV2NativeWitnessSelectedJournal(selectedJournal, journal); err != nil {
			return err
		}
		if request.Case == "stage_replay" && linuxV2NativeWitnessHasLegacyLinuxOwner(journal) {
			if err := validateLinuxV2NativeWitnessLegacyJournal(journal, request); err != nil {
				return err
			}
			witness.summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseNotReleased
			legacyStop := linuxV2NativeWitnessLegacyStop(journal)
			recoveryErr := recoverLinuxV2NativeWitnessRunKey(ctx, value, store, journal)
			journal, err = store.LoadJournal(key)
			if err != nil {
				return fmt.Errorf("read exact legacy owner witness RunJournal after recovery: %w", err)
			}
			if err := validateLinuxV2NativeWitnessLegacyJournalIdentity(journal, request); err != nil {
				return err
			}
			if linuxV2NativeWitnessLegacyJournalUnproven(journal) {
				witness.summary.Outcome = "unresolved"
				witness.summary.RecoveryState = "unresolved"
				witness.summary.JournalUnproven = true
				witness.summary.FailureCategory = "containment_unproven"
			} else if recoveryErr != nil {
				return errors.New("legacy v1 recovery failed after its exact RunJournal was cleared")
			} else {
				witness.summary.Outcome = "no_native_effects"
				witness.summary.RecoveryState = "no_native_effects"
				witness.summary.Stop = legacyStop
			}
			if err := witness.validateFinalSummary(); err != nil {
				return err
			}
			if err := store.Close(); err != nil {
				return fmt.Errorf("close recovered legacy owner witness Store before summary: %w", err)
			}
			storeOpen = false
			if err := witness.closeCapturedFDs(); err != nil {
				return err
			}
			return writeLinuxV2NativeWitnessSummary(output, witness.summary)
		}
		if err := validateLinuxV2NativeWitnessExistingJournal(journal, request); err != nil {
			return err
		}
		recoveryErr := recoverLinuxV2NativeWitnessRunKey(ctx, value, store, journal)
		journal, err = store.LoadJournal(key)
		if err != nil {
			return fmt.Errorf("read selected owner witness RunJournal after exact-key recovery: %w", err)
		}
		if err := validateLinuxV2NativeWitnessExistingJournal(journal, request); err != nil {
			return err
		}
		if recoveryErr == nil && request.Case == "stage_replay" && journal.LinuxCloneIntent != nil &&
			journal.LinuxCloneCheckoutReceipt == nil && journal.LinuxClonePromotionReceipt == nil {
			cloneRecovery := &daemon{
				config: value, store: store, workspace: workspace.New(value.Workspaces), recoveryOnly: true,
				background: ctx, running: make(map[state.RunKey]*runningRun),
			}
			if cleanupErr := cloneRecovery.cleanupRecoveredWorkspace(ctx, journal, false); cleanupErr != nil {
				recoveryErr = cleanupErr
			}
			journal, err = store.LoadJournal(key)
			if err != nil {
				return fmt.Errorf("read no-checkout owner witness journal after clone cleanup: %w", err)
			}
			if err := validateLinuxV2NativeWitnessExistingJournal(journal, request); err != nil {
				return err
			}
		}
		if recoveryErr != nil {
			if request.Case != "stage_replay" || !linuxV2NativeWitnessJournalHasUnresolvedAuthority(journal) {
				return errors.New("recover existing owner witness RunJournal without launch")
			}
			if err := witness.summarizeJournal(journal, false); err != nil {
				return err
			}
			witness.summary.Outcome = "unresolved"
			witness.summary.RecoveryState = "unresolved"
			witness.summary.JournalUnproven = true
			witness.summary.FailureCategory = "containment_unproven"
			if err := witness.restoreStageMarkers(); err != nil {
				return err
			}
			witness.normalizeStageReplayTarget()
			if err := witness.validateFinalSummary(); err != nil {
				return err
			}
			if err := store.Close(); err != nil {
				return fmt.Errorf("close unresolved owner witness Store before summary: %w", err)
			}
			storeOpen = false
			if err := witness.closeCapturedFDs(); err != nil {
				return err
			}
			return writeLinuxV2NativeWitnessSummary(output, witness.summary)
		}
		if err := witness.summarizeJournal(journal, true); err != nil {
			return err
		}
		witness.summary.InstalledOwnerVerified = recoveryErr == nil
		if request.Case == "stage_replay" {
			switch witness.summary.RecoveryState {
			case "released", "pre_gate_released":
				witness.summary.Outcome = "recovered"
			case "aborted_no_target":
				witness.summary.Outcome = "aborted_no_target"
			case "no_native_effects":
				witness.summary.Outcome = "no_native_effects"
			case "unresolved":
				witness.summary.Outcome = "unresolved"
				witness.summary.JournalUnproven = true
				witness.summary.FailureCategory = "containment_unproven"
			default:
				return errors.New("stage replay recovery produced no exact durable recovery state")
			}
		} else {
			witness.summary.Outcome = "recovered"
		}
		if err := witness.restoreStageMarkers(); err != nil {
			return err
		}
		if request.Case == "stage_replay" {
			witness.normalizeStageReplayTarget()
		}
		if err := witness.validateFinalSummary(); err != nil {
			return err
		}
		if err := store.Close(); err != nil {
			return fmt.Errorf("close recovered owner witness Store before summary: %w", err)
		}
		storeOpen = false
		if err := witness.closeCapturedFDs(); err != nil {
			return err
		}
		return writeLinuxV2NativeWitnessSummary(output, witness.summary)
	}
	if err := barrier.ensureFresh(); err != nil {
		return err
	}
	if err := witness.validateFreshClaim(time.Now()); err != nil {
		return err
	}
	writeResponseLost := func(after string, cause error) error {
		if err := witness.summarizeResponseLost(store, after); err != nil {
			return errors.Join(cause, err)
		}
		if err := witness.validateFinalSummary(); err != nil {
			return errors.Join(cause, err)
		}
		if err := store.Close(); err != nil {
			return errors.Join(cause, fmt.Errorf("close response-lost owner witness Store before summary: %w", err))
		}
		storeOpen = false
		if err := witness.closeCapturedFDs(); err != nil {
			return errors.Join(cause, err)
		}
		if err := writeLinuxV2NativeWitnessSummary(output, witness.summary); err != nil {
			return errors.Join(cause, err)
		}
		if cause == nil {
			cause = errors.New("Linux v2 native owner witness observed a dropped response")
		}
		return errors.Join(cause, execution.ErrLinuxV2MutationAckLost)
	}
	if err := witness.runFresh(ctx, store); err != nil {
		if request.Case == "stage_replay" && errors.Is(err, execution.ErrLinuxV2MutationAckLost) && request.DropAckAfter != "" {
			return writeResponseLost(request.DropAckAfter, err)
		}
		witness.mu.Lock()
		helperDropObserved := witness.summary.HelperDropObserved
		helperDropOperation := witness.summary.HelperDropOperation
		witness.mu.Unlock()
		if request.Case == "stage_replay" && helperDropObserved {
			return writeResponseLost(helperDropOperation, err)
		}
		return err
	}
	if request.Case == "stage_replay" && request.DropAckAfter != "" {
		return errors.New("owner witness completed without dropping its selected exact ACK")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		return fmt.Errorf("read final owner witness RunJournal: %w", err)
	}
	if err := witness.summarizeJournal(journal, false); err != nil {
		return err
	}
	if err := witness.restoreStageMarkers(); err != nil {
		return err
	}
	witness.summary.Release.AnchorRetired = true
	witness.summary.Outcome = "completed"
	if request.Case == "stage_replay" && request.DropAckAfter == "" && witness.summary.HelperDropObserved &&
		(witness.summary.RecoveryState == "released" || witness.summary.RecoveryState == "pre_gate_released") {
		witness.summary.Outcome = "recovered"
	}
	if request.Case == "wsl2_denial" {
		if witness.summary.FailureCategory == "" || witness.summary.Health.Healthy || witness.summary.Health.PeerVerified {
			return errors.New("WSL2 denial witness did not observe an exact supported peer permission denial")
		}
		witness.summary.Outcome = "peer_denied"
	} else if witness.summary.FailureCategory != "" {
		return errors.New("positive native owner witness encountered a peer permission denial")
	}
	if err := witness.validateFinalSummary(); err != nil {
		return err
	}
	if err := store.Close(); err != nil {
		return fmt.Errorf("close owner witness Store before summary: %w", err)
	}
	storeOpen = false
	if err := witness.closeCapturedFDs(); err != nil {
		return err
	}
	return writeLinuxV2NativeWitnessSummary(output, witness.summary)
}

func (witness *linuxV2NativeWitnessRun) closeCapturedFDs() error {
	witness.mu.Lock()
	child := witness.childPIDFD
	peer := witness.peer
	witness.childPIDFD = nil
	witness.peer = nil
	witness.mu.Unlock()
	var result error
	if child != nil {
		result = errors.Join(result, child.Close())
	}
	if peer != nil {
		result = errors.Join(result, peer.Close())
	}
	return result
}

func (witness *linuxV2NativeWitnessRun) setFailureCategory(value string) {
	witness.mu.Lock()
	witness.summary.FailureCategory = value
	witness.mu.Unlock()
}

func (witness *linuxV2NativeWitnessRun) updateHealth(update func(*linuxV2NativeWitnessHealth)) {
	witness.mu.Lock()
	update(&witness.summary.Health)
	witness.mu.Unlock()
}

func (witness *linuxV2NativeWitnessRun) validateFinalSummary() error {
	if witness.request.Case == "stage_replay" {
		if !linuxV2NativeWitnessValidReleaseState(witness.summary.NativeReleaseState) {
			return errors.New("stage replay omitted its tri-state native RELEASE assessment")
		}
		if witness.summary.Stages.NativeReleased !=
			(witness.summary.NativeReleaseState == linuxV2NativeWitnessNativeReleaseReleased) {
			return errors.New("stage replay nativeReleased flag differs from the explicit tri-state")
		}
		if witness.summary.ResponseLostAfter != "" && witness.summary.FailureCategory == "response_lost" {
			return witness.validateStageReplayResponseLostSummary()
		}
		if witness.summary.HelperDropObserved {
			if !linuxV2NativeWitnessHelperDropSupported(witness.summary.HelperDropOperation) ||
				witness.summary.ResponseLostAfter != witness.summary.HelperDropOperation {
				return errors.New("stage replay helper-drop summary lacks its exact authenticated operation")
			}
		}
		switch witness.summary.RecoveryState {
		case "unresolved":
			if witness.summary.Outcome != "unresolved" || witness.summary.FailureCategory != "containment_unproven" ||
				!witness.summary.JournalUnproven || witness.summary.NoTargetAbort || witness.summary.Release.AnchorRetired ||
				witness.summary.Release.OpaquePlatformRelease || witness.summary.Release.ClearPersisted || witness.summary.Release.LeafRemoved ||
				witness.summary.Stop.ReceiptVersion != 0 || witness.summary.Stages.JournalCleared {
				return errors.New("stage replay unresolved outcome lacks retained journal/owner uncertainty")
			}
			return nil
		case "aborted_no_target":
			if witness.summary.Outcome != "aborted_no_target" || !witness.summary.NoTargetAbort || witness.summary.JournalUnproven ||
				witness.summary.NativeReleaseState != linuxV2NativeWitnessNativeReleaseNotReleased ||
				witness.summary.Release.OpaquePlatformRelease || witness.summary.Release.ClearPersisted {
				return errors.New("stage replay no-target abort differs from its explicit non-receipt tombstone")
			}
			return nil
		case "no_native_effects":
			if witness.summary.Outcome != "no_native_effects" || witness.summary.JournalUnproven || witness.summary.NoTargetAbort ||
				witness.summary.NativeReleaseState != linuxV2NativeWitnessNativeReleaseNotReleased ||
				witness.summary.Release.OpaquePlatformRelease || witness.summary.Release.ClearPersisted {
				return errors.New("stage replay no-native-effects outcome contains unexplained owner evidence")
			}
			return nil
		case "released", "pre_gate_released":
			if witness.summary.JournalUnproven || witness.summary.NoTargetAbort {
				return errors.New("stage replay released outcome carries unresolved or no-target authority")
			}
			return witness.validateStageReplayPhysicalStopSummary()
		default:
			return errors.New("stage replay outcome has an unknown recovery state")
		}
	}
	if err := witness.validatePhysicalStopSummary(); err != nil {
		return err
	}
	if witness.request.Case == "wsl2_denial" {
		if witness.summary.FailureCategory == "" || witness.summary.Health.Healthy || witness.summary.Health.PeerVerified {
			return errors.New("WSL2 denial witness did not observe an exact supported peer permission denial")
		}
		if witness.summary.Target.PID == 0 {
			return nil
		}
		if !witness.summary.Stages.InitialLeaseAcked || !witness.summary.Stages.CheckoutReady || !witness.summary.Stages.NativeReleased {
			return errors.New("WSL2 peer denial lacks the exact initial-lease/checkout/RELEASE observation")
		}
		return nil
	}
	if witness.summary.FailureCategory != "" {
		return errors.New("positive native owner witness encountered a peer permission denial")
	}
	if witness.summary.Harness == config.RuntimeHarnessPi && (witness.summary.ProviderAction.Outcome != state.ProviderActionOutcomeSucceeded || witness.summary.ProviderAction.CompletedCount != 1) {
		return errors.New("isolated Pi owner witness did not complete exactly one real brokered resource.sync")
	}
	if witness.summary.Harness == config.RuntimeHarnessOpenCode && (!witness.summary.Health.Healthy || !witness.summary.Health.PeerVerified) {
		return errors.New("isolated OpenCode owner witness did not complete an exact B peer-verified health request")
	}
	if witness.summary.Harness == config.RuntimeHarnessOpenCode && (!witness.summary.Health.NetworkNamespaceVerified ||
		witness.summary.Health.NetworkNamespace != witness.summary.Target.NetworkNamespace ||
		witness.summary.Health.PeerPID != witness.summary.Target.PID || witness.summary.Health.PeerIdentity != witness.summary.Target.Identity) {
		return errors.New("OpenCode health summary is not bound to the exact target PID and network namespace")
	}
	if witness.request.Case == "helper_sigkill_detached_child" || witness.request.Case == "daemon_sigkill_helper_survives_detached_child" {
		child := witness.summary.DetachedChild
		if child == nil || !child.PIDFDVerified || !child.LeafMembershipVerified {
			return errors.New("detached-child witness lacks exact pidfd and run-leaf membership evidence")
		}
		if witness.request.Case == "helper_sigkill_detached_child" && !child.AnchorReaped {
			return errors.New("helper-loss witness lacks exact anchor descendant-reap evidence")
		}
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) validateStageReplayResponseLostSummary() error {
	summary := witness.summary
	if summary.Outcome != "unresolved" || summary.RecoveryState != "unresolved" || !summary.JournalUnproven ||
		summary.FailureCategory != "response_lost" || summary.ResponseLostAfter == "" {
		return errors.New("StageReplay response-loss summary lacks its unresolved run outcome")
	}
	localDrop := witness.request.DropAckAfter != "" && witness.request.DropAckAfter == summary.ResponseLostAfter &&
		linuxV2NativeWitnessAckLossSupported(summary.ResponseLostAfter) && !summary.HelperDropObserved
	helperDrop := witness.request.DropAckAfter == "" && summary.HelperDropObserved &&
		summary.HelperDropOperation == summary.ResponseLostAfter &&
		linuxV2NativeWitnessHelperDropSupported(summary.ResponseLostAfter)
	if !localDrop && !helperDrop {
		return errors.New("StageReplay response-loss summary differs from its exact App/helper boundary")
	}
	if localDrop && !linuxV2NativeWitnessStageObserved(summary.Stages, summary.ResponseLostAfter) {
		return errors.New("StageReplay lost ACK is not preceded by its exact durable stage")
	}
	if summary.NoTargetAbort {
		if summary.ResponseLostAfter != linuxV2NativeWitnessHelperStage &&
			summary.ResponseLostAfter != linuxV2NativeWitnessLauncherStage &&
			summary.ResponseLostAfter != linuxV2NativeWitnessGateStage ||
			summary.Stop.ReceiptVersion != 0 || summary.Release.OpaquePlatformRelease ||
			summary.Release.ClearPersisted || summary.Stages.JournalCleared {
			return errors.New("response-loss no-target tombstone contains a target stop or opaque release")
		}
		return nil
	}
	switch summary.ResponseLostAfter {
	case linuxV2NativeWitnessStopReceiptStage, "ack-stop-receipt":
		if !linuxV2NativeWitnessStopSummaryHasPhysicalProof(summary.Stop) || !summary.Stages.StopReceiptDurable ||
			summary.Release.OpaquePlatformRelease ||
			summary.Release.LeafRemoved || summary.Release.ClearPersisted || summary.Stages.LeafRemoved ||
			summary.Stages.JournalCleared {
			return errors.New("lost stop-receipt ACK lacks the exact retained v2 receipt/pending owner")
		}
	case linuxV2NativeWitnessClearedStage:
		if !linuxV2NativeWitnessStopSummaryHasPhysicalProof(summary.Stop) || !summary.Stages.StopReceiptDurable ||
			summary.Release.Version != authority.SupervisorHandoffVersionV2 || !summary.Release.OpaquePlatformRelease ||
			!summary.Release.LeafRemoved || !summary.Release.ClearPersisted || summary.Release.AnchorRetired ||
			!summary.Stages.LeafRemoved || !summary.Stages.JournalCleared {
			return errors.New("lost journal-clear ACK lacks its receipt-backed v2 release tombstone")
		}
	case "launcher-ready":
		if !helperDrop || !linuxV2NativeWitnessStopSummaryHasPhysicalProof(summary.Stop) ||
			!summary.Stages.StopReceiptDurable || summary.Release.Version != authority.SupervisorHandoffVersionV2 ||
			!summary.Release.OpaquePlatformRelease || !summary.Release.LeafRemoved || !summary.Release.ClearPersisted ||
			summary.Release.AnchorRetired || !summary.Stages.LeafRemoved || !summary.Stages.JournalCleared {
			return errors.New("launcher-ready loss lacks its safely stopped v2 gate and retained release tombstone")
		}
		if summary.NativeReleaseState != linuxV2NativeWitnessNativeReleaseNotReleased || summary.Stages.NativeReleased ||
			summary.Target.PID != 0 || summary.Target.Identity != "" {
			return errors.New("launcher-ready loss fabricated native B RELEASE or target execution")
		}
	default:
		if summary.Release.OpaquePlatformRelease || summary.Release.LeafRemoved || summary.Release.ClearPersisted ||
			summary.Stages.JournalCleared {
			return errors.New("pre-clear lost ACK invented a receipt-backed release tombstone")
		}
	}
	return nil
}

func linuxV2NativeWitnessStopSummaryHasPhysicalProof(stop linuxV2NativeWitnessStop) bool {
	return stop.ReceiptVersion == authority.SupervisorVersionV2 && stop.ReceiptStatus == "stopped" && stop.ProofKind != "" &&
		stop.Issuer.PID > 0 && stop.Issuer.Identity != "" && stop.Reaper.PID > 0 && stop.Reaper.Identity != "" &&
		stop.KillAcknowledged && stop.PopulatedZero && stop.NoMigrationObserved && stop.ProducerFenced && stop.DescendantsReaped &&
		stop.LauncherExit.PID > 0 && stop.LauncherExit.Identity != "" && stop.LauncherExit.Waited && stop.LauncherExit.Reaped &&
		stop.GateExit.PID > 0 && stop.GateExit.Identity != "" && stop.GateExit.Waited && stop.GateExit.Reaped
}

func linuxV2NativeWitnessStageObserved(stages linuxV2NativeWitnessStages, stage string) bool {
	switch stage {
	case linuxV2NativeWitnessHelperStage:
		return stages.HelperBound
	case linuxV2NativeWitnessLauncherStage:
		return stages.LauncherBound
	case linuxV2NativeWitnessGateStage:
		return stages.GateBound
	case linuxV2NativeWitnessPrepareStage:
		return stages.HandoffPrepared
	case linuxV2NativeWitnessCommitStage:
		return stages.HandoffCommitted
	case linuxV2NativeWitnessLeaseStage:
		return stages.InitialLeaseAcked
	case linuxV2NativeWitnessCheckoutStage:
		return stages.CheckoutReady
	case linuxV2NativeWitnessReleaseStage:
		return stages.NativeReleased
	case linuxV2NativeWitnessStopReceiptStage:
		return stages.StopReceiptDurable
	case linuxV2NativeWitnessClearedStage:
		return stages.JournalCleared
	default:
		return false
	}
}

func (witness *linuxV2NativeWitnessRun) validatePhysicalStopSummary() error {
	if err := witness.validatePhysicalStopProof(); err != nil {
		return err
	}
	if witness.summary.Target.PID > 0 && witness.summary.NativeReleaseState != linuxV2NativeWitnessNativeReleaseReleased {
		return errors.New("owner witness target PID lacks positive same-PID B RELEASE evidence")
	}
	if witness.summary.Target.PID > 0 && (!witness.summary.Stages.ReservationDurable || !witness.summary.Stages.CreateStarted ||
		!witness.summary.Stages.LeafBound || !witness.summary.Stages.HelperBound || !witness.summary.Stages.LauncherBound ||
		!witness.summary.Stages.GateBound || !witness.summary.Stages.HandoffPrepared || !witness.summary.Stages.HandoffCommitted ||
		!witness.summary.Stages.InitialLeaseAcked || !witness.summary.Stages.CheckoutReady || !witness.summary.Stages.NativeReleased) {
		return errors.New("positive owner witness lacks a durable reserve/bind/commit/lease/checkout/RELEASE stage")
	}
	if witness.summary.Target.PID > 0 {
		return witness.validateNativeTargetSummary()
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) validateStageReplayPhysicalStopSummary() error {
	if err := witness.validatePhysicalStopProof(); err != nil {
		return err
	}
	switch witness.summary.NativeReleaseState {
	case linuxV2NativeWitnessNativeReleaseNotReleased:
		if witness.summary.Stages.NativeReleased || witness.summary.Target.PID != 0 || witness.summary.Target.Identity != "" {
			return errors.New("independently held pre-RELEASE stage contains native B target evidence")
		}
		return nil
	case linuxV2NativeWitnessNativeReleaseUnknown:
		if witness.summary.Stages.NativeReleased || linuxV2NativeWitnessTargetHasBPeerEvidence(witness.summary.Target) {
			return errors.New("unknown native RELEASE state contains positive B peer evidence")
		}
		return nil
	case linuxV2NativeWitnessNativeReleaseReleased:
		if !witness.summary.Stages.NativeReleased {
			return errors.New("released native state lacks a durable release/later-stage marker")
		}
	default:
		return errors.New("stage replay native RELEASE state is invalid")
	}
	if !witness.summary.Stages.ReservationDurable || !witness.summary.Stages.CreateStarted ||
		!witness.summary.Stages.LeafBound || !witness.summary.Stages.HelperBound || !witness.summary.Stages.LauncherBound ||
		!witness.summary.Stages.GateBound || !witness.summary.Stages.HandoffPrepared || !witness.summary.Stages.HandoffCommitted ||
		!witness.summary.Stages.InitialLeaseAcked || !witness.summary.Stages.CheckoutReady {
		return errors.New("released stage replay lacks its exact durable reserve/bind/commit/lease/checkout history")
	}
	return witness.validateNativeTargetSummary()
}

func (witness *linuxV2NativeWitnessRun) validatePhysicalStopProof() error {
	if !witness.summary.Release.OpaquePlatformRelease || !witness.summary.Release.LeafRemoved ||
		!witness.summary.Release.ClearPersisted || witness.summary.Stop.ReceiptVersion != authority.SupervisorHandoffVersionV2 ||
		!witness.summary.Stop.KillAcknowledged || !witness.summary.Stop.PopulatedZero ||
		!witness.summary.Stop.NoMigrationObserved || !witness.summary.Stop.ProducerFenced ||
		!witness.summary.Stop.DescendantsReaped || !witness.summary.Release.AnchorRetired {
		return errors.New("owner witness did not prove exact v2 physical stop, opaque removal, retirement, and durable clear")
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) validateNativeTargetSummary() error {
	if witness.summary.Target.PID <= 0 || witness.summary.Target.Identity == "" || !witness.summary.Target.PIDFDVerified ||
		!witness.summary.Target.SamePIDExec || witness.summary.Target.UserNamespace.Device == 0 || witness.summary.Target.UserNamespace.Inode == 0 ||
		witness.summary.Target.NetworkNamespace.Device == 0 || witness.summary.Target.NetworkNamespace.Inode == 0 ||
		witness.summary.Owners.Gate.PID != witness.summary.Target.PID || witness.summary.Owners.Gate.Identity != witness.summary.Target.Identity {
		return errors.New("positive owner witness lacks the exact same-PID B pidfd/namespace/leaf target evidence")
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) normalizeStageReplayTarget() {
	if witness.request.Case != "stage_replay" {
		return
	}
	switch witness.summary.NativeReleaseState {
	case linuxV2NativeWitnessNativeReleaseNotReleased:
		witness.summary.Target = linuxV2NativeWitnessTarget{}
	case linuxV2NativeWitnessNativeReleaseReleased:
		// Keep the positive peer evidence copied from its durable stage marker.
	case linuxV2NativeWitnessNativeReleaseUnknown:
		// A journal PID may be the same A gate before or after exec; never erase
		// it just because the post-Runner marker is absent.
	}
}

func linuxV2NativeWitnessTargetHasBPeerEvidence(target linuxV2NativeWitnessTarget) bool {
	return target.PIDFDVerified || target.HostUID != 0 || target.UserNamespace.Device != 0 || target.UserNamespace.Inode != 0 ||
		target.NetworkNamespace.Device != 0 || target.NetworkNamespace.Inode != 0 || target.SamePIDExec
}

type linuxV2NativeWitnessRequestWire struct {
	Version        int                         `json:"version"`
	Case           string                      `json:"case"`
	Harness        string                      `json:"harness"`
	RunKey         linuxV2NativeWitnessRunKey  `json:"run_key"`
	ReplayRequired bool                        `json:"replay_required,omitempty"`
	Fence          protocol.Fence              `json:"fence"`
	Claim          json.RawMessage             `json:"claim"`
	Subject        protocol.Subject            `json:"subject"`
	Barrier        linuxV2NativeWitnessBarrier `json:"barrier"`
	DropAckAfter   string                      `json:"drop_ack_after,omitempty"`
}

type linuxV2NativeWitnessWorkWire struct {
	Goal                 string          `json:"goal"`
	AgentProfile         string          `json:"agent_profile"`
	Workspace            string          `json:"workspace"`
	Input                json.RawMessage `json:"input,omitempty"`
	RequiredCapabilities map[string]bool `json:"required_capabilities,omitempty"`
}

type linuxV2NativeWitnessProviderGrantWire struct {
	ResourceID string   `json:"resource_id"`
	Provider   string   `json:"provider"`
	Kind       string   `json:"kind"`
	Operations []string `json:"operations"`
}

type linuxV2NativeWitnessProviderAccessWire struct {
	Path   string                                  `json:"path"`
	Token  string                                  `json:"token"`
	Grants []linuxV2NativeWitnessProviderGrantWire `json:"grants"`
}

type linuxV2NativeWitnessClaimWire struct {
	RunID            string                       `json:"run_id"`
	TaskID           string                       `json:"task_id"`
	Generation       int64                        `json:"generation"`
	HarnessSessionID *string                      `json:"harness_session_id"`
	HarnessBindingID *string                      `json:"harness_binding_id"`
	ClaimID          string                       `json:"claim_id"`
	LeaseToken       string                       `json:"lease_token"`
	LeaseExpiresAt   time.Time                    `json:"lease_expires_at"`
	LeaseRemainingMS int64                        `json:"lease_remaining_ms"`
	Work             linuxV2NativeWitnessWorkWire `json:"work"`
	ProviderAccess   json.RawMessage              `json:"provider_access,omitempty"`
}

func decodeLinuxV2NativeWitnessConfig(contents []byte) (config.Config, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var value config.Config
	if err := decoder.Decode(&value); err != nil {
		return config.Config{}, fmt.Errorf("decode strict owner witness config: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return config.Config{}, err
	}
	if err := value.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("validate strict owner witness config: %w", err)
	}
	return value, nil
}

func decodeLinuxV2NativeWitnessRequest(contents []byte) (linuxV2NativeWitnessRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var wire linuxV2NativeWitnessRequestWire
	if err := decoder.Decode(&wire); err != nil {
		return linuxV2NativeWitnessRequest{}, fmt.Errorf("decode strict owner witness request: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return linuxV2NativeWitnessRequest{}, err
	}
	claim, err := decodeLinuxV2NativeWitnessClaim(wire.Claim)
	if err != nil {
		return linuxV2NativeWitnessRequest{}, err
	}
	return linuxV2NativeWitnessRequest{
		Version: wire.Version, Case: wire.Case, Harness: wire.Harness,
		RunKey: wire.RunKey, ReplayRequired: wire.ReplayRequired, Fence: wire.Fence, Claim: claim,
		Subject: wire.Subject, Barrier: wire.Barrier, DropAckAfter: wire.DropAckAfter,
	}, nil
}

func decodeLinuxV2NativeWitnessClaim(contents json.RawMessage) (protocol.ClaimResponse, error) {
	if len(contents) == 0 || !json.Valid(contents) {
		return protocol.ClaimResponse{}, errors.New("owner witness claim is missing or malformed")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var wire linuxV2NativeWitnessClaimWire
	if err := decoder.Decode(&wire); err != nil {
		return protocol.ClaimResponse{}, fmt.Errorf("decode authentic Control ClaimResponse fixture: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return protocol.ClaimResponse{}, err
	}
	var access *protocol.ProviderAccess
	if len(wire.ProviderAccess) > 0 {
		if bytes.Equal(bytes.TrimSpace(wire.ProviderAccess), []byte("null")) {
			return protocol.ClaimResponse{}, errors.New("Control ClaimResponse contains null provider_access")
		}
		accessDecoder := json.NewDecoder(bytes.NewReader(wire.ProviderAccess))
		accessDecoder.DisallowUnknownFields()
		var decoded linuxV2NativeWitnessProviderAccessWire
		if err := accessDecoder.Decode(&decoded); err != nil {
			return protocol.ClaimResponse{}, fmt.Errorf("decode claim-scoped provider access: %w", err)
		}
		if err := requireJSONEOF(accessDecoder); err != nil {
			return protocol.ClaimResponse{}, err
		}
		access = &protocol.ProviderAccess{Path: decoded.Path, Token: decoded.Token, Grants: make([]protocol.ProviderGrant, len(decoded.Grants))}
		for index, grant := range decoded.Grants {
			access.Grants[index] = protocol.ProviderGrant{
				ResourceID: grant.ResourceID, Provider: grant.Provider, Kind: grant.Kind,
				Operations: append([]string(nil), grant.Operations...),
			}
		}
	}
	work := protocol.Work{
		Goal: wire.Work.Goal, AgentProfile: wire.Work.AgentProfile,
		Workspace: wire.Work.Workspace, Input: append(json.RawMessage(nil), wire.Work.Input...),
	}
	if wire.Work.RequiredCapabilities != nil {
		work.RequiredCapabilities = make(map[string]bool, len(wire.Work.RequiredCapabilities))
		for name, enabled := range wire.Work.RequiredCapabilities {
			work.RequiredCapabilities[name] = enabled
		}
	}
	return protocol.ClaimResponse{
		RunID: wire.RunID, TaskID: wire.TaskID, Generation: wire.Generation,
		HarnessSessionID: wire.HarnessSessionID, HarnessBindingID: wire.HarnessBindingID,
		ClaimID: wire.ClaimID, LeaseToken: wire.LeaseToken, LeaseExpiresAt: wire.LeaseExpiresAt,
		LeaseRemainingMS: wire.LeaseRemainingMS, Work: work, ProviderAccess: access,
	}, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("fixture contains trailing JSON")
		}
		return errors.New("fixture contains trailing data")
	}
	return nil
}

func validateLinuxV2NativeWitnessPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 {
		return errors.New("fixture path must be a canonical absolute path")
	}
	relative, err := filepath.Rel(linuxV2NativeOwnerWitnessRoot, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("fixture path is outside /opt/symmetry-owner-test")
	}
	return nil
}

func validateLinuxV2NativeWitnessStateDirectoryPath(path string, owner linuxgate.OwnerConfig) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 {
		return errors.New("owner witness StateDir must be a canonical absolute path")
	}
	if path != owner.StateDir {
		return errors.New("owner witness StateDir must exactly match the root-installed OwnerConfig.StateDir")
	}
	if path == linuxV2NativeOwnerWitnessStateDir {
		return nil
	}
	return validateLinuxV2NativeWitnessPath(path)
}

func readLinuxV2NativeWitnessFile(path string, owner linuxgate.OwnerConfig, denyBRead bool) ([]byte, error) {
	if err := validateLinuxV2NativeWitnessPath(path); err != nil {
		return nil, err
	}
	if err := verifyLinuxWitnessPathAncestors(path, owner.AUID); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("open root-controlled fixture file")
	}
	file := os.NewFile(uintptr(fd), "linux-owner-witness-fixture")
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("wrap root-controlled fixture file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, errors.New("stat root-controlled fixture file")
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || !info.Mode().IsRegular() || identity.uid != 0 || identity.mode&0022 != 0 || info.Size() < 0 || info.Size() > linuxV2NativeWitnessRequestMax {
		return nil, errors.New("fixture file must be a bounded root-owned non-writable regular file")
	}
	if denyBRead {
		bAccount, lookupErr := user.LookupId(strconv.FormatUint(uint64(owner.BUID), 10))
		if lookupErr != nil {
			return nil, errors.New("lookup fixed B account for private request validation")
		}
		bSubject, subjectErr := linuxACLSubjectForAccount(bAccount)
		if subjectErr != nil || verifyEffectiveLinuxAccess(path, bSubject, 0, 7) != nil {
			return nil, errors.New("root-controlled claim/request fixture must be unreadable and unwritable by B")
		}
	}
	contents, err := io.ReadAll(io.LimitReader(file, linuxV2NativeWitnessRequestMax+1))
	if err != nil || len(contents) > linuxV2NativeWitnessRequestMax {
		return nil, errors.New("read bounded root-controlled fixture file")
	}
	return contents, nil
}

type linuxV2NativeWitnessBarriers struct {
	request  linuxV2NativeWitnessBarrier
	caseName string
	runKey   linuxV2NativeWitnessRunKey
	aUID     uint32
	mu       sync.Mutex
	held     bool
}

var linuxV2NativeWitnessRunnerEnvironment sync.Mutex

func newLinuxV2NativeWitnessBarriers(request linuxV2NativeWitnessRequest, owner linuxgate.OwnerConfig) (*linuxV2NativeWitnessBarriers, error) {
	path := request.Barrier.Directory
	if err := validateLinuxV2NativeWitnessPath(path); err != nil {
		return nil, fmt.Errorf("validate witness barrier directory: %w", err)
	}
	if err := verifyLinuxWitnessPathAncestors(filepath.Join(path, ".stage"), owner.AUID); err != nil {
		return nil, fmt.Errorf("verify owner witness barrier parent chain: %w", err)
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return nil, fmt.Errorf("inspect witness barrier directory: %w", err)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || !info.IsDir() || identity.uid != uint32(os.Geteuid()) || identity.mode&07777 != 0o700 {
		return nil, errors.New("witness barrier directory must be A-owned mode 0700")
	}
	return &linuxV2NativeWitnessBarriers{
		request: request.Barrier, caseName: request.Case, runKey: request.RunKey, aUID: owner.AUID,
	}, nil
}

func (barrier *linuxV2NativeWitnessBarriers) ensureFresh() error {
	entries, err := os.ReadDir(barrier.request.Directory)
	if err != nil {
		return fmt.Errorf("list fresh witness barrier directory: %w", err)
	}
	if len(entries) != 0 {
		return errors.New("fresh witness barrier directory must be empty")
	}
	if barrier.request.ReleaseFile != "" {
		if _, err := os.Lstat(barrier.request.ReleaseFile); !errors.Is(err, os.ErrNotExist) {
			return errors.New("fresh witness release marker already exists or cannot be inspected")
		}
	}
	return nil
}

func (barrier *linuxV2NativeWitnessBarriers) stage(ctx context.Context, marker linuxV2NativeWitnessStageMarker, hold bool) error {
	if ctx == nil || marker.Stage == "" || marker.Case != barrier.caseName || marker.RunKey != barrier.runKey {
		return errors.New("witness stage marker does not match its exact case and RunKey")
	}
	encoded, err := json.Marshal(marker)
	if err != nil {
		return errors.New("encode redacted owner witness stage marker")
	}
	path := filepath.Join(barrier.request.Directory, marker.Stage+".json")
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if errors.Is(err, unix.EEXIST) {
		previous, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(previous, encoded) {
			return errors.New("existing owner witness stage marker differs from exact replay")
		}
	} else if err != nil {
		return fmt.Errorf("create redacted owner witness stage marker: %w", err)
	} else {
		file := os.NewFile(uintptr(fd), "owner-witness-stage")
		if file == nil {
			_ = unix.Close(fd)
			return errors.New("wrap owner witness stage marker")
		}
		if _, err := file.Write(encoded); err != nil {
			_ = file.Close()
			return fmt.Errorf("write redacted owner witness stage marker: %w", err)
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return fmt.Errorf("sync redacted owner witness stage marker: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close redacted owner witness stage marker: %w", err)
		}
		if err := syncLinuxV2NativeWitnessDirectory(barrier.request.Directory); err != nil {
			return err
		}
	}
	if !hold || barrier.request.HoldAfter != marker.Stage {
		return nil
	}
	barrier.mu.Lock()
	if barrier.held {
		barrier.mu.Unlock()
		return nil
	}
	barrier.held = true
	barrier.mu.Unlock()
	return barrier.waitRelease(ctx)
}

func (barrier *linuxV2NativeWitnessBarriers) waitRelease(ctx context.Context) error {
	if barrier.request.ReleaseFile == "" {
		return errors.New("held witness stage has no release file")
	}
	timer := time.NewTimer(time.Duration(barrier.request.TimeoutSeconds) * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(linuxV2NativeOwnerWitnessPoll)
	defer ticker.Stop()
	for {
		fd, err := unix.Open(barrier.request.ReleaseFile, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err == nil {
			file := os.NewFile(uintptr(fd), "owner-witness-release")
			if file == nil {
				_ = unix.Close(fd)
				return errors.New("wrap owner witness release marker")
			}
			contents, readErr := io.ReadAll(io.LimitReader(file, 16))
			stat, statErr := file.Stat()
			closeErr := file.Close()
			if readErr != nil || statErr != nil || closeErr != nil || stat == nil || !stat.Mode().IsRegular() || string(contents) != "release\n" {
				return errors.New("witness release marker is not the exact bounded root-controlled signal")
			}
			identity, identityErr := linuxFileIdentity(stat)
			if identityErr != nil || (identity.uid != 0 && identity.uid != barrier.aUID) ||
				identity.mode&0022 != 0 || string(contents) != "release\n" {
				return errors.New("witness release marker is not the exact bounded root-controlled signal")
			}
			return nil
		}
		if !errors.Is(err, unix.ENOENT) {
			return errors.New("inspect witness release marker")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("owner witness barrier cancelled: %w", ctx.Err())
		case <-timer.C:
			return errors.New("owner witness release marker did not arrive before its bounded timeout")
		case <-ticker.C:
		}
	}
}

func syncLinuxV2NativeWitnessDirectory(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open witness stage directory for sync: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.Fsync(fd); err != nil {
		return fmt.Errorf("sync witness stage directory: %w", err)
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) summarizeLiveJournal(journal state.RunJournal) error {
	key := state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation}
	if journal.Key() != key || !linuxV2JournalMatchesFence(journal, key, witness.request.Fence) {
		return errors.New("owner witness stage readback differs from the exact RunKey/fence")
	}
	witness.mu.Lock()
	defer witness.mu.Unlock()
	witness.fillSummaryFromJournal(journal)
	return nil
}

func (witness *linuxV2NativeWitnessRun) summarizeJournal(journal state.RunJournal, recovered bool) error {
	key := state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation}
	if journal.Key() != key || !linuxV2JournalMatchesFence(journal, key, witness.request.Fence) {
		return errors.New("final owner witness journal differs from the exact RunKey/fence")
	}
	witness.mu.Lock()
	defer witness.mu.Unlock()
	witness.fillSummaryFromJournal(journal)
	if witness.summary.Release.ClearPersisted && !journal.HasProcessDetails() && journal.ContainmentCgroup == nil &&
		journal.ContainmentHandoff == nil && journal.ContainmentAuthority == nil {
		witness.summary.Release.AnchorRetired = recovered || witness.summary.Release.AnchorRetired
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) summarizeResponseLost(store *state.Store, after string) error {
	if witness == nil || store == nil || witness.request.Case != "stage_replay" {
		return errors.New("response-loss summary requires an exact StageReplay Store")
	}
	witness.mu.Lock()
	helperDropObserved := witness.summary.HelperDropObserved
	helperDropOperation := witness.summary.HelperDropOperation
	witness.mu.Unlock()
	localDrop := witness.request.DropAckAfter != "" && witness.request.DropAckAfter == after &&
		linuxV2NativeWitnessAckLossSupported(after) && !helperDropObserved
	helperDrop := witness.request.DropAckAfter == "" && helperDropObserved &&
		helperDropOperation == after && linuxV2NativeWitnessHelperDropSupported(after)
	if !localDrop && !helperDrop {
		return errors.New("response-loss summary does not match an exact selected App or helper response")
	}
	key := state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation}
	journal, err := store.LoadJournal(key)
	if err != nil || !linuxV2JournalMatchesFence(journal, key, witness.request.Fence) {
		return errors.Join(errors.New("read exact RunJournal for response-loss summary"), err)
	}
	if err := witness.summarizeJournal(journal, false); err != nil {
		return err
	}
	witness.summary.Outcome = "unresolved"
	witness.summary.RecoveryState = "unresolved"
	witness.summary.JournalUnproven = true
	witness.summary.FailureCategory = "response_lost"
	witness.summary.ResponseLostAfter = after
	if err := witness.restoreStageMarkers(); err != nil {
		return err
	}
	witness.normalizeStageReplayTarget()
	return nil
}

func (witness *linuxV2NativeWitnessRun) fillSummaryFromJournal(journal state.RunJournal) {
	summary := &witness.summary
	if journal.ContainmentCgroup != nil {
		reservation := journal.ContainmentCgroup
		summary.Stages.ReservationDurable = true
		summary.Stages.CreateStarted = reservation.CreateStarted
		summary.Owners.Anchor = linuxV2NativeWitnessProcess{PID: reservation.Anchor.PID, Identity: reservation.Anchor.Identity}
		if reservation.Helper != nil {
			summary.Owners.Helper = linuxV2NativeWitnessProcess{PID: reservation.Helper.PID, Identity: reservation.Helper.Identity}
			summary.Stages.HelperBound = true
		}
		if reservation.Launcher != nil {
			summary.Owners.Launcher = linuxV2NativeWitnessProcess{PID: reservation.Launcher.PID, Identity: reservation.Launcher.Identity}
			summary.Stages.LauncherBound = true
		}
		if reservation.Gate != nil {
			summary.Owners.Gate = linuxV2NativeWitnessProcess{PID: reservation.Gate.Process.PID, Identity: reservation.Gate.Process.Identity}
			summary.Stages.GateBound = true
		}
		if reservation.LeafIdentity != nil {
			summary.Leaf = linuxV2NativeWitnessLeafFromIdentity(*reservation.LeafIdentity)
			summary.Stages.LeafBound = true
		}
	}
	if journal.ContainmentHandoff != nil {
		handoff := journal.ContainmentHandoff
		summary.Stages.HandoffPrepared = true
		summary.Owners.Helper = linuxV2NativeWitnessProcess{PID: handoff.SupervisorPID, Identity: handoff.SupervisorIdentity}
		summary.Owners.Anchor = linuxV2NativeWitnessProcess{PID: handoff.AnchorPID, Identity: handoff.AnchorIdentity}
		summary.Owners.Launcher = linuxV2NativeWitnessProcess{PID: handoff.LauncherPID, Identity: handoff.LauncherIdentity}
		summary.Owners.Gate = linuxV2NativeWitnessProcess{PID: handoff.TargetPID, Identity: handoff.TargetIdentity}
		summary.Target.PID, summary.Target.Identity = handoff.TargetPID, handoff.TargetIdentity
		if handoff.CgroupIdentity != nil {
			summary.Leaf = linuxV2NativeWitnessLeafFromIdentity(*handoff.CgroupIdentity)
		}
	}
	if journal.ContainmentAuthority != nil {
		owner := journal.ContainmentAuthority
		summary.Stages.HandoffCommitted = owner.Version == authority.SupervisorVersionV2
		summary.Owners.Helper = linuxV2NativeWitnessProcess{PID: owner.SupervisorPID, Identity: owner.SupervisorIdentity}
		summary.Owners.Anchor = linuxV2NativeWitnessProcess{PID: owner.AnchorPID, Identity: owner.AnchorIdentity}
		summary.Owners.Launcher = linuxV2NativeWitnessProcess{PID: owner.LauncherPID, Identity: owner.LauncherIdentity}
		summary.Owners.Gate = linuxV2NativeWitnessProcess{PID: owner.TargetPID, Identity: owner.TargetIdentity}
		summary.Target.PID, summary.Target.Identity = owner.TargetPID, owner.TargetIdentity
		if owner.CgroupIdentity != nil {
			summary.Leaf = linuxV2NativeWitnessLeafFromIdentity(*owner.CgroupIdentity)
		}
		if owner.StopReceipt != nil {
			linuxV2NativeWitnessSummarizeStop(&summary.Stop, *owner.StopReceipt)
			if owner.StopReceipt.LinuxProof != nil {
				summary.Owners.Reaper = linuxV2NativeWitnessProcess{
					PID: owner.StopReceipt.LinuxProof.Reaper.PID, Identity: owner.StopReceipt.LinuxProof.Reaper.Identity,
				}
			}
		}
	}
	if journal.ContainmentCgroupReleaseProof != nil {
		proof := journal.ContainmentCgroupReleaseProof
		summary.Stages.ReservationDurable = true
		summary.Stages.CreateStarted = true
		summary.Stages.LeafBound = proof.CgroupIdentity != nil
		summary.Stages.HelperBound = proof.SupervisorPID > 0
		summary.Stages.LauncherBound = proof.LauncherPID > 0
		summary.Stages.GateBound = proof.TargetPID > 0
		summary.Stages.HandoffPrepared = true
		summary.Target.PID, summary.Target.Identity = proof.TargetPID, proof.TargetIdentity
		summary.Owners.Anchor = linuxV2NativeWitnessProcess{PID: proof.AnchorPID, Identity: proof.AnchorIdentity}
		summary.Owners.Helper = linuxV2NativeWitnessProcess{PID: proof.SupervisorPID, Identity: proof.SupervisorIdentity}
		summary.Owners.Launcher = linuxV2NativeWitnessProcess{PID: proof.LauncherPID, Identity: proof.LauncherIdentity}
		summary.Owners.Gate = linuxV2NativeWitnessProcess{PID: proof.TargetPID, Identity: proof.TargetIdentity}
		if proof.CgroupIdentity != nil {
			summary.Leaf = linuxV2NativeWitnessLeafFromIdentity(*proof.CgroupIdentity)
		}
		removed := proof.LeafDisposition == authority.LinuxCgroupLeafRemoved || proof.LeafDisposition == authority.LinuxCgroupLeafAbsentAfterReceipt
		summary.Leaf.Removed = removed
		summary.Release = linuxV2NativeWitnessRelease{
			Version: proof.Version, LeafDisposition: proof.LeafDisposition,
			OpaquePlatformRelease: proof.Validate() == nil,
			LeafRemoved:           removed, ClearPersisted: true,
		}
		if proof.StopReceipt != nil {
			linuxV2NativeWitnessSummarizeStop(&summary.Stop, *proof.StopReceipt)
			if proof.StopReceipt.LinuxProof != nil {
				summary.Owners.Reaper = linuxV2NativeWitnessProcess{
					PID: proof.StopReceipt.LinuxProof.Reaper.PID, Identity: proof.StopReceipt.LinuxProof.Reaper.Identity,
				}
			}
		}
		if proof.LauncherExit != nil {
			summary.Stop.LauncherExit = linuxV2NativeWitnessExit{
				PID: proof.LauncherExit.Process.PID, Identity: proof.LauncherExit.Process.Identity,
				Waited: proof.LauncherExit.Waited, Reaped: proof.LauncherExit.Reaped,
			}
		}
		summary.Stages.StopReceiptDurable = proof.StopReceipt != nil
		summary.Stages.LeafRemoved = removed
		summary.Stages.JournalCleared = !journal.HasProcessDetails() && journal.ContainmentCgroup == nil &&
			journal.ContainmentAuthority == nil && journal.ContainmentHandoff == nil
		summary.Release.ClearPersisted = summary.Stages.JournalCleared
	}
	if journal.ContainmentCgroupPreGateReleaseProof != nil {
		proof := journal.ContainmentCgroupPreGateReleaseProof
		receipt := proof.Receipt
		physical := receipt.Proof
		summary.Stages.ReservationDurable = true
		summary.Stages.CreateStarted = true
		summary.Stages.LeafBound = true
		summary.Stages.HelperBound = true
		summary.Stages.LauncherBound = true
		summary.Stages.StopReceiptDurable = true
		summary.Stages.LeafRemoved = proof.LeafDisposition == authority.LinuxCgroupLeafRemoved ||
			proof.LeafDisposition == authority.LinuxCgroupLeafAbsentAfterReceipt
		summary.Stages.JournalCleared = journal.ContainmentCgroup == nil && !journal.HasProcessDetails() &&
			journal.ContainmentHandoff == nil && journal.ContainmentAuthority == nil
		summary.Owners.Anchor = linuxV2NativeWitnessProcess{PID: receipt.Anchor.PID, Identity: receipt.Anchor.Identity}
		summary.Owners.Helper = linuxV2NativeWitnessProcess{PID: receipt.Helper.PID, Identity: receipt.Helper.Identity}
		summary.Owners.Launcher = linuxV2NativeWitnessProcess{PID: receipt.Launcher.PID, Identity: receipt.Launcher.Identity}
		summary.Owners.Reaper = linuxV2NativeWitnessProcess{PID: physical.Reaper.PID, Identity: physical.Reaper.Identity}
		summary.Leaf = linuxV2NativeWitnessLeafFromIdentity(receipt.CgroupIdentity)
		summary.Leaf.Removed = summary.Stages.LeafRemoved
		summary.Stop = linuxV2NativeWitnessStop{
			ReceiptVersion: receipt.Version, ReceiptStatus: "pre_gate_stopped", ProofKind: physical.Kind,
			Issuer:           linuxV2NativeWitnessProcess{PID: physical.Issuer.PID, Identity: physical.Issuer.Identity},
			Reaper:           linuxV2NativeWitnessProcess{PID: physical.Reaper.PID, Identity: physical.Reaper.Identity},
			KillAcknowledged: physical.CgroupKillAcknowledged, PopulatedZero: physical.CgroupPopulatedZero,
			NoMigrationObserved: physical.NoMigrationObserved, ProducerFenced: physical.ProducerFenced,
			DescendantsReaped: physical.DescendantsReaped,
			LauncherExit: linuxV2NativeWitnessExit{
				PID: physical.LauncherExit.Process.PID, Identity: physical.LauncherExit.Process.Identity,
				Waited: physical.LauncherExit.Waited, Reaped: physical.LauncherExit.Reaped,
			},
		}
		if physical.HelperExit != nil {
			exit := linuxV2NativeWitnessExit{
				PID: physical.HelperExit.Process.PID, Identity: physical.HelperExit.Process.Identity,
				Waited: physical.HelperExit.Waited, Reaped: physical.HelperExit.Reaped,
			}
			summary.Stop.HelperExit = &exit
		}
		summary.Release = linuxV2NativeWitnessRelease{
			Version: proof.Version, LeafDisposition: proof.LeafDisposition,
			OpaquePlatformRelease: proof.Validate() == nil,
			LeafRemoved:           summary.Stages.LeafRemoved, ClearPersisted: summary.Stages.JournalCleared,
		}
	}
	if journal.ContainmentCgroup != nil && journal.ContainmentCgroup.AbortProof != nil && !journal.LinuxCloneAbortPending {
		proof := journal.ContainmentCgroup.AbortProof
		summary.NoTargetAbort = true
		summary.Release.Version = proof.Version
		summary.Release.LeafDisposition = proof.LeafDisposition
		summary.Release.LeafRemoved = proof.LeafDisposition == authority.LinuxCgroupAbortLeafRemovedEmpty ||
			proof.LeafDisposition == authority.LinuxCgroupAbortLeafAbsentAfterRmdir
	}
	if journal.LinuxCloneAbortProof != nil && !journal.LinuxCloneAbortPending && journal.ContainmentCgroupReleaseProof == nil &&
		journal.ContainmentCgroupPreGateReleaseProof == nil {
		summary.NoTargetAbort = true
		if abort := journal.LinuxCloneAbortProof.CgroupAbortProof; abort != nil {
			summary.Release.Version = abort.Version
			summary.Release.LeafDisposition = abort.LeafDisposition
			summary.Release.LeafRemoved = abort.LeafDisposition == authority.LinuxCgroupAbortLeafRemovedEmpty ||
				abort.LeafDisposition == authority.LinuxCgroupAbortLeafAbsentAfterRmdir
		}
	}
	if journal.LinuxCloneIntent != nil {
		encoded, _ := json.Marshal(journal.LinuxCloneIntent)
		digest := sha256.Sum256(encoded)
		summary.Clone.IntentSHA256 = hex.EncodeToString(digest[:])
	}
	if journal.LinuxCloneSeedReceipt != nil {
		summary.Clone.SeedDigest = journal.LinuxCloneSeedReceipt.Digest
	}
	if journal.LinuxCloneCheckoutReceipt != nil {
		receipt := journal.LinuxCloneCheckoutReceipt
		summary.Clone.CheckoutReady = true
		summary.Clone.CheckoutDevice = receipt.TargetDevice
		summary.Clone.CheckoutInode = receipt.TargetInode
		summary.Stages.CheckoutReady = true
	}
	if len(journal.ProviderActionIntents) > 0 {
		summary.ProviderAction.Attempted = true
		summary.ProviderAction.CompletedCount = 0
		summary.ProviderAction.Outcome = ""
		for _, intent := range journal.ProviderActionIntents {
			if intent.Operation != "resource.sync" || intent.ResourceID != witness.request.Subject.ResourceID {
				summary.ProviderAction.Outcome = "unexpected"
				continue
			}
			if intent.Outcome == state.ProviderActionOutcomeSucceeded {
				summary.ProviderAction.CompletedCount++
				summary.ProviderAction.Outcome = state.ProviderActionOutcomeSucceeded
			} else if summary.ProviderAction.Outcome == "" || intent.Outcome == state.ProviderActionOutcomeUnknown {
				summary.ProviderAction.Outcome = intent.Outcome
			}
		}
	}
	if witness.peer != nil {
		summary.Target.PID = witness.peer.PID
		summary.Target.Identity = witness.peer.Identity
		summary.Target.PIDFDVerified = witness.peer.PIDFD != nil
		summary.Target.UserNamespace = linuxV2NativeWitnessNamespace{
			Device: witness.peer.UserNamespace.Device, Inode: witness.peer.UserNamespace.Inode,
		}
		summary.Target.HostUID = witness.ownerConfig.BUID
		summary.Target.SamePIDExec = summary.Target.PID > 0 && summary.Target.PID == summary.Owners.Gate.PID &&
			summary.Target.Identity == summary.Owners.Gate.Identity
	}
	switch {
	case summary.Release.ClearPersisted:
		if journal.ContainmentCgroupPreGateReleaseProof != nil {
			summary.RecoveryState = "pre_gate_released"
		} else {
			summary.RecoveryState = "released"
		}
	case summary.NoTargetAbort:
		summary.RecoveryState = "aborted_no_target"
	case journal.LinuxCloneIntent == nil && journal.ContainmentCgroup == nil && journal.ContainmentHandoff == nil &&
		journal.ContainmentAuthority == nil && !journal.HasProcessDetails() && !journal.ContainmentUnproven:
		summary.RecoveryState = "no_native_effects"
	case hasPendingLinuxV2Containment(journal) || journal.ContainmentUnproven || journal.LinuxCloneAbortPending ||
		journal.LinuxCloneIntent != nil && journal.LinuxCloneAbortProof == nil && journal.LinuxClonePromotionReceipt == nil:
		summary.RecoveryState = "unresolved"
		summary.JournalUnproven = true
	}
	if witness.peer != nil {
		summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseReleased
	} else if summary.NativeReleaseState == linuxV2NativeWitnessNativeReleaseUnknown {
		switch {
		case summary.NoTargetAbort || journal.ContainmentCgroupPreGateReleaseProof != nil:
			summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseNotReleased
		case journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Version == authority.SupervisorHandoffVersionV2 &&
			journal.ContainmentAuthority == nil:
			summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseNotReleased
		case journal.ContainmentCgroup != nil && journal.ContainmentHandoff == nil && journal.ContainmentAuthority == nil:
			summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseNotReleased
		case summary.RecoveryState == "no_native_effects":
			summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseNotReleased
		}
	}
	if witness.child != nil {
		copy := *witness.child
		if summary.Stop.DescendantsReaped && summary.Owners.Reaper.PID == summary.Owners.Anchor.PID &&
			summary.Owners.Reaper.Identity == summary.Owners.Anchor.Identity {
			copy.AnchorReaped = true
		}
		summary.DetachedChild = &copy
	}
	witness.stages = mergeLinuxV2NativeWitnessStages(witness.stages, summary.Stages)
	summary.Stages = witness.stages
}

func mergeLinuxV2NativeWitnessStages(left, right linuxV2NativeWitnessStages) linuxV2NativeWitnessStages {
	return linuxV2NativeWitnessStages{
		ReservationDurable: left.ReservationDurable || right.ReservationDurable,
		CreateStarted:      left.CreateStarted || right.CreateStarted,
		LeafBound:          left.LeafBound || right.LeafBound,
		HelperBound:        left.HelperBound || right.HelperBound,
		LauncherBound:      left.LauncherBound || right.LauncherBound,
		GateBound:          left.GateBound || right.GateBound,
		HandoffPrepared:    left.HandoffPrepared || right.HandoffPrepared,
		HandoffCommitted:   left.HandoffCommitted || right.HandoffCommitted,
		InitialLeaseAcked:  left.InitialLeaseAcked || right.InitialLeaseAcked,
		CheckoutReady:      left.CheckoutReady || right.CheckoutReady,
		NativeReleased:     left.NativeReleased || right.NativeReleased,
		NativeTurnStarted:  left.NativeTurnStarted || right.NativeTurnStarted,
		StopReceiptDurable: left.StopReceiptDurable || right.StopReceiptDurable,
		LeafRemoved:        left.LeafRemoved || right.LeafRemoved,
		JournalCleared:     left.JournalCleared || right.JournalCleared,
		DetachedChildLive:  left.DetachedChildLive || right.DetachedChildLive,
	}
}

func linuxV2NativeWitnessLeafFromIdentity(identity authority.LinuxCgroupIdentity) linuxV2NativeWitnessLeaf {
	nameHash := sha256.Sum256([]byte(identity.LeafName))
	return linuxV2NativeWitnessLeaf{
		Version: identity.Version, ParentMountID: identity.Parent.MountID,
		ParentDevice: identity.Parent.Device, ParentInode: identity.Parent.Inode,
		Device: identity.LeafDevice, Inode: identity.LeafInode,
		NameSHA256: hex.EncodeToString(nameHash[:]),
	}
}

func linuxV2NativeWitnessSummarizeStop(output *linuxV2NativeWitnessStop, receipt authority.StopReceipt) {
	if output == nil {
		return
	}
	output.ReceiptVersion = receipt.Version
	output.ReceiptStatus = receipt.Status
	if receipt.LinuxProof == nil {
		return
	}
	proof := receipt.LinuxProof
	output.ProofKind = proof.Kind
	output.Issuer = linuxV2NativeWitnessProcess{PID: proof.Issuer.PID, Identity: proof.Issuer.Identity}
	output.Reaper = linuxV2NativeWitnessProcess{PID: proof.Reaper.PID, Identity: proof.Reaper.Identity}
	output.KillAcknowledged = proof.CgroupKillAcknowledged
	output.PopulatedZero = proof.CgroupPopulatedZero
	output.NoMigrationObserved = proof.NoMigrationObserved
	output.ProducerFenced = proof.ProducerFenced
	output.DescendantsReaped = proof.DescendantsReaped
	output.LauncherExit = linuxV2NativeWitnessExit{
		PID: proof.LauncherExit.Process.PID, Identity: proof.LauncherExit.Process.Identity,
		Waited: proof.LauncherExit.Waited, Reaped: proof.LauncherExit.Reaped,
	}
	output.GateExit = linuxV2NativeWitnessExit{
		PID: proof.GateExit.Process.PID, Identity: proof.GateExit.Process.Identity,
		Waited: proof.GateExit.Waited, Reaped: proof.GateExit.Reaped,
	}
	if proof.HelperExit != nil {
		exit := linuxV2NativeWitnessExit{
			PID: proof.HelperExit.Process.PID, Identity: proof.HelperExit.Process.Identity,
			Waited: proof.HelperExit.Waited, Reaped: proof.HelperExit.Reaped,
		}
		output.HelperExit = &exit
	}
}

func verifyLinuxV2NativeWitnessHookRoot(path string, owner linuxgate.OwnerConfig) error {
	if err := validateLinuxV2NativeWitnessPath(path); err != nil {
		return fmt.Errorf("native hook marker root is outside the owner witness root: %w", err)
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return fmt.Errorf("inspect native hook marker root: %w", err)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || !info.IsDir() || identity.uid != owner.AUID || identity.gid != owner.BGID || identity.mode&07777 != 0o2770 {
		return errors.New("native hook marker root must be the isolated A-owned/B-group 2770 fixture directory")
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) startDetachedChildCapture(ctx context.Context, daemon *daemon, store *state.Store) {
	if witness.request.Case != "helper_sigkill_detached_child" && witness.request.Case != "daemon_sigkill_helper_survives_detached_child" {
		return
	}
	hookRoot := os.Getenv(linuxV2NativeOwnerWitnessHookEnv)
	go func() {
		timer := time.NewTimer(time.Duration(witness.request.Barrier.TimeoutSeconds) * time.Second)
		defer timer.Stop()
		ticker := time.NewTicker(linuxV2NativeOwnerWitnessPoll)
		defer ticker.Stop()
		markerPath := filepath.Join(hookRoot, "post-commit-child.pid")
		for {
			journal, err := store.LoadJournal(state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation})
			if err == nil {
				if leaf := linuxV2NativeWitnessJournalLeaf(journal); leaf != nil {
					child, pidfd, captureErr := captureLinuxV2NativeWitnessChild(markerPath, *leaf)
					if captureErr == nil {
						witness.mu.Lock()
						witness.child = child
						witness.childPIDFD = pidfd
						witness.summary.DetachedChild = child
						witness.mu.Unlock()
						_ = witness.stage(linuxV2NativeWitnessChildStage, daemon, store, false)
						return
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (witness *linuxV2NativeWitnessRun) waitForDetachedChild(ctx context.Context, daemon *daemon, store *state.Store) error {
	deadline := time.NewTimer(time.Duration(witness.request.Barrier.TimeoutSeconds) * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(linuxV2NativeOwnerWitnessPoll)
	defer ticker.Stop()
	for {
		witness.mu.Lock()
		child := witness.child
		witness.mu.Unlock()
		if child != nil {
			return witness.stage(linuxV2NativeWitnessTurnStage, daemon, store, true)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for exact B detached-child hook witness: %w", ctx.Err())
		case <-deadline.C:
			return errors.New("B detached-child marker did not become a live pidfd witness before the bound")
		case <-ticker.C:
		}
	}
}

func linuxV2NativeWitnessJournalLeaf(journal state.RunJournal) *authority.LinuxCgroupIdentity {
	if journal.ContainmentCgroup != nil && journal.ContainmentCgroup.LeafIdentity != nil {
		identity := *journal.ContainmentCgroup.LeafIdentity
		return &identity
	}
	if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.CgroupIdentity != nil {
		identity := *journal.ContainmentHandoff.CgroupIdentity
		return &identity
	}
	if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.CgroupIdentity != nil {
		identity := *journal.ContainmentAuthority.CgroupIdentity
		return &identity
	}
	if journal.ContainmentCgroupReleaseProof != nil && journal.ContainmentCgroupReleaseProof.CgroupIdentity != nil {
		identity := *journal.ContainmentCgroupReleaseProof.CgroupIdentity
		return &identity
	}
	return nil
}

func captureLinuxV2NativeWitnessChild(markerPath string, leaf authority.LinuxCgroupIdentity) (*linuxV2NativeWitnessDetachedChild, *os.File, error) {
	if err := validateLinuxV2NativeWitnessPath(markerPath); err != nil {
		return nil, nil, err
	}
	file, err := os.Open(markerPath)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 32 {
		return nil, nil, errors.New("detached-child pid marker is not a bounded regular file")
	}
	contents, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil || len(contents) > 32 {
		return nil, nil, errors.New("detached-child pid marker exceeds its bound")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil || pid <= 0 {
		return nil, nil, errors.New("detached-child marker does not contain a positive PID")
	}
	identity, err := platform.ProcessIdentity(pid)
	if err != nil {
		return nil, nil, err
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open exact detached-child pidfd: %w", err)
	}
	pidfd := os.NewFile(uintptr(fd), "owner-witness-detached-child-pidfd")
	if pidfd == nil {
		_ = unix.Close(fd)
		return nil, nil, errors.New("wrap detached-child pidfd")
	}
	if current, err := platform.ProcessIdentity(pid); err != nil || current != identity {
		_ = pidfd.Close()
		return nil, nil, errors.New("detached-child identity changed after pidfd open")
	}
	var poll [1]unix.PollFd
	poll[0] = unix.PollFd{Fd: int32(fd), Events: unix.POLLIN}
	if _, err := unix.Poll(poll[:], 0); err != nil || poll[0].Revents != 0 {
		_ = pidfd.Close()
		return nil, nil, errors.New("detached-child pidfd is not live")
	}
	parent, processGroup, sessionID, err := linuxV2NativeWitnessProcessGroups(pid)
	if err != nil {
		_ = pidfd.Close()
		return nil, nil, err
	}
	member, err := linuxV2NativeWitnessPIDInLeaf(pid, leaf)
	if err != nil || !member {
		_ = pidfd.Close()
		return nil, nil, errors.Join(errors.New("detached child is not in the exact reserved cgroup leaf"), err)
	}
	return &linuxV2NativeWitnessDetachedChild{
		PID: pid, Identity: identity, ParentPID: parent, ProcessGroup: processGroup,
		SessionID: sessionID, PIDFDVerified: true, LeafMembershipVerified: true,
	}, pidfd, nil
}

func linuxV2NativeWitnessProcessGroups(pid int) (int, int, int, error) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read detached-child process groups: %w", err)
	}
	closeParen := bytes.LastIndexByte(contents, ')')
	if closeParen < 0 || closeParen+2 >= len(contents) {
		return 0, 0, 0, errors.New("detached-child process stat has no command boundary")
	}
	fields := strings.Fields(string(contents[closeParen+1:]))
	if len(fields) < 4 {
		return 0, 0, 0, errors.New("detached-child process stat lacks PPID/PGRP/SID")
	}
	parent, parentErr := strconv.Atoi(fields[1])
	group, groupErr := strconv.Atoi(fields[2])
	session, sessionErr := strconv.Atoi(fields[3])
	if parentErr != nil || groupErr != nil || sessionErr != nil || parent < 0 || group <= 0 || session <= 0 {
		return 0, 0, 0, errors.New("detached-child PPID/PGRP/SID is invalid")
	}
	return parent, group, session, nil
}

func linuxV2NativeWitnessNetworkNamespace(pid int) (linuxV2NativeWitnessNamespace, error) {
	if pid <= 0 {
		return linuxV2NativeWitnessNamespace{}, errors.New("network namespace identity requires the exact native PID")
	}
	var native, local unix.Stat_t
	if err := unix.Stat(fmt.Sprintf("/proc/%d/ns/net", pid), &native); err != nil {
		return linuxV2NativeWitnessNamespace{}, fmt.Errorf("stat native network namespace: %w", err)
	}
	if err := unix.Stat("/proc/self/ns/net", &local); err != nil {
		return linuxV2NativeWitnessNamespace{}, fmt.Errorf("stat owner network namespace: %w", err)
	}
	if native.Mode&unix.S_IFMT != unix.S_IFREG || local.Mode&unix.S_IFMT != unix.S_IFREG ||
		native.Dev != local.Dev || native.Ino != local.Ino {
		return linuxV2NativeWitnessNamespace{}, errors.New("native process does not retain the exact host network namespace")
	}
	return linuxV2NativeWitnessNamespace{Device: uint64(native.Dev), Inode: native.Ino}, nil
}

func linuxV2NativeWitnessPIDInLeaf(pid int, expected authority.LinuxCgroupIdentity) (bool, error) {
	if pid <= 0 || expected.Validate() != nil {
		return false, errors.New("detached-child leaf membership check is incomplete")
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return false, fmt.Errorf("read detached-child cgroup membership: %w", err)
	}
	want := filepath.Join(expected.Parent.Path, expected.LeafName)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "0::") {
			return strings.TrimPrefix(line, "0::") == want, nil
		}
	}
	return false, errors.New("detached-child has no unified cgroup membership entry")
}

func classifyLinuxV2NativeWitnessPeerDenial(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	permissionDenied := errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) ||
		strings.Contains(message, "operation not permitted") || strings.Contains(message, "permission denied")
	if strings.Contains(message, "pidfd_getfd") && permissionDenied {
		return "pidfd_getfd_denied"
	}
	if strings.Contains(message, "fdinfo") && permissionDenied {
		return "fdinfo_denied"
	}
	if strings.Contains(message, "namespace") && (permissionDenied || strings.Contains(message, "nsfs")) ||
		strings.Contains(message, "nsfs inode") && permissionDenied {
		return "nsfs_namespace_denied"
	}
	return ""
}

func writeLinuxV2NativeWitnessSummary(output io.Writer, summary linuxV2NativeWitnessSummary) error {
	if output == nil {
		return errors.New("owner witness output writer is nil")
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode redacted owner witness summary: %w", err)
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) stage(stage string, daemon *daemon, store *state.Store, allowHold bool) error {
	key := state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation}
	journal, err := store.LoadJournal(key)
	if err != nil {
		return fmt.Errorf("read exact RunJournal at owner witness %s barrier: %w", stage, err)
	}
	if err := witness.summarizeLiveJournal(journal); err != nil {
		return err
	}
	if err := witness.setStage(stage); err != nil {
		return err
	}
	witness.mu.Lock()
	marker := linuxV2NativeWitnessStageMarker{
		Version: 1, Case: witness.request.Case, Stage: stage,
		HoldAfter: witness.request.Barrier.HoldAfter, RunKey: witness.request.RunKey,
		NativeReleaseState: witness.summary.NativeReleaseState,
		Target:             witness.summary.Target,
		Owners:             witness.summary.Owners, Leaf: witness.summary.Leaf, Stop: witness.summary.Stop,
		Release: witness.summary.Release,
	}
	if witness.request.DropAckAfter == stage {
		marker.DropAckAfter = stage
	}
	if witness.child != nil {
		copy := *witness.child
		marker.DetachedChild = &copy
	}
	witness.mu.Unlock()
	return witness.barrier.stage(witness.ctx, marker, allowHold)
}

func (witness *linuxV2NativeWitnessRun) setStage(stage string) error {
	witness.mu.Lock()
	defer witness.mu.Unlock()
	switch stage {
	case linuxV2NativeWitnessReserveStage:
		witness.stages.ReservationDurable = true
	case linuxV2NativeWitnessCreateStage:
		witness.stages.CreateStarted = true
	case linuxV2NativeWitnessLeafStage:
		witness.stages.LeafBound = true
	case linuxV2NativeWitnessHelperStage:
		witness.stages.HelperBound = true
	case linuxV2NativeWitnessLauncherStage:
		witness.stages.LauncherBound = true
	case linuxV2NativeWitnessGateStage:
		witness.stages.GateBound = true
	case linuxV2NativeWitnessPrepareStage:
		witness.stages.HandoffPrepared = true
	case linuxV2NativeWitnessCommitStage:
		witness.stages.HandoffCommitted = true
	case linuxV2NativeWitnessLeaseStage:
		witness.stages.InitialLeaseAcked = true
	case linuxV2NativeWitnessCheckoutStage:
		witness.stages.CheckoutReady = true
	case linuxV2NativeWitnessReleaseStage:
		witness.stages.NativeReleased = true
	case linuxV2NativeWitnessTurnStage:
		witness.stages.NativeReleased = true
		witness.stages.NativeTurnStarted = true
	case linuxV2NativeWitnessStopReceiptStage:
		witness.stages.StopReceiptDurable = true
	case linuxV2NativeWitnessLeafRemovedStage:
		witness.stages.LeafRemoved = true
	case linuxV2NativeWitnessClearedStage:
		witness.stages.JournalCleared = true
	case linuxV2NativeWitnessChildStage:
		witness.stages.NativeReleased = true
		witness.stages.DetachedChildLive = true
	default:
		return fmt.Errorf("unknown Linux owner witness stage %q", stage)
	}
	if releaseState, known := linuxV2NativeWitnessStageReleaseState(stage); known {
		witness.summary.NativeReleaseState = releaseState
	}
	witness.summary.Stages = witness.stages
	return nil
}

func (witness *linuxV2NativeWitnessRun) restoreStageMarkers() error {
	journalReleaseState := witness.summary.NativeReleaseState
	releaseStageObserved := false
	heldPreReleaseStageObserved := false
	for _, stage := range []string{
		linuxV2NativeWitnessReserveStage, linuxV2NativeWitnessCreateStage, linuxV2NativeWitnessLeafStage,
		linuxV2NativeWitnessHelperStage, linuxV2NativeWitnessLauncherStage, linuxV2NativeWitnessGateStage,
		linuxV2NativeWitnessPrepareStage, linuxV2NativeWitnessCommitStage, linuxV2NativeWitnessLeaseStage,
		linuxV2NativeWitnessCheckoutStage, linuxV2NativeWitnessReleaseStage, linuxV2NativeWitnessTurnStage,
		linuxV2NativeWitnessStopReceiptStage, linuxV2NativeWitnessLeafRemovedStage, linuxV2NativeWitnessClearedStage,
		linuxV2NativeWitnessChildStage,
	} {
		path := filepath.Join(witness.request.Barrier.Directory, stage+".json")
		contents, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || len(contents) > 64<<10 {
			return errors.New("read existing bounded owner witness stage marker")
		}
		decoder := json.NewDecoder(bytes.NewReader(contents))
		decoder.DisallowUnknownFields()
		var marker linuxV2NativeWitnessStageMarker
		if err := decoder.Decode(&marker); err != nil || requireJSONEOF(decoder) != nil || marker.Version != 1 ||
			marker.Case != witness.request.Case || marker.Stage != stage || marker.RunKey != witness.request.RunKey {
			return errors.New("existing owner witness stage marker differs from the exact request")
		}
		if marker.HoldAfter != "" && marker.HoldAfter != witness.request.Barrier.HoldAfter {
			return errors.New("existing owner witness stage marker belongs to another held boundary")
		}
		if marker.DropAckAfter != "" && (marker.DropAckAfter != stage || marker.HoldAfter != stage ||
			!linuxV2NativeWitnessAckLossSupported(marker.DropAckAfter)) {
			return errors.New("existing owner witness stage marker has an invalid lost-ACK boundary")
		}
		stageReleaseState, knownStage := linuxV2NativeWitnessStageReleaseState(stage)
		if marker.NativeReleaseState == "" {
			marker.NativeReleaseState = linuxV2NativeWitnessNativeReleaseUnknown
			if knownStage {
				// Legacy marker position only describes state at that stage; the
				// final not_released assessment below requires the exact held boundary.
				marker.NativeReleaseState = stageReleaseState
			}
		}
		if !linuxV2NativeWitnessValidReleaseState(marker.NativeReleaseState) ||
			knownStage && marker.NativeReleaseState != stageReleaseState {
			return errors.New("existing owner witness stage marker has an inconsistent native-release state")
		}
		if marker.NativeReleaseState == linuxV2NativeWitnessNativeReleaseReleased {
			releaseStageObserved = true
		}
		if marker.Stage == witness.request.Barrier.HoldAfter && marker.HoldAfter == witness.request.Barrier.HoldAfter &&
			marker.NativeReleaseState == linuxV2NativeWitnessNativeReleaseNotReleased {
			heldPreReleaseStageObserved = true
		}
		if err := witness.setStage(stage); err != nil {
			return err
		}
		if marker.DetachedChild != nil {
			copy := *marker.DetachedChild
			witness.child = &copy
			witness.summary.DetachedChild = &copy
		}
		if marker.Target.PID > 0 && marker.Target.Identity != "" {
			witness.summary.Target = marker.Target
		}
		witness.summary.NativeReleaseState = marker.NativeReleaseState
		if marker.NativeReleaseState == linuxV2NativeWitnessNativeReleaseReleased && !witness.stages.NativeReleased {
			if err := witness.setStage(linuxV2NativeWitnessReleaseStage); err != nil {
				return err
			}
		}
	}
	if witness.request.Case == "stage_replay" {
		switch {
		case releaseStageObserved || journalReleaseState == linuxV2NativeWitnessNativeReleaseReleased:
			witness.summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseReleased
		case heldPreReleaseStageObserved || journalReleaseState == linuxV2NativeWitnessNativeReleaseNotReleased:
			witness.summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseNotReleased
		default:
			witness.summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseUnknown
		}
	}
	witness.refreshDetachedChildReaped()
	return nil
}

func (witness *linuxV2NativeWitnessRun) refreshDetachedChildReaped() {
	witness.mu.Lock()
	defer witness.mu.Unlock()
	if witness.child == nil {
		return
	}
	copy := *witness.child
	copy.AnchorReaped = witness.summary.Stop.DescendantsReaped &&
		witness.summary.Owners.Reaper.PID == witness.summary.Owners.Anchor.PID &&
		witness.summary.Owners.Reaper.Identity != "" &&
		witness.summary.Owners.Reaper.Identity == witness.summary.Owners.Anchor.Identity
	witness.child = &copy
	witness.summary.DetachedChild = &copy
}

func verifyLinuxWitnessPathAncestors(path string, aUID uint32) error {
	rootInfo, err := lstatCanonicalLinuxPath(linuxV2NativeOwnerWitnessRoot)
	if err != nil {
		return errors.New("fixed owner witness root is unavailable or not canonical")
	}
	rootIdentity, err := linuxFileIdentity(rootInfo)
	if err != nil || !rootInfo.IsDir() || rootIdentity.uid != 0 || rootIdentity.mode&0022 != 0 {
		return errors.New("fixed owner witness root must be root-owned and non-writable")
	}
	current := filepath.Dir(path)
	for current != linuxV2NativeOwnerWitnessRoot {
		info, statErr := lstatCanonicalLinuxPath(current)
		if statErr != nil {
			return errors.New("fixture parent directory is unavailable or not canonical")
		}
		identity, identityErr := linuxFileIdentity(info)
		if identityErr != nil || !info.IsDir() || (identity.uid != 0 && identity.uid != aUID) || identity.mode&0022 != 0 {
			return errors.New("fixture parent directory is not controlled by root or A")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return errors.New("fixture path escaped the fixed owner witness root")
		}
		current = parent
	}
	return nil
}

func validateLinuxV2NativeWitnessConfig(value config.Config, owner linuxgate.OwnerConfig) error {
	if err := value.Validate(); err != nil {
		return fmt.Errorf("validate strict owner witness config: %w", err)
	}
	if value.Runtime.HarnessKind != config.RuntimeHarnessPi && value.Runtime.HarnessKind != config.RuntimeHarnessOpenCode {
		return errors.New("owner witness config must select pinned Pi or OpenCode")
	}
	if value.Runtime.HarnessKind == config.RuntimeHarnessPi && value.Runtime.HarnessVersion != pi.TestedVersion ||
		value.Runtime.HarnessKind == config.RuntimeHarnessOpenCode && value.Runtime.HarnessVersion != opencode.TestedVersion {
		return errors.New("owner witness config native harness version differs from the tested release")
	}
	profile := value.AgentProfiles[value.Runtime.AgentProfile]
	if value.Runtime.AdapterVersion == "legacy" || value.Runtime.AdapterProtocolVersion <= 0 ||
		!filepath.IsAbs(profile.Command) || filepath.Clean(profile.Command) != profile.Command {
		return errors.New("owner witness requires an absolute native command and explicit adapter metadata")
	}
	if value.LinuxOwnerCgroupMount == "" || value.LinuxOwnerDelegatedPath == "" || value.LinuxOwnerSocket == "" {
		return errors.New("owner witness requires an installed delegated cgroup-v2 parent and authenticated anchor socket")
	}
	for _, name := range profile.EnvAllowlist {
		if name != linuxV2NativeOwnerWitnessHookEnv {
			return errors.New("owner witness native environment may allowlist only the non-secret test hook marker root")
		}
	}
	workspaceBinding := value.Workspaces[value.Runtime.Workspace]
	if workspaceBinding.Policy != config.WorkspacePolicyGitCloneV2 || workspaceBinding.Path != "" || workspaceBinding.Ref != "" {
		return errors.New("owner witness requires the B-owned git_clone_v2 workspace policy")
	}
	if err := validateLinuxV2NativeWitnessStateDirectoryPath(value.StateDir, owner); err != nil {
		return fmt.Errorf("owner witness StateDir is not the installed test StateDir: %w", err)
	}
	for _, path := range []string{workspaceBinding.Repository, workspaceBinding.SeedRoot, workspaceBinding.Root} {
		if err := validateLinuxV2NativeWitnessPath(path); err != nil {
			return fmt.Errorf("owner witness data path is outside its test root: %w", err)
		}
	}
	if value.Runtime.HarnessKind == config.RuntimeHarnessPi {
		if err := pi.ValidateRPCProfileArgs(profile.Args); err != nil {
			return fmt.Errorf("validate pinned Pi RPC profile: %w", err)
		}
		if err := pi.ValidateProviderBridgeProfileArgs(profile.Args); err != nil {
			return fmt.Errorf("validate pinned Pi provider bridge profile: %w", err)
		}
		if profile.ProviderAccess {
			if value.PiProviderExtensionDir == "" {
				return errors.New("Pi provider access requires the configured extension share")
			}
			if err := validateLinuxV2NativeWitnessPath(value.PiProviderExtensionDir); err != nil {
				return fmt.Errorf("Pi extension share is outside owner witness root: %w", err)
			}
			if value.PiProviderExtensionGroupID != owner.BGID {
				return errors.New("Pi provider extension group differs from the trusted fixed-B primary group")
			}
		}
	} else {
		if profile.ProviderAccess || len(profile.Args) != 0 {
			return errors.New("OpenCode owner witness cannot enable Pi broker access or unverified argv")
		}
	}
	if owner.Validate() != nil || owner.AUID == 0 || owner.BUID == 0 || owner.AUID == owner.BUID || owner.BGID == 0 {
		return errors.New("installed owner configuration is incomplete for the native witness")
	}
	return nil
}

func populateTrustedLinuxWitnessPiExtensionGroup(value *config.Config, owner linuxgate.OwnerConfig) error {
	if value == nil || owner.Validate() != nil || owner.BGID == 0 {
		return errors.New("trusted installed B primary group is unavailable")
	}
	value.PiProviderExtensionGroupID = owner.BGID
	return verifyTrustedLinuxWitnessPiExtensionGroup(*value, owner)
}

func validateLinuxV2NativeWitnessInstalledOwner(owner linuxgate.OwnerConfig, uid, euid, gid, egid uint32) error {
	if owner.Validate() != nil {
		return errors.New("trusted installed Linux A/B owner config is invalid")
	}
	return verifyLinuxOwnerIdentity(owner, uid, euid, gid, egid)
}

func verifyTrustedLinuxWitnessPiExtensionGroup(value config.Config, owner linuxgate.OwnerConfig) error {
	if owner.Validate() != nil || owner.BGID == 0 || value.PiProviderExtensionGroupID != owner.BGID {
		return errors.New("Pi provider extension share group differs from the trusted fixed-B primary GID")
	}
	return nil
}

func validateLinuxV2NativeWitnessRequest(request linuxV2NativeWitnessRequest, value config.Config, owner linuxgate.OwnerConfig) error {
	if request.Version != 1 || request.RunKey.RunID == "" || len(request.RunKey.RunID) > 128 ||
		request.RunKey.Generation <= 0 || request.Claim.RunID != request.RunKey.RunID ||
		request.Claim.Generation != request.RunKey.Generation || request.Fence.Generation != request.RunKey.Generation ||
		request.Fence.RuntimeID == "" || len(request.Fence.RuntimeID) > 256 || request.Fence.RuntimeEpoch <= 0 ||
		request.Fence.ClaimID == "" || request.Fence.ClaimID != request.Claim.ClaimID ||
		request.Fence.LeaseToken == "" || len(request.Fence.LeaseToken) > 8192 || request.Fence.LeaseToken != request.Claim.LeaseToken ||
		request.Claim.TaskID == "" || request.Claim.LeaseRemainingMS <= 0 || request.Claim.LeaseExpiresAt.IsZero() ||
		request.Claim.HarnessSessionID != nil || request.Claim.HarnessBindingID != nil {
		return errors.New("owner witness request lacks an exact Control ClaimResponse, RunKey, or fence")
	}
	if request.ReplayRequired && request.Case != "stage_replay" {
		return errors.New("replay_required is restricted to the opt-in stage-replay owner witness")
	}
	if strings.IndexByte(request.RunKey.RunID, 0) >= 0 || strings.IndexByte(request.Fence.RuntimeID, 0) >= 0 ||
		strings.IndexByte(request.Fence.ClaimID, 0) >= 0 || strings.IndexByte(request.Fence.LeaseToken, 0) >= 0 ||
		request.Claim.LeaseRemainingMS > int64((24*time.Hour)/time.Millisecond) {
		return errors.New("owner witness claim or fence contains an invalid bounded identity")
	}
	if request.Harness != value.Runtime.HarnessKind || request.Subject.Validate() != nil ||
		request.Subject.ResourceID != value.Runtime.RepositoryResourceID ||
		request.Claim.Work.AgentProfile != value.Runtime.AgentProfile || request.Claim.Work.Workspace != value.Runtime.Workspace ||
		strings.TrimSpace(request.Claim.Work.Goal) == "" {
		return errors.New("owner witness claim Work, Subject, resource, and runtime configuration do not match")
	}
	if !linuxV2NativeWitnessCaseMatches(request.Case, request.Harness) {
		return errors.New("owner witness case does not match its configured native harness")
	}
	if (request.Case == "helper_sigkill_detached_child" || request.Case == "daemon_sigkill_helper_survives_detached_child") &&
		request.Barrier.HoldAfter != linuxV2NativeWitnessTurnStage {
		return errors.New("detached-child crash witness requires a native_turn_started barrier")
	}
	if request.Case == "stage_replay" && request.Barrier.HoldAfter == "none" {
		return errors.New("stage_replay requires one exact held crash boundary")
	}
	if request.Harness == config.RuntimeHarnessOpenCode && request.Barrier.HoldAfter == linuxV2NativeWitnessTurnStage {
		return errors.New("OpenCode owner witness has no native turn stage; use an exact Open/health or stop barrier")
	}
	if request.Barrier.TimeoutSeconds <= 0 || request.Barrier.TimeoutSeconds > linuxV2NativeOwnerWitnessTimeoutMax ||
		!linuxV2NativeWitnessHoldSupported(request.Barrier.HoldAfter) {
		return errors.New("owner witness barrier stage or timeout is invalid")
	}
	if request.DropAckAfter != "" && (request.Case != "stage_replay" ||
		request.DropAckAfter != request.Barrier.HoldAfter || !linuxV2NativeWitnessAckLossSupported(request.DropAckAfter)) {
		return errors.New("drop_ack_after requires one exact supported StageReplay ACK boundary and matching hold_after")
	}
	if request.Barrier.HoldAfter == linuxV2NativeWitnessLeaseStage && request.Barrier.TimeoutSeconds != linuxV2NativeOwnerWitnessTimeoutMax {
		return errors.New("Runner initial-lease barrier requires its fixed 30-second timeout")
	}
	if err := validateLinuxV2NativeWitnessPath(request.Barrier.Directory); err != nil ||
		request.Barrier.ReleaseFile != "" && request.Barrier.ReleaseFile != filepath.Join(request.Barrier.Directory, "release") {
		return errors.New("owner witness barrier paths are invalid")
	}
	if request.Barrier.HoldAfter == "none" && request.Barrier.ReleaseFile != "" ||
		request.Barrier.HoldAfter != "none" && request.Barrier.ReleaseFile == "" {
		return errors.New("owner witness release file does not match its bounded hold stage")
	}
	if request.Harness == config.RuntimeHarnessPi {
		profileAllowsProvider := value.AgentProfiles[value.Runtime.AgentProfile].ProviderAccess
		requiresProvider := request.Case == "pi_provider_action" || request.Case == "helper_sigkill_detached_child" ||
			request.Case == "daemon_sigkill_helper_survives_detached_child"
		if requiresProvider {
			if len(request.Claim.Work.RequiredCapabilities) != 1 || !request.Claim.Work.RequiredCapabilities["provider_access"] {
				return errors.New("Pi provider witness claim must require only provider_access")
			}
		} else if len(request.Claim.Work.RequiredCapabilities) != 0 {
			return errors.New("stage replay claim must not require native or provider capabilities")
		}
		if requiresProvider && (!profileAllowsProvider || request.Claim.ProviderAccess == nil) {
			return errors.New("Pi action witness requires the authentic claim-scoped Control provider access")
		}
		if request.Claim.ProviderAccess != nil {
			if !profileAllowsProvider {
				return errors.New("Pi ClaimResponse carries provider authority not enabled by its pinned profile")
			}
			access := request.Claim.ProviderAccess
			if access.Token == "" || strings.Contains(request.Claim.Work.Goal, access.Token) ||
				bytes.Contains(request.Claim.Work.Input, []byte(access.Token)) {
				return errors.New("provider access credential is missing or duplicated in persistable Work data")
			}
			for _, argument := range value.AgentProfiles[value.Runtime.AgentProfile].Args {
				if strings.Contains(argument, access.Token) {
					return errors.New("provider access credential is present in native argv configuration")
				}
			}
			if access.Path != "/api/v1/provider-actions" || strings.TrimSpace(access.Token) == "" || len(access.Token) > 8192 || len(access.Grants) != 1 {
				return errors.New("Pi owner witness provider access is not a bounded claim-scoped broker capability")
			}
			grant := access.Grants[0]
			if grant.ResourceID != request.Subject.ResourceID || grant.Provider != "github" || grant.Kind != "repository" ||
				len(grant.Operations) != 1 || grant.Operations[0] != "resource.sync" {
				return errors.New("Pi owner witness token does not grant exactly resource.sync for the exact repository")
			}
		}
	} else if request.Claim.ProviderAccess != nil {
		return errors.New("OpenCode owner witness must not receive Pi provider bridge authority")
	} else if len(request.Claim.Work.RequiredCapabilities) != 0 {
		return errors.New("OpenCode owner witness claim must not require native/provider capability promotion")
	}
	if owner.BUID == 0 || owner.BGID == 0 || value.Runtime.RepositoryResourceID != request.Subject.ResourceID {
		return errors.New("owner witness does not match the installed B/resource identity")
	}
	if request.Barrier.HoldAfter != "none" && request.Barrier.ReleaseFile != filepath.Join(request.Barrier.Directory, "release") {
		return errors.New("owner witness release file must be the fixed barrier child")
	}
	return nil
}

func linuxV2NativeWitnessCaseMatches(testCase, harnessKind string) bool {
	switch testCase {
	case "stage_replay":
		return harnessKind == config.RuntimeHarnessPi || harnessKind == config.RuntimeHarnessOpenCode
	case "pi_provider_action", "helper_sigkill_detached_child", "daemon_sigkill_helper_survives_detached_child":
		return harnessKind == config.RuntimeHarnessPi
	case "opencode_native_health", "wsl2_denial":
		return harnessKind == config.RuntimeHarnessOpenCode
	default:
		return false
	}
}

func linuxV2NativeWitnessHoldSupported(stage string) bool {
	switch stage {
	case "none", linuxV2NativeWitnessReserveStage, linuxV2NativeWitnessCreateStage, linuxV2NativeWitnessLeafStage,
		linuxV2NativeWitnessHelperStage, linuxV2NativeWitnessLauncherStage, linuxV2NativeWitnessGateStage,
		linuxV2NativeWitnessPrepareStage, linuxV2NativeWitnessCommitStage, linuxV2NativeWitnessLeaseStage,
		linuxV2NativeWitnessCheckoutStage, linuxV2NativeWitnessReleaseStage, linuxV2NativeWitnessTurnStage,
		linuxV2NativeWitnessStopReceiptStage, linuxV2NativeWitnessLeafRemovedStage, linuxV2NativeWitnessClearedStage:
		return true
	default:
		return false
	}
}

func linuxV2NativeWitnessAckLossSupported(stage string) bool {
	switch stage {
	case execution.LinuxV2AckStageHelperBound, execution.LinuxV2AckStageLauncherBound,
		execution.LinuxV2AckStageGateBound, execution.LinuxV2AckStageHandoffPrepared,
		execution.LinuxV2AckStageHandoffCommitted, execution.LinuxV2AckStageLeaseArmed,
		execution.LinuxV2AckStageCheckoutReady, execution.LinuxV2AckStageStopReceiptDurable,
		execution.LinuxV2AckStageNativeRelease, execution.LinuxV2AckStageJournalCleared:
		return true
	default:
		return false
	}
}

func linuxV2NativeWitnessHelperDropSupported(operation string) bool {
	switch operation {
	case "stop", "ack-stop-receipt", "launcher-ready":
		return true
	default:
		return false
	}
}

type linuxV2NativeWitnessDropStatusReader interface {
	LinuxV2WitnessDropStatus(context.Context) (execution.LinuxV2WitnessDropStatus, error)
}

func (witness *linuxV2NativeWitnessRun) captureHelperDropStatus(store *state.Store, process Process) error {
	if witness == nil || store == nil || process == nil || witness.request.DropAckAfter != "" {
		return nil
	}
	reader, ok := process.(linuxV2NativeWitnessDropStatusReader)
	if !ok {
		return nil
	}
	statusContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := reader.LinuxV2WitnessDropStatus(statusContext)
	if err != nil {
		return err
	}
	key := state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation}
	if status.RunKey != key || status.Fence != witness.request.Fence || status.Helper.Validate() != nil ||
		status.Observed && !linuxV2NativeWitnessHelperDropSupported(status.Operation) ||
		!status.Observed && status.Operation != "" {
		return errors.New("Runner helper-drop status differs from the exact witness RunKey/fence or operation")
	}
	journal, err := store.LoadJournal(key)
	if err != nil || !linuxV2JournalMatchesFence(journal, key, witness.request.Fence) {
		return errors.Join(errors.New("read exact RunJournal for helper-drop status binding"), err)
	}
	expectedHelper, ok := linuxV2NativeWitnessHelperIdentity(journal)
	if !ok || !status.Helper.Equal(expectedHelper) {
		return errors.New("Runner helper-drop status differs from the exact RunJournal helper identity")
	}
	if !status.Observed {
		return nil
	}
	witness.mu.Lock()
	defer witness.mu.Unlock()
	if witness.summary.HelperDropObserved && witness.summary.HelperDropOperation != status.Operation {
		return errors.New("authenticated helper-drop status changed its one-shot operation")
	}
	witness.summary.HelperDropObserved = true
	witness.summary.HelperDropOperation = status.Operation
	if witness.summary.ResponseLostAfter == "" {
		witness.summary.ResponseLostAfter = status.Operation
	}
	return nil
}

func linuxV2NativeWitnessHelperIdentity(journal state.RunJournal) (authority.LinuxCgroupProcessIdentity, bool) {
	if journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Helper != nil {
		return *journal.ContainmentCgroup.Helper, true
	}
	if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.SupervisorPID > 0 && journal.ContainmentHandoff.SupervisorIdentity != "" {
		return authority.LinuxCgroupProcessIdentity{
			PID: journal.ContainmentHandoff.SupervisorPID, Identity: journal.ContainmentHandoff.SupervisorIdentity,
		}, true
	}
	if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.SupervisorPID > 0 && journal.ContainmentAuthority.SupervisorIdentity != "" {
		return authority.LinuxCgroupProcessIdentity{
			PID: journal.ContainmentAuthority.SupervisorPID, Identity: journal.ContainmentAuthority.SupervisorIdentity,
		}, true
	}
	if proof := journal.ContainmentCgroupReleaseProof; proof != nil && proof.SupervisorPID > 0 && proof.SupervisorIdentity != "" {
		return authority.LinuxCgroupProcessIdentity{PID: proof.SupervisorPID, Identity: proof.SupervisorIdentity}, true
	}
	if proof := journal.ContainmentCgroupPreGateReleaseProof; proof != nil && proof.Receipt.Helper.Validate() == nil {
		return proof.Receipt.Helper, true
	}
	return authority.LinuxCgroupProcessIdentity{}, false
}

func validateLinuxV2NativeWitnessStateDirectory(path string, owner linuxgate.OwnerConfig) error {
	if err := validateLinuxV2NativeWitnessStateDirectoryPath(path, owner); err != nil {
		return fmt.Errorf("validate installed owner witness StateDir: %w", err)
	}
	info, err := lstatCanonicalLinuxPath(path)
	if err != nil {
		return fmt.Errorf("inspect A-private owner witness StateDir: %w", err)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || !info.IsDir() || identity.uid != owner.AUID || identity.gid != owner.AGID || identity.mode&07777 != 0o700 {
		return errors.New("owner witness StateDir must be a private 0700 directory owned by A")
	}
	if err := verifyLinuxOwnerStateDirBinding(path, owner); err != nil {
		return fmt.Errorf("pin StateDir to root-installed owner authority: %w", err)
	}
	accounts, err := verifyLinuxOwnerAccounts(owner)
	if err != nil {
		return err
	}
	if err := verifyLinuxTrustedPathAncestors(path, owner.AUID, owner.AGID, &accounts.bSubject); err != nil {
		return fmt.Errorf("verify owner witness StateDir ancestors before Store open: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(path, accounts.aSubject, 7, 0); err != nil {
		return fmt.Errorf("A cannot manage owner witness StateDir before Store open: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(path, accounts.bSubject, 0, 7); err != nil {
		return fmt.Errorf("fixed B can access owner witness StateDir before Store open: %w", err)
	}
	if err := verifyLinuxDefaultLinuxAccess(path, accounts.bSubject, 0, 7); err != nil {
		return fmt.Errorf("future owner witness StateDir entries may be accessible to fixed B: %w", err)
	}
	if err := verifyLinuxNoUntrustedACLWrite(path, []uint32{0, owner.AUID}, []uint32{owner.AGID}, true); err != nil {
		return fmt.Errorf("owner witness StateDir is changeable by an untrusted identity: %w", err)
	}
	return nil
}

func validateLinuxV2NativeWitnessExistingJournal(journal state.RunJournal, request linuxV2NativeWitnessRequest) error {
	if err := validateLinuxV2NativeWitnessJournalIdentity(journal, request); err != nil {
		return err
	}
	if journal.LinuxCloneIntent == nil || journal.LinuxCloneIntent.ResourceID != request.Subject.ResourceID ||
		journal.LinuxCloneIntent.BaseCommit != request.Subject.Commit || journal.LinuxCloneIntent.BaseTreeDigest != request.Subject.TreeDigest {
		return errors.New("existing owner witness clone intent differs from the exact Subject")
	}
	return nil
}

func recoverLinuxV2NativeWitnessRunKey(
	ctx context.Context,
	value config.Config,
	store *state.Store,
	journal state.RunJournal,
) error {
	if ctx == nil || store == nil || journal.Key() == (state.RunKey{}) {
		return errors.New("exact owner-witness recovery requires its live context, locked Store, and selected RunJournal")
	}
	recoveryContext, cancel := context.WithTimeout(ctx, value.CleanupTimeout())
	defer cancel()
	recovery := &daemon{
		config: value,
		store:  store,
		options: options{
			store: store, clock: time.Now,
			terminatePersist: func(pid int, identity string) error {
				return terminatePersistedProcessWithContext(recoveryContext, pid, identity)
			},
			terminatePersistAuthority:             terminatePersistedProcessWithAuthority,
			terminatePersistAuthorityWithScanSeal: terminatePersistedProcessWithAuthorityScanSeal,
		},
		recoveryOnly:       true,
		background:         recoveryContext,
		running:            make(map[state.RunKey]*runningRun),
		recoveredStops:     make(map[state.RunKey]recoveredProcessStop),
		retainedWorkspaces: make(map[state.RunKey]struct{}),
	}
	defer recovery.close()
	return recoverContainmentOnlyJournals(recoveryContext, recovery, []state.RunJournal{journal})
}

func selectLinuxV2NativeWitnessJournal(
	journals []state.RunJournal,
	request linuxV2NativeWitnessRequest,
) (state.RunJournal, bool, error) {
	key := state.RunKey{RunID: request.RunKey.RunID, Generation: request.RunKey.Generation}
	var selected state.RunJournal
	found := false
	for _, journal := range journals {
		if journal.Key() != key {
			continue
		}
		if found {
			return state.RunJournal{}, false, errors.New("owner witness StateDir contains duplicate exact RunKey rows")
		}
		selected = journal
		found = true
	}
	if found {
		if request.Case == "stage_replay" && linuxV2NativeWitnessHasLegacyLinuxOwner(selected) {
			if err := validateLinuxV2NativeWitnessLegacyJournal(selected, request); err != nil {
				return state.RunJournal{}, false, err
			}
		} else if err := validateLinuxV2NativeWitnessExistingJournal(selected, request); err != nil {
			return state.RunJournal{}, false, err
		}
		return selected, true, nil
	}
	if request.ReplayRequired {
		return state.RunJournal{}, false, errLinuxV2NativeWitnessReplayTargetMissing
	}
	return state.RunJournal{}, false, nil
}

func verifyLinuxV2NativeWitnessSelectedJournal(selected, reloaded state.RunJournal) error {
	if selected.Key() != reloaded.Key() {
		return errors.New("selected owner witness RunJournal changed its exact RunKey before recovery")
	}
	selectedBytes, err := json.Marshal(selected)
	if err != nil {
		return fmt.Errorf("encode selected owner witness RunJournal: %w", err)
	}
	reloadedBytes, err := json.Marshal(reloaded)
	if err != nil {
		return fmt.Errorf("encode reloaded owner witness RunJournal: %w", err)
	}
	if !bytes.Equal(selectedBytes, reloadedBytes) {
		return errors.New("selected owner witness RunJournal changed before exact-key recovery")
	}
	return nil
}

func validateLinuxV2NativeWitnessJournalIdentity(journal state.RunJournal, request linuxV2NativeWitnessRequest) error {
	key := state.RunKey{RunID: request.RunKey.RunID, Generation: request.RunKey.Generation}
	if journal.Key() != key || journal.RuntimeID != request.Fence.RuntimeID || journal.ClaimedRuntimeEpoch != request.Fence.RuntimeEpoch ||
		journal.ClaimID != request.Fence.ClaimID || journal.LeaseToken != request.Fence.LeaseToken ||
		!journal.LeaseExpiresAt.Equal(request.Claim.LeaseExpiresAt) ||
		journal.WorkspaceBindingKey != request.Claim.Work.Workspace || !linuxV2WitnessWorkEqual(journal.Work, request.Claim.Work) {
		return errors.New("existing owner witness journal differs from the exact authentic claim/fence")
	}
	return nil
}

func linuxV2NativeWitnessHasLegacyLinuxOwner(journal state.RunJournal) bool {
	if journal.ContainmentHandoff != nil {
		return journal.ContainmentHandoff.Version == authority.SupervisorHandoffVersion &&
			journal.ContainmentHandoff.OwnerKind == authority.OwnerKindLinuxHelper
	}
	return journal.ContainmentAuthority != nil && journal.ContainmentAuthority.Version == authority.SupervisorVersion &&
		journal.ContainmentAuthority.OwnerKind == authority.OwnerKindLinuxHelper
}

func validateLinuxV2NativeWitnessLegacyJournal(journal state.RunJournal, request linuxV2NativeWitnessRequest) error {
	if err := validateLinuxV2NativeWitnessLegacyJournalIdentity(journal, request); err != nil {
		return err
	}
	if !linuxV2NativeWitnessHasLegacyLinuxOwner(journal) {
		return errors.New("stage replay journal has no exact legacy Linux helper handoff or authority")
	}
	return nil
}

func validateLinuxV2NativeWitnessLegacyJournalIdentity(journal state.RunJournal, request linuxV2NativeWitnessRequest) error {
	if err := validateLinuxV2NativeWitnessJournalIdentity(journal, request); err != nil {
		return err
	}
	if journal.ContainmentCgroup != nil || journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil ||
		journal.LinuxCloneIntent != nil || journal.LinuxCloneSeedReceipt != nil || journal.LinuxCloneCheckoutReceipt != nil ||
		journal.LinuxClonePromotionIntent != nil || journal.LinuxClonePromotionReceipt != nil ||
		journal.LinuxCloneAbortProof != nil || journal.LinuxCloneAbortPending {
		return errors.New("legacy StageReplay journal contains v2 cgroup or clone authority")
	}
	if journal.ContainmentHandoff != nil && (journal.ContainmentHandoff.Version != authority.SupervisorHandoffVersion ||
		journal.ContainmentHandoff.OwnerKind != authority.OwnerKindLinuxHelper) {
		return errors.New("legacy StageReplay handoff is not an exact v1 Linux helper owner")
	}
	if journal.ContainmentAuthority != nil && (journal.ContainmentAuthority.Version != authority.SupervisorVersion ||
		journal.ContainmentAuthority.OwnerKind != authority.OwnerKindLinuxHelper) {
		return errors.New("legacy StageReplay authority is not an exact v1 Linux helper owner")
	}
	if handoff := journal.ContainmentHandoff; handoff != nil && handoff.StopReceipt != nil &&
		(handoff.StopReceipt.Version != authority.SupervisorVersion || handoff.StopReceipt.LinuxProof != nil ||
			!handoff.StopReceipt.ValidForHandoff(*handoff)) {
		return errors.New("legacy StageReplay handoff receipt is not an exact v1 stop receipt")
	}
	if owner := journal.ContainmentAuthority; owner != nil && owner.StopReceipt != nil &&
		(owner.StopReceipt.Version != authority.SupervisorVersion || owner.StopReceipt.LinuxProof != nil ||
			!owner.StopReceipt.ValidFor(*owner)) {
		return errors.New("legacy StageReplay authority receipt is not an exact v1 stop receipt")
	}
	if journal.ContainmentHandoff != nil && journal.ContainmentAuthority != nil {
		return errors.New("legacy StageReplay journal has conflicting pending and committed owners")
	}
	return nil
}

func linuxV2NativeWitnessLegacyJournalUnproven(journal state.RunJournal) bool {
	return journal.HasProcessDetails() || journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil ||
		journal.ContainmentScans != nil || journal.ContainmentUnproven
}

func linuxV2NativeWitnessLegacyStop(journal state.RunJournal) (summary linuxV2NativeWitnessStop) {
	if journal.ContainmentHandoff != nil && journal.ContainmentHandoff.Version == authority.SupervisorHandoffVersion &&
		journal.ContainmentHandoff.StopReceipt != nil && journal.ContainmentHandoff.StopReceipt.Version == authority.SupervisorVersion &&
		journal.ContainmentHandoff.StopReceipt.ValidForHandoff(*journal.ContainmentHandoff) {
		linuxV2NativeWitnessSummarizeStop(&summary, *journal.ContainmentHandoff.StopReceipt)
	} else if journal.ContainmentAuthority != nil && journal.ContainmentAuthority.Version == authority.SupervisorVersion &&
		journal.ContainmentAuthority.StopReceipt != nil && journal.ContainmentAuthority.StopReceipt.Version == authority.SupervisorVersion &&
		journal.ContainmentAuthority.StopReceipt.ValidFor(*journal.ContainmentAuthority) {
		linuxV2NativeWitnessSummarizeStop(&summary, *journal.ContainmentAuthority.StopReceipt)
	}
	return summary
}

func linuxV2NativeWitnessJournalHasUnresolvedAuthority(journal state.RunJournal) bool {
	if journal.ContainmentCgroupReleaseProof != nil || journal.ContainmentCgroupPreGateReleaseProof != nil {
		return false
	}
	if journal.HasProcessDetails() || journal.ContainmentCgroup != nil || journal.ContainmentAuthority != nil ||
		journal.ContainmentHandoff != nil || journal.ContainmentUnproven || journal.ContainmentScans != nil ||
		journal.LinuxCloneAbortPending {
		return true
	}
	return journal.LinuxCloneIntent != nil && journal.LinuxCloneAbortProof == nil && journal.LinuxClonePromotionReceipt == nil
}

func linuxV2WitnessWorkEqual(left, right protocol.Work) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	leftCanonical, leftErr := protocol.CanonicalizeJSON(leftJSON)
	rightCanonical, rightErr := protocol.CanonicalizeJSON(rightJSON)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftCanonical, rightCanonical)
}

func linuxV2NativeWitnessJournalPhysicallyReleased(journal state.RunJournal) bool {
	if journal.HasProcessDetails() || journal.ContainmentCgroup != nil || journal.ContainmentAuthority != nil ||
		journal.ContainmentHandoff != nil || journal.ContainmentUnproven || journal.ContainmentScans != nil {
		return false
	}
	if proof := journal.ContainmentCgroupReleaseProof; proof != nil {
		return proof.Validate() == nil && proof.Version == authority.SupervisorHandoffVersionV2 && proof.StopReceipt != nil &&
			proof.StopReceipt.Version == authority.SupervisorVersionV2
	}
	if proof := journal.ContainmentCgroupPreGateReleaseProof; proof != nil {
		return proof.Validate() == nil && proof.Receipt.Validate() == nil
	}
	return false
}

func (witness *linuxV2NativeWitnessRun) runFresh(ctx context.Context, store *state.Store) (resultErr error) {
	profile := witness.config.AgentProfiles[witness.config.Runtime.AgentProfile]
	kind, err := configuredHarnessKind(witness.config.Runtime.HarnessKind)
	if err != nil {
		return err
	}
	if err := witness.verifyNativeExecutable(profile, kind); err != nil {
		return err
	}
	localNow := time.Now
	witnessDaemon := &daemon{
		config:          witness.config,
		store:           store,
		workspace:       workspace.New(witness.config.Workspaces),
		harnessRegistry: newHarnessRegistry(profile),
		runtimeID:       witness.request.Fence.RuntimeID,
		runtimeEpoch:    witness.request.Fence.RuntimeEpoch,
		running:         make(map[state.RunKey]*runningRun),
		log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		options: options{
			clock:      func() time.Time { return localNow().UTC() },
			localClock: localNow,
		},
	}
	if kind == harness.KindPi && witness.request.Claim.ProviderAccess != nil {
		// No registration, claim, or machine-auth request is made here. The
		// provider-action method uses only the authentic claim-scoped
		// ProviderAccess token; the existing client constructor still requires
		// a non-empty credential, so pass the same authentic claim's lease token.
		client, err := control.NewClient(witness.config.ControlPlaneURL, witness.request.Claim.LeaseToken, &http.Client{Timeout: 30 * time.Second})
		if err != nil {
			return fmt.Errorf("construct Control provider-action client: %w", err)
		}
		witnessDaemon.control = client
	}
	if err := witnessDaemon.checkLinuxV2NativeAdmissionBeforeClaim(ctx); err != nil {
		return fmt.Errorf("refuse fresh owner witness while foreign native ownership is unresolved: %w", err)
	}
	if err := witness.seedAuthenticClaim(store); err != nil {
		return err
	}
	owner, nativeEnvironment, err := witnessDaemon.prepareLinuxV2NativeOwner(
		ctx, state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation},
		witness.request.Fence, kind, profile, witness.request.Claim.ProviderAccess != nil,
	)
	if err != nil {
		return fmt.Errorf("preflight installed Linux v2 owner before clone/native work: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, owner.Close())
	}()
	resources, ok := owner.(*linuxV2OwnerResources)
	if !ok || resources == nil || resources.owner != witness.ownerConfig ||
		witness.config.PiProviderExtensionGroupID != witness.ownerConfig.BGID {
		return errors.New("witness owner resources differ from the trusted installed A/B config or Pi group")
	}
	home, _, err := linuxV2FixedNativeHome(witness.ownerConfig)
	if err != nil || validateLinuxV2NativeWitnessPath(home) != nil {
		return errors.Join(errors.New("fixed-B native HOME is outside the isolated owner witness root"), err)
	}
	if err := witness.verifyNativeExecutable(profile, kind); err != nil {
		return err
	}
	witness.summary.InstalledOwnerVerified = true
	witness.nativeEnvironment = append([]string(nil), nativeEnvironment...)
	prepared, grant, cloneWorkspace, err := witness.prepareClone(ctx, witnessDaemon, store)
	if err != nil {
		return err
	}
	witness.prepared = prepared
	witness.grant = grant
	witness.cloneWorkspace = cloneWorkspace
	return witness.launchNative(ctx, witnessDaemon, store, kind, profile, owner, prepared, grant)
}

func (witness *linuxV2NativeWitnessRun) verifyNativeExecutable(profile config.AgentProfile, kind harness.Kind) error {
	if !filepath.IsAbs(profile.Command) || filepath.Clean(profile.Command) != profile.Command {
		return errors.New("owner witness native executable must be canonical and absolute")
	}
	info, err := lstatCanonicalLinuxPath(profile.Command)
	if err != nil {
		return fmt.Errorf("inspect pinned native executable: %w", err)
	}
	identity, err := linuxFileIdentity(info)
	if err != nil || !info.Mode().IsRegular() || identity.uid != 0 || identity.mode&0022 != 0 || identity.links != 1 || identity.mode&0111 == 0 {
		return errors.New("pinned native executable must be a single-link root-owned non-writable executable")
	}
	if kind == harness.KindPi && witness.config.Runtime.HarnessVersion != pi.TestedVersion {
		return errors.New("Pi owner witness requires the tested native release")
	}
	if kind == harness.KindOpenCode && witness.config.Runtime.HarnessVersion != opencode.TestedVersion {
		return errors.New("OpenCode owner witness requires the tested release")
	}
	accounts, err := verifyLinuxOwnerAccounts(witness.ownerConfig)
	if err != nil {
		return err
	}
	if err := verifyLinuxTrustedPathAncestors(profile.Command, 0, 0, &accounts.bSubject); err != nil {
		return fmt.Errorf("native executable has an untrusted parent path: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(profile.Command, accounts.aSubject, 5, 2); err != nil {
		return fmt.Errorf("A cannot execute the pinned native binary without modifying it: %w", err)
	}
	if err := verifyEffectiveLinuxAccess(profile.Command, accounts.bSubject, 5, 2); err != nil {
		return fmt.Errorf("B cannot execute/read the pinned native binary without modifying it: %w", err)
	}
	return verifyLinuxNoUntrustedACLWrite(profile.Command, []uint32{0, witness.ownerConfig.AUID}, []uint32{witness.ownerConfig.AGID}, true)
}

func (witness *linuxV2NativeWitnessRun) prepareClone(
	ctx context.Context,
	daemon *daemon,
	store *state.Store,
) (workspace.Prepared, authority.LinuxCloneGrant, linuxV2CloneWorkspace, error) {
	cloneWorkspace, ok := daemon.workspace.(linuxV2CloneWorkspace)
	if !ok {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, errors.New("owner witness workspace lacks the B-owned git_clone_v2 contract")
	}
	key := state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation}
	runRef := workspace.RunRef{RunID: key.RunID, Generation: key.Generation}
	intent, err := cloneWorkspace.PlanLinuxCloneIntent(daemon.config.Runtime.Workspace, runRef, witness.request.Subject)
	if err != nil {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, fmt.Errorf("plan exact owner witness clone intent: %w", err)
	}
	if err := intent.Validate(); err != nil || intent.RunID != key.RunID || intent.Generation != key.Generation ||
		intent.BindingKey != daemon.config.Runtime.Workspace || intent.ResourceID != witness.request.Subject.ResourceID ||
		intent.BaseCommit != witness.request.Subject.Commit || intent.BaseTreeDigest != witness.request.Subject.TreeDigest {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, errors.Join(errors.New("owner witness clone intent differs from exact RunKey/Subject/runtime"), err)
	}
	callbacks := daemon.linuxCloneJournalCallbacks(key, witness.request.Fence)
	if err := callbacks.Reserve(intent); err != nil {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, fmt.Errorf("reserve owner witness clone before source Git effects: %w", err)
	}
	seed, err := cloneWorkspace.PrepareLinuxCloneSeed(ctx, intent)
	if err != nil {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, fmt.Errorf("prepare A-owned witness clone seed: %w", err)
	}
	if err := seed.Validate(); err != nil || seed.BaseCommit != intent.BaseCommit {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, errors.Join(errors.New("owner witness seed differs from exact source commit"), err)
	}
	if err := callbacks.BindSeed(intent, seed); err != nil {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, fmt.Errorf("bind A-owned witness seed receipt: %w", err)
	}
	grant := authority.LinuxCloneGrant{Intent: intent.Clone(), Seed: seed.Clone()}
	if err := grant.Validate(); err != nil {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, fmt.Errorf("validate exact owner witness clone grant: %w", err)
	}
	if _, err := daemon.loadLinuxCloneRunJournal(key, witness.request.Fence, grant, nil); err != nil {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, fmt.Errorf("read back exact owner witness clone grant: %w", err)
	}
	prepared, err := cloneWorkspace.PrepareLinuxClone(ctx, grant)
	if err != nil {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, fmt.Errorf("prepare A-side owner witness clone metadata: %w", err)
	}
	if prepared.Path != intent.Target || prepared.BindingKey != intent.BindingKey || prepared.Run != runRef {
		return workspace.Prepared{}, authority.LinuxCloneGrant{}, nil, errors.New("prepared owner witness workspace differs from its exact reserved B target")
	}
	return prepared, grant, cloneWorkspace, nil
}

func (witness *linuxV2NativeWitnessRun) launchNative(
	ctx context.Context,
	daemon *daemon,
	store *state.Store,
	kind harness.Kind,
	profile config.AgentProfile,
	owner linuxV2NativeOwner,
	prepared workspace.Prepared,
	grant authority.LinuxCloneGrant,
) (resultErr error) {
	defer func() {
		if witness.request.DropAckAfter != "" {
			return
		}
		witness.mu.Lock()
		process := witness.process
		witness.mu.Unlock()
		if process != nil {
			// The Runner preserves a positive authenticated helper-drop snapshot
			// across owner close. A missing live Status on a no-drop path is not
			// upgraded into evidence.
			_ = witness.captureHelperDropStatus(store, process)
		}
	}()
	key := state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation}
	remaining := time.Until(witness.request.Claim.LeaseExpiresAt)
	claimRemaining := time.Duration(witness.request.Claim.LeaseRemainingMS) * time.Millisecond
	if claimRemaining < remaining {
		remaining = claimRemaining
	}
	remaining -= leaseSafetyMargin
	if remaining <= time.Second {
		return errors.New("authentic Control claim has insufficient lease time for the native witness")
	}
	deadlineAt := time.Now().Add(remaining)
	baseEnvironment, err := execution.BuildEnvironment(profile.EnvAllowlist...)
	if err != nil {
		return errors.New("build explicitly allowlisted witness environment")
	}
	environment, err := mergeEnvironmentOverrides(baseEnvironment, witness.nativeEnvironment)
	if err != nil {
		return fmt.Errorf("merge fixed-B witness environment: %w", err)
	}
	if slices.Contains(profile.EnvAllowlist, linuxV2NativeOwnerWitnessHookEnv) {
		if err := verifyLinuxV2NativeWitnessHookRoot(os.Getenv(linuxV2NativeOwnerWitnessHookEnv), witness.ownerConfig); err != nil {
			return err
		}
	}
	if (witness.request.Case == "helper_sigkill_detached_child" || witness.request.Case == "daemon_sigkill_helper_survives_detached_child") &&
		!slices.Contains(profile.EnvAllowlist, linuxV2NativeOwnerWitnessHookEnv) {
		return errors.New("detached-child witness requires the fixed hook-marker environment allowlist")
	}
	var providerAccess *protocol.ProviderAccess
	if witness.request.Claim.ProviderAccess != nil {
		copy := *witness.request.Claim.ProviderAccess
		copy.Grants = append([]protocol.ProviderGrant(nil), copy.Grants...)
		for index := range copy.Grants {
			copy.Grants[index].Operations = append([]string(nil), copy.Grants[index].Operations...)
		}
		providerAccess = &copy
	}
	var providerBridge *harness.ProviderBridgeLaunch
	if providerAccess != nil {
		providerBridge, err = daemon.preparePiProviderBridge(ctx, key, kind, providerAccess)
		if err != nil {
			return errors.New("prepare durable exact-peer Pi provider bridge")
		}
	}
	bridgeTransferred := false
	defer func() {
		if providerBridge != nil && !bridgeTransferred {
			closeCtx, cancel := context.WithTimeout(context.Background(), piProviderBridgeCloseTimeout)
			resultErr = errors.Join(resultErr, providerBridge.Lifecycle.Close(closeCtx))
			cancel()
		}
	}()

	callbacks := daemon.linuxV2JournalCallbacks(key, witness.request.Fence)
	if err := witness.wrapLinuxV2Callbacks(daemon, store, &callbacks); err != nil {
		return err
	}
	if err := witness.installCloneCallbacks(daemon, store, grant, &callbacks); err != nil {
		return err
	}
	invocation := execution.Invocation{
		Program: profile.Command, Args: append([]string(nil), profile.Args...),
		Dir: prepared.Path, Env: environment, InitialLeaseDeadlineAt: deadlineAt,
		InitialLeaseSequence: 1, LinuxV2Required: true,
		PrepareSupervisorHandoff:           daemon.prepareSupervisorHandoffCallback(key),
		BindSupervisorHandoff:              daemon.bindSupervisorHandoffCallback(key),
		CommitSupervisorHandoff:            daemon.commitSupervisorHandoffCallback(key),
		RecordSupervisorHandoffStopReceipt: daemon.recordSupervisorHandoffStopReceiptCallback(key),
		ClearSupervisorHandoff:             daemon.clearSupervisorHandoffCallback(key),
		PersistProcessWithAuthority:        daemon.persistProcessAuthorityCallback(key),
		PersistContainmentUnproven:         daemon.markContainmentUnprovenCallback(key),
	}
	invocation, err = owner.BuildInvocation(ctx, key, witness.request.Fence, invocation, callbacks)
	if err != nil {
		return errors.New("build explicit App-owned Linux v2 witness invocation")
	}
	cloneGrant := grant.Clone()
	invocation.LinuxV2.CloneGrant = &cloneGrant
	if err := witness.installRunnerBarrierEnvironment(); err != nil {
		return err
	}
	defer witness.restoreRunnerBarrierEnvironment()
	if witness.request.Barrier.HoldAfter == linuxV2NativeWitnessLeaseStage {
		witness.watchRunnerLeaseBarrier(daemon, store)
	}
	adapter, err := daemon.harnessRegistry.Lookup(kind)
	if err != nil {
		return errors.New("lookup configured owner witness native adapter")
	}
	runner := execution.NewRunner()
	daemon.start = func(startContext context.Context, runnerInvocation execution.Invocation, sink execution.Sink) (Process, error) {
		process, startErr := runner.Start(startContext, runnerInvocation, sink)
		if process == nil {
			return nil, startErr
		}
		witness.mu.Lock()
		witness.process = process
		witness.mu.Unlock()
		if witness.request.DropAckAfter == "" {
			_ = witness.captureHelperDropStatus(store, process)
		}
		if startErr != nil {
			pid, identity := process.ProcessDetails()
			witness.mu.Lock()
			witness.summary.Target.PID = pid
			witness.summary.Target.Identity = identity
			witness.summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseUnknown
			witness.mu.Unlock()
			if witness.request.Case == "wsl2_denial" {
				witness.setFailureCategory(classifyLinuxV2NativeWitnessPeerDenial(startErr))
			}
			return process, startErr
		}
		witness.mu.Lock()
		barrierErr := witness.runnerBarrierErr
		witness.mu.Unlock()
		if barrierErr != nil {
			witness.mu.Lock()
			witness.summary.NativeReleaseState = linuxV2NativeWitnessNativeReleaseUnknown
			witness.mu.Unlock()
			return process, barrierErr
		}
		pid, identity := process.ProcessDetails()
		witness.mu.Lock()
		witness.summary.Target.PID = pid
		witness.summary.Target.Identity = identity
		witness.summary.Target.HostUID = witness.ownerConfig.BUID
		witness.summary.Target.SamePIDExec = pid > 0 && identity != ""
		witness.mu.Unlock()
		networkNamespace, namespaceErr := linuxV2NativeWitnessNetworkNamespace(pid)
		if namespaceErr != nil {
			if witness.request.Case == "wsl2_denial" {
				witness.setFailureCategory(classifyLinuxV2NativeWitnessPeerDenial(namespaceErr))
			}
			stageErr := witness.stage(linuxV2NativeWitnessReleaseStage, daemon, store, true)
			if stageErr != nil {
				return process, stageErr
			}
			return process, errors.New("cannot verify exact native network namespace")
		}
		witness.mu.Lock()
		witness.summary.Target.NetworkNamespace = networkNamespace
		witness.mu.Unlock()
		peer, peerErr := process.DuplicateLinuxV2Peer()
		if peerErr != nil {
			if witness.request.Case == "wsl2_denial" {
				witness.setFailureCategory(classifyLinuxV2NativeWitnessPeerDenial(peerErr))
			}
			stageErr := witness.stage(linuxV2NativeWitnessReleaseStage, daemon, store, true)
			if stageErr != nil {
				return process, stageErr
			}
			return process, errors.New("cannot verify launch-transferred native peer pidfd")
		}
		if peer == nil || peer.PID != pid || peer.Identity != identity || peer.PIDFD == nil ||
			peer.UserNamespace.Device == 0 || peer.UserNamespace.Inode == 0 {
			return process, errors.Join(errors.New("Runner pidfd/user namespace differs from the exact B process marker"), peer.Close())
		}
		witness.mu.Lock()
		witness.peer = peer
		witness.summary.Target = linuxV2NativeWitnessTarget{
			PID: pid, Identity: identity, PIDFDVerified: true, HostUID: witness.ownerConfig.BUID,
			UserNamespace:    linuxV2NativeWitnessNamespace{Device: peer.UserNamespace.Device, Inode: peer.UserNamespace.Inode},
			NetworkNamespace: networkNamespace, SamePIDExec: true,
		}
		witness.mu.Unlock()
		if err := witness.stage(linuxV2NativeWitnessReleaseStage, daemon, store, true); err != nil {
			return process, err
		}
		return process, nil
	}

	startContext, cancelStart := context.WithDeadline(ctx, deadlineAt)
	defer cancelStart()
	sink := harness.EventSinkFunc(func(eventContext context.Context, _ harness.Event) error { return eventContext.Err() })
	startRequest := harness.StartRequest{
		Workspace: prepared.Path, Goal: witness.request.Claim.Work.Goal,
		ProviderAccess: providerAccess, ProviderBridge: providerBridge, Invocation: invocation,
		PersistProcess: func(pid int, identity string) error {
			return daemon.persistProcessMarker(key, pid, identity, time.Now().UTC())
		},
		PersistProcessAuthority:       daemon.persistProcessAuthorityCallback(key),
		PersistContainmentStopReceipt: daemon.persistContainmentStopReceiptCallback(key),
	}
	if kind == harness.KindPi {
		startRequest.Context = witness.nativeContext()
	}
	session, startErr := adapter.Start(startContext, startRequest, sink)
	if session != nil {
		bridgeTransferred = providerBridge != nil
	}
	closed := false
	closeSession := func() error {
		if session == nil || closed {
			return nil
		}
		closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		closeErr := session.Close(closeCtx)
		if closeErr == nil {
			closed = true
		}
		return closeErr
	}
	if session != nil {
		defer func() {
			resultErr = errors.Join(resultErr, closeSession())
		}()
	}
	if startErr != nil {
		if witness.request.Case == "wsl2_denial" && witness.summary.FailureCategory == "" {
			witness.setFailureCategory(classifyLinuxV2NativeWitnessPeerDenial(startErr))
		}
		if witness.request.Case != "wsl2_denial" || witness.summary.FailureCategory == "" {
			return errors.New("start native owner witness transport")
		}
	}
	if session == nil {
		if witness.request.Case == "wsl2_denial" && witness.summary.FailureCategory != "" {
			journal, loadErr := store.LoadJournal(key)
			if loadErr == nil && linuxV2NativeWitnessJournalPhysicallyReleased(journal) {
				return nil
			}
		}
		return errors.New("native owner witness adapter returned no cleanup owner")
	}
	staged, ok := session.(harness.StagedSession)
	if !ok {
		return errors.New("native owner witness adapter did not return a staged session")
	}
	if kind == harness.KindOpenCode {
		if startErr == nil {
			witness.updateHealth(func(health *linuxV2NativeWitnessHealth) { health.Attempted = true })
			_, openErr := staged.Open(startContext)
			if openErr != nil {
				witness.setFailureCategory(classifyLinuxV2NativeWitnessPeerDenial(openErr))
				if witness.request.Case != "wsl2_denial" || witness.summary.FailureCategory == "" {
					return errors.New("open exact-peer OpenCode owner witness session")
				}
			} else {
				witness.updateHealth(func(health *linuxV2NativeWitnessHealth) {
					health.Healthy = true
					health.PeerVerified = true
					health.NetworkNamespaceVerified = true
					health.NetworkNamespace = witness.summary.Target.NetworkNamespace
					health.PeerPID = witness.summary.Target.PID
					health.PeerIdentity = witness.summary.Target.Identity
				})
			}
		}
	} else {
		if startErr != nil {
			return errors.New("Pi owner witness transport failed before native Open")
		}
		if _, err := staged.Open(startContext); err != nil {
			return errors.New("open pinned Pi owner witness RPC session")
		}
		witness.startDetachedChildCapture(startContext, daemon, store)
		turnDone := make(chan error, 1)
		go func() {
			turnDone <- staged.StartTurn(startContext, harness.TurnRequest{
				Goal: witness.request.Claim.Work.Goal, Context: witness.nativeContext(),
			})
		}()
		if witness.request.Barrier.HoldAfter == linuxV2NativeWitnessTurnStage &&
			(witness.request.Case == "helper_sigkill_detached_child" || witness.request.Case == "daemon_sigkill_helper_survives_detached_child") {
			if err := witness.waitForDetachedChild(startContext, daemon, store); err != nil {
				cancelStart()
				return err
			}
		}
		witness.mu.Lock()
		witness.summary.ProviderAction.Attempted = true
		witness.mu.Unlock()
		if err := witness.stage(linuxV2NativeWitnessTurnStage, daemon, store, true); err != nil {
			cancelStart()
			return err
		}
		turnErr := <-turnDone
		crashCase := witness.request.Case == "helper_sigkill_detached_child" || witness.request.Case == "daemon_sigkill_helper_survives_detached_child"
		if turnErr != nil && !crashCase {
			return errors.New("run authentic Pi owner witness turn")
		}
		if turnErr == nil {
			if err := staged.WaitTurn(startContext); err != nil && !crashCase {
				return errors.New("wait for authentic Pi owner witness turn")
			}
		}
	}
	if err := closeSession(); err != nil {
		if witness.request.Case != "wsl2_denial" || witness.summary.FailureCategory == "" {
			return errors.New("close native owner witness through v2 containment")
		}
	}
	if process := witness.process; process != nil {
		processResult := process.Wait()
		if processResult.TerminationError != nil || processResult.ContainmentError != nil {
			return errors.New("native owner witness process stop/containment result is unproven")
		}
		if finalizer, ok := process.(ProcessFinalizer); ok {
			if err := finalizer.FinalizeContainment(); err != nil {
				return errors.New("finalize native owner witness containment")
			}
		}
	}
	if witness.request.Case == "wsl2_denial" && witness.summary.FailureCategory == "" {
		return errors.New("WSL2 denial witness did not observe a supported exact peer permission denial")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		return fmt.Errorf("read physically stopped owner witness RunJournal: %w", err)
	}
	if journal.ContainmentCgroupReleaseProof == nil || journal.ContainmentCgroupReleaseProof.Validate() != nil ||
		journal.ContainmentCgroupReleaseProof.Version != authority.SupervisorHandoffVersionV2 ||
		journal.ContainmentCgroup != nil || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil || journal.HasProcessDetails() {
		return errors.New("owner witness stop lacks the exact cleared v2 release tombstone")
	}
	witness.summary.Release.AnchorRetired = true
	return nil
}

func (witness *linuxV2NativeWitnessRun) nativeContext() json.RawMessage {
	encoded, _ := json.Marshal(struct {
		Version int                        `json:"version"`
		Subject protocol.Subject           `json:"subject"`
		RunKey  linuxV2NativeWitnessRunKey `json:"run_key"`
	}{Version: 1, Subject: witness.request.Subject, RunKey: witness.request.RunKey})
	return encoded
}

func (witness *linuxV2NativeWitnessRun) installRunnerBarrierEnvironment() error {
	linuxV2NativeWitnessRunnerEnvironment.Lock()
	keys := []string{
		"SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS",
		"SYMMETRY_RUNNER_COMMIT_RESUME_BARRIER_REACHED_FILE",
		"SYMMETRY_RUNNER_COMMIT_RESUME_BARRIER_RELEASE_FILE",
		"SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_REACHED_FILE",
		"SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_RELEASE_FILE",
		"SYMMETRY_LINUX_SUPERVISOR_STOP_RECEIPT_BARRIER_REACHED_FILE",
		"SYMMETRY_LINUX_SUPERVISOR_STOP_RECEIPT_BARRIER_RELEASE_FILE",
	}
	previous := make(map[string]struct {
		value string
		set   bool
	}, len(keys))
	for _, key := range keys {
		value, set := os.LookupEnv(key)
		previous[key] = struct {
			value string
			set   bool
		}{value: value, set: set}
		if err := os.Unsetenv(key); err != nil {
			for restoreKey, old := range previous {
				if old.set {
					_ = os.Setenv(restoreKey, old.value)
				} else {
					_ = os.Unsetenv(restoreKey)
				}
			}
			linuxV2NativeWitnessRunnerEnvironment.Unlock()
			return errors.New("clear inherited process-witness environment")
		}
	}
	witness.runnerBarrierRestore = func() {
		for key, old := range previous {
			if old.set {
				_ = os.Setenv(key, old.value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
		linuxV2NativeWitnessRunnerEnvironment.Unlock()
	}
	if witness.request.Barrier.HoldAfter != linuxV2NativeWitnessLeaseStage {
		return nil
	}
	if witness.request.Barrier.TimeoutSeconds != linuxV2NativeOwnerWitnessTimeoutMax {
		witness.restoreRunnerBarrierEnvironment()
		return errors.New("Runner lease-armed barrier requires its fixed 30-second timeout")
	}
	reached := filepath.Join(witness.request.Barrier.Directory, ".runner-lease-armed")
	if _, err := os.Lstat(reached); !errors.Is(err, os.ErrNotExist) {
		witness.restoreRunnerBarrierEnvironment()
		return errors.New("Runner lease-armed reached marker already exists or cannot be inspected")
	}
	if err := os.Setenv("SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS", "1"); err != nil {
		witness.restoreRunnerBarrierEnvironment()
		return errors.New("enable bounded Runner lease witness barrier")
	}
	if err := os.Setenv("SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_REACHED_FILE", reached); err != nil {
		witness.restoreRunnerBarrierEnvironment()
		return errors.New("configure Runner lease witness reached marker")
	}
	if err := os.Setenv("SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_RELEASE_FILE", witness.request.Barrier.ReleaseFile); err != nil {
		witness.restoreRunnerBarrierEnvironment()
		return errors.New("configure Runner lease witness release marker")
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) restoreRunnerBarrierEnvironment() {
	if witness.runnerBarrierRestore == nil {
		return
	}
	restore := witness.runnerBarrierRestore
	witness.runnerBarrierRestore = nil
	restore()
}

func (witness *linuxV2NativeWitnessRun) watchRunnerLeaseBarrier(daemon *daemon, store *state.Store) {
	reached := filepath.Join(witness.request.Barrier.Directory, ".runner-lease-armed")
	go func() {
		timer := time.NewTimer(time.Duration(witness.request.Barrier.TimeoutSeconds) * time.Second)
		defer timer.Stop()
		ticker := time.NewTicker(linuxV2NativeOwnerWitnessPoll)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(reached); err == nil {
				err = witness.stage(linuxV2NativeWitnessLeaseStage, daemon, store, false)
				witness.mu.Lock()
				witness.runnerBarrierErr = err
				witness.mu.Unlock()
				return
			} else if !errors.Is(err, os.ErrNotExist) {
				witness.mu.Lock()
				witness.runnerBarrierErr = errors.New("inspect Runner lease-armed witness marker")
				witness.mu.Unlock()
				return
			}
			select {
			case <-witness.ctx.Done():
				return
			case <-timer.C:
				witness.mu.Lock()
				witness.runnerBarrierErr = errors.New("Runner did not reach the initial lease-ack barrier before its bound")
				witness.mu.Unlock()
				return
			case <-ticker.C:
			}
		}
	}()
}

func (witness *linuxV2NativeWitnessRun) wrapLinuxV2Callbacks(
	daemon *daemon,
	store *state.Store,
	callbacks *execution.LinuxV2JournalCallbacks,
) error {
	if callbacks == nil {
		return errors.New("owner witness Linux v2 callback set is unavailable")
	}
	stageAfter := func(stage string, already bool, callbackErr error) error {
		if callbackErr != nil {
			return callbackErr
		}
		if err := witness.stage(stage, daemon, store, true); err != nil {
			return err
		}
		return witness.dropAckAfterStage(stage, already)
	}
	reserve := callbacks.Reserve
	callbacks.Reserve = func(value state.LinuxCgroupReservation) error {
		return stageAfter(linuxV2NativeWitnessReserveStage, false, reserve(value))
	}
	markCreate := callbacks.MarkCreateStarted
	callbacks.MarkCreateStarted = func(value state.LinuxCgroupReservation) error {
		return stageAfter(linuxV2NativeWitnessCreateStage, false, markCreate(value))
	}
	bindLeaf := callbacks.BindLeaf
	callbacks.BindLeaf = func(value state.LinuxCgroupReservation, identity authority.LinuxCgroupIdentity) error {
		return stageAfter(linuxV2NativeWitnessLeafStage, false, bindLeaf(value, identity))
	}
	bindHelper := callbacks.BindHelper
	callbacks.BindHelper = func(value state.LinuxCgroupReservation, identity authority.LinuxCgroupProcessIdentity) error {
		already, err := witness.stageEffectAlreadyPresent(store, linuxV2NativeWitnessHelperStage)
		if err != nil {
			return err
		}
		return stageAfter(linuxV2NativeWitnessHelperStage, already, bindHelper(value, identity))
	}
	bindLauncher := callbacks.BindLauncher
	callbacks.BindLauncher = func(value state.LinuxCgroupReservation, identity authority.LinuxCgroupProcessIdentity) error {
		already, err := witness.stageEffectAlreadyPresent(store, linuxV2NativeWitnessLauncherStage)
		if err != nil {
			return err
		}
		return stageAfter(linuxV2NativeWitnessLauncherStage, already, bindLauncher(value, identity))
	}
	bindGate := callbacks.BindGate
	callbacks.BindGate = func(value state.LinuxCgroupReservation, identity authority.LinuxCgroupGateIdentity) error {
		already, err := witness.stageEffectAlreadyPresent(store, linuxV2NativeWitnessGateStage)
		if err != nil {
			return err
		}
		return stageAfter(linuxV2NativeWitnessGateStage, already, bindGate(value, identity))
	}
	prepare := callbacks.PrepareHandoff
	callbacks.PrepareHandoff = func(value authority.SupervisorHandoff) error {
		already, err := witness.stageEffectAlreadyPresent(store, linuxV2NativeWitnessPrepareStage)
		if err != nil {
			return err
		}
		return stageAfter(linuxV2NativeWitnessPrepareStage, already, prepare(value))
	}
	commit := callbacks.CommitHandoff
	callbacks.CommitHandoff = func(value authority.SupervisorHandoff, at time.Time) error {
		already, err := witness.stageEffectAlreadyPresent(store, linuxV2NativeWitnessCommitStage)
		if err != nil {
			return err
		}
		return stageAfter(linuxV2NativeWitnessCommitStage, already, commit(value, at))
	}
	recordReceipt := callbacks.RecordStopReceipt
	callbacks.RecordStopReceipt = func(handoff authority.SupervisorHandoff, receipt authority.StopReceipt) error {
		already, err := witness.stageEffectAlreadyPresent(store, linuxV2NativeWitnessStopReceiptStage)
		if err != nil {
			return err
		}
		return stageAfter(linuxV2NativeWitnessStopReceiptStage, already, recordReceipt(handoff, receipt))
	}
	clearHandoff := callbacks.ClearHandoffV2
	callbacks.ClearHandoffV2 = func(handoff authority.SupervisorHandoff, release platform.LinuxCgroupV2Release) error {
		proof, issued := release.Snapshot()
		if !issued || proof.Validate() != nil || proof.StopReceipt == nil || proof.CgroupIdentity == nil {
			return errors.New("witness leaf-removal barrier requires the exact opaque platform release")
		}
		witness.noteOpaqueRelease(proof.Version, proof.LeafDisposition)
		return witness.clearAfterLeafRemoval(daemon, store, func() error {
			return clearHandoff(handoff, release)
		})
	}
	clearProcess := callbacks.ClearProcessDetailsV2
	callbacks.ClearProcessDetailsV2 = func(pid int, identity string, release platform.LinuxCgroupV2Release) error {
		proof, issued := release.Snapshot()
		if !issued || proof.Validate() != nil || proof.StopReceipt == nil || proof.CgroupIdentity == nil ||
			proof.TargetPID != pid || proof.TargetIdentity != identity {
			return errors.New("witness leaf-removal barrier requires the exact opaque platform release for the target")
		}
		witness.noteOpaqueRelease(proof.Version, proof.LeafDisposition)
		return witness.clearAfterLeafRemoval(daemon, store, func() error {
			return clearProcess(pid, identity, release)
		})
	}
	clearReservation := callbacks.ClearReservationV2
	callbacks.ClearReservationV2 = func(reservation state.LinuxCgroupReservation, release platform.LinuxCgroupV2Release) error {
		proof, issued := release.SnapshotPreGate()
		if !issued || proof.Validate() != nil {
			return errors.New("witness pre-gate removal barrier requires the exact opaque platform release")
		}
		witness.noteOpaqueRelease(proof.Version, proof.LeafDisposition)
		return witness.clearAfterLeafRemoval(daemon, store, func() error {
			return clearReservation(reservation, release)
		})
	}
	callbacks.DropAckAfter = func(stage string, ownerEffectAlreadyAckedOnEntry bool) error {
		if (stage != linuxV2NativeWitnessLeaseStage && stage != linuxV2NativeWitnessReleaseStage) ||
			witness.request.DropAckAfter != stage || ownerEffectAlreadyAckedOnEntry {
			return nil
		}
		if err := witness.stage(stage, daemon, store, true); err != nil {
			return err
		}
		return witness.dropAckAfterStage(stage, ownerEffectAlreadyAckedOnEntry)
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) clearAfterLeafRemoval(daemon *daemon, store *state.Store, clear func() error) error {
	already, err := witness.stageEffectAlreadyPresent(store, linuxV2NativeWitnessClearedStage)
	if err != nil {
		return err
	}
	return runLinuxV2WitnessReleaseClearCallbacks(
		func() error { return witness.stage(linuxV2NativeWitnessLeafRemovedStage, daemon, store, true) },
		clear,
		func() error {
			if err := witness.markJournalClearedStage(daemon, store); err != nil {
				return err
			}
			return witness.dropAckAfterStage(linuxV2NativeWitnessClearedStage, already)
		},
	)
}

func (witness *linuxV2NativeWitnessRun) stageEffectAlreadyPresent(store *state.Store, stage string) (bool, error) {
	if witness.request.DropAckAfter != stage {
		return false, nil
	}
	key := state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation}
	journal, err := store.LoadJournal(key)
	if err != nil {
		return false, fmt.Errorf("read exact RunJournal before %s response-loss injection: %w", stage, err)
	}
	if !linuxV2JournalMatchesFence(journal, key, witness.request.Fence) {
		return false, errors.New("response-loss injection RunJournal differs from its exact RunKey/fence")
	}
	switch stage {
	case linuxV2NativeWitnessHelperStage:
		return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Helper != nil, nil
	case linuxV2NativeWitnessLauncherStage:
		return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Launcher != nil, nil
	case linuxV2NativeWitnessGateStage:
		return journal.ContainmentCgroup != nil && journal.ContainmentCgroup.Gate != nil ||
			journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil, nil
	case linuxV2NativeWitnessPrepareStage:
		return journal.ContainmentHandoff != nil || journal.ContainmentAuthority != nil ||
			journal.ContainmentCgroupReleaseProof != nil, nil
	case linuxV2NativeWitnessCommitStage:
		return journal.ContainmentAuthority != nil || journal.ContainmentCgroupReleaseProof != nil, nil
	case linuxV2NativeWitnessCheckoutStage:
		return journal.LinuxCloneCheckoutReceipt != nil, nil
	case linuxV2NativeWitnessStopReceiptStage:
		return journal.ContainmentHandoff != nil && journal.ContainmentHandoff.StopReceipt != nil ||
			journal.ContainmentAuthority != nil && journal.ContainmentAuthority.StopReceipt != nil ||
			journal.ContainmentCgroupReleaseProof != nil && journal.ContainmentCgroupReleaseProof.StopReceipt != nil, nil
	case linuxV2NativeWitnessClearedStage:
		return !journal.HasProcessDetails() && journal.ContainmentCgroup == nil && journal.ContainmentHandoff == nil &&
			journal.ContainmentAuthority == nil && (journal.ContainmentCgroupReleaseProof != nil ||
			journal.ContainmentCgroupPreGateReleaseProof != nil), nil
	default:
		return false, nil
	}
}

func (witness *linuxV2NativeWitnessRun) dropAckAfterStage(stage string, already bool) error {
	if witness.request.DropAckAfter != stage || already {
		return nil
	}
	if !linuxV2NativeWitnessAckLossSupported(stage) {
		return errors.New("unsupported Linux v2 ACK-loss injection stage")
	}
	return execution.ErrLinuxV2MutationAckLost
}

func runLinuxV2WitnessReleaseClearCallbacks(stageLeafRemoved, clear, stageJournalCleared func() error) error {
	if err := stageLeafRemoved(); err != nil {
		return err
	}
	if err := clear(); err != nil {
		return err
	}
	return stageJournalCleared()
}

func (witness *linuxV2NativeWitnessRun) installCloneCallbacks(
	daemon *daemon,
	store *state.Store,
	grant authority.LinuxCloneGrant,
	callbacks *execution.LinuxV2JournalCallbacks,
) error {
	if callbacks == nil || witness.cloneWorkspace == nil || grant.Validate() != nil ||
		grant.Intent.RunID != witness.request.RunKey.RunID || grant.Intent.Generation != witness.request.RunKey.Generation ||
		grant.Intent.ResourceID != witness.request.Subject.ResourceID || grant.Intent.BaseCommit != witness.request.Subject.Commit ||
		grant.Intent.BaseTreeDigest != witness.request.Subject.TreeDigest {
		return errors.New("owner witness clone callbacks differ from the exact RunKey/Subject grant")
	}
	key := state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation}
	fence := witness.request.Fence
	cloneCallbacks := daemon.linuxCloneJournalCallbacks(key, fence)
	callbacks.RecordLinuxCloneCheckoutReady = func(receipt authority.LinuxCloneCheckoutReceipt) error {
		already, err := witness.stageEffectAlreadyPresent(store, linuxV2NativeWitnessCheckoutStage)
		if err != nil {
			return err
		}
		if err := witness.cloneWorkspace.ValidateLinuxCloneCheckout(grant, receipt); err != nil {
			return fmt.Errorf("validate exact B-created witness checkout before READY: %w", err)
		}
		if err := cloneCallbacks.CheckoutReady(grant.Intent, receipt); err != nil {
			return err
		}
		journal, err := daemon.loadLinuxCloneRunJournal(key, fence, grant, &receipt)
		if err != nil || journal.LinuxCloneCheckoutReceipt == nil || !journal.LinuxCloneCheckoutReceipt.Equal(receipt) {
			return errors.Join(errors.New("owner witness checkout READY did not read back exactly"), err)
		}
		witness.mu.Lock()
		witness.summary.Clone.CheckoutReady = true
		witness.summary.Clone.CheckoutDevice = receipt.TargetDevice
		witness.summary.Clone.CheckoutInode = receipt.TargetInode
		witness.mu.Unlock()
		if err := witness.stage(linuxV2NativeWitnessCheckoutStage, daemon, store, true); err != nil {
			return err
		}
		return witness.dropAckAfterStage(linuxV2NativeWitnessCheckoutStage, already)
	}
	callbacks.LinuxCloneProposedCommit = func(context.Context, execution.LinuxCloneRunOutcome) (string, bool, error) {
		// This witness never classifies native output as Goal progress or a
		// candidate artifact; no A-owned source ref may be promoted here.
		return "", false, nil
	}
	callbacks.RetainUnpromotedLinuxClone = func(ctx context.Context) error {
		if ctx == nil {
			return errors.New("owner witness clone-retention context is nil")
		}
		journal, err := store.LoadJournal(key)
		if err != nil || !linuxV2JournalMatchesFence(journal, key, fence) || journal.LinuxCloneIntent == nil ||
			!journal.LinuxCloneIntent.Equal(grant.Intent) || journal.LinuxCloneSeedReceipt == nil ||
			!journal.LinuxCloneSeedReceipt.Equal(grant.Seed) || journal.LinuxClonePromotionIntent != nil ||
			journal.LinuxClonePromotionReceipt != nil ||
			!linuxCloneDurableStopReady(journal) {
			return errors.Join(errors.New("owner witness clone cannot be retained without exact durable stop evidence"), err)
		}
		return daemon.persistLinuxV2Mutation(store, key, fence, "retain isolated owner witness clone", func() (state.RunJournal, error) {
			return store.RetainWorkspace(key)
		}, func(current state.RunJournal) bool {
			return current.LinuxCloneIntent != nil && current.LinuxCloneIntent.Equal(grant.Intent) &&
				current.LinuxCloneSeedReceipt != nil && current.LinuxCloneSeedReceipt.Equal(grant.Seed) &&
				current.LinuxClonePromotionIntent == nil && current.LinuxClonePromotionReceipt == nil && current.RetainWorkspace
		})
	}
	callbacks.PromoteLinuxClone = func(context.Context, authority.LinuxCloneGrant, authority.LinuxCloneCheckoutReceipt, string, authority.StopReceipt, platform.LinuxCgroupV2Release) (authority.LinuxClonePromotionReceipt, error) {
		return authority.LinuxClonePromotionReceipt{}, errors.New("isolated owner witness never promotes native work")
	}
	callbacks.RecordLinuxClonePromotion = func(receipt authority.LinuxClonePromotionReceipt) error {
		return errors.New("isolated owner witness never records native promotion")
	}
	callbacks.FinalizeNoCheckoutLinuxCloneAfterRetirement = func(ctx context.Context, intent authority.LinuxCloneIntent, seed *authority.LinuxCloneSeedReceipt, preGate *authority.LinuxCgroupPreGateReleaseProof, abort *authority.LinuxCgroupReservationAbortProof) error {
		if !intent.Equal(grant.Intent) || seed != nil && !seed.Equal(grant.Seed) {
			return errors.New("owner witness no-checkout cleanup differs from its exact clone grant")
		}
		return daemon.abortLinuxV2CloneAfterRetirement(ctx, key, fence, intent, seed, preGate, abort)
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) markJournalClearedStage(daemon *daemon, store *state.Store) error {
	journal, err := store.LoadJournal(state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation})
	if err != nil {
		return fmt.Errorf("read owner witness after exact v2 clear: %w", err)
	}
	if journal.HasProcessDetails() || journal.ContainmentCgroup != nil || journal.ContainmentAuthority != nil || journal.ContainmentHandoff != nil {
		return nil
	}
	return witness.stage(linuxV2NativeWitnessClearedStage, daemon, store, true)
}

func (witness *linuxV2NativeWitnessRun) noteOpaqueRelease(version int, disposition string) {
	witness.mu.Lock()
	witness.summary.Release = linuxV2NativeWitnessRelease{
		Version: version, LeafDisposition: disposition, OpaquePlatformRelease: true,
		LeafRemoved: disposition == authority.LinuxCgroupLeafRemoved ||
			disposition == authority.LinuxCgroupLeafAbsentAfterReceipt,
	}
	witness.mu.Unlock()
}

func (witness *linuxV2NativeWitnessRun) validateFreshClaim(now time.Time) error {
	remaining := time.Until(witness.request.Claim.LeaseExpiresAt)
	claimRemaining := time.Duration(witness.request.Claim.LeaseRemainingMS) * time.Millisecond
	if claimRemaining < remaining {
		remaining = claimRemaining
	}
	remaining -= leaseSafetyMargin
	if remaining <= time.Second || witness.request.Claim.LeaseExpiresAt.Before(now.Add(leaseSafetyMargin+time.Second)) {
		return errors.New("authentic Control ClaimResponse lease is expired or too close to expiry for the bounded native witness")
	}
	if witness.request.Barrier.HoldAfter != "none" && remaining <= time.Duration(witness.request.Barrier.TimeoutSeconds)*time.Second {
		return errors.New("authentic Control claim lease cannot cover the bounded witness barrier")
	}
	return nil
}

func (witness *linuxV2NativeWitnessRun) seedAuthenticClaim(store *state.Store) error {
	key := state.RunKey{RunID: witness.request.RunKey.RunID, Generation: witness.request.RunKey.Generation}
	intent := state.ClaimIntent{
		Key: key, RuntimeKey: witness.config.Runtime.RuntimeKey,
		RuntimeID: witness.request.Fence.RuntimeID, RuntimeEpoch: witness.request.Fence.RuntimeEpoch,
		ClaimID: witness.request.Fence.ClaimID, LocalState: "claiming",
		Work: witness.request.Claim.Work, WorkspaceBindingKey: witness.config.Runtime.Workspace,
	}
	writeIntent := func() (state.RunJournal, error) { return store.SaveClaimIntent(intent) }
	if _, err := writeIntent(); err != nil {
		if _, replayErr := writeIntent(); replayErr != nil {
			return errors.Join(errors.New("persist authentic owner witness claim intent"), replayErr)
		}
	}
	// Rewriting the exact lifecycle state supplies a fresh persistence barrier
	// if SaveClaimIntent reported an unknown post-rename result.
	if _, err := store.SetLocalState(key, "claiming"); err != nil {
		if _, replayErr := store.SetLocalState(key, "claiming"); replayErr != nil {
			return errors.Join(errors.New("persist owner witness claim-intent barrier"), err, replayErr)
		}
	}
	if journal, err := store.LoadJournal(key); err != nil || journal.RuntimeID != intent.RuntimeID ||
		journal.ClaimedRuntimeEpoch != intent.RuntimeEpoch || journal.ClaimID != intent.ClaimID ||
		journal.LocalState != "claiming" || !linuxV2WitnessWorkEqual(journal.Work, intent.Work) {
		return errors.Join(errors.New("owner witness claim-intent readback differs from the authentic request"), err)
	}
	grant := witness.request.Claim
	writeGrant := func() (state.RunJournal, error) { return store.SaveClaimGrant(key, grant) }
	if _, err := writeGrant(); err != nil {
		if _, replayErr := writeGrant(); replayErr != nil {
			return errors.Join(errors.New("persist authentic owner witness claim grant"), err, replayErr)
		}
	}
	journal, err := store.LoadJournal(key)
	if err != nil || journal.RuntimeID != intent.RuntimeID || journal.ClaimedRuntimeEpoch != intent.RuntimeEpoch ||
		journal.ClaimID != intent.ClaimID || journal.LeaseToken != witness.request.Fence.LeaseToken ||
		!journal.LeaseExpiresAt.Equal(grant.LeaseExpiresAt) || journal.LocalState != "claimed" || !linuxV2WitnessWorkEqual(journal.Work, grant.Work) {
		return errors.Join(errors.New("owner witness claim-grant readback differs from the authentic Control response"), err)
	}
	return nil
}
