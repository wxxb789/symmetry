package workspace

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// SubjectErrorCode identifies a fail-closed workspace subject error.
type SubjectErrorCode string

const (
	SubjectErrorInvalid          SubjectErrorCode = "invalid"
	SubjectErrorUnreachable      SubjectErrorCode = "unreachable"
	SubjectErrorMismatch         SubjectErrorCode = "mismatch"
	SubjectErrorUnsupportedTree  SubjectErrorCode = "unsupported_tree"
	SubjectErrorWorkspaceBinding SubjectErrorCode = "workspace_binding"
)

const (
	// These bounds protect the daemon from retaining untrusted Git output in
	// memory. The values are deliberately local to the workspace boundary:
	// artifact/tree sizes are not part of the wire contract or daemon config.
	maxSubjectArtifactBlobBytes = 16 << 20
	maxSubjectTreeOutputBytes   = 64 << 20
)

var (
	// ErrSubjectUnreachable means that the admitted commit is not a usable
	// commit object in the configured repository's object database.
	ErrSubjectUnreachable = errors.New("subject commit is unreachable")
	// ErrSubjectMismatch means that the requested subject does not describe the
	// artifact actually bound to the workspace or object database.
	ErrSubjectMismatch = errors.New("workspace subject mismatch")
	// ErrUnsupportedSubjectTree means that the committed tree contains an
	// entry for which the manifest contract has no fixed safe representation.
	ErrUnsupportedSubjectTree = errors.New("subject tree contains unsupported entry")
	// ErrSubjectArtifactTooLarge means that a committed blob is too large for
	// bounded in-memory workspace inspection.
	ErrSubjectArtifactTooLarge = errors.New("subject artifact exceeds size limit")
	// ErrSubjectTreeTooLarge means that the committed tree listing is too large
	// for bounded in-memory workspace inspection.
	ErrSubjectTreeTooLarge = errors.New("subject tree exceeds size limit")
)

// SubjectError is a typed, fail-closed error returned by subject-aware
// workspace operations. Expected and Actual are populated for identity and
// digest mismatches when available.
type SubjectError struct {
	Code       SubjectErrorCode
	BindingKey string
	Path       string
	Commit     string
	EntryPath  string
	EntryMode  string
	EntryType  string
	Expected   string
	Actual     string
	Cause      error
}

func (err *SubjectError) Error() string {
	if err == nil {
		return "<nil>"
	}
	parts := []string{string(err.Code)}
	if err.BindingKey != "" {
		parts = append(parts, fmt.Sprintf("binding=%q", err.BindingKey))
	}
	if err.Path != "" {
		parts = append(parts, fmt.Sprintf("path=%q", err.Path))
	}
	if err.Commit != "" {
		parts = append(parts, fmt.Sprintf("commit=%q", err.Commit))
	}
	if err.EntryPath != "" {
		parts = append(parts, fmt.Sprintf("entry=%q", err.EntryPath))
	}
	if err.EntryMode != "" || err.EntryType != "" {
		parts = append(parts, fmt.Sprintf("mode=%q type=%q", err.EntryMode, err.EntryType))
	}
	if err.Expected != "" || err.Actual != "" {
		parts = append(parts, fmt.Sprintf("expected=%q actual=%q", err.Expected, err.Actual))
	}
	message := strings.Join(parts, ": ")
	if err.Cause != nil {
		return message + ": " + err.Cause.Error()
	}
	return message
}

func (err *SubjectError) Unwrap() error {
	if err == nil {
		return nil
	}
	switch err.Code {
	case SubjectErrorUnreachable:
		return errors.Join(ErrSubjectUnreachable, err.Cause)
	case SubjectErrorMismatch:
		return errors.Join(ErrSubjectMismatch, err.Cause)
	case SubjectErrorUnsupportedTree:
		return errors.Join(ErrUnsupportedSubjectTree, err.Cause)
	default:
		return err.Cause
	}
}

// SubjectWorkspace is a workspace whose checked-out commit and committed
// tracked-artifact manifest have been verified against Subject.
type SubjectWorkspace struct {
	Prepared Prepared
	Subject  protocol.Subject
}

// MaterializeSubject resolves a configured Git worktree binding and derives a
// complete protocol.Subject from an exact commit object. It never consults the
// binding's mutable Ref after the commit argument has been supplied.
func (manager *Manager) MaterializeSubject(ctx context.Context, bindingKey, resourceID, commit string) (protocol.Subject, error) {
	if manager == nil {
		return protocol.Subject{}, errors.New("workspace manager is nil")
	}
	if err := validateSubjectContext(ctx); err != nil {
		return protocol.Subject{}, err
	}
	_, repository, err := manager.subjectBinding(ctx, bindingKey)
	if err != nil {
		return protocol.Subject{}, err
	}
	subject, err := manager.materializeAdmittedSubject(ctx, bindingKey, repository, resourceID, commit)
	if err != nil {
		return protocol.Subject{}, err
	}
	if err := subject.Validate(); err != nil {
		return protocol.Subject{}, &SubjectError{Code: SubjectErrorInvalid, BindingKey: bindingKey, Commit: commit, Cause: err}
	}
	return subject, nil
}

// PrepareSubject creates or recovers a daemon-owned detached worktree at the
// exact admitted commit and verifies its complete protocol.Subject. The
// configured binding Ref is intentionally ignored for this path.
func (manager *Manager) PrepareSubject(ctx context.Context, bindingKey string, run RunRef, admitted protocol.Subject) (SubjectWorkspace, error) {
	if manager == nil {
		return SubjectWorkspace{}, errors.New("workspace manager is nil")
	}
	if err := validateSubjectContext(ctx); err != nil {
		return SubjectWorkspace{}, err
	}
	if err := validateRun(run); err != nil {
		return SubjectWorkspace{}, err
	}
	binding, repository, err := manager.subjectBinding(ctx, bindingKey)
	if err != nil {
		return SubjectWorkspace{}, err
	}
	if err := admitted.Validate(); err != nil {
		return SubjectWorkspace{}, &SubjectError{Code: SubjectErrorInvalid, BindingKey: bindingKey, Commit: admitted.Commit, Cause: err}
	}
	materialized, err := manager.materializeAdmittedSubject(ctx, bindingKey, repository, admitted.ResourceID, admitted.Commit)
	if err != nil {
		return SubjectWorkspace{}, err
	}
	if materialized != admitted {
		return SubjectWorkspace{}, subjectMismatch(bindingKey, admitted.Commit, subjectDigestDescription(admitted), subjectDigestDescription(materialized))
	}
	prepared, err := manager.prepareWorktreeAt(ctx, bindingKey, binding, run, admitted.Commit, true)
	if err != nil {
		return SubjectWorkspace{}, err
	}
	verified, err := manager.VerifySubject(ctx, prepared, admitted)
	if err != nil {
		return SubjectWorkspace{}, err
	}
	return SubjectWorkspace{Prepared: prepared, Subject: verified}, nil
}

// RecoverSubject reconstructs a previously prepared subject workspace and
// verifies that it still belongs to the exact admitted subject.
func (manager *Manager) RecoverSubject(ctx context.Context, bindingKey string, run RunRef, persistedPath string, admitted protocol.Subject) (SubjectWorkspace, error) {
	if manager == nil {
		return SubjectWorkspace{}, errors.New("workspace manager is nil")
	}
	if err := validateSubjectContext(ctx); err != nil {
		return SubjectWorkspace{}, err
	}
	if err := validateRun(run); err != nil {
		return SubjectWorkspace{}, err
	}
	_, repository, err := manager.subjectBinding(ctx, bindingKey)
	if err != nil {
		return SubjectWorkspace{}, err
	}
	if err := admitted.Validate(); err != nil {
		return SubjectWorkspace{}, &SubjectError{Code: SubjectErrorInvalid, BindingKey: bindingKey, Commit: admitted.Commit, Cause: err}
	}
	materialized, err := manager.materializeAdmittedSubject(ctx, bindingKey, repository, admitted.ResourceID, admitted.Commit)
	if err != nil {
		return SubjectWorkspace{}, err
	}
	if materialized != admitted {
		return SubjectWorkspace{}, subjectMismatch(bindingKey, admitted.Commit, subjectDigestDescription(admitted), subjectDigestDescription(materialized))
	}
	prepared, err := manager.Recover(ctx, bindingKey, run, persistedPath)
	if err != nil {
		return SubjectWorkspace{}, err
	}
	verified, err := manager.VerifySubject(ctx, prepared, admitted)
	if err != nil {
		return SubjectWorkspace{}, err
	}
	return SubjectWorkspace{Prepared: prepared, Subject: verified}, nil
}

// VerifySubject checks ownership, repository identity, detached HEAD, exact
// commit and object-database manifest digest for a prepared workspace. The
// returned Subject is derived from Git, not from mutable files in the checkout.
func (manager *Manager) VerifySubject(ctx context.Context, prepared Prepared, expected protocol.Subject) (protocol.Subject, error) {
	if manager == nil {
		return protocol.Subject{}, errors.New("workspace manager is nil")
	}
	if err := validateSubjectContext(ctx); err != nil {
		return protocol.Subject{}, err
	}
	if err := expected.Validate(); err != nil {
		return protocol.Subject{}, &SubjectError{Code: SubjectErrorInvalid, BindingKey: prepared.BindingKey, Path: prepared.Path, Commit: expected.Commit, Cause: err}
	}
	repository, actualCommit, err := manager.verifyPreparedSubject(ctx, prepared)
	if err != nil {
		return protocol.Subject{}, err
	}
	if actualCommit != expected.Commit {
		return protocol.Subject{}, &SubjectError{Code: SubjectErrorMismatch, BindingKey: prepared.BindingKey, Path: prepared.Path, Expected: expected.Commit, Actual: actualCommit, Cause: ErrSubjectMismatch}
	}
	actual, err := manager.materializeAdmittedSubject(ctx, prepared.BindingKey, repository, expected.ResourceID, expected.Commit)
	if err != nil {
		return protocol.Subject{}, err
	}
	if actual != expected {
		return protocol.Subject{}, subjectMismatch(prepared.BindingKey, expected.Commit, subjectDigestDescription(expected), subjectDigestDescription(actual))
	}
	return actual, nil
}

// DeriveSubject verifies the daemon-owned detached worktree and derives a
// complete protocol.Subject from its actual checked-out commit. The caller
// supplies only the repository resource identity; commit and tree digest are
// read from the local Git object database.
func (manager *Manager) DeriveSubject(ctx context.Context, prepared Prepared, resourceID string) (protocol.Subject, error) {
	if manager == nil {
		return protocol.Subject{}, errors.New("workspace manager is nil")
	}
	if err := validateSubjectContext(ctx); err != nil {
		return protocol.Subject{}, err
	}
	if err := validateSubjectResourceID(resourceID); err != nil {
		return protocol.Subject{}, &SubjectError{Code: SubjectErrorInvalid, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: err}
	}
	repository, commit, err := manager.verifyPreparedSubject(ctx, prepared)
	if err != nil {
		return protocol.Subject{}, err
	}
	return manager.materializeAdmittedSubject(ctx, prepared.BindingKey, repository, resourceID, commit)
}

func (manager *Manager) verifyPreparedSubject(ctx context.Context, prepared Prepared) (string, string, error) {
	if !prepared.managed || prepared.repository == "" || prepared.root == "" {
		return "", "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: errors.New("subject verification requires a daemon-managed Git worktree")}
	}
	binding, repository, err := manager.subjectBinding(ctx, prepared.BindingKey)
	if err != nil {
		return "", "", err
	}
	if binding.Policy != config.WorkspacePolicyGitWorktree {
		return "", "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: errors.New("subject verification requires a git_worktree binding")}
	}
	if same, err := sameDirectory(repository, prepared.repository); err != nil {
		return "", "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: fmt.Errorf("compare configured repository: %w", err)}
	} else if !same {
		return "", "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: errors.New("prepared repository does not match configured repository")}
	}
	if err := manager.verifyPrepared(ctx, prepared); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", "", ctxErr
		}
		return "", "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: err}
	}
	actualCommit, err := verifySubjectWorktreeIdentity(ctx, prepared, repository)
	if err != nil {
		return "", "", err
	}
	return repository, actualCommit, nil
}

func (manager *Manager) materializeAdmittedSubject(ctx context.Context, bindingKey, repository, resourceID, commit string) (protocol.Subject, error) {
	if err := validateSubjectIdentity(resourceID, commit); err != nil {
		return protocol.Subject{}, err
	}
	if err := verifySubjectCommit(ctx, repository, commit); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return protocol.Subject{}, ctxErr
		}
		return protocol.Subject{}, subjectError(bindingKey, "", commit, SubjectErrorUnreachable, err)
	}
	treeDigest, err := gitTreeDigest(ctx, repository, commit)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return protocol.Subject{}, ctxErr
		}
		return protocol.Subject{}, subjectError(bindingKey, "", commit, subjectErrorCode(err), err)
	}
	return protocol.Subject{ResourceID: resourceID, Commit: commit, TreeDigest: treeDigest}, nil
}

func (manager *Manager) subjectBinding(ctx context.Context, bindingKey string) (config.Workspace, string, error) {
	if !validFingerprintBindingKey(bindingKey) {
		return config.Workspace{}, "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: bindingKey, Cause: errors.New("workspace binding key is invalid")}
	}
	binding, ok := manager.bindings[bindingKey]
	if !ok {
		return config.Workspace{}, "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: bindingKey, Cause: fmt.Errorf("workspace binding %q is not configured", bindingKey)}
	}
	if binding.Policy != config.WorkspacePolicyGitWorktree {
		return config.Workspace{}, "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: bindingKey, Cause: fmt.Errorf("workspace binding policy %q cannot bind a committed Subject", binding.Policy)}
	}
	repository, err := resolveDirectory(binding.Repository)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return config.Workspace{}, "", ctxErr
		}
		return config.Workspace{}, "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: bindingKey, Cause: fmt.Errorf("resolve workspace repository %q: %w", binding.Repository, err)}
	}
	root, err := gitDirectoryIdentity(ctx, repository, "--show-toplevel", repository)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return config.Workspace{}, "", ctxErr
		}
		return config.Workspace{}, "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: bindingKey, Cause: fmt.Errorf("identify workspace repository root: %w", err)}
	}
	if same, err := sameDirectory(root, repository); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return config.Workspace{}, "", ctxErr
		}
		return config.Workspace{}, "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: bindingKey, Cause: fmt.Errorf("compare workspace repository root: %w", err)}
	} else if !same {
		return config.Workspace{}, "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: bindingKey, Cause: errors.New("configured repository is not a Git worktree root")}
	}
	if _, err := gitDirectoryIdentity(ctx, repository, "--git-common-dir", repository); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return config.Workspace{}, "", ctxErr
		}
		return config.Workspace{}, "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: bindingKey, Cause: fmt.Errorf("identify workspace repository common directory: %w", err)}
	}
	return binding, repository, nil
}

func validateSubjectResourceID(resourceID string) error {
	placeholder := protocol.Subject{
		ResourceID: resourceID,
		Commit:     strings.Repeat("0", 40),
		TreeDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if err := placeholder.Validate(); err != nil {
		return err
	}
	return nil
}

func validateSubjectContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("subject context is nil")
	}
	return nil
}

func validateSubjectIdentity(resourceID, commit string) error {
	placeholder := protocol.Subject{
		ResourceID: resourceID,
		Commit:     commit,
		TreeDigest: "sha256:" + strings.Repeat("0", 64),
	}
	if err := placeholder.Validate(); err != nil {
		return &SubjectError{Code: SubjectErrorInvalid, Commit: commit, Cause: err}
	}
	return nil
}

func verifySubjectCommit(ctx context.Context, repository, commit string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	command := gitNoReplaceObjectsCommand(ctx, repository, "rev-parse", "--verify", commit+"^{commit}")
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		return fmt.Errorf("configure headless Git process: %w", err)
	}
	output, err := command.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("git commit %q is not reachable: %w", commit, err)
	}
	resolved := strings.TrimSpace(string(output))
	if resolved != commit {
		return fmt.Errorf("git resolved commit %q as %q", commit, resolved)
	}
	return nil
}

func verifySubjectWorktreeIdentity(ctx context.Context, prepared Prepared, repository string) (string, error) {
	root, err := gitDirectoryIdentity(ctx, prepared.Path, "--show-toplevel", prepared.Path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: fmt.Errorf("identify prepared worktree root: %w", err)}
	}
	if same, err := sameDirectory(root, prepared.Path); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: fmt.Errorf("compare prepared worktree root: %w", err)}
	} else if !same {
		return "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: errors.New("prepared path is not a Git worktree root")}
	}
	commonDirectory, err := gitDirectoryIdentity(ctx, prepared.Path, "--git-common-dir", prepared.Path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: fmt.Errorf("identify prepared worktree common directory: %w", err)}
	}
	repositoryCommonDirectory, err := gitDirectoryIdentity(ctx, repository, "--git-common-dir", repository)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: fmt.Errorf("identify configured common directory: %w", err)}
	}
	if same, err := sameDirectory(commonDirectory, repositoryCommonDirectory); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: fmt.Errorf("compare Git common directories: %w", err)}
	} else if !same {
		return "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: errors.New("prepared worktree uses a different Git common directory")}
	}
	detached, err := subjectWorktreeDetached(ctx, prepared.Path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", &SubjectError{Code: SubjectErrorWorkspaceBinding, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: err}
	}
	if !detached {
		return "", &SubjectError{Code: SubjectErrorMismatch, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: errors.New("prepared worktree HEAD is attached to a branch")}
	}
	actualCommit, err := runGitRevParse(ctx, prepared.Path, "HEAD^{commit}")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", &SubjectError{Code: SubjectErrorMismatch, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: fmt.Errorf("read prepared worktree commit: %w", err)}
	}
	if len(actualCommit) == 0 {
		return "", &SubjectError{Code: SubjectErrorMismatch, BindingKey: prepared.BindingKey, Path: prepared.Path, Cause: errors.New("prepared worktree has no commit")}
	}
	return actualCommit, nil
}

func subjectWorktreeDetached(ctx context.Context, path string) (bool, error) {
	command := gitNoReplaceObjectsCommand(ctx, path, "symbolic-ref", "--quiet", "HEAD")
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		return false, fmt.Errorf("configure headless Git process: %w", err)
	}
	err := command.Run()
	if err == nil {
		return false, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, fmt.Errorf("inspect detached HEAD: %w", err)
}

type treeManifestEntry struct {
	mode     string
	typeName string
	path     []byte
	objectID string
	digest   [sha256.Size]byte
}

type subjectGitOutputReader func(context.Context, string, int, error, ...string) ([]byte, error)
type subjectGitBatchOpener func(context.Context, string) (*subjectGitBatchStream, error)

// subjectGitBatchStream is one persistent `git cat-file --batch` child that
// serves the committed objects of a complete Subject digest. It retains no
// object content: every response is streamed into the caller's hash through
// one reusable buffer.
type subjectGitBatchStream struct {
	ctx      context.Context
	command  *exec.Cmd
	budget   context.CancelFunc
	sentinel error
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	request  []byte
	buffer   []byte
	// desynced marks a stream whose last response was not consumed to a record
	// boundary. Such a child is terminated instead of being asked to exit.
	desynced bool
}

func gitTreeDigest(ctx context.Context, repository, commit string) (string, error) {
	return gitTreeDigestWithReaders(ctx, repository, commit, subjectGitOutput, openSubjectGitBatch)
}

// subjectGitOutput runs the bounded workspace Git command used for the
// committed tree listing of the subject digest.
func subjectGitOutput(ctx context.Context, repository string, maxBytes int, limitErr error, arguments ...string) ([]byte, error) {
	command := gitNoReplaceObjectsCommand(ctx, repository, arguments...)
	return gitCommandOutputLimited(ctx, command, maxBytes, limitErr)
}

func gitTreeDigestWithReaders(ctx context.Context, repository, commit string, readOutput subjectGitOutputReader, openBatch subjectGitBatchOpener) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if readOutput == nil || openBatch == nil {
		return "", errors.New("subject Git readers are incomplete")
	}
	output, err := readOutput(ctx, repository, maxSubjectTreeOutputBytes, ErrSubjectTreeTooLarge, "ls-tree", "--full-tree", "-r", "-z", commit)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("list committed tree for %q: %w", commit, err)
	}
	entries, err := parseTreeManifest(output)
	if err != nil {
		return "", err
	}
	stream, err := openBatch(ctx, repository)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("open committed object batch stream for %q: %w", commit, err)
	}
	failure := hashTreeManifest(ctx, stream, entries)
	if closeErr := stream.close(failure); closeErr != nil {
		failure = closeErr
	}
	if failure != nil {
		return "", failure
	}
	sort.Slice(entries, func(left, right int) bool {
		return bytes.Compare(entries[left].path, entries[right].path) < 0
	})
	// The canonical manifest is a sequence of NUL-delimited records sorted by
	// raw Git path bytes: mode NUL type NUL path NUL sha256:<blob> NUL. It is
	// intentionally derived from committed Git objects, never checkout bytes,
	// and hashed record by record so no whole-manifest copy is retained.
	digest := sha256.New()
	record := make([]byte, 0, 256)
	for _, entry := range entries {
		record = record[:0]
		record = append(record, entry.mode...)
		record = append(record, 0)
		record = append(record, entry.typeName...)
		record = append(record, 0)
		record = append(record, entry.path...)
		record = append(record, 0)
		record = append(record, "sha256:"...)
		record = hex.AppendEncode(record, entry.digest[:])
		record = append(record, 0)
		digest.Write(record)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

// hashTreeManifest streams every committed blob through the persistent batch
// stream and records each entry digest in committed tree order.
func hashTreeManifest(ctx context.Context, stream *subjectGitBatchStream, entries []treeManifestEntry) error {
	hasher := sha256.New()
	for index := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		hasher.Reset()
		if err := stream.streamObject(entries[index].objectID, hasher, maxSubjectArtifactBlobBytes); err != nil {
			return fmt.Errorf("read committed blob %q: %w", string(entries[index].path), err)
		}
		// Sum appends into the entry digest storage, so streaming a committed
		// blob allocates no per-object buffer. The explicit copy keeps the
		// entry digest correct even if Sum returns a different backing array:
		// Hash.Sum does not promise that it reused the storage it was given.
		blobDigest := hasher.Sum(entries[index].digest[:0])
		copy(entries[index].digest[:], blobDigest)
	}
	return nil
}

// parseTreeManifest validates a bounded NUL-delimited Git tree listing into
// canonical manifest entries without reading object content.
func parseTreeManifest(output []byte) ([]treeManifestEntry, error) {
	records := bytes.Split(output, []byte{0})
	entries := make([]treeManifestEntry, 0, len(records))
	for index, record := range records {
		if len(record) == 0 {
			if index == len(records)-1 {
				continue
			}
			return nil, fmt.Errorf("committed tree contains an empty entry at index %d", index)
		}
		tab := bytes.IndexByte(record, '\t')
		if tab <= 0 || tab == len(record)-1 {
			return nil, fmt.Errorf("committed tree entry %d has invalid NUL record", index)
		}
		fields := bytes.Split(record[:tab], []byte{' '})
		if len(fields) != 3 || len(fields[0]) == 0 || len(fields[1]) == 0 || len(fields[2]) == 0 {
			return nil, fmt.Errorf("committed tree entry %d has invalid header", index)
		}
		mode := string(fields[0])
		typeName := string(fields[1])
		objectID := string(fields[2])
		path := append([]byte(nil), record[tab+1:]...)
		if bytes.IndexByte(path, 0) >= 0 || len(path) == 0 {
			return nil, fmt.Errorf("committed tree entry %d has invalid path", index)
		}
		if typeName != "blob" || (mode != "100644" && mode != "100755" && mode != "120000") {
			return nil, &SubjectError{
				Code:      SubjectErrorUnsupportedTree,
				EntryPath: string(path),
				EntryMode: mode,
				EntryType: typeName,
				Cause:     ErrUnsupportedSubjectTree,
			}
		}
		entries = append(entries, treeManifestEntry{mode: mode, typeName: typeName, path: path, objectID: objectID})
	}
	return entries, nil
}

// openSubjectGitBatch starts the persistent object batch stream for the
// configured workspace repository. The workspace command policy is unchanged:
// replace objects stay ignored and the child inherits the daemon environment.
func openSubjectGitBatch(ctx context.Context, repository string) (*subjectGitBatchStream, error) {
	command := gitNoReplaceObjectsCommand(ctx, repository, "cat-file", "--batch")
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		return nil, fmt.Errorf("configure headless Git process: %w", err)
	}
	return startSubjectGitBatch(ctx, command, nil, nil)
}

// startSubjectGitBatch starts one batch child and returns its request stream.
// budget releases a path-specific command budget; sentinel is the path's
// unresolved-process error joined when the child has to be terminated.
func startSubjectGitBatch(ctx context.Context, command *exec.Cmd, budget context.CancelFunc, sentinel error) (*subjectGitBatchStream, error) {
	if ctx == nil {
		return nil, errors.New("subject Git batch context is nil")
	}
	if command == nil {
		return nil, errors.New("subject Git batch command is nil")
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		if budget != nil {
			budget()
		}
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		if budget != nil {
			budget()
		}
		_ = stdin.Close()
		// Start never runs on this path, so Start's own descriptor cleanup
		// cannot release the child's read end that StdinPipe installed on the
		// command; without this close the descriptor survives until the
		// os.File finalizer runs.
		if closer, ok := command.Stdin.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, err
	}
	if err := command.Start(); err != nil {
		// Snapshot the context before releasing the path's budget: the budget's
		// cancel function cancels this very context on the Linux clone path, so
		// reading it afterwards would report a live launch failure as a caller
		// cancellation and drop the real error.
		ctxErr := ctx.Err()
		if budget != nil {
			budget()
		}
		_ = stdin.Close()
		if ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	return &subjectGitBatchStream{
		ctx: ctx, command: command, budget: budget, sentinel: sentinel,
		stdin: stdin, stdout: bufio.NewReaderSize(stdout, 32<<10),
		request: make([]byte, 0, 64), buffer: make([]byte, 64<<10),
	}, nil
}

// streamObject requests one committed object from the batch child and streams
// its content into content. It refuses missing objects, non-blob responses and
// content beyond maxBytes without retaining the object.
func (stream *subjectGitBatchStream) streamObject(objectID string, content io.Writer, maxBytes int64) error {
	if err := stream.ctx.Err(); err != nil {
		return err
	}
	stream.request = append(stream.request[:0], objectID...)
	stream.request = append(stream.request, '\n')
	if _, err := stream.stdin.Write(stream.request); err != nil {
		return stream.desync(stream.streamError(fmt.Errorf("request object %s from Git batch stream: %w", objectID, err)))
	}
	header, err := stream.stdout.ReadSlice('\n')
	if err != nil {
		return stream.desync(stream.streamError(fmt.Errorf("read Git batch response header for object %s: %w", objectID, err)))
	}
	fields := bytes.Split(bytes.TrimSuffix(header, []byte{'\n'}), []byte{' '})
	if len(fields) == 2 && string(fields[0]) == objectID && string(fields[1]) == "missing" {
		return fmt.Errorf("committed object %s is missing from the repository", objectID)
	}
	if len(fields) != 3 || string(fields[0]) != objectID {
		return stream.desync(fmt.Errorf("Git batch stream returned an invalid response header for object %s", objectID))
	}
	if string(fields[1]) != "blob" {
		return stream.desync(fmt.Errorf("Git batch stream returned type %q for object %s", string(fields[1]), objectID))
	}
	size, err := strconv.ParseInt(string(fields[2]), 10, 64)
	if err != nil || size < 0 {
		return stream.desync(fmt.Errorf("Git batch stream returned an invalid size for object %s", objectID))
	}
	if size > maxBytes {
		return stream.desync(fmt.Errorf("%w: object %s is %d bytes", ErrSubjectArtifactTooLarge, objectID, size))
	}
	if err := stream.copyExact(content, size); err != nil {
		return stream.desync(stream.streamError(fmt.Errorf("read content of object %s from Git batch stream: %w", objectID, err)))
	}
	trailer, err := stream.stdout.ReadByte()
	if err != nil {
		return stream.desync(stream.streamError(fmt.Errorf("read content terminator of object %s from Git batch stream: %w", objectID, err)))
	}
	if trailer != '\n' {
		return stream.desync(fmt.Errorf("Git batch stream did not terminate object %s content", objectID))
	}
	return nil
}

// copyExact copies exactly size bytes of batch output into content through the
// stream's reusable buffer.
func (stream *subjectGitBatchStream) copyExact(content io.Writer, size int64) error {
	for remaining := size; remaining > 0; {
		chunk := int64(len(stream.buffer))
		if remaining < chunk {
			chunk = remaining
		}
		if _, err := io.ReadFull(stream.stdout, stream.buffer[:chunk]); err != nil {
			return err
		}
		if _, err := content.Write(stream.buffer[:chunk]); err != nil {
			return err
		}
		remaining -= chunk
	}
	return nil
}

// streamError reports the batch context failure in preference to a pipe error
// caused by the same cancellation or command budget.
func (stream *subjectGitBatchStream) streamError(err error) error {
	if ctxErr := stream.ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

// desync marks the stream desynced before returning err. Every refusal that
// leaves the last batch response unconsumed to a record boundary must go
// through here: close() derives its terminate-versus-ask-to-exit choice from
// that flag.
func (stream *subjectGitBatchStream) desync(err error) error {
	stream.desynced = true
	return err
}

// close completes the batch stream: it waits for the child and joins the
// caller's digest failure with the path's teardown result. A desynced child is
// terminated through the path's cancellation policy instead of being asked to
// exit, so no owned batch stream survives a failed or cancelled digest.
func (stream *subjectGitBatchStream) close(failure error) error {
	if stream == nil {
		return failure
	}
	if stream.budget != nil {
		defer stream.budget()
	}
	if failure == nil || !stream.desynced {
		return errors.Join(failure, stream.finish())
	}
	terminateErr := stream.terminate()
	waitErr := stream.command.Wait()
	return errors.Join(stream.sentinel, failure, terminateErr, waitErr)
}

// finish asks an in-sync batch child to exit and proves that it was reaped. A
// cancellation that reaches this path still joins the path's unresolved-process
// fence: this path cannot observe whether the path's cancellation policy
// actually stopped the child or the child exited on its own first, so the stop
// is presumed unproven and stays observable to the caller.
func (stream *subjectGitBatchStream) finish() error {
	closeErr := stream.stdin.Close()
	waitErr := stream.command.Wait()
	ctxErr := stream.ctx.Err()
	if ctxErr != nil {
		return errors.Join(stream.sentinel, ctxErr, closeErr, waitErr)
	}
	if waitErr == nil && closeErr == nil {
		return nil
	}
	return fmt.Errorf("finish Git batch stream: %w", errors.Join(closeErr, waitErr))
}

// terminate stops the batch child through the path's cancellation policy. A
// cancellation failure falls back to killing the retained child process, so a
// live batch stream is still stopped and reaped promptly by the single Wait in
// close. The failed cancellation is returned either way: an unresolved path
// stop is never reported as a successful one.
func (stream *subjectGitBatchStream) terminate() error {
	var cancelErr error
	if stream.command.Cancel != nil {
		cancelErr = stream.command.Cancel()
		if cancelErr == nil || errors.Is(cancelErr, os.ErrProcessDone) {
			return nil
		}
	}
	if stream.command.Process == nil {
		return cancelErr
	}
	if err := stream.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return errors.Join(cancelErr, err)
	}
	return cancelErr
}

func gitBlobContent(ctx context.Context, repository, objectID string) ([]byte, error) {
	command := gitNoReplaceObjectsCommand(ctx, repository, "cat-file", "blob", objectID)
	output, err := gitCommandOutputLimited(ctx, command, maxSubjectArtifactBlobBytes, ErrSubjectArtifactTooLarge)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	return output, nil
}

// gitCommandOutputLimited runs a Git command while bounding the amount of
// stdout retained by the daemon. The command is killed as soon as the bound
// is crossed, and Wait is always called after Start so no child or pipe is
// leaked. CommandContext remains the authority for cancellation.
func gitCommandOutputLimited(ctx context.Context, command *exec.Cmd, maxBytes int, limitErr error) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("git command context is nil")
	}
	if command == nil {
		return nil, errors.New("git command is nil")
	}
	if maxBytes <= 0 {
		return nil, errors.New("git command output limit must be positive")
	}
	if limitErr == nil {
		return nil, errors.New("git command output limit error is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		return nil, fmt.Errorf("configure headless Git process: %w", err)
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}

	output, readErr := io.ReadAll(io.LimitReader(stdout, int64(maxBytes)+1))
	overLimit := readErr == nil && len(output) > maxBytes
	if overLimit || readErr != nil {
		// A reader error or an output limit can otherwise leave a child blocked
		// on a full pipe. Process.Kill is best-effort; Wait below is mandatory.
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if overLimit {
		return nil, fmt.Errorf("%w: output exceeded %d bytes", limitErr, maxBytes)
	}
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, waitErr
	}
	return output, nil
}

func subjectError(bindingKey, path, commit string, code SubjectErrorCode, err error) error {
	if typed, ok := err.(*SubjectError); ok {
		if typed.BindingKey == "" {
			typed.BindingKey = bindingKey
		}
		if typed.Path == "" {
			typed.Path = path
		}
		if typed.Commit == "" {
			typed.Commit = commit
		}
		return typed
	}
	return &SubjectError{Code: code, BindingKey: bindingKey, Path: path, Commit: commit, Cause: err}
}

func subjectErrorCode(err error) SubjectErrorCode {
	var typed *SubjectError
	if errors.As(err, &typed) && typed != nil {
		return typed.Code
	}
	return SubjectErrorUnreachable
}

func subjectMismatch(bindingKey, commit, expected, actual string) error {
	return &SubjectError{Code: SubjectErrorMismatch, BindingKey: bindingKey, Commit: commit, Expected: expected, Actual: actual, Cause: ErrSubjectMismatch}
}

func subjectDigestDescription(subject protocol.Subject) string {
	return subject.Commit + ":" + subject.TreeDigest
}
