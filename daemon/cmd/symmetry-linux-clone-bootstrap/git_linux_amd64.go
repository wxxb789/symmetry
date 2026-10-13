//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

const (
	gitFDInChild      = 3
	seedFDInChild     = 4
	templateFDChild   = 5
	gitOutputMaxBytes = 64 << 10
)

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		return 0, errors.New("fixed Git output exceeded its bound")
	}
	if len(value) > remaining {
		n, _ := buffer.buffer.Write(value[:remaining])
		return n, errors.New("fixed Git output exceeded its bound")
	}
	return buffer.buffer.Write(value)
}

type gitCommandError struct {
	cause error
}

func (failure *gitCommandError) Error() string {
	if failure == nil || failure.cause == nil {
		return "fixed Git command failed"
	}
	return failure.cause.Error()
}

func (failure *gitCommandError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func listBundleHeads(ctx context.Context, args bootstrapArgs, seedFD int) ([]string, error) {
	output, err := runGit(ctx, args, seedFD, -1, gitOutputMaxBytes, "bundle", "list-heads", fmt.Sprintf("/proc/self/fd/%d", seedFDInChild))
	if err != nil {
		return nil, err
	}
	text := string(output)
	if !strings.HasSuffix(text, "\n") {
		return nil, errors.New("fixed Git bundle list-heads output is not newline terminated")
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for _, line := range lines {
		if line == "" || strings.ContainsAny(line, "\r\x00") {
			return nil, errors.New("fixed Git bundle list-heads output is malformed")
		}
	}
	return lines, nil
}

func initializeAndCheckout(ctx context.Context, args bootstrapArgs, seedFD int, target *cloneTarget,
	grant authority.LinuxCloneGrant, baseRef string) error {
	templateFD, templateName, err := createEmptyGitTemplate(target.fd)
	if err != nil {
		return fmt.Errorf("create private empty Git template: %w", err)
	}
	templateRemoved := false
	defer func() {
		if !templateRemoved {
			_ = removeEmptyGitTemplate(target.fd, templateFD, templateName)
		}
	}()
	initOutput, initErr := runGit(ctx, args, seedFD, templateFD, 4096,
		"init", "--quiet", "--template="+fmt.Sprintf("/proc/self/fd/%d", templateFDChild))
	clear(initOutput)
	removeErr := removeEmptyGitTemplate(target.fd, templateFD, templateName)
	templateRemoved = removeErr == nil
	closeErr := unix.Close(templateFD)
	if initErr != nil || removeErr != nil || closeErr != nil {
		return errors.Join(initErr, removeErr, closeErr)
	}

	fetchRefspec := "+" + baseRef + ":refs/heads/symmetry/base"
	fetchOutput, err := runGit(ctx, args, seedFD, -1, 4096,
		"fetch", "--no-tags", "--no-recurse-submodules", "--force",
		fmt.Sprintf("/proc/self/fd/%d", seedFDInChild), fetchRefspec)
	clear(fetchOutput)
	if err != nil {
		return err
	}
	verifyOutput, err := runGit(ctx, args, seedFD, -1, 4096,
		"cat-file", "-e", grant.Intent.BaseCommit+"^{commit}")
	clear(verifyOutput)
	if err != nil {
		return err
	}
	checkoutOutput, err := runGit(ctx, args, seedFD, -1, 4096,
		"checkout", "--quiet", "--detach", "--force", grant.Intent.BaseCommit)
	clear(checkoutOutput)
	if err != nil {
		return err
	}
	return nil
}

func verifyStandaloneCheckout(ctx context.Context, args bootstrapArgs, seedFD int, target *cloneTarget,
	grant authority.LinuxCloneGrant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	head, err := runGit(ctx, args, seedFD, -1, 4096, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(head)) != grant.Intent.BaseCommit {
		clear(head)
		return errors.New("fresh B checkout HEAD differs from the admitted base commit")
	}
	clear(head)
	top, err := runGit(ctx, args, seedFD, -1, 4096, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if strings.TrimSuffix(string(top), "\n") != grant.Intent.Target {
		clear(top)
		return errors.New("fresh B checkout worktree root differs from the exact target path")
	}
	clear(top)
	common, err := runGit(ctx, args, seedFD, -1, 4096, "rev-parse", "--git-common-dir")
	if err != nil {
		return err
	}
	if strings.TrimSuffix(string(common), "\n") != ".git" {
		clear(common)
		return errors.New("fresh B checkout is not a standalone Git repository")
	}
	clear(common)
	return verifyStandaloneGitMetadata(target.fd)
}

func createEmptyGitTemplate(targetFD int) (int, string, error) {
	const name = ".symmetry-empty-git-template"
	if err := unix.Mkdirat(targetFD, name, 0o700); err != nil {
		return -1, "", fmt.Errorf("create empty Git template directory: %w", err)
	}
	if err := unix.Fsync(targetFD); err != nil {
		return -1, "", fmt.Errorf("sync checkout after creating empty template: %w", err)
	}
	fd, err := unix.Openat(targetFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		_ = unix.Unlinkat(targetFD, name, unix.AT_REMOVEDIR)
		return -1, "", err
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		_ = unix.Close(fd)
		_ = unix.Unlinkat(targetFD, name, unix.AT_REMOVEDIR)
		return -1, "", fmt.Errorf("stat empty Git template directory: %w", err)
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Uid != 1 || info.Gid != 1 {
		_ = unix.Close(fd)
		_ = unix.Unlinkat(targetFD, name, unix.AT_REMOVEDIR)
		return -1, "", errors.New("empty Git template is not a B-owned directory")
	}
	if err := requireEmptyDirectory(fd); err != nil {
		_ = unix.Close(fd)
		_ = unix.Unlinkat(targetFD, name, unix.AT_REMOVEDIR)
		return -1, "", err
	}
	return fd, name, nil
}

func removeEmptyGitTemplate(targetFD, templateFD int, name string) error {
	if templateFD < 0 || !safeComponent(name) {
		return errors.New("empty Git template removal identity is invalid")
	}
	if err := requireEmptyDirectory(templateFD); err != nil {
		return fmt.Errorf("Git template changed before removal: %w", err)
	}
	var pinned, named unix.Stat_t
	if err := unix.Fstat(templateFD, &pinned); err != nil {
		return err
	}
	if err := unix.Fstatat(targetFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if !sameStatIdentity(&pinned, &named) || named.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("empty Git template pathname changed before removal")
	}
	if err := unix.Unlinkat(targetFD, name, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	return unix.Fsync(targetFD)
}

func openNativeImage(path string) (*os.File, unix.Stat_t, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" ||
		strings.ContainsRune(path, '\x00') {
		return nil, unix.Stat_t{}, errors.New("native image path is not a canonical absolute executable path")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, unix.Stat_t{}, fmt.Errorf("open approved native image without following final symlink: %w", err)
	}
	file := os.NewFile(uintptr(fd), "approved-native-image")
	if file == nil {
		_ = unix.Close(fd)
		return nil, unix.Stat_t{}, errors.New("wrap approved native image descriptor")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, unix.Stat_t{}, fmt.Errorf("stat approved native image: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0111 == 0 || stat.Mode&06000 != 0 {
		_ = file.Close()
		return nil, unix.Stat_t{}, errors.New("approved native image is not a non-set-ID executable regular file")
	}
	var header [20]byte
	for {
		n, readErr := unix.Pread(fd, header[:], 0)
		if errors.Is(readErr, unix.EINTR) {
			continue
		}
		if readErr != nil || n != len(header) {
			_ = file.Close()
			if readErr != nil {
				return nil, unix.Stat_t{}, fmt.Errorf("read approved native ELF header: %w", readErr)
			}
			return nil, unix.Stat_t{}, errors.New("approved native ELF header is truncated")
		}
		break
	}
	if string(header[:4]) != "\x7fELF" || header[4] != 2 || header[5] != 1 ||
		binary.LittleEndian.Uint16(header[16:18]) != unix.ET_EXEC && binary.LittleEndian.Uint16(header[16:18]) != unix.ET_DYN ||
		binary.LittleEndian.Uint16(header[18:20]) != unix.EM_X86_64 {
		_ = file.Close()
		return nil, unix.Stat_t{}, errors.New("approved native image is not Linux/amd64 ELF")
	}
	return file, stat, nil
}

func verifyStandaloneGitMetadata(targetFD int) error {
	gitFD, err := unix.Openat(targetFD, ".git", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open fresh B .git directory: %w", err)
	}
	defer unix.Close(gitFD)
	var gitInfo unix.Stat_t
	if err := unix.Fstat(gitFD, &gitInfo); err != nil {
		return fmt.Errorf("stat fresh .git metadata: %w", err)
	}
	if gitInfo.Mode&unix.S_IFMT != unix.S_IFDIR || gitInfo.Uid != 1 || gitInfo.Gid != 1 {
		return errors.New("fresh .git metadata is not a B-owned directory")
	}
	for _, entry := range []struct {
		parent int
		name   string
	}{
		{parent: gitFD, name: "commondir"},
		{parent: gitFD, name: "worktrees"},
	} {
		if err := requireAbsentAt(entry.parent, entry.name); err != nil {
			return fmt.Errorf("fresh .git metadata is linked or not standalone: %w", err)
		}
	}
	objectsFD, err := unix.Openat(gitFD, "objects", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open fresh .git object directory: %w", err)
	}
	defer unix.Close(objectsFD)
	infoFD, err := unix.Openat(objectsFD, "info", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("open fresh .git object info directory: %w", err)
	}
	if err == nil {
		defer unix.Close(infoFD)
		if err := requireAbsentAt(infoFD, "alternates"); err != nil {
			return fmt.Errorf("fresh .git uses an external object alternate: %w", err)
		}
	}
	return nil
}

func requireAbsentAt(parentFD int, name string) error {
	if !safeComponent(name) {
		return errors.New("metadata path component is invalid")
	}
	var info unix.Stat_t
	err := unix.Fstatat(parentFD, name, &info, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("forbidden Git metadata entry exists")
}

func runGit(ctx context.Context, args bootstrapArgs, seedFD, templateFD, outputLimit int, gitArgs ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, &gitCommandError{cause: context.Cause(ctx)}
	}
	gitImage, err := duplicateFile(args.gitFD, "fixed-git-image")
	if err != nil {
		return nil, &gitCommandError{cause: err}
	}
	defer gitImage.Close()
	seedFile, err := duplicateFile(seedFD, "immutable-clone-bundle")
	if err != nil {
		return nil, &gitCommandError{cause: err}
	}
	defer seedFile.Close()
	extraFiles := []*os.File{gitImage, seedFile}
	if templateFD >= 0 {
		templateFile, dupErr := duplicateFile(templateFD, "empty-git-template")
		if dupErr != nil {
			return nil, &gitCommandError{cause: dupErr}
		}
		defer templateFile.Close()
		extraFiles = append(extraFiles, templateFile)
	}
	stderrFile, err := duplicateFile(args.stderrFD, "native-stderr")
	if err != nil {
		return nil, &gitCommandError{cause: err}
	}
	defer stderrFile.Close()
	stdin, err := os.Open("/dev/null")
	if err != nil {
		return nil, &gitCommandError{cause: err}
	}
	defer stdin.Close()
	var stdout *os.File
	if outputLimit <= 0 {
		stdout, err = os.OpenFile("/dev/null", os.O_WRONLY, 0)
		if err != nil {
			return nil, &gitCommandError{cause: err}
		}
		defer stdout.Close()
	}

	argv := make([]string, 0, 8+len(gitArgs))
	argv = append(argv,
		"-c", "core.hooksPath=/dev/null",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
		"-c", "core.attributesFile=/dev/null",
	)
	argv = append(argv, gitArgs...)
	cmd := exec.Command(fmt.Sprintf("/proc/self/fd/%d", gitFDInChild), argv...)
	cmd.Args[0] = "git"
	cmd.Env = []string{
		"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "HOME=/nonexistent",
		"XDG_CONFIG_HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0",
		"GIT_NO_REPLACE_OBJECTS=1",
	}
	cmd.ExtraFiles = extraFiles
	cmd.Stdin = stdin
	cmd.Stderr = stderrFile
	cmd.WaitDelay = 2 * time.Second
	capture := &boundedBuffer{limit: outputLimit}
	if outputLimit > 0 {
		cmd.Stdout = capture
	} else {
		cmd.Stdout = stdout
	}
	if err := cmd.Start(); err != nil {
		return nil, &gitCommandError{cause: fmt.Errorf("start fixed Git image: %w", err)}
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case waitErr := <-wait:
		if waitErr != nil {
			return nil, &gitCommandError{cause: fmt.Errorf("fixed Git operation failed: %w", waitErr)}
		}
		if err := ctx.Err(); err != nil {
			return nil, &gitCommandError{cause: context.Cause(ctx)}
		}
		return append([]byte(nil), capture.buffer.Bytes()...), nil
	case <-ctx.Done():
		cause := context.Cause(ctx)
		if cause == nil {
			cause = ctx.Err()
		}
		killErr := cmd.Process.Kill()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		select {
		case waitErr := <-wait:
			return nil, &gitCommandError{cause: errors.Join(cause, killErr, waitErr)}
		case <-time.After(5 * time.Second):
			return nil, &gitCommandError{cause: errors.Join(cause, killErr, errors.New("fixed Git child did not settle after SIGKILL"))}
		}
	}
}

func duplicateFile(fd int, name string) (*os.File, error) {
	copyFD, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 10)
	if err != nil {
		return nil, fmt.Errorf("duplicate %s descriptor: %w", name, err)
	}
	file := os.NewFile(uintptr(copyFD), name)
	if file == nil {
		_ = unix.Close(copyFD)
		return nil, fmt.Errorf("wrap %s descriptor", name)
	}
	return file, nil
}
