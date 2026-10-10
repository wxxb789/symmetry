// Credential-free Linux user-namespace/fdinfo/pidfd_getfd prerequisite probe.
// This reports host prerequisites only, not production A/B installation or native support.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	probeBUID     = 65534
	probeBGID     = 65534
	capSysSetgid  = 6
	capSysSetuid  = 7
	capSysPtrace  = 19
	soNetNSCookie = 71
)

const namespaceGateScript = `
import os, sys
def fail(stage, error):
    os.write(1, ("namespace-gate-failure=%s detail=%r\n" % (stage, str(error))).encode())
    raise SystemExit(1)
def supplementary_groups():
    with open("/proc/self/status", encoding="ascii") as stream:
        for line in stream:
            if line.startswith("Groups:"):
                return line.partition(":")[2].split()
    raise RuntimeError("Groups missing from process status")
if len(sys.argv) != 4:
    fail("arguments", "expected probe binary, delegated leaf, and A namespace")
groups = supplementary_groups()
if groups:
    fail("supplementary-groups-not-empty", "namespace probe cannot sanitize A groups without the production root launcher; count=%d" % len(groups))
try:
    os.write(3, b"E")
except OSError as error:
    fail("uid-map-ready", error)
if os.read(0, 1) != b"U":
    fail("uid-map-release", "parent did not authorize UID map")
try:
    os.write(3, b"G")
except OSError as error:
    fail("gid-map-ready", error)
if os.read(0, 1) != b"G":
    fail("gid-map-release", "parent did not authorize GID map")
os.close(3)
try:
    os.execv(sys.argv[1], [sys.argv[1], "--namespace-worker", sys.argv[2], sys.argv[3]])
except OSError as error:
    fail("worker-exec", error)
`

// Called through sudo only inside the delegated service; writes fixed B maps to an authenticated direct child.
const rootMapScript = `
import os, sys
BUID = 65534
BGID = 65534
def fail(stage, error):
    os.write(1, ("mapping-failure=%s detail=%r\n" % (stage, str(error))).encode())
    raise SystemExit(1)
def read_at(procfd, name):
    fd = os.open(name, os.O_RDONLY, dir_fd=procfd)
    try:
        return os.read(fd, 65536).decode().strip()
    finally:
        os.close(fd)
def write_at(procfd, name, value):
    fd = os.open(name, os.O_WRONLY, dir_fd=procfd)
    try:
        os.write(fd, value.encode())
    finally:
        os.close(fd)
def status(pid):
    values = {}
    with open("/proc/%d/status" % pid, encoding="ascii") as stream:
        for line in stream:
            key, separator, value = line.partition(":")
            if separator and key in ("Uid", "Gid", "PPid", "Groups"):
                values[key] = [int(item) for item in value.split()]
    if "Uid" not in values or "Gid" not in values or "PPid" not in values or "Groups" not in values:
        raise RuntimeError("incomplete process status")
    return values
def cgroup(pid):
    with open("/proc/%d/cgroup" % pid, encoding="ascii") as stream:
        for line in stream:
            fields = line.rstrip("\n").split(":", 2)
            if len(fields) == 3 and fields[0] == "0" and fields[1] == "":
                return fields[2]
    raise RuntimeError("unified cgroup membership missing")
def starttime(procfd):
    value = read_at(procfd, "stat")
    end = value.rfind(")")
    fields = value[end + 1:].split()
    if end < 0 or len(fields) <= 19:
        raise RuntimeError("invalid process stat")
    return int(fields[19])
def map_ids(fields):
    return fields == ["1", "65534", "1"]
try:
    if os.geteuid() != 0 or len(sys.argv) != 9:
        fail("arguments", "expected root and fixed mapping arguments")
    mode = sys.argv[1]
    a_pid, child_pid, ns_dev, ns_ino, child_start = [int(value) for value in sys.argv[2:7]]
    a_cgroup, leaf_cgroup = sys.argv[7:9]
    owner = int(os.environ["SUDO_UID"])
    if mode not in ("uid", "gid") or a_pid <= 1 or child_pid <= 1 or owner == BUID:
        fail("arguments", "invalid mode, PID, or fixed-B identity")
    sudo_parent_pid = os.getppid()
    if sudo_parent_pid != a_pid and status(sudo_parent_pid)["PPid"] != [a_pid]:
        fail("caller-process", "mapping helper was not invoked by A")
    procfd = os.open("/proc/%d" % child_pid, os.O_RDONLY | os.O_DIRECTORY)
    try:
        a = status(a_pid)
        child = status(child_pid)
        helper_cgroup = cgroup(os.getpid())
        if a["Uid"] != [owner] * 4 or child["Uid"] != [owner] * 4 or child["Gid"] != a["Gid"]:
            fail("caller-identity", "A and its exact child must retain their host credentials")
        if child["Groups"]:
            fail("supplementary-groups", "A must clear supplementary groups before either ID map")
        if child["PPid"] != [a_pid] or cgroup(a_pid) != a_cgroup or helper_cgroup != a_cgroup:
            fail("delegated-service-identity", "mapping helper and A must share the transient service cgroup")
        if not os.path.isabs(a_cgroup) or os.path.normpath(a_cgroup) != a_cgroup or not os.path.isabs(leaf_cgroup) or os.path.normpath(leaf_cgroup) != leaf_cgroup:
            fail("delegated-cgroup-path", "cgroup paths must be normalized absolute paths")
        if cgroup(child_pid) != leaf_cgroup or os.path.dirname(leaf_cgroup) != a_cgroup:
            fail("delegated-leaf-identity", "target must be A's direct cgroup leaf")
        if not os.path.basename(leaf_cgroup).startswith("symmetry-cgroup-probe-"):
            fail("delegated-leaf-name", "unexpected probe leaf")
        owner_namespace = os.stat("/proc/%d/ns/user" % a_pid)
        if owner_namespace.st_dev == ns_dev and owner_namespace.st_ino == ns_ino:
            fail("userns-owner", "target namespace must be a child of A's user namespace")
        namespace = os.stat("/proc/%d/ns/user" % child_pid)
        if namespace.st_dev != ns_dev or namespace.st_ino != ns_ino:
            fail("userns-identity", "target namespace identity changed")
        if starttime(procfd) != child_start:
            fail("pid-identity", "target PID was reused")
        uid_map = read_at(procfd, "uid_map").split()
        gid_map = read_at(procfd, "gid_map").split()
        setgroups = read_at(procfd, "setgroups")
        if mode == "uid":
            if uid_map or gid_map or setgroups != "allow":
                fail("uid-map-precondition", "child maps or setgroups policy changed")
            write_at(procfd, "uid_map", "1 65534 1\n")
            if not map_ids(read_at(procfd, "uid_map").split()) or read_at(procfd, "gid_map").strip():
                fail("uid-map-verification", "only fixed B UID must be mapped")
        else:
            if not map_ids(uid_map) or gid_map or setgroups != "allow":
                fail("gid-map-precondition", "B UID map or setgroups allow state missing")
            write_at(procfd, "setgroups", "deny\n")
            if read_at(procfd, "setgroups") != "deny":
                fail("setgroups-policy", "setgroups denial was not installed")
            write_at(procfd, "gid_map", "1 65534 1\n")
            if not map_ids(read_at(procfd, "gid_map").split()):
                fail("gid-map-verification", "only fixed B GID must be mapped")
        if starttime(procfd) != child_start or status(child_pid)["PPid"] != [a_pid]:
            fail("pid-identity", "target process identity changed while mapping")
        final_namespace = os.stat("/proc/%d/ns/user" % child_pid)
        if final_namespace.st_dev != ns_dev or final_namespace.st_ino != ns_ino:
            fail("userns-identity", "target namespace changed while mapping")
        print("mapping=%s-passed groups=empty" % mode, flush=True)
    finally:
        os.close(procfd)
except SystemExit:
    raise
except Exception as error:
    fail("helper-error", error)
`

// Clear A's inherited supplementary groups before it creates the child user namespace.
const rootGroupPrepScript = `
import os, stat, sys
def fail(stage, error):
    os.write(2, ("group-preparation-failure=%s detail=%r\n" % (stage, str(error))).encode())
    raise SystemExit(1)
def status(pid):
    values = {}
    with open("/proc/%d/status" % pid, encoding="ascii") as stream:
        for line in stream:
            key, separator, value = line.partition(":")
            if separator and key in ("Uid", "Gid", "PPid", "Groups"):
                values[key] = [int(item) for item in value.split()]
    if any(key not in values for key in ("Uid", "Gid", "PPid", "Groups")):
        raise RuntimeError("incomplete process status")
    return values
def capability_mask(name):
    with open("/proc/self/status", encoding="ascii") as stream:
        for line in stream:
            if line.startswith(name + ":"):
                return int(line.partition(":")[2].strip(), 16)
    raise RuntimeError(name + " missing from process status")
def cgroup(pid):
    with open("/proc/%d/cgroup" % pid, encoding="ascii") as stream:
        for line in stream:
            fields = line.rstrip("\n").split(":", 2)
            if len(fields) == 3 and fields[0] == "0" and fields[1] == "":
                return fields[2]
    raise RuntimeError("unified cgroup membership missing")
try:
    os.chdir("/")
    if os.geteuid() != 0 or len(sys.argv) != 5:
        fail("arguments", "expected root, A PID/UID/GID, and exact probe binary path")
    a_pid, a_uid, a_gid, binary_path = int(sys.argv[1]), int(sys.argv[2]), int(sys.argv[3]), sys.argv[4]
    owner = int(os.environ["SUDO_UID"])
    sudo_gid = int(os.environ["SUDO_GID"])
    sudo_pid = os.getppid()
    a = status(a_pid)
    if owner != a_uid or sudo_gid != a_gid or a["Uid"] != [a_uid] * 4 or a["Gid"] != [a_gid] * 4:
        fail("caller-identity", "A credentials do not match the authenticated sudo caller")
    if sudo_pid != a_pid:
        sudo = status(sudo_pid)
        if sudo["PPid"] != [a_pid]:
            fail("sudo-parent-identity", "root helper was not launched by A through sudo")
    service_cgroup = cgroup(a_pid)
    if cgroup(sudo_pid) != service_cgroup or cgroup(os.getpid()) != service_cgroup:
        fail("service-cgroup", "A, sudo, and root prep helper must share the transient service cgroup")
    if not os.path.isabs(binary_path) or os.path.normpath(binary_path) != binary_path or os.path.basename(binary_path) != "cgroup-v2-delegation-probe":
        fail("probe-path", "unexpected probe executable path")
    if os.readlink("/proc/%d/exe" % a_pid) != binary_path:
        fail("probe-executable-identity", "A is not running the exact probe binary path")
    binary_fd = os.open(binary_path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        binary = os.fstat(binary_fd)
        process_binary = os.stat("/proc/%d/exe" % a_pid)
        if not stat.S_ISREG(binary.st_mode) or binary.st_ino != process_binary.st_ino or binary.st_dev != process_binary.st_dev:
            fail("probe-executable-identity", "probe path does not name A's exact executable")
        if binary.st_uid != a_uid or binary.st_mode & 0o022 or binary.st_mode & 0o6000 or not binary.st_mode & 0o111:
            fail("probe-executable-ownership", "probe must be A-owned, executable, non-setid, and not group/world writable")
        groups_before = a["Groups"]
        os.setgroups([])
        if os.getgroups():
            fail("supplementary-groups", "root could not clear A's inherited groups (count=%d)" % len(groups_before))
        os.setresgid(a_gid, a_gid, a_gid)
        os.setresuid(a_uid, a_uid, a_uid)
        dropped = status(os.getpid())
        if (dropped["Uid"] != [a_uid] * 4 or dropped["Gid"] != [a_gid] * 4 or
            dropped["Groups"] or os.getgroups() or
            any(capability_mask(name) != 0 for name in ("CapEff", "CapPrm", "CapAmb"))):
            fail("credential-drop", "root prep did not fully drop to group-free A")
        print("group-preparation=passed initial_supplementary_group_count=%d" % len(groups_before), flush=True)
        if cgroup(os.getpid()) != service_cgroup:
            fail("service-cgroup-after-drop", "prepared probe left the transient service cgroup")
        os.execve(binary_fd, [binary_path, "--prepared"], {"PATH": "/usr/bin:/bin", "LANG": "C", "LC_ALL": "C"})
    finally:
        os.close(binary_fd)
except SystemExit:
    raise
except Exception as error:
    fail("helper-error", error)
`

type report struct {
	uid                int
	childUID           int
	bUID               int
	kernel             string
	cgroup             string
	leafCgroup         string
	capability         string
	crossUIDCapability string
	suidDumpable       string
	usernsEnabled      bool
	usernsCreated      bool
	hostCapEff         string
	failureDetail      string
}

func membership() (string, error) {
	file, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), ":", 3)
		if len(fields) == 3 && fields[0] == "0" && fields[1] == "" &&
			strings.HasPrefix(fields[2], "/") && filepath.Clean(fields[2]) == fields[2] {
			return fields[2], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("unified cgroup membership missing")
}

func unescapeMountField(value string) (string, error) {
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			result.WriteByte(value[i])
			continue
		}
		if i+3 >= len(value) {
			return "", fmt.Errorf("invalid mountinfo escape")
		}
		code, err := strconv.ParseUint(value[i+1:i+4], 8, 8)
		if err != nil {
			return "", err
		}
		result.WriteByte(byte(code))
		i += 3
	}
	return result.String(), nil
}

func cgroupDirectory(cgroup string) (string, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	bestRoot := ""
	bestDirectory := ""
	for scanner.Scan() {
		line := scanner.Text()
		separator := strings.Index(line, " - ")
		if separator < 0 {
			continue
		}
		before := strings.Fields(line[:separator])
		after := strings.Fields(line[separator+3:])
		if len(before) < 5 || len(after) < 1 || after[0] != "cgroup2" {
			continue
		}
		root, err := unescapeMountField(before[3])
		if err != nil {
			return "", err
		}
		mountpoint, err := unescapeMountField(before[4])
		if err != nil {
			return "", err
		}
		matches := cgroup == root || root == "/" ||
			(strings.HasPrefix(cgroup, strings.TrimSuffix(root, "/")) &&
				strings.HasPrefix(strings.TrimPrefix(cgroup, strings.TrimSuffix(root, "/")), "/"))
		if !matches || len(root) < len(bestRoot) {
			continue
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(cgroup, root), "/")
		bestRoot = root
		bestDirectory = filepath.Join(mountpoint, filepath.FromSlash(relative))
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if bestDirectory == "" {
		return "", fmt.Errorf("cgroup2 mount not found")
	}
	return bestDirectory, nil
}

func child() int {
	cgroup, err := membership()
	if err != nil {
		return 1
	}
	fmt.Printf("uid=%d\ncgroup_path=%s\n", os.Geteuid(), cgroup)
	return 0
}

func membershipFor(pid int) (string, error) {
	file, err := os.Open(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), ":", 3)
		if len(fields) == 3 && fields[0] == "0" && fields[1] == "" &&
			strings.HasPrefix(fields[2], "/") && filepath.Clean(fields[2]) == fields[2] {
			return fields[2], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("unified cgroup membership missing")
}

func statusValue(pid int, name string) (string, error) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return "", err
	}
	prefix := name + ":"
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix)), nil
		}
	}
	return "", fmt.Errorf("%s missing from process status", name)
}

func statusIDs(pid int, name string) ([]int, error) {
	value, err := statusValue(pid, name)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(value)
	if len(fields) != 4 {
		return nil, fmt.Errorf("%s did not contain four IDs", name)
	}
	ids := make([]int, len(fields))
	for i, field := range fields {
		ids[i], err = strconv.Atoi(field)
		if err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func permissionDenied(err error) bool {
	return err != nil && (errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM))
}

func recordCleanupFailure(failure *string, detail *string, stage string, err error) {
	message := stage + ": " + err.Error()
	if *failure == "" {
		*failure = stage
		*detail = err.Error()
		return
	}
	if *detail == "" {
		*detail = message
	} else {
		*detail += "; " + message
	}
}

func usernsCheckChild() int {
	namespace, err := os.Readlink("/proc/self/ns/user")
	if err != nil {
		fmt.Printf("userns-check-failure=%q\n", err.Error())
		return 1
	}
	fmt.Printf("userns=%s\n", namespace)
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		fmt.Printf("userns-check-failure=%q\n", err.Error())
		return 1
	}
	return 0
}

func checkUsernsCreate() error {
	parentNamespace, err := os.Readlink("/proc/self/ns/user")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/proc/self/exe", "--userns-check")
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWUSER}
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	input, err := command.StdinPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	waited := false
	defer func() {
		_ = input.Close()
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil {
		return err
	}
	values := parseValues(line)
	childNamespace := values["userns"]
	if childNamespace == "" || childNamespace == parentNamespace {
		return fmt.Errorf("child did not enter a distinct user namespace: %s", strings.TrimSpace(line))
	}
	if _, err := input.Write([]byte{'x'}); err != nil {
		return err
	}
	if err := input.Close(); err != nil {
		return err
	}
	if err := command.Wait(); err != nil {
		waited = true
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	waited = true
	return nil
}

func checkHostPrerequisites(result *report) string {
	dumpable, err := os.ReadFile("/proc/sys/fs/suid_dumpable")
	if err != nil {
		result.suidDumpable = "unavailable"
	} else {
		result.suidDumpable = strings.TrimSpace(string(dumpable))
	}

	maxNamespaces, err := os.ReadFile("/proc/sys/user/max_user_namespaces")
	if err != nil {
		result.failureDetail = err.Error()
		return "read-user-max-user-namespaces"
	}
	maxCount, err := strconv.ParseUint(strings.TrimSpace(string(maxNamespaces)), 10, 64)
	if err != nil || maxCount == 0 {
		result.failureDetail = "user.max_user_namespaces must be positive"
		return "user-namespaces-disabled"
	}
	if unprivilegedClone, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone"); err == nil {
		if strings.TrimSpace(string(unprivilegedClone)) != "1" {
			result.failureDetail = "kernel.unprivileged_userns_clone must be 1"
			return "unprivileged-user-namespaces-disabled"
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		result.failureDetail = err.Error()
		return "read-unprivileged-user-namespaces"
	}
	result.usernsEnabled = true

	capEff, err := statusValue(os.Getpid(), "CapEff")
	if err != nil {
		result.failureDetail = err.Error()
		return "read-host-cap-eff"
	}
	result.hostCapEff = capEff
	capMask, err := strconv.ParseUint(capEff, 16, 64)
	if err != nil || capMask != 0 {
		result.failureDetail = "the host runner process must have CapEff=0"
		return "host-capabilities-nonzero"
	}
	hostUIDs, uidErr := statusIDs(os.Getpid(), "Uid")
	hostGIDs, gidErr := statusIDs(os.Getpid(), "Gid")
	if uidErr != nil || gidErr != nil || len(hostUIDs) != 4 || len(hostGIDs) != 4 {
		result.failureDetail = "could not verify A's host UID/GID tuple"
		return "host-id-tuple"
	}
	for i := range hostUIDs {
		if hostUIDs[i] == probeBUID || hostGIDs[i] == probeBGID {
			result.failureDetail = "fixed B UID/GID must differ from every A UID/GID"
			return "fixed-b-not-distinct"
		}
	}
	groups, err := statusValue(os.Getpid(), "Groups")
	if err != nil {
		result.failureDetail = err.Error()
		return "read-host-supplementary-groups"
	}
	groupCount := len(strings.Fields(groups))
	if groupCount != 0 {
		result.failureDetail = fmt.Sprintf("A has %d supplementary groups; this probe cannot sanitize them without the production root launcher", groupCount)
		return "host-supplementary-groups-not-empty"
	}
	if err := checkUsernsCreate(); err != nil {
		result.failureDetail = err.Error()
		return "user-namespace-create-denied"
	}
	result.usernsCreated = true
	return ""
}

func exactIDMap(pid int, name string, expectedID int) error {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", pid, name))
	if err != nil {
		return err
	}
	fields := strings.Fields(string(contents))
	expected := []string{"1", strconv.Itoa(expectedID), "1"}
	if len(fields) != len(expected) {
		return fmt.Errorf("%s was not a single-ID B-only mapping", name)
	}
	for i := range expected {
		if fields[i] != expected[i] {
			return fmt.Errorf("%s was not the required B-only mapping", name)
		}
	}
	return nil
}

func namespaceIdentity(pid int) (uint64, uint64, error) {
	info, err := os.Stat(fmt.Sprintf("/proc/%d/ns/user", pid))
	if err != nil {
		return 0, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("user namespace stat unavailable")
	}
	return uint64(stat.Dev), stat.Ino, nil
}

func processStartTime(pid int) (uint64, error) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	end := strings.LastIndex(string(contents), ")")
	if end < 0 {
		return 0, fmt.Errorf("process stat command field malformed")
	}
	fields := strings.Fields(string(contents)[end+1:])
	if len(fields) <= 19 {
		return 0, fmt.Errorf("process start time missing")
	}
	return strconv.ParseUint(fields[19], 10, 64)
}

func trustedRootExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not a trusted root-owned executable", path)
	}
	return nil
}

type boundedOutput struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (output *boundedOutput) Write(value []byte) (int, error) {
	length := len(value)
	available := output.limit - output.Len()
	if available <= 0 {
		output.truncated = length != 0 || output.truncated
		return length, nil
	}
	if length > available {
		output.truncated = true
		value = value[:available]
	}
	_, err := output.Buffer.Write(value)
	return length, err
}

func hasPreparedSuccessMarkers(output string) bool {
	expected := map[string]string{
		"group-preparation":    "passed",
		"capability":           "clone3-cgroupfd-pidfd",
		"cross_uid_capability": "fixed-b-child-userns-fdinfo-pidfd-getfd",
		"host_a_pidfd_getfd":   "passed",
		"status":               "passed",
	}
	counts := make(map[string]int, len(expected))
	matched := make(map[string]int, len(expected))
	for _, line := range strings.Split(output, "\n") {
		for _, field := range strings.Fields(line) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			if want, required := expected[key]; required {
				counts[key]++
				if value == want {
					matched[key]++
				}
			}
		}
	}
	for key := range expected {
		if counts[key] != 1 || matched[key] != 1 {
			return false
		}
	}
	return true
}

func runRootMapping(mode string, aPID int, childPID int, nsDev uint64, nsIno uint64,
	childStart uint64, aCgroup string, leafCgroup string) error {
	for _, path := range []string{"/usr/bin/sudo", "/usr/bin/python3"} {
		if err := trustedRootExecutable(path); err != nil {
			return err
		}
	}
	args := []string{
		"-n", "--", "/usr/bin/python3", "-I", "-S", "-c", rootMapScript,
		mode, strconv.Itoa(aPID), strconv.Itoa(childPID), strconv.FormatUint(nsDev, 10),
		strconv.FormatUint(nsIno, 10), strconv.FormatUint(childStart, 10), aCgroup, leafCgroup,
	}
	command := exec.Command("/usr/bin/sudo", args...)
	command.Dir = "/"
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("transient %s map helper failed: %w: %s %s", mode, err,
			strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()))
	}
	if !strings.Contains(stdout.String(), "mapping="+mode+"-passed") {
		return fmt.Errorf("transient %s map helper did not confirm the fixed B map: %s", mode, strings.TrimSpace(stdout.String()))
	}
	fmt.Println(strings.TrimSpace(stdout.String()))
	return nil
}

func runWithRootPreparedGroups() int {
	binaryPath, err := os.Executable()
	if err != nil {
		fmt.Printf("status=failed stage=group-preparation-path detail=%q\n", err.Error())
		return 1
	}
	for _, path := range []string{"/usr/bin/sudo", "/usr/bin/python3"} {
		if err := trustedRootExecutable(path); err != nil {
			fmt.Printf("status=failed stage=group-preparation-prerequisite detail=%q\n", err.Error())
			return 1
		}
	}
	args := []string{
		"-n", "--", "/usr/bin/python3", "-I", "-S", "-c", rootGroupPrepScript,
		strconv.Itoa(os.Getpid()), strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid()), binaryPath,
	}
	command := exec.Command("/usr/bin/sudo", args...)
	command.Dir = "/"
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	stdout := boundedOutput{limit: 64 * 1024}
	stderr := boundedOutput{limit: 64 * 1024}
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	_, _ = os.Stdout.Write(stdout.Bytes())
	_, _ = os.Stderr.Write(stderr.Bytes())
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() >= 0 {
			return exitError.ExitCode()
		}
		return 1
	}
	if stdout.truncated || stderr.truncated || !hasPreparedSuccessMarkers(stdout.String()) {
		fmt.Println("status=failed stage=prepared-probe-success-markers")
		return 1
	}
	return 0
}

func parseValues(line string) map[string]string {
	values := make(map[string]string)
	for _, field := range strings.Fields(line) {
		key, value, ok := strings.Cut(field, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func procSocketInode(pid int, fd int) (uint64, error) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/fdinfo/%d", pid, fd))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "ino:" {
			infoInode, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return infoInode, nil
		}
	}
	return 0, fmt.Errorf("inode missing from fdinfo")
}

func tcpFD(conn *net.TCPConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return -1, err
	}
	fd := -1
	if err := raw.Control(func(value uintptr) { fd = int(value) }); err != nil {
		return -1, err
	}
	if fd < 0 {
		return -1, fmt.Errorf("TCP socket descriptor missing")
	}
	return fd, nil
}

func socketNetnsCookie(fd int) (uint64, error) {
	var cookie uint64
	length := uint32(8)
	_, _, errno := syscall.Syscall6(syscall.SYS_GETSOCKOPT,
		uintptr(fd), uintptr(syscall.SOL_SOCKET), uintptr(soNetNSCookie),
		uintptr(unsafe.Pointer(&cookie)), uintptr(unsafe.Pointer(&length)), 0)
	runtime.KeepAlive(&cookie)
	runtime.KeepAlive(&length)
	if errno != 0 {
		return 0, errno
	}
	if length != 8 {
		return 0, fmt.Errorf("SO_NETNS_COOKIE returned %d bytes", length)
	}
	return cookie, nil
}

func sameTCPAddr(left net.Addr, right net.Addr) bool {
	a, okA := left.(*net.TCPAddr)
	b, okB := right.(*net.TCPAddr)
	return okA && okB && a.Port == b.Port && bytes.Equal(a.IP.To4(), b.IP.To4())
}

func verifySocketDuplicate(fd int, inode uint64, expectedLocal net.Addr,
	expectedRemote net.Addr, expectedNetnsCookie uint64) (uint64, error) {
	duplicateFile := os.NewFile(uintptr(fd), "pidfd-getfd-socket")
	if duplicateFile == nil {
		_ = syscall.Close(fd)
		return 0, fmt.Errorf("duplicated descriptor was invalid")
	}
	defer duplicateFile.Close()
	var duplicateStat syscall.Stat_t
	if err := syscall.Fstat(fd, &duplicateStat); err != nil || uint64(duplicateStat.Ino) != inode {
		return 0, fmt.Errorf("pidfd duplicate did not match fdinfo inode %d: %v", inode, err)
	}
	socketType, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || socketType != unix.SOCK_STREAM {
		return 0, fmt.Errorf("expected SOCK_STREAM: %v", err)
	}
	tcpInfo, err := unix.GetsockoptTCPInfo(fd, unix.IPPROTO_TCP, unix.TCP_INFO)
	if err != nil || tcpInfo.State != 1 {
		return 0, fmt.Errorf("expected TCP_ESTABLISHED: %v", err)
	}
	connection, err := net.FileConn(duplicateFile)
	if err != nil {
		return 0, err
	}
	defer connection.Close()
	local, okLocal := connection.LocalAddr().(*net.TCPAddr)
	remote, okRemote := connection.RemoteAddr().(*net.TCPAddr)
	if !okLocal || !okRemote || !local.IP.IsLoopback() || !remote.IP.IsLoopback() ||
		local.Port == 0 || remote.Port == 0 || !sameTCPAddr(local, expectedLocal) ||
		!sameTCPAddr(remote, expectedRemote) {
		return 0, fmt.Errorf("duplicated B socket tuple did not match the live loopback connection")
	}
	netnsCookie, err := socketNetnsCookie(fd)
	if err != nil || netnsCookie == 0 || netnsCookie != expectedNetnsCookie {
		return 0, fmt.Errorf("duplicated B socket network namespace cookie mismatch: %v", err)
	}
	return netnsCookie, nil
}

func probeLeafWriteDenied(leafDirectory string) error {
	file, err := os.OpenFile(filepath.Join(leafDirectory, "cgroup.procs"), os.O_WRONLY, 0)
	if err != nil {
		if permissionDenied(err) {
			return nil
		}
		return fmt.Errorf("opening A's cgroup.procs did not return EACCES/EPERM: %w", err)
	}
	defer file.Close()
	_, err = file.Write([]byte("not-a-pid\n"))
	if permissionDenied(err) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("B could write A's cgroup.procs")
	}
	return fmt.Errorf("writing A's cgroup.procs returned %v, not EACCES/EPERM", err)
}

func peerFailure(stage string, err error) int {
	fmt.Printf("peer-failure=%s detail=%q\n", stage, err.Error())
	return 1
}

func socketPeer() int {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return peerFailure("set-nondumpable", err)
	}
	if os.Getuid() != 1 || os.Getgid() != 1 {
		return peerFailure("fixed-b-namespace-credentials", fmt.Errorf("expected namespace uid/gid 1"))
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return peerFailure("loopback-listen", err)
	}
	defer listener.Close()
	fmt.Printf("port=%d\n", listener.Addr().(*net.TCPAddr).Port)
	connection, err := listener.AcceptTCP()
	if err != nil {
		return peerFailure("loopback-accept", err)
	}
	defer connection.Close()
	fd, err := tcpFD(connection)
	if err != nil {
		return peerFailure("socket-fd", err)
	}
	if err := probeLeafWriteDenied(os.Args[2]); err != nil {
		return peerFailure("b-cgroup-write-permission", err)
	}
	fmt.Printf("socket-ready fd=%d uid=%d gid=%d leaf_write=denied\n", fd, os.Getuid(), os.Getgid())
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		return peerFailure("socket-peer-wait", err)
	}
	return 0
}

func namespaceLaunchCapabilities() string {
	fields := []string{"CapEff", "CapPrm", "CapAmb", "NoNewPrivs", "Seccomp"}
	var values []string
	for _, field := range fields {
		value, err := statusValue(os.Getpid(), field)
		if err != nil {
			values = append(values, field+"=unavailable")
			continue
		}
		values = append(values, field+"="+strings.ReplaceAll(value, " ", "_"))
		if field == "CapEff" || field == "CapPrm" || field == "CapAmb" {
			mask, err := strconv.ParseUint(value, 16, 64)
			if err != nil {
				values = append(values, field+"_parse=failed")
				continue
			}
			for _, capability := range []struct {
				name string
				bit  uint
			}{{"setgid", 6}, {"setuid", 7}, {"ptrace", 19}} {
				values = append(values, field+"_"+capability.name+"="+strconv.FormatBool(mask&(1<<capability.bit) != 0))
			}
		}
	}
	return strings.Join(values, " ")
}

func namespaceWorker(leafDirectory string, parentUserNS string, input *bufio.Reader) int {
	currentUserNS, err := os.Readlink("/proc/self/ns/user")
	if err != nil {
		return peerFailure("read-worker-userns", err)
	}
	if currentUserNS == parentUserNS {
		return peerFailure("worker-userns-not-child", fmt.Errorf("worker remained in A's parent user namespace"))
	}

	peer := exec.Command("/proc/self/exe", "--socket-peer", leafDirectory)
	peer.Dir = "/"
	peer.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	peerPIDFD := -1
	peer.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 1, Gid: 1, NoSetGroups: true},
		Pdeathsig:  syscall.SIGKILL,
		PidFD:      &peerPIDFD,
	}
	peerOutput, err := peer.StdoutPipe()
	if err != nil {
		return peerFailure("peer-stdout", err)
	}
	peerInput, err := peer.StdinPipe()
	if err != nil {
		return peerFailure("peer-stdin", err)
	}
	peer.Stderr = io.Discard
	if err := peer.Start(); err != nil {
		fmt.Printf("peer-start-diagnostic error=%q %s\n", err.Error(), namespaceLaunchCapabilities())
		if peerPIDFD >= 0 {
			_ = syscall.Close(peerPIDFD)
		}
		return peerFailure("cross-uid-b-launch", err)
	}
	if peerPIDFD < 0 {
		_ = peer.Process.Kill()
		_ = peer.Wait()
		return peerFailure("cross-uid-b-pidfd", fmt.Errorf("B launch did not return a pidfd"))
	}
	defer unix.Close(peerPIDFD)
	defer func() {
		_ = peerInput.Close()
		_ = peer.Process.Kill()
		_ = peer.Wait()
	}()

	peerReader := bufio.NewReader(peerOutput)
	portLine, err := peerReader.ReadString('\n')
	if err != nil {
		return peerFailure("loopback-listener-ready", err)
	}
	portValues := parseValues(portLine)
	port, err := strconv.Atoi(portValues["port"])
	if err != nil || port < 1 || port > 65535 {
		return peerFailure("loopback-listener-port", fmt.Errorf("invalid listener port"))
	}
	connection, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		return peerFailure("loopback-connect", err)
	}
	defer connection.Close()
	client, ok := connection.(*net.TCPConn)
	if !ok {
		return peerFailure("loopback-client-type", fmt.Errorf("connection was not TCP"))
	}

	peerLine, err := peerReader.ReadString('\n')
	if err != nil {
		return peerFailure("b-socket-ready", err)
	}
	peerValues := parseValues(peerLine)
	if peerValues["leaf_write"] != "denied" || peerValues["uid"] != "1" || peerValues["gid"] != "1" {
		return peerFailure("b-cgroup-write-permission", fmt.Errorf("B identity or denied leaf write was not proved"))
	}
	fd, err := strconv.Atoi(peerValues["fd"])
	if err != nil || fd < 0 {
		return peerFailure("b-socket-fd", fmt.Errorf("invalid B socket descriptor"))
	}
	clientLocal := client.LocalAddr().(*net.TCPAddr)
	clientRemote := client.RemoteAddr().(*net.TCPAddr)
	if !clientLocal.IP.IsLoopback() || !clientRemote.IP.IsLoopback() ||
		clientLocal.Port == 0 || clientRemote.Port == 0 {
		return peerFailure("loopback-client-tuple", fmt.Errorf("client was not an established loopback TCP connection"))
	}
	clientFD, err := tcpFD(client)
	if err != nil {
		return peerFailure("client-socket-fd", err)
	}
	clientNetnsCookie, err := socketNetnsCookie(clientFD)
	if err != nil || clientNetnsCookie == 0 {
		return peerFailure("client-socket-netns-cookie", fmt.Errorf("loopback client lacked a network namespace cookie: %v", err))
	}
	peerStart, err := processStartTime(peer.Process.Pid)
	if err != nil {
		return peerFailure("b-process-start-time", err)
	}
	fmt.Printf("peer pid=%d start=%d fd=%d leaf_write=denied client_local_ip=%s client_local_port=%d client_remote_ip=%s client_remote_port=%d client_netns_cookie=%d\n",
		peer.Process.Pid, peerStart, fd, clientLocal.IP.String(), clientLocal.Port,
		clientRemote.IP.String(), clientRemote.Port, clientNetnsCookie)

	signal, err := input.ReadByte()
	if err != nil || signal != 'P' {
		return peerFailure("host-a-cross-uid-check", fmt.Errorf("host A did not authorize namespace proof"))
	}

	inode, err := procSocketInode(peer.Process.Pid, fd)
	if err != nil {
		return peerFailure("namespace-fdinfo-permission", err)
	}
	duplicateFD, err := unix.PidfdGetfd(peerPIDFD, fd, 0)
	if err != nil {
		return peerFailure("namespace-pidfd-getfd-permission", err)
	}
	netnsCookie, err := verifySocketDuplicate(duplicateFD, inode, clientRemote, clientLocal, clientNetnsCookie)
	if err != nil {
		return peerFailure("namespace-duplicated-socket", err)
	}
	fmt.Printf("namespace-proof=passed fdinfo_inode=%d tuple=%s:%d<->%s:%d tcp_state=ESTABLISHED netns_cookie=%d pidfd_getfd=passed\n",
		inode, clientRemote.IP.String(), clientRemote.Port, clientLocal.IP.String(), clientLocal.Port, netnsCookie)
	return 0
}

func leafIsEmpty(leafDirectory string) (bool, error) {
	events, err := os.ReadFile(filepath.Join(leafDirectory, "cgroup.events"))
	if err != nil {
		return false, err
	}
	fields := strings.Fields(string(events))
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "populated" {
			return fields[i+1] == "0", nil
		}
	}
	return false, fmt.Errorf("populated entry missing from cgroup.events")
}

func waitForLeafEmpty(leafDirectory string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		empty, err := leafIsEmpty(leafDirectory)
		if err != nil {
			return err
		}
		if empty {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("delegated leaf remained populated after cross-UID cleanup")
}

func runCrossUIDProbe(result *report, leafFD *os.File, leafDirectory string) (failure string, detail string) {
	leafInfo, err := os.Stat(leafDirectory)
	if err != nil {
		return "cross-uid-leaf-stat", err.Error()
	}
	leafStat, ok := leafInfo.Sys().(*syscall.Stat_t)
	if !ok || int(leafStat.Uid) != result.uid {
		return "cross-uid-leaf-not-owned-by-a", "delegated probe leaf must be owned by A"
	}
	procsInfo, err := os.Stat(filepath.Join(leafDirectory, "cgroup.procs"))
	if err != nil {
		return "cross-uid-leaf-procs-stat", err.Error()
	}
	procsStat, ok := procsInfo.Sys().(*syscall.Stat_t)
	if !ok || int(procsStat.Uid) != result.uid {
		return "cross-uid-leaf-procs-not-owned-by-a", "delegated cgroup.procs must be owned by A"
	}
	parentUserNS, err := os.Readlink("/proc/self/ns/user")
	if err != nil {
		return "parent-userns-read", err.Error()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		return "cross-uid-executable-path", err.Error()
	}
	if err := trustedRootExecutable("/usr/bin/python3"); err != nil {
		return "cross-uid-python-prerequisite", err.Error()
	}
	command := exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-S", "-c",
		namespaceGateScript, executable, leafDirectory, parentUserNS)
	command.Dir = "/"
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stderr = io.Discard
	pidFD := -1
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:  syscall.CLONE_NEWUSER,
		UseCgroupFD: true,
		CgroupFD:    int(leafFD.Fd()),
		PidFD:       &pidFD,
		AmbientCaps: []uintptr{capSysSetgid, capSysSetuid, capSysPtrace},
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		return "userns-ready-pipe", err.Error()
	}
	defer readyRead.Close()
	command.ExtraFiles = []*os.File{readyWrite}
	input, err := command.StdinPipe()
	if err != nil {
		_ = readyWrite.Close()
		return "userns-stdin", err.Error()
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = readyWrite.Close()
		_ = input.Close()
		return "userns-stdout", err.Error()
	}
	if err := command.Start(); err != nil {
		_ = readyWrite.Close()
		_ = input.Close()
		return "cross-uid-userns-create", err.Error()
	}
	waited := false
	defer func() {
		_ = input.Close()
		_ = readyWrite.Close()
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
			waited = true
		}
		if err := waitForLeafEmpty(leafDirectory); err != nil {
			recordCleanupFailure(&failure, &detail, "cross-uid-child-cleanup", err)
		}
	}()
	_ = readyWrite.Close()
	pid := command.Process.Pid
	if pidFD < 0 {
		return "cross-uid-userns-pidfd", "clone3 did not return a pidfd"
	}
	defer syscall.Close(pidFD)
	ready := make([]byte, 1)
	if _, err := io.ReadFull(readyRead, ready); err != nil || ready[0] != 'E' {
		diagnostic, _ := io.ReadAll(output)
		return "cross-uid-userns-create", fmt.Sprintf("child user namespace unavailable: %v: %s", err, strings.TrimSpace(string(diagnostic)))
	}
	fmt.Println("namespace_gate_groups=empty")
	nsDev, nsIno, err := namespaceIdentity(pid)
	if err != nil {
		return "cross-uid-userns-identity", err.Error()
	}
	childStart, err := processStartTime(pid)
	if err != nil {
		return "cross-uid-child-identity", err.Error()
	}
	if err := runRootMapping("uid", os.Getpid(), pid, nsDev, nsIno, childStart,
		result.cgroup, result.leafCgroup); err != nil {
		return "cross-uid-fixed-b-uid-map", err.Error()
	}
	if err := exactIDMap(pid, "uid_map", probeBUID); err != nil {
		return "cross-uid-fixed-b-uid-map", err.Error()
	}
	if _, err := input.Write([]byte{'U'}); err != nil {
		return "cross-uid-clear-groups", err.Error()
	}
	if _, err := io.ReadFull(readyRead, ready); err != nil || ready[0] != 'G' {
		diagnostic, _ := io.ReadAll(output)
		return "cross-uid-gid-map-ready", fmt.Sprintf("namespace gate did not reach the GID map boundary: %v: %s", err, strings.TrimSpace(string(diagnostic)))
	}
	if err := runRootMapping("gid", os.Getpid(), pid, nsDev, nsIno, childStart,
		result.cgroup, result.leafCgroup); err != nil {
		return "cross-uid-fixed-b-gid-map", err.Error()
	}
	if err := exactIDMap(pid, "gid_map", probeBGID); err != nil {
		return "cross-uid-fixed-b-gid-map", err.Error()
	}
	setgroups, err := os.ReadFile(fmt.Sprintf("/proc/%d/setgroups", pid))
	if err != nil || strings.TrimSpace(string(setgroups)) != "deny" {
		return "cross-uid-setgroups-policy", "setgroups policy was not deny after the B-only GID map"
	}
	fmt.Println("userns_uid_map=1,65534,1")
	fmt.Println("userns_gid_map=1,65534,1")
	fmt.Println("userns_setgroups=deny")
	fmt.Println("root_mapping=transient-helper-in-delegated-unit")
	if _, err := input.Write([]byte{'G'}); err != nil {
		return "cross-uid-userns-release", err.Error()
	}

	parentNamespace, err := os.Readlink("/proc/self/ns/user")
	if err != nil {
		return "parent-userns-read", err.Error()
	}
	childNamespace, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/user", pid))
	if err != nil || childNamespace == parentNamespace {
		return "cross-uid-userns-not-child", "A-owned child user namespace was not observed"
	}
	ownerIDs, err := statusIDs(pid, "Uid")
	if err != nil || len(ownerIDs) != 4 || ownerIDs[0] != result.uid || ownerIDs[1] != result.uid ||
		ownerIDs[2] != result.uid || ownerIDs[3] != result.uid {
		return "cross-uid-userns-owner", "user namespace worker did not retain A's host UID"
	}
	parentGIDs, err := statusIDs(os.Getpid(), "Gid")
	workerGIDs, workerGIDErr := statusIDs(pid, "Gid")
	if err != nil || workerGIDErr != nil || len(parentGIDs) != 4 || len(workerGIDs) != 4 {
		return "cross-uid-userns-owner-gid", "could not verify A's host GID in the namespace worker"
	}
	for i := range parentGIDs {
		if workerGIDs[i] != parentGIDs[i] {
			return "cross-uid-userns-owner-gid", "user namespace worker did not retain A's host GID"
		}
	}
	groups, err := statusValue(pid, "Groups")
	if err != nil || strings.TrimSpace(groups) != "" {
		return "cross-uid-groups-not-cleared", "A-owned namespace worker retained supplementary groups"
	}
	workerCaps, err := statusValue(pid, "CapEff")
	workerCapMask, capErr := strconv.ParseUint(workerCaps, 16, 64)
	if err != nil || capErr != nil || workerCapMask&(1<<capSysPtrace) == 0 {
		return "namespace-cap-sys-ptrace-unavailable", "A-owned child namespace lacks CAP_SYS_PTRACE"
	}
	if result.hostCapEff != "0000000000000000" {
		return "host-capabilities-changed", "A must retain host CapEff=0"
	}
	hostCapEff, err := statusValue(os.Getpid(), "CapEff")
	if err != nil || strings.TrimSpace(hostCapEff) != "0000000000000000" {
		return "host-capabilities-changed", "A must retain host CapEff=0"
	}

	reader := bufio.NewReader(output)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "cross-uid-b-launch", fmt.Sprintf("B socket peer did not become ready: %v", err)
	}
	values := parseValues(line)
	if !strings.HasPrefix(line, "peer ") {
		return "cross-uid-b-launch", strings.TrimSpace(line)
	}
	peerPID, err := strconv.Atoi(values["pid"])
	if err != nil || peerPID <= 0 {
		return "cross-uid-b-launch", "invalid B PID"
	}
	peerStart, err := strconv.ParseUint(values["start"], 10, 64)
	if err != nil || peerStart == 0 {
		return "cross-uid-b-start-time", "invalid B start time"
	}
	peerFD, err := strconv.Atoi(values["fd"])
	if err != nil || peerFD < 0 {
		return "cross-uid-b-socket-fd", "invalid B socket FD"
	}
	clientLocalIP := net.ParseIP(values["client_local_ip"])
	clientLocalPort, localPortErr := strconv.Atoi(values["client_local_port"])
	clientRemoteIP := net.ParseIP(values["client_remote_ip"])
	clientRemotePort, remotePortErr := strconv.Atoi(values["client_remote_port"])
	clientNetnsCookie, cookieErr := strconv.ParseUint(values["client_netns_cookie"], 10, 64)
	if localPortErr != nil || remotePortErr != nil || cookieErr != nil ||
		clientLocalIP == nil || clientRemoteIP == nil || !clientLocalIP.IsLoopback() ||
		!clientRemoteIP.IsLoopback() || clientLocalPort < 1 || clientRemotePort < 1 || clientNetnsCookie == 0 {
		return "cross-uid-loopback-metadata", "B peer did not forward a valid live loopback tuple and network namespace cookie"
	}
	expectedLocal := &net.TCPAddr{IP: clientRemoteIP, Port: clientRemotePort}
	expectedRemote := &net.TCPAddr{IP: clientLocalIP, Port: clientLocalPort}
	hostPeerFD, err := unix.PidfdOpen(peerPID, 0)
	if err != nil {
		return "host-a-pidfd-open", err.Error()
	}
	defer unix.Close(hostPeerFD)
	currentStart, err := processStartTime(peerPID)
	if err != nil || currentStart != peerStart {
		return "host-a-b-pid-identity", "B PID/start-time changed before host-A pidfd acquisition"
	}
	peerParent, err := statusValue(peerPID, "PPid")
	parentPID, parentErr := strconv.Atoi(peerParent)
	if err != nil || parentErr != nil || parentPID != pid {
		return "host-a-b-parent-identity", "B was not the exact child of A's namespace worker"
	}
	if values["leaf_write"] != "denied" {
		return "cross-uid-b-cgroup-write", "B did not prove denial on A's cgroup.procs"
	}
	peerUIDs, err := statusIDs(peerPID, "Uid")
	if err != nil || len(peerUIDs) != 4 || peerUIDs[0] != probeBUID || peerUIDs[1] != probeBUID ||
		peerUIDs[2] != probeBUID || peerUIDs[3] != probeBUID {
		return "cross-uid-b-host-uid", "B process did not run with the fixed host UID"
	}
	peerGIDs, err := statusIDs(peerPID, "Gid")
	if err != nil || len(peerGIDs) != 4 || peerGIDs[0] != probeBGID || peerGIDs[1] != probeBGID ||
		peerGIDs[2] != probeBGID || peerGIDs[3] != probeBGID {
		return "cross-uid-b-host-gid", "B process did not run with the fixed host GID"
	}
	peerGroups, err := statusValue(peerPID, "Groups")
	if err != nil || strings.TrimSpace(peerGroups) != "" {
		return "cross-uid-b-groups-not-empty", "B retained supplementary groups"
	}
	peerCapEff, err := statusValue(peerPID, "CapEff")
	if err != nil || strings.TrimSpace(peerCapEff) != "0000000000000000" {
		return "cross-uid-b-capabilities-nonzero", "B must have CapEff=0"
	}
	peerUserNS, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/user", peerPID))
	if err != nil || peerUserNS != childNamespace {
		return "cross-uid-b-userns", "B was not launched in A's child user namespace"
	}
	peerCgroup, err := membershipFor(peerPID)
	if err != nil || peerCgroup != result.leafCgroup {
		return "cross-uid-b-cgroup", "B was not launched in A's exact delegated leaf"
	}

	hostCapEff, err = statusValue(os.Getpid(), "CapEff")
	if err != nil || strings.TrimSpace(hostCapEff) != "0000000000000000" {
		return "host-a-capabilities-nonzero", "host A must retain CapEff=0 for its positive B socket proof"
	}
	inode, err := procSocketInode(peerPID, peerFD)
	if err != nil {
		return "host-a-fdinfo-permission", err.Error()
	}
	hostDuplicate, err := unix.PidfdGetfd(hostPeerFD, peerFD, 0)
	if err != nil {
		return "host-a-pidfd-getfd-permission", err.Error()
	}
	hostNetnsCookie, err := verifySocketDuplicate(hostDuplicate, inode, expectedLocal, expectedRemote, clientNetnsCookie)
	if err != nil {
		return "host-a-duplicated-socket", err.Error()
	}
	fmt.Printf("host_a_pidfd_getfd=passed b_pid=%d b_start=%d fdinfo_inode=%d netns_cookie=%d host_cap_eff=0\n",
		peerPID, peerStart, inode, hostNetnsCookie)

	if _, err := input.Write([]byte{'P'}); err != nil {
		return "cross-uid-namespace-release", err.Error()
	}
	proof, err := io.ReadAll(reader)
	if err != nil {
		return "namespace-pidfd-proof-read", err.Error()
	}
	waitErr := command.Wait()
	waited = true
	if waitErr != nil {
		if ctx.Err() != nil {
			return "cross-uid-probe-timeout", ctx.Err().Error()
		}
		return "cross-uid-namespace-pidfd-proof", waitErr.Error() + ": " + strings.TrimSpace(string(proof))
	}
	if !strings.Contains(string(proof), "namespace-proof=passed") ||
		!strings.Contains(string(proof), "fdinfo_inode=") ||
		!strings.Contains(string(proof), "tcp_state=ESTABLISHED") ||
		!strings.Contains(string(proof), "netns_cookie=") ||
		!strings.Contains(string(proof), "pidfd_getfd=passed") {
		return "cross-uid-namespace-pidfd-proof", strings.TrimSpace(string(proof))
	}
	return "", ""
}

func runProbe() (result report, failure string) {
	result.uid = os.Geteuid()
	result.bUID = probeBUID
	result.capability = "not-proven"
	result.crossUIDCapability = "not-proven"
	kernel, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return result, "kernel-version"
	}
	result.kernel = "linux/" + strings.TrimSpace(string(kernel))
	if result.uid == 0 {
		return result, "runner-is-root"
	}
	if failure := checkHostPrerequisites(&result); failure != "" {
		return result, failure
	}
	result.cgroup, err = membership()
	if err != nil {
		return result, "parent-membership"
	}
	parentDirectory, err := cgroupDirectory(result.cgroup)
	if err != nil {
		return result, "cgroup2-mount"
	}
	leafDirectory, err := os.MkdirTemp(parentDirectory, "symmetry-cgroup-probe-")
	if err != nil {
		result.capability = "unavailable"
		return result, "delegated-parent-not-writable"
	}
	result.leafCgroup = result.cgroup
	if result.leafCgroup != "/" {
		result.leafCgroup += "/"
	}
	result.leafCgroup += filepath.Base(leafDirectory)
	leafCreated := true
	var leafFD *os.File
	defer func() {
		if leafFD != nil {
			if err := leafFD.Close(); err != nil {
				recordCleanupFailure(&failure, &result.failureDetail, "close-leaf-fd", err)
			}
		}
		if leafCreated {
			if err := os.Remove(leafDirectory); err != nil {
				recordCleanupFailure(&failure, &result.failureDetail, "remove-leaf", err)
			}
		}
	}()
	leafFD, err = os.Open(leafDirectory)
	if err != nil {
		return result, "open-leaf-fd"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output := new(bytes.Buffer)
	pidFD := -1
	command := exec.CommandContext(ctx, "/proc/self/exe", "--child")
	command.Stdout = output
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{
		UseCgroupFD: true,
		CgroupFD:    int(leafFD.Fd()),
		PidFD:       &pidFD,
	}
	if err := command.Start(); err != nil {
		result.capability = "unavailable"
		return result, "clone3-cgroupfd-pidfd"
	}
	if pidFD < 0 {
		cancel()
		_ = command.Wait()
		result.capability = "unavailable"
		return result, "pidfd-not-returned"
	}
	pidFDErr := syscall.Close(pidFD)
	waitErr := command.Wait()
	timedOut := ctx.Err() != nil
	cancel()
	if pidFDErr != nil {
		return result, "close-pidfd"
	}
	if waitErr != nil {
		if timedOut {
			return result, "child-timeout"
		}
		return result, "child-exit"
	}

	values := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	result.childUID, err = strconv.Atoi(values["uid"])
	if err != nil || result.childUID != result.uid {
		return result, "child-uid-mismatch"
	}
	if values["cgroup_path"] != result.leafCgroup {
		return result, "child-leaf-mismatch"
	}
	events, err := os.ReadFile(filepath.Join(leafDirectory, "cgroup.events"))
	if err != nil {
		return result, "read-cgroup-events"
	}
	populatedZero := false
	fields := strings.Fields(string(events))
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "populated" && fields[i+1] == "0" {
			populatedZero = true
		}
	}
	if !populatedZero {
		return result, "leaf-still-populated"
	}
	result.capability = "clone3-cgroupfd-pidfd"
	if failure, detail := runCrossUIDProbe(&result, leafFD, leafDirectory); failure != "" {
		result.failureDetail = detail
		return result, failure
	}
	events, err = os.ReadFile(filepath.Join(leafDirectory, "cgroup.events"))
	if err != nil {
		return result, "read-cross-uid-cgroup-events"
	}
	populatedZero = false
	fields = strings.Fields(string(events))
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "populated" && fields[i+1] == "0" {
			populatedZero = true
		}
	}
	if !populatedZero {
		return result, "cross-uid-leaf-still-populated"
	}
	result.crossUIDCapability = "fixed-b-child-userns-fdinfo-pidfd-getfd"
	return result, ""
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--userns-check" {
		os.Exit(usernsCheckChild())
	}
	if len(os.Args) == 2 && os.Args[1] == "--child" {
		os.Exit(child())
	}
	if len(os.Args) == 4 && os.Args[1] == "--namespace-worker" {
		os.Exit(namespaceWorker(os.Args[2], os.Args[3], bufio.NewReader(os.Stdin)))
	}
	if len(os.Args) == 3 && os.Args[1] == "--socket-peer" {
		os.Exit(socketPeer())
	}
	if len(os.Args) == 1 {
		os.Exit(runWithRootPreparedGroups())
	}
	if len(os.Args) != 2 || os.Args[1] != "--prepared" {
		fmt.Println("status=failed stage=invalid-probe-invocation")
		os.Exit(2)
	}
	result, failure := runProbe()
	fmt.Printf("uid=%d\n", result.uid)
	fmt.Printf("cross_uid_target=%d\n", result.bUID)
	fmt.Printf("cross_gid_target=%d\n", probeBGID)
	if result.childUID != 0 {
		fmt.Printf("child_uid=%d\n", result.childUID)
	}
	if result.kernel != "" {
		fmt.Printf("kernel=%s\n", result.kernel)
	}
	if result.cgroup != "" {
		fmt.Printf("cgroup_path=%s\n", result.cgroup)
	}
	if result.leafCgroup != "" {
		fmt.Printf("leaf_cgroup_path=%s\n", result.leafCgroup)
	}
	fmt.Printf("capability=%s\n", result.capability)
	fmt.Printf("cross_uid_capability=%s\n", result.crossUIDCapability)
	fmt.Println("coverage=production_a_gate=not-tested native_pi_opencode=not-tested daemon_crash=not-tested")
	if result.suidDumpable != "" {
		fmt.Printf("fs_suid_dumpable=%s\n", result.suidDumpable)
	}
	if result.usernsEnabled {
		fmt.Println("userns_enabled=true")
	}
	if result.usernsCreated {
		fmt.Println("userns_create=passed")
	}
	if result.hostCapEff != "" {
		fmt.Printf("host_cap_eff=%s\n", result.hostCapEff)
	}
	if failure != "" {
		fmt.Printf("status=failed stage=%s", failure)
		if result.failureDetail != "" {
			fmt.Printf(" detail=%q", result.failureDetail)
		}
		fmt.Println()
		os.Exit(1)
	}
	fmt.Println("status=passed")
}
