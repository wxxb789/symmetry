package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/config"
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
	subject := protocol.Subject{ResourceID: resourceID, Commit: commit, TreeDigest: treeDigest}
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
	materialized, err := manager.materializeAtRepository(ctx, bindingKey, repository, admitted.ResourceID, admitted.Commit)
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
	materialized, err := manager.materializeAtRepository(ctx, bindingKey, repository, admitted.ResourceID, admitted.Commit)
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
	actual, err := manager.materializeAtRepository(ctx, prepared.BindingKey, repository, expected.ResourceID, expected.Commit)
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
	return manager.materializeAtRepository(ctx, prepared.BindingKey, repository, resourceID, commit)
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

func (manager *Manager) materializeAtRepository(ctx context.Context, bindingKey, repository, resourceID, commit string) (protocol.Subject, error) {
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
	digest   [sha256.Size]byte
}

func gitTreeDigest(ctx context.Context, repository, commit string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	command := gitNoReplaceObjectsCommand(ctx, repository, "ls-tree", "--full-tree", "-r", "-z", commit)
	output, err := gitCommandOutputLimited(ctx, command, maxSubjectTreeOutputBytes, ErrSubjectTreeTooLarge)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("list committed tree for %q: %w", commit, err)
	}
	entries, err := parseTreeManifest(ctx, repository, output)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(left, right int) bool {
		return bytes.Compare(entries[left].path, entries[right].path) < 0
	})
	// The canonical manifest is a sequence of NUL-delimited records sorted by
	// raw Git path bytes: mode NUL type NUL path NUL sha256:<blob> NUL. It is
	// intentionally derived from committed Git objects, never checkout bytes.
	manifest := make([]byte, 0, len(entries)*128)
	for _, entry := range entries {
		manifest = append(manifest, entry.mode...)
		manifest = append(manifest, 0)
		manifest = append(manifest, entry.typeName...)
		manifest = append(manifest, 0)
		manifest = append(manifest, entry.path...)
		manifest = append(manifest, 0)
		manifest = append(manifest, "sha256:"...)
		manifest = append(manifest, hex.EncodeToString(entry.digest[:])...)
		manifest = append(manifest, 0)
	}
	digest := sha256.Sum256(manifest)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func parseTreeManifest(ctx context.Context, repository string, output []byte) ([]treeManifestEntry, error) {
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
		content, err := gitBlobContent(ctx, repository, objectID)
		if err != nil {
			return nil, fmt.Errorf("read committed blob %q: %w", string(path), err)
		}
		entries = append(entries, treeManifestEntry{mode: mode, typeName: typeName, path: path, digest: sha256.Sum256(content)})
	}
	return entries, nil
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
