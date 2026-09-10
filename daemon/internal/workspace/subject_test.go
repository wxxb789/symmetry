package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

const subjectResourceID = "00000000-0000-4000-8000-000000000001"

func TestPrepareSubjectUsesAdmittedCommitAfterRefDrift(t *testing.T) {
	repository := newRepository(t)
	runGit(t, repository, "branch", "-M", "admitted")
	commitA := gitOutput(t, repository, "rev-parse", "HEAD")
	root := filepath.Join(t.TempDir(), "worktrees")
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "admitted", Cleanup: config.CleanupAlways,
		},
	})

	subject, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commitA)
	if err != nil {
		t.Fatalf("MaterializeSubject() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("new ref content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "README.md")
	runGit(t, repository, "commit", "-m", "move admitted ref")
	commitB := gitOutput(t, repository, "rev-parse", "HEAD")
	if commitA == commitB {
		t.Fatal("test commits unexpectedly match")
	}

	bound, err := manager.PrepareSubject(context.Background(), "primary", RunRef{RunID: "subject-ref-drift", Generation: 1}, subject)
	if err != nil {
		t.Fatalf("PrepareSubject() error = %v", err)
	}
	defer func() {
		if err := manager.Cleanup(context.Background(), bound.Prepared, true); err != nil {
			t.Errorf("Cleanup() error = %v", err)
		}
	}()
	if bound.Subject != subject {
		t.Fatalf("bound Subject = %#v, want %#v", bound.Subject, subject)
	}
	if got := gitOutput(t, bound.Prepared.Path, "rev-parse", "HEAD"); got != commitA {
		t.Fatalf("prepared HEAD = %q, want admitted commit %q (ref drifted to %q)", got, commitA, commitB)
	}
	command := exec.Command("git", "-C", bound.Prepared.Path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("prepared worktree is attached to branch %q", strings.TrimSpace(string(output)))
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("inspect detached worktree HEAD: %v\n%s", err, output)
		}
	}
}

func TestPrepareSubjectRejectsMismatchedDigestBeforeCreatingWorkspace(t *testing.T) {
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	admitted, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commit)
	if err != nil {
		t.Fatalf("MaterializeSubject() error = %v", err)
	}
	admitted.TreeDigest = "sha256:" + strings.Repeat("f", 64)

	_, err = manager.PrepareSubject(context.Background(), "primary", RunRef{RunID: "subject-bad-digest", Generation: 1}, admitted)
	if err == nil || !errors.Is(err, ErrSubjectMismatch) {
		t.Fatalf("PrepareSubject() error = %v, want ErrSubjectMismatch", err)
	}
	var subjectErr *SubjectError
	if !errors.As(err, &subjectErr) || subjectErr.Code != SubjectErrorMismatch {
		t.Fatalf("PrepareSubject() error = %v, want typed mismatch", err)
	}
	if _, statErr := os.Stat(root); !os.IsNotExist(statErr) {
		t.Fatalf("mismatched subject created workspace root: %v", statErr)
	}
}

func TestVerifySubjectIgnoresDirtyWorktreeForCommittedSubject(t *testing.T) {
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("dirty source checkout\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "source-untracked.txt"), []byte("not committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	subject, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commit)
	if err != nil {
		t.Fatalf("MaterializeSubject() error = %v", err)
	}
	bound, err := manager.PrepareSubject(context.Background(), "primary", RunRef{RunID: "subject-dirty", Generation: 1}, subject)
	if err != nil {
		t.Fatalf("PrepareSubject() error = %v", err)
	}
	defer func() {
		if err := manager.Cleanup(context.Background(), bound.Prepared, true); err != nil {
			t.Errorf("Cleanup() error = %v", err)
		}
	}()
	if err := os.WriteFile(filepath.Join(bound.Prepared.Path, "README.md"), []byte("dirty working tree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bound.Prepared.Path, "untracked.txt"), []byte("not in commit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	verified, err := manager.VerifySubject(context.Background(), bound.Prepared, subject)
	if err != nil {
		t.Fatalf("VerifySubject() error = %v", err)
	}
	if verified != subject {
		t.Fatalf("verified Subject = %#v, want %#v", verified, subject)
	}
}

func TestDeriveSubjectReadsActualDetachedHeadAndCommittedTree(t *testing.T) {
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	expected, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commit)
	if err != nil {
		t.Fatalf("MaterializeSubject() error = %v", err)
	}
	bound, err := manager.PrepareSubject(context.Background(), "primary", RunRef{RunID: "subject-derive", Generation: 1}, expected)
	if err != nil {
		t.Fatalf("PrepareSubject() error = %v", err)
	}
	defer func() {
		if err := manager.Cleanup(context.Background(), bound.Prepared, true); err != nil {
			t.Errorf("Cleanup() error = %v", err)
		}
	}()
	if err := os.WriteFile(filepath.Join(bound.Prepared.Path, "README.md"), []byte("dirty but not committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	derived, err := manager.DeriveSubject(context.Background(), bound.Prepared, subjectResourceID)
	if err != nil {
		t.Fatalf("DeriveSubject() error = %v", err)
	}
	if derived != expected {
		t.Fatalf("derived Subject = %#v, want %#v", derived, expected)
	}
}

func TestDeriveSubjectRejectsAttachedHead(t *testing.T) {
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	expected, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commit)
	if err != nil {
		t.Fatalf("MaterializeSubject() error = %v", err)
	}
	bound, err := manager.PrepareSubject(context.Background(), "primary", RunRef{RunID: "subject-attached", Generation: 1}, expected)
	if err != nil {
		t.Fatalf("PrepareSubject() error = %v", err)
	}
	defer func() {
		if err := manager.Cleanup(context.Background(), bound.Prepared, true); err != nil {
			t.Errorf("Cleanup() error = %v", err)
		}
	}()
	runGit(t, bound.Prepared.Path, "checkout", "-b", "attached-subject-test")
	_, err = manager.DeriveSubject(context.Background(), bound.Prepared, subjectResourceID)
	if err == nil || !errors.Is(err, ErrSubjectMismatch) {
		t.Fatalf("DeriveSubject() error = %v, want ErrSubjectMismatch", err)
	}
	var subjectErr *SubjectError
	if !errors.As(err, &subjectErr) || subjectErr.Code != SubjectErrorMismatch {
		t.Fatalf("DeriveSubject() error = %v, want typed mismatch", err)
	}
}

func TestRecoverSubjectRevalidatesCommitAfterJournalLoss(t *testing.T) {
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	bindings := map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	}
	manager := New(bindings)
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	subject, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commit)
	if err != nil {
		t.Fatalf("MaterializeSubject() error = %v", err)
	}
	run := RunRef{RunID: "subject-recover", Generation: 1}
	prepared, err := manager.PrepareSubject(context.Background(), "primary", run, subject)
	if err != nil {
		t.Fatalf("PrepareSubject() error = %v", err)
	}
	if err := os.Remove(prepared.Prepared.journal); err != nil {
		t.Fatalf("Remove(journal) error = %v", err)
	}
	recovered, err := New(bindings).RecoverSubject(context.Background(), "primary", run, prepared.Prepared.Path, subject)
	if err != nil {
		t.Fatalf("RecoverSubject() error = %v", err)
	}
	if recovered.Subject != subject {
		t.Fatalf("recovered Subject = %#v, want %#v", recovered.Subject, subject)
	}
	if err := New(bindings).Cleanup(context.Background(), recovered.Prepared, true); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestMaterializeSubjectRejectsUnreachableCommit(t *testing.T) {
	repository := newRepository(t)
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	_, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, strings.Repeat("0", 40))
	if err == nil || !errors.Is(err, ErrSubjectUnreachable) {
		t.Fatalf("MaterializeSubject() error = %v, want ErrSubjectUnreachable", err)
	}
	var subjectErr *SubjectError
	if !errors.As(err, &subjectErr) || subjectErr.Code != SubjectErrorUnreachable {
		t.Fatalf("MaterializeSubject() error = %v, want typed unreachable", err)
	}
}

func TestMaterializeSubjectRejectsUnsupportedSubmoduleTreeEntry(t *testing.T) {
	repository := newRepository(t)
	child := newRepository(t)
	childCommit := gitOutput(t, child, "rev-parse", "HEAD")
	runGit(t, repository, "update-index", "--add", "--cacheinfo", "160000,"+childCommit+",vendor/child")
	runGit(t, repository, "commit", "-m", "add unsupported submodule entry")
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})

	_, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commit)
	if err == nil || !errors.Is(err, ErrUnsupportedSubjectTree) {
		t.Fatalf("MaterializeSubject() error = %v, want ErrUnsupportedSubjectTree", err)
	}
	var subjectErr *SubjectError
	if !errors.As(err, &subjectErr) || subjectErr.Code != SubjectErrorUnsupportedTree {
		t.Fatalf("MaterializeSubject() error = %v, want typed unsupported tree", err)
	}
	if subjectErr.EntryPath != "vendor/child" || subjectErr.EntryMode != "160000" || subjectErr.EntryType != "commit" {
		t.Fatalf("unsupported entry details = %#v, want vendor/child 160000 commit", subjectErr)
	}
}

func TestMaterializeSubjectTreeDigestIsIndependentOfGitInsertionOrder(t *testing.T) {
	firstRepository := newRepository(t)
	secondRepository := newRepository(t)
	for _, repository := range []string{firstRepository, secondRepository} {
		if err := os.WriteFile(filepath.Join(repository, "a.txt"), []byte("a\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repository, "z.txt"), []byte("z\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, firstRepository, "add", "z.txt", "a.txt")
	runGit(t, firstRepository, "commit", "-m", "add files in reverse order")
	runGit(t, secondRepository, "add", "a.txt", "z.txt")
	runGit(t, secondRepository, "commit", "-m", "add files in forward order")

	firstManager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: firstRepository, Root: filepath.Join(t.TempDir(), "first-worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	secondManager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: secondRepository, Root: filepath.Join(t.TempDir(), "second-worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	first, err := firstManager.MaterializeSubject(context.Background(), "primary", subjectResourceID, gitOutput(t, firstRepository, "rev-parse", "HEAD"))
	if err != nil {
		t.Fatalf("first MaterializeSubject() error = %v", err)
	}
	second, err := secondManager.MaterializeSubject(context.Background(), "primary", subjectResourceID, gitOutput(t, secondRepository, "rev-parse", "HEAD"))
	if err != nil {
		t.Fatalf("second MaterializeSubject() error = %v", err)
	}
	if first.TreeDigest != second.TreeDigest {
		t.Fatalf("tree digests differ for identical committed trees: %q != %q", first.TreeDigest, second.TreeDigest)
	}
}

func gitOutput(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	return strings.TrimSpace(runGitOutput(t, directory, arguments...))
}

func runGitOutput(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}
