//go:build linux && amd64

package protocol

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// InaccessibleUserNamespaceError is returned when the host procfs/ptrace
// policy prevents A from reading the exact parked gate's user-namespace
// nsfs identity. Linux v2 must fail closed on this host; it never falls back
// to PID-only identity or a native RELEASE.
type InaccessibleUserNamespaceError struct {
	PID   int
	Cause error
}

func (failure *InaccessibleUserNamespaceError) Error() string {
	if failure == nil {
		return ErrInaccessibleUserNamespace.Error()
	}
	return fmt.Sprintf("%v for parked gate PID %d: %v", ErrInaccessibleUserNamespace, failure.PID, failure.Cause)
}

func (failure *InaccessibleUserNamespaceError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return errors.Join(ErrInaccessibleUserNamespace, failure.Cause)
}

// nativeImageIdentity is captured from an A-opened O_PATH descriptor before
// RELEASE. The status bytes from B are only a claim; they must match this
// identity and the same-PID /proc/<pid>/exe readback independently.
type nativeImageIdentity struct {
	Device uint64
	Inode  uint64
	UID    uint32
	GID    uint32
	Mode   uint32
	Size   int64
	Links  uint64
}

// pinnedNativeImage retains the exact approved inode for the complete
// RELEASE/readback boundary and remembers the symlink-free path A inspected.
type pinnedNativeImage struct {
	file     *os.File
	path     string
	identity nativeImageIdentity
}

func (image *pinnedNativeImage) close() error {
	if image == nil || image.file == nil {
		return nil
	}
	err := image.file.Close()
	image.file = nil
	return err
}

func verifyExecAttemptImageIdentity(device, inode uint64, expected nativeImageIdentity) error {
	if expected.Inode == 0 || device != expected.Device || inode != expected.Inode {
		return errors.New("B exec-attempt status differs from A-pinned approved image")
	}
	return nil
}

func pinNativeImage(path string, config OwnerConfig) (*pinnedNativeImage, error) {
	parentFD, basename, err := openTrustedImageParent(path, config)
	if err != nil {
		return nil, err
	}
	fd, openErr := unix.Openat(parentFD, basename, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	parentCloseErr := unix.Close(parentFD)
	if openErr != nil {
		return nil, errors.Join(fmt.Errorf("open approved native image %q without following symlinks: %w", path, openErr), parentCloseErr)
	}
	if parentCloseErr != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("close approved native image parent descriptor: %w", parentCloseErr)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("stat A-pinned native image %q: %w", path, err)
	}
	identity, err := approvedNativeImageIdentity(stat, config)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("native image %q is not A-approved: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap pinned native image %q", path)
	}
	return &pinnedNativeImage{file: file, path: path, identity: identity}, nil
}

func openTrustedImageParent(path string, config OwnerConfig) (int, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || len(path) > maxCommandString || strings.ContainsRune(path, '\x00') {
		return -1, "", errors.New("approved native image path must be a normalized absolute non-root path")
	}
	components := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(components) < 2 {
		return -1, "", errors.New("approved native image must have an A-controlled parent directory")
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return -1, "", errors.New("approved native image path contains an unsafe component")
		}
	}
	currentFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, "", fmt.Errorf("open root for approved native image: %w", err)
	}
	if err := verifyTrustedImageDirectory(currentFD, config); err != nil {
		_ = unix.Close(currentFD)
		return -1, "", fmt.Errorf("root is not a trusted native-image parent: %w", err)
	}
	for _, component := range components[:len(components)-1] {
		nextFD, err := unix.Openat(currentFD, component, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			_ = unix.Close(currentFD)
			return -1, "", fmt.Errorf("open native-image parent component %q without following symlinks: %w", component, err)
		}
		if err := verifyTrustedImageDirectory(nextFD, config); err != nil {
			_ = unix.Close(nextFD)
			_ = unix.Close(currentFD)
			return -1, "", fmt.Errorf("native-image parent component %q is not A-controlled and non-B-writable: %w", component, err)
		}
		if err := unix.Close(currentFD); err != nil {
			_ = unix.Close(nextFD)
			return -1, "", fmt.Errorf("close prior native-image parent descriptor: %w", err)
		}
		currentFD = nextFD
	}
	return currentFD, components[len(components)-1], nil
}

func verifyTrustedImageDirectory(fd int, config OwnerConfig) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || (stat.Uid != 0 && stat.Uid != config.AUID) ||
		stat.Mode&0022 != 0 || stat.Mode&0111 == 0 {
		return errors.New("parent must be a root/A-owned directory with traversal permission and no group/world write")
	}
	return nil
}

func approvedNativeImageIdentity(stat unix.Stat_t, config OwnerConfig) (nativeImageIdentity, error) {
	identity := nativeImageIdentity{
		Device: uint64(stat.Dev), Inode: stat.Ino, UID: stat.Uid, GID: stat.Gid,
		Mode: stat.Mode, Size: stat.Size, Links: stat.Nlink,
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || (stat.Uid != 0 && stat.Uid != config.AUID) ||
		stat.Mode&0022 != 0 || stat.Mode&07000 != 0 || stat.Nlink == 0 || stat.Size <= 0 || stat.Mode&0111 == 0 {
		return nativeImageIdentity{}, errors.New("image must be a root/A-owned regular executable without set-ID bits or B write access")
	}
	return identity, nil
}

func (image *pinnedNativeImage) verifyCurrent(config OwnerConfig) error {
	if image == nil || image.file == nil || image.path == "" || image.identity.Inode == 0 {
		return errors.New("A-pinned native image descriptor is unavailable")
	}
	var pinnedStat unix.Stat_t
	if err := unix.Fstat(int(image.file.Fd()), &pinnedStat); err != nil {
		return fmt.Errorf("re-read A-pinned native image: %w", err)
	}
	pinnedIdentity, err := approvedNativeImageIdentity(pinnedStat, config)
	if err != nil || pinnedIdentity != image.identity {
		return fmt.Errorf("A-pinned native image metadata changed: %v", err)
	}
	current, err := pinNativeImage(image.path, config)
	if err != nil {
		return fmt.Errorf("re-open approved native image path: %w", err)
	}
	defer current.close()
	if current.identity != image.identity {
		return errors.New("approved native image path changed after A pinned it")
	}
	return nil
}

func verifyInstalledLauncher(config OwnerConfig) error {
	for _, path := range []string{"/usr", "/usr/libexec", "/usr/libexec/symmetry"} {
		fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return fmt.Errorf("open fixed launcher directory %q: %w", path, err)
		}
		var directory unix.Stat_t
		statErr := unix.Fstat(fd, &directory)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil || directory.Mode&unix.S_IFMT != unix.S_IFDIR || directory.Uid != 0 || directory.Gid != 0 || directory.Mode&0022 != 0 {
			return errors.Join(fmt.Errorf("fixed launcher directory %q is not root-owned and non-writable", path), statErr, closeErr)
		}
	}
	fd, err := unix.Open(launcherPath, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open fixed root launcher: %w", err)
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat fixed root launcher: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Gid != config.AGID ||
		stat.Mode&07777 != (unix.S_ISUID|0750) || stat.Nlink != 1 {
		return errors.New("fixed launcher is not root:A-group mode 04750, singly-linked, and executable only by A's dedicated group")
	}
	return nil
}

func verifyInstalledCloneBootstrap(config OwnerConfig) (*pinnedNativeImage, error) {
	if err := verifyInstalledLauncher(config); err != nil {
		return nil, err
	}
	image, err := pinNativeImage(cloneBootstrapPath, config)
	if err != nil {
		return nil, fmt.Errorf("open fixed B clone bootstrap: %w", err)
	}
	identity := image.identity
	if identity.UID != 0 || identity.GID != 0 || identity.Mode&07777 != 0755 || identity.Links != 1 {
		_ = image.close()
		return nil, errors.New("fixed B clone bootstrap is not singly-linked root:root mode 0755")
	}
	return image, nil
}

func verifyCgroupLeafFD(fd int, ownerUID uint32) error {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return fmt.Errorf("stat pinned cgroup leaf filesystem: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat pinned cgroup leaf: %w", err)
	}
	if uint64(filesystem.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != ownerUID {
		return errors.New("pinned cgroup leaf is not an A-owned cgroup-v2 directory")
	}
	return nil
}

func leafContainsPID(leafFD, pid int) error {
	fd, err := unix.Openat(leafFD, "cgroup.procs", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open pinned leaf cgroup.procs: %w", err)
	}
	defer unix.Close(fd)
	buffer := make([]byte, 64<<10)
	n, err := unix.Read(fd, buffer)
	if err != nil {
		return fmt.Errorf("read pinned leaf cgroup.procs: %w", err)
	}
	if n == len(buffer) {
		return errors.New("pinned leaf cgroup.procs exceeded the bounded read")
	}
	wanted := strconv.Itoa(pid)
	for _, line := range strings.Fields(string(buffer[:n])) {
		if line == wanted {
			return nil
		}
	}
	return fmt.Errorf("exact gate PID %d is not in the pinned run leaf", pid)
}

func verifyOnlyFixedBMaps(pid int, config OwnerConfig) error {
	uidMap, err := readProcFile(pid, "uid_map")
	if err != nil {
		return err
	}
	gidMap, err := readProcFile(pid, "gid_map")
	if err != nil {
		return err
	}
	setgroups, err := readProcFile(pid, "setgroups")
	if err != nil {
		return err
	}
	if canonicalMap(uidMap) != fmt.Sprintf("1 %d 1", config.BUID) || canonicalMap(gidMap) != fmt.Sprintf("1 %d 1", config.BGID) || strings.TrimSpace(setgroups) != "deny" {
		return fmt.Errorf("A gate user namespace is not the exact B-only map: uid=%q gid=%q setgroups=%q", uidMap, gidMap, setgroups)
	}
	return nil
}

func readProcFile(pid int, name string) (string, error) {
	if pid <= 1 || (name != "uid_map" && name != "gid_map" && name != "setgroups") {
		return "", errors.New("read Linux user-namespace proc file requires a fixed name and positive PID")
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", pid, name))
	if err != nil {
		return "", fmt.Errorf("read exact process %s: %w", name, err)
	}
	if len(data) == 0 || len(data) > 4096 {
		return "", fmt.Errorf("exact process %s has an invalid size %d", name, len(data))
	}
	return string(data), nil
}

func canonicalMap(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

type procStatus struct {
	uids       [4]uint32
	gids       [4]uint32
	groups     []uint32
	parent     int
	capEff     uint64
	capPrm     uint64
	capInh     uint64
	capBnd     uint64
	capAmb     uint64
	noNewPrivs uint32
}

func readProcessStatus(pid int) (procStatus, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return procStatus{}, fmt.Errorf("read exact process status: %w", err)
	}
	var status procStatus
	seen := uint16(0)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		parseIDs := func(destination *[4]uint32) error {
			if len(fields) != 4 {
				return errors.New("process status credential vector is not four IDs")
			}
			for index, field := range fields {
				parsed, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return err
				}
				destination[index] = uint32(parsed)
			}
			return nil
		}
		parseNumber := func(base int, bits int) (uint64, error) {
			if len(fields) != 1 {
				return 0, errors.New("process status scalar is malformed")
			}
			return strconv.ParseUint(fields[0], base, bits)
		}
		var err error
		switch key {
		case "Uid":
			err = parseIDs(&status.uids)
			seen |= 1
		case "Gid":
			err = parseIDs(&status.gids)
			seen |= 2
		case "Groups":
			status.groups = make([]uint32, len(fields))
			for index, field := range fields {
				parsed, parseErr := strconv.ParseUint(field, 10, 32)
				if parseErr != nil {
					err = parseErr
					break
				}
				status.groups[index] = uint32(parsed)
			}
			seen |= 4
		case "PPid":
			var value uint64
			value, err = parseNumber(10, 31)
			status.parent = int(value)
			seen |= 8
		case "CapEff":
			status.capEff, err = parseNumber(16, 64)
			seen |= 16
		case "CapPrm":
			status.capPrm, err = parseNumber(16, 64)
			seen |= 32
		case "CapInh":
			status.capInh, err = parseNumber(16, 64)
			seen |= 64
		case "CapBnd":
			status.capBnd, err = parseNumber(16, 64)
			seen |= 256
		case "CapAmb":
			status.capAmb, err = parseNumber(16, 64)
			seen |= 512
		case "NoNewPrivs":
			var value uint64
			value, err = parseNumber(10, 32)
			status.noNewPrivs = uint32(value)
			seen |= 128
		}
		if err != nil {
			return procStatus{}, fmt.Errorf("parse process status %s: %w", key, err)
		}
	}
	if seen&15 != 15 {
		return procStatus{}, errors.New("process status lacks UID/GID/Groups/PPid fields")
	}
	if seen&1008 != 1008 {
		return procStatus{}, errors.New("process status lacks capability bounding/ambient or no_new_privs fields")
	}
	return status, nil
}

type namespaceStat struct{ device, inode uint64 }

func readUserNamespace(pid int) (namespaceStat, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(fmt.Sprintf("/proc/%d/ns/user", pid), &stat); err != nil {
		return namespaceStat{}, fmt.Errorf("stat exact user namespace: %w", err)
	}
	return namespaceIdentityFromStat(pid, stat)
}

func namespaceIdentityFromStat(pid int, stat syscall.Stat_t) (namespaceStat, error) {
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Ino == 0 {
		cause := fmt.Errorf("exact user namespace is not an accessible nsfs inode: mode=%#o inode=%d", stat.Mode, stat.Ino)
		return namespaceStat{}, &InaccessibleUserNamespaceError{PID: pid, Cause: cause}
	}
	return namespaceStat{device: uint64(stat.Dev), inode: stat.Ino}, nil
}

func readProcessStartTime(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, fmt.Errorf("read exact process stat: %w", err)
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+1 >= len(data) {
		return 0, errors.New("process stat lacks a command terminator")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) <= 19 {
		return 0, errors.New("process stat lacks start-time field")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return 0, fmt.Errorf("process start-time field is invalid: %w", err)
	}
	return start, nil
}

func readPIDFDTarget(pidfd int) (int, error) {
	fdInfo, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", pidfd))
	if err != nil {
		return 0, fmt.Errorf("read exact pidfd info: %w", err)
	}
	for _, line := range strings.Split(string(fdInfo), "\n") {
		key, value, found := strings.Cut(line, ":")
		if found && key == "Pid" {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(value))
			if parseErr != nil || pid <= 0 {
				return 0, errors.New("pidfd info has invalid target PID")
			}
			return pid, nil
		}
	}
	return 0, errors.New("fdinfo does not identify a pidfd target")
}

func readBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("read kernel boot identity: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" || len(value) > 64 {
		return "", errors.New("kernel boot identity is empty or oversized")
	}
	return value, nil
}

// CaptureLauncherIdentity uses the platform's existing linux:v2 identity
// spelling (boot ID, PID namespace inode, PID, start tick). A restricted
// /proc/<pid>/ns/pid symlink is not a namespace identity.
func CaptureLauncherIdentity(pid int) (LauncherIdentity, error) {
	if pid <= 1 {
		return LauncherIdentity{}, errors.New("launcher identity requires a PID above one")
	}
	startTime, err := readProcessStartTime(pid)
	if err != nil {
		return LauncherIdentity{}, err
	}
	bootID, err := readBootID()
	if err != nil {
		return LauncherIdentity{}, err
	}
	var namespace syscall.Stat_t
	if err := syscall.Stat(fmt.Sprintf("/proc/%d/ns/pid", pid), &namespace); err != nil {
		return LauncherIdentity{}, fmt.Errorf("stat launcher PID namespace: %w", err)
	}
	if namespace.Mode&syscall.S_IFMT != syscall.S_IFREG || namespace.Ino == 0 {
		return LauncherIdentity{}, errors.New("launcher PID namespace is not an accessible nsfs inode")
	}
	identity := fmt.Sprintf("linux:v2:%s:%d:%d:%d", bootID, namespace.Ino, pid, startTime)
	return LauncherIdentity{PID: pid, StartTime: startTime, BootID: bootID, Identity: identity}, nil
}

func probePIDFDSupport() error {
	fd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return fmt.Errorf("open process pidfd: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.PidfdSendSignal(fd, 0, nil, 0); err != nil {
		return fmt.Errorf("pidfd signal-zero probe: %w", err)
	}
	var info unix.Siginfo
	err = unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
	if !errors.Is(err, unix.ECHILD) {
		return fmt.Errorf("pidfd waitid probe did not return the expected non-child result: %w", err)
	}
	return nil
}
