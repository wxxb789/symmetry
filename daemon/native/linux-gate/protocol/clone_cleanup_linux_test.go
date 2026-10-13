//go:build linux && amd64

package protocol

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	taskprotocol "github.com/wxxb789/symmetry/daemon/internal/protocol"
	"golang.org/x/sys/unix"
)

func TestRemoveBCloneAfterReceiptRejectsMissingStopReceiptBeforeLauncher(t *testing.T) {
	root := t.TempDir()
	intent := cleanupTestIntent(t, root, "cleanup-no-stop-proof")
	grant := cleanupTestGrant(intent)
	if err := grant.Validate(); err != nil {
		t.Fatalf("test grant is invalid: %v", err)
	}
	cleanup := cleanupTestIntentAuthority(t, intent, grant.Seed)
	if err := cleanup.Validate(); err == nil {
		t.Fatal("test cleanup intent unexpectedly has a durable normal StopReceipt")
	}
	marker := filepath.Join(root, "target-must-remain")
	if err := os.WriteFile(marker, []byte("no cleanup without durable stop proof\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)
	if _, err := RemoveBCloneAfterReceipt(context.Background(), rootFD, -1, grant, cleanup, nil); err == nil {
		t.Fatal("root cleanup was authorized without a valid exact normal StopReceipt and release proof")
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "no cleanup without durable stop proof\n" {
		t.Fatalf("unauthorized cleanup changed target state: content=%q err=%v", content, err)
	}
}

// TestInstalledLinuxOwnerCloneCleanupScratch exercises the actual setuid
// wrapper/core path only on a root-fixtured disposable clone. Its release
// intent is structural test data, not a claim that this test obtained a real
// v2 physical StopReceipt; the credentialed owner-run E2E supplies that proof.
func TestInstalledLinuxOwnerCloneCleanupScratch(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_SCRATCH") != "1" ||
		os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_ROOT") == "" ||
		os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_SIBLING") == "" {
		t.Skip("requires a root-preprovisioned B-owned cleanup scratch clone and installed setuid wrapper")
	}
	owner, err := ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if uint32(os.Getuid()) != owner.AUID || uint32(os.Getgid()) != owner.AGID {
		t.Fatal("cleanup smoke test must run as the configured A identity")
	}
	rootPath := os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_ROOT")
	siblingPath := os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_SIBLING")
	if err := validateCleanupScratchPath(rootPath); err != nil {
		t.Fatalf("cleanup scratch root is invalid: %v", err)
	}
	if err := validateCleanupScratchPath(siblingPath); err != nil {
		t.Fatalf("unrelated cleanup sibling is invalid: %v", err)
	}
	runID, bindingKey := "cleanup-scratch", "cleanup-binding"
	intent := cleanupTestIntentWithBinding(t, rootPath, runID, bindingKey, 1)
	targetPath := intent.Target
	if configuredTarget := os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_TARGET"); configuredTarget != "" && configuredTarget != targetPath {
		t.Fatalf("preprovisioned target differs from exact RunKey target: got=%q want=%q", configuredTarget, targetPath)
	}
	rootFD, err := openCleanupTestDirectory(rootPath)
	if err != nil {
		t.Fatalf("open pinned B clone root as A: %v", err)
	}
	defer unix.Close(rootFD)
	targetFD, err := openCleanupTestDirectory(targetPath)
	if err != nil {
		t.Fatalf("open exact B checkout O_PATH as A: %v", err)
	}
	defer unix.Close(targetFD)
	var rootStat, targetStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fstat(targetFD, &targetStat); err != nil {
		t.Fatal(err)
	}
	if rootStat.Uid != owner.BUID || rootStat.Gid != owner.BGID || targetStat.Uid != owner.BUID || targetStat.Gid != owner.BGID ||
		rootStat.Mode&unix.S_IFMT != unix.S_IFDIR || targetStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		t.Fatalf("scratch root/target are not B-owned directories: root=%+v target=%+v", rootStat, targetStat)
	}
	// The inherited checkout pin is O_PATH; this nested file ensures the root
	// protocol must enumerate through its separate readable directory pin.
	for _, relative := range []string{".git", ".git/objects", ".git/objects/pack", "nested", "nested/deep"} {
		info, err := os.Lstat(filepath.Join(targetPath, relative))
		if err != nil || !info.IsDir() {
			t.Fatalf("root fixture must contain a real nested directory %q: info=%v err=%v", relative, info, err)
		}
	}
	probe := filepath.Join(targetPath, ".git", "config")
	probeInfo, err := os.Lstat(probe)
	if err != nil || !probeInfo.Mode().IsRegular() {
		t.Fatalf("root fixture must contain the A-unlink-denied Git config probe: %v", err)
	}
	for _, relative := range []string{".git/objects/pack/pack-test", "nested/deep/file"} {
		info, err := os.Lstat(filepath.Join(targetPath, relative))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("root fixture must contain a real nested file %q: info=%v err=%v", relative, info, err)
		}
	}
	if err := os.Remove(probe); err == nil || (!errors.Is(err, unix.EACCES) && !errors.Is(err, unix.EPERM)) {
		t.Fatalf("A direct unlink of B-owned .git/config was not denied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(siblingPath, "keep")); err != nil {
		t.Fatalf("unrelated sibling marker must exist before cleanup: %v", err)
	}
	grant, cleanup := cleanupTestGrantAndIntent(t, intent, owner, targetStat)
	cleanupContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	proof, err := RemoveBCloneAfterReceipt(cleanupContext, rootFD, targetFD, grant, cleanup, nil)
	cancel()
	if err != nil {
		t.Fatalf("installed root clone cleanup failed: %v", err)
	}
	anchor, err := CaptureLauncherIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	expectedAnchor := authority.LinuxCgroupProcessIdentity{PID: anchor.PID, Identity: anchor.Identity}
	if err := proof.Validate(); err != nil || proof.Disposition != authority.LinuxCloneTargetDispositionRemoved ||
		proof.TargetDevice != cleanup.CheckoutReceipt.TargetDevice || proof.TargetInode != cleanup.CheckoutReceipt.TargetInode ||
		!proof.RootDeleterExit.ValidFor(proof.RootDeleter, expectedAnchor) {
		t.Fatalf("root result is not the exact target removal and anchor wait/reap proof: proof=%+v err=%v", proof, err)
	}
	if _, err := os.Lstat(targetPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("B target remains after exact root removal: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(siblingPath, "keep")); err != nil || len(content) == 0 {
		t.Fatalf("root cleanup changed an unrelated sibling: bytes=%d err=%v", len(content), err)
	}
	// This is an in-memory structural replay fixture, not evidence that State
	// durably recorded the prior proof; production Workspace passes State readback.
	if _, err := RemoveBCloneAfterReceipt(context.Background(), rootFD, -1, grant, cleanup, nil); !errors.Is(err, ErrCloneCleanupOutcomeUnknown) {
		t.Fatalf("absent target without a Store-bound prior removal proof was not unresolved: %v", err)
	}
	replayContext, cancelReplay := context.WithTimeout(context.Background(), 3*time.Minute)
	replay, err := RemoveBCloneAfterReceipt(replayContext, rootFD, -1, grant, cleanup, &proof)
	cancelReplay()
	if err != nil {
		t.Fatalf("replay exact Begin-authorized target absence after lost response: %v", err)
	}
	if !replay.Equal(proof) || replay.Disposition != authority.LinuxCloneTargetDispositionRemoved ||
		!replay.RootDeleterExit.ValidFor(replay.RootDeleter, expectedAnchor) {
		t.Fatalf("absent-target replay did not return the exact prior removal proof: got=%+v want=%+v", replay, proof)
	}
	t.Log("structural normal-release fixture only; this smoke does not claim a real v2 physical StopReceipt")
}

const cleanupParentDeathWorkerEnv = "SYMMETRY_LINUX_OWNER_CLEANUP_PARENT_DEATH_WORKER"

// TestInstalledLinuxOwnerCloneCleanupParentDeathScratch kills the A process
// after the root worker deletes the first sorted entry but while later entries
// remain. The helper is a direct child of that A process; its PDEATHSIG must
// stop it. Its cleanup authority is structural fixture data, not a real v2
// physical StopReceipt. The fixture is disposable and may remain partially
// deleted for the root provisioner to reset.
func TestInstalledLinuxOwnerCloneCleanupParentDeathScratch(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_SCRATCH") != "1" ||
		os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_ROOT") == "" ||
		os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_SIBLING") == "" ||
		os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_PARENT_DEATH_TARGET") == "" {
		t.Skip("requires a root-preprovisioned parent-death cleanup target on a disposable Linux host")
	}
	owner, err := ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if uint32(os.Getuid()) != owner.AUID || uint32(os.Getgid()) != owner.AGID {
		t.Fatal("parent-death cleanup smoke must run as configured A")
	}
	rootPath := os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_ROOT")
	targetPath := os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_PARENT_DEATH_TARGET")
	if err := validateCleanupScratchPath(rootPath); err != nil {
		t.Fatal(err)
	}
	intent := cleanupTestIntentWithBinding(t, rootPath, "cleanup-parent-death", "cleanup-binding", 1)
	if targetPath != intent.Target {
		t.Fatalf("parent-death target differs from exact RunKey path: got=%q want=%q", targetPath, intent.Target)
	}
	first := filepath.Join(targetPath, "--cleanup-first")
	later := filepath.Join(targetPath, "z-later-000000")
	if _, err := os.Lstat(first); err != nil {
		t.Fatalf("root fixture lacks the first sorted cleanup entry: %v", err)
	}
	if _, err := os.Lstat(later); err != nil {
		t.Fatalf("root fixture lacks the later entry needed to keep the deleter busy: %v", err)
	}
	inotifyFD, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatalf("initialize target deletion witness: %v", err)
	}
	defer unix.Close(inotifyFD)
	if _, err := unix.InotifyAddWatch(inotifyFD, targetPath, unix.IN_DELETE); err != nil {
		t.Fatalf("watch exact B target directory before cleanup: %v", err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestLinuxOwnerCleanupParentDeathWorker$")
	command.Env = append(os.Environ(), cleanupParentDeathWorkerEnv+"=1")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	workerPID := command.Process.Pid
	workerWaited := false
	t.Cleanup(func() {
		if !workerWaited && command.Process != nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	})
	rootPID, err := waitForDirectCleanupChild(workerPID, 45*time.Second)
	if err != nil {
		t.Fatalf("observe direct transient root cleanup child: %v", err)
	}
	rootPIDFD, err := unix.PidfdOpen(rootPID, 0)
	if err != nil {
		t.Fatalf("pin root deleter before A parent death: %v", err)
	}
	defer unix.Close(rootPIDFD)
	if pidfdReady(rootPIDFD) {
		t.Fatal("root deleter exited before the parent-death boundary was exercised")
	}
	if err := waitForNamedDeletion(inotifyFD, "--cleanup-first", 45*time.Second); err != nil {
		t.Fatalf("root deleter did not reach the partial-effect boundary: %v", err)
	}
	if _, err := os.Lstat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("exact first entry remains after its delete event: %v", err)
	}
	if pidfdReady(rootPIDFD) {
		t.Fatal("root deleter completed before A parent death could fence it")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill only the A parent of the root cleanup worker: %v", err)
	}
	_, waitErr := command.Process.Wait()
	workerWaited = true
	if waitErr == nil {
		t.Fatal("A cleanup caller exited without the requested parent-death signal")
	}
	if err := waitForPIDFDExit(rootPIDFD, 10*time.Second); err != nil {
		t.Fatalf("root cleanup worker survived its exact A parent: %v", err)
	}
	if _, err := os.Lstat(targetPath); err != nil {
		t.Fatalf("parent-death fence removed or lost the exact B target unexpectedly: %v", err)
	}
	if _, err := os.Lstat(later); err != nil {
		t.Fatalf("parent-death fence did not stop the in-flight deleter before later entries: %v", err)
	}
}

func TestLinuxOwnerCleanupParentDeathWorker(t *testing.T) {
	if os.Getenv(cleanupParentDeathWorkerEnv) != "1" {
		t.Skip("launched only by the installed parent-death cleanup scratch test")
	}
	owner, err := ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	rootPath := os.Getenv("SYMMETRY_LINUX_OWNER_CLEANUP_ROOT")
	rootFD, err := openCleanupTestDirectory(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)
	intent := cleanupTestIntentWithBinding(t, rootPath, "cleanup-parent-death", "cleanup-binding", 1)
	targetFD, err := openCleanupTestDirectory(intent.Target)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(targetFD)
	var target unix.Stat_t
	if err := unix.Fstat(targetFD, &target); err != nil {
		t.Fatal(err)
	}
	grant, cleanup := cleanupTestGrantAndIntent(t, intent, owner, target)
	_, err = RemoveBCloneAfterReceipt(context.Background(), rootFD, targetFD, grant, cleanup, nil)
	t.Fatalf("parent test killed A cleanup owner but root call returned: %v", err)
}

func waitForDirectCleanupChild(parentPID int, timeout time.Duration) (int, error) {
	parentPIDFD, err := unix.PidfdOpen(parentPID, 0)
	if err != nil {
		return 0, fmt.Errorf("open cleanup caller pidfd: %w", err)
	}
	defer unix.Close(parentPIDFD)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tasks, readErr := os.ReadDir(fmt.Sprintf("/proc/%d/task", parentPID))
		if readErr == nil {
			for _, task := range tasks {
				data, readErr := os.ReadFile(filepath.Join("/proc", strconv.Itoa(parentPID), "task", task.Name(), "children"))
				if readErr != nil {
					continue
				}
				for _, field := range strings.Fields(string(data)) {
					pid, parseErr := strconv.Atoi(field)
					if parseErr == nil && pid > 1 {
						return pid, nil
					}
				}
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if remaining > 10*time.Millisecond {
			remaining = 10 * time.Millisecond
		}
		fds := []unix.PollFd{{Fd: int32(parentPIDFD), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		count, pollErr := unix.Poll(fds, int((remaining+time.Millisecond-1)/time.Millisecond))
		if pollErr != nil && !errors.Is(pollErr, unix.EINTR) {
			return 0, fmt.Errorf("wait for cleanup caller while locating root child: %w", pollErr)
		}
		if count != 0 {
			return 0, errors.New("A cleanup caller exited before root child was identified")
		}
	}
	return 0, errors.New("direct A cleanup child did not appear")
}

func waitForNamedDeletion(inotifyFD int, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	buffer := make([]byte, 32<<10)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		fds := []unix.PollFd{{Fd: int32(inotifyFD), Events: unix.POLLIN | unix.POLLERR | unix.POLLHUP}}
		count, err := unix.Poll(fds, int((remaining+time.Millisecond-1)/time.Millisecond))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count == 0 {
			continue
		}
		n, err := unix.Read(inotifyFD, buffer)
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
			continue
		}
		if err != nil {
			return err
		}
		for offset := 0; offset+16 <= n; {
			mask := binary.LittleEndian.Uint32(buffer[offset+4 : offset+8])
			nameSize := int(binary.LittleEndian.Uint32(buffer[offset+12 : offset+16]))
			end := offset + 16 + nameSize
			if nameSize < 0 || end > n {
				return errors.New("cleanup inotify event is truncated")
			}
			eventName := strings.TrimRight(string(buffer[offset+16:end]), "\x00")
			if mask&unix.IN_DELETE != 0 && eventName == name {
				return nil
			}
			offset = end
		}
	}
	return errors.New("exact cleanup unlink event did not arrive before its bounded deadline")
}

func pidfdReady(pidfd int) bool {
	fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	n, err := unix.Poll(fds, 0)
	return err == nil && n != 0
}

func waitForPIDFDExit(pidfd int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pidfdReady(pidfd) {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		count, err := unix.Poll(fds, int((remaining+time.Millisecond-1)/time.Millisecond))
		if err != nil && !errors.Is(err, unix.EINTR) {
			return err
		}
		if count != 0 {
			return nil
		}
	}
	return errors.New("pidfd did not signal exact root cleanup worker exit")
}

func cleanupTestIntent(t *testing.T, root, runID string) authority.LinuxCloneIntent {
	t.Helper()
	return cleanupTestIntentWithBinding(t, root, runID, "cleanup-binding", 1)
}

func cleanupTestIntentWithBinding(t *testing.T, root, runID, bindingKey string, generation int64) authority.LinuxCloneIntent {
	t.Helper()
	intent := authority.LinuxCloneIntent{
		Version: authority.LinuxCloneVersion, RunID: runID, Generation: generation,
		BindingKey: bindingKey, ResourceID: "00000000-0000-4000-8000-000000000001",
		BaseCommit: strings.Repeat("a", 40), BaseTreeDigest: "sha256:" + strings.Repeat("b", 64),
		Nonce:    strings.Repeat("c", authority.LinuxCloneNonceBytes*2),
		SeedPath: filepath.Join(root+"-seed", "binding-"+bindingKey, "run-"+runID, fmt.Sprintf("generation-%d.bundle", generation)),
		Target:   filepath.Join(root, "binding-"+bindingKey, "run-"+runID, fmt.Sprintf("generation-%d", generation)),
	}
	if err := intent.Validate(); err != nil {
		t.Fatalf("construct structural cleanup test intent: %v", err)
	}
	return intent
}

func cleanupTestGrant(intent authority.LinuxCloneIntent) authority.LinuxCloneGrant {
	seed := authority.LinuxCloneSeedReceipt{
		Version: authority.LinuxCloneVersion, BaseCommit: intent.BaseCommit,
		Digest: "sha256:" + strings.Repeat("d", 64), Size: 1, Device: 1, Inode: 2,
	}
	return authority.LinuxCloneGrant{Intent: intent.Clone(), Seed: seed}
}

func cleanupTestIntentAuthority(t *testing.T, intent authority.LinuxCloneIntent, seed authority.LinuxCloneSeedReceipt) authority.LinuxCloneCleanupIntent {
	t.Helper()
	baseRef, _ := authority.LinuxCloneBaseRef(intent.RunID, intent.Generation)
	promotionRef, _ := authority.LinuxClonePromotionRef(intent.RunID, intent.Generation)
	promotionCommit := strings.Repeat("f", 40)
	promotionTree := "sha256:" + strings.Repeat("1", 64)
	promotionSubjectHash, err := (taskprotocol.Subject{ResourceID: intent.ResourceID, Commit: promotionCommit, TreeDigest: promotionTree}).Hash()
	if err != nil {
		t.Fatal(err)
	}
	return authority.LinuxCloneCleanupIntent{
		Version: authority.LinuxCloneVersion, Intent: intent.Clone(),
		FenceDigest: "sha256:" + strings.Repeat("e", 64),
		RootPath:    filepath.Dir(filepath.Dir(filepath.Dir(intent.Target))), BaseRef: baseRef,
		SeedReceipt: seed.Clone(),
		CheckoutReceipt: authority.LinuxCloneCheckoutReceipt{
			Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
			BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target,
			Nonce: intent.Nonce, SeedDigest: seed.Digest, BaseCommit: intent.BaseCommit,
			TargetDevice: 1, TargetInode: 3,
		},
		PromotionIntent: authority.LinuxClonePromotionIntent{
			Version: authority.LinuxCloneVersion, ResourceID: intent.ResourceID,
			Commit: promotionCommit, TreeDigest: promotionTree,
		},
		PromotionReceipt: authority.LinuxClonePromotionReceipt{
			Version: authority.LinuxCloneVersion, Commit: promotionCommit,
			TreeDigest: promotionTree, Ref: promotionRef,
		},
		ArtifactEvidenceID:     "00000000-0000-4000-8000-000000000002",
		ArtifactEvidenceKey:    "artifact:cleanup-test",
		ArtifactSubjectHash:    promotionSubjectHash,
		ArtifactDeliveryDigest: strings.Repeat("3", 64),
	}
}

func cleanupTestGrantAndIntent(t *testing.T, intent authority.LinuxCloneIntent, owner OwnerConfig, target unix.Stat_t) (authority.LinuxCloneGrant, authority.LinuxCloneCleanupIntent) {
	t.Helper()
	grant := cleanupTestGrant(intent)
	seed := grant.Seed.Clone()
	checkout := authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target,
		Nonce: intent.Nonce, SeedDigest: seed.Digest, BaseCommit: intent.BaseCommit,
		TargetDevice: uint64(target.Dev), TargetInode: target.Ino,
	}
	promotionCommit := strings.Repeat("f", len(intent.BaseCommit))
	promotionTree := "sha256:" + strings.Repeat("1", 64)
	promotionRef, err := authority.LinuxClonePromotionRef(intent.RunID, intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	baseRef, err := authority.LinuxCloneBaseRef(intent.RunID, intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := CaptureLauncherIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	candidateSubject := taskprotocol.Subject{ResourceID: intent.ResourceID, Commit: promotionCommit, TreeDigest: promotionTree}
	artifactSubjectHash, err := candidateSubject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	cleanup := authority.LinuxCloneCleanupIntent{
		Version: authority.LinuxCloneVersion, Intent: intent.Clone(),
		FenceDigest: "sha256:" + strings.Repeat("e", 64),
		RootPath:    filepath.Dir(filepath.Dir(filepath.Dir(intent.Target))), BaseRef: baseRef,
		SeedReceipt:     seed,
		CheckoutReceipt: checkout,
		PromotionIntent: authority.LinuxClonePromotionIntent{
			Version: authority.LinuxCloneVersion, ResourceID: intent.ResourceID,
			Commit: promotionCommit, TreeDigest: promotionTree,
		},
		PromotionReceipt: authority.LinuxClonePromotionReceipt{
			Version: authority.LinuxCloneVersion, Commit: promotionCommit, TreeDigest: promotionTree, Ref: promotionRef,
		},
		NormalReleaseProof:     cleanupTestNormalReleaseProof(t, owner, anchor),
		ArtifactEvidenceID:     "00000000-0000-4000-8000-000000000002",
		ArtifactEvidenceKey:    "artifact:cleanup-scratch",
		ArtifactSubjectHash:    artifactSubjectHash,
		ArtifactDeliveryDigest: strings.Repeat("2", 64),
	}
	if err := cleanup.Validate(); err != nil {
		t.Fatalf("structural cleanup intent fixture is invalid: %v", err)
	}
	return grant, cleanup
}

func cleanupTestNormalReleaseProof(t *testing.T, owner OwnerConfig, anchorLauncher LauncherIdentity) authority.SupervisorHandoffReleaseProof {
	t.Helper()
	anchor := authority.LinuxCgroupProcessIdentity{PID: anchorLauncher.PID, Identity: anchorLauncher.Identity}
	process := func(pid int, role string) authority.LinuxCgroupProcessIdentity {
		return authority.LinuxCgroupProcessIdentity{PID: anchor.PID + pid, Identity: role + ":structural-test"}
	}
	helper := process(10, "helper")
	launcher := process(20, "launcher")
	target := process(30, "target")
	mirror := process(40, "mirror")
	parent := authority.LinuxCgroupParentIdentity{
		BootID: "00000000-0000-4000-8000-000000000001", MountID: 42, Device: 84, Inode: 126,
		Path: "/system.slice/symmetry-owner-test",
	}
	cgroup := &authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: parent,
		LeafName: strings.Repeat("9", authority.TokenBytes*2), LeafDevice: 168, LeafInode: 210,
	}
	launcherExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: launcher, Witness: helper, Waited: true, Reaped: true,
	}
	gateExit := authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: target, Witness: helper, Waited: true, Reaped: true,
	}
	stopProof := &authority.LinuxCgroupStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
		Issuer: helper, Reaper: helper, PinnedLeafFD: true, CgroupKillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
		LauncherExit: launcherExit, GateExit: gateExit,
	}
	launchToken := strings.Repeat("a", authority.TokenBytes*2)
	pipeToken := strings.Repeat("b", authority.TokenBytes*2)
	jobID := strings.Repeat("c", authority.TokenBytes*2)
	receipt := &authority.StopReceipt{
		Version: authority.SupervisorVersionV2, Status: "stopped", OwnerKind: authority.OwnerKindLinuxHelper,
		OwnerContext: "/run/symmetry-owner/cleanup-test.sock", TargetPID: target.PID, TargetIdentity: target.Identity,
		OwnerUID: owner.AUID, LauncherUID: 0, PipeToken: pipeToken, JobID: jobID,
		SupervisorPID: helper.PID, SupervisorIdentity: helper.Identity, ActiveProcesses: 0,
		TargetBirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, CgroupIdentity: cgroup, LinuxProof: stopProof,
	}
	proof := authority.SupervisorHandoffReleaseProof{
		Version: authority.SupervisorHandoffVersionV2, Status: "released", LaunchToken: launchToken,
		OwnerKind: authority.OwnerKindLinuxHelper, OwnerContext: "/run/symmetry-owner/cleanup-test.sock",
		TargetPID: target.PID, TargetIdentity: target.Identity, OwnerUID: owner.AUID, LauncherUID: 0,
		PipeToken: pipeToken, JobID: jobID, SupervisorPID: helper.PID, SupervisorIdentity: helper.Identity,
		TargetBirthKind: authority.LinuxCgroupTargetBirthTrustedAGate, CgroupIdentity: cgroup,
		AnchorPID: anchor.PID, AnchorIdentity: anchor.Identity, LauncherPID: launcher.PID,
		LauncherIdentity: launcher.Identity, LauncherExit: &launcherExit, Mirror: &mirror,
		LeafDisposition: authority.LinuxCgroupLeafRemoved, StopReceipt: receipt,
	}
	if err := proof.Validate(); err != nil {
		t.Fatalf("structural v2 release fixture is invalid: %v", err)
	}
	return proof
}

func validateCleanupScratchPath(path string) error {
	const prefix = "/opt/symmetry-owner-test/"
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !strings.HasPrefix(path, prefix) {
		return errors.New("path must be normalized and below /opt/symmetry-owner-test")
	}
	return nil
}

func openCleanupTestDirectory(path string) (int, error) {
	if err := validateCleanupScratchPath(path); err != nil {
		return -1, err
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, openErr := unix.Openat(fd, component, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return -1, openErr
		}
		fd = next
	}
	return fd, nil
}
