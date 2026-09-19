package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

func TestFingerprintCanonicalizesWorkspacePathAliases(t *testing.T) {
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	prepared, err := New(map[string]config.Workspace{
		"primary": {Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways},
	}).Prepare(context.Background(), "primary", RunRef{RunID: "fingerprint-alias", Generation: 1})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer func() {
		if err := New(map[string]config.Workspace{
			"primary": {Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways},
		}).Cleanup(context.Background(), prepared, true); err != nil {
			t.Fatalf("Cleanup() error = %v", err)
		}
	}()

	canonical, err := Fingerprint(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Fingerprint(canonical) error = %v", err)
	}
	aliased := prepared
	aliased.Path = filepath.Join(prepared.Path, ".", "..", filepath.Base(prepared.Path))
	aliasedFingerprint, err := Fingerprint(context.Background(), aliased)
	if err != nil {
		t.Fatalf("Fingerprint(alias) error = %v", err)
	}
	if aliasedFingerprint != canonical {
		t.Fatalf("Fingerprint(alias) = %q, want %q", aliasedFingerprint, canonical)
	}

	linkParent := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Dir(prepared.Path), linkParent); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	aliased.Path = filepath.Join(linkParent, filepath.Base(prepared.Path))
	aliasedFingerprint, err = Fingerprint(context.Background(), aliased)
	if err != nil {
		t.Fatalf("Fingerprint(symlink alias) error = %v", err)
	}
	if aliasedFingerprint != canonical {
		t.Fatalf("Fingerprint(symlink alias) = %q, want %q", aliasedFingerprint, canonical)
	}
}

func TestFingerprintBindsRepositoryAndWorktree(t *testing.T) {
	firstRepository := newRepository(t)
	secondRepository := newRepository(t)
	firstRoot := filepath.Join(t.TempDir(), "first-worktrees")
	sameRepositoryRoot := filepath.Join(t.TempDir(), "same-repository-worktrees")
	secondRoot := filepath.Join(t.TempDir(), "second-worktrees")
	firstManager := New(map[string]config.Workspace{
		"primary": {Policy: config.WorkspacePolicyGitWorktree, Repository: firstRepository, Root: firstRoot, Ref: "HEAD", Cleanup: config.CleanupAlways},
	})
	sameRepositoryManager := New(map[string]config.Workspace{
		"primary": {Policy: config.WorkspacePolicyGitWorktree, Repository: firstRepository, Root: sameRepositoryRoot, Ref: "HEAD", Cleanup: config.CleanupAlways},
	})
	secondManager := New(map[string]config.Workspace{
		"primary": {Policy: config.WorkspacePolicyGitWorktree, Repository: secondRepository, Root: secondRoot, Ref: "HEAD", Cleanup: config.CleanupAlways},
	})
	first, err := firstManager.Prepare(context.Background(), "primary", RunRef{RunID: "fingerprint-binding", Generation: 1})
	if err != nil {
		t.Fatalf("first Prepare() error = %v", err)
	}
	sameRepository, err := sameRepositoryManager.Prepare(context.Background(), "primary", RunRef{RunID: "fingerprint-binding", Generation: 1})
	if err != nil {
		t.Fatalf("same-repository Prepare() error = %v", err)
	}
	second, err := secondManager.Prepare(context.Background(), "primary", RunRef{RunID: "fingerprint-binding", Generation: 1})
	if err != nil {
		t.Fatalf("second Prepare() error = %v", err)
	}
	defer func() {
		if err := firstManager.Cleanup(context.Background(), first, true); err != nil {
			t.Errorf("first Cleanup() error = %v", err)
		}
		if err := sameRepositoryManager.Cleanup(context.Background(), sameRepository, true); err != nil {
			t.Errorf("same-repository Cleanup() error = %v", err)
		}
		if err := secondManager.Cleanup(context.Background(), second, true); err != nil {
			t.Errorf("second Cleanup() error = %v", err)
		}
	}()

	firstFingerprint, err := Fingerprint(context.Background(), first)
	if err != nil {
		t.Fatalf("Fingerprint(first) error = %v", err)
	}
	sameRepositoryFingerprint, err := Fingerprint(context.Background(), sameRepository)
	if err != nil {
		t.Fatalf("Fingerprint(same repository) error = %v", err)
	}
	secondFingerprint, err := Fingerprint(context.Background(), second)
	if err != nil {
		t.Fatalf("Fingerprint(second) error = %v", err)
	}
	if firstFingerprint == sameRepositoryFingerprint || firstFingerprint == secondFingerprint || sameRepositoryFingerprint == secondFingerprint {
		t.Fatalf("Fingerprint() values must differ for distinct repository/worktree bindings: %q, %q, %q", firstFingerprint, sameRepositoryFingerprint, secondFingerprint)
	}
	if !strings.HasPrefix(firstFingerprint, "sha256:") || !strings.HasPrefix(sameRepositoryFingerprint, "sha256:") || !strings.HasPrefix(secondFingerprint, "sha256:") {
		t.Fatalf("Fingerprint() values must be opaque sha256 digests: %q, %q, %q", firstFingerprint, sameRepositoryFingerprint, secondFingerprint)
	}
}

func TestFingerprintRejectsNonGitDirectory(t *testing.T) {
	prepared := Prepared{Path: t.TempDir(), BindingKey: "primary", Run: RunRef{RunID: "fingerprint-non-git", Generation: 1}}
	_, err := Fingerprint(context.Background(), prepared)
	if err == nil {
		t.Fatal("Fingerprint(non-Git directory) error = nil")
	}
}

func TestFingerprintPropagatesContextCancellation(t *testing.T) {
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	prepared, err := New(map[string]config.Workspace{
		"primary": {Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways},
	}).Prepare(context.Background(), "primary", RunRef{RunID: "fingerprint-cancel", Generation: 1})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer func() {
		if err := New(map[string]config.Workspace{
			"primary": {Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways},
		}).Cleanup(context.Background(), prepared, true); err != nil {
			t.Fatalf("Cleanup() error = %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Fingerprint(ctx, prepared)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fingerprint(canceled) error = %v, want context.Canceled", err)
	}
}
