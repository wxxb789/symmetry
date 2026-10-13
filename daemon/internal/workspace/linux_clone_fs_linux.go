//go:build linux && amd64

package workspace

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

const maxLinuxCloneDirectoryEntries = 250_000

type linuxCloneContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader linuxCloneContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

type linuxCloneTarget struct {
	parentFD int
	fd       int
	name     string
	identity unix.Stat_t
	root     string
	intent   authority.LinuxCloneIntent
	receipt  authority.LinuxCloneCheckoutReceipt
}

type linuxCloneCleanupTarget struct {
	rootFD    int
	targetFD  int
	rootPath  string
	target    string
	rootInfo  unix.Stat_t
	ancestors []linuxCloneNamedDirectory
	missingAt int
}

func (target *linuxCloneCleanupTarget) close() error {
	if target == nil {
		return nil
	}
	err := target.closeTarget()
	if target.rootFD >= 0 {
		if closeErr := unix.Close(target.rootFD); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		target.rootFD = -1
	}
	return err
}

func (target *linuxCloneCleanupTarget) closeTarget() error {
	if target == nil || target.targetFD < 0 {
		return nil
	}
	err := unix.Close(target.targetFD)
	target.targetFD = -1
	return err
}

func (target *linuxCloneTarget) close() error {
	if target == nil {
		return nil
	}
	var err error
	if target.fd >= 0 {
		if closeErr := unix.Close(target.fd); closeErr != nil {
			err = closeErr
		}
		target.fd = -1
	}
	if target.parentFD >= 0 {
		if closeErr := unix.Close(target.parentFD); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		target.parentFD = -1
	}
	return err
}

func openLinuxCloneDirPath(path string) (int, unix.Stat_t, error) {
	return openLinuxCloneTrustedPathDir(path, unix.O_RDONLY)
}

func openLinuxCloneOPathDirPath(path string) (int, unix.Stat_t, error) {
	return openLinuxCloneTrustedPathDir(path, unix.O_PATH)
}

// openLinuxCloneTrustedPathDir walks the configured absolute path without
// following links or crossing symlinks. Mount crossings are allowed only while
// resolving trusted ancestors/root; all subsequent traversal starts from the
// pinned root FD and rejects mount crossings.
func openLinuxCloneTrustedPathDir(path string, accessFlags int) (int, unix.Stat_t, error) {
	if !canonicalLinuxClonePath(path) {
		return -1, unix.Stat_t{}, errors.New("directory path must be canonical and absolute")
	}
	if path == "/" {
		return -1, unix.Stat_t{}, errors.New("filesystem root cannot be configured as a B clone root")
	}
	fd, err := unix.Open("/", accessFlags|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	components := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, component := range components {
		if component == "" {
			continue
		}
		if component == "." || component == ".." {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("configured directory path contains an invalid component")
		}
		var named unix.Stat_t
		if err := unix.Fstatat(fd, component, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, fmt.Errorf("inspect configured directory component %q: %w", component, err)
		}
		if named.Mode&unix.S_IFMT != unix.S_IFDIR {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, fmt.Errorf("configured directory component %q is not a real directory", component)
		}
		last := index == len(components)-1
		if last {
			if named.Mode&0o022 != 0 {
				_ = unix.Close(fd)
				return -1, unix.Stat_t{}, errors.New("configured B root has group/world mode-bit write access")
			}
		} else if err := verifyLinuxCloneTrustedRootAncestor(&named); err != nil {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, fmt.Errorf("configured root ancestor %q is not trusted: %w", component, err)
		}
		next, err := unix.Openat(fd, component, accessFlags|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(fd)
		if err != nil {
			return -1, unix.Stat_t{}, fmt.Errorf("pin configured root component %q: %w", component, err)
		}
		var opened unix.Stat_t
		if err := unix.Fstat(next, &opened); err != nil {
			_ = unix.Close(next)
			return -1, unix.Stat_t{}, fmt.Errorf("read pinned configured root component %q: %w", component, err)
		}
		if !sameLinuxCloneDirectoryIdentity(&named, &opened) || named.Uid != opened.Uid || named.Gid != opened.Gid || named.Mode&0o7777 != opened.Mode&0o7777 {
			_ = unix.Close(next)
			return -1, unix.Stat_t{}, errors.New("configured root component changed while pinning")
		}
		if last && opened.Mode&0o022 != 0 {
			_ = unix.Close(next)
			return -1, unix.Stat_t{}, errors.New("configured B root gained group/world mode-bit write access")
		}
		if !last {
			if err := verifyLinuxCloneTrustedRootAncestor(&opened); err != nil {
				_ = unix.Close(next)
				return -1, unix.Stat_t{}, fmt.Errorf("pinned configured root ancestor %q is not trusted: %w", component, err)
			}
		}
		fd = next
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, errors.New("path is not a directory")
	}
	return fd, info, nil
}

func verifyLinuxCloneTrustedRootAncestor(info *unix.Stat_t) error {
	if info == nil || info.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("ancestor is not a real directory")
	}
	uid, gid := uint32(info.Uid), uint32(info.Gid)
	if uid != 0 && uid != uint32(os.Geteuid()) {
		return errors.New("ancestor is neither root-owned nor A-owned")
	}
	// Root-owned sticky ancestors protect a private descendant name from other
	// UIDs; deployed clone admission additionally enforces its ACL policy.
	stickyRoot := uid == 0 && info.Mode&unix.S_ISVTX != 0
	if info.Mode&0o002 != 0 && !stickyRoot {
		return errors.New("ancestor is world-writable outside a root-owned sticky directory")
	}
	if info.Mode&0o020 != 0 && gid != uint32(os.Getegid()) && !stickyRoot {
		return errors.New("ancestor is writable by a group other than A's primary group")
	}
	return nil
}

func openLinuxCloneOPathDirAt(parentFD int, name string) (int, error) {
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
	}
	fd, err := unix.Openat2(parentFD, name, how)
	if err != nil {
		return -1, err
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return -1, errors.New("entry is not a real directory")
	}
	return fd, nil
}

func openLinuxCloneCleanupTarget(ctx context.Context, root, target string, checkout authority.LinuxCloneCheckoutReceipt) (*linuxCloneCleanupTarget, error) {
	if ctx == nil || checkout.Validate() != nil || !canonicalLinuxClonePath(root) || !canonicalLinuxClonePath(target) || !within(root, target) || root == target {
		return nil, errors.New("Linux clone cleanup target identity is incomplete")
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return nil, errors.New("Linux clone cleanup target is not beneath its configured root")
	}
	components := strings.Split(relative, string(filepath.Separator))
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return nil, errors.New("Linux clone cleanup target contains an invalid path component")
		}
	}
	rootFD, rootInfo, err := openLinuxCloneOPathDirPath(root)
	if err != nil {
		return nil, fmt.Errorf("pin configured B clone root with O_PATH: %w", err)
	}
	targetInfo := &linuxCloneCleanupTarget{
		rootFD: rootFD, targetFD: -1, rootPath: root, target: target,
		rootInfo: rootInfo, missingAt: len(components) - 1,
		ancestors: make([]linuxCloneNamedDirectory, 0, len(components)-1),
	}
	currentFD := rootFD
	for index, component := range components {
		if err := ctx.Err(); err != nil {
			if currentFD != rootFD {
				_ = unix.Close(currentFD)
			}
			_ = targetInfo.close()
			return nil, err
		}
		var named unix.Stat_t
		if err := unix.Fstatat(currentFD, component, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if !errors.Is(err, unix.ENOENT) {
				if currentFD != rootFD {
					_ = unix.Close(currentFD)
				}
				_ = targetInfo.close()
				return nil, fmt.Errorf("inspect exact B cleanup path component %q: %w", component, err)
			}
			var readback unix.Stat_t
			if readbackErr := unix.Fstatat(currentFD, component, &readback, unix.AT_SYMLINK_NOFOLLOW); errors.Is(readbackErr, unix.ENOENT) {
				if currentFD != rootFD {
					_ = unix.Close(currentFD)
				}
				targetInfo.missingAt = index
				if err := targetInfo.verifyAbsent(ctx); err != nil {
					_ = targetInfo.close()
					return nil, err
				}
				return targetInfo, nil
			} else if readbackErr != nil {
				if currentFD != rootFD {
					_ = unix.Close(currentFD)
				}
				_ = targetInfo.close()
				return nil, fmt.Errorf("recheck exact B cleanup path component %q: %w", component, readbackErr)
			}
			if currentFD != rootFD {
				_ = unix.Close(currentFD)
			}
			_ = targetInfo.close()
			return nil, fmt.Errorf("B cleanup path component %q appeared during absence readback", component)
		}
		if named.Mode&unix.S_IFMT != unix.S_IFDIR {
			if currentFD != rootFD {
				_ = unix.Close(currentFD)
			}
			_ = targetInfo.close()
			return nil, fmt.Errorf("B cleanup path component %q is not a real directory", component)
		}
		if index == len(components)-1 && (uint64(named.Dev) != checkout.TargetDevice || uint64(named.Ino) != checkout.TargetInode) {
			if currentFD != rootFD {
				_ = unix.Close(currentFD)
			}
			_ = targetInfo.close()
			return nil, errors.New("B cleanup target inode differs from its checkout receipt")
		}
		nextFD, err := openLinuxCloneOPathDirAt(currentFD, component)
		if err != nil {
			if currentFD != rootFD {
				_ = unix.Close(currentFD)
			}
			_ = targetInfo.close()
			return nil, fmt.Errorf("open exact B cleanup path component %q: %w", component, err)
		}
		var opened unix.Stat_t
		if err := unix.Fstat(nextFD, &opened); err != nil {
			_ = unix.Close(nextFD)
			if currentFD != rootFD {
				_ = unix.Close(currentFD)
			}
			_ = targetInfo.close()
			return nil, fmt.Errorf("read pinned B cleanup path component %q: %w", component, err)
		}
		if !sameLinuxCloneDirectoryIdentity(&named, &opened) {
			_ = unix.Close(nextFD)
			if currentFD != rootFD {
				_ = unix.Close(currentFD)
			}
			_ = targetInfo.close()
			return nil, fmt.Errorf("B cleanup path component %q changed while pinning", component)
		}
		if index == len(components)-1 {
			targetInfo.targetFD = nextFD
			if currentFD != rootFD {
				_ = unix.Close(currentFD)
			}
			if uint64(opened.Dev) != checkout.TargetDevice || uint64(opened.Ino) != checkout.TargetInode {
				_ = targetInfo.close()
				return nil, errors.New("pinned B cleanup target inode differs from its checkout receipt")
			}
			return targetInfo, nil
		}
		targetInfo.ancestors = append(targetInfo.ancestors, linuxCloneNamedDirectory{name: component, identity: opened})
		if currentFD != rootFD {
			_ = unix.Close(currentFD)
		}
		currentFD = nextFD
	}
	if currentFD != rootFD {
		_ = unix.Close(currentFD)
	}
	_ = targetInfo.close()
	return nil, errors.New("Linux clone cleanup target traversal did not reach the target")
}

func (target *linuxCloneCleanupTarget) verifyAbsent(ctx context.Context) error {
	if target == nil || target.rootFD < 0 || ctx == nil {
		return errors.New("Linux clone cleanup absence handle is invalid")
	}
	for range 2 {
		if err := target.verifyRootPinned(ctx); err != nil {
			return err
		}
		if err := verifyLinuxCloneCleanupTargetAbsentAtRoot(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func (target *linuxCloneCleanupTarget) verifyRootPinned(ctx context.Context) error {
	if target == nil || target.rootFD < 0 || ctx == nil {
		return errors.New("Linux clone cleanup root handle is invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var pinnedRoot unix.Stat_t
	if err := unix.Fstat(target.rootFD, &pinnedRoot); err != nil {
		return fmt.Errorf("stat pinned B root: %w", err)
	}
	if !sameLinuxCloneDirectoryIdentity(&target.rootInfo, &pinnedRoot) {
		return errors.New("pinned B root changed during cleanup")
	}
	pathRootFD, pathRootInfo, err := openLinuxCloneOPathDirPath(target.rootPath)
	if err != nil {
		return fmt.Errorf("reopen configured B root for cleanup: %w", err)
	}
	if closeErr := unix.Close(pathRootFD); closeErr != nil {
		return fmt.Errorf("close configured B root readback descriptor: %w", closeErr)
	}
	if !sameLinuxCloneDirectoryIdentity(&target.rootInfo, &pathRootInfo) {
		return errors.New("configured B root changed during cleanup")
	}
	return nil
}

func verifyLinuxCloneCleanupTargetAbsentAtRoot(ctx context.Context, target *linuxCloneCleanupTarget) error {
	relative, err := filepath.Rel(target.rootPath, target.target)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return errors.New("Linux clone cleanup absence path is invalid")
	}
	components := strings.Split(relative, string(filepath.Separator))
	currentFD := target.rootFD
	for index, component := range components {
		if err := ctx.Err(); err != nil {
			if currentFD != target.rootFD {
				_ = unix.Close(currentFD)
			}
			return err
		}
		var named unix.Stat_t
		if err := unix.Fstatat(currentFD, component, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if !errors.Is(err, unix.ENOENT) {
				if currentFD != target.rootFD {
					_ = unix.Close(currentFD)
				}
				return fmt.Errorf("recheck B cleanup path component %q: %w", component, err)
			}
			var readback unix.Stat_t
			if readbackErr := unix.Fstatat(currentFD, component, &readback, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(readbackErr, unix.ENOENT) {
				if currentFD != target.rootFD {
					_ = unix.Close(currentFD)
				}
				if readbackErr != nil {
					return readbackErr
				}
				return errors.New("B cleanup path component reappeared during absence readback")
			}
			if index != target.missingAt {
				if currentFD != target.rootFD {
					_ = unix.Close(currentFD)
				}
				return errors.New("B cleanup path absence moved to a different component")
			}
			if currentFD != target.rootFD {
				_ = unix.Close(currentFD)
			}
			return nil
		}
		if index == target.missingAt || index == len(components)-1 {
			if currentFD != target.rootFD {
				_ = unix.Close(currentFD)
			}
			return errors.New("B cleanup target or missing ancestor reappeared")
		}
		if named.Mode&unix.S_IFMT != unix.S_IFDIR || index >= len(target.ancestors) ||
			!sameLinuxCloneDirectoryIdentity(&target.ancestors[index].identity, &named) {
			if currentFD != target.rootFD {
				_ = unix.Close(currentFD)
			}
			return fmt.Errorf("B cleanup parent %q changed during absence readback", component)
		}
		nextFD, err := openLinuxCloneOPathDirAt(currentFD, component)
		if err != nil {
			if currentFD != target.rootFD {
				_ = unix.Close(currentFD)
			}
			return fmt.Errorf("reopen B cleanup parent %q: %w", component, err)
		}
		var opened unix.Stat_t
		if err := unix.Fstat(nextFD, &opened); err != nil {
			_ = unix.Close(nextFD)
			if currentFD != target.rootFD {
				_ = unix.Close(currentFD)
			}
			return fmt.Errorf("read reopened B cleanup parent %q: %w", component, err)
		}
		if !sameLinuxCloneDirectoryIdentity(&target.ancestors[index].identity, &opened) {
			_ = unix.Close(nextFD)
			if currentFD != target.rootFD {
				_ = unix.Close(currentFD)
			}
			return fmt.Errorf("B cleanup parent %q changed while reopening", component)
		}
		if currentFD != target.rootFD {
			_ = unix.Close(currentFD)
		}
		currentFD = nextFD
	}
	if currentFD != target.rootFD {
		_ = unix.Close(currentFD)
	}
	return errors.New("B cleanup target absence was not observed")
}

func openLinuxCloneOwnedDirPath(path string) (int, unix.Stat_t, error) {
	if !canonicalLinuxClonePath(path) {
		return -1, unix.Stat_t{}, errors.New("A-owned directory path must be canonical and absolute")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	components := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, component := range components {
		if component == "" {
			continue
		}
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return -1, unix.Stat_t{}, openErr
		}
		fd = next
		var info unix.Stat_t
		if err := unix.Fstat(fd, &info); err != nil {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, err
		}
		if info.Mode&unix.S_IFMT != unix.S_IFDIR {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("A-owned path contains a non-directory component")
		}
		last := index == len(components)-1
		uid := uint32(info.Uid)
		if last {
			if uid != uint32(os.Geteuid()) || info.Mode&0o022 != 0 {
				_ = unix.Close(fd)
				return -1, unix.Stat_t{}, errors.New("final directory must be A-owned and not group/world writable")
			}
		} else if (uid != 0 && uid != uint32(os.Geteuid())) || (info.Mode&0o022 != 0 && (uid != 0 || info.Mode&0o1000 == 0)) {
			_ = unix.Close(fd)
			return -1, unix.Stat_t{}, errors.New("A-owned path has an unsafe ancestor")
		}
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, err
	}
	return fd, info, nil
}

func openLinuxCloneSeedParent(seedRoot, seedPath string, create bool) (int, string, error) {
	rootFD, rootInfo, err := openLinuxCloneOwnedDirPath(seedRoot)
	if err != nil {
		return -1, "", err
	}
	if uint32(rootInfo.Uid) != uint32(os.Geteuid()) || rootInfo.Mode&0o022 != 0 {
		_ = unix.Close(rootFD)
		return -1, "", errors.New("seed root must be A-owned and not group/world writable")
	}
	relativeParent, err := filepath.Rel(seedRoot, filepath.Dir(seedPath))
	if err != nil || relativeParent == ".." || strings.HasPrefix(relativeParent, ".."+string(filepath.Separator)) || filepath.IsAbs(relativeParent) {
		_ = unix.Close(rootFD)
		return -1, "", errors.New("seed parent escapes the A-owned seed root")
	}
	parentFD := rootFD
	if relativeParent != "." {
		for _, component := range strings.Split(relativeParent, string(filepath.Separator)) {
			if component == "" || component == "." || component == ".." {
				_ = unix.Close(parentFD)
				return -1, "", errors.New("seed parent contains an invalid path component")
			}
			if create {
				if err := unix.Mkdirat(parentFD, component, 0o750); err == nil {
					if err := unix.Fsync(parentFD); err != nil {
						_ = unix.Close(parentFD)
						return -1, "", err
					}
				} else if !errors.Is(err, unix.EEXIST) {
					_ = unix.Close(parentFD)
					return -1, "", err
				}
			}
			next, openErr := openLinuxCloneDirAt(parentFD, component)
			_ = unix.Close(parentFD)
			if openErr != nil {
				return -1, "", openErr
			}
			parentFD = next
			var info unix.Stat_t
			if err := unix.Fstat(parentFD, &info); err != nil {
				_ = unix.Close(parentFD)
				return -1, "", err
			}
			if uint32(info.Uid) != uint32(os.Geteuid()) || info.Mode&0o022 != 0 {
				_ = unix.Close(parentFD)
				return -1, "", errors.New("seed parent must be A-owned and not group/world writable")
			}
		}
	}
	name := filepath.Base(seedPath)
	if name == "." || name == ".." || name == string(filepath.Separator) || strings.ContainsAny(name, "/\x00\r\n") {
		_ = unix.Close(parentFD)
		return -1, "", errors.New("seed file name is invalid")
	}
	return parentFD, name, nil
}

func linuxCloneSeedTempName(nonce string) (string, error) {
	if len(nonce) != authority.LinuxCloneNonceBytes*2 || !lowerHex(nonce) {
		return "", errors.New("Linux clone seed temp nonce is invalid")
	}
	return ".clone-seed-" + nonce[:24], nil
}

type linuxCloneScratchEntries struct {
	seedTemp   bool
	quarantine bool
	template   bool
}

func linuxCloneScratchNames(nonce string) (seedTemp, quarantine, template string, err error) {
	seedTemp, err = linuxCloneSeedTempName(nonce)
	if err != nil {
		return "", "", "", err
	}
	suffix := strings.TrimPrefix(seedTemp, ".clone-seed-")
	return seedTemp, ".symmetry-clone-quarantine-" + suffix, ".symmetry-clone-template-" + suffix, nil
}

func inspectLinuxCloneScratchEntries(ctx context.Context, parentFD int, intent authority.LinuxCloneIntent) (linuxCloneScratchEntries, error) {
	seedTemp, quarantine, template, err := linuxCloneScratchNames(intent.Nonce)
	if err != nil {
		return linuxCloneScratchEntries{}, err
	}
	names, err := readLinuxCloneDirNames(ctx, parentFD)
	if err != nil {
		return linuxCloneScratchEntries{}, err
	}
	var entries linuxCloneScratchEntries
	for _, name := range names {
		switch {
		case strings.HasPrefix(name, ".clone-seed-"):
			if name != seedTemp {
				return linuxCloneScratchEntries{}, fmt.Errorf("%w: unrecognized A seed temp %q", ErrLinuxCloneProcessUnresolved, name)
			}
			entries.seedTemp = true
		case strings.HasPrefix(name, ".symmetry-clone-quarantine-"):
			if name != quarantine {
				return linuxCloneScratchEntries{}, fmt.Errorf("%w: unrecognized A clone quarantine %q", ErrLinuxCloneProcessUnresolved, name)
			}
			entries.quarantine = true
		case strings.HasPrefix(name, ".symmetry-clone-template-"):
			if name != template {
				return linuxCloneScratchEntries{}, fmt.Errorf("%w: unrecognized A clone template %q", ErrLinuxCloneProcessUnresolved, name)
			}
			entries.template = true
		case strings.HasPrefix(name, ".symmetry-clone-"):
			return linuxCloneScratchEntries{}, fmt.Errorf("%w: unrecognized A clone scratch %q", ErrLinuxCloneProcessUnresolved, name)
		}
	}
	return entries, nil
}

func removeLinuxClonePromotionScratch(ctx context.Context, parentFD int, intent authority.LinuxCloneIntent) error {
	_, quarantine, template, err := linuxCloneScratchNames(intent.Nonce)
	if err != nil {
		return err
	}
	entries, err := inspectLinuxCloneScratchEntries(ctx, parentFD, intent)
	if err != nil {
		return err
	}
	if entries.seedTemp {
		return fmt.Errorf("%w: exact seed temp remains during A promotion cleanup", ErrLinuxCloneProcessUnresolved)
	}
	if entries.template {
		if err := removeLinuxCloneScratchDirectory(ctx, parentFD, template); err != nil {
			return err
		}
	}
	if entries.quarantine {
		if err := removeLinuxCloneScratchDirectory(ctx, parentFD, quarantine); err != nil {
			return err
		}
	}
	remaining, err := inspectLinuxCloneScratchEntries(ctx, parentFD, intent)
	if err != nil {
		return err
	}
	if remaining.seedTemp || remaining.quarantine || remaining.template {
		return errors.New("A-owned Linux clone promotion scratch remains after cleanup")
	}
	return nil
}

// removeLinuxCloneCleanupScratch requires the caller's A-writer fence plus
// the immutable State cleanup Begin. It retires only nonce-bound entries and
// never sweeps a scratch-name prefix.
func removeLinuxCloneCleanupScratch(ctx context.Context, parentFD int, intent authority.LinuxCloneIntent) error {
	seedTemp, quarantine, template, err := linuxCloneScratchNames(intent.Nonce)
	if err != nil {
		return err
	}
	entries, err := inspectLinuxCloneScratchEntries(ctx, parentFD, intent)
	if err != nil {
		return err
	}
	if entries.seedTemp {
		if err := removeLinuxCloneSeedTempChecked(parentFD, seedTemp, nil); err != nil {
			return fmt.Errorf("remove exact A-owned Linux clone seed temp: %w", err)
		}
	}
	if entries.template {
		if err := removeLinuxCloneScratchDirectory(ctx, parentFD, template); err != nil {
			return err
		}
	}
	if entries.quarantine {
		if err := removeLinuxCloneScratchDirectory(ctx, parentFD, quarantine); err != nil {
			return err
		}
	}
	remaining, err := inspectLinuxCloneScratchEntries(ctx, parentFD, intent)
	if err != nil {
		return err
	}
	if remaining.seedTemp || remaining.quarantine || remaining.template {
		return errors.New("exact A-owned Linux clone scratch remains after cleanup")
	}
	return nil
}

func removeLinuxCloneScratchDirectory(ctx context.Context, parentFD int, name string) error {
	if !strings.HasPrefix(name, ".symmetry-clone-quarantine-") && !strings.HasPrefix(name, ".symmetry-clone-template-") {
		return errors.New("Linux clone scratch name is invalid")
	}
	suffix := name[strings.LastIndexByte(name, '-')+1:]
	if len(suffix) != 24 || !lowerHex(suffix) {
		return errors.New("Linux clone scratch name is invalid")
	}
	var named unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return verifyLinuxCloneEntryAbsent(parentFD, name)
		}
		return err
	}
	if named.Mode&unix.S_IFMT != unix.S_IFDIR || uint32(named.Uid) != uint32(os.Geteuid()) || named.Mode&0o077 != 0 {
		return errors.New("exact A clone scratch is not a private A-owned directory")
	}
	directoryFD, err := openLinuxCloneDirAt(parentFD, name)
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	var opened unix.Stat_t
	if err := unix.Fstat(directoryFD, &opened); err != nil || !sameLinuxCloneDirectoryIdentity(&named, &opened) {
		return errors.New("exact A clone scratch changed while opening")
	}
	entryCount := 0
	if err := removeLinuxCloneOwnedScratchContents(ctx, directoryFD, named.Dev, &entryCount, 0); err != nil {
		return fmt.Errorf("remove exact A clone scratch contents: %w", err)
	}
	var current unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameLinuxCloneDirectoryIdentity(&named, &current) {
		return errors.New("exact A clone scratch name changed during removal")
	}
	if err := unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR); err != nil {
		return fmt.Errorf("remove exact A clone scratch directory: %w", err)
	}
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync exact A clone scratch removal: %w", err)
	}
	return verifyLinuxCloneEntryAbsent(parentFD, name)
}

func removeLinuxCloneOwnedScratchContents(ctx context.Context, directoryFD int, device uint64, entryCount *int, depth int) error {
	if depth > 16 {
		return errors.New("A clone scratch directory depth exceeds its bound")
	}
	names, err := readLinuxCloneDirNames(ctx, directoryFD)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		*entryCount = *entryCount + 1
		if *entryCount > maxLinuxCloneDirectoryEntries {
			return errLinuxCloneObjectsTooLarge
		}
		var before unix.Stat_t
		if err := unix.Fstatat(directoryFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if uint64(before.Dev) != device || uint32(before.Uid) != uint32(os.Geteuid()) || before.Mode&0o022 != 0 {
			return fmt.Errorf("A clone scratch entry %q is foreign, cross-device, or writable by another principal", name)
		}
		switch before.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			childFD, err := openLinuxCloneDirAt(directoryFD, name)
			if err != nil {
				return err
			}
			var opened unix.Stat_t
			if err := unix.Fstat(childFD, &opened); err != nil || !sameLinuxCloneDirectoryIdentity(&before, &opened) {
				_ = unix.Close(childFD)
				return fmt.Errorf("A clone scratch directory %q changed while opening", name)
			}
			if err := removeLinuxCloneOwnedScratchContents(ctx, childFD, device, entryCount, depth+1); err != nil {
				_ = unix.Close(childFD)
				return err
			}
			var current unix.Stat_t
			if err := unix.Fstatat(directoryFD, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameLinuxCloneDirectoryIdentity(&before, &current) {
				_ = unix.Close(childFD)
				return fmt.Errorf("A clone scratch directory %q changed during removal", name)
			}
			if err := unix.Unlinkat(directoryFD, name, unix.AT_REMOVEDIR); err != nil {
				_ = unix.Close(childFD)
				return err
			}
			_ = unix.Close(childFD)
			if err := verifyLinuxCloneEntryAbsent(directoryFD, name); err != nil {
				return err
			}
		case unix.S_IFREG:
			if before.Nlink != 1 {
				return fmt.Errorf("A clone scratch file %q has unexpected hard links", name)
			}
			if err := unix.Unlinkat(directoryFD, name, 0); err != nil {
				return err
			}
			if err := verifyLinuxCloneEntryAbsent(directoryFD, name); err != nil {
				return err
			}
		default:
			return fmt.Errorf("A clone scratch entry %q has an unsupported type", name)
		}
	}
	return unix.Fsync(directoryFD)
}

func verifyLinuxCloneEntryAbsent(directoryFD int, name string) error {
	var info unix.Stat_t
	if err := unix.Fstatat(directoryFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		var readback unix.Stat_t
		if readbackErr := unix.Fstatat(directoryFD, name, &readback, unix.AT_SYMLINK_NOFOLLOW); errors.Is(readbackErr, unix.ENOENT) {
			return nil
		} else if readbackErr != nil {
			return readbackErr
		}
		return errors.New("A clone scratch entry reappeared during absence readback")
	} else if err != nil {
		return err
	}
	return errors.New("A clone scratch entry remains after removal")
}

func createLinuxCloneSeedTemp(parentFD int, name string) (*os.File, error) {
	if !strings.HasPrefix(name, ".clone-seed-") || !lowerHex(strings.TrimPrefix(name, ".clone-seed-")) || len(name) != len(".clone-seed-")+24 {
		return nil, errors.New("Linux clone seed temp name is invalid")
	}
	fd, err := unix.Openat(parentFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

// removeLinuxCloneSeedTempChecked removes only the deterministic temp named by
// this intent after the caller's RunJournal fence/operation mutex proves no A
// writer can remain. It never scans or removes a temp-name prefix.
func removeLinuxCloneSeedTempChecked(parentFD int, name string, expected *authority.LinuxCloneSeedReceipt) error {
	if !strings.HasPrefix(name, ".clone-seed-") || len(name) != len(".clone-seed-")+24 || !lowerHex(strings.TrimPrefix(name, ".clone-seed-")) {
		return errors.New("Linux clone seed temp name is invalid")
	}
	var before unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || uint32(before.Uid) != uint32(os.Geteuid()) ||
		before.Nlink != 1 || before.Mode&0o077 != 0 || before.Size < 0 || before.Size > authority.LinuxCloneMaxSeedBytes {
		return errors.New("exact Linux clone seed temp is not a private A-owned regular file")
	}
	if expected != nil && (uint64(before.Dev) != expected.Device || uint64(before.Ino) != expected.Inode || before.Size != expected.Size) {
		return errors.New("exact Linux clone seed temp differs from the Store receipt")
	}
	if err := unix.Unlinkat(parentFD, name, 0); err != nil {
		if absentErr := verifyLinuxCloneSeedAbsent(parentFD, name); absentErr != nil {
			return fmt.Errorf("unlink exact Linux clone seed temp: %w; readback: %v", err, absentErr)
		}
	}
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync exact Linux clone seed temp removal: %w", err)
	}
	return verifyLinuxCloneSeedAbsent(parentFD, name)
}

func openLinuxCloneTargetParent(root, target string) (int, string, error) {
	if !canonicalLinuxClonePath(root) || !canonicalLinuxClonePath(target) || !within(root, target) || root == target {
		return -1, "", errors.New("Linux clone target must be a canonical path beneath its B root")
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return -1, "", errors.New("Linux clone target parent is invalid")
	}
	components := strings.Split(relative, string(filepath.Separator))
	name := components[len(components)-1]
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00\r\n") {
		return -1, "", errors.New("Linux clone target name is invalid")
	}
	parentFD, _, err := openLinuxCloneDirPath(root)
	if err != nil {
		return -1, "", err
	}
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." || component == ".." {
			_ = unix.Close(parentFD)
			return -1, "", errors.New("Linux clone target parent contains an invalid path component")
		}
		next, openErr := openLinuxCloneDirAt(parentFD, component)
		_ = unix.Close(parentFD)
		if openErr != nil {
			return -1, "", openErr
		}
		parentFD = next
	}
	return parentFD, name, nil
}

func openLinuxCloneTarget(intent authority.LinuxCloneIntent, receipt authority.LinuxCloneCheckoutReceipt, root string) (*linuxCloneTarget, error) {
	parentFD, name, err := openLinuxCloneTargetParent(root, intent.Target)
	if err != nil {
		return nil, err
	}
	var named unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		_ = unix.Close(parentFD)
		return nil, err
	}
	if named.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(named.Dev) != receipt.TargetDevice || uint64(named.Ino) != receipt.TargetInode {
		_ = unix.Close(parentFD)
		return nil, errors.New("Linux clone target inode differs from the checkout receipt")
	}
	fd, err := openLinuxCloneDirAt(parentFD, name)
	if err != nil {
		_ = unix.Close(parentFD)
		return nil, err
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		_ = unix.Close(parentFD)
		return nil, err
	}
	if !sameLinuxCloneDirectoryIdentity(&named, &opened) || opened.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint64(opened.Dev) != receipt.TargetDevice || uint64(opened.Ino) != receipt.TargetInode {
		_ = unix.Close(fd)
		_ = unix.Close(parentFD)
		return nil, errors.New("Linux clone target changed while opening the checkout receipt inode")
	}
	return &linuxCloneTarget{
		parentFD: parentFD, fd: fd, name: name, identity: opened,
		root: root, intent: intent.Clone(), receipt: receipt.Clone(),
	}, nil
}

func verifyLinuxCloneTargetStillBound(target *linuxCloneTarget) error {
	if target == nil || target.fd < 0 || target.parentFD < 0 {
		return errors.New("Linux clone target descriptor is unavailable")
	}
	var opened, named unix.Stat_t
	if err := unix.Fstat(target.fd, &opened); err != nil {
		return err
	}
	if err := unix.Fstatat(target.parentFD, target.name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if !sameLinuxCloneDirectoryIdentity(&target.identity, &opened) || !sameLinuxCloneDirectoryIdentity(&target.identity, &named) ||
		opened.Mode&unix.S_IFMT != unix.S_IFDIR || named.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("Linux clone target name or inode changed during object extraction")
	}
	currentPath, err := openLinuxCloneTarget(target.intent, target.receipt, target.root)
	if err != nil {
		return fmt.Errorf("Linux clone target path no longer names the checkout receipt inode: %w", err)
	}
	if err := currentPath.close(); err != nil {
		return err
	}
	return nil
}

func verifyLinuxCloneTargetAbsentUnderRoot(ctx context.Context, root, target string) error {
	return verifyLinuxCloneTargetAbsentUnderRootWithReadback(ctx, root, target, nil)
}

func verifyLinuxCloneTargetAbsentUnderRootWithReadback(ctx context.Context, root, target string, afterAbsent func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !canonicalLinuxClonePath(root) || !canonicalLinuxClonePath(target) || !within(root, target) || root == target {
		return errors.New("Linux clone no-target check escapes the configured B root")
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return errors.New("Linux clone no-target path is invalid")
	}
	components := strings.Split(relative, string(filepath.Separator))
	parentFD, rootIdentity, err := openLinuxCloneDirPath(root)
	if err != nil {
		return fmt.Errorf("open configured B clone root: %w", err)
	}
	defer func() { _ = unix.Close(parentFD) }()
	ancestors := make([]linuxCloneNamedDirectory, 0, len(components))
	for index, component := range components {
		if component == "" || component == "." || component == ".." {
			return errors.New("Linux clone target contains an invalid path component")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var named unix.Stat_t
		if err := unix.Fstatat(parentFD, component, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if !errors.Is(err, unix.ENOENT) {
				return err
			}
			var readback unix.Stat_t
			if readbackErr := unix.Fstatat(parentFD, component, &readback, unix.AT_SYMLINK_NOFOLLOW); errors.Is(readbackErr, unix.ENOENT) {
				if afterAbsent != nil {
					afterAbsent()
				}
				return verifyLinuxCloneTargetAbsentAfterRewalk(ctx, root, rootIdentity, ancestors, component)
			} else if readbackErr != nil {
				return readbackErr
			}
			return errors.New("Linux clone target parent appeared during no-target readback")
		}
		if index == len(components)-1 {
			return errors.New("Linux clone target already exists without a checkout receipt")
		}
		if named.Mode&unix.S_IFMT != unix.S_IFDIR {
			return errors.New("Linux clone target parent is not a real directory")
		}
		childFD, err := openLinuxCloneDirAt(parentFD, component)
		if err != nil {
			return err
		}
		var opened unix.Stat_t
		if err := unix.Fstat(childFD, &opened); err != nil || !sameLinuxCloneDirectoryIdentity(&named, &opened) {
			_ = unix.Close(childFD)
			return errors.New("Linux clone target parent changed during no-target readback")
		}
		ancestors = append(ancestors, linuxCloneNamedDirectory{name: component, identity: opened})
		_ = unix.Close(parentFD)
		parentFD = childFD
	}
	return errors.New("Linux clone target absence was not observed")
}

type linuxCloneNamedDirectory struct {
	name     string
	identity unix.Stat_t
}

func verifyLinuxCloneTargetAbsentAfterRewalk(ctx context.Context, root string, rootIdentity unix.Stat_t, ancestors []linuxCloneNamedDirectory, missingName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parentFD, currentRoot, err := openLinuxCloneDirPath(root)
	if err != nil {
		return fmt.Errorf("reopen configured B clone root: %w", err)
	}
	defer func() { _ = unix.Close(parentFD) }()
	if !sameLinuxCloneDirectoryIdentity(&rootIdentity, &currentRoot) {
		return errors.New("configured B clone root changed during no-target observation")
	}
	for _, ancestor := range ancestors {
		if err := ctx.Err(); err != nil {
			return err
		}
		var named unix.Stat_t
		if err := unix.Fstatat(parentFD, ancestor.name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return fmt.Errorf("recheck B clone parent %q: %w", ancestor.name, err)
		}
		if !sameLinuxCloneDirectoryIdentity(&ancestor.identity, &named) {
			return fmt.Errorf("B clone parent %q changed during no-target observation", ancestor.name)
		}
		childFD, err := openLinuxCloneDirAt(parentFD, ancestor.name)
		if err != nil {
			return fmt.Errorf("reopen B clone parent %q: %w", ancestor.name, err)
		}
		var opened unix.Stat_t
		if err := unix.Fstat(childFD, &opened); err != nil || !sameLinuxCloneDirectoryIdentity(&ancestor.identity, &opened) {
			_ = unix.Close(childFD)
			return fmt.Errorf("B clone parent %q changed during no-target rewalk", ancestor.name)
		}
		_ = unix.Close(parentFD)
		parentFD = childFD
	}
	var absent unix.Stat_t
	if err := unix.Fstatat(parentFD, missingName, &absent, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		var readback unix.Stat_t
		if readbackErr := unix.Fstatat(parentFD, missingName, &readback, unix.AT_SYMLINK_NOFOLLOW); errors.Is(readbackErr, unix.ENOENT) {
			return nil
		} else if readbackErr != nil {
			return readbackErr
		}
		return errors.New("Linux clone target reappeared during final path readback")
	} else if err != nil {
		return err
	}
	return errors.New("Linux clone target exists on the current B root path")
}

func sameLinuxCloneDirectoryIdentity(left, right *unix.Stat_t) bool {
	return left != nil && right != nil && left.Dev == right.Dev && left.Ino == right.Ino &&
		left.Mode&unix.S_IFMT == unix.S_IFDIR && right.Mode&unix.S_IFMT == unix.S_IFDIR
}

func createLinuxCloneQuarantine(ctx context.Context, target *linuxCloneTarget, quarantineRoot string, intent authority.LinuxCloneIntent, objectIDLength int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if target == nil || target.fd < 0 || intent.Validate() != nil || filepath.Dir(intent.SeedPath) != quarantineRoot ||
		!canonicalLinuxClonePath(quarantineRoot) || (objectIDLength != 40 && objectIDLength != 64) {
		return "", errors.New("Linux clone quarantine identity is incomplete")
	}
	rootFD, rootInfo, err := openLinuxCloneOwnedDirPath(quarantineRoot)
	if err != nil {
		return "", fmt.Errorf("open A-owned clone quarantine parent: %w", err)
	}
	defer unix.Close(rootFD)
	if uint32(rootInfo.Uid) != uint32(os.Geteuid()) || rootInfo.Mode&0o022 != 0 {
		return "", errors.New("clone quarantine parent must be A-owned and not group/world writable")
	}
	_, quarantineName, templateName, err := linuxCloneScratchNames(intent.Nonce)
	if err != nil {
		return "", err
	}
	scratch, err := inspectLinuxCloneScratchEntries(ctx, rootFD, intent)
	if err != nil {
		return "", err
	}
	if scratch.seedTemp || scratch.quarantine || scratch.template {
		return "", fmt.Errorf("%w: A clone scratch for this intent already exists", ErrLinuxCloneProcessUnresolved)
	}
	gitFD, err := openLinuxCloneDirAt(target.fd, ".git")
	if err != nil {
		return "", fmt.Errorf("standalone clone .git must be a real directory: %w", err)
	}
	defer unix.Close(gitFD)
	var gitInfo unix.Stat_t
	if err := unix.Fstat(gitFD, &gitInfo); err != nil {
		return "", err
	}
	if err := rejectLinuxCloneEntry(gitFD, "shallow"); err != nil {
		return "", fmt.Errorf("reject shallow B clone: %w", err)
	}
	objectsFD, err := openLinuxCloneDirAt(gitFD, "objects")
	if err != nil {
		return "", fmt.Errorf("open B clone object directory: %w", err)
	}
	defer unix.Close(objectsFD)
	var objectsInfo unix.Stat_t
	if err := unix.Fstat(objectsFD, &objectsInfo); err != nil {
		return "", err
	}
	if err := rejectLinuxCloneObjectAlternates(ctx, objectsFD); err != nil {
		return "", err
	}
	quarantinePath := filepath.Join(quarantineRoot, quarantineName)
	templatePath := filepath.Join(quarantineRoot, templateName)
	quarantineCreated := false
	templateCreated := false
	cleanupCreated := func(cause error) (string, error) {
		if errors.Is(cause, ErrLinuxCloneProcessUnresolved) {
			return "", cause
		}
		cleanupCtx := context.WithoutCancel(ctx)
		var cleanupErr error
		if templateCreated {
			cleanupErr = errors.Join(cleanupErr, removeLinuxCloneScratchDirectory(cleanupCtx, rootFD, templateName))
		}
		if quarantineCreated {
			cleanupErr = errors.Join(cleanupErr, removeLinuxCloneScratchDirectory(cleanupCtx, rootFD, quarantineName))
		}
		return "", errors.Join(cause, cleanupErr)
	}
	if err := unix.Mkdirat(rootFD, quarantineName, 0o700); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return "", fmt.Errorf("%w: deterministic A clone quarantine already exists", ErrLinuxCloneProcessUnresolved)
		}
		return "", fmt.Errorf("create deterministic A clone quarantine: %w", err)
	}
	quarantineCreated = true
	if err := unix.Fsync(rootFD); err != nil {
		return cleanupCreated(fmt.Errorf("sync deterministic A clone quarantine creation: %w", err))
	}
	if err := unix.Mkdirat(rootFD, templateName, 0o700); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return cleanupCreated(fmt.Errorf("%w: deterministic A clone template already exists", ErrLinuxCloneProcessUnresolved))
		}
		return cleanupCreated(fmt.Errorf("create deterministic A clone template: %w", err))
	}
	templateCreated = true
	if err := unix.Fsync(rootFD); err != nil {
		return cleanupCreated(fmt.Errorf("sync deterministic A clone template creation: %w", err))
	}
	quarantineFD, quarantineInfo, err := openLinuxCloneOwnedDirPath(quarantinePath)
	if err != nil {
		return cleanupCreated(err)
	}
	_ = unix.Close(quarantineFD)
	if uint32(quarantineInfo.Uid) != uint32(os.Geteuid()) || quarantineInfo.Mode&0o077 != 0 {
		return cleanupCreated(errors.New("A-only clone quarantine directory is not private"))
	}
	templateFD, templateInfo, err := openLinuxCloneOwnedDirPath(templatePath)
	if err != nil {
		return cleanupCreated(err)
	}
	_ = unix.Close(templateFD)
	if uint32(templateInfo.Uid) != uint32(os.Geteuid()) || templateInfo.Mode&0o077 != 0 {
		return cleanupCreated(errors.New("A-only clone template directory is not private"))
	}
	objectFormat := "sha1"
	if objectIDLength == 64 {
		objectFormat = "sha256"
	}
	_, initErr := linuxCloneGitOutput(ctx, quarantinePath, 64<<10, errors.New("Git init output exceeds limit"), "init", "--bare", "--object-format="+objectFormat, "--template="+templatePath)
	if initErr != nil {
		return cleanupCreated(fmt.Errorf("initialize A-only clone quarantine: %w", initErr))
	}
	if err := removeLinuxCloneScratchDirectory(context.WithoutCancel(ctx), rootFD, templateName); err != nil {
		return cleanupCreated(fmt.Errorf("remove exact empty Git template directory: %w", err))
	}
	templateCreated = false
	quarantineFD, _, err = openLinuxCloneOwnedDirPath(quarantinePath)
	if err != nil {
		return cleanupCreated(err)
	}
	defer unix.Close(quarantineFD)
	destinationObjectsFD, err := openLinuxCloneDirAt(quarantineFD, "objects")
	if err != nil {
		return cleanupCreated(err)
	}
	defer unix.Close(destinationObjectsFD)
	if err := unix.Fchmod(destinationObjectsFD, 0o700); err != nil {
		return cleanupCreated(err)
	}
	if err := copyLinuxCloneObjectStore(ctx, objectsFD, destinationObjectsFD, objectIDLength); err != nil {
		return cleanupCreated(err)
	}
	if err := verifyLinuxCloneDirectoryStillBound(gitFD, target.fd, ".git", &gitInfo); err != nil {
		return cleanupCreated(err)
	}
	if err := verifyLinuxCloneDirectoryStillBound(objectsFD, gitFD, "objects", &objectsInfo); err != nil {
		return cleanupCreated(err)
	}
	if err := verifyLinuxCloneTargetStillBound(target); err != nil {
		return cleanupCreated(err)
	}
	return quarantinePath, nil
}

func openLinuxCloneDirAt(parentFD int, name string) (int, error) {
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
	}
	fd, err := unix.Openat2(parentFD, name, how)
	if err != nil {
		return -1, err
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return -1, errors.New("entry is not a real directory")
	}
	return fd, nil
}

func openLinuxCloneFileAt(parentFD int, name string, flags int) (int, error) {
	how := &unix.OpenHow{
		Flags:   uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
	}
	return unix.Openat2(parentFD, name, how)
}

func rejectLinuxCloneEntry(directoryFD int, name string) error {
	var info unix.Stat_t
	err := unix.Fstatat(directoryFD, name, &info, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("unsupported B Git metadata entry %q is present", name)
}

func rejectLinuxCloneObjectAlternates(ctx context.Context, objectsFD int) error {
	infoFD, err := openLinuxCloneDirAt(objectsFD, "info")
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("B Git object info directory is unsupported: %w", err)
	}
	defer unix.Close(infoFD)
	names, err := readLinuxCloneDirNames(ctx, infoFD)
	if err != nil {
		return err
	}
	if len(names) != 0 {
		return errors.New("B Git object info files, including alternates, are unsupported")
	}
	return nil
}

func copyLinuxCloneObjectStore(ctx context.Context, sourceFD, destinationFD int, objectIDLength int) error {
	names, err := readLinuxCloneDirNames(ctx, sourceFD)
	if err != nil {
		return err
	}
	var totalBytes int64
	var copiedFiles int
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch name {
		case "info":
			infoFD, err := openLinuxCloneDirAt(sourceFD, name)
			if err != nil {
				return fmt.Errorf("open B Git object info directory: %w", err)
			}
			var infoStat unix.Stat_t
			if err := unix.Fstat(infoFD, &infoStat); err != nil {
				_ = unix.Close(infoFD)
				return err
			}
			infoNames, readErr := readLinuxCloneDirNames(ctx, infoFD)
			identityErr := verifyLinuxCloneDirectoryStillBound(infoFD, sourceFD, name, &infoStat)
			_ = unix.Close(infoFD)
			if readErr != nil {
				return readErr
			}
			if identityErr != nil {
				return identityErr
			}
			if len(infoNames) != 0 {
				return errors.New("B Git object info contains unsupported metadata or alternates")
			}
		case "pack":
			packFD, err := openLinuxCloneDirAt(sourceFD, name)
			if err != nil {
				return fmt.Errorf("open B Git pack directory: %w", err)
			}
			var packInfo unix.Stat_t
			if err := unix.Fstat(packFD, &packInfo); err != nil {
				_ = unix.Close(packFD)
				return err
			}
			destinationPackFD, openErr := openLinuxCloneDirAt(destinationFD, name)
			if openErr != nil {
				_ = unix.Close(packFD)
				return openErr
			}
			packFiles, copyErr := copyLinuxClonePackDirectory(ctx, packFD, destinationPackFD, objectIDLength, &totalBytes, &copiedFiles)
			identityErr := verifyLinuxCloneDirectoryStillBound(packFD, sourceFD, name, &packInfo)
			_ = unix.Close(destinationPackFD)
			_ = unix.Close(packFD)
			if copyErr != nil {
				return copyErr
			}
			if identityErr != nil {
				return identityErr
			}
			if err := verifyLinuxClonePackPairs(packFiles); err != nil {
				return err
			}
			if err := unix.Fsync(destinationFD); err != nil {
				return err
			}
		default:
			if len(name) != 2 || !lowerHex(name) {
				return fmt.Errorf("%w: unexpected B object-store entry %q", errLinuxCloneUnsupportedObjects, name)
			}
			fanoutFD, err := openLinuxCloneDirAt(sourceFD, name)
			if err != nil {
				return fmt.Errorf("%w: open loose-object fanout %q: %v", errLinuxCloneUnsupportedObjects, name, err)
			}
			var fanoutInfo unix.Stat_t
			if err := unix.Fstat(fanoutFD, &fanoutInfo); err != nil {
				_ = unix.Close(fanoutFD)
				return err
			}
			if err := unix.Mkdirat(destinationFD, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
				_ = unix.Close(fanoutFD)
				return err
			}
			destinationFanoutFD, openErr := openLinuxCloneDirAt(destinationFD, name)
			if openErr != nil {
				_ = unix.Close(fanoutFD)
				return openErr
			}
			if err := copyLinuxCloneLooseDirectory(ctx, fanoutFD, destinationFanoutFD, name, objectIDLength, &totalBytes, &copiedFiles); err != nil {
				_ = unix.Close(destinationFanoutFD)
				_ = unix.Close(fanoutFD)
				return err
			}
			identityErr := verifyLinuxCloneDirectoryStillBound(fanoutFD, sourceFD, name, &fanoutInfo)
			_ = unix.Close(destinationFanoutFD)
			_ = unix.Close(fanoutFD)
			if identityErr != nil {
				return identityErr
			}
		}
	}
	if copiedFiles == 0 {
		return errors.New("B Git object store is empty")
	}
	return unix.Fsync(destinationFD)
}

func copyLinuxCloneLooseDirectory(ctx context.Context, sourceFD, destinationFD int, fanout string, objectIDLength int, totalBytes *int64, copiedFiles *int) error {
	names, err := readLinuxCloneDirNames(ctx, sourceFD)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(name) != objectIDLength-2 || !lowerHex(name) || len(fanout+name) != objectIDLength || !lowerHex(fanout+name) {
			return fmt.Errorf("%w: invalid loose-object name %q", errLinuxCloneUnsupportedObjects, name)
		}
		if err := copyLinuxCloneObjectFile(ctx, sourceFD, destinationFD, name, totalBytes, copiedFiles); err != nil {
			return err
		}
	}
	return unix.Fsync(destinationFD)
}

func copyLinuxClonePackDirectory(ctx context.Context, sourceFD, destinationFD int, objectIDLength int, totalBytes *int64, copiedFiles *int) (map[string]uint8, error) {
	names, err := readLinuxCloneDirNames(ctx, sourceFD)
	if err != nil {
		return nil, err
	}
	pairs := make(map[string]uint8, len(names)/2)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		extension := ""
		var bit uint8
		switch {
		case strings.HasPrefix(name, "pack-") && strings.HasSuffix(name, ".pack"):
			extension, bit = ".pack", 1
		case strings.HasPrefix(name, "pack-") && strings.HasSuffix(name, ".idx"):
			extension, bit = ".idx", 2
		case strings.HasPrefix(name, "pack-") && strings.HasSuffix(name, ".rev"):
			// Git 2.43+ may emit an optional reverse index. Copy the exact
			// bounded bytes; quarantine Git validates/uses it with its pack.
			extension, bit = ".rev", 4
		default:
			return nil, fmt.Errorf("%w: unsupported pack entry %q", errLinuxCloneUnsupportedObjects, name)
		}
		stem := strings.TrimSuffix(name, extension)
		hash := strings.TrimPrefix(stem, "pack-")
		if len(hash) != objectIDLength || !lowerHex(hash) {
			return nil, fmt.Errorf("%w: invalid pack name %q", errLinuxCloneUnsupportedObjects, name)
		}
		if pairs[stem]&bit != 0 {
			return nil, fmt.Errorf("%w: duplicate pack entry %q", errLinuxCloneUnsupportedObjects, name)
		}
		pairs[stem] |= bit
		if err := copyLinuxCloneObjectFile(ctx, sourceFD, destinationFD, name, totalBytes, copiedFiles); err != nil {
			return nil, err
		}
	}
	if err := unix.Fsync(destinationFD); err != nil {
		return nil, err
	}
	return pairs, nil
}

func verifyLinuxClonePackPairs(pairs map[string]uint8) error {
	for stem, bits := range pairs {
		if bits&3 != 3 || bits&^uint8(7) != 0 {
			return fmt.Errorf("%w: pack %q lacks a matching .pack/.idx pair", errLinuxCloneUnsupportedObjects, stem)
		}
	}
	return nil
}

func copyLinuxCloneObjectFile(ctx context.Context, sourceParentFD, destinationParentFD int, name string, totalBytes *int64, copiedFiles *int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var namedBefore unix.Stat_t
	if err := unix.Fstatat(sourceParentFD, name, &namedBefore, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if namedBefore.Mode&unix.S_IFMT != unix.S_IFREG || namedBefore.Size <= 0 {
		return fmt.Errorf("%w: object entry %q is not a non-empty regular file", errLinuxCloneUnsupportedObjects, name)
	}
	if *copiedFiles >= maxLinuxCloneObjectFiles || namedBefore.Size > maxLinuxCloneObjectBytes-*totalBytes {
		return errLinuxCloneObjectsTooLarge
	}
	sourceFD, err := openLinuxCloneFileAt(sourceParentFD, name, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return fmt.Errorf("open B Git object %q: %w", name, err)
	}
	source := os.NewFile(uintptr(sourceFD), name)
	defer source.Close()
	var openedBefore unix.Stat_t
	if err := unix.Fstat(sourceFD, &openedBefore); err != nil || !sameLinuxCloneFileIdentity(&namedBefore, &openedBefore) {
		return fmt.Errorf("B Git object %q changed while opening", name)
	}
	destinationFD, err := unix.Openat(destinationParentFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create A-only quarantine object %q: %w", name, err)
	}
	destination := os.NewFile(uintptr(destinationFD), name)
	destinationOpen := true
	defer func() {
		if destinationOpen {
			_ = destination.Close()
		}
	}()
	hasher := sha256.New()
	written, err := io.CopyN(io.MultiWriter(destination, hasher), linuxCloneContextReader{ctx: ctx, reader: source}, openedBefore.Size)
	if err != nil || written != openedBefore.Size {
		return fmt.Errorf("copy B Git object %q: %w", name, linuxCloneErrorOr(err, io.ErrUnexpectedEOF))
	}
	var extra [1]byte
	contextSource := linuxCloneContextReader{ctx: ctx, reader: source}
	if count, readErr := contextSource.Read(extra[:]); readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	} else if count != 0 || !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("B Git object %q grew during copy", name)
	}
	if err := destination.Sync(); err != nil {
		return fmt.Errorf("sync A-only quarantine object %q: %w", name, err)
	}
	if err := destination.Close(); err != nil {
		return fmt.Errorf("close A-only quarantine object %q: %w", name, err)
	}
	destinationOpen = false
	var sourceAfter unix.Stat_t
	if err := unix.Fstat(sourceFD, &sourceAfter); err != nil || !sameLinuxCloneFileIdentity(&openedBefore, &sourceAfter) {
		return fmt.Errorf("B Git object %q changed during copy", name)
	}
	var sourceNamedAfter unix.Stat_t
	if err := unix.Fstatat(sourceParentFD, name, &sourceNamedAfter, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameLinuxCloneFileIdentity(&openedBefore, &sourceNamedAfter) {
		return fmt.Errorf("B Git object name %q changed during copy", name)
	}
	if err := verifyLinuxCloneCopiedFile(ctx, destinationParentFD, name, openedBefore.Size, hasher.Sum(nil)); err != nil {
		return fmt.Errorf("verify A-only quarantine object %q: %w", name, err)
	}
	if err := verifyLinuxCloneSourceFileReadback(ctx, sourceParentFD, name, openedBefore, hasher.Sum(nil)); err != nil {
		return fmt.Errorf("verify B Git object readback %q: %w", name, err)
	}
	*totalBytes += openedBefore.Size
	(*copiedFiles)++
	return nil
}

func verifyLinuxCloneCopiedFile(ctx context.Context, parentFD int, name string, expectedSize int64, expectedDigest []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Size != expectedSize {
		return errors.New("quarantine object type or size differs")
	}
	hasher := sha256.New()
	size, err := io.Copy(hasher, io.LimitReader(linuxCloneContextReader{ctx: ctx, reader: file}, expectedSize+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errors.New("quarantine object byte readback differs")
	}
	if size != expectedSize || !equalBytes(hasher.Sum(nil), expectedDigest) {
		return errors.New("quarantine object byte readback differs")
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || !sameLinuxCloneFileIdentity(&before, &after) {
		return errors.New("quarantine object changed during readback")
	}
	var named unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameLinuxCloneFileIdentity(&before, &named) {
		return errors.New("quarantine object name changed during readback")
	}
	return nil
}

func verifyLinuxCloneSourceFileReadback(ctx context.Context, parentFD int, name string, original unix.Stat_t, expectedDigest []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := openLinuxCloneFileAt(parentFD, name, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil || !sameLinuxCloneFileIdentity(&original, &before) {
		return errors.New("B Git object inode changed before readback")
	}
	hasher := sha256.New()
	size, err := io.Copy(hasher, io.LimitReader(linuxCloneContextReader{ctx: ctx, reader: file}, original.Size+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errors.New("B Git object byte readback differs")
	}
	if size != original.Size || !equalBytes(hasher.Sum(nil), expectedDigest) {
		return errors.New("B Git object byte readback differs")
	}
	var after, named unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || !sameLinuxCloneFileIdentity(&before, &after) {
		return errors.New("B Git object changed during readback")
	}
	if err := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameLinuxCloneFileIdentity(&original, &named) {
		return errors.New("B Git object name changed during readback")
	}
	return nil
}

func readLinuxCloneDirNames(ctx context.Context, directoryFD int) ([]string, error) {
	fd, err := unix.Openat(directoryFD, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(fd), "Linux clone directory")
	defer directory.Close()
	names := make([]string, 0)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, readErr := directory.ReadDir(128)
		for _, entry := range entries {
			name := entry.Name()
			if name == "" || strings.ContainsAny(name, "/\x00\r\n") {
				return nil, errors.New("directory returned an invalid entry name")
			}
			if len(names) >= maxLinuxCloneDirectoryEntries {
				return nil, errLinuxCloneObjectsTooLarge
			}
			names = append(names, name)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return names, nil
}

func lowerHex(value string) bool {
	if value == "" || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func verifyLinuxCloneDirectoryStillBound(openedFD, parentFD int, name string, expected *unix.Stat_t) error {
	if expected == nil || openedFD < 0 || parentFD < 0 {
		return nil
	}
	var opened, named unix.Stat_t
	if err := unix.Fstat(openedFD, &opened); err != nil {
		return err
	}
	if err := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if !sameLinuxCloneDirectoryIdentity(expected, &opened) || !sameLinuxCloneDirectoryIdentity(expected, &named) {
		return errors.New("B Git object directory changed during copy")
	}
	return nil
}
