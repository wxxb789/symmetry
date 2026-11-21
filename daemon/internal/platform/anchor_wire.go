//go:build linux && amd64

package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

type linuxOwnerAnchorRequest struct {
	Version             int                                         `json:"version"`
	Operation           string                                      `json:"operation"`
	ClientPID           int                                         `json:"client_pid"`
	ClientIdentity      string                                      `json:"client_identity"`
	Grant               LinuxOwnerAnchorGrant                       `json:"grant"`
	Helper              *authority.LinuxCgroupProcessIdentity       `json:"helper,omitempty"`
	Gate                *authority.LinuxCgroupGateIdentity          `json:"gate,omitempty"`
	UserNamespace       *LinuxUserNamespaceIdentity                 `json:"user_namespace,omitempty"`
	StopFence           *LinuxOwnerAnchorProducerFence              `json:"stop_fence,omitempty"`
	Creator             *authority.LinuxCgroupProcessIdentity       `json:"creator,omitempty"`
	Process             *authority.LinuxCgroupProcessIdentity       `json:"process,omitempty"`
	Role                string                                      `json:"role,omitempty"`
	ReleaseProof        *authority.SupervisorHandoffReleaseProof    `json:"release_proof,omitempty"`
	PreGateReleaseProof *authority.LinuxCgroupPreGateReleaseProof   `json:"pre_gate_release_proof,omitempty"`
	AbortProof          *authority.LinuxCgroupReservationAbortProof `json:"abort_proof,omitempty"`
	RunKey              LinuxOwnerAnchorRunKey                      `json:"run_key,omitempty"`
	Fence               protocol.Fence                              `json:"fence,omitempty"`
	FenceDigest         string                                      `json:"fence_digest,omitempty"`
	AfterPID            int                                         `json:"after_pid,omitempty"`
	CloneGrant          *authority.LinuxCloneGrant                  `json:"clone_grant,omitempty"`
	Cleanup             *authority.LinuxCloneCleanupIntent          `json:"cleanup,omitempty"`
	PriorRemovalProof   *authority.LinuxCloneTargetRemovalProof     `json:"prior_removal_proof,omitempty"`
	TargetFDPresent     bool                                        `json:"target_fd_present,omitempty"`
}

type linuxOwnerAnchorResponse struct {
	Version             int                                     `json:"version"`
	Operation           string                                  `json:"operation"`
	Status              string                                  `json:"status"`
	RunKey              LinuxOwnerAnchorRunKey                  `json:"run_key"`
	Fence               protocol.Fence                          `json:"fence"`
	FenceDigest         string                                  `json:"fence_digest,omitempty"`
	LaunchToken         string                                  `json:"launch_token"`
	Anchor              authority.LinuxCgroupProcessIdentity    `json:"anchor"`
	Parent              authority.LinuxCgroupParentIdentity     `json:"parent"`
	OwnerUID            uint32                                  `json:"owner_uid"`
	ManagerPath         string                                  `json:"manager_path"`
	Helper              *authority.LinuxCgroupProcessIdentity   `json:"helper,omitempty"`
	ChildClassification *linuxOwnerAnchorRunChildClassification `json:"child_classification,omitempty"`
	Evidence            *LinuxOwnerAnchorReapEvidence           `json:"evidence,omitempty"`
	ReapEvidence        []LinuxOwnerAnchorReapEvidence          `json:"reap_evidence,omitempty"`
	StopEvidence        *LinuxOwnerAnchorStopEvidence           `json:"stop_evidence,omitempty"`
	HelperExit          *authority.LinuxCgroupExitProof         `json:"helper_exit,omitempty"`
	NextPID             int                                     `json:"next_pid,omitempty"`
	More                bool                                    `json:"more,omitempty"`
	CloneRemoval        *authority.LinuxCloneTargetRemovalProof `json:"clone_removal_proof,omitempty"`
}

func (response linuxOwnerAnchorResponse) matches(grant LinuxOwnerAnchorGrant, operation string) bool {
	return response.Version == linuxOwnerAnchorProtocolVersion && response.Operation == operation &&
		response.RunKey == grant.RunKey && response.Fence == grant.Fence && response.LaunchToken == grant.LaunchToken &&
		response.Anchor.Equal(grant.Anchor) && response.Parent.Equal(grant.Parent) &&
		response.OwnerUID == grant.OwnerUID && response.ManagerPath == grant.Parent.Path
}

// linuxOwnerAnchorRequestField names one presence-bearing optional request
// field so the per-operation shape table can state its required and forbidden
// fields declaratively.
type linuxOwnerAnchorRequestField string

const (
	linuxOwnerAnchorFieldGrant               linuxOwnerAnchorRequestField = "grant"
	linuxOwnerAnchorFieldHelper              linuxOwnerAnchorRequestField = "helper"
	linuxOwnerAnchorFieldGate                linuxOwnerAnchorRequestField = "gate"
	linuxOwnerAnchorFieldUserNamespace       linuxOwnerAnchorRequestField = "user_namespace"
	linuxOwnerAnchorFieldStopFence           linuxOwnerAnchorRequestField = "stop_fence"
	linuxOwnerAnchorFieldCreator             linuxOwnerAnchorRequestField = "creator"
	linuxOwnerAnchorFieldProcess             linuxOwnerAnchorRequestField = "process"
	linuxOwnerAnchorFieldRole                linuxOwnerAnchorRequestField = "role"
	linuxOwnerAnchorFieldReleaseProof        linuxOwnerAnchorRequestField = "release_proof"
	linuxOwnerAnchorFieldPreGateReleaseProof linuxOwnerAnchorRequestField = "pre_gate_release_proof"
	linuxOwnerAnchorFieldAbortProof          linuxOwnerAnchorRequestField = "abort_proof"
	linuxOwnerAnchorFieldRunKey              linuxOwnerAnchorRequestField = "run_key"
	linuxOwnerAnchorFieldFence               linuxOwnerAnchorRequestField = "fence"
	linuxOwnerAnchorFieldFenceDigest         linuxOwnerAnchorRequestField = "fence_digest"
	linuxOwnerAnchorFieldAfterPID            linuxOwnerAnchorRequestField = "after_pid"
	linuxOwnerAnchorFieldCloneGrant          linuxOwnerAnchorRequestField = "clone_grant"
	linuxOwnerAnchorFieldCleanup             linuxOwnerAnchorRequestField = "cleanup"
	linuxOwnerAnchorFieldPriorRemovalProof   linuxOwnerAnchorRequestField = "prior_removal_proof"
	linuxOwnerAnchorFieldTargetFDPresent     linuxOwnerAnchorRequestField = "target_fd_present"
)

// fieldPresent reports whether one optional request field carries a value. The
// zero value of a field is absent, exactly as the pre-table checks treated it.
func (request linuxOwnerAnchorRequest) fieldPresent(field linuxOwnerAnchorRequestField) bool {
	switch field {
	case linuxOwnerAnchorFieldGrant:
		return request.Grant != (LinuxOwnerAnchorGrant{})
	case linuxOwnerAnchorFieldHelper:
		return request.Helper != nil
	case linuxOwnerAnchorFieldGate:
		return request.Gate != nil
	case linuxOwnerAnchorFieldUserNamespace:
		return request.UserNamespace != nil
	case linuxOwnerAnchorFieldStopFence:
		return request.StopFence != nil
	case linuxOwnerAnchorFieldCreator:
		return request.Creator != nil
	case linuxOwnerAnchorFieldProcess:
		return request.Process != nil
	case linuxOwnerAnchorFieldRole:
		return request.Role != ""
	case linuxOwnerAnchorFieldReleaseProof:
		return request.ReleaseProof != nil
	case linuxOwnerAnchorFieldPreGateReleaseProof:
		return request.PreGateReleaseProof != nil
	case linuxOwnerAnchorFieldAbortProof:
		return request.AbortProof != nil
	case linuxOwnerAnchorFieldRunKey:
		return request.RunKey != (LinuxOwnerAnchorRunKey{})
	case linuxOwnerAnchorFieldFence:
		return request.Fence != (protocol.Fence{})
	case linuxOwnerAnchorFieldFenceDigest:
		return request.FenceDigest != ""
	case linuxOwnerAnchorFieldAfterPID:
		return request.AfterPID != 0
	case linuxOwnerAnchorFieldCloneGrant:
		return request.CloneGrant != nil
	case linuxOwnerAnchorFieldCleanup:
		return request.Cleanup != nil
	case linuxOwnerAnchorFieldPriorRemovalProof:
		return request.PriorRemovalProof != nil
	case linuxOwnerAnchorFieldTargetFDPresent:
		return request.TargetFDPresent
	default:
		return false
	}
}

// linuxOwnerAnchorTimeoutClass is the deadline class one operation shares
// between the server connection deadline and the client exchange timeout.
type linuxOwnerAnchorTimeoutClass int

const (
	// linuxOwnerAnchorTimeoutRequest is the zero value, and also the class of an
	// operation the table does not cover.
	linuxOwnerAnchorTimeoutRequest linuxOwnerAnchorTimeoutClass = iota
	linuxOwnerAnchorTimeoutWait
	linuxOwnerAnchorTimeoutCleanup
)

func (class linuxOwnerAnchorTimeoutClass) duration() time.Duration {
	switch class {
	case linuxOwnerAnchorTimeoutWait:
		return linuxOwnerAnchorWaitTimeout
	case linuxOwnerAnchorTimeoutCleanup:
		return linuxOwnerAnchorCleanupTimeout
	default:
		return linuxOwnerAnchorRequestTimeout
	}
}

// linuxOwnerAnchorOperationShape is the declarative per-operation request
// contract of the anchor control socket, and the single source both sides read:
// the server enforces the field-presence sets, the received descriptor count and
// the timeout class; the client enforces the descriptor counts it may transfer
// and the timeout class it must use. The presence sets reproduce the pre-table
// checks exactly, including the optional fields the base accepted on operations
// that never inspected them.
type linuxOwnerAnchorOperationShape struct {
	operation string

	// required and forbidden are the presence rules of the operation.
	required  []linuxOwnerAnchorRequestField
	forbidden []linuxOwnerAnchorRequestField

	// serverRequiredFDs is the exact count of received descriptors;
	// serverRequiredFDsWithTarget is used when TargetFDPresent is set
	// (remove-b-clone-after-receipt's variable count).
	serverRequiredFDs           int
	serverRequiredFDsWithTarget int

	// clientSendFDs is the count of descriptors the client transfers;
	// clientSendFDsWithTarget is used when TargetFDPresent is set.
	// clientCheckedFDs records whether exchange enforces the count (the three
	// operations that check it today) and clientFDsMismatch is their error text.
	clientSendFDs           int
	clientSendFDsWithTarget int
	clientCheckedFDs        bool
	clientFDsMismatch       string

	// responseFDs is the descriptor count a non-rejection response carries; it
	// is -1 when the count depends on the status (lookup-helper-v2 carries three
	// descriptors for attached and two for helper_exited).
	responseFDs int

	// timeoutClass is the deadline class both sides use.
	timeoutClass linuxOwnerAnchorTimeoutClass

	// stoppingAnswer records whether a stopping anchor answers this operation
	// with anchor_stopping before the per-operation shape gate runs.
	stoppingAnswer bool

	// runHelperPeer records whether the operation requires the request helper to
	// be the exact registered run helper of the authenticated peer.
	runHelperPeer bool

	// shape carries the relational rules a presence set cannot express; it may
	// assume every required field is present.
	shape func(linuxOwnerAnchorRequest) bool
}

func (row linuxOwnerAnchorOperationShape) forbids(field linuxOwnerAnchorRequestField) bool {
	for _, candidate := range row.forbidden {
		if candidate == field {
			return true
		}
	}
	return false
}

// presenceRejected reports whether a forbidden field is present or a required
// field is absent.
func (row linuxOwnerAnchorOperationShape) presenceRejected(request linuxOwnerAnchorRequest) bool {
	for _, field := range row.forbidden {
		if request.fieldPresent(field) {
			return true
		}
	}
	for _, field := range row.required {
		if !request.fieldPresent(field) {
			return true
		}
	}
	return false
}

// descriptorRejected reports whether the descriptor count differs from the row's
// expectation for this request, including the TargetFDPresent-dependent count of
// remove-b-clone-after-receipt.
func (row linuxOwnerAnchorOperationShape) descriptorRejected(request linuxOwnerAnchorRequest, descriptors int) bool {
	expected := row.serverRequiredFDs
	if request.TargetFDPresent {
		expected = row.serverRequiredFDsWithTarget
	}
	return descriptors != expected
}

// shapeRejected reports whether the relational rules fail; it may assume every
// required field is present.
func (row linuxOwnerAnchorOperationShape) shapeRejected(request linuxOwnerAnchorRequest) bool {
	return row.shape != nil && !row.shape(request)
}

// refuses reports whether the request violates this row: a forbidden field is
// present, a required field is absent, the received descriptor count differs, or
// a relational rule fails.
func (row linuxOwnerAnchorOperationShape) refuses(request linuxOwnerAnchorRequest, descriptors int) bool {
	return row.presenceRejected(request) || row.descriptorRejected(request, descriptors) || row.shapeRejected(request)
}

// exclusiveFieldRejected reports whether the request carries one of the six
// fields that only their owning operation group may carry. The base server
// checked exactly these three groups (clone cleanup, gate, stop leaf) before its
// stopping answer, so the check keeps that position ahead of the per-operation
// shape gate.
func (request linuxOwnerAnchorRequest) exclusiveFieldRejected(row linuxOwnerAnchorOperationShape) bool {
	for _, field := range [...]linuxOwnerAnchorRequestField{
		linuxOwnerAnchorFieldCloneGrant,
		linuxOwnerAnchorFieldCleanup,
		linuxOwnerAnchorFieldTargetFDPresent,
		linuxOwnerAnchorFieldGate,
		linuxOwnerAnchorFieldUserNamespace,
		linuxOwnerAnchorFieldStopFence,
	} {
		if request.fieldPresent(field) && row.forbids(field) {
			return true
		}
	}
	return false
}

// linuxOwnerAnchorRequestFDCap bounds the descriptors one request may transfer.
// It is sized by readLinuxOwnerAnchorRequest's four-descriptor ancillary buffer
// and dominates every row's variable descriptor count; the table test asserts
// that dominance.
const linuxOwnerAnchorRequestFDCap = 3

func linuxOwnerAnchorRegisterProcessShape(request linuxOwnerAnchorRequest) bool {
	return request.Role == "launcher" || request.Role == "native" || request.Role == "descendant"
}

func linuxOwnerAnchorClassifyChildShape(request linuxOwnerAnchorRequest) bool {
	return request.Helper.Validate() == nil && request.Process.Validate() == nil
}

func linuxOwnerAnchorGateShape(request linuxOwnerAnchorRequest) bool {
	return request.Helper.Validate() == nil && request.Gate.Validate() == nil && request.UserNamespace.valid()
}

func linuxOwnerAnchorStopLeafShape(request linuxOwnerAnchorRequest) bool {
	return request.Helper.Validate() == nil && request.StopFence.ValidateFor(request.Grant, *request.Helper) == nil
}

func linuxOwnerAnchorWaitReapShape(request linuxOwnerAnchorRequest) bool {
	return linuxOwnerAnchorValidChildRole(request.Role)
}

func linuxOwnerAnchorReapRunShape(request linuxOwnerAnchorRequest) bool {
	return request.AfterPID >= 0
}

// linuxOwnerAnchorRetireReleaseShape requires exactly one release capability.
func linuxOwnerAnchorRetireReleaseShape(request linuxOwnerAnchorRequest) bool {
	return (request.ReleaseProof == nil) != (request.PreGateReleaseProof == nil)
}

func linuxOwnerAnchorRetireClearedShape(request linuxOwnerAnchorRequest) bool {
	return request.Helper.Validate() == nil && validLinuxOwnerAnchorRunID(request.RunKey.RunID) &&
		request.RunKey.Generation > 0 && request.Fence.Generation == request.RunKey.Generation &&
		linuxOwnerAnchorRetireReleaseShape(request)
}

func linuxOwnerAnchorRetireAbortedShape(request linuxOwnerAnchorRequest) bool {
	return request.Helper.Validate() == nil && validLinuxOwnerAnchorRunID(request.RunKey.RunID) &&
		request.RunKey.Generation > 0 && request.Fence.Generation == request.RunKey.Generation
}

// linuxOwnerAnchorCloneCleanupShape carries the clone-cleanup rules that depend
// on the cleanup intent: the run key and fence digest must equal the intent's,
// and the prior-removal proof is required for an absent target and forbidden for
// a live one.
func linuxOwnerAnchorCloneCleanupShape(request linuxOwnerAnchorRequest) bool {
	if request.TargetFDPresent && request.PriorRemovalProof != nil {
		return false
	}
	if !request.TargetFDPresent && request.PriorRemovalProof == nil {
		return false
	}
	return request.RunKey == (LinuxOwnerAnchorRunKey{RunID: request.Cleanup.Intent.RunID, Generation: request.Cleanup.Intent.Generation}) &&
		request.FenceDigest == request.Cleanup.FenceDigest
}

// linuxOwnerAnchorOperationShapes is the single source of the per-operation
// request shape: field presence, descriptor counts, timeout class and the
// relational predicates above.
var linuxOwnerAnchorOperationShapes = []linuxOwnerAnchorOperationShape{
	{
		operation: linuxOwnerAnchorOperationIdentity,
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldCreator,
			linuxOwnerAnchorFieldProcess, linuxOwnerAnchorFieldRole, linuxOwnerAnchorFieldReleaseProof,
			linuxOwnerAnchorFieldPreGateReleaseProof, linuxOwnerAnchorFieldAbortProof,
			linuxOwnerAnchorFieldRunKey, linuxOwnerAnchorFieldFence,
			linuxOwnerAnchorFieldGate, linuxOwnerAnchorFieldUserNamespace, linuxOwnerAnchorFieldStopFence,
			linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutRequest,
		stoppingAnswer:    true,
	},
	{
		operation: linuxOwnerAnchorOperationStart,
		required:  []linuxOwnerAnchorRequestField{linuxOwnerAnchorFieldGrant},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldProcess,
			linuxOwnerAnchorFieldRole, linuxOwnerAnchorFieldGate, linuxOwnerAnchorFieldUserNamespace,
			linuxOwnerAnchorFieldStopFence, linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup,
			linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 2,
		clientSendFDs:     2,
		responseFDs:       3,
		timeoutClass:      linuxOwnerAnchorTimeoutRequest,
		stoppingAnswer:    true,
	},
	{
		operation: linuxOwnerAnchorOperationAttach,
		required:  []linuxOwnerAnchorRequestField{linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldProcess, linuxOwnerAnchorFieldRole,
			linuxOwnerAnchorFieldGate, linuxOwnerAnchorFieldUserNamespace, linuxOwnerAnchorFieldStopFence,
			linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		responseFDs:       3,
		timeoutClass:      linuxOwnerAnchorTimeoutRequest,
	},
	{
		operation: linuxOwnerAnchorOperationLookup,
		required:  []linuxOwnerAnchorRequestField{linuxOwnerAnchorFieldGrant},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldProcess,
			linuxOwnerAnchorFieldRole, linuxOwnerAnchorFieldGate, linuxOwnerAnchorFieldUserNamespace,
			linuxOwnerAnchorFieldStopFence, linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup,
			linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		responseFDs:       -1,
		timeoutClass:      linuxOwnerAnchorTimeoutWait,
	},
	{
		operation: linuxOwnerAnchorOperationRegisterProcess,
		required: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldCreator,
			linuxOwnerAnchorFieldProcess, linuxOwnerAnchorFieldRole,
		},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGate, linuxOwnerAnchorFieldUserNamespace, linuxOwnerAnchorFieldStopFence,
			linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 1,
		clientSendFDs:     1,
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutRequest,
		stoppingAnswer:    true,
		runHelperPeer:     true,
		shape:             linuxOwnerAnchorRegisterProcessShape,
	},
	{
		operation: linuxOwnerAnchorOperationClassifyChild,
		required: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldProcess,
		},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldRole, linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldGate,
			linuxOwnerAnchorFieldUserNamespace, linuxOwnerAnchorFieldStopFence,
			linuxOwnerAnchorFieldReleaseProof, linuxOwnerAnchorFieldPreGateReleaseProof,
			linuxOwnerAnchorFieldAbortProof, linuxOwnerAnchorFieldRunKey, linuxOwnerAnchorFieldFence,
			linuxOwnerAnchorFieldFenceDigest, linuxOwnerAnchorFieldAfterPID,
			linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup,
			linuxOwnerAnchorFieldPriorRemovalProof, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 1,
		clientSendFDs:     1,
		clientCheckedFDs:  true,
		clientFDsMismatch: "Linux owner child-origin request differs from its exact helper/process PIDFD",
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutWait,
		stoppingAnswer:    true,
		runHelperPeer:     true,
		shape:             linuxOwnerAnchorClassifyChildShape,
	},
	{
		operation: linuxOwnerAnchorOperationRegisterGate,
		required: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldGate,
			linuxOwnerAnchorFieldUserNamespace,
		},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldProcess, linuxOwnerAnchorFieldRole,
			linuxOwnerAnchorFieldStopFence, linuxOwnerAnchorFieldReleaseProof,
			linuxOwnerAnchorFieldPreGateReleaseProof, linuxOwnerAnchorFieldAbortProof,
			linuxOwnerAnchorFieldRunKey, linuxOwnerAnchorFieldFence, linuxOwnerAnchorFieldFenceDigest,
			linuxOwnerAnchorFieldAfterPID, linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup,
			linuxOwnerAnchorFieldPriorRemovalProof, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 1,
		clientSendFDs:     1,
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutRequest,
		stoppingAnswer:    true,
		runHelperPeer:     true,
		shape:             linuxOwnerAnchorGateShape,
	},
	{
		operation: linuxOwnerAnchorOperationAuthorizeGate,
		required: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldGate,
			linuxOwnerAnchorFieldUserNamespace,
		},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldProcess, linuxOwnerAnchorFieldRole,
			linuxOwnerAnchorFieldStopFence, linuxOwnerAnchorFieldReleaseProof,
			linuxOwnerAnchorFieldPreGateReleaseProof, linuxOwnerAnchorFieldAbortProof,
			linuxOwnerAnchorFieldRunKey, linuxOwnerAnchorFieldFence, linuxOwnerAnchorFieldFenceDigest,
			linuxOwnerAnchorFieldAfterPID, linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup,
			linuxOwnerAnchorFieldPriorRemovalProof, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutRequest,
		stoppingAnswer:    true,
		runHelperPeer:     true,
		shape:             linuxOwnerAnchorGateShape,
	},
	{
		operation: linuxOwnerAnchorOperationStopLeaf,
		required: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldStopFence,
		},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldProcess, linuxOwnerAnchorFieldRole,
			linuxOwnerAnchorFieldGate, linuxOwnerAnchorFieldUserNamespace,
			linuxOwnerAnchorFieldReleaseProof, linuxOwnerAnchorFieldPreGateReleaseProof,
			linuxOwnerAnchorFieldAbortProof, linuxOwnerAnchorFieldRunKey, linuxOwnerAnchorFieldFence,
			linuxOwnerAnchorFieldFenceDigest, linuxOwnerAnchorFieldAfterPID,
			linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup,
			linuxOwnerAnchorFieldPriorRemovalProof, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		clientCheckedFDs:  true,
		clientFDsMismatch: "Linux owner anchor StopLeaf request differs from its exact helper producer fence",
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutWait,
		stoppingAnswer:    true,
		runHelperPeer:     true,
		shape:             linuxOwnerAnchorStopLeafShape,
	},
	{
		operation: linuxOwnerAnchorOperationWaitReap,
		required: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldProcess,
			linuxOwnerAnchorFieldRole,
		},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldGate, linuxOwnerAnchorFieldUserNamespace,
			linuxOwnerAnchorFieldStopFence, linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup,
			linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutWait,
		shape:             linuxOwnerAnchorWaitReapShape,
	},
	{
		operation: linuxOwnerAnchorOperationReapRun,
		required:  []linuxOwnerAnchorRequestField{linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldProcess, linuxOwnerAnchorFieldRole,
			linuxOwnerAnchorFieldGate, linuxOwnerAnchorFieldUserNamespace, linuxOwnerAnchorFieldStopFence,
			linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutWait,
		shape:             linuxOwnerAnchorReapRunShape,
	},
	{
		operation: linuxOwnerAnchorOperationWaitHelper,
		required:  []linuxOwnerAnchorRequestField{linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldProcess, linuxOwnerAnchorFieldRole,
			linuxOwnerAnchorFieldGate, linuxOwnerAnchorFieldUserNamespace, linuxOwnerAnchorFieldStopFence,
			linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutWait,
	},
	{
		operation: linuxOwnerAnchorOperationRetire,
		required:  []linuxOwnerAnchorRequestField{linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldCreator, linuxOwnerAnchorFieldProcess, linuxOwnerAnchorFieldRole,
			linuxOwnerAnchorFieldAbortProof, linuxOwnerAnchorFieldGate, linuxOwnerAnchorFieldUserNamespace,
			linuxOwnerAnchorFieldStopFence, linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup,
			linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutRequest,
		shape:             linuxOwnerAnchorRetireReleaseShape,
	},
	{
		operation: linuxOwnerAnchorOperationRetireCleared,
		required: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldRunKey, linuxOwnerAnchorFieldFence,
		},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldAbortProof, linuxOwnerAnchorFieldGate,
			linuxOwnerAnchorFieldUserNamespace, linuxOwnerAnchorFieldStopFence,
			linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutWait,
		shape:             linuxOwnerAnchorRetireClearedShape,
	},
	{
		operation: linuxOwnerAnchorOperationRetireAborted,
		required: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldRunKey, linuxOwnerAnchorFieldFence,
			linuxOwnerAnchorFieldAbortProof,
		},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldReleaseProof,
			linuxOwnerAnchorFieldPreGateReleaseProof, linuxOwnerAnchorFieldGate,
			linuxOwnerAnchorFieldUserNamespace, linuxOwnerAnchorFieldStopFence,
			linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup, linuxOwnerAnchorFieldTargetFDPresent,
		},
		serverRequiredFDs: 0,
		responseFDs:       0,
		timeoutClass:      linuxOwnerAnchorTimeoutWait,
		shape:             linuxOwnerAnchorRetireAbortedShape,
	},
	{
		operation: linuxOwnerAnchorOperationCloneCleanup,
		required: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldCloneGrant, linuxOwnerAnchorFieldCleanup,
		},
		forbidden: []linuxOwnerAnchorRequestField{
			linuxOwnerAnchorFieldGrant, linuxOwnerAnchorFieldHelper, linuxOwnerAnchorFieldCreator,
			linuxOwnerAnchorFieldProcess, linuxOwnerAnchorFieldRole, linuxOwnerAnchorFieldReleaseProof,
			linuxOwnerAnchorFieldPreGateReleaseProof, linuxOwnerAnchorFieldAbortProof,
			linuxOwnerAnchorFieldFence, linuxOwnerAnchorFieldAfterPID, linuxOwnerAnchorFieldGate,
			linuxOwnerAnchorFieldUserNamespace, linuxOwnerAnchorFieldStopFence,
		},
		serverRequiredFDs:           1,
		serverRequiredFDsWithTarget: 2,
		clientSendFDs:               1,
		clientSendFDsWithTarget:     2,
		clientCheckedFDs:            true,
		clientFDsMismatch:           "Linux owner anchor cleanup request is not bound to its durable RunKey and fence digest",
		responseFDs:                 0,
		timeoutClass:                linuxOwnerAnchorTimeoutCleanup,
		shape:                       linuxOwnerAnchorCloneCleanupShape,
	},
}

// linuxOwnerAnchorOperationShapeFor returns the shape row of one operation. An
// absent row replaces the base switch's default arm: the request is refused
// without a response.
func linuxOwnerAnchorOperationShapeFor(operation string) (linuxOwnerAnchorOperationShape, bool) {
	for _, row := range linuxOwnerAnchorOperationShapes {
		if row.operation == operation {
			return row, true
		}
	}
	return linuxOwnerAnchorOperationShape{}, false
}

// linuxOwnerAnchorTimeoutFor is the deadline class of one operation; an unknown
// operation keeps the request class.
func linuxOwnerAnchorTimeoutFor(operation string) time.Duration {
	row, ok := linuxOwnerAnchorOperationShapeFor(operation)
	if !ok {
		return linuxOwnerAnchorTimeoutRequest.duration()
	}
	return row.timeoutClass.duration()
}

func readLinuxOwnerAnchorRequest(connection *net.UnixConn) (linuxOwnerAnchorRequest, []int, error) {
	data := make([]byte, linuxOwnerAnchorMaxFrame+1)
	oob := make([]byte, syscall.CmsgSpace(4*4))
	n, oobn, flags, _, err := connection.ReadMsgUnix(data, oob)
	if err != nil {
		return linuxOwnerAnchorRequest{}, nil, fmt.Errorf("read Linux owner anchor request: %w", sanitizeLinuxOwnerAnchorSocketError(err))
	}
	fds, err := parseLinuxOwnerAnchorRights(oob[:oobn], flags)
	if err != nil {
		return linuxOwnerAnchorRequest{}, fds, err
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 || n > linuxOwnerAnchorMaxFrame {
		return linuxOwnerAnchorRequest{}, fds, errors.New("Linux owner anchor request exceeded its bounded frame")
	}
	decoder := json.NewDecoder(bytes.NewReader(data[:n]))
	decoder.DisallowUnknownFields()
	var request linuxOwnerAnchorRequest
	if err := decoder.Decode(&request); err != nil {
		return linuxOwnerAnchorRequest{}, fds, fmt.Errorf("decode Linux owner anchor request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return linuxOwnerAnchorRequest{}, fds, errors.New("Linux owner anchor request has trailing JSON")
	}
	return request, fds, nil
}

func readLinuxOwnerAnchorResponse(connection *net.UnixConn) (linuxOwnerAnchorResponse, []int, error) {
	data := make([]byte, linuxOwnerAnchorMaxFrame+1)
	oob := make([]byte, syscall.CmsgSpace(4*4))
	n, oobn, flags, _, err := connection.ReadMsgUnix(data, oob)
	if err != nil {
		return linuxOwnerAnchorResponse{}, nil, fmt.Errorf("read Linux owner anchor response: %w", sanitizeLinuxOwnerAnchorSocketError(err))
	}
	fds, err := parseLinuxOwnerAnchorRights(oob[:oobn], flags)
	if err != nil {
		return linuxOwnerAnchorResponse{}, fds, err
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 || n > linuxOwnerAnchorMaxFrame {
		closeLinuxOwnerAnchorFDs(fds)
		return linuxOwnerAnchorResponse{}, nil, errors.New("Linux owner anchor response exceeded its bounded frame")
	}
	decoder := json.NewDecoder(bytes.NewReader(data[:n]))
	decoder.DisallowUnknownFields()
	var response linuxOwnerAnchorResponse
	if err := decoder.Decode(&response); err != nil {
		return linuxOwnerAnchorResponse{}, fds, fmt.Errorf("decode Linux owner anchor response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return linuxOwnerAnchorResponse{}, fds, errors.New("Linux owner anchor response has trailing JSON")
	}
	for _, fd := range fds {
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC == 0 {
			closeLinuxOwnerAnchorFDs(fds)
			return linuxOwnerAnchorResponse{}, nil, errors.Join(errors.New("received Linux owner anchor descriptor is not close-on-exec"), err)
		}
	}
	return response, fds, nil
}

func linuxOwnerAnchorPeerFromConn(connection *net.UnixConn, owner linuxgate.OwnerConfig) (linuxOwnerAnchorPeer, error) {
	if connection == nil {
		return linuxOwnerAnchorPeer{}, errors.New("Linux owner anchor peer connection is nil")
	}
	credentials, err := linuxOwnerAnchorPeerCredentials(connection)
	if err != nil {
		return linuxOwnerAnchorPeer{}, err
	}
	if credentials.Pid <= 0 || credentials.Uid != owner.AUID || credentials.Gid != owner.AGID {
		return linuxOwnerAnchorPeer{}, errors.New("Linux owner anchor peer SO_PEERCRED UID/GID/PID is not configured A")
	}
	identity, err := ProcessIdentity(int(credentials.Pid))
	if err != nil {
		return linuxOwnerAnchorPeer{}, fmt.Errorf("read exact Linux owner anchor peer identity: %w", err)
	}
	if err := verifyLinuxOwnerAProcess(int(credentials.Pid), identity, owner.AUID, owner.AGID, 0, ""); err != nil {
		return linuxOwnerAnchorPeer{}, err
	}
	cgroupPath, err := readLinuxOwnerProcessCgroupPath(int(credentials.Pid))
	if err != nil {
		return linuxOwnerAnchorPeer{}, err
	}
	actualIdentity, err := ProcessIdentity(int(credentials.Pid))
	if err != nil || actualIdentity != identity {
		return linuxOwnerAnchorPeer{}, fmt.Errorf("Linux owner anchor peer identity changed during authentication: %w", errOr(err, ErrLinuxOwnerCreationIdentityDiffers))
	}
	return linuxOwnerAnchorPeer{PID: int(credentials.Pid), Identity: identity, CgroupPath: cgroupPath}, nil
}

type linuxOwnerAnchorPeer struct {
	PID        int
	Identity   string
	CgroupPath string
}

func verifyLinuxOwnerAnchorPeer(connection *net.UnixConn, owner linuxgate.OwnerConfig, expected authority.LinuxCgroupProcessIdentity) error {
	if expected.Validate() != nil {
		return errors.New("expected Linux owner anchor process identity is invalid")
	}
	peer, err := linuxOwnerAnchorPeerFromConn(connection, owner)
	if err != nil {
		return err
	}
	if peer.PID != expected.PID || peer.Identity != expected.Identity {
		return errors.New("Linux owner anchor peer PID/creation identity does not match the durable anchor identity")
	}
	return nil
}

func linuxOwnerAnchorPeerCredentials(connection *net.UnixConn) (*unix.Ucred, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("access Linux owner anchor socket: %w", err)
	}
	var credentials *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, fmt.Errorf("read Linux owner anchor SO_PEERCRED: %w", err)
	}
	if controlErr != nil || credentials == nil {
		return nil, fmt.Errorf("read Linux owner anchor SO_PEERCRED: %w", errOr(controlErr, errors.New("credentials unavailable")))
	}
	return credentials, nil
}

func validateLinuxOwnerAnchorSocketPath(socketPath string) error {
	if !canonicalAbsoluteLinuxPath(socketPath) || len(socketPath) >= linuxOwnerAnchorMaxSocketPath || filepath.Base(socketPath) == "." || filepath.Base(socketPath) == "/" {
		return errors.New("Linux owner anchor socket path must be canonical, absolute, and fit sockaddr_un")
	}
	return nil
}

type linuxOwnerAnchorSocketIdentity struct {
	device uint64
	inode  uint64
}

func listenLinuxOwnerAnchor(socketPath string, owner linuxgate.OwnerConfig) (*net.UnixListener, linuxOwnerAnchorSocketIdentity, error) {
	if err := validateLinuxOwnerAnchorSocketPath(socketPath); err != nil {
		return nil, linuxOwnerAnchorSocketIdentity{}, err
	}
	directoryFD, directoryIdentity, err := openLinuxOwnerAnchorSocketDirectory(filepath.Dir(socketPath), owner.AUID)
	if err != nil {
		return nil, linuxOwnerAnchorSocketIdentity{}, err
	}
	defer unix.Close(directoryFD)
	var existing unix.Stat_t
	if err := unix.Fstatat(directoryFD, filepath.Base(socketPath), &existing, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return nil, linuxOwnerAnchorSocketIdentity{}, errors.New("Linux owner anchor socket path already exists; refusing stale or replacement endpoint")
	} else if !errors.Is(err, unix.ENOENT) {
		return nil, linuxOwnerAnchorSocketIdentity{}, fmt.Errorf("inspect Linux owner anchor socket path: %w", err)
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socketPath, Net: "unixpacket"})
	if err != nil {
		return nil, linuxOwnerAnchorSocketIdentity{}, fmt.Errorf("listen on stable Linux owner anchor socket: %w", sanitizeLinuxOwnerAnchorSocketError(err))
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(socketPath, 0600); err != nil {
		listener.SetUnlinkOnClose(true)
		_ = listener.Close()
		return nil, linuxOwnerAnchorSocketIdentity{}, fmt.Errorf("set private Linux owner anchor socket permissions: %w", err)
	}
	var socketStat, directoryAfter unix.Stat_t
	if err := unix.Fstatat(directoryFD, filepath.Base(socketPath), &socketStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		listener.SetUnlinkOnClose(true)
		_ = listener.Close()
		return nil, linuxOwnerAnchorSocketIdentity{}, fmt.Errorf("verify bound Linux owner anchor socket: %w", err)
	}
	if err := unix.Fstat(directoryFD, &directoryAfter); err != nil || directoryAfter.Dev != directoryIdentity.Dev || directoryAfter.Ino != directoryIdentity.Ino ||
		socketStat.Mode&unix.S_IFMT != unix.S_IFSOCK || socketStat.Uid != owner.AUID || socketStat.Gid != owner.AGID || socketStat.Mode&0777 != 0600 {
		listener.SetUnlinkOnClose(true)
		_ = listener.Close()
		return nil, linuxOwnerAnchorSocketIdentity{}, errors.Join(errors.New("bound Linux owner anchor socket or containing directory changed identity"), err)
	}
	listener.SetUnlinkOnClose(false)
	return listener, linuxOwnerAnchorSocketIdentity{device: uint64(socketStat.Dev), inode: socketStat.Ino}, nil
}

func openLinuxOwnerAnchorSocketDirectory(directory string, ownerUID uint32) (int, unix.Stat_t, error) {
	if !canonicalAbsoluteLinuxPath(directory) {
		return -1, unix.Stat_t{}, errors.New("Linux owner anchor socket directory is not canonical")
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, unix.Stat_t{}, fmt.Errorf("open root for Linux owner anchor socket path: %w", err)
	}
	parts := strings.Split(strings.TrimPrefix(directory, "/"), "/")
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("Linux owner anchor socket directory has an unsafe component")
		}
		how := &unix.OpenHow{
			Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
			Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
		}
		next, err := unix.Openat2(fd, part, how)
		if err != nil {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, fmt.Errorf("open Linux owner anchor socket directory component: %w", err)
		}
		_ = unix.Close(fd)
		fd = next
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, fmt.Errorf("stat Linux owner anchor socket directory component: %w", err)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR || (stat.Uid != 0 && stat.Uid != ownerUID) || stat.Mode&0022 != 0 {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("Linux owner anchor socket path crosses a non-root writable or untrusted directory")
		}
		if index == len(parts)-1 && (stat.Uid != ownerUID || stat.Mode&0700 != 0700 || stat.Mode&0077 != 0) {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("Linux owner anchor socket directory must be A-owned and private")
		}
	}
	var final unix.Stat_t
	if err := unix.Fstat(fd, &final); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("stat final Linux owner anchor socket directory: %w", err)
	}
	return fd, final, nil
}

func unlinkLinuxOwnerAnchorSocket(socketPath string, expected linuxOwnerAnchorSocketIdentity, ownerUID uint32) {
	if expected.inode == 0 {
		return
	}
	directoryFD, directoryIdentity, err := openLinuxOwnerAnchorSocketDirectory(filepath.Dir(socketPath), ownerUID)
	if err != nil {
		return
	}
	defer unix.Close(directoryFD)
	var stat unix.Stat_t
	if unix.Fstatat(directoryFD, filepath.Base(socketPath), &stat, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		uint64(stat.Dev) != expected.device || stat.Ino != expected.inode || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Uid != ownerUID {
		return
	}
	var currentDirectory unix.Stat_t
	if unix.Fstat(directoryFD, &currentDirectory) != nil || currentDirectory.Dev != directoryIdentity.Dev || currentDirectory.Ino != directoryIdentity.Ino {
		return
	}
	_ = unix.Unlinkat(directoryFD, filepath.Base(socketPath), 0)
}

func notifyLinuxOwnerAnchorReady() error {
	address := os.Getenv("NOTIFY_SOCKET")
	if address == "" {
		return nil
	}
	connection, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("connect systemd Linux owner anchor notification socket: %w", sanitizeLinuxOwnerAnchorSocketError(err))
	}
	defer connection.Close()
	if _, err := connection.Write([]byte("READY=1\nSTATUS=delegated cgroup2 parent, manager subgroup, subreaper, and private socket verified\n")); err != nil {
		return fmt.Errorf("notify systemd Linux owner anchor readiness: %w", sanitizeLinuxOwnerAnchorSocketError(err))
	}
	return nil
}

func sanitizeLinuxOwnerAnchorSocketError(err error) error {
	switch typed := err.(type) {
	case *net.OpError:
		copy := *typed
		copy.Source = nil
		copy.Addr = nil
		copy.Err = sanitizeLinuxOwnerAnchorSocketError(typed.Err)
		return &copy
	case *os.PathError:
		copy := *typed
		copy.Path = "Linux owner anchor socket"
		copy.Err = sanitizeLinuxOwnerAnchorSocketError(typed.Err)
		return &copy
	case *os.SyscallError:
		copy := *typed
		copy.Err = sanitizeLinuxOwnerAnchorSocketError(typed.Err)
		return &copy
	default:
		return err
	}
}
