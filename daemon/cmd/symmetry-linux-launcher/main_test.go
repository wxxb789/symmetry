//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

func TestCloneCleanupWalkerRemovesPinnedTreeWithoutFollowingSymlinks(t *testing.T) {
	root := newCleanupTestRoot(t)
	targetRelative := filepath.Join("binding-cleanup-test", "run-cleanup-test", "generation-1")
	target := filepath.Join(root, targetRelative)
	sibling := filepath.Join(root, "sibling")
	if err := os.MkdirAll(filepath.Join(target, ".git", "objects", "pack"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".git", "objects", "pack", "pack-test"), []byte("B clone object\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "keep"), []byte("unrelated sibling\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sibling, "keep"), filepath.Join(target, "outside-link")); err != nil {
		t.Fatal(err)
	}
	rootFD, targetFD, rootIdentity, targetIdentity := openCleanupTestPins(t, root, targetRelative)
	defer unix.Close(rootFD)
	defer unix.Close(targetFD)
	config := protocolOwnerForCleanupTest()
	if err := removePinnedCloneTarget(context.Background(), rootFD, targetFD, root, targetRelative, rootIdentity, targetIdentity, config, nil); err != nil {
		t.Fatalf("remove exact pinned B target: %v", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target still exists after cleanup: err=%v", err)
	}
	if content, err := os.ReadFile(filepath.Join(sibling, "keep")); err != nil || string(content) != "unrelated sibling\n" {
		t.Fatalf("symlink target or sibling was changed: content=%q err=%v", content, err)
	}
}

func TestCloneCleanupWalkerRejectsHardlinkAndReplaysAfterAliasRetired(t *testing.T) {
	root := newCleanupTestRoot(t)
	targetRelative := filepath.Join("binding-cleanup-test", "run-cleanup-retry", "generation-1")
	target := filepath.Join(root, targetRelative)
	sibling := filepath.Join(root, "sibling")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "a-first"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideHardlink := filepath.Join(sibling, "outside-hardlink")
	if err := os.WriteFile(outsideHardlink, []byte("shared\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outsideHardlink, filepath.Join(target, "z-hardlink")); err != nil {
		t.Fatal(err)
	}
	rootFD, targetFD, rootIdentity, targetIdentity := openCleanupTestPins(t, root, targetRelative)
	defer unix.Close(rootFD)
	defer unix.Close(targetFD)
	config := protocolOwnerForCleanupTest()
	if err := removePinnedCloneTarget(context.Background(), rootFD, targetFD, root, targetRelative, rootIdentity, targetIdentity, config, nil); err == nil {
		t.Fatal("cleanup accepted a hard-linked file with an alias outside the pinned target")
	}
	safeFile := filepath.Join(target, "a-first")
	if info, err := os.Lstat(safeFile); err == nil {
		if !info.Mode().IsRegular() {
			t.Fatalf("cleanup replaced the safe file with a non-regular entry: mode=%v", info.Mode())
		}
		content, err := os.ReadFile(safeFile)
		if err != nil || string(content) != "first\n" {
			t.Fatalf("cleanup modified the safe file before rejecting the external hard link: content=%q err=%v", content, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("safe file readback after hard-link rejection failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(target, "z-hardlink")); err != nil {
		t.Fatalf("cleanup changed the unsafe hard-link entry: %v", err)
	}
	if _, err := os.Stat(outsideHardlink); err != nil {
		t.Fatalf("cleanup changed the outside hard-link sibling: %v", err)
	}
	if err := os.Remove(outsideHardlink); err != nil {
		t.Fatal(err)
	}
	if err := removePinnedCloneTarget(context.Background(), rootFD, targetFD, root, targetRelative, rootIdentity, targetIdentity, config, nil); err != nil {
		t.Fatalf("replay exact target cleanup after the unsafe alias was retired: %v", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("exact target remained after cleanup replay: err=%v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("cleanup removed the unrelated sibling: %v", err)
	}
}

func TestCloneCleanupDetectsConcurrentSiblingExchangeWithoutFalseRemoval(t *testing.T) {
	root := newCleanupTestRoot(t)
	targetRelative := filepath.Join("binding-cleanup-test", "run-cleanup-swap", "generation-1")
	target := filepath.Join(root, targetRelative)
	sibling := filepath.Join(root, "sibling")
	siblingReplacement := filepath.Join(sibling, "exchange-directory")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(siblingReplacement, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "keep"), []byte("sibling\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacementFD, err := unix.Open(siblingReplacement, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(replacementFD)
	var replacementBefore unix.Stat_t
	if err := unix.Fstat(replacementFD, &replacementBefore); err != nil {
		t.Fatal(err)
	}
	rootFD, targetFD, rootIdentity, targetIdentity := openCleanupTestPins(t, root, targetRelative)
	defer unix.Close(rootFD)
	defer unix.Close(targetFD)
	config := protocolOwnerForCleanupTest()
	swapped := false
	swapAtRmdir := func() error {
		completed := make(chan error, 1)
		go func() {
			if err := unix.Renameat2(unix.AT_FDCWD, target, unix.AT_FDCWD, siblingReplacement, unix.RENAME_EXCHANGE); err != nil {
				completed <- fmt.Errorf("exchange original target with same-UID B sibling: %w", err)
				return
			}
			swapped = true
			completed <- nil
		}()
		return <-completed
	}
	err = removePinnedCloneTarget(context.Background(), rootFD, targetFD, root, targetRelative,
		rootIdentity, targetIdentity, config, swapAtRmdir)
	if !swapped || err == nil {
		t.Fatalf("concurrent B target swap was not detected as unresolved: swapped=%t err=%v", swapped, err)
	}
	var original unix.Stat_t
	if err := unix.Fstat(targetFD, &original); err != nil || original.Ino != targetIdentity.Inode || original.Nlink == 0 {
		t.Fatalf("original checkout inode did not survive the injected sibling exchange: stat=%+v err=%v", original, err)
	}
	var replacementAfter unix.Stat_t
	if err := unix.Fstat(replacementFD, &replacementAfter); err != nil || replacementAfter.Ino != replacementBefore.Ino || replacementAfter.Nlink != 0 {
		t.Fatalf("root did not expose that the exchanged B sibling was removed: stat=%+v err=%v", replacementAfter, err)
	}
	var originalAtSibling unix.Stat_t
	if err := unix.Stat(siblingReplacement, &originalAtSibling); err != nil ||
		uint64(originalAtSibling.Dev) != targetIdentity.Device || originalAtSibling.Ino != targetIdentity.Inode {
		t.Fatalf("the original checkout did not remain under the exchanged B sibling name: stat=%+v err=%v", originalAtSibling, err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("exchanged sibling target name survived root rmdir: err=%v", err)
	}
	if content, err := os.ReadFile(filepath.Join(sibling, "keep")); err != nil || string(content) != "sibling\n" {
		t.Fatalf("concurrent target swap changed the unrelated B sibling: content=%q err=%v", content, err)
	}
}

func TestCloneCleanupRejectsReplacedTargetSymlink(t *testing.T) {
	root := newCleanupTestRoot(t)
	targetRelative := filepath.Join("binding-cleanup-test", "run-cleanup-replaced", "generation-1")
	target := filepath.Join(root, targetRelative)
	sibling := filepath.Join(root, "sibling")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "inside"), []byte("pinned target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "keep"), []byte("sibling\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootFD, targetFD, rootIdentity, targetIdentity := openCleanupTestPins(t, root, targetRelative)
	defer unix.Close(rootFD)
	defer unix.Close(targetFD)
	displaced := target + ".displaced"
	if err := os.Rename(target, displaced); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sibling, target); err != nil {
		t.Fatal(err)
	}
	if err := removePinnedCloneTarget(context.Background(), rootFD, targetFD, root, targetRelative, rootIdentity, targetIdentity, protocolOwnerForCleanupTest(), nil); err == nil {
		t.Fatal("cleanup followed a replaced target symlink")
	}
	if content, err := os.ReadFile(filepath.Join(sibling, "keep")); err != nil || string(content) != "sibling\n" {
		t.Fatalf("replacement symlink reached sibling: content=%q err=%v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(displaced, "inside")); err != nil || string(content) != "pinned target\n" {
		t.Fatalf("cleanup removed the displaced pinned inode: content=%q err=%v", content, err)
	}
}

func TestCloneCleanupRejectsCrossMountEntry(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_MOUNTS") != "1" || os.Geteuid() != 0 {
		t.Skip("cross-mount cleanup proof requires explicit opt-in on a disposable root test host")
	}
	root := newCleanupTestRoot(t)
	targetRelative := filepath.Join("binding-cleanup-test", "run-cleanup-mount", "generation-1")
	target := filepath.Join(root, targetRelative)
	sibling := filepath.Join(root, "sibling")
	mountPoint := filepath.Join(target, "mounted")
	if err := os.MkdirAll(mountPoint, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "keep"), []byte("mount source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(sibling, mountPoint, "", unix.MS_BIND, ""); err != nil {
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENODEV) {
			t.Skipf("mount capability unavailable on this host: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(mountPoint, 0); err != nil {
			t.Errorf("unmount disposable cleanup fixture: %v", err)
		}
	})
	rootFD, targetFD, rootIdentity, targetIdentity := openCleanupTestPins(t, root, targetRelative)
	defer unix.Close(rootFD)
	defer unix.Close(targetFD)
	if err := removePinnedCloneTarget(context.Background(), rootFD, targetFD, root, targetRelative, rootIdentity, targetIdentity, protocolOwnerForCleanupTest(), nil); err == nil {
		t.Fatal("cleanup crossed a mount below the exact target")
	}
	if content, err := os.ReadFile(filepath.Join(sibling, "keep")); err != nil || string(content) != "mount source\n" {
		t.Fatalf("cross-mount cleanup touched the sibling: content=%q err=%v", content, err)
	}
}

func TestInstalledLinuxOwnerCleanupRejectsBCallerScratch(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_SCRATCH") != "1" {
		t.Skip("requires an operator-installed root:A cleanup wrapper")
	}
	if os.Geteuid() != 0 {
		t.Skip("B caller denial probe must be launched by root with a real B credential")
	}
	owner, err := protocol.ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/usr/libexec/symmetry/symmetry-linux-launcher", "-clone-cleanup")
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	command.Dir = "/"
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid: owner.BUID, Gid: owner.BGID, Groups: []uint32{},
	}}
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("B was allowed to invoke the setuid cleanup wrapper: output=%q", output)
	}
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
		return
	}
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 126 {
		t.Fatalf("B wrapper denial did not fail closed with fixed exit 126: output=%q err=%v", output, err)
	}
}

func TestInstalledLinuxOwnerCleanupLockSerializesWorkersScratch(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_SCRATCH") != "1" || os.Geteuid() != 0 {
		t.Skip("cleanup lock proof requires the explicit installed-owner scratch environment as root")
	}
	if _, err := protocol.ReadInstalledOwnerConfig(); err != nil {
		t.Fatalf("read installed owner configuration for lock proof: %v", err)
	}
	first, err := acquireCloneCleanupLock()
	if err != nil {
		t.Fatalf("acquire first cleanup lock on installed owner config: %v", err)
	}
	defer func() {
		if first != nil {
			_ = first.Close()
		}
	}()
	if second, err := acquireCloneCleanupLock(); err == nil {
		_ = second.Close()
		t.Fatal("a concurrent root cleanup worker acquired the same config-inode lock")
	} else if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("second cleanup lock failed for an unexpected reason: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("release first cleanup lock: %v", err)
	}
	first = nil
	third, err := acquireCloneCleanupLock()
	if err != nil {
		t.Fatalf("acquire cleanup lock after prior worker closed: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("release reacquired cleanup lock: %v", err)
	}
}

func openCleanupTestPins(t *testing.T, root, relative string) (int, int, cleanupDirectoryIdentity, cleanupDirectoryIdentity) {
	t.Helper()
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	rootIdentity, err := fstatCleanupPath(rootFD)
	if err != nil {
		_ = unix.Close(rootFD)
		t.Fatal(err)
	}
	config := protocolOwnerForCleanupTest()
	targetFD, targetStat, err := openCleanupRelativeDirectory(rootFD, relative, unix.O_PATH|unix.O_DIRECTORY,
		config.BUID, config.BGID, rootIdentity.Device)
	if err != nil {
		_ = unix.Close(rootFD)
		t.Fatal(err)
	}
	return rootFD, targetFD, rootIdentity, cleanupDirectoryIdentity{Device: uint64(targetStat.Dev), Inode: targetStat.Ino}
}

func newCleanupTestRoot(t *testing.T) string {
	t.Helper()
	// The root-side policy rejects writable ancestors such as /tmp; keep each
	// disposable fixture below the invoking user's protected home instead.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve protected cleanup test home: %v", err)
	}
	if !filepath.IsAbs(home) || filepath.Clean(home) != home {
		t.Fatalf("cleanup test home is not a normalized absolute path: %q", home)
	}
	root, err := os.MkdirTemp(home, "symmetry-linux-cleanup-test-")
	if err != nil {
		t.Fatalf("create protected cleanup test root below home: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove only this cleanup test root: %v", err)
		}
	})
	return root
}

func protocolOwnerForCleanupTest() protocol.OwnerConfig {
	return protocol.OwnerConfig{AUID: uint32(os.Getuid()), AGID: uint32(os.Getgid()), BUID: uint32(os.Getuid()), BGID: uint32(os.Getgid())}
}
