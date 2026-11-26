//go:build linux && amd64

package linuxreadback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	localReproEnv  = "SYMMETRY_LINUX_READBACK_LOCAL_REPRO"
	localChildEnv  = "SYMMETRY_LINUX_READBACK_LOCAL_CHILD"
	localTargetEnv = "SYMMETRY_LINUX_READBACK_LOCAL_TARGET"
	localClientEnv = "SYMMETRY_LINUX_READBACK_LOCAL_CLIENT"
	localStateEnv  = "SYMMETRY_LINUX_READBACK_LOCAL_STATE"
	localReaderEnv = "SYMMETRY_LINUX_READBACK_LOCAL_READER_BINARY"
	localCgroupEnv = "SYMMETRY_LINUX_READBACK_LOCAL_CGROUP_ROOT"
	localWideEnv   = "SYMMETRY_LINUX_READBACK_LOCAL_WIDE"

	localAUID    = 52000
	localAGID    = 52000
	localBUID    = 52001
	localBGID    = 52001
	localTimeout = 30 * time.Second
)

// The planted-symlink row's root-owned target: the reader must never touch it.
const localVictimContent = "READBACK_LOCAL_VICTIM=exact\n"

// The exact target vector both sides compare: the parent's expectation and the
// child's exec environment are this one list.
var localReproTargetEnv = []string{
	"PATH=/bin:/usr/bin",
	"LANG=C",
	"READBACK_LOCAL_PROBE=exact-value",
	localTargetEnv + "=1",
}

type localReproState struct {
	Channel    string           `json:"channel"`
	PID        int              `json:"pid"`
	StartTime  uint64           `json:"start_time"`
	BootID     string           `json:"boot_id"`
	Identity   string           `json:"identity"`
	Executable string           `json:"executable"`
	ExeDevice  uint64           `json:"exe_device"`
	ExeInode   uint64           `json:"exe_inode"`
	Leaf       string           `json:"leaf"`
	Victim     string           `json:"victim"`
	Expected   []string         `json:"expected"`
	Wide       *localReproState `json:"wide,omitempty"`
}

// TestReadbackLocalReproduction is the opt-in WSL row: a root reader against a
// userns-dropped child, with an A-credentialed client and the mutant check.
func TestReadbackLocalReproduction(t *testing.T) {
	if os.Getenv(localReproEnv) != "1" {
		t.Skip("opt-in local reproduction of the root readback reader")
	}
	if os.Getuid() != 0 {
		t.Fatal("the local readback reproduction must run as root")
	}
	// The A/B-external processes must traverse this tree, so it is a plain
	// 0755 directory rather than t.TempDir's 0700 test-name parent.
	root, err := os.MkdirTemp("", "readback-local-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	image, err := copyLocalReproImage(root)
	if err != nil {
		t.Fatal(err)
	}
	channel := filepath.Join(root, "channel")
	if err := os.Mkdir(channel, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(channel, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(channel, 0, localAGID); err != nil {
		t.Fatal(err)
	}
	child, leaf, err := startLocalReproChild(t, image, nil)
	if err != nil {
		t.Fatal(err)
	}
	widePath, err := createLocalReproWidePath(root)
	if err != nil {
		t.Fatal(err)
	}
	wideChild, wideLeaf, err := startLocalReproChild(t, image, []string{localWideEnv + "=" + widePath})
	if err != nil {
		t.Fatal(err)
	}
	// The root-owned symlink target the planted-response-temp row protects.
	victimPath := filepath.Join(root, "readback-victim")
	if err := os.WriteFile(victimPath, []byte(localVictimContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(victimPath, 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := localReproStateFor(t, child, image, leaf, channel)
	if err != nil {
		t.Fatal(err)
	}
	state.Victim = victimPath
	wideState, err := localReproStateFor(t, wideChild, image, wideLeaf, channel)
	if err != nil {
		t.Fatal(err)
	}
	state.Wide = &wideState
	stopReader, err := startLocalReproReader(t, channel, localReproLeafRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer stopReader()
	statePath := filepath.Join(root, "state.json")
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	client := exec.Command(image, "-test.run=^TestReadbackLocalClient$")
	client.Env = []string{localClientEnv + "=1", localStateEnv + "=" + statePath}
	client.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: localAUID, Gid: localAGID, NoSetGroups: true}}
	output, err := client.CombinedOutput()
	fmt.Printf("%s", output)
	if err != nil {
		t.Fatalf("A-side local readback client did not hold every row: %v", err)
	}
}

// TestReadbackLocalChild starts inside CLONE_NEWUSER, re-execs to regain
// namespace capabilities after the maps, drops to the B extent and execs.
func TestReadbackLocalChild(t *testing.T) {
	switch os.Getenv(localChildEnv) {
	case "":
		t.Skip("internal local readback userns child")
	case "maps-ready":
		localReproDropAndExec(t)
	default:
		localReproAwaitMapsAndReexec(t)
	}
}

func localReproAwaitMapsAndReexec(t *testing.T) {
	sync := os.NewFile(3, "readback-local-sync")
	if sync == nil {
		t.Fatal("local readback child lacks its synchronization descriptor")
	}
	var signal [1]byte
	if _, err := sync.Read(signal[:]); err != nil {
		t.Fatal(err)
	}
	image, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The first exec cleared this namespace's capabilities before its map
	// existed; the second exec after the map restores the namespace root set.
	env := []string{localChildEnv + "=maps-ready"}
	if wide := os.Getenv(localWideEnv); wide != "" {
		env = append(env, localWideEnv+"="+wide)
	}
	if err := syscall.Exec(image, []string{image, "-test.run=^TestReadbackLocalChild$"}, env); err != nil {
		t.Fatal(err)
	}
}

func localReproDropAndExec(t *testing.T) {
	if err := unix.Setresgid(1, 1, 1); err != nil {
		t.Fatalf("drop to the B group extent after the live map: %v", err)
	}
	if err := unix.Setresuid(1, 1, 1); err != nil {
		t.Fatalf("drop to the B user extent after the live map: %v", err)
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	image, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := localReproTargetEnv
	if wide := os.Getenv(localWideEnv); wide != "" {
		env = append(append([]string(nil), localReproTargetEnv...), localWideEnv+"="+wide)
		for range maxResponseBytes/len(wide) + 16 {
			// O_RDONLY without O_CLOEXEC: the census must see these across exec.
			if _, err := unix.Open(wide, unix.O_RDONLY, 0); err != nil {
				t.Fatalf("open wide readback descriptor: %v", err)
			}
		}
	}
	if err := syscall.Exec(image, []string{image, "-test.run=^TestReadbackLocalTarget$"}, env); err != nil {
		t.Fatal(err)
	}
}

// TestReadbackLocalTarget is the fixed-B stand-in: it holds the exact vector
// until the parent kills it.
func TestReadbackLocalTarget(t *testing.T) {
	if os.Getenv(localTargetEnv) != "1" {
		t.Skip("internal local readback target")
	}
	for {
		time.Sleep(time.Second)
	}
}

// TestReadbackLocalClient is the A-credentialed row set: A denied, the reader
// allowed, the PID-mismatch refusal and the corrupted-expected-byte mutant.
func TestReadbackLocalClient(t *testing.T) {
	if os.Getenv(localClientEnv) != "1" {
		t.Skip("internal local readback A client")
	}
	fail := func(format string, args ...any) {
		fmt.Printf("READBACK_LOCAL_FAIL: "+format+"\n", args...)
		os.Exit(3)
	}
	if os.Getuid() != localAUID || os.Getgid() != localAGID {
		fail("client must run as the configured A identity: uid=%d gid=%d", os.Getuid(), os.Getgid())
	}
	encoded, err := os.ReadFile(os.Getenv(localStateEnv))
	if err != nil {
		fail("client cannot read the local row state: %v", err)
	}
	var state localReproState
	if err := json.Unmarshal(encoded, &state); err != nil {
		fail("client cannot decode the local row state: %v", err)
	}
	if _, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", state.PID)); !errors.Is(err, os.ErrPermission) {
		fail("A unexpectedly read the target environment directly: %v", err)
	}
	fmt.Println("A_DIRECT_ENVIRON=denied")
	if _, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", state.PID)); !errors.Is(err, os.ErrPermission) {
		fail("A unexpectedly listed the target descriptor table directly: %v", err)
	}
	fmt.Println("A_DIRECT_FD=denied")
	want := Want{
		PID: state.PID, StartTime: state.StartTime, BootID: state.BootID, Identity: state.Identity,
		Leaf: state.Leaf, Executable: state.Executable, ExeDevice: state.ExeDevice, ExeInode: state.ExeInode,
		Environ: true, Descriptors: true, Namespaces: []string{"user", "net"},
	}
	capture, err := Read(state.Channel, want, localTimeout)
	if err != nil {
		fail("root reader did not capture the userns-dropped target: %v", err)
	}
	if err := CheckEnvironment(capture, state.Expected); err != nil {
		fail("root reader captured the wrong environment: %v", err)
	}
	if capture.ExeDevice != state.ExeDevice || capture.ExeInode != state.ExeInode {
		fail("root reader did not bind the approved image dev/ino: device=%d inode=%d", capture.ExeDevice, capture.ExeInode)
	}
	userNamespace, networkNamespace := capture.Namespaces["user"], capture.Namespaces["net"]
	if userNamespace.Inode == 0 || networkNamespace.Inode == 0 {
		fail("root reader returned incomplete namespace identities: %+v", capture.Namespaces)
	}
	fmt.Printf("READER_CAPTURE=ok environ_bytes=%d descriptors=%d user_ns=%d:%d net_ns=%d:%d\n",
		len(capture.Environ), len(capture.Descriptors), userNamespace.Device, userNamespace.Inode,
		networkNamespace.Device, networkNamespace.Inode)
	mutated := append([]string(nil), state.Expected...)
	mutated[0] = string([]byte{mutated[0][0] ^ 1}) + mutated[0][1:]
	if err := CheckEnvironment(capture, mutated); err == nil {
		fail("one corrupted expected environment byte did not redden the assertion")
	}
	fmt.Println("MUTANT_ENV_BYTE=red")
	mismatch := want
	mismatch.StartTime++
	if _, err := Read(state.Channel, mismatch, localTimeout); err == nil {
		fail("a wrong start time was accepted as the target identity")
	} else if stage, ok := RefusalStageOf(err); !ok || stage != RefusalIdentity {
		fail("wrong-start-time refusal is not the typed identity refusal: %v", err)
	}
	fmt.Printf("PID_MISMATCH=refused stage=%s\n", RefusalIdentity)
	if state.Wide == nil {
		fail("local row state lacks the wide descriptor target")
	}
	selfStart, err := readStatStartTime(os.Getpid())
	if err != nil {
		fail("client cannot read its own start time: %v", err)
	}
	selfBootID, err := readBootID()
	if err != nil {
		fail("client cannot read the kernel boot identity: %v", err)
	}
	credentials := want
	credentials.PID, credentials.StartTime, credentials.BootID, credentials.Identity = os.Getpid(), selfStart, selfBootID, ""
	if _, err := Read(state.Channel, credentials, localTimeout); err == nil {
		fail("an A-credentialed target was captured instead of refused")
	} else if stage, ok := RefusalStageOf(err); !ok || stage != RefusalCredentials {
		fail("an A-credentialed target is not the typed credentials refusal: %v", err)
	}
	fmt.Printf("REFUSAL_CREDENTIALS=refused stage=%s\n", RefusalCredentials)
	outside := want
	outside.Leaf = "/tmp/symmetry-readback-leaf-outside"
	if _, err := Read(state.Channel, outside, localTimeout); err == nil {
		fail("a leaf outside the fixed leaf root was accepted")
	} else if stage, ok := RefusalStageOf(err); !ok || stage != RefusalRequest {
		fail("a leaf outside the fixed leaf root is not the typed request refusal: %v", err)
	}
	fmt.Printf("REFUSAL_LEAF_OUTSIDE=refused stage=%s\n", RefusalRequest)
	membership := want
	membership.Leaf = state.Wide.Leaf
	if _, err := Read(state.Channel, membership, localTimeout); err == nil {
		fail("a target outside the named leaf was captured")
	} else if stage, ok := RefusalStageOf(err); !ok || stage != RefusalLeaf {
		fail("a target outside the named leaf is not the typed leaf refusal: %v", err)
	}
	fmt.Printf("REFUSAL_LEAF_MEMBERSHIP=refused stage=%s\n", RefusalLeaf)
	executable := want
	executable.ExeInode++
	if _, err := Read(state.Channel, executable, localTimeout); err == nil {
		fail("a wrong executable inode was accepted as the approved image")
	} else if stage, ok := RefusalStageOf(err); !ok || stage != RefusalExecutable {
		fail("a wrong executable inode is not the typed executable refusal: %v", err)
	}
	fmt.Printf("REFUSAL_EXECUTABLE=refused stage=%s\n", RefusalExecutable)
	wide := Want{
		PID: state.Wide.PID, StartTime: state.Wide.StartTime, BootID: state.Wide.BootID,
		Identity: state.Wide.Identity, Leaf: state.Wide.Leaf, Environ: true, Descriptors: true,
	}
	if _, err := Read(state.Channel, wide, localTimeout); err == nil {
		fail("a descriptor census beyond the response bound was captured instead of refused")
	} else if stage, ok := RefusalStageOf(err); !ok || stage != RefusalLimit {
		fail("an oversized descriptor census is not the typed limit refusal: %v", err)
	}
	fmt.Printf("RESPONSE_LIMIT=refused stage=%s\n", RefusalLimit)
	localReproAdversarialRows(fail, state, want)
	fmt.Println("READBACK_LOCAL_CLIENT=ok")
	os.Exit(0)
}

// localReproAdversarialRows plants the A-side names the reader must refuse:
// response-temp symlink/FIFO plants and request symlink/FIFO/hard-link plants.
func localReproAdversarialRows(fail func(string, ...any), state localReproState, want Want) {
	channel := state.Channel
	// A planted response temp must be refused, never followed to the victim.
	nonce, err := newNonce()
	if err != nil {
		fail("client cannot mint the response-temp nonce: %v", err)
	}
	request := filepath.Join(channel, requestName(nonce))
	if err := writeLocalReproFile(request, []byte("not json")); err != nil {
		fail("client cannot plant the response-temp request: %v", err)
	}
	temp := filepath.Join(channel, responseName(nonce)+".tmp")
	if err := os.Symlink(state.Victim, temp); err != nil {
		fail("client cannot plant the response temp symlink: %v", err)
	}
	deadline := time.Now().Add(localTimeout)
	for {
		if _, err := os.Lstat(filepath.Join(channel, responseName(nonce))); err == nil {
			fail("the reader published through the planted response-temp symlink")
		}
		if _, err := os.Lstat(request); errors.Is(err, os.ErrNotExist) {
			break
		}
		if !time.Now().Before(deadline) {
			fail("the reader never processed the planted response-temp request")
		}
		time.Sleep(pollInterval)
	}
	if target, err := os.Readlink(temp); err != nil || target != state.Victim {
		fail("the planted response temp was not left a symlink to the victim: target=%q err=%v", target, err)
	}
	victim, err := os.ReadFile(state.Victim)
	if err != nil || string(victim) != localVictimContent {
		fail("the planted response temp target changed: bytes=%q err=%v", victim, err)
	}
	var victimStat syscall.Stat_t
	if err := syscall.Stat(state.Victim, &victimStat); err != nil ||
		victimStat.Uid != 0 || victimStat.Gid != 0 || victimStat.Mode&0o777 != 0o644 {
		fail("the planted response temp target changed owner or mode: %+v err=%v", victimStat, err)
	}
	fmt.Println("SYMLINK_RESP_TEMP=refused victim=intact")

	encoded, err := json.Marshal(want)
	if err != nil {
		fail("client cannot encode the exact request payload: %v", err)
	}
	// A request-name symlink to an A-owned valid request must never be followed.
	nonce, err = newNonce()
	if err != nil {
		fail("client cannot mint the request-symlink nonce: %v", err)
	}
	payload := filepath.Join(channel, nonce+".payload")
	if err := writeLocalReproFile(payload, encoded); err != nil {
		fail("client cannot write the request-symlink target: %v", err)
	}
	request = filepath.Join(channel, requestName(nonce))
	if err := os.Symlink(payload, request); err != nil {
		fail("client cannot plant the request symlink: %v", err)
	}
	if stage, err := localAwaitRefusal(channel, nonce, want); err != nil || stage != RefusalRequest {
		fail("a symlinked request was not the typed request refusal: stage=%s err=%v", stage, err)
	}
	_ = os.Remove(payload)
	fmt.Println("SYMLINK_REQUEST=refused stage=request")

	// A FIFO at the request name must be refused without hanging the reader.
	nonce, err = newNonce()
	if err != nil {
		fail("client cannot mint the FIFO nonce: %v", err)
	}
	fifo := filepath.Join(channel, requestName(nonce))
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		fail("client cannot plant the request FIFO: %v", err)
	}
	if stage, err := localAwaitRefusal(channel, nonce, want); err != nil || stage != RefusalRequest {
		fail("a FIFO request was not the typed request refusal: stage=%s err=%v", stage, err)
	}
	_ = os.Remove(fifo)
	fmt.Println("FIFO_REQUEST=refused stage=request")

	// A hard link at the request name must fail the single-link request gate.
	nonce, err = newNonce()
	if err != nil {
		fail("client cannot mint the hard-link nonce: %v", err)
	}
	payload = filepath.Join(channel, nonce+".payload")
	if err := writeLocalReproFile(payload, encoded); err != nil {
		fail("client cannot write the hard-link target: %v", err)
	}
	request = filepath.Join(channel, requestName(nonce))
	if err := os.Link(payload, request); err != nil {
		fail("client cannot plant the request hard link: %v", err)
	}
	if stage, err := localAwaitRefusal(channel, nonce, want); err != nil || stage != RefusalRequest {
		fail("a hard-linked request was not the typed request refusal: stage=%s err=%v", stage, err)
	}
	_ = os.Remove(request)
	_ = os.Remove(payload)
	fmt.Println("HARDLINK_REQUEST=refused stage=request")

	// A FIFO at the response temp must refuse fast: O_EXCL rejects the existing
	// name before any blocking FIFO open (the old bytes hung here).
	nonce, err = newNonce()
	if err != nil {
		fail("client cannot mint the response-FIFO nonce: %v", err)
	}
	request = filepath.Join(channel, requestName(nonce))
	if err := writeLocalReproFile(request, []byte("not json")); err != nil {
		fail("client cannot plant the response-FIFO request: %v", err)
	}
	fifo = filepath.Join(channel, responseName(nonce)+".tmp")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		fail("client cannot plant the response temp FIFO: %v", err)
	}
	deadline = time.Now().Add(localTimeout)
	for {
		if _, err := os.Lstat(filepath.Join(channel, responseName(nonce))); err == nil {
			fail("the reader published a response through the planted response-temp FIFO")
		}
		if _, err := os.Lstat(request); errors.Is(err, os.ErrNotExist) {
			break
		}
		if !time.Now().Before(deadline) {
			fail("the reader blocked on the planted response-temp FIFO")
		}
		time.Sleep(pollInterval)
	}
	var fifoStat syscall.Stat_t
	if err := syscall.Lstat(fifo, &fifoStat); err != nil || fifoStat.Mode&syscall.S_IFMT != syscall.S_IFIFO {
		fail("the planted response temp was not left a FIFO: %+v err=%v", fifoStat, err)
	}
	_ = os.Remove(fifo)
	fmt.Println("FIFO_RESP_TEMP=refused not-blocked")
}

// writeLocalReproFile writes exactly one A-owned 0600 file for a planted row.
func writeLocalReproFile(path string, contents []byte) error {
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// localAwaitRefusal polls for one nonce's typed refusal; a capture instead of
// a refusal is the mutant signature every planted row must redden.
func localAwaitRefusal(channel, nonce string, want Want) (RefusalStage, error) {
	path := filepath.Join(channel, responseName(nonce))
	deadline := time.Now().Add(localTimeout)
	for {
		body, err := readResponse(path)
		if err == nil {
			_ = os.Remove(path)
			if _, refusalErr := decodeCapture(nonce, want, body); refusalErr != nil {
				if stage, ok := RefusalStageOf(refusalErr); ok {
					return stage, nil
				}
				return "", refusalErr
			}
			return "", errors.New("reader answered the planted name with a capture")
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if !time.Now().Before(deadline) {
			return "", errors.New("reader did not answer the planted name within the local timeout")
		}
		time.Sleep(pollInterval)
	}
}

func copyLocalReproImage(root string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(root, "image")
	if err := os.Mkdir(directory, 0o755); err != nil {
		return "", err
	}
	contents, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	image := filepath.Join(directory, "readback-local-test")
	if err := os.WriteFile(image, contents, 0o755); err != nil {
		return "", err
	}
	return image, nil
}

func startLocalReproChild(t *testing.T, image string, extraEnv []string) (*exec.Cmd, string, error) {
	syncRead, syncWrite, err := os.Pipe()
	if err != nil {
		return nil, "", err
	}
	child := exec.Command(image, "-test.run=^TestReadbackLocalChild$")
	child.Env = append([]string{localChildEnv + "=1"}, extraEnv...)
	child.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWUSER}
	child.ExtraFiles = []*os.File{syncRead}
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		_ = syncWrite.Close()
		return nil, "", err
	}
	_ = syncRead.Close()
	pid := child.Process.Pid
	// The child's creator uid must stay mapped across its exec, or the exec
	// clears its namespace capabilities before it can drop to the B extent.
	uidMap := fmt.Sprintf("0 0 1\n1 %d 1\n", localBUID)
	gidMap := fmt.Sprintf("0 0 1\n1 %d 1\n", localBGID)
	for _, mapping := range []struct{ path, value string }{
		{fmt.Sprintf("/proc/%d/setgroups", pid), "deny"},
		{fmt.Sprintf("/proc/%d/uid_map", pid), uidMap},
		{fmt.Sprintf("/proc/%d/gid_map", pid), gidMap},
	} {
		if err := os.WriteFile(mapping.path, []byte(mapping.value), 0o600); err != nil {
			_ = child.Process.Kill()
			return nil, "", fmt.Errorf("write %s: %w", mapping.path, err)
		}
	}
	if _, err := syncWrite.Write([]byte{'x'}); err != nil {
		_ = child.Process.Kill()
		return nil, "", err
	}
	_ = syncWrite.Close()
	leafRoot := localReproLeafRoot()
	leaf := filepath.Join(leafRoot, fmt.Sprintf("symmetry-readback-local-%d", pid))
	if err := os.Mkdir(leaf, 0o755); err != nil {
		_ = child.Process.Kill()
		return nil, "", err
	}
	if err := waitForLocal(t, func() (bool, error) {
		var stat unix.Stat_t
		if err := unix.Stat(fmt.Sprintf("/proc/%d", pid), &stat); err != nil {
			return false, err
		}
		if stat.Uid != localBUID {
			return false, nil
		}
		target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil {
			return false, err
		}
		return target == image, nil
	}); err != nil {
		_ = child.Process.Kill()
		return nil, "", fmt.Errorf("userns child never reached the exact target image: %w", err)
	}
	if err := os.WriteFile(filepath.Join(leaf, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o600); err != nil {
		_ = child.Process.Kill()
		return nil, "", fmt.Errorf("move the target into the exact leaf: %w", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
		_ = os.Remove(leaf)
	})
	return child, leaf, nil
}

func createLocalReproWidePath(root string) (string, error) {
	path := root
	for len(path) < 1900 {
		path = filepath.Join(path, strings.Repeat("w", 240))
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	file := filepath.Join(path, "readback-wide-file")
	if err := os.WriteFile(file, []byte("wide\n"), 0o644); err != nil {
		return "", err
	}
	return file, nil
}

func localReproStateFor(t *testing.T, child *exec.Cmd, image, leaf, channel string) (localReproState, error) {
	pid := child.Process.Pid
	state := localReproState{Channel: channel, PID: pid, Leaf: leaf, Executable: image, Expected: localReproTargetEnv}
	bootID, err := readBootID()
	if err != nil {
		return state, err
	}
	state.BootID = bootID
	if state.StartTime, err = readStatStartTime(pid); err != nil {
		return state, err
	}
	pidStat, err := statNamespace(pid, "pid")
	if err != nil {
		return state, err
	}
	state.Identity = fmt.Sprintf("linux:v2:%s:%d:%d:%d", bootID, pidStat.Inode, pid, state.StartTime)
	var stat unix.Stat_t
	if err := unix.Stat(image, &stat); err != nil {
		return state, err
	}
	state.ExeDevice, state.ExeInode = uint64(stat.Dev), stat.Ino
	return state, nil
}

func localReproLeafRoot() string {
	root := os.Getenv(localCgroupEnv)
	if root == "" {
		root = "/sys/fs/cgroup"
	}
	return root
}

func startLocalReproReader(t *testing.T, channel, leafRoot string) (func(), error) {
	ready := filepath.Join(channel, "ready")
	if binary := os.Getenv(localReaderEnv); binary != "" {
		reader := exec.Command(binary, "serve",
			"--channel", channel, "--leaf-root", leafRoot,
			"--a-uid", strconv.Itoa(localAUID), "--a-gid", strconv.Itoa(localAGID),
			"--b-uid", strconv.Itoa(localBUID), "--b-gid", strconv.Itoa(localBGID))
		reader.Stdout, reader.Stderr = os.Stdout, os.Stderr
		if err := reader.Start(); err != nil {
			return nil, err
		}
		waited := false
		t.Cleanup(func() {
			if !waited {
				_ = reader.Process.Kill()
				_ = reader.Wait()
			}
		})
		if err := waitForLocal(t, func() (bool, error) { _, statErr := os.Stat(ready); return statErr == nil, nil }); err != nil {
			return nil, fmt.Errorf("reader binary never published its ready file: %w", err)
		}
		return func() {
			// SIGTERM is the fixture's stop: the reader treats it as a clean exit.
			if err := reader.Process.Signal(syscall.SIGTERM); err != nil {
				t.Errorf("signal reader binary SIGTERM: %v", err)
			}
			if err := reader.Wait(); err != nil {
				t.Errorf("reader binary did not stop cleanly: %v", err)
			}
			waited = true
		}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ServeOptions{
			Channel: channel, LeafRoot: leafRoot,
			AUID: localAUID, AGID: localAGID, BUID: localBUID, BGID: localBGID,
		})
	}()
	if err := waitForLocal(t, func() (bool, error) { _, statErr := os.Stat(ready); return statErr == nil, nil }); err != nil {
		cancel()
		return nil, fmt.Errorf("in-process reader never published its ready file: %w", err)
	}
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("in-process reader must stop cleanly on cancellation: %v", err)
		}
	}, nil
}

func waitForLocal(t *testing.T, condition func() (bool, error)) error {
	t.Helper()
	deadline := time.Now().Add(localTimeout)
	for time.Now().Before(deadline) {
		ready, err := condition()
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("condition did not hold within the local timeout")
}
