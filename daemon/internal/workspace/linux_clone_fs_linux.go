//go:build linux && amd64

package workspace

import (
	"context"
	"crypto/rand"
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
	if !canonicalLinuxClonePath(path) {
		return -1, unix.Stat_t{}, errors.New("directory path must be canonical and absolute")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if component == "" {
			continue
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
		return -1, unix.Stat_t{}, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, errors.New("path is not a directory")
	}
	return fd, info, nil
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

func createLinuxCloneSeedTemp(parentFD int) (*os.File, string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return nil, "", err
		}
		name := ".clone-seed-" + hex.EncodeToString(random)
		fd, err := unix.Openat(parentFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if err == nil {
			file := os.NewFile(uintptr(fd), name)
			return file, name, nil
		}
		if !errors.Is(err, unix.EEXIST) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("could not allocate a unique Linux clone seed temporary")
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

func verifyLinuxCloneTargetAbsent(parentFD int, name string) error {
	var info unix.Stat_t
	err := unix.Fstatat(parentFD, name, &info, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("Linux clone target reappeared during cleanup readback")
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

func createLinuxCloneQuarantine(ctx context.Context, target *linuxCloneTarget, quarantineRoot string, objectIDLength int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if target == nil || target.fd < 0 || !canonicalLinuxClonePath(quarantineRoot) || (objectIDLength != 40 && objectIDLength != 64) {
		return "", errors.New("Linux clone quarantine identity is incomplete")
	}
	rootFD, rootInfo, err := openLinuxCloneOwnedDirPath(quarantineRoot)
	if err != nil {
		return "", fmt.Errorf("open A-owned clone quarantine parent: %w", err)
	}
	_ = unix.Close(rootFD)
	if uint32(rootInfo.Uid) != uint32(os.Geteuid()) || rootInfo.Mode&0o022 != 0 {
		return "", errors.New("clone quarantine parent must be A-owned and not group/world writable")
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
	quarantine, err := os.MkdirTemp(quarantineRoot, ".symmetry-clone-quarantine-")
	if err != nil {
		return "", err
	}
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.RemoveAll(quarantine)
		}
	}()
	quarantineFD, quarantineInfo, err := openLinuxCloneOwnedDirPath(quarantine)
	if err != nil {
		return "", err
	}
	_ = unix.Close(quarantineFD)
	if uint32(quarantineInfo.Uid) != uint32(os.Geteuid()) || quarantineInfo.Mode&0o077 != 0 {
		return "", errors.New("A-only clone quarantine directory is not private")
	}
	objectFormat := "sha1"
	if objectIDLength == 64 {
		objectFormat = "sha256"
	}
	templatePath, err := os.MkdirTemp(quarantineRoot, ".symmetry-clone-template-")
	if err != nil {
		return "", err
	}
	_, initErr := linuxCloneGitOutput(ctx, quarantine, 64<<10, errors.New("Git init output exceeds limit"), "init", "--bare", "--object-format="+objectFormat, "--template="+templatePath)
	if initErr != nil {
		if errors.Is(initErr, ErrLinuxCloneProcessUnresolved) {
			removeOnError = false
		} else if removeTemplateErr := os.Remove(templatePath); removeTemplateErr != nil {
			return "", errors.Join(fmt.Errorf("initialize A-only clone quarantine: %w", initErr), fmt.Errorf("remove empty Git template directory: %w", removeTemplateErr))
		}
		return "", fmt.Errorf("initialize A-only clone quarantine: %w", initErr)
	}
	if removeTemplateErr := os.Remove(templatePath); removeTemplateErr != nil {
		return "", fmt.Errorf("remove empty Git template directory: %w", removeTemplateErr)
	}
	quarantineFD, _, err = openLinuxCloneOwnedDirPath(quarantine)
	if err != nil {
		return "", err
	}
	defer unix.Close(quarantineFD)
	destinationObjectsFD, err := openLinuxCloneDirAt(quarantineFD, "objects")
	if err != nil {
		return "", err
	}
	defer unix.Close(destinationObjectsFD)
	if err := unix.Fchmod(destinationObjectsFD, 0o700); err != nil {
		return "", err
	}
	if err := copyLinuxCloneObjectStore(ctx, objectsFD, destinationObjectsFD, objectIDLength); err != nil {
		return "", err
	}
	if err := verifyLinuxCloneDirectoryStillBound(gitFD, target.fd, ".git", &gitInfo); err != nil {
		return "", err
	}
	if err := verifyLinuxCloneDirectoryStillBound(objectsFD, gitFD, "objects", &objectsInfo); err != nil {
		return "", err
	}
	if err := verifyLinuxCloneTargetStillBound(target); err != nil {
		return "", err
	}
	removeOnError = false
	return quarantine, nil
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

func removeLinuxCloneTarget(ctx context.Context, target *linuxCloneTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyLinuxCloneTargetStillBound(target); err != nil {
		return err
	}
	if err := removeLinuxCloneDirectoryContents(ctx, target.fd); err != nil {
		return err
	}
	if err := verifyLinuxCloneTargetStillBound(target); err != nil {
		return err
	}
	if err := unix.Unlinkat(target.parentFD, target.name, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	if err := unix.Fsync(target.parentFD); err != nil {
		return fmt.Errorf("sync removed Linux clone target parent: %w", err)
	}
	if err := verifyLinuxCloneTargetAbsent(target.parentFD, target.name); err != nil {
		return err
	}
	return verifyLinuxCloneTargetAbsentUnderRoot(ctx, target.root, target.intent.Target)
}

func removeLinuxCloneDirectoryContents(ctx context.Context, directoryFD int) error {
	names, err := readLinuxCloneDirNames(ctx, directoryFD)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		var before unix.Stat_t
		if err := unix.Fstatat(directoryFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if before.Mode&unix.S_IFMT == unix.S_IFDIR {
			childFD, err := openLinuxCloneDirAt(directoryFD, name)
			if err != nil {
				return err
			}
			var child unix.Stat_t
			if err := unix.Fstat(childFD, &child); err != nil || !sameLinuxCloneFileIdentity(&before, &child) {
				_ = unix.Close(childFD)
				return errors.New("B clone directory changed while opening for cleanup")
			}
			if err := removeLinuxCloneDirectoryContents(ctx, childFD); err != nil {
				_ = unix.Close(childFD)
				return err
			}
			if err := unix.Fstatat(directoryFD, name, &child, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameLinuxCloneDirectoryIdentity(&before, &child) {
				_ = unix.Close(childFD)
				return errors.New("B clone directory name changed during cleanup")
			}
			if err := unix.Unlinkat(directoryFD, name, unix.AT_REMOVEDIR); err != nil {
				_ = unix.Close(childFD)
				return err
			}
			_ = unix.Close(childFD)
		} else {
			if err := unix.Unlinkat(directoryFD, name, 0); err != nil {
				return err
			}
			var after unix.Stat_t
			if err := unix.Fstatat(directoryFD, name, &after, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
				if err != nil {
					return err
				}
				return errors.New("B clone entry reappeared during cleanup")
			}
		}
	}
	return unix.Fsync(directoryFD)
}
