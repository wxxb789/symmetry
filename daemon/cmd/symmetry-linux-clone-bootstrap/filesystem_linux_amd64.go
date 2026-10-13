//go:build linux && amd64

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

const (
	cloneTreeMaxEntries = 250_000
	cloneTreeMaxDepth   = 32
	cloneTreeMaxBytes   = int64(4 << 30)
	cloneHashBufferSize = 128 << 10
)

type cloneSeed struct {
	fd       int
	parentFD int
	name     string
	identity unix.Stat_t
}

func (seed *cloneSeed) close() error {
	if seed == nil {
		return nil
	}
	var result error
	if seed.fd >= 0 {
		if err := unix.Close(seed.fd); err != nil && !errors.Is(err, unix.EINTR) {
			result = errors.Join(result, err)
		}
		seed.fd = -1
	}
	if seed.parentFD >= 0 {
		if err := unix.Close(seed.parentFD); err != nil && !errors.Is(err, unix.EINTR) {
			result = errors.Join(result, err)
		}
		seed.parentFD = -1
	}
	return result
}

type cloneTarget struct {
	rootPath        string
	bindingName     string
	runName         string
	generationName  string
	rootFD          int
	bindingFD       int
	runFD           int
	fd              int
	rootIdentity    unix.Stat_t
	bindingIdentity unix.Stat_t
	runIdentity     unix.Stat_t
	identity        unix.Stat_t
}

func (target *cloneTarget) close() error {
	if target == nil {
		return nil
	}
	var result error
	for _, fd := range []*int{&target.fd, &target.runFD, &target.bindingFD, &target.rootFD} {
		if *fd < 0 {
			continue
		}
		if err := unix.Close(*fd); err != nil && !errors.Is(err, unix.EINTR) {
			result = errors.Join(result, err)
		}
		*fd = -1
	}
	return result
}

func openCloneSeed(grant authority.LinuxCloneGrant) (*cloneSeed, error) {
	intent, seedReceipt := grant.Intent, grant.Seed
	if !canonicalAbsolutePath(intent.SeedPath) {
		return nil, errors.New("A seed path is not canonical and absolute")
	}
	parentPath, name := filepath.Dir(intent.SeedPath), filepath.Base(intent.SeedPath)
	if !safeComponent(name) {
		return nil, errors.New("A seed basename is invalid")
	}
	parentFD, _, err := openReadOnlyAPathDirectory(parentPath)
	if err != nil {
		return nil, fmt.Errorf("open A-owned seed parent without symlinks: %w", err)
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		_ = unix.Close(parentFD)
		return nil, fmt.Errorf("open immutable A seed bundle: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		_ = unix.Close(parentFD)
		return nil, fmt.Errorf("stat immutable A seed bundle: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size != seedReceipt.Size ||
		stat.Size <= 0 || stat.Size > authority.LinuxCloneMaxSeedBytes || stat.Nlink != 1 ||
		stat.Uid == 1 || stat.Gid == 1 ||
		stat.Mode&(0222|0111|06000) != 0 || uint64(stat.Dev) != seedReceipt.Device || stat.Ino != seedReceipt.Inode {
		_ = unix.Close(fd)
		_ = unix.Close(parentFD)
		return nil, errors.New("A seed bundle differs from its exact immutable RunJournal receipt")
	}
	if err := requireBWriteDenied(parentFD, name); err != nil {
		_ = unix.Close(fd)
		_ = unix.Close(parentFD)
		return nil, fmt.Errorf("A seed file is writable by B: %w", err)
	}
	return &cloneSeed{fd: fd, parentFD: parentFD, name: name, identity: stat}, nil
}

func verifyCloneSeed(ctx context.Context, seed *cloneSeed, grant authority.LinuxCloneGrant) error {
	if seed == nil || seed.fd < 0 || seed.parentFD < 0 {
		return errors.New("pinned A seed descriptor is unavailable")
	}
	var before unix.Stat_t
	if err := unix.Fstat(seed.fd, &before); err != nil {
		return fmt.Errorf("stat pinned A seed before digest: %w", err)
	}
	if !sameStatIdentity(&seed.identity, &before) || before.Size != grant.Seed.Size || before.Mode&0222 != 0 {
		return errors.New("pinned A seed identity or immutability changed before digest")
	}
	hash := sha256.New()
	buffer := make([]byte, cloneHashBufferSize)
	var offset int64
	for offset < before.Size {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := before.Size - offset
		if remaining < int64(len(buffer)) {
			buffer = buffer[:int(remaining)]
		}
		n, err := unix.Pread(seed.fd, buffer, offset)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read A seed bytes for digest: %w", err)
		}
		if n == 0 {
			return errors.New("A seed ended before its measured byte count")
		}
		if _, err := hash.Write(buffer[:n]); err != nil {
			return fmt.Errorf("hash A seed bytes: %w", err)
		}
		offset += int64(n)
	}
	var extra [1]byte
	n, err := unix.Pread(seed.fd, extra[:], before.Size)
	if err != nil && !errors.Is(err, unix.EINTR) {
		return fmt.Errorf("check A seed end: %w", err)
	}
	if n != 0 {
		return errors.New("A seed grew after its receipt was measured")
	}
	actualDigest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if actualDigest != grant.Seed.Digest {
		return errors.New("A seed digest differs from its immutable RunJournal receipt")
	}
	var after unix.Stat_t
	if err := unix.Fstat(seed.fd, &after); err != nil {
		return fmt.Errorf("stat A seed after digest verification: %w", err)
	}
	if !sameStatIdentity(&before, &after) || after.Size != before.Size || after.Mode&(0222|0111|06000) != 0 {
		return errors.New("A seed identity changed during digest verification")
	}
	return verifySeedPathStillBound(seed, grant)
}

func verifySeedPathStillBound(seed *cloneSeed, grant authority.LinuxCloneGrant) error {
	var opened, named unix.Stat_t
	if err := unix.Fstat(seed.fd, &opened); err != nil {
		return fmt.Errorf("stat pinned A seed: %w", err)
	}
	if err := unix.Fstatat(seed.parentFD, seed.name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("read back A seed path without following symlinks: %w", err)
	}
	if !sameStatIdentity(&seed.identity, &opened) || !sameStatIdentity(&seed.identity, &named) ||
		uint64(named.Dev) != grant.Seed.Device || named.Ino != grant.Seed.Inode || named.Size != grant.Seed.Size ||
		named.Mode&unix.S_IFMT != unix.S_IFREG || named.Mode&(0222|0111|06000) != 0 {
		return errors.New("A seed pathname no longer names the exact immutable RunJournal bundle")
	}
	return nil
}

func createFreshCloneTarget(grant authority.LinuxCloneGrant) (*cloneTarget, error) {
	intent := grant.Intent
	if !canonicalAbsolutePath(intent.Target) {
		return nil, errors.New("B clone target is not canonical and absolute")
	}
	generationName := fmt.Sprintf("generation-%d", intent.Generation)
	bindingName := "binding-" + intent.BindingKey
	runName := "run-" + intent.RunID
	rootPath := filepath.Dir(filepath.Dir(filepath.Dir(intent.Target)))
	if rootPath == "/" || filepath.Join(rootPath, bindingName, runName, generationName) != intent.Target {
		return nil, errors.New("B clone target does not have the exact binding/run/generation layout")
	}
	if pathOverlaps(rootPath, intent.SeedPath) {
		return nil, errors.New("B clone root aliases or contains the A-owned seed path")
	}
	unix.Umask(0o077)
	rootFD, rootInfo, err := openAbsoluteDirectory(rootPath)
	if err != nil {
		return nil, fmt.Errorf("open pre-provisioned B clone root without symlinks: %w", err)
	}
	target := &cloneTarget{
		rootPath: rootPath, bindingName: bindingName, runName: runName, generationName: generationName,
		rootFD: rootFD, bindingFD: -1, runFD: -1, fd: -1, rootIdentity: rootInfo,
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = target.close()
		}
	}()
	if err := requireOwnedBDirectory(rootInfo, "pre-provisioned B clone root", true); err != nil {
		return nil, err
	}
	bindingFD, bindingInfo, err := openOrCreateBDirectory(rootFD, bindingName)
	if err != nil {
		return nil, fmt.Errorf("open or create B binding directory: %w", err)
	}
	target.bindingFD, target.bindingIdentity = bindingFD, bindingInfo
	runFD, runInfo, err := openOrCreateBDirectory(bindingFD, runName)
	if err != nil {
		return nil, fmt.Errorf("open or create B run directory: %w", err)
	}
	target.runFD, target.runIdentity = runFD, runInfo
	if err := unix.Mkdirat(runFD, generationName, 0o770); err != nil {
		return nil, fmt.Errorf("create fresh B generation directory (existing targets are never adopted): %w", err)
	}
	if err := unix.Fsync(runFD); err != nil {
		return nil, fmt.Errorf("sync B run directory after target creation: %w", err)
	}
	targetFD, err := unix.Openat(runFD, generationName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open newly-created B generation directory: %w", err)
	}
	target.fd = targetFD
	var targetInfo unix.Stat_t
	if err := unix.Fstat(targetFD, &targetInfo); err != nil {
		return nil, fmt.Errorf("stat newly-created B generation directory: %w", err)
	}
	if err := requireOwnedBDirectory(targetInfo, "new B generation directory", false); err != nil {
		return nil, err
	}
	target.identity = targetInfo
	if err := requireEmptyDirectory(targetFD); err != nil {
		return nil, fmt.Errorf("new B generation directory is not empty: %w", err)
	}
	if err := verifyTargetPathStillBound(target, grant); err != nil {
		return nil, err
	}
	cleanupOnError = false
	return target, nil
}

func openAbsoluteDirectory(path string) (int, unix.Stat_t, error) {
	if !canonicalAbsolutePath(path) || path == "/" {
		return -1, unix.Stat_t{}, errors.New("directory path must be canonical, absolute, and non-root")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if !safeComponent(component) {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("directory path contains an unsafe component")
		}
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return -1, unix.Stat_t{}, openErr
		}
		fd = next
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("stat directory path: %w", err)
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, errors.New("path is not a directory")
	}
	if err := unix.Faccessat(fd, ".", unix.W_OK, 0); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("B cannot create targets in its pre-provisioned clone root: %w", err)
	}
	return fd, info, nil
}

func openReadOnlyAPathDirectory(path string) (int, unix.Stat_t, error) {
	if !canonicalAbsolutePath(path) || path == "/" {
		return -1, unix.Stat_t{}, errors.New("A seed parent must be canonical, absolute, and non-root")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if !safeComponent(component) {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("A seed parent contains an unsafe component")
		}
		if err := requireBWriteDenied(fd, "."); err != nil {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, fmt.Errorf("A seed ancestor is writable by B: %w", err)
		}
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return -1, unix.Stat_t{}, openErr
		}
		fd = next
	}
	if err := requireBWriteDenied(fd, "."); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("A seed parent is writable by B: %w", err)
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("stat A seed parent: %w", err)
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, errors.New("A seed parent is not a directory")
	}
	return fd, info, nil
}

func requireBWriteDenied(parentFD int, name string) error {
	err := unix.Faccessat(parentFD, name, unix.W_OK, 0)
	if err == nil {
		return errors.New("B has effective write access")
	}
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.EROFS) {
		return nil
	}
	return err
}

func openOrCreateBDirectory(parentFD int, name string) (int, unix.Stat_t, error) {
	if !safeComponent(name) {
		return -1, unix.Stat_t{}, errors.New("B target parent component is unsafe")
	}
	created := false
	if err := unix.Mkdirat(parentFD, name, 0o770); err == nil {
		created = true
	} else if !errors.Is(err, unix.EEXIST) {
		return -1, unix.Stat_t{}, err
	}
	if created {
		if err := unix.Fsync(parentFD); err != nil {
			return -1, unix.Stat_t{}, fmt.Errorf("sync parent after B directory creation: %w", err)
		}
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, err
	}
	if err := requireOwnedBDirectory(info, "B target parent", false); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, err
	}
	return fd, info, nil
}

func requireOwnedBDirectory(info unix.Stat_t, label string, root bool) error {
	if info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Uid != 1 || info.Gid != 1 ||
		info.Mode&(unix.S_ISUID|unix.S_ISVTX) != 0 || info.Mode&0002 != 0 {
		return fmt.Errorf("%s is not a safe B-owned directory: uid=%d gid=%d mode=%#o", label, info.Uid, info.Gid, info.Mode)
	}
	if root && info.Mode&0022 != 0 {
		return errors.New("pre-provisioned B clone root must not be group/world writable")
	}
	return nil
}

func requireEmptyDirectory(fd int) error {
	duplicate, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(duplicate), "fresh-clone-target-readback")
	if file == nil {
		_ = unix.Close(duplicate)
		return errors.New("wrap fresh target directory for emptiness readback")
	}
	defer file.Close()
	entries, err := file.ReadDir(1)
	if len(entries) != 0 {
		return errors.New("fresh B target already contains entries")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func verifyTargetPathStillBound(target *cloneTarget, grant authority.LinuxCloneGrant) error {
	if target == nil || target.fd < 0 || target.rootFD < 0 {
		return errors.New("pinned B clone target descriptors are unavailable")
	}
	if filepath.Join(target.rootPath, target.bindingName, target.runName, target.generationName) != grant.Intent.Target {
		return errors.New("pinned B clone path differs from the exact RunJournal target")
	}
	freshRoot, rootInfo, err := openAbsoluteDirectory(target.rootPath)
	if err != nil {
		return fmt.Errorf("reopen B clone root path: %w", err)
	}
	defer unix.Close(freshRoot)
	if !sameStatIdentity(&target.rootIdentity, &rootInfo) {
		return errors.New("B clone root path changed after target creation")
	}
	bindingFD, bindingInfo, err := openOwnedBChild(freshRoot, target.bindingName)
	if err != nil {
		return fmt.Errorf("reopen exact B binding directory: %w", err)
	}
	defer unix.Close(bindingFD)
	if !sameStatIdentity(&target.bindingIdentity, &bindingInfo) {
		return errors.New("B binding directory path changed after target creation")
	}
	runFD, runInfo, err := openOwnedBChild(bindingFD, target.runName)
	if err != nil {
		return fmt.Errorf("reopen exact B run directory: %w", err)
	}
	defer unix.Close(runFD)
	if !sameStatIdentity(&target.runIdentity, &runInfo) {
		return errors.New("B run directory path changed after target creation")
	}
	named, err := statAtNoFollow(runFD, target.generationName)
	if err != nil {
		return fmt.Errorf("read back exact B generation pathname: %w", err)
	}
	openedFD, err := unix.Openat(runFD, target.generationName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("reopen exact B generation pathname: %w", err)
	}
	defer unix.Close(openedFD)
	var opened unix.Stat_t
	if err := unix.Fstat(openedFD, &opened); err != nil {
		return err
	}
	var pinned unix.Stat_t
	if err := unix.Fstat(target.fd, &pinned); err != nil {
		return err
	}
	if !sameStatIdentity(&target.identity, &named) || !sameStatIdentity(&target.identity, &opened) ||
		!sameStatIdentity(&target.identity, &pinned) || named.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("B target path or pinned inode changed during clone bootstrap")
	}
	if err := requireOwnedBDirectory(pinned, "pinned B clone target", false); err != nil {
		return err
	}
	return nil
}

func openOwnedBChild(parentFD int, name string) (int, unix.Stat_t, error) {
	if !safeComponent(name) {
		return -1, unix.Stat_t{}, errors.New("B target directory component is unsafe")
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, err
	}
	if err := requireOwnedBDirectory(info, "B target parent", false); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, err
	}
	return fd, info, nil
}

func statAtNoFollow(parentFD int, name string) (unix.Stat_t, error) {
	var info unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return unix.Stat_t{}, err
	}
	return info, nil
}

func safeComponent(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00\r\n")
}

func canonicalAbsolutePath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

func sameStatIdentity(left, right *unix.Stat_t) bool {
	return left != nil && right != nil && left.Dev == right.Dev && left.Ino == right.Ino &&
		left.Mode&unix.S_IFMT == right.Mode&unix.S_IFMT
}

func pathOverlaps(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+string(filepath.Separator)) ||
		strings.HasPrefix(right, left+string(filepath.Separator))
}

func checkoutReceipt(grant authority.LinuxCloneGrant, target *cloneTarget) authority.LinuxCloneCheckoutReceipt {
	intent := grant.Intent
	return authority.LinuxCloneCheckoutReceipt{
		Version: intent.Version, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target,
		Nonce: intent.Nonce, SeedDigest: grant.Seed.Digest, BaseCommit: intent.BaseCommit,
		TargetDevice: uint64(target.identity.Dev), TargetInode: target.identity.Ino,
	}
}

func syncClonePath(target *cloneTarget) error {
	for _, fd := range []int{target.fd, target.runFD, target.bindingFD, target.rootFD} {
		if err := unix.Fsync(fd); err != nil {
			return fmt.Errorf("sync B clone directory descriptor: %w", err)
		}
	}
	return nil
}

func syncCloneTree(ctx context.Context, rootFD int) error {
	entries := 0
	var totalBytes int64
	return syncCloneDirectory(ctx, rootFD, 0, &entries, &totalBytes)
}

func syncCloneDirectory(ctx context.Context, dirFD, depth int, entries *int, totalBytes *int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > cloneTreeMaxDepth {
		return errors.New("B clone checkout exceeds the bounded directory depth")
	}
	readFD, err := unix.Openat(dirFD, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(readFD), "clone-sync-directory")
	if file == nil {
		_ = unix.Close(readFD)
		return errors.New("wrap clone directory for bounded sync traversal")
	}
	defer file.Close()
	for {
		children, readErr := file.ReadDir(128)
		for _, child := range children {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := child.Name()
			if name == "" || name == "." || name == ".." {
				return errors.New("B clone directory contains an unsafe name")
			}
			*entries = *entries + 1
			if *entries > cloneTreeMaxEntries {
				return errors.New("B clone checkout exceeds the bounded entry count")
			}
			var named unix.Stat_t
			if err := unix.Fstatat(dirFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return fmt.Errorf("stat B clone entry without following links: %w", err)
			}
			switch named.Mode & unix.S_IFMT {
			case unix.S_IFDIR:
				dir, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
				if err != nil {
					return fmt.Errorf("open B clone directory without following links: %w", err)
				}
				var opened unix.Stat_t
				if err := unix.Fstat(dir, &opened); err != nil {
					_ = unix.Close(dir)
					return fmt.Errorf("stat B clone directory during sync traversal: %w", err)
				}
				if !sameStatIdentity(&named, &opened) {
					_ = unix.Close(dir)
					return errors.New("B clone directory changed during sync traversal")
				}
				if err := syncCloneDirectory(ctx, dir, depth+1, entries, totalBytes); err != nil {
					_ = unix.Close(dir)
					return err
				}
				if err := unix.Close(dir); err != nil && !errors.Is(err, unix.EINTR) {
					return err
				}
			case unix.S_IFREG:
				if named.Size < 0 || named.Size > cloneTreeMaxBytes-*totalBytes {
					return errors.New("B clone checkout exceeds the bounded regular-file byte count")
				}
				*totalBytes += named.Size
				fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
				if err != nil {
					return fmt.Errorf("open B clone file without following links: %w", err)
				}
				var opened unix.Stat_t
				statErr := unix.Fstat(fd, &opened)
				if statErr != nil {
					_ = unix.Close(fd)
					return fmt.Errorf("stat B clone file during sync traversal: %w", statErr)
				}
				if !sameStatIdentity(&named, &opened) || opened.Size != named.Size {
					_ = unix.Close(fd)
					return errors.New("B clone file changed during sync traversal")
				}
				syncErr := unix.Fsync(fd)
				closeErr := unix.Close(fd)
				if syncErr != nil || closeErr != nil {
					return errors.Join(syncErr, closeErr)
				}
			case unix.S_IFLNK:
				// Git checkouts may legitimately contain symlinks. Fsync of the
				// containing directory persists the link entry; never follow it.
			default:
				return errors.New("B clone checkout contains a non-regular, non-directory, non-symlink entry")
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read B clone directory during sync traversal: %w", readErr)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync B clone directory: %w", err)
	}
	return nil
}
