package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

const (
	launchMagic                = "SYMLA001"
	wireVersion                = uint16(1)
	frameLaunchRequest         = uint16(1)
	frameLaunchResult          = uint16(2)
	frameGateReady             = uint16(3)
	frameGateMap               = uint16(4)
	frameGateParked            = uint16(5)
	frameRelease               = uint16(6)
	frameExecAttempt           = uint16(7)
	frameExecFailure           = uint16(8)
	frameLauncherReady         = uint16(9)
	frameLauncherReadyFailure  = uint16(10)
	frameBootstrapExecAttempt  = uint16(20)
	frameCheckoutReady         = uint16(21)
	frameCheckoutReadyAck      = uint16(22)
	frameNativeExecAttempt     = uint16(23)
	frameCloneBootstrapFailure = uint16(24)
	releaseWireVersion         = uint16(2)
	releaseCloneWireVersion    = uint16(3)
	cloneStatusWireVersion     = uint16(3)
	launchRequestSize          = 92
	launchResultSize           = 128
	gateReadySize              = 16
	gateMapSize                = 60
	gateParkedSize             = 16
	maxLauncherReadySize       = 512
	launcherReadyFailureSize   = 16
	cloneExecAttemptSize       = 28
	cloneFailureSize           = 20
	maxCloneStatusFrameSize    = 64 << 10
	execAttemptSize            = 28
	execFailureSize            = 16
	releaseHeaderSize          = 60
	releaseStdioFDCount        = 3
	releaseFDCount             = 4
	maxReleaseSize             = 128 << 10
	maxCommandString           = 16 << 10
	maxCommandVector           = 128
)

// Non-secret namespace UID/GID 1 map to BUID/BGID; host A and host root are
// deliberately absent from both maps, so an empty StateDir means legacy v1 config.
type OwnerConfig struct {
	AUID     uint32
	AGID     uint32
	BUID     uint32
	BGID     uint32
	StateDir string
}

type ProcessIdentity struct {
	PID          int
	StartTime    uint64
	BootID       string
	UserNSDevice uint64
	UserNSInode  uint64
	HostUID      uint32
	HostGID      uint32
	OwnerPID     int
}

// The caller's hook must persist this identity or fail: a hook error closes the
// private channel and prevents gate creation.
type LauncherIdentity struct {
	PID       int
	StartTime uint64
	BootID    string
	Identity  string
}

// Emitted only after the root core verified its sanitized environment, empty
// supplementary groups, inherited FDs and A channel; identity = command.Start PID.
type LauncherReady struct {
	PID       int
	StartTime uint64
	Identity  string
}

type LauncherReadyFailureStage uint32

const (
	LauncherReadyFailureEnvironment LauncherReadyFailureStage = iota + 1
	LauncherReadyFailureGroups
	LauncherReadyFailureParentFence
	LauncherReadyFailureDescriptors
	LauncherReadyFailurePeer
	LauncherReadyFailureLeaf
	LauncherReadyFailureIdentity
	LauncherReadyFailureEncoding
)

func (stage LauncherReadyFailureStage) String() string {
	switch stage {
	case LauncherReadyFailureEnvironment:
		return "root-environment"
	case LauncherReadyFailureGroups:
		return "supplementary-groups"
	case LauncherReadyFailureParentFence:
		return "parent-death-fence"
	case LauncherReadyFailureDescriptors:
		return "inherited-descriptors"
	case LauncherReadyFailurePeer:
		return "socket-peers"
	case LauncherReadyFailureLeaf:
		return "leaf-identity"
	case LauncherReadyFailureIdentity:
		return "launcher-identity"
	case LauncherReadyFailureEncoding:
		return "ready-encoding"
	default:
		return "unknown"
	}
}

// Safe to surface to A: only a fixed stage code, never B-controlled data or paths.
type LauncherReadyFailureError struct {
	Stage LauncherReadyFailureStage
}

func (failure *LauncherReadyFailureError) Error() string {
	if failure == nil {
		return "transient root launcher failed before READY"
	}
	return "transient root launcher failed READY stage " + failure.Stage.String()
}

// Sent only over the authenticated inherited socket, never via argv or environment.
type GateProductionAuth struct {
	LaunchToken string
	Secret      string
}

// Secret is raw entropy and must be cleared after result verification.
type LaunchRequest struct {
	ReleaseToken [32]byte
	LaunchToken  string
	Secret       [32]byte
}

const LaunchRequestSize = launchRequestSize

const (
	// The A gate is mapped, parked, and pidfd-transferred.
	LaunchStatusReady uint32 = iota
	// Setup failed and the launcher proved its gate child exited.
	LaunchStatusRejected
	// The root launcher could not prove that the gate child exited.
	LaunchStatusUnresolved
	// Signed by root only for failures before the core calls Cmd.Start.
	LaunchStatusNeverProduced
)

type LaunchResult struct {
	Status        uint32
	Errno         uint32
	Token         [32]byte
	Identity      ProcessIdentity
	Authenticator [32]byte
}

// Path is opened symlink-free and must be a direct root/A-owned image; the three
// pipe ends are borrowed and the runner retires its child-side copies after Release.
type NativeCommand struct {
	Path   string
	Args   []string
	Env    []string
	Dir    string
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File
	Clone  *authority.LinuxCloneGrant
	Reuse  *authority.LinuxCloneReuseGrant
}

// Free-form Git output never crosses the status pipe.
type CloneBootstrapFailureStage uint32

const (
	CloneFailureRequest CloneBootstrapFailureStage = iota + 1
	CloneFailureSeed
	CloneFailureTarget
	CloneFailureGit
	CloneFailureCheckout
	CloneFailureNativeImage
	CloneFailureExec
	// Gate-side pre-clone guard stages: the gate names which guard failed in
	// the same closed numeric vocabulary its clone release binds.
	CloneFailureGateClose
	CloneFailureGateDrop
	CloneFailureGateParentDeath
	CloneFailureGateFence
	// A failed B credential drop names the first unsatisfied credential fact in
	// the drop's own order; the drop path itself is unchanged.
	CloneFailureGateDropGroups
	CloneFailureGateDropBoundingSet
	CloneFailureGateDropCredentials
	CloneFailureGateDropCapabilities
	CloneFailureGateDropAmbient
	CloneFailureGateDropNoNewPrivs
	CloneFailureGateDropStatus
)

type CloneBootstrapFailureError struct {
	Stage CloneBootstrapFailureStage
	Errno uint32
}

func (failure *CloneBootstrapFailureError) Error() string {
	if failure == nil {
		return "Linux B clone bootstrap failed"
	}
	return fmt.Sprintf("Linux B clone bootstrap failed at stage %d (errno=%d)", failure.Stage, failure.Errno)
}

func encodeLaunchRequest(token [32]byte, auth GateProductionAuth) ([]byte, error) {
	launchToken, err := hex.DecodeString(auth.LaunchToken)
	if err != nil || len(launchToken) != authority.TokenBytes || hex.EncodeToString(launchToken) != auth.LaunchToken {
		return nil, errors.New("Linux prepare request launch token is invalid")
	}
	secret, err := hex.DecodeString(auth.Secret)
	if err != nil || len(secret) != authority.SecretBytes || hex.EncodeToString(secret) != auth.Secret {
		clear(launchToken)
		return nil, errors.New("Linux prepare request authority secret is invalid")
	}
	packet := make([]byte, launchRequestSize)
	copy(packet[:8], launchMagic)
	binary.LittleEndian.PutUint16(packet[8:10], wireVersion)
	binary.LittleEndian.PutUint16(packet[10:12], frameLaunchRequest)
	copy(packet[12:44], token[:])
	copy(packet[44:60], launchToken)
	copy(packet[60:92], secret)
	clear(launchToken)
	clear(secret)
	return packet, nil
}

func EncodeLauncherReady(identity LauncherIdentity) ([]byte, error) {
	if identity.PID <= 1 || identity.PID > int(^uint32(0)>>1) || identity.StartTime == 0 ||
		identity.Identity == "" || len(identity.Identity) > 256 || strings.ContainsRune(identity.Identity, '\x00') {
		return nil, errors.New("transient launcher readiness identity is invalid")
	}
	packet := fixedFrame(frameLauncherReady, 12)
	packet = appendWireUint32(packet, uint32(identity.PID))
	var startTime [8]byte
	binary.LittleEndian.PutUint64(startTime[:], identity.StartTime)
	packet = append(packet, startTime[:]...)
	packet = appendWireString(packet, identity.Identity)
	if len(packet) > maxLauncherReadySize {
		clear(packet)
		return nil, errors.New("transient launcher readiness frame exceeds its fixed bound")
	}
	return packet, nil
}

func DecodeLauncherReady(packet []byte) (LauncherReady, error) {
	var ready LauncherReady
	if len(packet) < 29 || len(packet) > maxLauncherReadySize || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != wireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != frameLauncherReady {
		return ready, errors.New("transient launcher readiness magic, version, type, or size is invalid")
	}
	position := 12
	pid, err := readWireUint32(packet, &position)
	if err != nil || pid <= 1 {
		return LauncherReady{}, errors.New("transient launcher readiness PID is invalid")
	}
	if len(packet)-position < 8 {
		return LauncherReady{}, errors.New("transient launcher readiness start time is truncated")
	}
	ready.PID = int(pid)
	ready.StartTime = binary.LittleEndian.Uint64(packet[position : position+8])
	position += 8
	ready.Identity, err = readWireString(packet, &position)
	if err != nil || ready.StartTime == 0 || ready.Identity == "" || len(ready.Identity) > 256 || position != len(packet) {
		return LauncherReady{}, errors.New("transient launcher readiness identity is invalid or has trailing bytes")
	}
	return ready, nil
}

func EncodeLauncherReadyFailure(stage LauncherReadyFailureStage) []byte {
	packet := fixedFrame(frameLauncherReadyFailure, launcherReadyFailureSize)
	binary.LittleEndian.PutUint32(packet[12:16], uint32(stage))
	return packet
}

func DecodeLauncherReadyFailure(packet []byte) (LauncherReadyFailureStage, error) {
	if validateFixedFrame(packet, frameLauncherReadyFailure, launcherReadyFailureSize) != nil {
		return 0, errors.New("transient launcher readiness failure frame is malformed")
	}
	stage := LauncherReadyFailureStage(binary.LittleEndian.Uint32(packet[12:16]))
	if stage < LauncherReadyFailureEnvironment || stage > LauncherReadyFailureEncoding {
		return 0, errors.New("transient launcher readiness failure stage is invalid")
	}
	return stage, nil
}

func DecodeLaunchResult(packet []byte) (LaunchResult, error) {
	if len(packet) != launchResultSize || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != wireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != frameLaunchResult {
		return LaunchResult{}, errors.New("root launcher result magic, version, or type is invalid")
	}
	var result LaunchResult
	copy(result.Token[:], packet[20:52])
	if binary.LittleEndian.Uint32(packet[68:72]) != 0 {
		return LaunchResult{}, errors.New("root launcher result reserved field is nonzero")
	}
	result.Status = binary.LittleEndian.Uint32(packet[12:16])
	result.Errno = binary.LittleEndian.Uint32(packet[16:20])
	result.Identity = ProcessIdentity{
		PID:          int(binary.LittleEndian.Uint32(packet[52:56])),
		HostUID:      binary.LittleEndian.Uint32(packet[56:60]),
		HostGID:      binary.LittleEndian.Uint32(packet[60:64]),
		OwnerPID:     int(binary.LittleEndian.Uint32(packet[64:68])),
		StartTime:    binary.LittleEndian.Uint64(packet[72:80]),
		UserNSDevice: binary.LittleEndian.Uint64(packet[80:88]),
		UserNSInode:  binary.LittleEndian.Uint64(packet[88:96]),
	}
	copy(result.Authenticator[:], packet[96:128])
	if result.Status > LaunchStatusNeverProduced || result.Errno > uint32(^uint32(0)>>1) {
		return LaunchResult{}, errors.New("root launcher result status or errno is invalid")
	}
	if result.Status == LaunchStatusReady && (result.Identity.PID <= 1 || result.Identity.StartTime == 0 ||
		result.Identity.OwnerPID <= 1 || result.Identity.UserNSInode == 0) {
		return LaunchResult{}, errors.New("root launcher returned an invalid gate identity")
	}
	return result, nil
}

func EncodeLaunchResult(result LaunchResult) ([]byte, error) {
	if result.Status > LaunchStatusNeverProduced || result.Errno > uint32(^uint32(0)>>1) {
		return nil, errors.New("Linux owner launcher result status or errno is invalid")
	}
	packet := fixedFrame(frameLaunchResult, launchResultSize)
	binary.LittleEndian.PutUint32(packet[12:16], result.Status)
	binary.LittleEndian.PutUint32(packet[16:20], result.Errno)
	copy(packet[20:52], result.Token[:])
	binary.LittleEndian.PutUint32(packet[52:56], uint32(result.Identity.PID))
	binary.LittleEndian.PutUint32(packet[56:60], result.Identity.HostUID)
	binary.LittleEndian.PutUint32(packet[60:64], result.Identity.HostGID)
	binary.LittleEndian.PutUint32(packet[64:68], uint32(result.Identity.OwnerPID))
	binary.LittleEndian.PutUint64(packet[72:80], result.Identity.StartTime)
	binary.LittleEndian.PutUint64(packet[80:88], result.Identity.UserNSDevice)
	binary.LittleEndian.PutUint64(packet[88:96], result.Identity.UserNSInode)
	copy(packet[96:128], result.Authenticator[:])
	return packet, nil
}

func DecodeLaunchRequest(packet []byte) (LaunchRequest, error) {
	var request LaunchRequest
	if len(packet) != launchRequestSize || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != wireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != frameLaunchRequest {
		return request, errors.New("Linux owner prepare request magic, version, type, or size is invalid")
	}
	copy(request.ReleaseToken[:], packet[12:44])
	request.LaunchToken = hex.EncodeToString(packet[44:60])
	copy(request.Secret[:], packet[60:92])
	if request.ReleaseToken == [32]byte{} || request.LaunchToken == strings.Repeat("0", authority.TokenBytes*2) || request.Secret == [32]byte{} {
		clear(request.Secret[:])
		return LaunchRequest{}, errors.New("Linux owner prepare request carries an empty token or secret")
	}
	return request, nil
}

func EncodeGateReady() []byte { return fixedFrame(frameGateReady, gateReadySize) }

func ValidateGateReady(packet []byte) error {
	return validateFixedFrame(packet, frameGateReady, gateReadySize)
}

func EncodeGateParked() []byte { return fixedFrame(frameGateParked, gateParkedSize) }

func ValidateGateParked(packet []byte) error {
	return validateFixedFrame(packet, frameGateParked, gateParkedSize)
}

func validateFixedFrame(packet []byte, kind uint16, size int) error {
	if len(packet) != size || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != wireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != kind {
		return errors.New("Linux owner fixed frame magic, version, type, or size is invalid")
	}
	return nil
}

func fixedFrame(kind uint16, size int) []byte {
	packet := make([]byte, size)
	copy(packet[:8], launchMagic)
	binary.LittleEndian.PutUint16(packet[8:10], wireVersion)
	binary.LittleEndian.PutUint16(packet[10:12], kind)
	return packet
}

func appendWireUint32(destination []byte, value uint32) []byte {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	return append(destination, encoded[:]...)
}

func appendWireUint64(destination []byte, value uint64) []byte {
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], value)
	return append(destination, encoded[:]...)
}

func appendWireString(destination []byte, value string) []byte {
	destination = appendWireUint32(destination, uint32(len(value)))
	return append(destination, value...)
}

func readWireUint32(body []byte, position *int) (uint32, error) {
	if position == nil || *position < 0 || len(body)-*position < 4 {
		return 0, errors.New("length-prefixed command body ended before uint32")
	}
	value := binary.LittleEndian.Uint32(body[*position : *position+4])
	*position += 4
	return value, nil
}

func readWireUint64(body []byte, position *int) (uint64, error) {
	if position == nil || *position < 0 || len(body)-*position < 8 {
		return 0, errors.New("length-prefixed command body ended before uint64")
	}
	value := binary.LittleEndian.Uint64(body[*position : *position+8])
	*position += 8
	return value, nil
}

func readWireString(body []byte, position *int) (string, error) {
	length, err := readWireUint32(body, position)
	if err != nil {
		return "", err
	}
	if length > maxCommandString || uint64(length) > uint64(len(body)-*position) {
		return "", errors.New("length-prefixed command string is oversized or truncated")
	}
	start := *position
	*position += int(length)
	value := body[start:*position]
	if bytes.IndexByte(value, 0) >= 0 {
		return "", errors.New("length-prefixed command string contains NUL")
	}
	return string(value), nil
}

// Written immediately before same-PID execveat; a successful exec closes this
// status descriptor because the gate marks it CLOEXEC.
func EncodeExecAttempt(device, inode uint64) []byte {
	packet := fixedFrame(frameExecAttempt, execAttemptSize)
	binary.LittleEndian.PutUint64(packet[12:20], device)
	binary.LittleEndian.PutUint64(packet[20:28], inode)
	return packet
}

func EncodeExecFailure(errno uint32) []byte {
	packet := fixedFrame(frameExecFailure, execFailureSize)
	binary.LittleEndian.PutUint32(packet[12:16], errno)
	return packet
}

func cloneStatusFrame(kind uint16, size int) []byte {
	packet := fixedFrame(kind, size)
	binary.LittleEndian.PutUint16(packet[8:10], cloneStatusWireVersion)
	return packet
}

// CloneStatusReadError names the exact closed producer of a failed clone-status
// read; the message stays verbatim, so every existing rendered line is unchanged.
type CloneStatusReadError struct {
	Class string
	Errno int
	Err   error
}

// Closed read_class vocabulary: one code per clone-status read producer.
const (
	CloneReadClassWait           = "wait_failed"
	CloneReadClassRecv           = "recv_failed"
	CloneReadClassAncillary      = "ancillary_data"
	CloneReadClassTruncated      = "truncated_frame"
	CloneReadClassEmptyPacket    = "empty_packet"
	CloneReadClassMagicOrVersion = "magic_or_version"
	CloneReadClassGateV1Frame    = "gate_v1_frame"
	CloneReadClassPeerClosed     = "peer_closed_without_frame"
	CloneReadClassFrameHeader    = "frame_header"
	CloneReadClassImageInode     = "image_inode_missing"
	CloneReadClassGuestMalformed = "guest_failure_malformed"
	CloneReadClassGuestCode      = "guest_failure_code"
)

func (failure *CloneStatusReadError) Error() string {
	if failure == nil || failure.Err == nil {
		return "clone status read failed"
	}
	return failure.Err.Error()
}

func (failure *CloneStatusReadError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}

func cloneStatusReadFailure(class string, err error) error {
	return &CloneStatusReadError{Class: class, Err: err}
}

func validateCloneStatusFrame(packet []byte, kind uint16, size int) error {
	if len(packet) != size || string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != cloneStatusWireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != kind {
		return cloneStatusReadFailure(CloneReadClassFrameHeader,
			errors.New("Linux clone status frame version, type, or size is invalid"))
	}
	return nil
}

func encodeCloneExecAttempt(kind uint16, device, inode uint64) []byte {
	packet := cloneStatusFrame(kind, cloneExecAttemptSize)
	binary.LittleEndian.PutUint64(packet[12:20], device)
	binary.LittleEndian.PutUint64(packet[20:28], inode)
	return packet
}

func decodeCloneExecAttempt(packet []byte, kind uint16) (uint64, uint64, error) {
	if err := validateCloneStatusFrame(packet, kind, cloneExecAttemptSize); err != nil {
		return 0, 0, err
	}
	device := binary.LittleEndian.Uint64(packet[12:20])
	inode := binary.LittleEndian.Uint64(packet[20:28])
	if inode == 0 {
		return 0, 0, cloneStatusReadFailure(CloneReadClassImageInode,
			errors.New("Linux clone exec-attempt frame lacks an image inode"))
	}
	return device, inode, nil
}

func EncodeBootstrapExecAttempt(device, inode uint64) []byte {
	return encodeCloneExecAttempt(frameBootstrapExecAttempt, device, inode)
}

func DecodeBootstrapExecAttempt(packet []byte) (uint64, uint64, error) {
	return decodeCloneExecAttempt(packet, frameBootstrapExecAttempt)
}

func EncodeNativeExecAttempt(device, inode uint64) []byte {
	return encodeCloneExecAttempt(frameNativeExecAttempt, device, inode)
}

func DecodeNativeExecAttempt(packet []byte) (uint64, uint64, error) {
	return decodeCloneExecAttempt(packet, frameNativeExecAttempt)
}

func encodeCloneCheckoutReceipt(kind uint16, receipt authority.LinuxCloneCheckoutReceipt) ([]byte, error) {
	if err := receipt.Validate(); err != nil {
		return nil, err
	}
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
	if len(packet) > maxCloneStatusFrameSize {
		clear(packet)
		return nil, errors.New("Linux clone checkout receipt exceeds the status frame bound")
	}
	return packet, nil
}

func decodeCloneCheckoutReceipt(packet []byte, kind uint16) (authority.LinuxCloneCheckoutReceipt, error) {
	receipt, err := decodeCloneCheckoutReceiptFrame(packet, kind)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, cloneStatusReadFailure(CloneReadClassFrameHeader, err)
	}
	return receipt, nil
}

func decodeCloneCheckoutReceiptFrame(packet []byte, kind uint16) (authority.LinuxCloneCheckoutReceipt, error) {
	var receipt authority.LinuxCloneCheckoutReceipt
	if len(packet) < 12+4+4+1+8+4*6+16 || len(packet) > maxCloneStatusFrameSize ||
		string(packet[:8]) != launchMagic ||
		binary.LittleEndian.Uint16(packet[8:10]) != cloneStatusWireVersion ||
		binary.LittleEndian.Uint16(packet[10:12]) != kind {
		return receipt, errors.New("Linux clone checkout frame header or size is invalid")
	}
	position := 12
	version, err := readWireUint32(packet, &position)
	if err != nil {
		return receipt, err
	}
	receipt.Version = int(version)
	receipt.RunID, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	generation, err := readWireUint64(packet, &position)
	if err != nil || generation > uint64(^uint64(0)>>1) {
		return authority.LinuxCloneCheckoutReceipt{}, errors.New("Linux clone checkout generation is invalid")
	}
	receipt.Generation = int64(generation)
	receipt.BindingKey, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	receipt.ResourceID, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	receipt.Target, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	receipt.Nonce, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	receipt.SeedDigest, err = readWireString(packet, &position)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	receipt.BaseCommit, err = readWireString(packet, &position)
	if err != nil || len(packet)-position != 16 {
		return authority.LinuxCloneCheckoutReceipt{}, errors.New("Linux clone checkout image identity is truncated or has trailing bytes")
	}
	receipt.TargetDevice = binary.LittleEndian.Uint64(packet[position : position+8])
	receipt.TargetInode = binary.LittleEndian.Uint64(packet[position+8 : position+16])
	if err := receipt.Validate(); err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	return receipt, nil
}

func EncodeCloneCheckoutReady(receipt authority.LinuxCloneCheckoutReceipt) ([]byte, error) {
	return encodeCloneCheckoutReceipt(frameCheckoutReady, receipt)
}

func DecodeCloneCheckoutReady(packet []byte) (authority.LinuxCloneCheckoutReceipt, error) {
	return decodeCloneCheckoutReceipt(packet, frameCheckoutReady)
}

// Sent only after A durably records the receipt; the same body makes the ACK nonce-bound.
func EncodeCloneCheckoutReadyAck(receipt authority.LinuxCloneCheckoutReceipt) ([]byte, error) {
	return encodeCloneCheckoutReceipt(frameCheckoutReadyAck, receipt)
}

func DecodeCloneCheckoutReadyAck(packet []byte) (authority.LinuxCloneCheckoutReceipt, error) {
	return decodeCloneCheckoutReceipt(packet, frameCheckoutReadyAck)
}

func EncodeCloneBootstrapFailure(stage CloneBootstrapFailureStage, errno uint32) ([]byte, error) {
	if stage < CloneFailureRequest || stage > CloneFailureGateDropStatus || errno == 0 || errno > uint32(^uint32(0)>>1) {
		return nil, errors.New("Linux clone bootstrap failure code is invalid")
	}
	packet := cloneStatusFrame(frameCloneBootstrapFailure, cloneFailureSize)
	binary.LittleEndian.PutUint32(packet[12:16], uint32(stage))
	binary.LittleEndian.PutUint32(packet[16:20], errno)
	return packet, nil
}

func DecodeCloneBootstrapFailure(packet []byte) (*CloneBootstrapFailureError, error) {
	if validateCloneStatusFrame(packet, frameCloneBootstrapFailure, cloneFailureSize) != nil {
		return nil, cloneStatusReadFailure(CloneReadClassGuestMalformed,
			errors.New("Linux clone bootstrap failure frame is malformed"))
	}
	stage := CloneBootstrapFailureStage(binary.LittleEndian.Uint32(packet[12:16]))
	errno := binary.LittleEndian.Uint32(packet[16:20])
	if stage < CloneFailureRequest || stage > CloneFailureGateDropStatus || errno == 0 || errno > uint32(^uint32(0)>>1) {
		return nil, cloneStatusReadFailure(CloneReadClassGuestCode,
			errors.New("Linux clone bootstrap failure frame code is invalid"))
	}
	return &CloneBootstrapFailureError{Stage: stage, Errno: errno}, nil
}

func encodeGuardedRelease(token [32]byte, command NativeCommand, deadlineNS uint64) ([]byte, error) {
	if deadlineNS == 0 {
		return nil, errors.New("Linux RELEASE requires a nonzero monotonic lease cutoff")
	}
	body := make([]byte, 0, 24+len(command.Path)+len(command.Dir))
	body = appendWireString(body, command.Path)
	body = appendWireUint32(body, uint32(len(command.Args)))
	for _, value := range command.Args {
		body = appendWireString(body, value)
	}
	body = appendWireUint32(body, uint32(len(command.Env)))
	for _, value := range command.Env {
		body = appendWireString(body, value)
	}
	body = appendWireString(body, command.Dir)
	version := releaseWireVersion
	if command.Clone != nil {
		cloneGrant, err := json.Marshal(command.Clone)
		if err != nil {
			return nil, fmt.Errorf("encode A-authorized Linux clone grant: %w", err)
		}
		if len(cloneGrant) == 0 || len(cloneGrant) > maxReleaseSize-releaseHeaderSize-len(body)-4 {
			clear(cloneGrant)
			return nil, errors.New("Linux clone grant exceeds the guarded RELEASE bound")
		}
		body = appendWireUint32(body, uint32(len(cloneGrant)))
		body = append(body, cloneGrant...)
		clear(cloneGrant)
		version = releaseCloneWireVersion
	}
	if len(body) > maxReleaseSize-releaseHeaderSize {
		return nil, errors.New("Linux owner native command exceeds the release bound")
	}
	packet := fixedFrame(frameRelease, releaseHeaderSize+len(body))
	binary.LittleEndian.PutUint16(packet[8:10], version)
	copy(packet[12:44], token[:])
	binary.LittleEndian.PutUint32(packet[44:48], uint32(len(body)))
	binary.LittleEndian.PutUint32(packet[48:52], releaseFDCount)
	binary.LittleEndian.PutUint64(packet[52:60], deadlineNS)
	copy(packet[releaseHeaderSize:], body)
	return packet, nil
}
