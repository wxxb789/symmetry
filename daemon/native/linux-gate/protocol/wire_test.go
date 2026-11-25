package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

const (
	wireTestLaunchRequestGoldenHex         = "53594d4c41303031010001000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20ababababababababababababababababcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	wireTestLaunchResultGoldenHex          = "53594d4c413030310100020000000000000000000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f2069100000e9030000ea0300006a1000000000000075f3c8f4e500000004000000000000001300000000000000a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"
	wireTestGateReadyGoldenHex             = "53594d4c413030310100030000000000"
	wireTestGateParkedGoldenHex            = "53594d4c413030310100050000000000"
	wireTestLauncherReadyFailureGoldenHex  = "53594d4c4130303101000a0005000000"
	wireTestExecAttemptGoldenHex           = "53594d4c413030310100070007000000000000001300000000000000"
	wireTestExecFailureGoldenHex           = "53594d4c41303031010008000d000000"
	wireTestBootstrapExecAttemptGoldenHex  = "53594d4c413030310300140007000000000000001300000000000000"
	wireTestNativeExecAttemptGoldenHex     = "53594d4c41303031030017000b000000000000001700000000000000"
	wireTestCloneBootstrapFailureGoldenHex = "53594d4c41303031030018000200000005000000"
	// Fixed lease cutoff that keeps the guarded RELEASE bytes independent of CLOCK_MONOTONIC.
	wireTestReleaseGoldenDeadlineNS = uint64(123456789)
	// The v2 scratch RELEASE frame; its encoder is portable, so the pinned bytes run on every GOOS.
	wireTestReleaseV2GoldenHex = "53594d4c41303031020006000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20310000000400000015cd5b0700000000090000002f62696e2f7472756501000000090000002f62696e2f7472756501000000060000004c414e473d43010000002f"
)

const (
	wireTestUnknownFrameKind = uint16(0x00ff)
	wireTestUnknownVersion   = uint16(0xffff)
	wireTestCorruptMagicByte = byte(0x00)
)

func wireTestToken() [32]byte {
	var token [32]byte
	for index := range token {
		token[index] = byte(index + 1)
	}
	return token
}

func wireTestAuthenticator() [32]byte {
	var authenticator [32]byte
	for index := range authenticator {
		authenticator[index] = byte(0xa0 + index)
	}
	return authenticator
}

func wireTestGateProductionAuth() GateProductionAuth {
	return GateProductionAuth{
		LaunchToken: strings.Repeat("ab", authority.TokenBytes),
		Secret:      strings.Repeat("cd", authority.SecretBytes),
	}
}

func wireTestLaunchRequestFixture(t *testing.T) ([32]byte, GateProductionAuth, LaunchRequest) {
	t.Helper()
	token := wireTestToken()
	auth := wireTestGateProductionAuth()
	launchToken, err := hex.DecodeString(auth.LaunchToken)
	if err != nil || len(launchToken) != authority.TokenBytes {
		t.Fatalf("wire test launch token %q is not canonical hex: %v", auth.LaunchToken, err)
	}
	secret, err := hex.DecodeString(auth.Secret)
	if err != nil || len(secret) != authority.SecretBytes {
		t.Fatalf("wire test authority secret is not canonical hex: %v", err)
	}
	request := LaunchRequest{ReleaseToken: token, LaunchToken: auth.LaunchToken}
	copy(request.Secret[:], secret)
	return token, auth, request
}

func wireTestLauncherIdentity() LauncherIdentity {
	return LauncherIdentity{PID: 4200, StartTime: 987654321012, BootID: "boot-1", Identity: "launcher:4200:start"}
}

func wireTestLauncherReady() LauncherReady {
	identity := wireTestLauncherIdentity()
	return LauncherReady{PID: identity.PID, StartTime: identity.StartTime, Identity: identity.Identity}
}

func wireTestProcessIdentity() ProcessIdentity {
	return ProcessIdentity{
		PID: 4201, StartTime: 987654321013,
		UserNSDevice: 4, UserNSInode: 19, HostUID: 1001, HostGID: 1002, OwnerPID: 4202,
	}
}

func wireTestLaunchResult() LaunchResult {
	return LaunchResult{
		Status: LaunchStatusReady, Token: wireTestToken(),
		Identity: wireTestProcessIdentity(), Authenticator: wireTestAuthenticator(),
	}
}

func wireTestCheckoutReceipt() authority.LinuxCloneCheckoutReceipt {
	return authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: "run-1", Generation: 1,
		BindingKey: "binding-1", ResourceID: "00000000-0000-4000-8000-000000000001",
		Target: "/tmp/symmetry-target", Nonce: strings.Repeat("c", authority.LinuxCloneNonceBytes*2),
		SeedDigest: "sha256:" + strings.Repeat("d", 64), BaseCommit: strings.Repeat("a", 40),
		TargetDevice: 7, TargetInode: 19,
	}
}

func wireTestCloneCheckoutPacket(kind uint16, receipt authority.LinuxCloneCheckoutReceipt) []byte {
	packet := cloneStatusFrame(kind, 12)
	packet = appendWireUint32(packet, uint32(receipt.Version))
	packet = appendWireString(packet, receipt.RunID)
	packet = appendWireUint64(packet, uint64(receipt.Generation))
	packet = appendWireString(packet, receipt.BindingKey)
	packet = appendWireString(packet, receipt.ResourceID)
	packet = appendWireString(packet, receipt.Target)
	packet = appendWireString(packet, receipt.Nonce)
	packet = appendWireString(packet, receipt.SeedDigest)
	packet = appendWireString(packet, receipt.BaseCommit)
	packet = appendWireUint64(packet, receipt.TargetDevice)
	packet = appendWireUint64(packet, receipt.TargetInode)
	return packet
}

type wireRoundTripCase struct {
	name    string
	kind    uint16
	version uint16
	encode  func() ([]byte, error)
	// Returns the canonical decoded form; a decode error is fatal for the case.
	decode func(t *testing.T, packet []byte) string
	want   string
	// Pins the exact frame bytes; empty for variable-length frames.
	goldenHex string
}

func TestWireFrameRoundTrips(t *testing.T) {
	cases := wireTestRoundTripCases(t)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			packet, err := testCase.encode()
			if err != nil {
				t.Fatalf("encode %s frame: %v", testCase.name, err)
			}
			if len(packet) < 12 || string(packet[:8]) != launchMagic {
				t.Fatalf("%s frame lacks the %s magic: %x", testCase.name, launchMagic, packet)
			}
			if version := binary.LittleEndian.Uint16(packet[8:10]); version != testCase.version {
				t.Errorf("%s frame version = %d, want %d", testCase.name, version, testCase.version)
			}
			if kind := binary.LittleEndian.Uint16(packet[10:12]); kind != testCase.kind {
				t.Errorf("%s frame kind = %d, want %d", testCase.name, kind, testCase.kind)
			}
			if testCase.goldenHex != "" {
				if got := hex.EncodeToString(packet); got != testCase.goldenHex {
					t.Errorf("%s golden bytes = %s, want %s", testCase.name, got, testCase.goldenHex)
				}
			}
			if got := testCase.decode(t, packet); got != testCase.want {
				t.Errorf("%s round trip = %s, want %s", testCase.name, got, testCase.want)
			}
		})
	}
}

func TestWireGuardedReleaseV2Encode(t *testing.T) {
	command := NativeCommand{Path: "/bin/true", Args: []string{"/bin/true"}, Env: []string{"LANG=C"}, Dir: "/"}
	packet, err := encodeGuardedRelease(wireTestToken(), command, wireTestReleaseGoldenDeadlineNS)
	if err != nil {
		t.Fatalf("encode portable v2 RELEASE: %v", err)
	}
	if version := binary.LittleEndian.Uint16(packet[8:10]); version != releaseWireVersion ||
		binary.LittleEndian.Uint64(packet[52:60]) != wireTestReleaseGoldenDeadlineNS {
		t.Fatalf("portable v2 RELEASE header = %x", packet[:releaseHeaderSize])
	}
	if got := hex.EncodeToString(packet); got != wireTestReleaseV2GoldenHex {
		t.Errorf("portable v2 RELEASE golden bytes = %s, want %s", got, wireTestReleaseV2GoldenHex)
	}
	if _, err := encodeGuardedRelease(wireTestToken(), command, 0); err == nil ||
		err.Error() != "Linux RELEASE requires a nonzero monotonic lease cutoff" {
		t.Errorf("zero-deadline encode refusal = %v", err)
	}
}

func wireTestRoundTripCases(t *testing.T) []wireRoundTripCase {
	t.Helper()
	token, auth, request := wireTestLaunchRequestFixture(t)
	result := wireTestLaunchResult()
	ready := wireTestLauncherReady()
	receipt := wireTestCheckoutReceipt()
	cases := []wireRoundTripCase{
		{
			name: "launch request", kind: frameLaunchRequest, version: wireVersion,
			encode: func() ([]byte, error) { return encodeLaunchRequest(token, auth) },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				decoded, err := DecodeLaunchRequest(packet)
				if err != nil {
					t.Fatalf("decode launch request: %v", err)
				}
				return fmt.Sprintf("release=%x launch=%s secret=%x", decoded.ReleaseToken, decoded.LaunchToken, decoded.Secret)
			},
			want:      fmt.Sprintf("release=%x launch=%s secret=%x", request.ReleaseToken, request.LaunchToken, request.Secret),
			goldenHex: wireTestLaunchRequestGoldenHex,
		},
		{
			name: "launch result", kind: frameLaunchResult, version: wireVersion,
			encode: func() ([]byte, error) { return EncodeLaunchResult(result) },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				decoded, err := DecodeLaunchResult(packet)
				if err != nil {
					t.Fatalf("decode launch result: %v", err)
				}
				return fmt.Sprintf("%+v", decoded)
			},
			want:      fmt.Sprintf("%+v", result),
			goldenHex: wireTestLaunchResultGoldenHex,
		},
		{
			name: "gate ready", kind: frameGateReady, version: wireVersion,
			encode: func() ([]byte, error) { return EncodeGateReady(), nil },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				if err := ValidateGateReady(packet); err != nil {
					t.Fatalf("validate gate ready: %v", err)
				}
				return "validated"
			},
			want:      "validated",
			goldenHex: wireTestGateReadyGoldenHex,
		},
		{
			name: "gate parked", kind: frameGateParked, version: wireVersion,
			encode: func() ([]byte, error) { return EncodeGateParked(), nil },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				if err := ValidateGateParked(packet); err != nil {
					t.Fatalf("validate gate parked: %v", err)
				}
				return "validated"
			},
			want:      "validated",
			goldenHex: wireTestGateParkedGoldenHex,
		},
		{
			name: "launcher ready", kind: frameLauncherReady, version: wireVersion,
			encode: func() ([]byte, error) { return EncodeLauncherReady(wireTestLauncherIdentity()) },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				decoded, err := DecodeLauncherReady(packet)
				if err != nil {
					t.Fatalf("decode launcher ready: %v", err)
				}
				return fmt.Sprintf("%+v", decoded)
			},
			want: fmt.Sprintf("%+v", ready),
		},
		{
			name: "launcher ready failure", kind: frameLauncherReadyFailure, version: wireVersion,
			encode: func() ([]byte, error) { return EncodeLauncherReadyFailure(LauncherReadyFailurePeer), nil },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				stage, err := DecodeLauncherReadyFailure(packet)
				if err != nil {
					t.Fatalf("decode launcher ready failure: %v", err)
				}
				return fmt.Sprintf("%d %s", stage, stage.String())
			},
			want:      fmt.Sprintf("%d %s", LauncherReadyFailurePeer, LauncherReadyFailurePeer.String()),
			goldenHex: wireTestLauncherReadyFailureGoldenHex,
		},
		{
			name: "exec attempt", kind: frameExecAttempt, version: wireVersion,
			encode: func() ([]byte, error) { return EncodeExecAttempt(7, 19), nil },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				if err := validateFixedFrame(packet, frameExecAttempt, execAttemptSize); err != nil {
					t.Fatalf("exec attempt frame is not a fixed exec-attempt frame: %v", err)
				}
				return fmt.Sprintf("device=%d inode=%d",
					binary.LittleEndian.Uint64(packet[12:20]), binary.LittleEndian.Uint64(packet[20:28]))
			},
			want:      "device=7 inode=19",
			goldenHex: wireTestExecAttemptGoldenHex,
		},
		{
			name: "exec failure", kind: frameExecFailure, version: wireVersion,
			encode: func() ([]byte, error) { return EncodeExecFailure(13), nil },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				if err := validateFixedFrame(packet, frameExecFailure, execFailureSize); err != nil {
					t.Fatalf("exec failure frame is not a fixed exec-failure frame: %v", err)
				}
				return fmt.Sprintf("errno=%d", binary.LittleEndian.Uint32(packet[12:16]))
			},
			want:      "errno=13",
			goldenHex: wireTestExecFailureGoldenHex,
		},
		{
			name: "bootstrap exec attempt", kind: frameBootstrapExecAttempt, version: cloneStatusWireVersion,
			encode: func() ([]byte, error) { return EncodeBootstrapExecAttempt(7, 19), nil },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				device, inode, err := DecodeBootstrapExecAttempt(packet)
				if err != nil {
					t.Fatalf("decode bootstrap exec attempt: %v", err)
				}
				return fmt.Sprintf("device=%d inode=%d", device, inode)
			},
			want:      "device=7 inode=19",
			goldenHex: wireTestBootstrapExecAttemptGoldenHex,
		},
		{
			name: "native exec attempt", kind: frameNativeExecAttempt, version: cloneStatusWireVersion,
			encode: func() ([]byte, error) { return EncodeNativeExecAttempt(11, 23), nil },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				device, inode, err := DecodeNativeExecAttempt(packet)
				if err != nil {
					t.Fatalf("decode native exec attempt: %v", err)
				}
				return fmt.Sprintf("device=%d inode=%d", device, inode)
			},
			want:      "device=11 inode=23",
			goldenHex: wireTestNativeExecAttemptGoldenHex,
		},
		{
			name: "checkout ready", kind: frameCheckoutReady, version: cloneStatusWireVersion,
			encode: func() ([]byte, error) { return EncodeCloneCheckoutReady(receipt) },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				decoded, err := DecodeCloneCheckoutReady(packet)
				if err != nil {
					t.Fatalf("decode checkout ready: %v", err)
				}
				return fmt.Sprintf("%+v", decoded)
			},
			want: fmt.Sprintf("%+v", receipt),
		},
		{
			name: "checkout ready acknowledgement", kind: frameCheckoutReadyAck, version: cloneStatusWireVersion,
			encode: func() ([]byte, error) { return EncodeCloneCheckoutReadyAck(receipt) },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				decoded, err := DecodeCloneCheckoutReadyAck(packet)
				if err != nil {
					t.Fatalf("decode checkout ready acknowledgement: %v", err)
				}
				return fmt.Sprintf("%+v", decoded)
			},
			want: fmt.Sprintf("%+v", receipt),
		},
		{
			name: "clone bootstrap failure", kind: frameCloneBootstrapFailure, version: cloneStatusWireVersion,
			encode: func() ([]byte, error) { return EncodeCloneBootstrapFailure(CloneFailureSeed, 5) },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				failure, err := DecodeCloneBootstrapFailure(packet)
				if err != nil {
					t.Fatalf("decode clone bootstrap failure: %v", err)
				}
				return fmt.Sprintf("%+v", *failure)
			},
			want:      fmt.Sprintf("%+v", CloneBootstrapFailureError{Stage: CloneFailureSeed, Errno: 5}),
			goldenHex: wireTestCloneBootstrapFailureGoldenHex,
		},
	}
	for _, stage := range []CloneBootstrapFailureStage{
		CloneFailureGateClose, CloneFailureGateDrop, CloneFailureGateParentDeath, CloneFailureGateFence,
		CloneFailureGateDropGroups, CloneFailureGateDropBoundingSet, CloneFailureGateDropCredentials,
		CloneFailureGateDropCapabilities, CloneFailureGateDropAmbient, CloneFailureGateDropNoNewPrivs,
		CloneFailureGateDropStatus,
	} {
		stage := stage
		cases = append(cases, wireRoundTripCase{
			name: "clone bootstrap failure gate guard " + fmt.Sprintf("%d", stage),
			kind: frameCloneBootstrapFailure, version: cloneStatusWireVersion,
			encode: func() ([]byte, error) { return EncodeCloneBootstrapFailure(stage, 1) },
			decode: func(t *testing.T, packet []byte) string {
				t.Helper()
				failure, err := DecodeCloneBootstrapFailure(packet)
				if err != nil {
					t.Fatalf("decode clone bootstrap failure: %v", err)
				}
				return fmt.Sprintf("%+v", *failure)
			},
			want: fmt.Sprintf("%+v", CloneBootstrapFailureError{Stage: stage, Errno: 1}),
		})
	}
	return cases
}

type wireRefusalCase struct {
	name   string
	mutate func(packet []byte) []byte
	want   string
	// Matches a wrapped error whose inner text is not part of the wire contract.
	wantPrefix bool
}

func wireTestRefusals(t *testing.T, valid []byte, decode func(packet []byte) error, cases []wireRefusalCase) {
	t.Helper()
	if err := decode(valid); err != nil {
		t.Fatalf("valid frame was rejected before its refusal cases: %v", err)
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := decode(testCase.mutate(valid))
			if testCase.wantPrefix {
				if err == nil || !strings.HasPrefix(err.Error(), testCase.want) {
					t.Errorf("%s: got error %v, want prefix %q", testCase.name, err, testCase.want)
				}
				return
			}
			if err == nil || err.Error() != testCase.want {
				t.Errorf("%s: got error %v, want %q", testCase.name, err, testCase.want)
			}
		})
	}
}

func wireTestFixedFrameRefusals(want string) []wireRefusalCase {
	return []wireRefusalCase{
		{name: "truncated", mutate: wireTestTruncateLast(), want: want},
		{name: "oversized", mutate: wireTestAppendZero(), want: want},
		{name: "unknown tag", mutate: wireTestSetUint16(10, wireTestUnknownFrameKind), want: want},
		{name: "bad magic", mutate: wireTestSetByte(0, wireTestCorruptMagicByte), want: want},
		{name: "bad version", mutate: wireTestSetUint16(8, wireTestUnknownVersion), want: want},
	}
}

func wireTestCopy(packet []byte) []byte { return append([]byte(nil), packet...) }

func wireTestSetByte(index int, value byte) func([]byte) []byte {
	return func(packet []byte) []byte {
		mutated := wireTestCopy(packet)
		mutated[index] = value
		return mutated
	}
}

func wireTestSetUint16(index int, value uint16) func([]byte) []byte {
	return func(packet []byte) []byte {
		mutated := wireTestCopy(packet)
		binary.LittleEndian.PutUint16(mutated[index:index+2], value)
		return mutated
	}
}

func wireTestSetUint32(index int, value uint32) func([]byte) []byte {
	return func(packet []byte) []byte {
		mutated := wireTestCopy(packet)
		binary.LittleEndian.PutUint32(mutated[index:index+4], value)
		return mutated
	}
}

func wireTestSetUint64(index int, value uint64) func([]byte) []byte {
	return func(packet []byte) []byte {
		mutated := wireTestCopy(packet)
		binary.LittleEndian.PutUint64(mutated[index:index+8], value)
		return mutated
	}
}

func wireTestZeroRange(start, end int) func([]byte) []byte {
	return func(packet []byte) []byte {
		mutated := wireTestCopy(packet)
		clear(mutated[start:end])
		return mutated
	}
}

func wireTestTruncateLast() func([]byte) []byte {
	return func(packet []byte) []byte {
		return wireTestCopy(packet[:len(packet)-1])
	}
}

func wireTestCopyUpTo(size int) func([]byte) []byte {
	return func(packet []byte) []byte {
		return wireTestCopy(packet[:size])
	}
}

func wireTestAppendZero() func([]byte) []byte {
	return func(packet []byte) []byte {
		return append(wireTestCopy(packet), 0)
	}
}

func wireTestAppendOversizedCloneStatus() func([]byte) []byte {
	return func(packet []byte) []byte {
		return append(wireTestCopy(packet), make([]byte, maxCloneStatusFrameSize)...)
	}
}

func wireTestAppendOversizedLauncherReady() func([]byte) []byte {
	return func(packet []byte) []byte {
		return append(wireTestCopy(packet), make([]byte, maxLauncherReadySize)...)
	}
}

func TestWireLaunchRequestRefusals(t *testing.T) {
	token, auth, request := wireTestLaunchRequestFixture(t)
	packet, err := encodeLaunchRequest(token, auth)
	if err != nil {
		t.Fatalf("encode launch request fixture: %v", err)
	}
	decoded, err := DecodeLaunchRequest(packet)
	if err != nil || fmt.Sprintf("%+v", decoded) != fmt.Sprintf("%+v", request) {
		t.Fatalf("valid launch request fixture does not decode exactly: got %+v err %v", decoded, err)
	}
	cases := wireTestFixedFrameRefusals("Linux owner prepare request magic, version, type, or size is invalid")
	cases = append(cases,
		wireRefusalCase{
			name: "empty release token", mutate: wireTestZeroRange(12, 44),
			want: "Linux owner prepare request carries an empty token or secret",
		},
		wireRefusalCase{
			name: "empty launch token", mutate: wireTestZeroRange(44, 60),
			want: "Linux owner prepare request carries an empty token or secret",
		},
		wireRefusalCase{
			name: "empty secret", mutate: wireTestZeroRange(60, 92),
			want: "Linux owner prepare request carries an empty token or secret",
		},
	)
	wireTestRefusals(t, packet, func(candidate []byte) error {
		_, err := DecodeLaunchRequest(candidate)
		return err
	}, cases)
}

func TestWireLaunchResultRefusals(t *testing.T) {
	packet, err := EncodeLaunchResult(wireTestLaunchResult())
	if err != nil {
		t.Fatalf("encode launch result fixture: %v", err)
	}
	cases := wireTestFixedFrameRefusals("root launcher result magic, version, or type is invalid")
	cases = append(cases,
		wireRefusalCase{
			name: "nonzero reserved field", mutate: wireTestSetUint32(68, 1),
			want: "root launcher result reserved field is nonzero",
		},
		wireRefusalCase{
			name: "status out of range", mutate: wireTestSetUint32(12, LaunchStatusNeverProduced+1),
			want: "root launcher result status or errno is invalid",
		},
		wireRefusalCase{
			name: "errno out of range", mutate: wireTestSetUint32(16, uint32(^uint32(0)>>1)+1),
			want: "root launcher result status or errno is invalid",
		},
		wireRefusalCase{
			name: "ready status without a gate identity", mutate: wireTestZeroRange(52, 56),
			want: "root launcher returned an invalid gate identity",
		},
	)
	wireTestRefusals(t, packet, func(candidate []byte) error {
		_, err := DecodeLaunchResult(candidate)
		return err
	}, cases)
}

func TestWireLauncherReadyRefusals(t *testing.T) {
	identity := wireTestLauncherIdentity()
	packet, err := EncodeLauncherReady(identity)
	if err != nil {
		t.Fatalf("encode launcher ready fixture: %v", err)
	}
	readyCases := []wireRefusalCase{
		{
			name: "truncated", mutate: wireTestTruncateLast(),
			want: "transient launcher readiness identity is invalid or has trailing bytes",
		},
		{
			name: "one trailing byte", mutate: wireTestAppendZero(),
			want: "transient launcher readiness identity is invalid or has trailing bytes",
		},
		{
			name: "oversized beyond the fixed bound", mutate: wireTestAppendOversizedLauncherReady(),
			want: "transient launcher readiness magic, version, type, or size is invalid",
		},
		{
			name: "unknown tag", mutate: wireTestSetUint16(10, wireTestUnknownFrameKind),
			want: "transient launcher readiness magic, version, type, or size is invalid",
		},
		{
			name: "bad magic", mutate: wireTestSetByte(0, wireTestCorruptMagicByte),
			want: "transient launcher readiness magic, version, type, or size is invalid",
		},
		{
			name: "bad version", mutate: wireTestSetUint16(8, wireTestUnknownVersion),
			want: "transient launcher readiness magic, version, type, or size is invalid",
		},
		{
			name: "pid not above one", mutate: wireTestSetUint32(12, 1),
			want: "transient launcher readiness PID is invalid",
		},
		{
			// The dedicated "start time is truncated" guard is unreachable; the size guard refuses this case.
			name: "cut into the start time", mutate: wireTestCopyUpTo(23),
			want: "transient launcher readiness magic, version, type, or size is invalid",
		},
		{
			name: "zero start time", mutate: wireTestSetUint64(16, 0),
			want: "transient launcher readiness identity is invalid or has trailing bytes",
		},
		{
			name: "identity longer than its bound", mutate: func([]byte) []byte {
				oversized := fixedFrame(frameLauncherReady, 12)
				oversized = appendWireUint32(oversized, uint32(identity.PID))
				var startTime [8]byte
				binary.LittleEndian.PutUint64(startTime[:], identity.StartTime)
				oversized = append(oversized, startTime[:]...)
				return appendWireString(oversized, strings.Repeat("x", 257))
			},
			want: "transient launcher readiness identity is invalid or has trailing bytes",
		},
	}
	wireTestRefusals(t, packet, func(candidate []byte) error {
		_, err := DecodeLauncherReady(candidate)
		return err
	}, readyCases)

	failurePacket := EncodeLauncherReadyFailure(LauncherReadyFailurePeer)
	failureCases := wireTestFixedFrameRefusals("transient launcher readiness failure frame is malformed")
	failureCases = append(failureCases,
		wireRefusalCase{
			name: "stage zero", mutate: wireTestSetUint32(12, 0),
			want: "transient launcher readiness failure stage is invalid",
		},
		wireRefusalCase{
			name: "stage beyond the encoding stage", mutate: wireTestSetUint32(12, uint32(LauncherReadyFailureEncoding)+1),
			want: "transient launcher readiness failure stage is invalid",
		},
	)
	wireTestRefusals(t, failurePacket, func(candidate []byte) error {
		_, err := DecodeLauncherReadyFailure(candidate)
		return err
	}, failureCases)
}

func TestWireGateFrameRefusals(t *testing.T) {
	readyPacket := EncodeGateReady()
	wireTestRefusals(t, readyPacket, func(candidate []byte) error {
		return ValidateGateReady(candidate)
	}, wireTestFixedFrameRefusals("Linux owner fixed frame magic, version, type, or size is invalid"))

	parkedPacket := EncodeGateParked()
	wireTestRefusals(t, parkedPacket, func(candidate []byte) error {
		return ValidateGateParked(candidate)
	}, wireTestFixedFrameRefusals("Linux owner fixed frame magic, version, type, or size is invalid"))
}

func TestWireCloneStatusRefusals(t *testing.T) {
	bootstrapPacket := EncodeBootstrapExecAttempt(7, 19)
	bootstrapCases := wireTestFixedFrameRefusals("Linux clone status frame version, type, or size is invalid")
	bootstrapCases = append(bootstrapCases, wireRefusalCase{
		name: "inode zero", mutate: wireTestSetUint64(20, 0),
		want: "Linux clone exec-attempt frame lacks an image inode",
	})
	wireTestRefusals(t, bootstrapPacket, func(candidate []byte) error {
		_, _, err := DecodeBootstrapExecAttempt(candidate)
		return err
	}, bootstrapCases)

	nativePacket := EncodeNativeExecAttempt(11, 23)
	nativeCases := wireTestFixedFrameRefusals("Linux clone status frame version, type, or size is invalid")
	nativeCases = append(nativeCases, wireRefusalCase{
		name: "inode zero", mutate: wireTestSetUint64(20, 0),
		want: "Linux clone exec-attempt frame lacks an image inode",
	})
	wireTestRefusals(t, nativePacket, func(candidate []byte) error {
		_, _, err := DecodeNativeExecAttempt(candidate)
		return err
	}, nativeCases)

	receipt := wireTestCheckoutReceipt()
	readyPacket, err := EncodeCloneCheckoutReady(receipt)
	if err != nil {
		t.Fatalf("encode checkout ready fixture: %v", err)
	}
	ackPacket, err := EncodeCloneCheckoutReadyAck(receipt)
	if err != nil {
		t.Fatalf("encode checkout ready acknowledgement fixture: %v", err)
	}
	for _, receiptCase := range []struct {
		name   string
		kind   uint16
		packet []byte
		decode func(candidate []byte) error
	}{
		{
			name: "checkout ready", kind: frameCheckoutReady, packet: readyPacket,
			decode: func(candidate []byte) error {
				_, err := DecodeCloneCheckoutReady(candidate)
				return err
			},
		},
		{
			name: "checkout ready acknowledgement", kind: frameCheckoutReadyAck, packet: ackPacket,
			decode: func(candidate []byte) error {
				_, err := DecodeCloneCheckoutReadyAck(candidate)
				return err
			},
		},
	} {
		t.Run(receiptCase.name, func(t *testing.T) {
			cases := []wireRefusalCase{
				{
					name: "truncated", mutate: wireTestTruncateLast(),
					want: "Linux clone checkout image identity is truncated or has trailing bytes",
				},
				{
					name: "oversized", mutate: wireTestAppendOversizedCloneStatus(),
					want: "Linux clone checkout frame header or size is invalid",
				},
				{
					name: "unknown tag", mutate: wireTestSetUint16(10, wireTestUnknownFrameKind),
					want: "Linux clone checkout frame header or size is invalid",
				},
				{
					name: "bad magic", mutate: wireTestSetByte(0, wireTestCorruptMagicByte),
					want: "Linux clone checkout frame header or size is invalid",
				},
				{
					name: "bad version", mutate: wireTestSetUint16(8, wireTestUnknownVersion),
					want: "Linux clone checkout frame header or size is invalid",
				},
				{
					name: "generation not positive", mutate: func([]byte) []byte {
						degenerate := receipt
						degenerate.Generation = 0
						return wireTestCloneCheckoutPacket(receiptCase.kind, degenerate)
					},
					want: "Linux clone checkout receipt is invalid",
				},
				{
					name: "malformed seed digest", mutate: func([]byte) []byte {
						degenerate := receipt
						degenerate.SeedDigest = "sha256:" + strings.Repeat("g", 64)
						return wireTestCloneCheckoutPacket(receiptCase.kind, degenerate)
					},
					want: "Linux clone checkout receipt is invalid",
				},
				{
					name: "target inode zero", mutate: func([]byte) []byte {
						degenerate := receipt
						degenerate.TargetInode = 0
						return wireTestCloneCheckoutPacket(receiptCase.kind, degenerate)
					},
					want: "Linux clone checkout receipt is invalid",
				},
				{
					name: "trailing bytes after the image identity", mutate: wireTestAppendZero(),
					want: "Linux clone checkout image identity is truncated or has trailing bytes",
				},
			}
			wireTestRefusals(t, receiptCase.packet, receiptCase.decode, cases)
		})
	}

	failurePacket, err := EncodeCloneBootstrapFailure(CloneFailureSeed, 5)
	if err != nil {
		t.Fatalf("encode clone bootstrap failure fixture: %v", err)
	}
	failureCases := wireTestFixedFrameRefusals("Linux clone bootstrap failure frame is malformed")
	failureCases = append(failureCases,
		wireRefusalCase{
			name: "stage zero", mutate: wireTestSetUint32(12, 0),
			want: "Linux clone bootstrap failure frame code is invalid",
		},
		wireRefusalCase{
			name: "stage beyond the last gate stage", mutate: wireTestSetUint32(12, uint32(CloneFailureGateDropStatus)+1),
			want: "Linux clone bootstrap failure frame code is invalid",
		},
		wireRefusalCase{
			name: "errno zero", mutate: wireTestSetUint32(16, 0),
			want: "Linux clone bootstrap failure frame code is invalid",
		},
		wireRefusalCase{
			name: "errno out of range", mutate: wireTestSetUint32(16, uint32(^uint32(0)>>1)+1),
			want: "Linux clone bootstrap failure frame code is invalid",
		},
	)
	wireTestRefusals(t, failurePacket, func(candidate []byte) error {
		_, err := DecodeCloneBootstrapFailure(candidate)
		return err
	}, failureCases)
}

func wireTestAddFrameSeeds(f *testing.F, packet []byte) {
	f.Helper()
	f.Add(packet)
	f.Add(packet[:len(packet)-1])
	f.Add(append(wireTestCopy(packet), 0))
}

func wireTestFuzzDecode[T any](t *testing.T, candidate []byte, decode func(packet []byte) (T, error)) {
	first, err := decode(candidate)
	if err != nil {
		return
	}
	second, err := decode(candidate)
	if err != nil {
		t.Fatalf("accepted packet was rejected on replay: %v", err)
	}
	if fmt.Sprintf("%+v", first) != fmt.Sprintf("%+v", second) {
		t.Fatalf("packet decode is not order-independent: %+v != %+v", first, second)
	}
}

func wireTestFuzzValidate(t *testing.T, candidate []byte, validate func(packet []byte) error) {
	if err := validate(candidate); err != nil {
		return
	}
	if err := validate(candidate); err != nil {
		t.Fatalf("accepted packet was rejected on replay: %v", err)
	}
}

type wireTestExecProof struct {
	device uint64
	inode  uint64
}

func wireTestFuzzReleaseCommand(raw []byte) NativeCommand {
	fields := bytes.SplitN(raw, []byte{0}, 2*maxCommandVector+2)
	for len(fields) < 2 {
		fields = append(fields, nil)
	}
	command := NativeCommand{Path: string(fields[0]), Dir: string(fields[1])}
	for index, field := range fields[2:] {
		if index%2 == 0 {
			command.Args = append(command.Args, string(field))
		} else {
			command.Env = append(command.Env, string(field))
		}
	}
	return command
}

func FuzzEncodeGuardedRelease(f *testing.F) {
	f.Add([]byte("/bin/true\x00/\x00--scratch\x00LANG=C"), wireTestReleaseGoldenDeadlineNS)
	f.Fuzz(func(t *testing.T, raw []byte, deadlineNS uint64) {
		command := wireTestFuzzReleaseCommand(raw)
		packet, err := encodeGuardedRelease(wireTestToken(), command, deadlineNS)
		if err != nil {
			again, againErr := encodeGuardedRelease(wireTestToken(), command, deadlineNS)
			if againErr == nil || againErr.Error() != err.Error() || again != nil {
				t.Fatalf("encoder refusal is not deterministic: %v then %v", err, againErr)
			}
			return
		}
		again, againErr := encodeGuardedRelease(wireTestToken(), command, deadlineNS)
		if againErr != nil || !bytes.Equal(packet, again) {
			t.Fatalf("encoder is not deterministic: %v then %v", err, againErr)
		}
		if len(packet) < releaseHeaderSize || len(packet) > maxReleaseSize ||
			string(packet[:8]) != launchMagic ||
			binary.LittleEndian.Uint16(packet[8:10]) != releaseWireVersion ||
			binary.LittleEndian.Uint16(packet[10:12]) != frameRelease ||
			binary.LittleEndian.Uint32(packet[44:48]) != uint32(len(packet)-releaseHeaderSize) ||
			binary.LittleEndian.Uint32(packet[48:52]) != releaseFDCount ||
			binary.LittleEndian.Uint64(packet[52:60]) != deadlineNS {
			t.Fatalf("portable RELEASE frame is malformed: len=%d", len(packet))
		}
	})
}

func FuzzDecodeLaunchRequest(f *testing.F) {
	packet, err := encodeLaunchRequest(wireTestToken(), wireTestGateProductionAuth())
	if err != nil {
		f.Fatalf("encode launch request seed: %v", err)
	}
	wireTestAddFrameSeeds(f, packet)
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzDecode(t, candidate, DecodeLaunchRequest)
	})
}

func FuzzDecodeLaunchResult(f *testing.F) {
	packet, err := EncodeLaunchResult(wireTestLaunchResult())
	if err != nil {
		f.Fatalf("encode launch result seed: %v", err)
	}
	wireTestAddFrameSeeds(f, packet)
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzDecode(t, candidate, DecodeLaunchResult)
	})
}

func FuzzDecodeLauncherReady(f *testing.F) {
	packet, err := EncodeLauncherReady(wireTestLauncherIdentity())
	if err != nil {
		f.Fatalf("encode launcher ready seed: %v", err)
	}
	wireTestAddFrameSeeds(f, packet)
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzDecode(t, candidate, DecodeLauncherReady)
	})
}

func FuzzDecodeLauncherReadyFailure(f *testing.F) {
	wireTestAddFrameSeeds(f, EncodeLauncherReadyFailure(LauncherReadyFailurePeer))
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzDecode(t, candidate, DecodeLauncherReadyFailure)
	})
}

func FuzzValidateGateReady(f *testing.F) {
	wireTestAddFrameSeeds(f, EncodeGateReady())
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzValidate(t, candidate, ValidateGateReady)
	})
}

func FuzzValidateGateParked(f *testing.F) {
	wireTestAddFrameSeeds(f, EncodeGateParked())
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzValidate(t, candidate, ValidateGateParked)
	})
}

func FuzzDecodeBootstrapExecAttempt(f *testing.F) {
	wireTestAddFrameSeeds(f, EncodeBootstrapExecAttempt(7, 19))
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzDecode(t, candidate, func(packet []byte) (wireTestExecProof, error) {
			device, inode, err := DecodeBootstrapExecAttempt(packet)
			return wireTestExecProof{device: device, inode: inode}, err
		})
	})
}

func FuzzDecodeNativeExecAttempt(f *testing.F) {
	wireTestAddFrameSeeds(f, EncodeNativeExecAttempt(11, 23))
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzDecode(t, candidate, func(packet []byte) (wireTestExecProof, error) {
			device, inode, err := DecodeNativeExecAttempt(packet)
			return wireTestExecProof{device: device, inode: inode}, err
		})
	})
}

func FuzzDecodeCloneCheckoutReady(f *testing.F) {
	packet, err := EncodeCloneCheckoutReady(wireTestCheckoutReceipt())
	if err != nil {
		f.Fatalf("encode checkout ready seed: %v", err)
	}
	wireTestAddFrameSeeds(f, packet)
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzDecode(t, candidate, DecodeCloneCheckoutReady)
	})
}

func FuzzDecodeCloneCheckoutReadyAck(f *testing.F) {
	packet, err := EncodeCloneCheckoutReadyAck(wireTestCheckoutReceipt())
	if err != nil {
		f.Fatalf("encode checkout ready acknowledgement seed: %v", err)
	}
	wireTestAddFrameSeeds(f, packet)
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzDecode(t, candidate, DecodeCloneCheckoutReadyAck)
	})
}

func FuzzDecodeCloneBootstrapFailure(f *testing.F) {
	packet, err := EncodeCloneBootstrapFailure(CloneFailureSeed, 5)
	if err != nil {
		f.Fatalf("encode clone bootstrap failure seed: %v", err)
	}
	wireTestAddFrameSeeds(f, packet)
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzDecode(t, candidate, DecodeCloneBootstrapFailure)
	})
}

func TestCloneBootstrapFailureStageNumbersArePinned(t *testing.T) {
	for _, testCase := range []struct {
		stage CloneBootstrapFailureStage
		want  uint32
	}{
		{CloneFailureRequest, 1},
		{CloneFailureSeed, 2},
		{CloneFailureTarget, 3},
		{CloneFailureGit, 4},
		{CloneFailureCheckout, 5},
		{CloneFailureNativeImage, 6},
		{CloneFailureExec, 7},
		{CloneFailureGateClose, 8},
		{CloneFailureGateDrop, 9},
		{CloneFailureGateParentDeath, 10},
		{CloneFailureGateFence, 11},
		{CloneFailureGateDropGroups, 12},
		{CloneFailureGateDropBoundingSet, 13},
		{CloneFailureGateDropCredentials, 14},
		{CloneFailureGateDropCapabilities, 15},
		{CloneFailureGateDropAmbient, 16},
		{CloneFailureGateDropNoNewPrivs, 17},
		{CloneFailureGateDropStatus, 18},
	} {
		if uint32(testCase.stage) != testCase.want {
			t.Fatalf("clone bootstrap failure stage %d carries %d; the C gate and the Go decoder must agree",
				testCase.want, uint32(testCase.stage))
		}
	}
}
