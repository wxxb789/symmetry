//go:build linux && amd64

package protocol

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

const (
	cloneScratchRootPrefix = "/opt/symmetry-owner-test/"
	cloneNativeHelperEnv   = "SYMMETRY_LINUX_CLONE_NATIVE_HELPER"
)

// TestInstalledLinuxOwnerCloneCheckoutScratch exercises the one-use RELEASE,
// fixed B bootstrap, real Git bundle clone, durable callback barrier, and a
// normal B-owned Git commit on an explicitly configured capability-qualified
// scratch host. B's disposable HOME contains a hostile global hook config; the
// A callback proves it did not run before checkout ACK, while the final B
// native helper proves that B can read its own config and commit afterward.
func TestInstalledLinuxOwnerCloneCheckoutScratch(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_SCRATCH") != "1" || os.Getenv("SYMMETRY_LINUX_CLONE_TEST_SCRATCH") != "1" {
		t.Skip("requires an operator-created, explicitly named clone scratch leaf and capability-qualified installed A/B launcher")
	}
	leafPath := os.Getenv("SYMMETRY_LINUX_OWNER_TEST_LEAF")
	if err := validateScratchPath(leafPath, "/sys/fs/cgroup/"); err != nil || !strings.HasPrefix(filepath.Base(leafPath), "symmetry-owner-test-") {
		t.Fatalf("clone scratch test leaf is invalid: %v", err)
	}
	inputs, err := readCloneScratchInputs()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if uint32(os.Getuid()) != owner.AUID || uint32(os.Getgid()) != owner.AGID {
		t.Fatal("clone scratch test must run as the configured A service identity")
	}
	leafFD, err := unix.Open(leafPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatalf("open operator-created clone scratch leaf: %v", err)
	}
	defer unix.Close(leafFD)
	if err := verifyCgroupLeafFD(leafFD, owner.AUID); err != nil {
		t.Fatal(err)
	}
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("clone scratch leaf must begin empty: populated=%t err=%v", populated, err)
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatalf("configure A as the exact clone leader reaper: %v", err)
	}
	t.Cleanup(func() {
		if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0); err != nil {
			t.Errorf("restore clone scratch subreaper state: %v", err)
		}
	})

	source, intent, grant := createCloneScratchSeed(t, inputs)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binaryInfo, err := os.Lstat(executable)
	if err != nil || !binaryInfo.Mode().IsRegular() || binaryInfo.Mode()&0111 == 0 {
		t.Fatalf("native clone test image is not a directly executable file: mode=%v err=%v", binaryInfo.Mode(), err)
	}
	nativeImage, err := copyCloneNativeImageForB(executable, inputs.ackRoot, intent.RunID)
	if err != nil {
		t.Fatalf("stage exact native test image for fixed B: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(nativeImage) })
	ackPath := filepath.Join(inputs.ackRoot, "checkout-ready-"+intent.RunID+".json")
	if _, err := os.Lstat(ackPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("durable clone ACK path must be new: %s err=%v", ackPath, err)
	}
	t.Cleanup(func() { _ = os.Remove(ackPath) })
	if err := requireScratchMarkerAbsent(inputs.marker); err != nil {
		t.Fatalf("B hook marker must be absent before RELEASE: %v", err)
	}

	var gate *PreparedGate
	leaderReaped := false
	t.Cleanup(func() {
		if gate == nil {
			return
		}
		if !leaderReaped && gate.PIDFD() != nil {
			_ = gate.Abort()
			if err := unix.PidfdSendSignal(int(gate.PIDFD().Fd()), unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
				t.Errorf("stop only the exact clone scratch leader by pidfd: %v", err)
			}
			if err := killScratchCloneDescendants(leafFD); err != nil {
				t.Errorf("stop only descendants in the explicitly named clone scratch leaf: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := gate.WaitLeader(ctx)
			cancel()
			if err != nil {
				t.Errorf("reap clone scratch leader by pidfd: %v", err)
			}
		}
		if err := gate.Close(); err != nil {
			t.Errorf("close clone scratch gate descriptors: %v", err)
		}
	})

	helperIdentity, err := CaptureLauncherIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("capture exact clone helper identity: %v", err)
	}
	launcherRecorded := false
	var recordedLauncher LauncherIdentity
	prepareContext, cancelPrepare := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelPrepare()
	gate, err = Prepare(prepareContext, leafFD, scratchGateProductionAuth(), func(_ context.Context, identity LauncherIdentity, pidfd *os.File) error {
		if identity.PID <= 1 || identity.StartTime == 0 || identity.BootID == "" || pidfd == nil {
			return errors.New("clone scratch root producer identity is incomplete")
		}
		if target, err := readPIDFDTarget(int(pidfd.Fd())); err != nil || target != identity.PID {
			return fmt.Errorf("clone root producer pidfd mismatch: target=%d err=%v", target, err)
		}
		recordedLauncher = identity
		launcherRecorded = true
		return nil
	})
	if err != nil {
		var unsupported *InaccessibleUserNamespaceError
		if errors.As(err, &unsupported) {
			if gate == nil || gate.PIDFD() == nil {
				t.Fatalf("unsupported nsfs host did not retain the exact gate pidfd: %v", err)
			}
			_ = gate.Abort()
			if stopErr := unix.PidfdSendSignal(int(gate.PIDFD().Fd()), unix.SIGKILL, nil, 0); stopErr != nil && !errors.Is(stopErr, unix.ESRCH) {
				t.Fatalf("stop unsupported-host clone gate only by pidfd: %v", stopErr)
			}
			waitContext, cancelWait := context.WithTimeout(context.Background(), 10*time.Second)
			_, waitErr := gate.WaitLeader(waitContext)
			cancelWait()
			if waitErr != nil {
				t.Fatalf("reap unsupported-host clone gate by pidfd: %v", waitErr)
			}
			leaderReaped = true
			if closeErr := gate.Close(); closeErr != nil {
				t.Fatalf("close unsupported-host clone gate: %v", closeErr)
			}
			gate = nil
			if populated, popErr := scratchLeafPopulated(leafFD); popErr != nil || populated {
				t.Fatalf("unsupported clone host did not empty scratch leaf: populated=%t err=%v", populated, popErr)
			}
			if markerErr := requireScratchMarkerAbsent(inputs.marker); markerErr != nil {
				t.Fatalf("unsupported clone host executed B hook: %v", markerErr)
			}
			t.Skipf("host cannot expose exact gate user-namespace nsfs identity; native clone remains fail-closed: %v", err)
		}
		t.Fatalf("prepare clone gate in capability-qualified scratch leaf: %v", err)
	}
	if gate == nil || !launcherRecorded {
		t.Fatal("clone Prepare returned without its exact parked gate and producer fence")
	}
	launcherExit, ok := gate.LauncherExitProof()
	if !ok || !launcherExit.Waited || !launcherExit.Reaped ||
		launcherExit.Process.PID != recordedLauncher.PID || launcherExit.Witness.PID != helperIdentity.PID {
		t.Fatalf("clone Prepare lacks exact waited/reaped root producer proof: %+v", launcherExit)
	}
	assertScratchLauncherExitProof(t, launcherExit, recordedLauncher, helperIdentity)
	if err := gate.VerifyLeafMembership(leafFD); err != nil {
		t.Fatalf("parked clone gate is not in the exact leaf: %v", err)
	}
	if err := assertParkedAGate(gate.Identity(), owner); err != nil {
		t.Fatalf("clone gate crossed the A-only pre-RELEASE boundary: %v", err)
	}

	nativeEnv := []string{
		"PATH=/usr/bin:/bin",
		"LANG=C",
		"LC_ALL=C",
		"HOME=" + inputs.bHome,
		"SYMMETRY_LINUX_CLONE_NATIVE_HELPER=1",
		"SYMMETRY_LINUX_CLONE_TEST_BASE_COMMIT=" + intent.BaseCommit,
		"SYMMETRY_LINUX_CLONE_TEST_TARGET=" + intent.Target,
		"SYMMETRY_LINUX_CLONE_TEST_SOURCE_PATH=" + source.path,
		"SYMMETRY_LINUX_CLONE_TEST_ACK_PATH=" + ackPath,
		"SYMMETRY_LINUX_CLONE_TEST_MARKER=" + inputs.marker,
		"SYMMETRY_LINUX_CLONE_TEST_HOOKS=" + filepath.Join(inputs.bHome, "hooks"),
	}
	pipes := newScratchNativePipes(t)
	command := NativeCommand{
		Path: nativeImage,
		Args: []string{nativeImage, "-test.run=^TestLinuxOwnerCloneNativeGitHelper$"},
		Env:  nativeEnv, Dir: intent.Target,
		Stdin: pipes.stdinRead, Stdout: pipes.stdoutWrite, Stderr: pipes.stderrWrite,
		Clone: &grant,
	}
	if err := command.Validate(); err != nil {
		t.Fatalf("validate positive clone native command: %v", err)
	}
	releaseGuard, _ := newScratchReleaseGuard(t, 3*time.Minute)
	readyRecorded := false
	var recordedReceipt authority.LinuxCloneCheckoutReceipt
	releaseContext, cancelRelease := context.WithTimeout(context.Background(), 3*time.Minute)
	releaseErr := gate.Release(releaseContext, leafFD, command, releaseGuard, func(ctx context.Context, receipt authority.LinuxCloneCheckoutReceipt) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateCloneCheckoutReceipt(grant, receipt); err != nil {
			return err
		}
		if err := requireScratchMarkerAbsent(inputs.marker); err != nil {
			return fmt.Errorf("B global Git hook ran before checkout ACK: %w", err)
		}
		if err := writeDurableCheckoutReceipt(ackPath, inputs.ackRoot, receipt); err != nil {
			return err
		}
		recordedReceipt = receipt.Clone()
		readyRecorded = true
		return nil
	})
	cancelRelease()
	if releaseErr != nil {
		t.Fatalf("one-use B clone RELEASE and durable checkout ACK/native exec: %v", releaseErr)
	}
	if !readyRecorded {
		t.Fatal("native clone exec completed without a durable checkout-ready callback")
	}
	if err := pipes.closeNativeCopies(); err != nil {
		t.Fatalf("close A copies of transferred clone stdio: %v", err)
	}
	if err := assertCloneNativeBProcess(gate.Identity(), owner, nativeImage, nativeEnv, intent.Target, grant.Intent.SeedPath, releaseGuard.OwnerPipe); err != nil {
		t.Fatalf("same-PID clone-native process is not exact B: %v", err)
	}
	if err := gate.VerifyLeafMembership(leafFD); err != nil {
		t.Fatalf("same-PID B native process left its exact scratch leaf: %v", err)
	}
	persisted, err := os.ReadFile(ackPath)
	if err != nil {
		t.Fatalf("read fsynced checkout-ready record: %v", err)
	}
	var persistedReceipt authority.LinuxCloneCheckoutReceipt
	if err := json.Unmarshal(persisted, &persistedReceipt); err != nil || !persistedReceipt.Equal(recordedReceipt) {
		t.Fatalf("durable checkout-ready record is malformed or mismatched: receipt=%+v err=%v", persistedReceipt, err)
	}
	if _, err := pipes.stdinWrite.Write([]byte{'X'}); err != nil {
		t.Fatalf("release B Git-commit helper after identity checks: %v", err)
	}
	if err := pipes.stdinWrite.Close(); err != nil {
		t.Fatalf("close B Git-commit helper stdin: %v", err)
	}
	stdout, err := io.ReadAll(pipes.stdoutRead)
	if err != nil {
		t.Fatalf("read B clone helper stdout: %v", err)
	}
	stderr, err := io.ReadAll(pipes.stderrRead)
	if err != nil {
		t.Fatalf("read B clone helper stderr: %v", err)
	}
	commitLine := ""
	for _, line := range strings.Split(string(stdout), "\n") {
		if strings.HasPrefix(line, "B_COMMIT=") {
			commitLine = strings.TrimPrefix(line, "B_COMMIT=")
		}
	}
	if !strings.Contains(string(stdout), "B_READY=checkout-ack-observed") || len(commitLine) != 40 ||
		strings.Trim(commitLine, "0123456789abcdef") != "" || commitLine == intent.BaseCommit {
		t.Fatalf("B native helper did not report its real Git commit: stdout=%q stderr=%q", stdout, stderr)
	}
	marker, err := os.ReadFile(inputs.marker)
	if err != nil || !strings.Contains(string(marker), "post-commit") || strings.Contains(string(marker), "post-checkout") {
		t.Fatalf("B's global Git config was not isolated to its native commit: marker=%q err=%v", marker, err)
	}
	if got := runCloneTestGitAsA(t, source.path, "rev-parse", "HEAD"); got != intent.BaseCommit {
		t.Fatalf("A source repository changed during B clone/commit: got=%s want=%s", got, intent.BaseCommit)
	}
	waitContext, cancelWait := context.WithTimeout(context.Background(), 15*time.Second)
	if _, err := gate.WaitLeader(waitContext); err != nil {
		cancelWait()
		t.Fatalf("reap same-PID B clone native process by pidfd: %v", err)
	}
	cancelWait()
	leaderReaped = true
	if err := gate.Close(); err != nil {
		t.Fatalf("close reaped B clone gate: %v", err)
	}
	gate = nil
	if populated, err := scratchLeafPopulated(leafFD); err != nil || populated {
		t.Fatalf("B clone native exit did not empty scratch leaf: populated=%t err=%v", populated, err)
	}
	if empty, err := scratchLeafProcsEmpty(leafFD); err != nil || !empty {
		t.Fatalf("B clone native exit left a scratch PID: empty=%t err=%v", empty, err)
	}
}

func TestLinuxOwnerCloneNativeGitHelper(t *testing.T) {
	if os.Getenv(cloneNativeHelperEnv) != "1" {
		t.Skip("internal post-checkout B native clone witness")
	}
	if os.Getuid() != 1 || os.Geteuid() != 1 || os.Getgid() != 1 || os.Getegid() != 1 {
		t.Fatal("clone Git helper did not run under namespace B")
	}
	target, err := os.Getwd()
	if err != nil || target != os.Getenv("SYMMETRY_LINUX_CLONE_TEST_TARGET") {
		t.Fatalf("clone helper cwd differs from B target: cwd=%q err=%v", target, err)
	}
	if _, err := os.Stat(os.Getenv("SYMMETRY_LINUX_CLONE_TEST_SOURCE_PATH")); !errors.Is(err, syscall.EACCES) && !errors.Is(err, syscall.EPERM) {
		t.Fatalf("fixed B can read the A-only Git source path: err=%v", err)
	}
	baseCommit := os.Getenv("SYMMETRY_LINUX_CLONE_TEST_BASE_COMMIT")
	if len(baseCommit) != 40 || strings.Trim(baseCommit, "0123456789abcdef") != "" {
		t.Fatal("clone helper base OID is invalid")
	}
	ackData, err := os.ReadFile(os.Getenv("SYMMETRY_LINUX_CLONE_TEST_ACK_PATH"))
	if err != nil {
		t.Fatalf("B cannot read A's fsynced checkout-ready record: %v", err)
	}
	var receipt authority.LinuxCloneCheckoutReceipt
	if err := json.Unmarshal(ackData, &receipt); err != nil || receipt.Validate() != nil || receipt.Target != target || receipt.BaseCommit != baseCommit {
		t.Fatalf("B received no exact durable checkout ACK before native start: receipt=%+v err=%v", receipt, err)
	}
	gitDir, err := os.Lstat(filepath.Join(target, ".git"))
	if err != nil {
		t.Fatalf("B checkout lacks a normal .git directory: %v", err)
	}
	if !gitDir.IsDir() {
		t.Fatalf("B checkout .git entry is not a real directory: mode=%v", gitDir.Mode())
	}
	gitStat, ok := gitDir.Sys().(*syscall.Stat_t)
	if !ok || gitStat.Uid != 1 || gitStat.Gid != 1 {
		t.Fatalf("B does not own its .git metadata: stat=%#v", gitDir.Sys())
	}
	for _, name := range []string{filepath.Join(".git", "commondir"), filepath.Join(".git", "worktrees"), filepath.Join(".git", "objects", "info", "alternates")} {
		if _, err := os.Lstat(filepath.Join(target, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("B checkout contains linked Git metadata %q: %v", name, err)
		}
	}
	marker := os.Getenv("SYMMETRY_LINUX_CLONE_TEST_MARKER")
	if err := requireScratchMarkerAbsent(marker); err != nil {
		t.Fatalf("bootstrap ran a B-owned global hook before checkout ACK: %v", err)
	}
	fmt.Fprintln(os.Stdout, "B_READY=checkout-ack-observed")
	var release [1]byte
	if _, err := io.ReadFull(os.Stdin, release[:]); err != nil || release[0] != 'X' {
		t.Fatalf("wait for A to finish same-PID B identity proof: byte=%q err=%v", release[0], err)
	}
	if got := runCloneTestGitCurrent(t, target, "rev-parse", "HEAD"); got != baseCommit {
		t.Fatalf("standalone B clone did not start at the admitted OID: got=%s want=%s", got, baseCommit)
	}
	hooks := os.Getenv("SYMMETRY_LINUX_CLONE_TEST_HOOKS")
	if got := runCloneTestGitCurrent(t, target, "config", "--global", "--get", "core.hooksPath"); got != hooks {
		t.Fatalf("B global Git config was not available to the native process: hooks=%q got=%q", hooks, got)
	}
	if got := runCloneTestGitCurrent(t, target, "config", "--global", "--get", "user.name"); got != "Symmetry B clone fixture" {
		t.Fatalf("B global Git identity config was not available to native Git: %q", got)
	}
	if got := runCloneTestGitCurrent(t, target, "config", "--global", "--get", "user.email"); got != "b-clone@example.invalid" {
		t.Fatalf("B global Git email config was not available to native Git: %q", got)
	}
	if err := os.WriteFile(filepath.Join(target, "b-change.txt"), []byte("created by fixed B\n"), 0o600); err != nil {
		t.Fatalf("write ordinary B-owned Git change: %v", err)
	}
	runCloneTestGitCurrent(t, target, "add", "--", "b-change.txt")
	runCloneTestGitCurrent(t, target, "commit", "-m", "B-owned standalone clone commit")
	commit := runCloneTestGitCurrent(t, target, "rev-parse", "HEAD")
	if commit == baseCommit || len(commit) != 40 {
		t.Fatalf("B did not create a new real Git commit: base=%s current=%s", baseCommit, commit)
	}
	markerData, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(markerData), "post-commit") || strings.Contains(string(markerData), "post-checkout") {
		t.Fatalf("B global post-commit hook did not run only in B native code: marker=%q err=%v", markerData, err)
	}
	fmt.Printf("B_COMMIT=%s\n", commit)
}

type cloneScratchInputs struct {
	root       string
	seedRoot   string
	sourceRoot string
	bHome      string
	ackRoot    string
	marker     string
}

func readCloneScratchInputs() (cloneScratchInputs, error) {
	inputs := cloneScratchInputs{
		root:       os.Getenv("SYMMETRY_LINUX_CLONE_TEST_ROOT"),
		seedRoot:   os.Getenv("SYMMETRY_LINUX_CLONE_TEST_SEED_ROOT"),
		sourceRoot: os.Getenv("SYMMETRY_LINUX_CLONE_TEST_SOURCE_ROOT"),
		bHome:      os.Getenv("SYMMETRY_LINUX_CLONE_TEST_B_HOME"),
		ackRoot:    os.Getenv("SYMMETRY_LINUX_CLONE_TEST_ACK_ROOT"),
		marker:     os.Getenv("SYMMETRY_LINUX_CLONE_TEST_MARKER"),
	}
	for _, candidate := range []struct{ name, path string }{
		{"B clone root", inputs.root}, {"A seed root", inputs.seedRoot}, {"A source root", inputs.sourceRoot},
		{"B disposable HOME", inputs.bHome}, {"A ACK root", inputs.ackRoot}, {"B hook marker", inputs.marker},
	} {
		if err := validateScratchPath(candidate.path, cloneScratchRootPrefix); err != nil {
			return cloneScratchInputs{}, fmt.Errorf("%s: %w", candidate.name, err)
		}
	}
	paths := []string{inputs.root, inputs.seedRoot, inputs.sourceRoot, inputs.bHome, inputs.ackRoot}
	for left := 0; left < len(paths); left++ {
		for right := left + 1; right < len(paths); right++ {
			if cloneTestPathsOverlap(paths[left], paths[right]) {
				return cloneScratchInputs{}, fmt.Errorf("scratch paths overlap: %q and %q", paths[left], paths[right])
			}
		}
		if cloneTestPathsOverlap(paths[left], inputs.marker) {
			return cloneScratchInputs{}, fmt.Errorf("scratch marker overlaps configured path %q", paths[left])
		}
	}
	for _, candidate := range []struct{ name, path string }{
		{"A seed root", inputs.seedRoot}, {"A source root", inputs.sourceRoot}, {"A ACK root", inputs.ackRoot},
	} {
		info, err := os.Stat(candidate.path)
		if err != nil {
			return cloneScratchInputs{}, fmt.Errorf("stat %s: %w", candidate.name, err)
		}
		if !info.IsDir() {
			return cloneScratchInputs{}, fmt.Errorf("%s is not a directory", candidate.name)
		}
	}
	return inputs, nil
}

func cloneTestPathsOverlap(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+string(filepath.Separator)) ||
		strings.HasPrefix(right, left+string(filepath.Separator))
}

func killScratchCloneDescendants(leafFD int) error {
	fd, err := unix.Openat(leafFD, "cgroup.kill", unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open exact scratch leaf cgroup.kill: %w", err)
	}
	n, writeErr := unix.Write(fd, []byte("1\n"))
	closeErr := unix.Close(fd)
	if writeErr != nil || n != 2 || closeErr != nil {
		return errors.Join(writeErr, closeErr, func() error {
			if n != 2 {
				return io.ErrShortWrite
			}
			return nil
		}())
	}
	return nil
}

func validateScratchPath(path, prefix string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !strings.HasPrefix(path, prefix) ||
		path == strings.TrimSuffix(prefix, string(filepath.Separator)) {
		return errors.New("path must be normalized, absolute, and below the disposable scratch root")
	}
	return nil
}

func createCloneScratchSeed(t *testing.T, inputs cloneScratchInputs) (cloneScratchSource, authority.LinuxCloneIntent, authority.LinuxCloneGrant) {
	t.Helper()
	sourceDir, err := os.MkdirTemp(inputs.sourceRoot, "source-")
	if err != nil {
		t.Fatalf("create A-private clone source repository: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sourceDir) })
	if err := os.Chmod(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	runCloneTestGitAsA(t, sourceDir, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("A-owned admitted base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCloneTestGitAsA(t, sourceDir, "add", "--", "README.md")
	runCloneTestGitAsA(t, sourceDir, "-c", "user.name=Symmetry A clone fixture", "-c", "user.email=owner-test@example.invalid", "commit", "--quiet", "-m", "admitted base")
	baseCommit := runCloneTestGitAsA(t, sourceDir, "rev-parse", "HEAD")
	baseTreeDigest := cloneScratchTreeDigest([]byte("A-owned admitted base\n"))
	// Each invocation uses a unique RunKey/ref so the source pin is never
	// overwritten by a different scratch attempt.
	runID := "clone-scratch-" + randomLowerHex(t, 12)
	baseRef, err := authority.LinuxCloneBaseRef(runID, 1)
	if err != nil {
		t.Fatal(err)
	}
	runCloneTestGitAsA(t, sourceDir, "update-ref", baseRef, baseCommit)
	seedParent := filepath.Join(inputs.seedRoot, "binding-clone-scratch", "run-"+runID)
	if err := os.MkdirAll(seedParent, 0o750); err != nil {
		t.Fatalf("create A-owned seed parent: %v", err)
	}
	seedPath := filepath.Join(seedParent, "generation-1.bundle")
	t.Cleanup(func() {
		_ = os.Remove(seedPath)
		_ = os.Remove(seedParent)
		_ = os.Remove(filepath.Dir(seedParent))
	})
	runCloneTestGitAsA(t, sourceDir, "bundle", "create", seedPath, baseRef)
	runCloneTestGitAsA(t, sourceDir, "bundle", "verify", seedPath)
	if err := os.Chmod(seedPath, 0o440); err != nil {
		t.Fatalf("make A bundle immutable and B-read-only: %v", err)
	}
	bundleFile, err := os.Open(seedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := bundleFile.Sync(); err != nil {
		_ = bundleFile.Close()
		t.Fatalf("sync immutable A bundle: %v", err)
	}
	bundleInfo, err := bundleFile.Stat()
	closeErr := bundleFile.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("stat/close immutable A bundle: stat=%v close=%v", err, closeErr)
	}
	if bundleInfo.Size() <= 0 || bundleInfo.Size() > authority.LinuxCloneMaxSeedBytes {
		t.Fatalf("scratch A bundle size is outside production bound: %d", bundleInfo.Size())
	}
	bundleDigest, err := hashCloneTestFile(seedPath)
	if err != nil {
		t.Fatal(err)
	}
	seedStat, ok := bundleInfo.Sys().(*syscall.Stat_t)
	if !ok || seedStat.Ino == 0 {
		t.Fatal("scratch A bundle stat has no stable device/inode identity")
	}
	seedParentFD, err := unix.Open(seedParent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Fsync(seedParentFD); err != nil {
		_ = unix.Close(seedParentFD)
		t.Fatalf("sync A seed parent: %v", err)
	}
	_ = unix.Close(seedParentFD)

	bindingKey := "clone-scratch"
	generation := int64(1)
	nonce := randomLowerHex(t, authority.LinuxCloneNonceBytes)
	target := filepath.Join(inputs.root, "binding-"+bindingKey, "run-"+runID, "generation-1")
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: runID, Generation: generation,
		BindingKey: bindingKey, ResourceID: "00000000-0000-4000-8000-000000000001",
		BaseCommit: baseCommit, BaseTreeDigest: baseTreeDigest,
		Nonce: nonce, SeedPath: seedPath, Target: target,
	}
	if err := intent.Validate(); err != nil {
		t.Fatalf("validate scratch Linux clone intent: %v", err)
	}
	seedReceipt := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: baseCommit, Digest: bundleDigest,
		Size: bundleInfo.Size(), Device: uint64(seedStat.Dev), Inode: seedStat.Ino,
	}
	if err := seedReceipt.Validate(); err != nil {
		t.Fatalf("validate scratch immutable seed receipt: %v", err)
	}
	grant := authority.LinuxCloneGrant{Intent: intent, Seed: seedReceipt}
	if err := grant.Validate(); err != nil {
		t.Fatalf("validate scratch Linux clone grant: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(seedPath) })
	return cloneScratchSource{path: sourceDir}, intent, grant
}

type cloneScratchSource struct{ path string }

func randomLowerHex(t *testing.T, size int) string {
	t.Helper()
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		t.Fatalf("generate scratch clone identity: %v", err)
	}
	return hex.EncodeToString(value)
}

func hashCloneTestFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, authority.LinuxCloneMaxSeedBytes+1)); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func copyCloneNativeImageForB(source, directory, runID string) (string, error) {
	destination := filepath.Join(directory, "native-test-"+runID)
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	before, err := input.Stat()
	if err != nil {
		return "", fmt.Errorf("stat native test source: %w", err)
	}
	if !before.Mode().IsRegular() {
		return "", errors.New("native test source is not a regular file")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return "", err
	}
	copyBytes, copyErr := io.Copy(output, input)
	chmodErr := output.Chmod(0o755)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || chmodErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return "", errors.Join(copyErr, chmodErr, syncErr, closeErr)
	}
	if copyBytes != before.Size() {
		_ = os.Remove(destination)
		return "", errors.New("native test image copy size differs from its opened source")
	}
	dirFD, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		_ = os.Remove(destination)
		return "", err
	}
	syncDirErr := unix.Fsync(dirFD)
	closeDirErr := unix.Close(dirFD)
	if syncDirErr != nil || closeDirErr != nil {
		_ = os.Remove(destination)
		return "", errors.Join(syncDirErr, closeDirErr)
	}
	return destination, nil
}

func cloneScratchTreeDigest(content []byte) string {
	blobDigest := sha256.Sum256(content)
	manifest := []byte("100644\x00blob\x00README.md\x00sha256:" + hex.EncodeToString(blobDigest[:]) + "\x00")
	treeDigest := sha256.Sum256(manifest)
	return "sha256:" + hex.EncodeToString(treeDigest[:])
}

func runCloneTestGitAsA(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("/usr/bin/git", args...)
	command.Dir = directory
	command.Env = []string{
		"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "HOME=/nonexistent",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_REPLACE_OBJECTS=1",
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("A source-only Git command %q failed: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func runCloneTestGitCurrent(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("/usr/bin/git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("B native Git command %q failed: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func assertCloneNativeBProcess(identity ProcessIdentity, config OwnerConfig, executable string, expectedEnv []string,
	target, seedPath string, ownerPipe *os.File) error {
	if start, err := readProcessStartTime(identity.PID); err != nil || start != identity.StartTime {
		return fmt.Errorf("clone native PID/start identity changed: start=%d err=%v", start, err)
	}
	status, err := readProcessStatus(identity.PID)
	if err != nil {
		return err
	}
	if status.uids != [4]uint32{config.BUID, config.BUID, config.BUID, config.BUID} ||
		status.gids != [4]uint32{config.BGID, config.BGID, config.BGID, config.BGID} || len(status.groups) != 0 ||
		status.capEff != 0 || status.capPrm != 0 || status.capInh != 0 || status.capBnd != 0 || status.capAmb != 0 || status.noNewPrivs != 1 {
		return fmt.Errorf("clone native credential/capability state differs: uid=%v gid=%v groups=%v nnp=%d", status.uids, status.gids, status.groups, status.noNewPrivs)
	}
	if err := verifyOnlyFixedBMaps(identity.PID, config); err != nil {
		return err
	}
	actual, err := os.Stat(fmt.Sprintf("/proc/%d/exe", identity.PID))
	if err != nil {
		return err
	}
	want, err := os.Stat(executable)
	if err != nil {
		return err
	}
	actualStat, actualOK := actual.Sys().(*syscall.Stat_t)
	wantStat, wantOK := want.Sys().(*syscall.Stat_t)
	if !actualOK || !wantOK || actualStat.Dev != wantStat.Dev || actualStat.Ino != wantStat.Ino {
		return errors.New("clone native process is not the approved same-PID test image")
	}
	environment, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", identity.PID))
	if err != nil {
		return err
	}
	wantedEnvironment := []byte(strings.Join(expectedEnv, "\x00"))
	if len(expectedEnv) != 0 {
		wantedEnvironment = append(wantedEnvironment, 0)
	}
	if !bytes.Equal(environment, wantedEnvironment) {
		return errors.New("clone native environment differs from the exact RELEASE vector")
	}
	ownerTarget, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", ownerPipe.Fd()))
	if err != nil {
		return fmt.Errorf("read A owner-pipe identity for FD-leak check: %w", err)
	}
	fdDir := fmt.Sprintf("/proc/%d/fd", identity.PID)
	stdioTargets := make(map[string]struct{}, 3)
	for fd := 0; fd <= 2; fd++ {
		name, err := os.Readlink(filepath.Join(fdDir, strconv.Itoa(fd)))
		if err != nil {
			return fmt.Errorf("read clone native stdio fd %d: %w", fd, err)
		}
		stdioTargets[name] = struct{}{}
	}
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		fd, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil {
			return errors.New("clone native FD table contains a nonnumeric descriptor")
		}
		if fd <= 2 {
			continue
		}
		name, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			return err
		}
		if _, duplicateStdio := stdioTargets[name]; duplicateStdio {
			return fmt.Errorf("clone native process retained an extra stdio pipe descriptor %s -> %q", entry.Name(), name)
		}
		if name == ownerTarget || name == "/usr/bin/git" ||
			strings.Contains(name, "symmetry-linux-clone-bootstrap") ||
			strings.Contains(name, "symmetry-clone-request") || name == seedPath ||
			name == target || strings.HasPrefix(name, target+string(filepath.Separator)) ||
			strings.Contains(name, "cgroup") || name == "anon_inode:[pidfd]" || strings.HasPrefix(name, "socket:[") {
			return fmt.Errorf("clone native process inherited bootstrap authority FD %s -> %q", entry.Name(), name)
		}
	}
	return nil
}

func writeDurableCheckoutReceipt(path, directory string, receipt authority.LinuxCloneCheckoutReceipt) error {
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	writeErr := func() error {
		n, err := file.Write(encoded)
		if err != nil {
			return err
		}
		if n != len(encoded) {
			return io.ErrShortWrite
		}
		if err := file.Chmod(0o644); err != nil {
			return err
		}
		return file.Sync()
	}()
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return errors.Join(writeErr, closeErr)
	}
	dirFD, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	syncErr := unix.Fsync(dirFD)
	closeDirErr := unix.Close(dirFD)
	return errors.Join(syncErr, closeDirErr)
}
