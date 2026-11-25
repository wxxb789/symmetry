//go:build linux && amd64

package linuxreadback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func (options ServeOptions) readyPath() string {
	if options.ReadyFile != "" {
		return options.ReadyFile
	}
	return filepath.Join(options.Channel, "ready")
}

func (options ServeOptions) stopPath() string {
	if options.StopFile != "" {
		return options.StopFile
	}
	return filepath.Join(options.Channel, "stop")
}

// Serve runs the bounded root reader until the context is cancelled or a
// root-owned stop file appears. The ready file exists only while it serves.
func Serve(ctx context.Context, options ServeOptions) error {
	if err := validateServeOptions(options); err != nil {
		return err
	}
	if options.Log == nil {
		options.Log = os.Stderr
	}
	if options.MaxRequests == 0 {
		options.MaxRequests = 4096
	}
	logf := func(format string, args ...any) {
		fmt.Fprintf(options.Log, "readback-reader: "+format+"\n", args...)
	}
	if err := publishReadyFile(options); err != nil {
		return err
	}
	defer func() { _ = os.Remove(options.readyPath()) }()
	logf("ready channel=%s leaf_root=%s", options.Channel, options.LeafRoot)
	served := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if rootStopRequested(options.stopPath()) {
			logf("stop requests=%d", served)
			return nil
		}
		names, err := pendingRequests(options.Channel)
		if err != nil {
			return err
		}
		for _, name := range names {
			if served >= options.MaxRequests {
				return fmt.Errorf("readback request budget %d exhausted", options.MaxRequests)
			}
			served++
			serveRequest(options, name, logf)
		}
		select {
		case <-ctx.Done():
		case <-time.After(pollInterval):
		}
	}
}

func validateServeOptions(options ServeOptions) error {
	if err := validateChannelPath(options.Channel); err != nil {
		return err
	}
	if !filepath.IsAbs(options.LeafRoot) || filepath.Clean(options.LeafRoot) != options.LeafRoot || options.LeafRoot == "/" {
		return errors.New("readback leaf root must be a normalized absolute non-root path")
	}
	info, err := os.Stat(options.LeafRoot)
	if err != nil {
		return fmt.Errorf("stat readback leaf root: %w", err)
	}
	if !info.IsDir() {
		return errors.New("readback leaf root is not a directory")
	}
	if options.AUID == 0 || options.BUID == 0 || options.AUID == options.BUID || options.AGID == 0 || options.BGID == 0 {
		return errors.New("readback identities must be distinct non-root A and B identities")
	}
	if options.MaxRequests < 0 {
		return errors.New("readback request budget must not be negative")
	}
	return nil
}

func publishReadyFile(options ServeOptions) error {
	path := options.readyPath()
	_ = os.Remove(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o440)
	if err != nil {
		return fmt.Errorf("create readback ready file: %w", err)
	}
	_, writeErr := file.WriteString("ready\n")
	chmodErr := file.Chmod(0o440)
	chownErr := file.Chown(0, int(options.AGID))
	closeErr := file.Close()
	if writeErr != nil || chmodErr != nil || chownErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return errors.Join(writeErr, chmodErr, chownErr, closeErr)
	}
	return nil
}

func rootStopRequested(path string) bool {
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return false
	}
	// Only the fixture's root-owned stop file stops the reader: A can write the
	// channel directory, so a group-writable stop would let A end its own run.
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Uid == 0 && stat.Nlink == 1
}

func pendingRequests(channel string) ([]string, error) {
	entries, err := os.ReadDir(channel)
	if err != nil {
		return nil, fmt.Errorf("list readback channel: %w", err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, requestSuffix) || entry.IsDir() {
			continue
		}
		if !validNonce(strings.TrimSuffix(name, requestSuffix)) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func serveRequest(options ServeOptions, name string, logf func(string, ...any)) {
	nonce := strings.TrimSuffix(name, requestSuffix)
	requestPath := filepath.Join(options.Channel, name)
	body, err := readRequestFile(requestPath, options)
	if err != nil {
		refuse(options, nonce, RefusalRequest, "request file is not the exact A-owned single-linked bounded JSON file", logf)
		return
	}
	var want Want
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&want); err != nil {
		refuse(options, nonce, RefusalRequest, "request JSON is malformed", logf)
		return
	}
	if err := validateWant(options, want); err != nil {
		refuse(options, nonce, RefusalRequest, err.Error(), logf)
		return
	}
	capture, refusal := readTarget(options, want)
	if refusal != nil {
		refuse(options, nonce, refusal.Stage, refusal.Reason, logf)
		return
	}
	response := responseBody{
		Version: 1, Nonce: nonce,
		PID: capture.PID, StartTime: capture.StartTime, BootID: capture.BootID, Identity: capture.Identity,
		Executable: capture.Executable, ExeDevice: capture.ExeDevice, ExeInode: capture.ExeInode,
		Environ: capture.Environ, Descriptors: capture.Descriptors, Namespaces: capture.Namespaces,
	}
	if err := publishResponse(options, nonce, &response); err != nil {
		logf("response write failed nonce=%s pid=%d error=%v", nonce, want.PID, err)
		_ = os.Remove(requestPath)
		return
	}
	_ = os.Remove(requestPath)
	logf("capture nonce=%s pid=%d environ_bytes=%d descriptors=%d namespaces=%d",
		nonce, capture.PID, len(capture.Environ), len(capture.Descriptors), len(capture.Namespaces))
}

func readRequestFile(path string, options ServeOptions) ([]byte, error) {
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 ||
		stat.Uid != options.AUID || stat.Gid != options.AGID || stat.Mode&0o777 != 0o600 ||
		stat.Size <= 0 || stat.Size > maxRequestBytes {
		return nil, errors.New("request file identity differs from the exact A-owned 0600 file")
	}
	body := make([]byte, stat.Size)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	total, readErr := io.ReadFull(file, body)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || total != len(body) {
		return nil, errors.Join(readErr, closeErr)
	}
	return body, nil
}

func validateWant(options ServeOptions, want Want) error {
	if want.PID <= 1 || want.StartTime == 0 {
		return errors.New("request PID or start time is invalid")
	}
	if want.BootID == "" || len(want.BootID) > 64 || strings.Trim(want.BootID, "0123456789abcdef-") != "" {
		return errors.New("request boot identity is invalid")
	}
	if want.Identity != "" && !strings.HasPrefix(want.Identity, "linux:v2:") {
		return errors.New("request creation identity is not a linux:v2 identity")
	}
	if err := validateLeafBelow(options.LeafRoot, want.Leaf); err != nil {
		return err
	}
	if !executableExpectationComplete(want) {
		return errors.New("request executable expectation is invalid")
	}
	if !want.Environ && !want.Descriptors && len(want.Namespaces) == 0 && want.Identity == "" {
		return errors.New("request asks for no observable fact")
	}
	for _, name := range want.Namespaces {
		if name != "user" && name != "net" {
			return errors.New("request names a namespace outside the user/net allowlist")
		}
	}
	return nil
}

// The executable expectation is optional as a group: the e2e observer cannot
// stat B's magic link, but the native tests always bind the approved image.
func executableExpectationComplete(want Want) bool {
	if want.Executable == "" && want.ExeDevice == 0 && want.ExeInode == 0 {
		return true
	}
	return filepath.IsAbs(want.Executable) && filepath.Clean(want.Executable) == want.Executable &&
		want.Executable != "/" && want.ExeDevice != 0 && want.ExeInode != 0
}

func validateLeafBelow(root, leaf string) error {
	if !filepath.IsAbs(leaf) || filepath.Clean(leaf) != leaf || !strings.HasPrefix(leaf, root+string(filepath.Separator)) {
		return errors.New("request leaf is not a normalized path below the fixed leaf root")
	}
	return nil
}

func refuse(options ServeOptions, nonce string, stage RefusalStage, reason string, logf func(string, ...any)) {
	response := responseBody{Version: 1, Nonce: nonce, Stage: stage, Reason: reason}
	if err := publishResponse(options, nonce, &response); err != nil {
		logf("refusal write failed nonce=%s stage=%s error=%v", nonce, stage, err)
	}
	_ = os.Remove(filepath.Join(options.Channel, requestName(nonce)))
	logf("refusal nonce=%s stage=%s reason=%s", nonce, stage, reason)
}

func publishResponse(options ServeOptions, nonce string, response *responseBody) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode readback response: %w", err)
	}
	if len(encoded) > maxResponseBytes {
		return errors.New("encoded readback response exceeds its bound")
	}
	tempPath := filepath.Join(options.Channel, responseName(nonce)+".tmp")
	finalPath := filepath.Join(options.Channel, responseName(nonce))
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writeErr := func() error {
		if _, err := file.Write(encoded); err != nil {
			return err
		}
		if err := file.Chmod(0o600); err != nil {
			return err
		}
		if err := file.Chown(int(options.AUID), int(options.AGID)); err != nil {
			return err
		}
		return file.Close()
	}()
	if writeErr != nil {
		_ = os.Remove(tempPath)
		return writeErr
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

// readTarget binds the exact running process before it reads anything: pidfd,
// boot ID/start time/identity/B credentials/leaf/A-approved exe all must match.
func readTarget(options ServeOptions, want Want) (Capture, *CaptureError) {
	pidfd, err := unix.PidfdOpen(want.PID, 0)
	if err != nil {
		return Capture{}, targetRefusal(want.PID, err)
	}
	defer unix.Close(pidfd)
	startTime, err := readStatStartTime(want.PID)
	if err != nil {
		return Capture{}, targetRefusal(want.PID, err)
	}
	if startTime != want.StartTime {
		return Capture{}, &CaptureError{Stage: RefusalIdentity, Reason: "process start time differs from the request"}
	}
	bootID, err := readBootID()
	if err != nil {
		return Capture{}, &CaptureError{Stage: RefusalRead, Reason: "kernel boot identity is unreadable"}
	}
	if bootID != want.BootID {
		return Capture{}, &CaptureError{Stage: RefusalIdentity, Reason: "kernel boot identity differs from the request"}
	}
	userStat, networkStat, pidInode, err := readNamespaceIdentities(want.PID, want.Namespaces)
	if err != nil {
		return Capture{}, &CaptureError{Stage: RefusalRead, Reason: "process namespace identity is unreadable"}
	}
	identity := fmt.Sprintf("linux:v2:%s:%d:%d:%d", bootID, pidInode, want.PID, startTime)
	if want.Identity != "" && want.Identity != identity {
		return Capture{}, &CaptureError{Stage: RefusalIdentity, Reason: "recomputed process creation identity differs from the request"}
	}
	if err := verifyCredentials(want.PID, options); err != nil {
		return Capture{}, err
	}
	if err := verifyLeafMembership(want.Leaf, want.PID); err != nil {
		return Capture{}, err
	}
	exePath, exeDevice, exeInode, err := readExecutable(want.PID)
	if err != nil {
		return Capture{}, targetRefusal(want.PID, err)
	}
	if want.Executable != "" && (exeDevice != want.ExeDevice || exeInode != want.ExeInode) {
		return Capture{}, &CaptureError{Stage: RefusalExecutable, Reason: "process executable differs from the A-approved image"}
	}
	capture := Capture{
		PID: want.PID, StartTime: startTime, BootID: bootID, Identity: identity,
		Executable: exePath, ExeDevice: exeDevice, ExeInode: exeInode,
	}
	if want.Environ {
		environ, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", want.PID))
		if err != nil {
			return Capture{}, &CaptureError{Stage: RefusalRead, Reason: "process environment is unreadable"}
		}
		if len(environ) > maxEnvironBytes {
			return Capture{}, &CaptureError{Stage: RefusalLimit, Reason: "process environment exceeds the readback bound"}
		}
		capture.Environ = environ
	}
	if want.Descriptors {
		descriptors, refusal := readDescriptors(want.PID)
		if refusal != nil {
			return Capture{}, refusal
		}
		capture.Descriptors = descriptors
	}
	if len(want.Namespaces) != 0 {
		capture.Namespaces = map[string]Namespace{}
		if contains(want.Namespaces, "user") {
			capture.Namespaces["user"] = userStat
		}
		if contains(want.Namespaces, "net") {
			capture.Namespaces["net"] = networkStat
		}
	}
	after, err := readStatStartTime(want.PID)
	if err != nil || after != startTime {
		return Capture{}, &CaptureError{Stage: RefusalIdentity, Reason: "process start time changed during the readback"}
	}
	poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	if _, err := unix.Poll(poll, 0); err != nil || poll[0].Revents != 0 {
		return Capture{}, &CaptureError{Stage: RefusalTargetMissing, Reason: "process ended during the readback"}
	}
	return capture, nil
}

func targetRefusal(pid int, err error) *CaptureError {
	stage := RefusalRead
	if errors.Is(err, unix.ESRCH) || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) {
		stage = RefusalTargetMissing
	}
	return &CaptureError{Stage: stage, Reason: fmt.Sprintf("process %d is not a readable live target", pid)}
}

func verifyCredentials(pid int, options ServeOptions) *CaptureError {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return targetRefusal(pid, err)
	}
	var uids, gids []string
	var seenUID, seenGID bool
	for _, line := range strings.Split(string(status), "\n") {
		switch {
		case strings.HasPrefix(line, "Uid:"):
			uids, seenUID = strings.Fields(strings.TrimPrefix(line, "Uid:")), true
		case strings.HasPrefix(line, "Gid:"):
			gids, seenGID = strings.Fields(strings.TrimPrefix(line, "Gid:")), true
		}
	}
	wantUID, wantGID := strconv.FormatUint(uint64(options.BUID), 10), strconv.FormatUint(uint64(options.BGID), 10)
	if !seenUID || !seenGID || len(uids) != 4 || len(gids) != 4 {
		return &CaptureError{Stage: RefusalCredentials, Reason: "process status does not carry the exact credential vectors"}
	}
	for index := range 4 {
		if uids[index] != wantUID || gids[index] != wantGID {
			return &CaptureError{Stage: RefusalCredentials, Reason: "process credentials differ from the configured B identity"}
		}
	}
	return nil
}

func verifyLeafMembership(leaf string, pid int) *CaptureError {
	contents, err := os.ReadFile(filepath.Join(leaf, "cgroup.procs"))
	if err != nil {
		return &CaptureError{Stage: RefusalLeaf, Reason: "named leaf has no readable cgroup.procs"}
	}
	if len(contents) > 64<<10 {
		return &CaptureError{Stage: RefusalLeaf, Reason: "named leaf cgroup.procs exceeds the bounded read"}
	}
	wanted := strconv.Itoa(pid)
	for _, field := range strings.Fields(string(contents)) {
		if field == wanted {
			return nil
		}
	}
	return &CaptureError{Stage: RefusalLeaf, Reason: "process is not a member of the named leaf"}
}

func readExecutable(pid int) (string, uint64, uint64, error) {
	var stat unix.Stat_t
	if err := unix.Stat(fmt.Sprintf("/proc/%d/exe", pid), &stat); err != nil {
		return "", 0, 0, err
	}
	if stat.Ino == 0 {
		return "", 0, 0, errors.New("process executable has no stable inode")
	}
	target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "", 0, 0, err
	}
	return target, uint64(stat.Dev), stat.Ino, nil
}

func readNamespaceIdentities(pid int, names []string) (Namespace, Namespace, uint64, error) {
	pidStat, err := statNamespace(pid, "pid")
	if err != nil {
		return Namespace{}, Namespace{}, 0, err
	}
	userStat := Namespace{}
	networkStat := Namespace{}
	if contains(names, "user") {
		if userStat, err = statNamespace(pid, "user"); err != nil {
			return Namespace{}, Namespace{}, 0, err
		}
	}
	if contains(names, "net") {
		if networkStat, err = statNamespace(pid, "net"); err != nil {
			return Namespace{}, Namespace{}, 0, err
		}
	}
	return userStat, networkStat, pidStat.Inode, nil
}

func statNamespace(pid int, name string) (Namespace, error) {
	var stat unix.Stat_t
	if err := unix.Stat(fmt.Sprintf("/proc/%d/ns/%s", pid, name), &stat); err != nil {
		return Namespace{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Ino == 0 {
		return Namespace{}, errors.New("namespace is not an accessible nsfs inode")
	}
	return Namespace{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func readDescriptors(pid int) ([]Descriptor, *CaptureError) {
	directory := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, &CaptureError{Stage: RefusalRead, Reason: "process descriptor table is unreadable"}
	}
	if len(entries) > maxDescriptors {
		return nil, &CaptureError{Stage: RefusalLimit, Reason: "process descriptor table exceeds the readback bound"}
	}
	descriptors := make([]Descriptor, 0, len(entries))
	for _, entry := range entries {
		number, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil || number < 0 {
			return nil, &CaptureError{Stage: RefusalRead, Reason: "process descriptor table contains a nonnumeric descriptor"}
		}
		target, err := os.Readlink(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, &CaptureError{Stage: RefusalRead, Reason: "process descriptor readlink failed"}
		}
		descriptors = append(descriptors, Descriptor{Number: number, Target: target})
	}
	sort.Slice(descriptors, func(left, right int) bool { return descriptors[left].Number < descriptors[right].Number })
	return descriptors, nil
}

func readStatStartTime(pid int) (uint64, error) {
	contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	end := bytes.LastIndexByte(contents, ')')
	if end < 0 || end+1 >= len(contents) {
		return 0, errors.New("process stat lacks a command terminator")
	}
	fields := strings.Fields(string(contents[end+1:]))
	if len(fields) <= 19 {
		return 0, errors.New("process stat lacks the start-time field")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return 0, errors.New("process start-time field is invalid")
	}
	return start, nil
}

func readBootID() (string, error) {
	contents, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	bootID := strings.TrimSpace(string(contents))
	if bootID == "" || len(bootID) > 64 {
		return "", errors.New("kernel boot identity is empty or oversized")
	}
	return bootID, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
