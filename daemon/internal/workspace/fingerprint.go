package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const workspaceFingerprintSchemaVersion = 1

// Fingerprint returns the stable, machine-local identity of a daemon-owned
// linked worktree. The digest binds the canonical workspace path, the Git
// repository identity, and both Git and Symmetry ownership identities. A
// commit is intentionally not part of the identity because it is a moving
// artifact inside the workspace.
//
// Fingerprint fails closed for an unregistered, non-Git, or non-daemon-owned
// directory. It returns only a digest; the paths and ownership metadata used
// to derive it are never exposed to callers.
func Fingerprint(ctx context.Context, prepared Prepared) (string, error) {
	if ctx == nil {
		return "", errors.New("workspace fingerprint context is nil")
	}
	if err := validateRun(prepared.Run); err != nil {
		return "", fmt.Errorf("validate workspace run: %w", err)
	}
	if !validFingerprintBindingKey(prepared.BindingKey) {
		return "", errors.New("workspace binding key is invalid")
	}

	path, err := canonicalWorkspaceDirectory(prepared.Path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	topLevel, err := gitDirectoryIdentity(ctx, path, "--show-toplevel", path)
	if err != nil {
		return "", fmt.Errorf("identify Git worktree root: %w", err)
	}
	if same, err := sameDirectory(topLevel, path); err != nil {
		return "", fmt.Errorf("compare Git worktree root: %w", err)
	} else if !same {
		return "", fmt.Errorf("workspace path %q is not a Git worktree root", path)
	}

	repository, err := gitDirectoryIdentity(ctx, path, "--git-common-dir", path)
	if err != nil {
		return "", fmt.Errorf("identify Git repository: %w", err)
	}
	worktree, err := gitDirectoryIdentity(ctx, path, "--git-dir", path)
	if err != nil {
		return "", fmt.Errorf("identify Git worktree: %w", err)
	}
	if same, err := sameDirectory(repository, worktree); err != nil {
		return "", fmt.Errorf("compare Git identities: %w", err)
	} else if same {
		return "", errors.New("workspace is not a linked Git worktree")
	}

	registered, err := worktreeContains(ctx, topLevel, path)
	if err != nil {
		return "", fmt.Errorf("verify Git worktree registration: %w", err)
	}
	if !registered {
		return "", fmt.Errorf("workspace path %q is not a registered Git worktree", path)
	}

	owned, err := readFingerprintOwnership(ctx, path, prepared, repository)
	if err != nil {
		return "", fmt.Errorf("verify workspace ownership: %w", err)
	}

	identity := struct {
		Version     int    `json:"version"`
		Path        string `json:"path"`
		Repository  string `json:"repository"`
		GitWorktree string `json:"git_worktree"`
		BindingKey  string `json:"binding_key"`
		RunID       string `json:"run_id"`
		Generation  int64  `json:"generation"`
	}{
		Version:     workspaceFingerprintSchemaVersion,
		Path:        path,
		Repository:  repository,
		GitWorktree: worktree,
		BindingKey:  owned.BindingKey,
		RunID:       owned.RunID,
		Generation:  owned.Generation,
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("encode workspace identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validFingerprintBindingKey(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func canonicalWorkspaceDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path must not be empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func gitDirectoryIdentity(ctx context.Context, workspace, argument, base string) (string, error) {
	value, err := runGitRevParse(ctx, workspace, argument)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	return canonicalWorkspaceDirectory(value)
}

func runGitRevParse(ctx context.Context, workspace, argument string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, "git", "-C", workspace, "rev-parse", argument)
	output, err := command.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("git rev-parse %s: %w", argument, err)
	}
	value := strings.TrimSpace(string(output))
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("git rev-parse %s returned invalid output", argument)
	}
	return value, nil
}

func readFingerprintOwnership(ctx context.Context, path string, prepared Prepared, repository string) (ownership, error) {
	journal := filepath.Join(path, journalName)
	info, err := os.Lstat(journal)
	if errors.Is(err, os.ErrNotExist) {
		return ownership{}, errors.New("ownership journal is missing")
	}
	if err != nil {
		return ownership{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ownership{}, errors.New("ownership journal must not be a symlink")
	}
	contents, err := os.ReadFile(journal)
	if err != nil {
		return ownership{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	var value ownership
	if err := decoder.Decode(&value); err != nil {
		return ownership{}, fmt.Errorf("decode ownership journal: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return ownership{}, errors.New("ownership journal must contain one JSON object")
		}
		return ownership{}, fmt.Errorf("decode ownership journal: %w", err)
	}
	if value.Version != 1 || value.BindingKey != prepared.BindingKey || value.RunID != prepared.Run.RunID || value.Generation != prepared.Run.Generation {
		return ownership{}, errors.New("ownership journal does not belong to this run")
	}
	ownedTarget, err := canonicalWorkspaceDirectory(value.Target)
	if err != nil {
		return ownership{}, fmt.Errorf("resolve ownership target: %w", err)
	}
	if same, err := sameDirectory(ownedTarget, path); err != nil {
		return ownership{}, fmt.Errorf("compare ownership target: %w", err)
	} else if !same {
		return ownership{}, errors.New("ownership journal target does not match workspace")
	}
	ownedRepository, err := canonicalWorkspaceDirectory(value.Repository)
	if err != nil {
		return ownership{}, fmt.Errorf("resolve ownership repository: %w", err)
	}
	ownedCommonDirectory, err := gitDirectoryIdentity(ctx, ownedRepository, "--git-common-dir", ownedRepository)
	if err != nil {
		return ownership{}, fmt.Errorf("identify ownership repository: %w", err)
	}
	if same, err := sameDirectory(ownedCommonDirectory, repository); err != nil {
		return ownership{}, fmt.Errorf("compare ownership repository: %w", err)
	} else if !same {
		return ownership{}, errors.New("ownership journal repository does not match workspace")
	}
	return value, nil
}
