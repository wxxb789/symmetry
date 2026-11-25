//go:build linux && amd64

package protocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

const (
	wireTestGateMapGoldenHex   = "53594d4c4130303101000400e9030000ea030000d1070000d20700000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	wireTestReleaseV3GoldenHex = "53594d4c41303031030006000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20480300000400000015cd5b0700000000090000002f62696e2f7472756501000000090000002f62696e2f7472756501000000060000004c414e473d43420000002f746d702f73796d6d657472792d776972652d676f6c64656e2f62696e64696e672d62696e64696e672d312f72756e2d72756e2d312f67656e65726174696f6e2d31d20200007b22696e74656e74223a7b2276657273696f6e223a322c2272756e5f6964223a2272756e2d31222c2267656e65726174696f6e223a312c2262696e64696e675f6b6579223a2262696e64696e672d31222c227265736f757263655f6964223a2230303030303030302d303030302d343030302d383030302d303030303030303030303031222c22626173655f636f6d6d6974223a2261616161616161616161616161616161616161616161616161616161616161616161616161616161222c22626173655f747265655f646967657374223a227368613235363a62626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262222c226e6f6e6365223a2263636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363222c22736565645f70617468223a222f746d702f73796d6d657472792d776972652d676f6c64656e2d736565642f62696e64696e672d62696e64696e672d312f72756e2d72756e2d312f67656e65726174696f6e2d312e62756e646c65222c22746172676574223a222f746d702f73796d6d657472792d776972652d676f6c64656e2f62696e64696e672d62696e64696e672d312f72756e2d72756e2d312f67656e65726174696f6e2d31227d2c2273656564223a7b2276657273696f6e223a322c22626173655f636f6d6d6974223a2261616161616161616161616161616161616161616161616161616161616161616161616161616161222c22646967657374223a227368613235363a64646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464222c2273697a65223a312c22646576696365223a312c22696e6f6465223a327d7d"
)

func wireTestOwnerConfig() OwnerConfig {
	return OwnerConfig{AUID: 1001, AGID: 1002, BUID: 2001, BGID: 2002}
}

func TestWireLinuxGateMapRoundTrip(t *testing.T) {
	config := wireTestOwnerConfig()
	token := wireTestToken()
	packet := EncodeGateMap(config, token)
	if len(packet) != gateMapSize || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != wireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != frameGateMap {
		t.Fatalf("gate map frame header is invalid: %x", packet)
	}
	if got := hex.EncodeToString(packet); got != wireTestGateMapGoldenHex {
		t.Errorf("gate map golden bytes = %s, want %s", got, wireTestGateMapGoldenHex)
	}
	decoded, decodedToken, err := DecodeGateMap(packet)
	if err != nil {
		t.Fatalf("decode gate map: %v", err)
	}
	if fmt.Sprintf("%+v", decoded) != fmt.Sprintf("%+v", config) || decodedToken != token {
		t.Fatalf("decoded gate map = %+v token=%x, want %+v token=%x", decoded, decodedToken, config, token)
	}
}

func TestWireLinuxGateMapRefusals(t *testing.T) {
	config := wireTestOwnerConfig()
	packet := EncodeGateMap(config, wireTestToken())
	cases := wireTestFixedFrameRefusals("Linux owner fixed frame magic, version, type, or size is invalid")
	cases = append(cases,
		wireRefusalCase{
			name: "A and B user IDs collapse", mutate: wireTestSetUint32(12, config.BUID),
			want: "Linux owner A and fixed native B must be distinct non-root UIDs",
		},
		wireRefusalCase{
			name: "root user ID", mutate: wireTestSetUint32(12, 0),
			want: "Linux owner A and fixed native B must be distinct non-root UIDs",
		},
		wireRefusalCase{
			name: "A and B group IDs collapse", mutate: wireTestSetUint32(16, config.BGID),
			want: "Linux owner A and fixed native B must be distinct non-root primary GIDs",
		},
		wireRefusalCase{
			name: "root group ID", mutate: wireTestSetUint32(16, 0),
			want: "Linux owner A and fixed native B must be distinct non-root primary GIDs",
		},
		wireRefusalCase{
			name: "B group ID collapsed onto the A group", mutate: wireTestSetUint32(24, config.AGID),
			want: "Linux owner A and fixed native B must be distinct non-root primary GIDs",
		},
	)
	wireTestRefusals(t, packet, func(candidate []byte) error {
		_, _, err := DecodeGateMap(candidate)
		return err
	}, cases)
}

func TestWireLinuxReleaseScratchRoundTrip(t *testing.T) {
	pipes := newScratchNativePipes(t)
	guard, _ := newScratchReleaseGuard(t, time.Minute)
	token := wireTestToken()
	command := NativeCommand{
		Path: "/bin/true", Args: []string{"/bin/true", "--scratch"}, Env: []string{"LANG=C"}, Dir: "/",
		Stdin: pipes.stdinRead, Stdout: pipes.stdoutWrite, Stderr: pipes.stderrWrite,
	}
	packet, err := EncodeRelease(token, command, guard)
	if err != nil {
		t.Fatalf("encode guarded scratch RELEASE: %v", err)
	}
	if version := binary.LittleEndian.Uint16(packet[8:10]); version != releaseWireVersion {
		t.Fatalf("scratch RELEASE version = %d, want %d", version, releaseWireVersion)
	}
	descriptors := [releaseFDCount]*os.File{pipes.stdinRead, pipes.stdoutWrite, pipes.stderrWrite, guard.OwnerPipe}
	decoded, deadline, err := DecodeRelease(packet, token, descriptors)
	if err != nil {
		t.Fatalf("decode guarded scratch RELEASE: %v", err)
	}
	if headerDeadline := binary.LittleEndian.Uint64(packet[52:60]); deadline != headerDeadline || deadline == 0 {
		t.Fatalf("scratch RELEASE deadline = %d, want the nonzero header field %d", deadline, headerDeadline)
	}
	if decoded.Path != command.Path || decoded.Dir != command.Dir ||
		strings.Join(decoded.Args, "\x00") != strings.Join(command.Args, "\x00") ||
		strings.Join(decoded.Env, "\x00") != strings.Join(command.Env, "\x00") ||
		decoded.Stdin != pipes.stdinRead || decoded.Stdout != pipes.stdoutWrite || decoded.Stderr != pipes.stderrWrite ||
		decoded.Clone != nil || decoded.Reuse != nil {
		t.Fatalf("decoded scratch RELEASE differs from its command: %+v", decoded)
	}
}

func TestWireLinuxReleaseCloneRoundTrip(t *testing.T) {
	pipes := newScratchNativePipes(t)
	guard, _ := newScratchReleaseGuard(t, time.Minute)
	token := wireTestToken()
	intent := cleanupTestIntentWithBinding(t, t.TempDir(), "run-1", "binding-1", 1)
	grant := cleanupTestGrant(intent)
	command := NativeCommand{
		Path: "/bin/true", Args: []string{"/bin/true"}, Env: []string{},
		Dir: grant.Intent.Target, Stdin: pipes.stdinRead, Stdout: pipes.stdoutWrite, Stderr: pipes.stderrWrite,
		Clone: &grant,
	}
	packet, err := EncodeRelease(token, command, guard)
	if err != nil {
		t.Fatalf("encode guarded clone RELEASE: %v", err)
	}
	if version := binary.LittleEndian.Uint16(packet[8:10]); version != releaseCloneWireVersion {
		t.Fatalf("clone RELEASE version = %d, want %d", version, releaseCloneWireVersion)
	}
	descriptors := [releaseFDCount]*os.File{pipes.stdinRead, pipes.stdoutWrite, pipes.stderrWrite, guard.OwnerPipe}
	decoded, deadline, err := DecodeRelease(packet, token, descriptors)
	if err != nil {
		t.Fatalf("decode guarded clone RELEASE: %v", err)
	}
	if headerDeadline := binary.LittleEndian.Uint64(packet[52:60]); deadline != headerDeadline || deadline == 0 {
		t.Fatalf("clone RELEASE deadline = %d, want the nonzero header field %d", deadline, headerDeadline)
	}
	if decoded.Clone == nil || !decoded.Clone.Equal(grant) || decoded.Dir != grant.Intent.Target ||
		decoded.Path != command.Path || strings.Join(decoded.Args, "\x00") != strings.Join(command.Args, "\x00") {
		t.Fatalf("decoded clone RELEASE differs from its command: %+v", decoded)
	}

	// The C gate zeroes the one-use token before it seals the request memfd
	// (gate.c exec_as_b), so the bootstrap decodes the scrubbed frame.
	sealed := wireTestCopy(packet)
	clear(sealed[12:44])
	bootstrap, bootstrapDeadline, err := DecodeCloneBootstrapRequest(sealed)
	if err != nil {
		t.Fatalf("decode sealed clone bootstrap request: %v", err)
	}
	if bootstrapDeadline != deadline {
		t.Fatalf("sealed clone bootstrap deadline = %d, want the release deadline %d", bootstrapDeadline, deadline)
	}
	if bootstrap.Clone == nil || !bootstrap.Clone.Equal(grant) || bootstrap.Dir != grant.Intent.Target ||
		bootstrap.Path != command.Path || strings.Join(bootstrap.Args, "\x00") != strings.Join(command.Args, "\x00") ||
		len(bootstrap.Env) != 0 || bootstrap.Reuse != nil ||
		bootstrap.Stdin != nil || bootstrap.Stdout != nil || bootstrap.Stderr != nil {
		t.Fatalf("sealed clone bootstrap request differs from its release: %+v", bootstrap)
	}
}

func TestWireLinuxReleaseGoldenBytes(t *testing.T) {
	token := wireTestToken()
	root := filepath.Join(string(filepath.Separator), "tmp", "symmetry-wire-golden")
	intent := cleanupTestIntentWithBinding(t, root, "run-1", "binding-1", 1)
	grant := cleanupTestGrant(intent)
	scratch := NativeCommand{Path: "/bin/true", Args: []string{"/bin/true"}, Env: []string{"LANG=C"}, Dir: "/"}
	clone := scratch
	clone.Dir = grant.Intent.Target
	clone.Clone = &grant

	packetV2, err := encodeGuardedRelease(token, scratch, wireTestReleaseGoldenDeadlineNS)
	if err != nil {
		t.Fatalf("encode guarded scratch RELEASE bytes: %v", err)
	}
	if version := binary.LittleEndian.Uint16(packetV2[8:10]); version != releaseWireVersion ||
		binary.LittleEndian.Uint64(packetV2[52:60]) != wireTestReleaseGoldenDeadlineNS {
		t.Fatalf("scratch RELEASE header = %x", packetV2[:releaseHeaderSize])
	}
	if got := hex.EncodeToString(packetV2); got != wireTestReleaseV2GoldenHex {
		t.Errorf("scratch RELEASE golden bytes = %s, want %s", got, wireTestReleaseV2GoldenHex)
	}

	packetV3, err := encodeGuardedRelease(token, clone, wireTestReleaseGoldenDeadlineNS)
	if err != nil {
		t.Fatalf("encode guarded clone RELEASE bytes: %v", err)
	}
	if version := binary.LittleEndian.Uint16(packetV3[8:10]); version != releaseCloneWireVersion ||
		binary.LittleEndian.Uint64(packetV3[52:60]) != wireTestReleaseGoldenDeadlineNS {
		t.Fatalf("clone RELEASE header = %x", packetV3[:releaseHeaderSize])
	}
	if got := hex.EncodeToString(packetV3); got != wireTestReleaseV3GoldenHex {
		t.Errorf("clone RELEASE golden bytes = %s, want %s", got, wireTestReleaseV3GoldenHex)
	}
}

func wireTestReleaseArgcOffset(command NativeCommand) int {
	return releaseHeaderSize + 4 + len(command.Path)
}

func wireTestReleaseEnvCountOffset(command NativeCommand) int {
	offset := wireTestReleaseArgcOffset(command) + 4
	for _, arg := range command.Args {
		offset += 4 + len(arg)
	}
	return offset
}

func wireTestAppendAndFixBodySize(extra ...byte) func([]byte) []byte {
	return func(packet []byte) []byte {
		mutated := append(wireTestCopy(packet), extra...)
		binary.LittleEndian.PutUint32(mutated[44:48], uint32(len(mutated)-releaseHeaderSize))
		return mutated
	}
}

func wireTestXorByte(index int, mask byte) func([]byte) []byte {
	return func(packet []byte) []byte {
		mutated := wireTestCopy(packet)
		mutated[index] ^= mask
		return mutated
	}
}

func wireTestAppendOversizedRelease() func([]byte) []byte {
	return func(packet []byte) []byte {
		return append(wireTestCopy(packet), make([]byte, maxReleaseSize)...)
	}
}

func TestWireLinuxReleaseRefusals(t *testing.T) {
	pipes := newScratchNativePipes(t)
	guard, _ := newScratchReleaseGuard(t, time.Minute)
	descriptors := [releaseFDCount]*os.File{pipes.stdinRead, pipes.stdoutWrite, pipes.stderrWrite, guard.OwnerPipe}
	token := wireTestToken()
	command := NativeCommand{
		Path: "/bin/true", Args: []string{"/bin/true"}, Env: []string{"LANG=C"}, Dir: "/",
		Stdin: pipes.stdinRead, Stdout: pipes.stdoutWrite, Stderr: pipes.stderrWrite,
	}
	packet, err := encodeGuardedRelease(token, command, wireTestReleaseGoldenDeadlineNS)
	if err != nil {
		t.Fatalf("encode scratch RELEASE refusal fixture: %v", err)
	}
	cases := []wireRefusalCase{
		{
			name: "truncated", mutate: wireTestTruncateLast(),
			want: "Linux owner RELEASE body size is invalid",
		},
		{
			name: "one appended byte without a body size update", mutate: wireTestAppendZero(),
			want: "Linux owner RELEASE body size is invalid",
		},
		{
			name: "oversized beyond the release bound", mutate: wireTestAppendOversizedRelease(),
			want: "Linux owner RELEASE v2/v3 header version or type is invalid",
		},
		{
			name: "unknown tag", mutate: wireTestSetUint16(10, wireTestUnknownFrameKind),
			want: "Linux owner RELEASE v2/v3 header version or type is invalid",
		},
		{
			name: "bad magic", mutate: wireTestSetByte(0, wireTestCorruptMagicByte),
			want: "Linux owner RELEASE v2/v3 header version or type is invalid",
		},
		{
			name: "bad version", mutate: wireTestSetUint16(8, wireTestUnknownVersion),
			want: "Linux owner RELEASE v2/v3 header version or type is invalid",
		},
		{
			name: "legacy v1 version", mutate: wireTestSetUint16(8, wireVersion),
			want: "Linux owner RELEASE v2/v3 header version or type is invalid",
		},
		{
			name: "stale token", mutate: wireTestXorByte(12, 0xff),
			want: "Linux owner RELEASE token is stale",
		},
		{
			name: "wrong descriptor count", mutate: wireTestSetUint32(48, releaseStdioFDCount),
			want: "Linux owner RELEASE requires exactly four descriptors",
		},
		{
			name: "empty deadline", mutate: wireTestSetUint64(52, 0),
			want: "Linux owner RELEASE monotonic deadline is empty",
		},
		{
			name: "body size mismatch", mutate: wireTestSetUint32(44, uint32(len(packet)-releaseHeaderSize)+1),
			want: "Linux owner RELEASE body size is invalid",
		},
		{
			name: "trailing binary data", mutate: wireTestAppendAndFixBodySize(0),
			want: "Linux owner RELEASE has trailing binary data",
		},
		{
			name: "argv count zero", mutate: wireTestSetUint32(wireTestReleaseArgcOffset(command), 0),
			want: "Linux owner RELEASE argv count is invalid",
		},
		{
			name:   "argv count beyond the vector bound",
			mutate: wireTestSetUint32(wireTestReleaseArgcOffset(command), maxCommandVector+1),
			want:   "Linux owner RELEASE argv count is invalid",
		},
		{
			name:   "environment count beyond the vector bound",
			mutate: wireTestSetUint32(wireTestReleaseEnvCountOffset(command), maxCommandVector+1),
			want:   "Linux owner RELEASE environment count is invalid",
		},
	}
	wireTestRefusals(t, packet, func(candidate []byte) error {
		_, _, err := DecodeRelease(candidate, token, descriptors)
		return err
	}, cases)
}

func TestWireLinuxCloneBootstrapRefusals(t *testing.T) {
	token := wireTestToken()
	intent := cleanupTestIntentWithBinding(t, t.TempDir(), "run-1", "binding-1", 1)
	grant := cleanupTestGrant(intent)
	command := NativeCommand{
		Path: "/bin/true", Args: []string{"/bin/true"}, Env: []string{"LANG=C"},
		Dir: grant.Intent.Target, Clone: &grant,
	}
	packet, err := encodeGuardedRelease(token, command, wireTestReleaseGoldenDeadlineNS)
	if err != nil {
		t.Fatalf("encode clone RELEASE refusal fixture: %v", err)
	}
	sealed := wireTestCopy(packet)
	clear(sealed[12:44])
	scratchToken := wireTestToken()
	scratchCommand := NativeCommand{
		Path: "/bin/true", Args: []string{"/bin/true"}, Env: []string{"LANG=C"}, Dir: "/",
	}
	scratchPacket, err := encodeGuardedRelease(scratchToken, scratchCommand, wireTestReleaseGoldenDeadlineNS)
	if err != nil {
		t.Fatalf("encode v2 scratch RELEASE fixture: %v", err)
	}
	clear(scratchPacket[12:44])

	grantJSON, err := json.Marshal(&grant)
	if err != nil {
		t.Fatalf("marshal clone grant fixture: %v", err)
	}
	cloneSizeOffset := len(sealed) - 4 - len(grantJSON)
	if got := binary.LittleEndian.Uint32(sealed[cloneSizeOffset : cloneSizeOffset+4]); got != uint32(len(grantJSON)) {
		t.Fatalf("sealed clone grant size field = %d, want %d", got, len(grantJSON))
	}
	cases := []wireRefusalCase{
		{
			// The C gate only seals token-scrubbed requests; an unsealed frame must stay
			// refused by the token guard.
			name: "unsealed token", mutate: func([]byte) []byte { return wireTestCopy(packet) },
			want: "Linux owner RELEASE token is stale",
		},
		{
			name: "v2 packet", mutate: func([]byte) []byte { return wireTestCopy(scratchPacket) },
			want: "clone bootstrap requires RELEASE v3",
		},
		{
			name: "truncated", mutate: wireTestTruncateLast(),
			want: "Linux owner RELEASE body size is invalid",
		},
		{
			name: "one appended byte without a body size update", mutate: wireTestAppendZero(),
			want: "Linux owner RELEASE body size is invalid",
		},
		{
			name: "oversized beyond the release bound", mutate: wireTestAppendOversizedRelease(),
			want: "Linux owner RELEASE v2/v3 header version or type is invalid",
		},
		{
			name: "unknown tag", mutate: wireTestSetUint16(10, wireTestUnknownFrameKind),
			want: "Linux owner RELEASE v2/v3 header version or type is invalid",
		},
		{
			name: "bad magic", mutate: wireTestSetByte(0, wireTestCorruptMagicByte),
			want: "Linux owner RELEASE v2/v3 header version or type is invalid",
		},
		{
			name: "bad version", mutate: wireTestSetUint16(8, wireTestUnknownVersion),
			want: "Linux owner RELEASE v2/v3 header version or type is invalid",
		},
		{
			name: "grant payload size zero", mutate: wireTestSetUint32(cloneSizeOffset, 0),
			want: "Linux clone RELEASE grant payload is empty or truncated",
		},
		{
			name:   "grant payload size beyond the body",
			mutate: wireTestSetUint32(cloneSizeOffset, uint32(len(grantJSON)+1)),
			want:   "Linux clone RELEASE grant payload is empty or truncated",
		},
		{
			name: "grant json invalid", mutate: wireTestSetByte(cloneSizeOffset+4, ' '),
			want: "decode Linux clone RELEASE grant: ", wantPrefix: true,
		},
		{
			name: "grant json trailing data", mutate: func(packet []byte) []byte {
				mutated := append(wireTestCopy(packet), '{', '}')
				binary.LittleEndian.PutUint32(mutated[44:48], uint32(len(mutated)-releaseHeaderSize))
				binary.LittleEndian.PutUint32(mutated[cloneSizeOffset:cloneSizeOffset+4], uint32(len(grantJSON)+2))
				return mutated
			},
			want: "Linux clone RELEASE grant has trailing JSON data",
		},
		{
			name: "trailing binary data after the grant", mutate: wireTestAppendAndFixBodySize(0),
			want: "Linux owner RELEASE has trailing binary data",
		},
	}
	wireTestRefusals(t, sealed, func(candidate []byte) error {
		_, _, err := DecodeCloneBootstrapRequest(candidate)
		return err
	}, cases)
}

func wireTestFuzzReleaseDescriptors(f *testing.F) [releaseFDCount]*os.File {
	f.Helper()
	var descriptors [releaseFDCount]*os.File
	for index := range releaseStdioFDCount {
		read, write, err := os.Pipe()
		if err != nil {
			f.Fatalf("create fuzz stdio pipe %d: %v", index, err)
		}
		// stdin is the read end; stdout/stderr are write ends.
		retained := read
		if index != 0 {
			retained = write
		}
		descriptors[index] = retained
		f.Cleanup(func() {
			_ = read.Close()
			_ = write.Close()
		})
	}
	ownerRead, ownerWrite, err := os.Pipe()
	if err != nil {
		f.Fatalf("create fuzz owner pipe: %v", err)
	}
	descriptors[3] = ownerRead
	f.Cleanup(func() {
		_ = ownerRead.Close()
		_ = ownerWrite.Close()
	})
	return descriptors
}

func wireTestFuzzCloneGrant(f *testing.F) authority.LinuxCloneGrant {
	f.Helper()
	root := f.TempDir()
	const bindingKey, runID = "binding-1", "run-1"
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: runID, Generation: 1,
		BindingKey: bindingKey, ResourceID: "00000000-0000-4000-8000-000000000001",
		BaseCommit: strings.Repeat("a", 40), BaseTreeDigest: "sha256:" + strings.Repeat("b", 64),
		Nonce:    strings.Repeat("c", authority.LinuxCloneNonceBytes*2),
		SeedPath: filepath.Join(root+"-seed", "binding-"+bindingKey, "run-"+runID, "generation-1.bundle"),
		Target:   filepath.Join(root, "binding-"+bindingKey, "run-"+runID, "generation-1"),
	}
	if err := intent.Validate(); err != nil {
		f.Fatalf("structural clone fuzz intent is invalid: %v", err)
	}
	return cleanupTestGrant(intent)
}

type wireTestGateMap struct {
	config OwnerConfig
	token  [32]byte
}

func FuzzDecodeGateMap(f *testing.F) {
	wireTestAddFrameSeeds(f, EncodeGateMap(wireTestOwnerConfig(), wireTestToken()))
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzDecode(t, candidate, func(packet []byte) (wireTestGateMap, error) {
			config, token, err := DecodeGateMap(packet)
			return wireTestGateMap{config: config, token: token}, err
		})
	})
}

// Re-encodes so replay comparisons never depend on pointer identity: fmt prints
// the nested Clone pointer as an address, which differs between two decodes.
func wireTestReleaseCanonical(t *testing.T, token [32]byte, command NativeCommand, deadline uint64) string {
	t.Helper()
	packet, err := encodeGuardedRelease(token, command, deadline)
	if err != nil {
		t.Fatalf("re-encode accepted RELEASE: %v", err)
	}
	return hex.EncodeToString(packet)
}

func wireTestFuzzReleaseReplay(t *testing.T, candidate []byte, label string, token [32]byte,
	decode func(packet []byte) (NativeCommand, uint64, error)) {
	t.Helper()
	first, firstDeadline, err := decode(candidate)
	if err != nil {
		return
	}
	second, secondDeadline, err := decode(candidate)
	if err != nil {
		t.Fatalf("accepted %s was rejected on replay: %v", label, err)
	}
	if firstDeadline != secondDeadline ||
		wireTestReleaseCanonical(t, token, first, firstDeadline) != wireTestReleaseCanonical(t, token, second, secondDeadline) {
		t.Fatalf("%s decode is not order-independent: %d != %d", label, firstDeadline, secondDeadline)
	}
}

func FuzzDecodeRelease(f *testing.F) {
	descriptors := wireTestFuzzReleaseDescriptors(f)
	token := wireTestToken()
	scratch := NativeCommand{Path: "/bin/true", Args: []string{"/bin/true"}, Env: []string{"LANG=C"}, Dir: "/"}
	packetV2, err := encodeGuardedRelease(token, scratch, wireTestReleaseGoldenDeadlineNS)
	if err != nil {
		f.Fatalf("encode scratch RELEASE fuzz seed: %v", err)
	}
	grant := wireTestFuzzCloneGrant(f)
	clone := scratch
	clone.Dir = grant.Intent.Target
	clone.Clone = &grant
	packetV3, err := encodeGuardedRelease(token, clone, wireTestReleaseGoldenDeadlineNS)
	if err != nil {
		f.Fatalf("encode clone RELEASE fuzz seed: %v", err)
	}
	wireTestAddFrameSeeds(f, packetV2)
	wireTestAddFrameSeeds(f, packetV3)
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzReleaseReplay(t, candidate, "RELEASE", token, func(packet []byte) (NativeCommand, uint64, error) {
			return DecodeRelease(packet, token, descriptors)
		})
	})
}

func FuzzDecodeCloneBootstrapRequest(f *testing.F) {
	grant := wireTestFuzzCloneGrant(f)
	command := NativeCommand{
		Path: "/bin/true", Args: []string{"/bin/true"}, Env: []string{"LANG=C"}, Dir: grant.Intent.Target, Clone: &grant,
	}
	// The sealed request carries the gate's token-scrubbed v3 frame.
	sealed, err := encodeGuardedRelease([32]byte{}, command, wireTestReleaseGoldenDeadlineNS)
	if err != nil {
		f.Fatalf("encode sealed clone bootstrap fuzz seed: %v", err)
	}
	scratch, err := encodeGuardedRelease([32]byte{}, NativeCommand{
		Path: "/bin/true", Args: []string{"/bin/true"}, Env: []string{"LANG=C"}, Dir: "/",
	}, wireTestReleaseGoldenDeadlineNS)
	if err != nil {
		f.Fatalf("encode scratch v2 bootstrap fuzz seed: %v", err)
	}
	wireTestAddFrameSeeds(f, sealed)
	wireTestAddFrameSeeds(f, scratch)
	f.Fuzz(func(t *testing.T, candidate []byte) {
		wireTestFuzzReleaseReplay(t, candidate, "clone bootstrap request", [32]byte{}, func(packet []byte) (NativeCommand, uint64, error) {
			return DecodeCloneBootstrapRequest(packet)
		})
	})
}

type cloneStatusReadProbe struct {
	name string
	// prepare returns the descriptor the arm read must consume.
	prepare      func(t *testing.T, peer, local *os.File) int
	arm          func(ctx context.Context, fd int) error
	wantClass    string
	wantErrno    int
	wantText     string
	mustNotClass string
}

func runCloneStatusReadProbes(t *testing.T, probes []cloneStatusReadProbe) {
	t.Helper()
	for _, test := range probes {
		t.Run(test.name, func(t *testing.T) {
			local, peer := wireTestCloneStatusPair(t)
			defer local.Close()
			defer peer.Close()
			fd := test.prepare(t, peer, local)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			runCloneStatusReadProbe(t, test, test.arm(ctx, fd))
		})
	}
}

func runCloneStatusReadProbe(t *testing.T, test cloneStatusReadProbe, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("clone-status read succeeded, want the %s probe to fail", test.name)
	}
	var readFailure *CloneStatusReadError
	if !errors.As(err, &readFailure) {
		t.Fatalf("clone-status read failure is untyped: %v", err)
	}
	if test.wantClass == "" && test.mustNotClass == "" {
		t.Fatalf("probe %q pins neither an exact class nor a neighbour refusal", test.name)
	}
	if (test.wantClass != "" && readFailure.Class != test.wantClass) || readFailure.Errno != test.wantErrno {
		t.Fatalf("clone-status read class = %q errno=%d, want class=%q errno=%d",
			readFailure.Class, readFailure.Errno, test.wantClass, test.wantErrno)
	}
	if err.Error() != test.wantText {
		t.Fatalf("clone-status read text = %q, want the unchanged %q", err.Error(), test.wantText)
	}
	if test.mustNotClass != "" && readFailure.Class == test.mustNotClass {
		t.Fatalf("clone-status read collapsed onto the neighbouring %s producer", test.mustNotClass)
	}
}

func wireTestCloneStatusPair(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("create clone-status seqpacket pair: %v", err)
	}
	return os.NewFile(uintptr(pair[0]), "clone-status-local"), os.NewFile(uintptr(pair[1]), "clone-status-peer")
}

func wireTestCloneStatusSend(t *testing.T, local, peer *os.File, packet []byte) int {
	t.Helper()
	if _, err := unix.SendmsgN(int(peer.Fd()), packet, nil, nil, 0); err != nil {
		t.Fatalf("send clone-status probe frame: %v", err)
	}
	return int(local.Fd())
}

func wireTestCloneStatusAttempt(ctx context.Context, fd int) error {
	_, _, err := readCloneBootstrapExecAttempt(ctx, fd, time.Now().Add(5*time.Second))
	return err
}

// wireTestCloneReadGateV1Failure is the gate's 16-byte v1 FRAME_EXEC_FAILURE
// (WIRE_MAGIC, version 1, kind 8, errno 13); the test pins the literal bytes.
func wireTestCloneReadGateV1Failure(t *testing.T) []byte {
	t.Helper()
	frame, err := hex.DecodeString("53594d4c41303031010008000d000000")
	if err != nil || len(frame) != execFailureSize {
		t.Fatalf("decode the golden v1 gate failure frame: %v", err)
	}
	return frame
}

// TestCloneStatusReadClassNamesEveryProducer drives each closed clone-read
// producer through a real seqpacket arm and pins its class, errno and text.
func TestCloneStatusReadClassNamesEveryProducer(t *testing.T) {
	attempt := wireTestCloneStatusAttempt
	receipt := func(ctx context.Context, fd int) error {
		_, err := readCloneCheckoutReady(ctx, fd, time.Now().Add(5*time.Second))
		return err
	}
	proof := func(ctx context.Context, fd int) error {
		_, err := readCloneNativeExecResult(ctx, fd, time.Now().Add(5*time.Second))
		return err
	}
	quiet := func(t *testing.T, peer, local *os.File) int { return int(local.Fd()) }
	probes := []cloneStatusReadProbe{
		{
			name: "canceled wait",
			arm: func(ctx context.Context, fd int) error {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				return attempt(canceled, fd)
			},
			prepare: quiet, wantClass: CloneReadClassWait, wantText: context.Canceled.Error(),
		},
		{
			name: "expired deadline",
			arm: func(ctx context.Context, fd int) error {
				_, _, err := readCloneBootstrapExecAttempt(ctx, fd, time.Now().Add(-time.Second))
				return err
			},
			prepare: quiet, wantClass: CloneReadClassWait, wantText: context.DeadlineExceeded.Error(),
		},
		{
			name: "receive failure",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatalf("create non-socket probe descriptor: %v", err)
				}
				t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
				if _, err := writer.Write([]byte{'x'}); err != nil {
					t.Fatalf("prime non-socket probe descriptor: %v", err)
				}
				return int(reader.Fd())
			},
			arm: attempt, wantClass: CloneReadClassRecv, wantText: "socket operation on non-socket",
		},
		{
			name: "ancillary descriptors",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				extra, err := os.Open(os.DevNull)
				if err != nil {
					t.Fatalf("open ancillary probe descriptor: %v", err)
				}
				t.Cleanup(func() { _ = extra.Close() })
				if _, err := unix.SendmsgN(int(peer.Fd()), EncodeBootstrapExecAttempt(7, 19),
					unix.UnixRights(int(extra.Fd())), nil, 0); err != nil {
					t.Fatalf("send clone-status frame with rights: %v", err)
				}
				return int(local.Fd())
			},
			arm: attempt, wantClass: CloneReadClassAncillary, wantText: "clone status channel carried unexpected descriptors",
		},
		{
			name: "truncated frame",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				oversized := make([]byte, maxCloneStatusFrameSize+1)
				copy(oversized, cloneStatusFrame(frameBootstrapExecAttempt, len(oversized)))
				return wireTestCloneStatusSend(t, local, peer, oversized)
			},
			arm: attempt, wantClass: CloneReadClassTruncated, wantText: "clone status frame is truncated or oversized",
		},
		{
			// Linux AF_UNIX recvmsg never reports MSG_EOR, so a zero-length
			// record is indistinguishable from a peer close (probe below).
			name: "zero-length record inside the peer close class",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				if _, err := unix.SendmsgN(int(peer.Fd()), []byte{}, nil, nil, 0); err != nil {
					t.Fatalf("send zero-length clone-status packet: %v", err)
				}
				return int(local.Fd())
			},
			arm: attempt, wantClass: CloneReadClassPeerClosed, wantText: io.EOF.Error(),
		},
		{
			name: "bad magic",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				mutated := EncodeBootstrapExecAttempt(7, 19)
				mutated[0] = 'X'
				return wireTestCloneStatusSend(t, local, peer, mutated)
			},
			arm: attempt, wantClass: CloneReadClassMagicOrVersion, wantText: "clone status frame magic or version is invalid",
		},
		{
			name: "unrecognized version",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				mutated := EncodeBootstrapExecAttempt(7, 19)
				binary.LittleEndian.PutUint16(mutated[8:10], releaseWireVersion)
				return wireTestCloneStatusSend(t, local, peer, mutated)
			},
			arm: attempt, wantClass: CloneReadClassMagicOrVersion, wantText: "clone status frame magic or version is invalid",
		},
		{
			name: "v1 gate failure frame",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				frame := wireTestCloneReadGateV1Failure(t)
				if !bytes.Equal(frame, EncodeExecFailure(13)) {
					t.Fatalf("golden v1 gate failure frame differs from EncodeExecFailure(13)")
				}
				return wireTestCloneStatusSend(t, local, peer, frame)
			},
			arm: attempt, wantClass: CloneReadClassGateV1Frame, wantErrno: 13,
			wantText: "clone status frame magic or version is invalid",
		},
		{
			name: "v1 gate attempt frame",
			prepare: func(t *testing.T, peer, local *os.File) int {
				return wireTestCloneStatusSend(t, local, peer, EncodeExecAttempt(7, 19))
			},
			arm: attempt, wantClass: CloneReadClassGateV1Frame, wantText: "clone status frame magic or version is invalid",
		},
		{
			name: "v1 gate failure frame with errno beyond the signed range",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				frame := EncodeExecFailure(13)
				binary.LittleEndian.PutUint32(frame[12:16], ^uint32(0))
				return wireTestCloneStatusSend(t, local, peer, frame)
			},
			arm: attempt, wantClass: CloneReadClassGateV1Frame,
			wantText: "clone status frame magic or version is invalid",
		},
		{
			name: "peer closed before the attempt",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				if err := peer.Close(); err != nil {
					t.Fatalf("close the clone-status peer: %v", err)
				}
				return int(local.Fd())
			},
			arm: attempt, wantClass: CloneReadClassPeerClosed, wantText: io.EOF.Error(),
		},
		{
			name: "peer closed before the receipt",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				if err := peer.Close(); err != nil {
					t.Fatalf("close the clone-status peer: %v", err)
				}
				return int(local.Fd())
			},
			arm: receipt, wantClass: CloneReadClassPeerClosed, wantText: io.EOF.Error(),
		},
		{
			name: "peer closed before the native proof",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				if err := peer.Close(); err != nil {
					t.Fatalf("close the clone-status peer: %v", err)
				}
				return int(local.Fd())
			},
			arm: proof, wantClass: CloneReadClassPeerClosed, wantText: io.EOF.Error(),
		},
		{
			name: "wrong frame kind",
			prepare: func(t *testing.T, peer, local *os.File) int {
				return wireTestCloneStatusSend(t, local, peer, EncodeNativeExecAttempt(11, 23))
			},
			arm: attempt, wantClass: CloneReadClassFrameHeader,
			wantText: "Linux clone status frame version, type, or size is invalid",
		},
		{
			name: "wrong frame size",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				return wireTestCloneStatusSend(t, local, peer, cloneStatusFrame(frameBootstrapExecAttempt, 12))
			},
			arm: attempt, wantClass: CloneReadClassFrameHeader,
			wantText: "Linux clone status frame version, type, or size is invalid",
		},
		{
			name: "attempt frame without the image inode",
			prepare: func(t *testing.T, peer, local *os.File) int {
				return wireTestCloneStatusSend(t, local, peer, EncodeBootstrapExecAttempt(7, 0))
			},
			arm: attempt, wantClass: CloneReadClassImageInode,
			wantText: "Linux clone exec-attempt frame lacks an image inode",
		},
		{
			name: "malformed guest failure frame",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				return wireTestCloneStatusSend(t, local, peer, cloneStatusFrame(frameCloneBootstrapFailure, 16))
			},
			arm: attempt, wantClass: CloneReadClassGuestMalformed,
			wantText: "Linux clone bootstrap failure frame is malformed",
		},
		{
			name: "invalid guest failure code",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				return wireTestCloneStatusSend(t, local, peer, cloneStatusFrame(frameCloneBootstrapFailure, cloneFailureSize))
			},
			arm: attempt, wantClass: CloneReadClassGuestCode,
			wantText: "Linux clone bootstrap failure frame code is invalid",
		},
		{
			name: "checkout receipt with the attempt frame",
			prepare: func(t *testing.T, peer, local *os.File) int {
				return wireTestCloneStatusSend(t, local, peer, EncodeBootstrapExecAttempt(7, 19))
			},
			arm: receipt, wantClass: CloneReadClassFrameHeader,
			wantText: "Linux clone checkout frame header or size is invalid",
		},
		{
			name: "checkout receipt with an oversized string field",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				mutated := cloneStatusFrame(frameCheckoutReady, 96)
				binary.LittleEndian.PutUint32(mutated[16:20], ^uint32(0))
				return wireTestCloneStatusSend(t, local, peer, mutated)
			},
			arm: receipt, wantClass: CloneReadClassFrameHeader,
			wantText: "length-prefixed command string is oversized or truncated",
		},
	}
	runCloneStatusReadProbes(t, probes)
}

// TestCloneStatusReadClassSplitsNeighbouringProducers pins that each producer
// never collapses onto the neighbouring class of the same read family.
func TestCloneStatusReadClassSplitsNeighbouringProducers(t *testing.T) {
	attempt := wireTestCloneStatusAttempt
	receipt := func(ctx context.Context, fd int) error {
		_, err := readCloneCheckoutReady(ctx, fd, time.Now().Add(5*time.Second))
		return err
	}
	probes := []cloneStatusReadProbe{
		{
			name: "v1 gate frame is not an unrecognized version",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				return wireTestCloneStatusSend(t, local, peer, wireTestCloneReadGateV1Failure(t))
			},
			arm: attempt, wantErrno: 13,
			wantText: "clone status frame magic or version is invalid", mustNotClass: CloneReadClassMagicOrVersion,
		},
		{
			name: "unrecognized version is not a v1 gate frame",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				mutated := EncodeBootstrapExecAttempt(7, 19)
				binary.LittleEndian.PutUint16(mutated[8:10], releaseWireVersion)
				return wireTestCloneStatusSend(t, local, peer, mutated)
			},
			arm:      attempt,
			wantText: "clone status frame magic or version is invalid", mustNotClass: CloneReadClassGateV1Frame,
		},
		{
			name: "wrong frame kind is not a missing inode",
			prepare: func(t *testing.T, peer, local *os.File) int {
				return wireTestCloneStatusSend(t, local, peer, EncodeNativeExecAttempt(11, 23))
			},
			arm:      attempt,
			wantText: "Linux clone status frame version, type, or size is invalid", mustNotClass: CloneReadClassImageInode,
		},
		{
			name: "missing inode is not a frame header mismatch",
			prepare: func(t *testing.T, peer, local *os.File) int {
				return wireTestCloneStatusSend(t, local, peer, EncodeBootstrapExecAttempt(7, 0))
			},
			arm:      attempt,
			wantText: "Linux clone exec-attempt frame lacks an image inode", mustNotClass: CloneReadClassFrameHeader,
		},
		{
			name: "malformed guest failure is not an invalid guest code",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				return wireTestCloneStatusSend(t, local, peer, cloneStatusFrame(frameCloneBootstrapFailure, 16))
			},
			arm:      attempt,
			wantText: "Linux clone bootstrap failure frame is malformed", mustNotClass: CloneReadClassGuestCode,
		},
		{
			name: "invalid guest code is not a malformed guest failure",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				return wireTestCloneStatusSend(t, local, peer, cloneStatusFrame(frameCloneBootstrapFailure, cloneFailureSize))
			},
			arm:      attempt,
			wantText: "Linux clone bootstrap failure frame code is invalid", mustNotClass: CloneReadClassGuestMalformed,
		},
		{
			name: "truncation is not a header mismatch",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				oversized := make([]byte, maxCloneStatusFrameSize+1)
				copy(oversized, cloneStatusFrame(frameBootstrapExecAttempt, len(oversized)))
				return wireTestCloneStatusSend(t, local, peer, oversized)
			},
			arm:      attempt,
			wantText: "clone status frame is truncated or oversized", mustNotClass: CloneReadClassMagicOrVersion,
		},
		{
			// The MSG_EOR branch is unreachable from a Linux AF_UNIX send; the
			// input it would classify renders the peer-close neighbour instead.
			name: "zero-length record never reaches the empty-packet branch",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				if _, err := unix.SendmsgN(int(peer.Fd()), []byte{}, nil, nil, 0); err != nil {
					t.Fatalf("send zero-length clone-status packet: %v", err)
				}
				return int(local.Fd())
			},
			arm:      attempt,
			wantText: io.EOF.Error(), mustNotClass: CloneReadClassEmptyPacket,
		},
		{
			name: "peer close is not a version mismatch",
			prepare: func(t *testing.T, peer, local *os.File) int {
				t.Helper()
				if err := peer.Close(); err != nil {
					t.Fatalf("close the clone-status peer: %v", err)
				}
				return int(local.Fd())
			},
			arm: attempt, wantText: io.EOF.Error(),
			mustNotClass: CloneReadClassMagicOrVersion,
		},
		{
			name: "checkout receipt wrong kind is not a version mismatch",
			prepare: func(t *testing.T, peer, local *os.File) int {
				return wireTestCloneStatusSend(t, local, peer, EncodeBootstrapExecAttempt(7, 19))
			},
			arm: receipt, wantText: "Linux clone checkout frame header or size is invalid",
			mustNotClass: CloneReadClassMagicOrVersion,
		},
	}
	runCloneStatusReadProbes(t, probes)
}

// TestCloneStatusReadClassVocabularyIsPinned pins every closed code by its
// literal, so a swapped or renamed constant (mutant M2) reddens here.
func TestCloneStatusReadClassVocabularyIsPinned(t *testing.T) {
	literals := []struct {
		literal string
		code    string
	}{
		{"wait_failed", CloneReadClassWait},
		{"recv_failed", CloneReadClassRecv},
		{"ancillary_data", CloneReadClassAncillary},
		{"truncated_frame", CloneReadClassTruncated},
		{"empty_packet", CloneReadClassEmptyPacket},
		{"magic_or_version", CloneReadClassMagicOrVersion},
		{"gate_v1_frame", CloneReadClassGateV1Frame},
		{"peer_closed_without_frame", CloneReadClassPeerClosed},
		{"frame_header", CloneReadClassFrameHeader},
		{"image_inode_missing", CloneReadClassImageInode},
		{"guest_failure_malformed", CloneReadClassGuestMalformed},
		{"guest_failure_code", CloneReadClassGuestCode},
	}
	seen := make(map[string]string, len(literals))
	for _, row := range literals {
		if row.code != row.literal {
			t.Fatalf("read class constant named %q carries %q", row.literal, row.code)
		}
		if other, duplicate := seen[row.code]; duplicate {
			t.Fatalf("read classes %q and %q share the literal %q", other, row.literal, row.code)
		}
		seen[row.code] = row.literal
	}
}

// TestCloneStatusReadClassAbsentOnSuccess pins that a completed arm read keeps
// no read sentinel and that the peer close stays a wait-class peer close.
func TestCloneStatusReadClassAbsentOnSuccess(t *testing.T) {
	local, peer := wireTestCloneStatusPair(t)
	defer local.Close()
	defer peer.Close()
	wireTestCloneStatusSend(t, local, peer, EncodeBootstrapExecAttempt(7, 19))
	device, inode, err := readCloneBootstrapExecAttempt(context.Background(), int(local.Fd()), time.Now().Add(5*time.Second))
	if err != nil || device != 7 || inode != 19 {
		t.Fatalf("successful clone-status read = device=%d inode=%d err=%v, want 7/19 and no failure", device, inode, err)
	}
	receipt := wireTestCheckoutReceipt()
	receiptPacket, err := EncodeCloneCheckoutReady(receipt)
	if err != nil {
		t.Fatalf("encode checkout receipt probe frame: %v", err)
	}
	wireTestCloneStatusSend(t, local, peer, receiptPacket)
	decoded, err := readCloneCheckoutReady(context.Background(), int(local.Fd()), time.Now().Add(5*time.Second))
	if err != nil || decoded != receipt {
		t.Fatalf("successful checkout receipt read = %+v err=%v, want the encoded receipt and no failure", decoded, err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("close the clone-status peer: %v", err)
	}
	_, _, err = readCloneBootstrapExecAttempt(context.Background(), int(local.Fd()), time.Now().Add(5*time.Second))
	if err == nil || !errors.Is(err, io.EOF) || ReleaseWaitErrorClass(err) != ReleaseWaitClassPeerClosed {
		t.Fatalf("peer close lost its wait class: %v (class %q)", err, ReleaseWaitErrorClass(err))
	}
}
