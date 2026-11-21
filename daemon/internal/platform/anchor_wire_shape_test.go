//go:build linux && amd64

package platform

// Unprivileged wire-shape harness for the anchor control socket (rf-w3-02,
// DP S-4). It drives the real serveConnection over a real unixpacket socket
// with an in-package linuxOwnerAnchor, so every assertion below is a frame
// count, a byte string, or a descriptor count that came off the kernel.
//
// Reachability, measured on this host: the anchor's pinned-parent and manager
// membership checks cannot pass for an unprivileged single-process harness
// (the peer is the anchor's own cgroup, and a delegated A/B parent pin needs a
// root-provisioned cgroup), so only the statuses written before those checks
// are reachable end to end:
//   - `anchor_stopping` for the seven operations that answer a stopping anchor;
//   - `request_rejected` for a clone-cleanup request that fails its own
//     validation after the shape and descriptor gates.
// Everything else (started/attached/helper_exited/retired/reaped/classified/
// registered/release_authorized/stopped/ready/run_unavailable/...) is pinned at
// the writer level by anchor_shape_table_test.go's golden byte strings and its
// descriptor counts; those statuses stay unpinned end to end here because
// reaching them requires a spawned helper run and a real delegated cgroup
// topology, not because the code path is untested. No test in this file skips.

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

const linuxOwnerAnchorWireManagerPath = "/system.slice/symmetry-owner.service/manager"

// The pinned frame goldens embed the fixture's anchor process identity and the
// parent boot ID, so the harness uses literal values instead of this host's own
// PID and boot ID; the test process only has to differ from the anchor PID.
const (
	linuxOwnerAnchorWireAnchorPID = 4242
	linuxOwnerAnchorWireBootID    = "00000000-0000-0000-0000-000000000000"
	// The identities below are the literal values the capture harness used, so
	// the pinned goldens stay verbatim.
	linuxOwnerAnchorWireAnchorIdentity  = "linux:v2:capture-anchor"
	linuxOwnerAnchorWireHelperIdentity  = "linux:v2:capture-helper"
	linuxOwnerAnchorWireProcessIdentity = "linux:v2:capture-process"
	linuxOwnerAnchorWireHelperPID       = 5150
	linuxOwnerAnchorWireProcessPID      = 5151
)

// The three operations below were string literals before this slice. The local
// names are deliberate: this harness must also keep compiling against the
// pre-slice base tree, which has no such production constants, so the recorded
// pre-refactor run stays reproducible. The wire strings themselves are pinned
// byte-for-byte by the goldens in anchor_shape_goldens_test.go.
const (
	linuxOwnerAnchorWireIdentityOperation        = "identity"
	linuxOwnerAnchorWireRegisterProcessOperation = "register-process"
	linuxOwnerAnchorWireWaitReapOperation        = "wait-reap"
)

// wireDescriptorCounts is the hard-coded per-operation descriptor count a
// request must carry. It is deliberately not read from the production table:
// a table mutation must fail this test, not move its expectation.
var wireDescriptorCounts = map[string]int{
	linuxOwnerAnchorWireIdentityOperation:        0,
	linuxOwnerAnchorOperationStart:               2,
	linuxOwnerAnchorOperationAttach:              0,
	linuxOwnerAnchorOperationLookup:              0,
	linuxOwnerAnchorWireRegisterProcessOperation: 1,
	linuxOwnerAnchorOperationClassifyChild:       1,
	linuxOwnerAnchorOperationRegisterGate:        1,
	linuxOwnerAnchorOperationAuthorizeGate:       0,
	linuxOwnerAnchorOperationStopLeaf:            0,
	linuxOwnerAnchorWireWaitReapOperation:        0,
	linuxOwnerAnchorOperationReapRun:             0,
	linuxOwnerAnchorOperationWaitHelper:          0,
	linuxOwnerAnchorOperationRetire:              0,
	linuxOwnerAnchorOperationRetireCleared:       0,
	linuxOwnerAnchorOperationRetireAborted:       0,
	linuxOwnerAnchorOperationCloneCleanup:        1,
}

type linuxOwnerAnchorWireFixture struct {
	anchor  *linuxOwnerAnchor
	grant   LinuxOwnerAnchorGrant
	helper  authority.LinuxCgroupProcessIdentity
	process authority.LinuxCgroupProcessIdentity
}

func newLinuxOwnerAnchorWireFixture(t *testing.T, stopping bool) linuxOwnerAnchorWireFixture {
	t.Helper()
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	if uid == 0 || gid == 0 || uint32(os.Geteuid()) != uid || uint32(os.Getegid()) != gid {
		t.Fatalf("wire harness requires an unprivileged A process: uid=%d euid=%d gid=%d egid=%d", uid, os.Geteuid(), gid, os.Getegid())
	}
	identity := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 4096, Identity: linuxOwnerAnchorWireAnchorIdentity}
	grant, err := testLinuxOwnerAnchorGrant(identity, linuxOwnerAnchorWireManagerPath)
	if err != nil {
		t.Fatalf("build wire-shape grant: %v", err)
	}
	grant.OwnerUID = uid
	identity = authority.LinuxCgroupProcessIdentity{PID: linuxOwnerAnchorWireAnchorPID, Identity: linuxOwnerAnchorWireAnchorIdentity}
	grant.Anchor = identity
	grant.Parent.BootID = linuxOwnerAnchorWireBootID
	grant.Leaf.Parent = grant.Parent
	anchor := &linuxOwnerAnchor{
		owner:       linuxgate.OwnerConfig{AUID: uid, AGID: gid, BUID: uid + 1, BGID: gid + 1},
		parent:      &LinuxCgroupV2Parent{fd: -1, identity: grant.Parent, ownerUID: uid},
		identity:    identity,
		managerPath: linuxOwnerAnchorWireManagerPath,
		stopping:    stopping,
		runs:        map[LinuxOwnerAnchorRunKey]*linuxOwnerAnchorRun{},
	}
	return linuxOwnerAnchorWireFixture{
		anchor:  anchor,
		grant:   grant,
		helper:  authority.LinuxCgroupProcessIdentity{PID: linuxOwnerAnchorWireHelperPID, Identity: linuxOwnerAnchorWireHelperIdentity},
		process: authority.LinuxCgroupProcessIdentity{PID: linuxOwnerAnchorWireProcessPID, Identity: linuxOwnerAnchorWireProcessIdentity},
	}
}

func linuxOwnerAnchorWireClientIdentity(t *testing.T) string {
	t.Helper()
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("capture wire harness client identity: %v", err)
	}
	return identity
}

func linuxOwnerAnchorWireFD(t *testing.T) int {
	t.Helper()
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return int(file.Fd())
}

func linuxOwnerAnchorWireFDs(t *testing.T, count int) []int {
	t.Helper()
	fds := make([]int, 0, count)
	for range count {
		fds = append(fds, linuxOwnerAnchorWireFD(t))
	}
	return fds
}

// linuxOwnerAnchorWireShapeRequest builds the request one operation's shape
// requires, with every optional field of the other operations left absent.
func linuxOwnerAnchorWireShapeRequest(t *testing.T, fixture linuxOwnerAnchorWireFixture, operation string) linuxOwnerAnchorRequest {
	t.Helper()
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: operation,
		ClientPID: os.Getpid(), ClientIdentity: linuxOwnerAnchorWireClientIdentity(t),
	}
	helper, process := fixture.helper, fixture.process
	switch operation {
	case linuxOwnerAnchorWireIdentityOperation:
	case linuxOwnerAnchorOperationStart:
		request.Grant = fixture.grant
	case linuxOwnerAnchorOperationAttach:
		request.Grant, request.Helper = fixture.grant, &helper
	case linuxOwnerAnchorOperationLookup:
		request.Grant = fixture.grant
	case linuxOwnerAnchorWireRegisterProcessOperation:
		request.Grant, request.Helper, request.Creator, request.Process, request.Role = fixture.grant, &helper, &helper, &process, "native"
	case linuxOwnerAnchorOperationClassifyChild:
		request.Grant, request.Helper, request.Process = fixture.grant, &helper, &process
	case linuxOwnerAnchorOperationRegisterGate, linuxOwnerAnchorOperationAuthorizeGate:
		gate := authority.LinuxCgroupGateIdentity{
			Process: helper, HostUID: fixture.grant.OwnerUID, ObservedAt: time.Unix(0, 0),
			BirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, Cgroup: fixture.grant.Leaf,
			BornInLeaf: true, TrustedImage: true,
		}
		userNamespace := LinuxUserNamespaceIdentity{Device: 21, Inode: 22}
		request.Grant, request.Helper, request.Gate, request.UserNamespace = fixture.grant, &helper, &gate, &userNamespace
	case linuxOwnerAnchorOperationStopLeaf:
		fence := LinuxOwnerAnchorProducerFence{
			Version: linuxOwnerAnchorStopEvidenceVersion,
			RunKey:  fixture.grant.RunKey, Fence: fixture.grant.Fence, LaunchToken: fixture.grant.LaunchToken,
			Helper: helper, StopLatched: true, LeaseStopped: true, PrepareQuiesced: true,
			ReleaseQuiesced: true, LeafMembershipVerified: true,
		}
		request.Grant, request.Helper, request.StopFence = fixture.grant, &helper, &fence
	case linuxOwnerAnchorWireWaitReapOperation:
		request.Grant, request.Helper, request.Process, request.Role = fixture.grant, &helper, &process, "native"
	case linuxOwnerAnchorOperationReapRun, linuxOwnerAnchorOperationWaitHelper:
		request.Grant, request.Helper = fixture.grant, &helper
	case linuxOwnerAnchorOperationRetire:
		proof := authority.SupervisorHandoffReleaseProof{LaunchToken: fixture.grant.LaunchToken}
		request.Grant, request.Helper, request.ReleaseProof = fixture.grant, &helper, &proof
	case linuxOwnerAnchorOperationRetireCleared:
		proof := authority.SupervisorHandoffReleaseProof{LaunchToken: fixture.grant.LaunchToken}
		request.Helper, request.RunKey, request.Fence, request.ReleaseProof = &helper, fixture.grant.RunKey, fixture.grant.Fence, &proof
	case linuxOwnerAnchorOperationRetireAborted:
		proof := authority.LinuxCgroupReservationAbortProof{Version: 1, LaunchToken: fixture.grant.LaunchToken}
		request.Helper, request.RunKey, request.Fence, request.AbortProof = &helper, fixture.grant.RunKey, fixture.grant.Fence, &proof
	case linuxOwnerAnchorOperationCloneCleanup:
		cloneGrant := authority.LinuxCloneGrant{}
		cleanup := authority.LinuxCloneCleanupIntent{}
		prior := authority.LinuxCloneTargetRemovalProof{}
		request.CloneGrant, request.Cleanup, request.PriorRemovalProof = &cloneGrant, &cleanup, &prior
	default:
		t.Fatalf("wire harness has no shape for operation %q", operation)
	}
	return request
}

// linuxOwnerAnchorWirePair opens one unixpacket pair with a 30-second deadline
// on both ends and returns the client and server sides.
func linuxOwnerAnchorWirePair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	endpoint := filepath.Join(t.TempDir(), "owner.sock")
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: endpoint, Net: "unixpacket"})
	if err != nil {
		t.Fatalf("listen unixpacket: %v", err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: endpoint, Net: "unixpacket"})
	if err != nil {
		t.Fatalf("dial unixpacket: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatalf("accept unixpacket: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	deadline := time.Now().Add(30 * time.Second)
	if err := client.SetDeadline(deadline); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}
	if err := server.SetDeadline(deadline); err != nil {
		t.Fatalf("set server deadline: %v", err)
	}
	return client, server
}

// linuxOwnerAnchorWireReadFrame reads the single frame the server wrote and its
// descriptors; a closed connection is the silent refusal.
func linuxOwnerAnchorWireReadFrame(t *testing.T, client *net.UnixConn) ([]byte, []int) {
	t.Helper()
	data := make([]byte, linuxOwnerAnchorMaxFrame+1)
	oob := make([]byte, syscall.CmsgSpace(8*4))
	n, oobn, _, _, err := client.ReadMsgUnix(data, oob)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		t.Fatalf("read wire frame: %v", err)
	}
	received, err := parseLinuxOwnerAnchorRights(oob[:oobn], 0)
	if err != nil {
		t.Fatalf("parse wire frame descriptors: %v", err)
	}
	return data[:n], received
}

// linuxOwnerAnchorWireServeRaw sends already-encoded bytes into a fresh
// unixpacket pair and returns the frame serveConnection wrote, or nil when it
// refused the request and closed the connection (the anchor closes every
// non-detached connection). Codec-level rejections use it to send a request
// that must not be shape-clean.
func linuxOwnerAnchorWireServeRaw(t *testing.T, anchor *linuxOwnerAnchor, encoded []byte, rights []byte) ([]byte, []int) {
	t.Helper()
	client, server := linuxOwnerAnchorWirePair(t)
	if _, _, err := client.WriteMsgUnix(encoded, rights, nil); err != nil {
		t.Fatalf("write wire request: %v", err)
	}
	anchor.serveConnection(server)
	return linuxOwnerAnchorWireReadFrame(t, client)
}

// linuxOwnerAnchorWireServe marshals one request and sends it with the given
// descriptors.
func linuxOwnerAnchorWireServe(t *testing.T, anchor *linuxOwnerAnchor, request linuxOwnerAnchorRequest, fds []int) ([]byte, []int) {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal wire request: %v", err)
	}
	var rights []byte
	if len(fds) != 0 {
		rights = syscall.UnixRights(fds...)
	}
	return linuxOwnerAnchorWireServeRaw(t, anchor, encoded, rights)
}

func TestLinuxOwnerAnchorWireShapeRejectsWrongDescriptorCounts(t *testing.T) {
	if len(wireDescriptorCounts) != 16 {
		t.Fatalf("wire descriptor table covers %d operations, want 16", len(wireDescriptorCounts))
	}
	fixture := newLinuxOwnerAnchorWireFixture(t, false)
	for operation, required := range wireDescriptorCounts {
		// remove-b-clone-after-receipt's count is TargetFDPresent-dependent and
		// is covered in both directions by the clone-cleanup gate test below.
		if operation == linuxOwnerAnchorOperationCloneCleanup {
			continue
		}
		cases := []int{required + 1}
		if required > 0 {
			cases = append(cases, required-1)
		}
		for _, count := range cases {
			request := linuxOwnerAnchorWireShapeRequest(t, fixture, operation)
			frame, fds := linuxOwnerAnchorWireServe(t, fixture.anchor, request, linuxOwnerAnchorWireFDs(t, count))
			if frame != nil || len(fds) != 0 {
				t.Fatalf("operation %q with %d descriptors was answered with %d frame bytes and %d descriptors; want a silent refusal", operation, count, len(frame), len(fds))
			}
		}
	}
}

func TestLinuxOwnerAnchorWireCloneCleanupDescriptorGate(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, false)
	// A clone-cleanup request that satisfies the shape and descriptor gates
	// reaches its own validation, so the accepted and refused counts are
	// distinguishable here: exactly the row's count is answered, every other
	// count is refused silently.
	request := linuxOwnerAnchorWireShapeRequest(t, fixture, linuxOwnerAnchorOperationCloneCleanup)
	frame, fds := linuxOwnerAnchorWireServe(t, fixture.anchor, request, linuxOwnerAnchorWireFDs(t, 1))
	if frame == nil || len(fds) != 0 {
		t.Fatalf("one-descriptor clone cleanup = %d frame bytes / %d descriptors; want the row's accepted count answered", len(frame), len(fds))
	}
	priorRemovalGolden := linuxOwnerAnchorShapeGoldenFrame(t, linuxOwnerAnchorWireClonePriorRemovalGolden)
	if string(frame) != priorRemovalGolden {
		t.Fatalf("one-descriptor clone cleanup frame = %q, want the pinned bytes %q", frame, priorRemovalGolden)
	}
	withTarget := linuxOwnerAnchorWireShapeRequest(t, fixture, linuxOwnerAnchorOperationCloneCleanup)
	withTarget.TargetFDPresent, withTarget.PriorRemovalProof = true, nil
	frame, fds = linuxOwnerAnchorWireServe(t, fixture.anchor, withTarget, linuxOwnerAnchorWireFDs(t, 2))
	if frame == nil || len(fds) != 0 {
		t.Fatalf("two-descriptor clone cleanup = %d frame bytes / %d descriptors; want the row's variable count answered", len(frame), len(fds))
	}
	liveTargetGolden := linuxOwnerAnchorShapeGoldenFrame(t, linuxOwnerAnchorWireCloneLiveTargetGolden)
	if string(frame) != liveTargetGolden {
		t.Fatalf("two-descriptor clone cleanup frame = %q, want the pinned bytes %q", frame, liveTargetGolden)
	}
	for _, refused := range []struct {
		target bool
		fds    int
	}{
		{false, 0}, {false, 2}, {true, 1}, {true, 3},
	} {
		bad := linuxOwnerAnchorWireShapeRequest(t, fixture, linuxOwnerAnchorOperationCloneCleanup)
		if refused.target {
			bad.TargetFDPresent, bad.PriorRemovalProof = true, nil
		}
		frame, fds := linuxOwnerAnchorWireServe(t, fixture.anchor, bad, linuxOwnerAnchorWireFDs(t, refused.fds))
		if frame != nil || len(fds) != 0 {
			t.Fatalf("clone cleanup target=%t with %d descriptors was answered with %d bytes; want a silent refusal", refused.target, refused.fds, len(frame))
		}
	}
}

func TestLinuxOwnerAnchorWireStoppingAnswerPrecedesShape(t *testing.T) {
	if len(linuxOwnerAnchorWireStoppingGoldens) != 7 {
		t.Fatalf("stopping golden table covers %d operations, want 7", len(linuxOwnerAnchorWireStoppingGoldens))
	}
	fixture := newLinuxOwnerAnchorWireFixture(t, true)
	for operation, pinned := range linuxOwnerAnchorWireStoppingGoldens {
		golden := linuxOwnerAnchorShapeGoldenFrame(t, pinned)
		request := linuxOwnerAnchorWireShapeRequest(t, fixture, operation)
		required := wireDescriptorCounts[operation]
		frame, fds := linuxOwnerAnchorWireServe(t, fixture.anchor, request, linuxOwnerAnchorWireFDs(t, required))
		if len(fds) != 0 {
			t.Fatalf("stopping answer for %q carried %d descriptors, want none", operation, len(fds))
		}
		if string(frame) != golden {
			t.Fatalf("stopping answer for %q = %q, want the pinned bytes %q", operation, frame, golden)
		}
		// A stopping anchor answers before the per-operation shape gate runs:
		// a wrong descriptor count must still produce the same frame.
		wrong := required + 1
		frame, fds = linuxOwnerAnchorWireServe(t, fixture.anchor, request, linuxOwnerAnchorWireFDs(t, wrong))
		if len(fds) != 0 || string(frame) != golden {
			t.Fatalf("stopping answer for %q with %d descriptors = %q / %d descriptors, want the pinned bytes and no descriptors", operation, wrong, frame, len(fds))
		}
	}
}

// TestLinuxOwnerAnchorWireExclusiveFieldsRefusedBeforeStoppingAnswer pins the
// phase-1 ordering the plan promises: the six clone/gate/stop-fence fields that
// only their owning operation group may carry are refused silently even by a
// stopping anchor, before its anchor_stopping answer. A single hoisted gate (the
// phase-1 check dropped, or one of its six fields dropped) answers a frame here.
func TestLinuxOwnerAnchorWireExclusiveFieldsRefusedBeforeStoppingAnswer(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, true)
	cases := []struct {
		name    string
		present func(*linuxOwnerAnchorRequest)
	}{
		{"clone grant", func(request *linuxOwnerAnchorRequest) { request.CloneGrant = &authority.LinuxCloneGrant{} }},
		{"clone cleanup intent", func(request *linuxOwnerAnchorRequest) {
			request.Cleanup = &authority.LinuxCloneCleanupIntent{}
		}},
		{"clone target fd", func(request *linuxOwnerAnchorRequest) { request.TargetFDPresent = true }},
		{"gate identity", func(request *linuxOwnerAnchorRequest) { request.Gate = &authority.LinuxCgroupGateIdentity{} }},
		{"user namespace", func(request *linuxOwnerAnchorRequest) {
			request.UserNamespace = &LinuxUserNamespaceIdentity{}
		}},
		{"stop fence", func(request *linuxOwnerAnchorRequest) {
			request.StopFence = &LinuxOwnerAnchorProducerFence{}
		}},
	}
	for _, item := range cases {
		request := linuxOwnerAnchorWireShapeRequest(t, fixture, linuxOwnerAnchorWireIdentityOperation)
		item.present(&request)
		frame, fds := linuxOwnerAnchorWireServe(t, fixture.anchor, request, nil)
		if frame != nil || len(fds) != 0 {
			t.Fatalf("stopping anchor answered a shape-forbidden identity request (%s) with %d frame bytes and %d descriptors; want the silent phase-1 refusal", item.name, len(frame), len(fds))
		}
	}
}

func TestLinuxOwnerAnchorWireCodecRejectsMalformedFrames(t *testing.T) {
	fixture := newLinuxOwnerAnchorWireFixture(t, false)
	clean := linuxOwnerAnchorWireShapeRequest(t, fixture, linuxOwnerAnchorWireIdentityOperation)
	encoded, err := json.Marshal(clean)
	if err != nil {
		t.Fatalf("marshal clean identity request: %v", err)
	}
	oversize := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorWireIdentityOperation,
		ClientPID: os.Getpid(), ClientIdentity: linuxOwnerAnchorWireClientIdentity(t),
		Role: strings.Repeat("x", linuxOwnerAnchorMaxFrame),
	}
	oversizeEncoded, err := json.Marshal(oversize)
	if err != nil {
		t.Fatalf("marshal oversize request: %v", err)
	}
	cases := []struct {
		name    string
		encoded []byte
		rights  []byte
	}{
		{"trailing-json", append(append([]byte{}, encoded...), []byte("{}")...), nil},
		{"unknown-field", []byte(`{"version":1,"operation":"identity","client_pid":1,"unknown_field":true}`), nil},
		{"oversize-frame", oversizeEncoded, nil},
		{"descriptor-overflow", encoded, syscall.UnixRights(linuxOwnerAnchorWireFDs(t, 5)...)},
	}
	for _, item := range cases {
		frame, fds := linuxOwnerAnchorWireServeRaw(t, fixture.anchor, item.encoded, item.rights)
		if frame != nil || len(fds) != 0 {
			t.Fatalf("%s was answered with %d frame bytes and %d descriptors; want a silent refusal", item.name, len(frame), len(fds))
		}
	}
}
