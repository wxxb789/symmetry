//go:build linux && amd64

package platform

// Per-operation request-shape oracle for the anchor control socket: every expectation in this file
// is hard-coded, so mutating one production row or baseResponse field fails here instead of moving it.

import (
	"encoding/json"
	"net"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	daemonprotocol "github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func linuxOwnerAnchorShapeFence() daemonprotocol.Fence {
	return daemonprotocol.Fence{RuntimeID: "shape-runtime", RuntimeEpoch: 1, Generation: 1, ClaimID: "shape-claim", LeaseToken: "shape-lease"}
}

type linuxOwnerAnchorShapeExpectation struct {
	timeoutClass        linuxOwnerAnchorTimeoutClass
	serverRequiredFDs   int
	serverFDsWithTarget int
	clientSendFDs       int
	clientFDsWithTarget int
	clientCheckedFDs    bool
	responseFDs         int
	stoppingAnswer      bool
	runHelperPeer       bool
}

var linuxOwnerAnchorShapeExpectations = map[string]linuxOwnerAnchorShapeExpectation{
	linuxOwnerAnchorOperationIdentity: {
		timeoutClass: linuxOwnerAnchorTimeoutClassRequest, serverRequiredFDs: 0, responseFDs: 0, stoppingAnswer: true,
	},
	linuxOwnerAnchorOperationStart: {
		timeoutClass: linuxOwnerAnchorTimeoutClassRequest, serverRequiredFDs: 2, clientSendFDs: 2, responseFDs: 3, stoppingAnswer: true,
	},
	linuxOwnerAnchorOperationAttach: {
		timeoutClass: linuxOwnerAnchorTimeoutClassRequest, serverRequiredFDs: 0, responseFDs: 3,
	},
	linuxOwnerAnchorOperationLookup: {
		timeoutClass: linuxOwnerAnchorTimeoutClassWait, serverRequiredFDs: 0, responseFDs: -1,
	},
	linuxOwnerAnchorOperationRegisterProcess: {
		timeoutClass: linuxOwnerAnchorTimeoutClassRequest, serverRequiredFDs: 1, clientSendFDs: 1, stoppingAnswer: true, runHelperPeer: true,
	},
	linuxOwnerAnchorOperationClassifyChild: {
		timeoutClass: linuxOwnerAnchorTimeoutClassWait, serverRequiredFDs: 1, clientSendFDs: 1, clientCheckedFDs: true,
		stoppingAnswer: true, runHelperPeer: true,
	},
	linuxOwnerAnchorOperationRegisterGate: {
		timeoutClass: linuxOwnerAnchorTimeoutClassRequest, serverRequiredFDs: 1, clientSendFDs: 1, stoppingAnswer: true, runHelperPeer: true,
	},
	linuxOwnerAnchorOperationAuthorizeGate: {
		timeoutClass: linuxOwnerAnchorTimeoutClassRequest, serverRequiredFDs: 0, stoppingAnswer: true, runHelperPeer: true,
	},
	linuxOwnerAnchorOperationStopLeaf: {
		timeoutClass: linuxOwnerAnchorTimeoutClassWait, serverRequiredFDs: 0, clientCheckedFDs: true,
		stoppingAnswer: true, runHelperPeer: true,
	},
	linuxOwnerAnchorOperationWaitReap: {
		timeoutClass: linuxOwnerAnchorTimeoutClassWait, serverRequiredFDs: 0,
	},
	linuxOwnerAnchorOperationReapRun: {
		timeoutClass: linuxOwnerAnchorTimeoutClassWait, serverRequiredFDs: 0,
	},
	linuxOwnerAnchorOperationWaitHelper: {
		timeoutClass: linuxOwnerAnchorTimeoutClassWait, serverRequiredFDs: 0,
	},
	linuxOwnerAnchorOperationRetire: {
		timeoutClass: linuxOwnerAnchorTimeoutClassRequest, serverRequiredFDs: 0,
	},
	linuxOwnerAnchorOperationRetireCleared: {
		timeoutClass: linuxOwnerAnchorTimeoutClassWait, serverRequiredFDs: 0,
	},
	linuxOwnerAnchorOperationRetireAborted: {
		timeoutClass: linuxOwnerAnchorTimeoutClassWait, serverRequiredFDs: 0,
	},
	linuxOwnerAnchorOperationCloneCleanup: {
		timeoutClass: linuxOwnerAnchorTimeoutClassCleanup, serverRequiredFDs: 1, serverFDsWithTarget: 2,
		clientSendFDs: 1, clientFDsWithTarget: 2, clientCheckedFDs: true,
	},
}

// linuxOwnerAnchorShapeFieldExpectations pins each row's declared required|forbidden field names; a
// loosened row (a forbidden field silently dropped) fails here instead of widening the accept set.
var linuxOwnerAnchorShapeFieldExpectations = map[string]string{
	linuxOwnerAnchorOperationIdentity:        "|grant,helper,creator,process,role,release_proof,pre_gate_release_proof,abort_proof,run_key,fence,gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationStart:           "grant|helper,creator,process,role,gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationAttach:          "grant,helper|creator,process,role,gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationLookup:          "grant|helper,creator,process,role,gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationRegisterProcess: "grant,helper,creator,process,role|gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationClassifyChild:   "grant,helper,process|role,creator,gate,user_namespace,stop_fence,release_proof,pre_gate_release_proof,abort_proof,run_key,fence,fence_digest,after_pid,clone_grant,cleanup,prior_removal_proof,target_fd_present",
	linuxOwnerAnchorOperationRegisterGate:    "grant,helper,gate,user_namespace|creator,process,role,stop_fence,release_proof,pre_gate_release_proof,abort_proof,run_key,fence,fence_digest,after_pid,clone_grant,cleanup,prior_removal_proof,target_fd_present",
	linuxOwnerAnchorOperationAuthorizeGate:   "grant,helper,gate,user_namespace|creator,process,role,stop_fence,release_proof,pre_gate_release_proof,abort_proof,run_key,fence,fence_digest,after_pid,clone_grant,cleanup,prior_removal_proof,target_fd_present",
	linuxOwnerAnchorOperationStopLeaf:        "grant,helper,stop_fence|creator,process,role,gate,user_namespace,release_proof,pre_gate_release_proof,abort_proof,run_key,fence,fence_digest,after_pid,clone_grant,cleanup,prior_removal_proof,target_fd_present",
	linuxOwnerAnchorOperationWaitReap:        "grant,helper,process,role|creator,gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationReapRun:         "grant,helper|creator,process,role,gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationWaitHelper:      "grant,helper|creator,process,role,gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationRetire:          "grant,helper|creator,process,role,gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationRetireCleared:   "helper,run_key,fence|grant,abort_proof,gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationRetireAborted:   "helper,run_key,fence,abort_proof|grant,release_proof,pre_gate_release_proof,gate,user_namespace,stop_fence,clone_grant,cleanup,target_fd_present",
	linuxOwnerAnchorOperationCloneCleanup:    "clone_grant,cleanup|grant,helper,creator,process,role,release_proof,pre_gate_release_proof,abort_proof,fence,after_pid,gate,user_namespace,stop_fence",
}

// TestLinuxOwnerAnchorShapeTableFieldSets pins every row's required and forbidden field names
// order-independently: the declaration order is not part of the contract.
func TestLinuxOwnerAnchorShapeTableFieldSets(t *testing.T) {
	if len(linuxOwnerAnchorShapeFieldExpectations) != len(linuxOwnerAnchorOperationShapes) {
		t.Fatalf("field expectations cover %d operations, table has %d",
			len(linuxOwnerAnchorShapeFieldExpectations), len(linuxOwnerAnchorOperationShapes))
	}
	sortedNames := func(fields []linuxOwnerAnchorRequestField) string {
		parts := make([]string, 0, len(fields))
		for _, field := range fields {
			parts = append(parts, string(field))
		}
		sort.Strings(parts)
		return strings.Join(parts, ",")
	}
	sortedList := func(list string) string {
		if list == "" {
			return ""
		}
		parts := strings.Split(list, ",")
		sort.Strings(parts)
		return strings.Join(parts, ",")
	}
	for operation, expected := range linuxOwnerAnchorShapeFieldExpectations {
		row := linuxOwnerAnchorShapeRowFor(t, operation)
		wantRequired, wantForbidden, ok := strings.Cut(expected, "|")
		if !ok {
			t.Fatalf("field expectation for %q is missing the required|forbidden separator", operation)
		}
		observed := sortedNames(row.required) + "|" + sortedNames(row.forbidden)
		want := sortedList(wantRequired) + "|" + sortedList(wantForbidden)
		if observed != want {
			t.Fatalf("operation %q declares\n  %s\nwant the pre-refactor field set\n  %s", operation, observed, expected)
		}
	}
}

// TestLinuxOwnerAnchorShapeTableOperationLiterals pins the three operation wire strings; no golden
// frame carries `wait-reap`, so it needs this independent pin against a renamed constant.
func TestLinuxOwnerAnchorShapeTableOperationLiterals(t *testing.T) {
	for _, item := range []struct {
		operation string
		literal   string
	}{
		{linuxOwnerAnchorOperationIdentity, "identity"},
		{linuxOwnerAnchorOperationRegisterProcess, "register-process"},
		{linuxOwnerAnchorOperationWaitReap, "wait-reap"},
	} {
		if item.operation != item.literal {
			t.Fatalf("operation constant for %q is %q, want the pre-slice wire string %q",
				item.literal, item.operation, item.literal)
		}
	}
}

// linuxOwnerAnchorShapeExpectedFDs mirrors the row's TargetFDPresent-dependent descriptor count so a
// row.refuses assertion isolates the rule under test instead of failing on descriptors.
func linuxOwnerAnchorShapeExpectedFDs(request linuxOwnerAnchorRequest, expected linuxOwnerAnchorShapeExpectation) int {
	if request.TargetFDPresent {
		return expected.serverFDsWithTarget
	}
	return expected.serverRequiredFDs
}

func linuxOwnerAnchorShapeRowFor(t *testing.T, operation string) linuxOwnerAnchorOperationShape {
	t.Helper()
	row, ok := linuxOwnerAnchorOperationShapeFor(operation)
	if !ok {
		t.Fatalf("operation %q has no shape row", operation)
	}
	return row
}

func TestLinuxOwnerAnchorShapeTableCoverage(t *testing.T) {
	if len(linuxOwnerAnchorOperationShapes) != 16 || len(linuxOwnerAnchorShapeExpectations) != 16 {
		t.Fatalf("shape table covers %d rows against %d hard-coded expectations, want 16",
			len(linuxOwnerAnchorOperationShapes), len(linuxOwnerAnchorShapeExpectations))
	}
	seen := map[string]bool{}
	for _, row := range linuxOwnerAnchorOperationShapes {
		if seen[row.operation] {
			t.Fatalf("operation %q appears twice in the shape table", row.operation)
		}
		seen[row.operation] = true
		expected, ok := linuxOwnerAnchorShapeExpectations[row.operation]
		if !ok {
			t.Fatalf("operation %q has no hard-coded expectation", row.operation)
		}
		for _, required := range row.required {
			if row.forbids(required) {
				t.Fatalf("operation %q requires and forbids field %q", row.operation, required)
			}
		}
		observed := linuxOwnerAnchorShapeExpectation{
			timeoutClass:        row.timeoutClass,
			serverRequiredFDs:   row.serverRequiredFDs,
			serverFDsWithTarget: row.serverRequiredFDsWithTarget,
			clientSendFDs:       row.clientSendFDs,
			clientFDsWithTarget: row.clientSendFDsWithTarget,
			clientCheckedFDs:    row.clientCheckedFDs,
			responseFDs:         row.responseFDs,
			stoppingAnswer:      row.stoppingAnswer,
			runHelperPeer:       row.runHelperPeer,
		}
		if observed != expected {
			t.Fatalf("operation %q row = %+v, want the pre-refactor content %+v", row.operation, observed, expected)
		}
		for _, count := range []int{row.serverRequiredFDs, row.serverRequiredFDsWithTarget} {
			if count > linuxOwnerAnchorRequestFDCap {
				t.Fatalf("operation %q requires %d descriptors beyond the %d-descriptor request cap",
					row.operation, count, linuxOwnerAnchorRequestFDCap)
			}
		}
		for _, count := range []int{row.clientSendFDs, row.clientSendFDsWithTarget} {
			if count > linuxOwnerAnchorRequestFDCap {
				t.Fatalf("operation %q sends %d descriptors beyond the %d-descriptor request cap",
					row.operation, count, linuxOwnerAnchorRequestFDCap)
			}
		}
	}
}

func TestLinuxOwnerAnchorShapeTableTimeoutDeadlines(t *testing.T) {
	deadlines := map[linuxOwnerAnchorTimeoutClass]time.Duration{
		linuxOwnerAnchorTimeoutClassRequest: linuxOwnerAnchorRequestTimeout,
		linuxOwnerAnchorTimeoutClassWait:    linuxOwnerAnchorWaitTimeout,
		linuxOwnerAnchorTimeoutClassCleanup: linuxOwnerAnchorCleanupTimeout,
	}
	for operation, expected := range linuxOwnerAnchorShapeExpectations {
		want, ok := deadlines[expected.timeoutClass]
		if !ok {
			t.Fatalf("operation %q has no production deadline for its class %d", operation, expected.timeoutClass)
		}
		if got := linuxOwnerAnchorTimeoutFor(operation); got != want {
			t.Fatalf("operation %q deadline = %s, want %s", operation, got, want)
		}
	}
}

func TestLinuxOwnerAnchorShapeTableAcceptsEveryOperationShape(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, false)
	for operation, expected := range linuxOwnerAnchorShapeExpectations {
		row := linuxOwnerAnchorShapeRowFor(t, operation)
		request := linuxOwnerAnchorWireShapeRequest(t, fixture, operation)
		if request.exclusiveFieldRejected(row) {
			t.Fatalf("operation %q rejected a shape-clean request at the exclusive-field gate", operation)
		}
		if row.refuses(request, expected.serverRequiredFDs) {
			t.Fatalf("operation %q refused a shape-clean request with %d descriptors", operation, expected.serverRequiredFDs)
		}
	}
}

func linuxOwnerAnchorShapePresentField(request *linuxOwnerAnchorRequest, field linuxOwnerAnchorRequestField) {
	helper := authority.LinuxCgroupProcessIdentity{PID: linuxOwnerAnchorWireHelperPID, Identity: linuxOwnerAnchorWireHelperIdentity}
	switch field {
	case linuxOwnerAnchorFieldGrant:
		request.Grant = LinuxOwnerAnchorGrant{ReservationVersion: linuxOwnerAnchorReservationVersion}
	case linuxOwnerAnchorFieldHelper:
		request.Helper = &helper
	case linuxOwnerAnchorFieldGate:
		request.Gate = &authority.LinuxCgroupGateIdentity{}
	case linuxOwnerAnchorFieldUserNamespace:
		request.UserNamespace = &LinuxUserNamespaceIdentity{}
	case linuxOwnerAnchorFieldStopFence:
		request.StopFence = &LinuxOwnerAnchorProducerFence{}
	case linuxOwnerAnchorFieldCreator:
		request.Creator = &helper
	case linuxOwnerAnchorFieldProcess:
		request.Process = &helper
	case linuxOwnerAnchorFieldRole:
		request.Role = "native"
	case linuxOwnerAnchorFieldReleaseProof:
		request.ReleaseProof = &authority.SupervisorHandoffReleaseProof{}
	case linuxOwnerAnchorFieldPreGateReleaseProof:
		request.PreGateReleaseProof = &authority.LinuxCgroupPreGateReleaseProof{}
	case linuxOwnerAnchorFieldAbortProof:
		request.AbortProof = &authority.LinuxCgroupReservationAbortProof{}
	case linuxOwnerAnchorFieldRunKey:
		request.RunKey = LinuxOwnerAnchorRunKey{RunID: "shape-run", Generation: 1}
	case linuxOwnerAnchorFieldFence:
		request.Fence = linuxOwnerAnchorShapeFence()
	case linuxOwnerAnchorFieldFenceDigest:
		request.FenceDigest = "sha256:shape-fence-digest"
	case linuxOwnerAnchorFieldAfterPID:
		request.AfterPID = 7
	case linuxOwnerAnchorFieldCloneGrant:
		request.CloneGrant = &authority.LinuxCloneGrant{}
	case linuxOwnerAnchorFieldCleanup:
		request.Cleanup = &authority.LinuxCloneCleanupIntent{}
	case linuxOwnerAnchorFieldPriorRemovalProof:
		request.PriorRemovalProof = &authority.LinuxCloneTargetRemovalProof{}
	case linuxOwnerAnchorFieldTargetFDPresent:
		request.TargetFDPresent = true
	}
}

func linuxOwnerAnchorShapeClearField(request *linuxOwnerAnchorRequest, field linuxOwnerAnchorRequestField) {
	switch field {
	case linuxOwnerAnchorFieldGrant:
		request.Grant = LinuxOwnerAnchorGrant{}
	case linuxOwnerAnchorFieldHelper:
		request.Helper = nil
	case linuxOwnerAnchorFieldGate:
		request.Gate = nil
	case linuxOwnerAnchorFieldUserNamespace:
		request.UserNamespace = nil
	case linuxOwnerAnchorFieldStopFence:
		request.StopFence = nil
	case linuxOwnerAnchorFieldCreator:
		request.Creator = nil
	case linuxOwnerAnchorFieldProcess:
		request.Process = nil
	case linuxOwnerAnchorFieldRole:
		request.Role = ""
	case linuxOwnerAnchorFieldReleaseProof:
		request.ReleaseProof = nil
	case linuxOwnerAnchorFieldPreGateReleaseProof:
		request.PreGateReleaseProof = nil
	case linuxOwnerAnchorFieldAbortProof:
		request.AbortProof = nil
	case linuxOwnerAnchorFieldRunKey:
		request.RunKey = LinuxOwnerAnchorRunKey{}
	case linuxOwnerAnchorFieldFence:
		request.Fence = daemonprotocol.Fence{}
	case linuxOwnerAnchorFieldFenceDigest:
		request.FenceDigest = ""
	case linuxOwnerAnchorFieldAfterPID:
		request.AfterPID = 0
	case linuxOwnerAnchorFieldCloneGrant:
		request.CloneGrant = nil
	case linuxOwnerAnchorFieldCleanup:
		request.Cleanup = nil
	case linuxOwnerAnchorFieldPriorRemovalProof:
		request.PriorRemovalProof = nil
	case linuxOwnerAnchorFieldTargetFDPresent:
		request.TargetFDPresent = false
	}
}

func TestLinuxOwnerAnchorShapeTableRejectsForbiddenFields(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, false)
	for operation := range linuxOwnerAnchorShapeExpectations {
		row := linuxOwnerAnchorShapeRowFor(t, operation)
		expected := linuxOwnerAnchorShapeExpectations[operation]
		if len(row.forbidden) == 0 {
			t.Fatalf("operation %q forbids no field, so the rejection set is untested", operation)
		}
		clean := linuxOwnerAnchorWireShapeRequest(t, fixture, operation)
		if row.presenceRejected(clean) {
			t.Fatalf("operation %q refuses the shape-clean request before any field is present", operation)
		}
		if row.refuses(clean, linuxOwnerAnchorShapeExpectedFDs(clean, expected)) {
			t.Fatalf("operation %q refuses the shape-clean request before any field is present", operation)
		}
		for _, field := range row.forbidden {
			request := linuxOwnerAnchorWireShapeRequest(t, fixture, operation)
			linuxOwnerAnchorShapePresentField(&request, field)
			if !row.presenceRejected(request) {
				t.Fatalf("operation %q accepted forbidden field %q", operation, field)
			}
			// The server refuses through row.refuses, which composes presence, descriptor and
			// relational terms; asserting the composition keeps a dropped term red.
			if !row.refuses(request, linuxOwnerAnchorShapeExpectedFDs(request, expected)) {
				t.Fatalf("operation %q does not refuse forbidden field %q in row.refuses", operation, field)
			}
		}
	}
}

func TestLinuxOwnerAnchorShapeTableRejectsMissingRequiredFields(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, false)
	required := 0
	for operation := range linuxOwnerAnchorShapeExpectations {
		row := linuxOwnerAnchorShapeRowFor(t, operation)
		expected := linuxOwnerAnchorShapeExpectations[operation]
		for _, field := range row.required {
			required++
			request := linuxOwnerAnchorWireShapeRequest(t, fixture, operation)
			linuxOwnerAnchorShapeClearField(&request, field)
			if !row.presenceRejected(request) {
				t.Fatalf("operation %q accepted a request without required field %q", operation, field)
			}
			if !row.refuses(request, linuxOwnerAnchorShapeExpectedFDs(request, expected)) {
				t.Fatalf("operation %q does not refuse a missing required field %q in row.refuses", operation, field)
			}
		}
	}
	if required == 0 {
		t.Fatal("the shape table requires no field at all")
	}
}

func TestLinuxOwnerAnchorShapeTableRejectsDescriptorCounts(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, false)
	for operation, expected := range linuxOwnerAnchorShapeExpectations {
		row := linuxOwnerAnchorShapeRowFor(t, operation)
		for count := range linuxOwnerAnchorRequestFDCap + 2 {
			request := linuxOwnerAnchorWireShapeRequest(t, fixture, operation)
			wantRefused := count != expected.serverRequiredFDs
			if got := row.descriptorRejected(request, count); got != wantRefused {
				t.Fatalf("operation %q with %d descriptors refused = %t, want %t", operation, count, got, wantRefused)
			}
			if got := row.refuses(request, count); got != wantRefused {
				t.Fatalf("operation %q row.refuses with %d descriptors = %t, want %t", operation, count, got, wantRefused)
			}
			if row.serverRequiredFDsWithTarget != 0 {
				withTarget := request
				withTarget.TargetFDPresent = true
				withTarget.PriorRemovalProof = nil
				wantRefused = count != expected.serverFDsWithTarget
				if got := row.descriptorRejected(withTarget, count); got != wantRefused {
					t.Fatalf("operation %q with a live target and %d descriptors refused = %t, want %t",
						operation, count, got, wantRefused)
				}
				if got := row.refuses(withTarget, count); got != wantRefused {
					t.Fatalf("operation %q row.refuses with a live target and %d descriptors = %t, want %t",
						operation, count, got, wantRefused)
				}
			}
		}
	}
}

func TestLinuxOwnerAnchorShapeTableRejectsRelationalViolations(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, false)
	cases := []struct {
		name       string
		operation  string
		violate    func(*linuxOwnerAnchorRequest)
		wantReason string
	}{
		{"register-process role", linuxOwnerAnchorOperationRegisterProcess, func(request *linuxOwnerAnchorRequest) {
			request.Role = "helper"
		}, "role whitelist"},
		{"classify helper identity", linuxOwnerAnchorOperationClassifyChild, func(request *linuxOwnerAnchorRequest) {
			request.Helper.Identity = ""
		}, "helper validation"},
		{"classify process identity", linuxOwnerAnchorOperationClassifyChild, func(request *linuxOwnerAnchorRequest) {
			request.Process.Identity = ""
		}, "process validation"},
		{"register-gate identity", linuxOwnerAnchorOperationRegisterGate, func(request *linuxOwnerAnchorRequest) {
			request.Gate = &authority.LinuxCgroupGateIdentity{}
		}, "gate validation"},
		{"register-gate helper validation", linuxOwnerAnchorOperationRegisterGate, func(request *linuxOwnerAnchorRequest) {
			request.Helper.Identity = ""
		}, "helper validation"},
		{"authorize-gate user namespace", linuxOwnerAnchorOperationAuthorizeGate, func(request *linuxOwnerAnchorRequest) {
			request.UserNamespace = &LinuxUserNamespaceIdentity{}
		}, "user namespace validation"},
		{"authorize-gate helper validation", linuxOwnerAnchorOperationAuthorizeGate, func(request *linuxOwnerAnchorRequest) {
			request.Helper.Identity = ""
		}, "helper validation"},
		{"stop-leaf fence", linuxOwnerAnchorOperationStopLeaf, func(request *linuxOwnerAnchorRequest) {
			request.StopFence = &LinuxOwnerAnchorProducerFence{}
		}, "producer fence validation"},
		{"stop-leaf helper validation", linuxOwnerAnchorOperationStopLeaf, func(request *linuxOwnerAnchorRequest) {
			request.Helper.Identity = ""
		}, "helper validation"},
		{"wait-reap role", linuxOwnerAnchorOperationWaitReap, func(request *linuxOwnerAnchorRequest) {
			request.Role = "helper"
		}, "child role whitelist"},
		{"reap-run cursor", linuxOwnerAnchorOperationReapRun, func(request *linuxOwnerAnchorRequest) {
			request.AfterPID = -1
		}, "non-negative cursor"},
		{"retire-helper both proofs", linuxOwnerAnchorOperationRetire, func(request *linuxOwnerAnchorRequest) {
			request.PreGateReleaseProof = &authority.LinuxCgroupPreGateReleaseProof{}
		}, "exactly one release capability"},
		{"retire-helper no proof", linuxOwnerAnchorOperationRetire, func(request *linuxOwnerAnchorRequest) {
			request.ReleaseProof = nil
		}, "exactly one release capability"},
		{"retire-cleared helper validation", linuxOwnerAnchorOperationRetireCleared, func(request *linuxOwnerAnchorRequest) {
			request.Helper.Identity = ""
		}, "helper validation"},
		{"retire-cleared run key generation", linuxOwnerAnchorOperationRetireCleared, func(request *linuxOwnerAnchorRequest) {
			request.RunKey.Generation = 0
			request.Fence.Generation = 0
		}, "run key generation"},
		{"retire-cleared fence generation", linuxOwnerAnchorOperationRetireCleared, func(request *linuxOwnerAnchorRequest) {
			request.Fence.Generation = request.RunKey.Generation + 1
		}, "fence generation"},
		{"retire-cleared run id", linuxOwnerAnchorOperationRetireCleared, func(request *linuxOwnerAnchorRequest) {
			request.RunKey.RunID = ""
		}, "run id"},
		{"retire-aborted helper validation", linuxOwnerAnchorOperationRetireAborted, func(request *linuxOwnerAnchorRequest) {
			request.Helper.Identity = ""
		}, "helper validation"},
		{"retire-aborted run id", linuxOwnerAnchorOperationRetireAborted, func(request *linuxOwnerAnchorRequest) {
			request.RunKey.RunID = ""
		}, "run id"},
		{"retire-aborted run key generation", linuxOwnerAnchorOperationRetireAborted, func(request *linuxOwnerAnchorRequest) {
			request.RunKey.Generation = 0
			request.Fence.Generation = 0
		}, "run key generation"},
		{"retire-aborted fence generation", linuxOwnerAnchorOperationRetireAborted, func(request *linuxOwnerAnchorRequest) {
			request.Fence.Generation = request.RunKey.Generation + 1
		}, "fence generation"},
		{"clone-cleanup run key", linuxOwnerAnchorOperationCloneCleanup, func(request *linuxOwnerAnchorRequest) {
			request.RunKey = LinuxOwnerAnchorRunKey{RunID: "other-run", Generation: 1}
		}, "run key equality"},
		{"clone-cleanup fence digest", linuxOwnerAnchorOperationCloneCleanup, func(request *linuxOwnerAnchorRequest) {
			request.FenceDigest = "sha256:other"
		}, "fence digest equality"},
		{"clone-cleanup prior removal", linuxOwnerAnchorOperationCloneCleanup, func(request *linuxOwnerAnchorRequest) {
			request.PriorRemovalProof = nil
		}, "absent target requires a prior removal proof"},
		{"clone-cleanup live target", linuxOwnerAnchorOperationCloneCleanup, func(request *linuxOwnerAnchorRequest) {
			request.TargetFDPresent = true
		}, "live target forbids a prior removal proof"},
	}
	covered := map[string]bool{}
	for _, item := range cases {
		row := linuxOwnerAnchorShapeRowFor(t, item.operation)
		request := linuxOwnerAnchorWireShapeRequest(t, fixture, item.operation)
		if row.refuses(request, row.serverRequiredFDs) {
			t.Fatalf("case %q starts from a request the row already refuses", item.name)
		}
		item.violate(&request)
		if !row.shapeRejected(request) {
			t.Fatalf("case %q (%s) was accepted by the relational rules", item.name, item.wantReason)
		}
		if !row.refuses(request, linuxOwnerAnchorShapeExpectedFDs(request, linuxOwnerAnchorShapeExpectations[item.operation])) {
			t.Fatalf("case %q (%s) was accepted by row.refuses", item.name, item.wantReason)
		}
		covered[item.operation] = true
	}
	for operation := range linuxOwnerAnchorShapeExpectations {
		row := linuxOwnerAnchorShapeRowFor(t, operation)
		if row.shape == nil && covered[operation] {
			t.Fatalf("operation %q has relational cases but no predicate", operation)
		}
		if row.shape != nil && !covered[operation] {
			t.Fatalf("operation %q has a relational predicate with no rejection case", operation)
		}
	}
}

func TestLinuxOwnerAnchorShapeTableAcceptsLocallyToleratedFields(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, false)

	tolerated := []struct {
		operation string
		tolerate  func(*linuxOwnerAnchorRequest)
	}{
		{linuxOwnerAnchorOperationStart, func(request *linuxOwnerAnchorRequest) {
			request.ReleaseProof = &authority.SupervisorHandoffReleaseProof{}
			request.AfterPID = 5
			request.FenceDigest = "sha256:tolerated"
		}},
		{linuxOwnerAnchorOperationAttach, func(request *linuxOwnerAnchorRequest) {
			request.RunKey = LinuxOwnerAnchorRunKey{RunID: "tolerated", Generation: 1}
			request.Fence = linuxOwnerAnchorShapeFence()
			request.AbortProof = &authority.LinuxCgroupReservationAbortProof{}
		}},
		{linuxOwnerAnchorOperationWaitReap, func(request *linuxOwnerAnchorRequest) {
			request.AfterPID = 9
			request.ReleaseProof = &authority.SupervisorHandoffReleaseProof{}
		}},
		{linuxOwnerAnchorOperationReapRun, func(request *linuxOwnerAnchorRequest) {
			request.AfterPID = 3
			request.FenceDigest = "sha256:tolerated"
		}},
		{linuxOwnerAnchorOperationRetire, func(request *linuxOwnerAnchorRequest) {
			request.RunKey = LinuxOwnerAnchorRunKey{RunID: "tolerated", Generation: 1}
			request.AfterPID = 4
			request.AbortProof = &authority.LinuxCgroupReservationAbortProof{}
		}},
	}
	for _, item := range tolerated {
		row := linuxOwnerAnchorShapeRowFor(t, item.operation)
		request := linuxOwnerAnchorWireShapeRequest(t, fixture, item.operation)
		item.tolerate(&request)
		expected := linuxOwnerAnchorShapeExpectations[item.operation]
		if row.refuses(request, linuxOwnerAnchorShapeExpectedFDs(request, expected)) {
			t.Fatalf("operation %q no longer tolerates the optional fields it accepted before the refactor", item.operation)
		}
	}
}

func linuxOwnerAnchorShapeWriteFrame(t *testing.T, write func(*net.UnixConn)) ([]byte, []int) {
	t.Helper()
	client, server := linuxOwnerAnchorWirePair(t)
	write(server)
	return linuxOwnerAnchorWireReadFrame(t, client)
}

func linuxOwnerAnchorShapeWriterPayload(t *testing.T, grant LinuxOwnerAnchorGrant) (
	helper, process authority.LinuxCgroupProcessIdentity,
	exitProof authority.LinuxCgroupExitProof,
	stopEvidence LinuxOwnerAnchorStopEvidence,
	reapEvidence []LinuxOwnerAnchorReapEvidence,
	cloneRemoval authority.LinuxCloneTargetRemovalProof,
	classification linuxOwnerAnchorRunChildClassification,
) {
	t.Helper()
	helper = authority.LinuxCgroupProcessIdentity{PID: linuxOwnerAnchorWireHelperPID, Identity: linuxOwnerAnchorWireHelperIdentity}
	process = authority.LinuxCgroupProcessIdentity{PID: linuxOwnerAnchorWireProcessPID, Identity: linuxOwnerAnchorWireProcessIdentity}
	exitProof = authority.LinuxCgroupExitProof{
		Version: 1, Process: helper,
		Witness: authority.LinuxCgroupProcessIdentity{PID: 5152, Identity: "linux:v2:capture-witness"},
		Waited:  true, Reaped: true,
	}
	stopEvidence = LinuxOwnerAnchorStopEvidence{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Anchor: grant.Anchor, Helper: helper,
		PinnedLeafFD: true, KillAttempted: true, KillAcknowledged: true, CgroupPopulatedZero: true,
	}
	reapEvidence = []LinuxOwnerAnchorReapEvidence{{
		Version: 1, RunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Parent: grant.Parent, Leaf: grant.Leaf, Helper: helper, Process: process, ProcessRole: "native",
		HelperExit: exitProof, ProcessExit: exitProof,
	}}
	cloneRemoval = authority.LinuxCloneTargetRemovalProof{
		Version: authority.LinuxCloneVersion, Intent: authority.LinuxCloneCleanupIntent{
			Version: authority.LinuxCloneVersion,
			Intent: authority.LinuxCloneIntent{
				Version: authority.LinuxCloneVersion, RunID: grant.RunKey.RunID, Generation: grant.RunKey.Generation, Target: "/b/clone",
			},
			FenceDigest: "sha256:capture-fence-digest", RootPath: "/b/clone-root",
			SeedReceipt: authority.LinuxCloneSeedReceipt{Version: authority.LinuxCloneVersion, Device: 9, Inode: 10, Size: 4096},
			CheckoutReceipt: authority.LinuxCloneCheckoutReceipt{
				Version: authority.LinuxCloneVersion, RunID: grant.RunKey.RunID, Generation: grant.RunKey.Generation,
				Target: "/b/clone", TargetDevice: 11, TargetInode: 12,
			},
			NormalReleaseProof: authority.SupervisorHandoffReleaseProof{
				LaunchToken: grant.LaunchToken, AnchorPID: grant.Anchor.PID, AnchorIdentity: grant.Anchor.Identity,
				OwnerUID: grant.OwnerUID, CgroupIdentity: &grant.Leaf,
			},
		},
		RootDevice: 17, RootInode: 18, TargetDevice: 19, TargetInode: 20,
		Disposition:        authority.LinuxCloneTargetDispositionRemoved,
		PathRewalkVerified: true, BOwnedMountConfined: true, TargetNlinkZero: true,
		RootDeleterUID: grant.OwnerUID, RootDeleter: helper,
		RootDeleterExit: exitProof,
	}
	classification = linuxOwnerAnchorRunChildClassification{
		Version: linuxOwnerAnchorRunChildClassificationVersion,
		RunKey:  grant.RunKey, OriginRunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
		Helper: helper, Process: process, Role: "native",
		Origin: linuxOwnerAnchorRunChildOriginRegisteredCurrent,
	}
	return helper, process, exitProof, stopEvidence, reapEvidence, cloneRemoval, classification
}

func TestLinuxOwnerAnchorShapeWriterGoldens(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, false)
	anchor, grant := fixture.anchor, fixture.grant
	helper, _, exitProof, stopEvidence, reapEvidence, cloneRemoval, classification := linuxOwnerAnchorShapeWriterPayload(t, grant)
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationStopLeaf,
		ClientPID: linuxOwnerAnchorWireHelperPID + 1, ClientIdentity: "linux:v2:capture-client",
		Grant: grant, Helper: &helper,
	}
	identityRequest := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationIdentity,
		ClientPID: linuxOwnerAnchorWireHelperPID + 1, ClientIdentity: "linux:v2:capture-client",
	}
	cloneRequest := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationCloneCleanup,
		ClientPID: linuxOwnerAnchorWireHelperPID + 1, ClientIdentity: "linux:v2:capture-client",
		RunKey: grant.RunKey, FenceDigest: cloneRemoval.Intent.FenceDigest, Cleanup: &cloneRemoval.Intent,
	}
	evidence := reapEvidence[0]
	cases := []struct {
		name  string
		write func(*net.UnixConn)
	}{
		{"plain", func(connection *net.UnixConn) {
			response := anchor.baseResponse(request, "stopped")
			response.Helper = &helper
			response.Evidence = &evidence
			anchor.writeResponse(connection, response)
		}},
		{"stop-evidence", func(connection *net.UnixConn) {
			response := anchor.baseResponse(request, "stopped")
			response.StopEvidence = &stopEvidence
			anchor.writeResponse(connection, response)
		}},
		{"clone-removal", func(connection *net.UnixConn) {
			response := anchor.baseResponse(request, "clone_removed")
			response.Helper = &helper
			response.Evidence = &evidence
			response.CloneRemoval = &cloneRemoval
			anchor.writeResponse(connection, response)
		}},
		{"descriptors", func(connection *net.UnixConn) {
			response := anchor.baseResponse(request, "attached")
			response.Helper = &helper
			response.Evidence = &evidence
			anchor.writeResponse(connection, response, linuxOwnerAnchorWireFD(t), linuxOwnerAnchorWireFD(t), linuxOwnerAnchorWireFD(t))
		}},
		{"reap-evidence", func(connection *net.UnixConn) {
			response := anchor.baseResponse(request, "reaped")
			response.ReapEvidence = reapEvidence
			response.StopEvidence = &stopEvidence
			response.NextPID = linuxOwnerAnchorWireProcessPID
			response.More = true
			anchor.writeResponse(connection, response)
		}},
		{"child-classification", func(connection *net.UnixConn) {
			response := anchor.baseResponse(request, "classified")
			response.Helper = request.Helper
			response.ChildClassification = &classification
			anchor.writeResponse(connection, response)
		}},
		{"helper-exit", func(connection *net.UnixConn) {
			response := anchor.baseResponse(request, "reaped")
			response.Helper = request.Helper
			response.HelperExit = &exitProof
			anchor.writeResponse(connection, response)
		}},
		{"run-pins", func(connection *net.UnixConn) {
			response := anchor.baseResponse(request, "helper_exited")
			response.Helper = &helper
			response.HelperExit = &exitProof
			anchor.writeResponse(connection, response, linuxOwnerAnchorWireFD(t), linuxOwnerAnchorWireFD(t))
		}},
		{"clone-override", func(connection *net.UnixConn) {
			response := anchor.baseResponse(cloneRequest, "clone_removed")
			response.CloneRemoval = &cloneRemoval
			anchor.writeResponse(connection, response)
		}},
		{"zero-identity", func(connection *net.UnixConn) {
			anchor.writeResponse(connection, anchor.baseResponse(identityRequest, "ready"))
		}},
	}
	if len(cases) != len(linuxOwnerAnchorShapeWriterGoldens) {
		t.Fatalf("writer cases = %d, pinned goldens = %d", len(cases), len(linuxOwnerAnchorShapeWriterGoldens))
	}
	for _, item := range cases {
		pinned, ok := linuxOwnerAnchorShapeWriterGoldens[item.name]
		if !ok {
			t.Fatalf("writer case %q has no pinned golden", item.name)
		}
		want := linuxOwnerAnchorShapeGoldenFrame(t, pinned)
		frame, fds := linuxOwnerAnchorShapeWriteFrame(t, item.write)
		if string(frame) != want {
			t.Fatalf("writer case %q frame = %q, want the pinned bytes %q", item.name, frame, want)
		}
		if len(fds) != linuxOwnerAnchorShapeWriterGoldenFDs[item.name] {
			t.Fatalf("writer case %q carried %d descriptors, want %d", item.name, len(fds), linuxOwnerAnchorShapeWriterGoldenFDs[item.name])
		}
	}

	override := linuxOwnerAnchorShapeWriterGoldens["clone-override"]
	if !strings.Contains(override, `"fence_digest":"`+cloneRemoval.Intent.FenceDigest+`"`) ||
		!strings.Contains(override, `"launch_token":"`+grant.LaunchToken+`"`) ||
		!strings.Contains(override, `"run_key":{"run_id":"`+grant.RunKey.RunID+`"`) {
		t.Fatal("the pinned clone-cleanup frame does not carry the cleanup intent's run key, fence digest and launch token")
	}
	// The zero-valued case pins that the identity response still carries the zero run key, fence and launch token.
	zero := linuxOwnerAnchorShapeWriterGoldens["zero-identity"]
	for _, fragment := range []string{`"run_key":{"run_id":"","generation":0}`, `"launch_token":""`, `"fence":{`} {
		if !strings.Contains(zero, fragment) {
			t.Fatalf("the pinned identity frame is missing %s", fragment)
		}
	}
}

func TestLinuxOwnerAnchorShapeWriterSignatureFrameIsStable(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, false)
	anchor, grant := fixture.anchor, fixture.grant
	helper, _, _, _, _, _, _ := linuxOwnerAnchorShapeWriterPayload(t, grant)
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationAttach,
		ClientPID: linuxOwnerAnchorWireHelperPID + 1, ClientIdentity: "linux:v2:capture-client",
		Grant: grant, Helper: &helper,
	}
	response := anchor.baseResponse(request, "attached")
	response.Helper = &helper
	pinFree, _ := linuxOwnerAnchorShapeWriteFrame(t, func(connection *net.UnixConn) {
		anchor.writeResponse(connection, response)
	})
	withPins, fds := linuxOwnerAnchorShapeWriteFrame(t, func(connection *net.UnixConn) {
		anchor.writeResponse(connection, response, linuxOwnerAnchorWireFD(t), linuxOwnerAnchorWireFD(t))
	})
	if len(fds) != 2 {
		t.Fatalf("pin-carrying write transferred %d descriptors, want 2", len(fds))
	}
	if string(pinFree) != string(withPins) {
		t.Fatalf("pin-carrying frame %q differs from the pin-free frame %q", withPins, pinFree)
	}
	var decoded linuxOwnerAnchorResponse
	if err := json.Unmarshal(pinFree, &decoded); err != nil {
		t.Fatalf("decode pin-free frame: %v", err)
	}
	if decoded.Operation != linuxOwnerAnchorOperationAttach || decoded.Status != "attached" {
		t.Fatalf("decoded pin-free frame = %q/%q, want the attach response", decoded.Operation, decoded.Status)
	}
}
