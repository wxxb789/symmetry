//go:build linux && amd64

package protocol

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

func (auth GateProductionAuth) Validate() error {
	if !canonicalLowerHex(auth.LaunchToken, authority.TokenBytes) || !canonicalLowerHex(auth.Secret, authority.SecretBytes) {
		return errors.New("Linux gate-production context requires the exact per-run token and secret")
	}
	return nil
}

// ReleaseGuard binds the one-use RELEASE to the helper's live owner pipe and
// the absolute deadline from its acknowledged lease. OwnerPipe remains owned
// by the caller; Release duplicates it for SCM_RIGHTS transfer.
type ReleaseGuard struct {
	OwnerPipe     *os.File
	LeaseDeadline time.Time
}

func sendPacket(fd int, packet []byte) error {
	if len(packet) == 0 {
		return errors.New("Linux owner protocol packet is empty")
	}
	n, err := unix.SendmsgN(fd, packet, nil, nil, unix.MSG_NOSIGNAL)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("Linux owner seqpacket was short: wrote %d of %d bytes", n, len(packet))
	}
	return nil
}

func sendPacketWithRights(fd int, packet []byte, rights []int) error {
	if len(rights) != releaseFDCount {
		return errors.New("RELEASE v2 requires exactly three stdio pipes and the owner pipe")
	}
	oob := unix.UnixRights(rights...)
	n, err := unix.SendmsgN(fd, packet, oob, nil, unix.MSG_NOSIGNAL|unix.MSG_DONTWAIT)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("Linux owner RELEASE seqpacket was short: wrote %d of %d bytes", n, len(packet))
	}
	return nil
}

func receiveLauncherResult(ctx context.Context, fd int, deadline time.Time) ([]byte, int, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return nil, -1, err
	}
	data := make([]byte, launchResultSize)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, -1, err
	}
	var fds []int
	if oobn != 0 {
		fds, err = parseRights(oob[:oobn])
		if err != nil {
			return nil, -1, err
		}
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n != launchResultSize {
		closeFDs(fds)
		return nil, -1, errors.New("root launcher result has an invalid size or truncation flag")
	}
	if len(fds) > 1 {
		closeFDs(fds)
		return nil, -1, errors.New("root launcher returned more than one pidfd")
	}
	if len(fds) == 0 {
		return data, -1, nil
	}
	return data, fds[0], nil
}

func receiveLauncherReady(ctx context.Context, fd int, deadline time.Time) (LauncherReady, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return LauncherReady{}, err
	}
	data := make([]byte, maxLauncherReadySize)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return LauncherReady{}, err
	}
	if oobn != 0 {
		fds, parseErr := parseRights(oob[:oobn])
		closeFDs(fds)
		if parseErr != nil {
			return LauncherReady{}, parseErr
		}
		return LauncherReady{}, errors.New("transient launcher readiness carried unexpected descriptors")
	}
	if n == 0 {
		if flags&unix.MSG_EOR != 0 {
			return LauncherReady{}, errors.New("transient launcher sent a zero-length READY packet")
		}
		return LauncherReady{}, io.EOF
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n > maxLauncherReadySize {
		return LauncherReady{}, errors.New("transient launcher readiness has invalid size or truncation flags")
	}
	if n >= 12 && binary.LittleEndian.Uint16(data[10:12]) == frameLauncherReadyFailure {
		stage, err := DecodeLauncherReadyFailure(data[:n])
		if err != nil {
			return LauncherReady{}, err
		}
		return LauncherReady{}, &LauncherReadyFailureError{Stage: stage}
	}
	if n < 29 {
		return LauncherReady{}, errors.New("transient launcher readiness frame is too short")
	}
	return DecodeLauncherReady(data[:n])
}

func parseRights(oob []byte) ([]int, error) {
	messages, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, err
	}
	var fds []int
	for _, message := range messages {
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_RIGHTS {
			closeFDs(fds)
			return nil, errors.New("Linux owner channel carried non-SCM_RIGHTS ancillary data")
		}
		rights, err := unix.ParseUnixRights(&message)
		if err != nil {
			closeFDs(fds)
			return nil, err
		}
		fds = append(fds, rights...)
	}
	return fds, nil
}

func waitForStatus(ctx context.Context, fd int, deadline time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := 100 * time.Millisecond
		if !deadline.IsZero() {
			remaining = time.Until(deadline)
			if remaining <= 0 {
				return context.DeadlineExceeded
			}
		}
		pollFor := remaining
		if pollFor > 100*time.Millisecond {
			pollFor = 100 * time.Millisecond
		}
		millis := int(math.Ceil(float64(pollFor) / float64(time.Millisecond)))
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		count, err := unix.Poll(fds, millis)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
	}
}

// EncodeGateMap carries the fixed IDs and one-use token only over the private
// root-to-static-gate socket. It is never placed in argv or environment.
func EncodeGateMap(config OwnerConfig, token [32]byte) []byte {
	packet := fixedFrame(frameGateMap, gateMapSize)
	binary.LittleEndian.PutUint32(packet[12:16], config.AUID)
	binary.LittleEndian.PutUint32(packet[16:20], config.AGID)
	binary.LittleEndian.PutUint32(packet[20:24], config.BUID)
	binary.LittleEndian.PutUint32(packet[24:28], config.BGID)
	copy(packet[28:], token[:])
	return packet
}

// DecodeGateMap validates the exact root-to-gate B-only map acknowledgement.
func DecodeGateMap(packet []byte) (OwnerConfig, [32]byte, error) {
	var token [32]byte
	if err := validateFixedFrame(packet, frameGateMap, gateMapSize); err != nil {
		return OwnerConfig{}, token, err
	}
	config := OwnerConfig{
		AUID: binary.LittleEndian.Uint32(packet[12:16]),
		AGID: binary.LittleEndian.Uint32(packet[16:20]),
		BUID: binary.LittleEndian.Uint32(packet[20:24]),
		BGID: binary.LittleEndian.Uint32(packet[24:28]),
	}
	if err := config.Validate(); err != nil {
		return OwnerConfig{}, token, err
	}
	copy(token[:], packet[28:60])
	return config, token, nil
}

// EncodeRelease creates a one-use guarded RELEASE. Non-clone scratch commands
// use v2; a clone grant is encoded only in v3 and is never downgraded. The
// fixed header binds an absolute CLOCK_MONOTONIC cutoff; SCM_RIGHTS always
// carries three stdio pipes and the owner-pipe read end.
func EncodeRelease(token [32]byte, command NativeCommand, guard ReleaseGuard) ([]byte, error) {
	if err := command.Validate(); err != nil {
		return nil, err
	}
	if err := validateReleaseOwnerPipe(guard.OwnerPipe, command); err != nil {
		return nil, err
	}
	deadlineNS, err := monotonicDeadlineNS(guard.LeaseDeadline)
	if err != nil {
		return nil, err
	}
	return encodeGuardedRelease(token, command, deadlineNS)
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

// DecodeRelease accepts a direct v2 or clone v3 frame, the exact fresh token,
// four descriptors, and a bounded complete command.
func DecodeRelease(packet []byte, expectedToken [32]byte, descriptors [releaseFDCount]*os.File) (NativeCommand, uint64, error) {
	return decodeReleaseFrame(packet, expectedToken, descriptors, true, false)
}

// DecodeCloneBootstrapRequest decodes the sealed, token-scrubbed v3 request
// passed to the trusted B bootstrap. The inherited control descriptors are
// validated by the C gate before this process is entered.
func DecodeCloneBootstrapRequest(packet []byte) (NativeCommand, uint64, error) {
	var noToken [32]byte
	return decodeReleaseFrame(packet, noToken, [releaseFDCount]*os.File{}, false, true)
}

func decodeReleaseFrame(packet []byte, expectedToken [32]byte, descriptors [releaseFDCount]*os.File,
	validateDescriptors, bootstrap bool) (NativeCommand, uint64, error) {
	if len(packet) < releaseHeaderSize || len(packet) > maxReleaseSize || string(packet[:8]) != launchMagic ||
		(binary.LittleEndian.Uint16(packet[8:10]) != releaseWireVersion && binary.LittleEndian.Uint16(packet[8:10]) != releaseCloneWireVersion) ||
		binary.LittleEndian.Uint16(packet[10:12]) != frameRelease {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE v2/v3 header version or type is invalid")
	}
	version := binary.LittleEndian.Uint16(packet[8:10])
	if bootstrap && version != releaseCloneWireVersion {
		return NativeCommand{}, 0, errors.New("clone bootstrap requires RELEASE v3")
	}
	if subtle.ConstantTimeCompare(packet[12:44], expectedToken[:]) != 1 {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE token is stale")
	}
	bodySize := binary.LittleEndian.Uint32(packet[44:48])
	if binary.LittleEndian.Uint32(packet[48:52]) != releaseFDCount {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE requires exactly four descriptors")
	}
	if uint64(bodySize) != uint64(len(packet)-releaseHeaderSize) {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE body size is invalid")
	}
	deadlineNS := binary.LittleEndian.Uint64(packet[52:60])
	if deadlineNS == 0 {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE monotonic deadline is empty")
	}
	body := packet[releaseHeaderSize:]
	position := 0
	path, err := readWireString(body, &position)
	if err != nil {
		return NativeCommand{}, 0, fmt.Errorf("decode Linux owner RELEASE path: %w", err)
	}
	argc, err := readWireUint32(body, &position)
	if err != nil || argc == 0 || argc > maxCommandVector {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE argv count is invalid")
	}
	command := NativeCommand{Path: path, Args: make([]string, int(argc))}
	for index := range command.Args {
		command.Args[index], err = readWireString(body, &position)
		if err != nil {
			return NativeCommand{}, 0, fmt.Errorf("decode Linux owner RELEASE argv[%d]: %w", index, err)
		}
	}
	envc, err := readWireUint32(body, &position)
	if err != nil || envc > maxCommandVector {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE environment count is invalid")
	}
	command.Env = make([]string, int(envc))
	for index := range command.Env {
		command.Env[index], err = readWireString(body, &position)
		if err != nil {
			return NativeCommand{}, 0, fmt.Errorf("decode Linux owner RELEASE env[%d]: %w", index, err)
		}
	}
	command.Dir, err = readWireString(body, &position)
	if err != nil {
		return NativeCommand{}, 0, fmt.Errorf("decode Linux owner RELEASE working directory: %w", err)
	}
	if version == releaseCloneWireVersion {
		cloneSize, sizeErr := readWireUint32(body, &position)
		if sizeErr != nil || cloneSize == 0 || uint64(cloneSize) > uint64(len(body)-position) {
			return NativeCommand{}, 0, errors.New("Linux clone RELEASE grant payload is empty or truncated")
		}
		decoder := json.NewDecoder(bytes.NewReader(body[position : position+int(cloneSize)]))
		decoder.DisallowUnknownFields()
		var grant authority.LinuxCloneGrant
		if err := decoder.Decode(&grant); err != nil {
			return NativeCommand{}, 0, fmt.Errorf("decode Linux clone RELEASE grant: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return NativeCommand{}, 0, errors.New("Linux clone RELEASE grant has trailing JSON data")
		}
		if err := grant.Validate(); err != nil {
			return NativeCommand{}, 0, fmt.Errorf("validate Linux clone RELEASE grant: %w", err)
		}
		command.Clone = &grant
		position += int(cloneSize)
	}
	if position != len(body) {
		return NativeCommand{}, 0, errors.New("Linux owner RELEASE has trailing binary data")
	}
	if validateDescriptors {
		command.Stdin, command.Stdout, command.Stderr = descriptors[0], descriptors[1], descriptors[2]
		if err := command.Validate(); err != nil {
			return NativeCommand{}, 0, err
		}
		if err := validateReleaseOwnerPipe(descriptors[3], command); err != nil {
			return NativeCommand{}, 0, err
		}
	} else if err := command.validateFields(); err != nil {
		return NativeCommand{}, 0, err
	}
	if bootstrap && command.Clone == nil {
		return NativeCommand{}, 0, errors.New("clone bootstrap request lacks an A-authorized grant")
	}
	return command, deadlineNS, nil
}

func monotonicDeadlineNS(deadline time.Time) (uint64, error) {
	if deadline.IsZero() {
		return 0, errors.New("Linux RELEASE requires an acknowledged monotonic lease deadline")
	}
	if deadline == deadline.Round(0) {
		return 0, errors.New("Linux RELEASE lease deadline has no monotonic component")
	}
	var now unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &now); err != nil {
		return 0, fmt.Errorf("read CLOCK_MONOTONIC for RELEASE: %w", err)
	}
	// Sample Go's monotonic deadline after CLOCK_MONOTONIC so conversion is
	// conservative: syscall/dispatch delay can shorten, never extend, the lease.
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, errors.New("Linux RELEASE lease deadline has already expired")
	}
	if now.Sec < 0 || now.Nsec < 0 || now.Nsec >= 1_000_000_000 {
		return 0, errors.New("CLOCK_MONOTONIC returned an invalid time")
	}
	const nanosecondsPerSecond = uint64(1_000_000_000)
	seconds := uint64(now.Sec)
	if seconds > ^uint64(0)/nanosecondsPerSecond {
		return 0, errors.New("CLOCK_MONOTONIC seconds overflow RELEASE deadline")
	}
	baseNS := seconds * nanosecondsPerSecond
	nanoseconds := uint64(now.Nsec)
	if nanoseconds > ^uint64(0)-baseNS {
		return 0, errors.New("CLOCK_MONOTONIC nanoseconds overflow RELEASE deadline")
	}
	nowNS := baseNS + nanoseconds
	delta := uint64(remaining.Nanoseconds())
	if delta > ^uint64(0)-nowNS {
		return 0, errors.New("lease deadline overflows monotonic nanoseconds")
	}
	return nowNS + delta, nil
}

func validateReleaseOwnerPipe(ownerPipe *os.File, command NativeCommand) error {
	if ownerPipe == nil {
		return errors.New("Linux RELEASE requires the helper-owned read-only owner pipe")
	}
	fd := int(ownerPipe.Fd())
	if fd < 0 {
		return errors.New("Linux RELEASE owner pipe descriptor is closed")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat helper owner pipe: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFIFO || stat.Uid != uint32(os.Getuid()) || stat.Gid != uint32(os.Getgid()) {
		return errors.New("Linux RELEASE owner pipe is not an A-owned FIFO")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY || flags&unix.O_PATH != 0 {
		return fmt.Errorf("Linux RELEASE owner pipe is not read-only: flags=%#x err=%v", flags, err)
	}
	for _, pipe := range []*os.File{command.Stdin, command.Stdout, command.Stderr} {
		var pipeStat unix.Stat_t
		if err := unix.Fstat(int(pipe.Fd()), &pipeStat); err != nil {
			return fmt.Errorf("stat native stdio while checking owner pipe: %w", err)
		}
		if stat.Dev == pipeStat.Dev && stat.Ino == pipeStat.Ino {
			return errors.New("Linux RELEASE owner pipe aliases a native stdio pipe")
		}
	}
	return nil
}

func (command NativeCommand) Validate() error {
	if err := command.validateFields(); err != nil {
		return err
	}
	return validateRunnerPipes(command.Stdin, command.Stdout, command.Stderr)
}

func (command NativeCommand) validateFields() error {
	if !filepath.IsAbs(command.Path) || command.Path == "/" || len(command.Path) > maxCommandString || strings.ContainsRune(command.Path, '\x00') {
		return errors.New("Linux owner native executable path must be an absolute non-root path")
	}
	if !filepath.IsAbs(command.Dir) || len(command.Dir) > maxCommandString || strings.ContainsRune(command.Dir, '\x00') {
		return errors.New("Linux owner native working directory must be absolute")
	}
	if len(command.Args) == 0 || len(command.Args) > maxCommandVector || len(command.Env) > maxCommandVector {
		return errors.New("Linux owner native argv/env vector has an invalid length")
	}
	if command.Clone != nil {
		if err := command.Clone.Validate(); err != nil {
			return fmt.Errorf("validate B-owned standalone clone grant: %w", err)
		}
		if command.Dir != command.Clone.Intent.Target {
			return errors.New("Linux clone native working directory differs from its exact target")
		}
	} else if command.Reuse != nil {
		if err := command.Reuse.Validate(); err != nil {
			return fmt.Errorf("validate retained Linux clone reuse grant: %w", err)
		}
		if command.Dir != command.Reuse.SourceCheckout.Target {
			return errors.New("Linux clone reuse working directory differs from its retained target")
		}
	}
	total := len(command.Path) + len(command.Dir)
	for _, value := range command.Args {
		if value == "" || len(value) > maxCommandString || strings.ContainsRune(value, '\x00') {
			return errors.New("Linux owner native argv contains an empty, oversized, or NUL value")
		}
		total += len(value)
	}
	seen := make(map[string]struct{}, len(command.Env))
	for _, value := range command.Env {
		if len(value) > maxCommandString || strings.ContainsRune(value, '\x00') {
			return errors.New("Linux owner native environment contains an oversized or NUL value")
		}
		key, _, found := strings.Cut(value, "=")
		if !found || key == "" || strings.ContainsRune(key, '\x00') {
			return errors.New("Linux owner native environment entry is not KEY=VALUE")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("Linux owner native environment repeats key %q", key)
		}
		seen[key] = struct{}{}
		total += len(value)
	}
	if total > maxReleaseSize-releaseHeaderSize-4096 {
		return errors.New("Linux owner native argv/env exceeds the release bound")
	}
	return nil
}

func validateRunnerPipes(stdin, stdout, stderr *os.File) error {
	files := [releaseStdioFDCount]*os.File{stdin, stdout, stderr}
	wantedAccess := [releaseStdioFDCount]int{unix.O_RDONLY, unix.O_WRONLY, unix.O_WRONLY}
	type pipeIdentity struct{ device, inode uint64 }
	var identities [releaseStdioFDCount]pipeIdentity
	var descriptors [releaseStdioFDCount]int
	for index, file := range files {
		if file == nil {
			return errors.New("native command requires Runner-owned stdin-read, stdout-write, and stderr-write pipes")
		}
		descriptors[index] = int(file.Fd())
		if descriptors[index] < 0 {
			return errors.New("native standard-I/O pipe descriptor is closed")
		}
		var stat unix.Stat_t
		if err := unix.Fstat(descriptors[index], &stat); err != nil {
			return fmt.Errorf("stat native standard-I/O pipe %d: %w", index, err)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFIFO || stat.Uid != uint32(os.Getuid()) || stat.Gid != uint32(os.Getgid()) {
			return fmt.Errorf("native standard-I/O descriptor %d is not an A-owned pipe", index)
		}
		flags, err := unix.FcntlInt(uintptr(descriptors[index]), unix.F_GETFL, 0)
		if err != nil || flags&unix.O_ACCMODE != wantedAccess[index] {
			return fmt.Errorf("native standard-I/O pipe %d has the wrong access direction: flags=%#x err=%v", index, flags, err)
		}
		identities[index] = pipeIdentity{device: uint64(stat.Dev), inode: stat.Ino}
		for prior := 0; prior < index; prior++ {
			if descriptors[prior] == descriptors[index] || identities[prior] == identities[index] {
				return errors.New("native stdin/stdout/stderr must be three distinct pipe ends")
			}
		}
	}
	return nil
}

func receiveCloneStatusPacket(ctx context.Context, fd int, deadline time.Time) ([]byte, bool, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return nil, false, err
	}
	data := make([]byte, maxCloneStatusFrameSize)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, false, err
	}
	if oobn != 0 {
		fds, parseErr := parseRights(oob[:oobn])
		closeFDs(fds)
		if parseErr != nil {
			return nil, false, parseErr
		}
		return nil, false, errors.New("clone status channel carried unexpected descriptors")
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n > maxCloneStatusFrameSize {
		return nil, false, errors.New("clone status frame is truncated or oversized")
	}
	if n == 0 {
		if flags&unix.MSG_EOR != 0 {
			return nil, false, errors.New("clone status channel carried a zero-length packet")
		}
		return nil, true, nil
	}
	if n < 12 || string(data[:8]) != launchMagic || binary.LittleEndian.Uint16(data[8:10]) != cloneStatusWireVersion {
		return nil, false, errors.New("clone status frame magic or version is invalid")
	}
	return data[:n], false, nil
}

func sendCloneStatusPacket(fd int, packet []byte) error {
	if len(packet) < 12 || len(packet) > maxCloneStatusFrameSize ||
		string(packet[:8]) != launchMagic || binary.LittleEndian.Uint16(packet[8:10]) != cloneStatusWireVersion {
		return errors.New("clone status packet is not a bounded v3 frame")
	}
	n, err := unix.SendmsgN(fd, packet, nil, nil, unix.MSG_NOSIGNAL|unix.MSG_DONTWAIT)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("clone status seqpacket short write: %d/%d", n, len(packet))
	}
	return nil
}

func readCloneBootstrapExecAttempt(ctx context.Context, fd int, deadline time.Time) (uint64, uint64, error) {
	packet, eof, err := receiveCloneStatusPacket(ctx, fd, deadline)
	if err != nil {
		return 0, 0, err
	}
	if eof {
		return 0, 0, io.EOF
	}
	if len(packet) >= 12 && binary.LittleEndian.Uint16(packet[10:12]) == frameCloneBootstrapFailure {
		failure, decodeErr := DecodeCloneBootstrapFailure(packet)
		if decodeErr != nil {
			return 0, 0, decodeErr
		}
		return 0, 0, failure
	}
	return decodeCloneExecAttempt(packet, frameBootstrapExecAttempt)
}

func readCloneCheckoutReady(ctx context.Context, fd int, deadline time.Time) (authority.LinuxCloneCheckoutReceipt, error) {
	packet, eof, err := receiveCloneStatusPacket(ctx, fd, deadline)
	if err != nil {
		return authority.LinuxCloneCheckoutReceipt{}, err
	}
	if eof {
		return authority.LinuxCloneCheckoutReceipt{}, io.EOF
	}
	if len(packet) >= 12 && binary.LittleEndian.Uint16(packet[10:12]) == frameCloneBootstrapFailure {
		failure, decodeErr := DecodeCloneBootstrapFailure(packet)
		if decodeErr != nil {
			return authority.LinuxCloneCheckoutReceipt{}, decodeErr
		}
		return authority.LinuxCloneCheckoutReceipt{}, failure
	}
	return DecodeCloneCheckoutReady(packet)
}

func readCloneNativeExecResult(ctx context.Context, fd int, deadline time.Time) (execStatus, error) {
	packet, eof, err := receiveCloneStatusPacket(ctx, fd, deadline)
	if err != nil {
		return execStatus{}, err
	}
	if eof {
		return execStatus{}, io.EOF
	}
	if len(packet) >= 12 && binary.LittleEndian.Uint16(packet[10:12]) == frameCloneBootstrapFailure {
		failure, decodeErr := DecodeCloneBootstrapFailure(packet)
		if decodeErr != nil {
			return execStatus{}, decodeErr
		}
		return execStatus{}, failure
	}
	device, inode, err := DecodeNativeExecAttempt(packet)
	if err != nil {
		return execStatus{}, err
	}
	final, eof, err := receiveCloneStatusPacket(ctx, fd, deadline)
	if err != nil {
		return execStatus{}, err
	}
	if eof {
		return execStatus{execAttempted: true, device: device, inode: inode}, nil
	}
	if len(final) >= 12 && binary.LittleEndian.Uint16(final[10:12]) == frameCloneBootstrapFailure {
		failure, decodeErr := DecodeCloneBootstrapFailure(final)
		if decodeErr != nil {
			return execStatus{}, decodeErr
		}
		return execStatus{}, failure
	}
	return execStatus{}, errors.New("clone status channel carried data after native exec attempt")
}

func receiveExecStatus(ctx context.Context, fd int, deadline time.Time) (execStatus, error) {
	first, err := receiveStatusPacket(ctx, fd, deadline)
	if err != nil {
		return execStatus{}, err
	}
	if len(first) == execFailureSize {
		if err := validateFixedFrame(first, frameExecFailure, execFailureSize); err != nil {
			return execStatus{}, err
		}
		return execStatus{errno: binary.LittleEndian.Uint32(first[12:16])}, nil
	}
	if err := validateFixedFrame(first, frameExecAttempt, execAttemptSize); err != nil {
		return execStatus{}, err
	}
	status := execStatus{
		execAttempted: true,
		device:        binary.LittleEndian.Uint64(first[12:20]),
		inode:         binary.LittleEndian.Uint64(first[20:28]),
	}
	if status.inode == 0 {
		return execStatus{}, errors.New("exec-attempt frame has no executable inode")
	}
	second, eof, err := receiveStatusPacketOrEOF(ctx, fd, deadline)
	if err != nil {
		return execStatus{}, err
	}
	if eof {
		return status, nil
	}
	if len(second) != execFailureSize || validateFixedFrame(second, frameExecFailure, execFailureSize) != nil {
		return execStatus{}, errors.New("exec-status channel carried an invalid post-attempt frame")
	}
	status.execAttempted = false
	status.errno = binary.LittleEndian.Uint32(second[12:16])
	return status, nil
}

type execStatus struct {
	execAttempted bool
	errno         uint32
	device        uint64
	inode         uint64
}

func receiveStatusPacket(ctx context.Context, fd int, deadline time.Time) ([]byte, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return nil, err
	}
	data := make([]byte, execAttemptSize+1)
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, nil, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, err
	}
	if n == 0 || oobn != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return nil, errors.New("exec-status channel closed or carried ancillary data before a frame")
	}
	return data[:n], nil
}

func receiveStatusPacketOrEOF(ctx context.Context, fd int, deadline time.Time) ([]byte, bool, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return nil, false, err
	}
	data := make([]byte, execFailureSize+1)
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, nil, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, false, err
	}
	if oobn != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return nil, false, errors.New("exec-status channel carried an invalid second packet")
	}
	if n == 0 {
		if flags&unix.MSG_EOR != 0 {
			return nil, false, errors.New("exec-status channel carried a zero-length packet instead of EOF")
		}
		return nil, true, nil
	}
	return data[:n], false, nil
}

func closeFDs(fds []int) {
	for _, fd := range fds {
		_ = unix.Close(fd)
	}
}
