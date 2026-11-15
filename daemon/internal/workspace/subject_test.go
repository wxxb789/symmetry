package workspace

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

const subjectResourceID = "00000000-0000-4000-8000-000000000001"

func TestPrepareSubjectUsesAdmittedCommitAfterRefDrift(t *testing.T) {
	repository := newSubjectRepository(t)
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
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() error = %v", err)
	}
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("prepared worktree is attached to branch %q", strings.TrimSpace(string(output)))
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("inspect detached worktree HEAD: %v\n%s", err, output)
		}
	}
}

func TestMaterializeSubjectIgnoresGitReplaceRefs(t *testing.T) {
	repository := newSubjectRepository(t)
	originalCommit := gitOutput(t, repository, "rev-parse", "HEAD")
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	expected, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, originalCommit)
	if err != nil {
		t.Fatalf("MaterializeSubject() before replace refs error = %v", err)
	}

	installSubjectReplaceRefs(t, repository, originalCommit, "README.md", "replacement tree and blob\n")
	actual, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, originalCommit)
	if err != nil {
		t.Fatalf("MaterializeSubject() with replace refs error = %v", err)
	}
	if actual != expected {
		t.Fatalf("MaterializeSubject() = %#v, want original Subject %#v", actual, expected)
	}
}

func TestPrepareSubjectIgnoresGitReplaceRefs(t *testing.T) {
	repository := newSubjectRepository(t)
	originalCommit := gitOutput(t, repository, "rev-parse", "HEAD")
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	subject, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, originalCommit)
	if err != nil {
		t.Fatalf("MaterializeSubject() error = %v", err)
	}
	installSubjectReplaceRefs(t, repository, originalCommit, "README.md", "replacement checkout bytes\n")

	prepared, err := manager.PrepareSubject(context.Background(), "primary", RunRef{RunID: "subject-replace-refs", Generation: 1}, subject)
	if err != nil {
		t.Fatalf("PrepareSubject() with replace refs error = %v", err)
	}
	defer func() {
		if err := manager.Cleanup(context.Background(), prepared.Prepared, true); err != nil {
			t.Errorf("Cleanup() error = %v", err)
		}
	}()
	content, err := os.ReadFile(filepath.Join(prepared.Prepared.Path, "README.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	if normalized != "test\n" {
		t.Fatalf("prepared README.md = %q (normalized %q), want original semantic bytes %q", content, normalized, "test\n")
	}
}

func TestPrepareSubjectRejectsMismatchedDigestBeforeCreatingWorkspace(t *testing.T) {
	repository := newSubjectRepository(t)
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
	repository := newSubjectRepository(t)
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
	repository := newSubjectRepository(t)
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
	repository := newSubjectRepository(t)
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
	repository := newSubjectRepository(t)
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
	repository := newSubjectRepository(t)
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
	repository := newSubjectRepository(t)
	child := newSubjectRepository(t)
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
	firstRepository := newSubjectRepository(t)
	secondRepository := newSubjectRepository(t)
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

func newSubjectRepository(t *testing.T) string {
	t.Helper()
	repository := newRepository(t)
	runGit(t, repository, "config", "core.autocrlf", "false")
	runGit(t, repository, "config", "core.eol", "lf")
	return repository
}

func gitOutputNoReplace(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"--no-replace-objects", "-C", directory}, arguments...)...)
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() error = %v", err)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git --no-replace-objects %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func installSubjectReplaceRefs(t *testing.T, repository, originalCommit, path, replacementContent string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repository, path), []byte(replacementContent), 0o600); err != nil {
		t.Fatalf("WriteFile() replacement error = %v", err)
	}
	runGit(t, repository, "add", path)
	runGit(t, repository, "commit", "-m", "replacement object")
	replacementCommit := gitOutput(t, repository, "rev-parse", "HEAD")
	originalBlob := gitOutputNoReplace(t, repository, "rev-parse", originalCommit+":"+path)
	replacementBlob := gitOutputNoReplace(t, repository, "rev-parse", replacementCommit+":"+path)
	if originalBlob == replacementBlob {
		t.Fatalf("replacement blob unexpectedly matches original %q", originalBlob)
	}
	runGit(t, repository, "replace", originalBlob, replacementBlob)
	runGit(t, repository, "replace", originalCommit, replacementCommit)
	if normal := gitOutput(t, repository, "show", originalCommit+":"+path); normal == gitOutputNoReplace(t, repository, "show", originalCommit+":"+path) {
		t.Fatal("replace refs did not change the visible artifact")
	}
}

func runGitOutput(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() error = %v", err)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func TestGitCommandOutputLimitedRejectsOversizedBlobOutput(t *testing.T) {
	limit := 64
	command := workspaceOutputHelperCommand(t, context.Background(), "emit", limit+1)

	_, err := gitCommandOutputLimited(context.Background(), command, limit, ErrSubjectArtifactTooLarge)
	if err == nil || !errors.Is(err, ErrSubjectArtifactTooLarge) {
		t.Fatalf("gitCommandOutputLimited() error = %v, want ErrSubjectArtifactTooLarge", err)
	}
}

func TestGitCommandOutputLimitedRejectsOversizedTreeOutput(t *testing.T) {
	limit := 96
	command := workspaceOutputHelperCommand(t, context.Background(), "emit", limit+1)

	_, err := gitCommandOutputLimited(context.Background(), command, limit, ErrSubjectTreeTooLarge)
	if err == nil || !errors.Is(err, ErrSubjectTreeTooLarge) {
		t.Fatalf("gitCommandOutputLimited() error = %v, want ErrSubjectTreeTooLarge", err)
	}
}

func TestGitCommandOutputLimitedHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := workspaceOutputHelperCommand(t, ctx, "block", 0)

	result := make(chan error, 1)
	go func() {
		_, err := gitCommandOutputLimited(ctx, command, 64, ErrSubjectTreeTooLarge)
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("gitCommandOutputLimited() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gitCommandOutputLimited() did not stop after context cancellation")
	}
}

func workspaceOutputHelperCommand(t *testing.T, ctx context.Context, mode string, count int) *exec.Cmd {
	t.Helper()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkspaceOutputHelper$", "--", mode, strconv.Itoa(count))
	command.Env = append(os.Environ(), "SYMMETRY_WORKSPACE_OUTPUT_HELPER=1")
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() error = %v", err)
	}
	return command
}

func TestWorkspaceOutputHelper(t *testing.T) {
	if os.Getenv("SYMMETRY_WORKSPACE_OUTPUT_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+2 >= len(os.Args) {
		fmt.Fprintln(os.Stderr, "invalid workspace output helper arguments")
		os.Exit(2)
	}
	mode := os.Args[separator+1]
	count, err := strconv.Atoi(os.Args[separator+2])
	if err != nil || count < 0 {
		fmt.Fprintln(os.Stderr, "invalid workspace output helper count")
		os.Exit(2)
	}
	switch mode {
	case "emit":
		_, _ = os.Stdout.Write([]byte(strings.Repeat("x", count)))
	case "block":
		_, _ = os.Stdout.Write([]byte("ready"))
		for {
			time.Sleep(time.Second)
		}
	default:
		fmt.Fprintln(os.Stderr, "invalid workspace output helper mode")
		os.Exit(2)
	}
}

// subjectDigestFixtureBlobs is the number of committed blobs written by
// writeSubjectDigestFixture.
const subjectDigestFixtureBlobs = 7

// subjectDigestManyBlobs is the number of committed blobs written by
// writeSubjectDigestManyBlobsFixture so that a digest stays in flight long
// enough to observe its Git child and to cancel it mid-stream.
const subjectDigestManyBlobs = 250

// TestMaterializeSubjectHashesEveryBlobInOneBatchStream pins the production
// Subject computation to one persistent Git batch stream for every committed
// blob, and to the canonical manifest digest that the independent source
// script computes for the same commit.
func TestMaterializeSubjectHashesEveryBlobInOneBatchStream(t *testing.T) {
	repository := newSubjectRepository(t)
	writeSubjectDigestFixture(t, repository)
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	expected := subjectManifestDigest(t, repository, commit)

	tracePath := filepath.Join(t.TempDir(), "git-trace2-events.json")
	t.Setenv("GIT_TRACE2_EVENT", tracePath)
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})

	subject, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commit)
	if err != nil {
		t.Fatalf("MaterializeSubject() error = %v", err)
	}
	if subject.TreeDigest != expected {
		t.Fatalf("MaterializeSubject() tree digest = %q, want canonical manifest digest %q", subject.TreeDigest, expected)
	}
	starts := gitTrace2Starts(t, tracePath)
	if len(starts) == 0 {
		t.Fatalf("Git recorded no trace2 process starts in %q; the production Git commands no longer inherit GIT_TRACE2_EVENT", tracePath)
	}
	if got := gitTrace2StartCount(starts, "cat-file"); got != 1 {
		t.Fatalf("Subject computation for %d blobs started %d git cat-file processes, want exactly one persistent batch stream", subjectDigestFixtureBlobs, got)
	}
}

// TestMaterializeSubjectRejectsMissingBlobObject keeps the fail-closed
// refusal for a committed tree that references an object the repository no
// longer has.
func TestMaterializeSubjectRejectsMissingBlobObject(t *testing.T) {
	repository := newSubjectRepository(t)
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	blob := gitOutputNoReplace(t, repository, "rev-parse", commit+":README.md")
	objectPath := filepath.Join(repository, ".git", "objects", blob[:2], blob[2:])
	if err := os.Remove(objectPath); err != nil {
		t.Fatalf("remove committed blob object %q: %v", blob, err)
	}
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})

	_, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commit)
	if err == nil || !errors.Is(err, ErrSubjectUnreachable) {
		t.Fatalf("MaterializeSubject() error = %v, want ErrSubjectUnreachable for a missing committed blob", err)
	}
	var subjectErr *SubjectError
	if !errors.As(err, &subjectErr) || subjectErr.Code != SubjectErrorUnreachable {
		t.Fatalf("MaterializeSubject() error = %v, want typed unreachable subject error", err)
	}
	assertNoOwnedGitChildProcesses(t)
}

// TestMaterializeSubjectRejectsOversizedBlob keeps the per-blob size bound on
// the production Subject computation.
func TestMaterializeSubjectRejectsOversizedBlob(t *testing.T) {
	repository := newSubjectRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "oversized.bin"), bytes.Repeat([]byte{0x5a}, maxSubjectArtifactBlobBytes+1), 0o600); err != nil {
		t.Fatalf("write oversized fixture: %v", err)
	}
	runGit(t, repository, "add", "oversized.bin")
	runGit(t, repository, "commit", "-m", "add oversized blob")
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})

	_, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commit)
	if err == nil || !errors.Is(err, ErrSubjectArtifactTooLarge) {
		t.Fatalf("MaterializeSubject() error = %v, want ErrSubjectArtifactTooLarge", err)
	}
	assertNoOwnedGitChildProcesses(t)
}

// TestMaterializeSubjectCancellationStopsBatchStream cancels a running
// Subject computation and requires the caller to observe the cancellation
// while no owned Git child stream keeps running.
func TestMaterializeSubjectCancellationStopsBatchStream(t *testing.T) {
	repository := newSubjectRepository(t)
	writeSubjectDigestManyBlobsFixture(t, repository)
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	tracePath := filepath.Join(t.TempDir(), "git-trace2-events.json")
	t.Setenv("GIT_TRACE2_EVENT", tracePath)
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := manager.MaterializeSubject(ctx, "primary", subjectResourceID, commit)
		result <- err
	}()

	waitForGitTrace2Start(t, tracePath, "cat-file", 30*time.Second)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("MaterializeSubject() error = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("MaterializeSubject did not stop after context cancellation")
	}
	assertNoOwnedGitChildProcesses(t)
}

// writeSubjectDigestFixture commits a small tree whose entries exercise
// executable files, a symlink entry, embedded binary bytes, a raw path with a
// space and paths whose Git tree order differs from raw byte order.
func writeSubjectDigestFixture(t *testing.T, repository string) {
	t.Helper()
	writeSubjectFixtureFile(t, repository, "alpha.txt", []byte("alpha\n"))
	writeSubjectFixtureFile(t, repository, filepath.Join("alpha", "inside.txt"), []byte("inside\n"))
	writeSubjectFixtureFile(t, repository, "binary.bin", []byte{0x00, 0x7f, 0xff, 0x80, 0x0a, 0x00, 0x01})
	writeSubjectFixtureFile(t, repository, "odd name.txt", []byte("raw path\n"))
	writeSubjectFixtureFile(t, repository, "tool.sh", []byte("#!/bin/sh\necho tool\n"))
	writeSubjectFixtureFile(t, repository, "link-target.txt", []byte("alpha.txt"))
	symlinkBlob := strings.TrimSpace(runGitOutput(t, repository, "hash-object", "-w", "link-target.txt"))
	if err := os.Remove(filepath.Join(repository, "link-target.txt")); err != nil {
		t.Fatalf("remove symlink target fixture: %v", err)
	}
	runGit(t, repository, "add", "--", "alpha.txt", "alpha/inside.txt", "binary.bin", "odd name.txt", "tool.sh")
	runGit(t, repository, "update-index", "--chmod=+x", "tool.sh")
	runGit(t, repository, "update-index", "--add", "--cacheinfo", "120000,"+symlinkBlob+",link.txt")
	runGit(t, repository, "commit", "-m", "subject digest fixture")
}

// writeSubjectDigestManyBlobsFixture commits many small distinct blobs so a
// Subject digest stays in flight long enough to sample and cancel its Git
// child process.
func writeSubjectDigestManyBlobsFixture(t *testing.T, repository string) {
	t.Helper()
	for index := range subjectDigestManyBlobs {
		content := fmt.Sprintf("blob %03d %s\n", index, strings.Repeat("x", 256))
		writeSubjectFixtureFile(t, repository, fmt.Sprintf("many/blob-%03d.txt", index), []byte(content))
	}
	runGit(t, repository, "add", "--", "many")
	runGit(t, repository, "commit", "-m", "many subject blobs")
}

func writeSubjectFixtureFile(t *testing.T, repository, name string, content []byte) {
	t.Helper()
	path := filepath.Join(repository, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create fixture directory for %q: %v", name, err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write fixture file %q: %v", name, err)
	}
}

// subjectManifestDigest derives the canonical Subject tree digest from
// committed Git objects with independent plumbing: NUL-delimited records of
// mode, type, raw path and sha256 blob digest, ordered by raw path bytes.
func subjectManifestDigest(t *testing.T, repository, commit string) string {
	t.Helper()
	type digestEntry struct {
		path   []byte
		record []byte
	}
	output := gitOutputBytes(t, repository, "ls-tree", "--full-tree", "-r", "-z", commit)
	records := bytes.Split(output, []byte{0})
	entries := make([]digestEntry, 0, len(records))
	for index, record := range records {
		if len(record) == 0 {
			if index == len(records)-1 {
				continue
			}
			t.Fatalf("Git tree listing has an empty record at index %d", index)
		}
		header, path, found := bytes.Cut(record, []byte{'\t'})
		if !found {
			t.Fatalf("Git tree record %d has no path separator", index)
		}
		fields := bytes.Split(header, []byte{' '})
		if len(fields) != 3 || !bytes.Equal(fields[1], []byte("blob")) {
			t.Fatalf("Git tree record %d header = %q, want a blob header", index, header)
		}
		content := gitOutputBytes(t, repository, "cat-file", "blob", string(fields[2]))
		blobDigest := sha256.Sum256(content)
		manifest := make([]byte, 0, len(record)+96)
		manifest = append(manifest, fields[0]...)
		manifest = append(manifest, 0)
		manifest = append(manifest, fields[1]...)
		manifest = append(manifest, 0)
		manifest = append(manifest, path...)
		manifest = append(manifest, 0)
		manifest = append(manifest, "sha256:"...)
		manifest = append(manifest, hex.EncodeToString(blobDigest[:])...)
		manifest = append(manifest, 0)
		entries = append(entries, digestEntry{path: append([]byte(nil), path...), record: manifest})
	}
	sort.Slice(entries, func(left, right int) bool {
		return bytes.Compare(entries[left].path, entries[right].path) < 0
	})
	hasher := sha256.New()
	for _, entry := range entries {
		hasher.Write(entry.record)
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}

// gitOutputBytes runs Git without replace objects and returns unmodified
// stdout so binary blobs and NUL-delimited tree listings stay exact.
func gitOutputBytes(t *testing.T, directory string, arguments ...string) []byte {
	t.Helper()
	command := exec.Command("git", append([]string{"--no-replace-objects", "-C", directory}, arguments...)...)
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() error = %v", err)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git --no-replace-objects %s: %v\n%s", strings.Join(arguments, " "), err, stderr.String())
	}
	return output
}

// gitTrace2StartEvent is one Git trace2 process start record.
type gitTrace2StartEvent struct {
	Event string   `json:"event"`
	Argv  []string `json:"argv"`
}

// gitTrace2Starts reads the process start records written by GIT_TRACE2_EVENT.
func gitTrace2Starts(t *testing.T, path string) []gitTrace2StartEvent {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Git trace2 event log %q: %v", path, err)
	}
	starts := make([]gitTrace2StartEvent, 0, 8)
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event gitTrace2StartEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode Git trace2 event %q: %v", line, err)
		}
		if event.Event == "start" {
			starts = append(starts, event)
		}
	}
	return starts
}

// gitTrace2StartCount counts process starts that run the named Git subcommand.
func gitTrace2StartCount(starts []gitTrace2StartEvent, name string) int {
	count := 0
	for _, start := range starts {
		for _, argument := range start.Argv {
			if argument == name {
				count++
				break
			}
		}
	}
	return count
}

// waitForGitTrace2Start waits until the trace log records a process start for
// the named Git subcommand.
func waitForGitTrace2Start(t *testing.T, path, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if content, err := os.ReadFile(path); err == nil {
			for _, line := range strings.Split(string(content), "\n") {
				var event gitTrace2StartEvent
				if json.Unmarshal([]byte(line), &event) != nil || event.Event != "start" {
					continue
				}
				for _, argument := range event.Argv {
					if argument == name {
						return
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Git trace2 log %q never recorded a %s process start within %s", path, name, timeout)
		}
		time.Sleep(time.Millisecond)
	}
}

// gitChildProcessIDs returns the process IDs of Git commands that are direct
// children of this test process, including children that are not yet reaped.
// Only the Linux /proc view exposes this; other platforms return nil, so their
// tests keep the portable cancellation assertions alone.
func gitChildProcessIDs() []int {
	if runtime.GOOS != "linux" {
		return nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	parent := os.Getpid()
	pids := make([]int, 0, 4)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == parent {
			continue
		}
		directory := filepath.Join("/proc", entry.Name())
		stat, err := os.ReadFile(filepath.Join(directory, "stat"))
		if err != nil {
			continue
		}
		command, parentPID, ok := parseProcStat(stat)
		if !ok || parentPID != parent || !strings.HasPrefix(command, "git") {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// parseProcStat reads the command name and parent PID from a /proc stat line.
func parseProcStat(stat []byte) (string, int, bool) {
	open := bytes.IndexByte(stat, '(')
	end := bytes.LastIndexByte(stat, ')')
	if open < 0 || end <= open {
		return "", 0, false
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 2 {
		return "", 0, false
	}
	parentPID, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", 0, false
	}
	return string(stat[open+1 : end]), parentPID, true
}

// assertNoOwnedGitChildProcesses waits briefly for cancelled Git children to
// be reaped and fails when one is still present or appears afterwards. Only
// Linux exposes the /proc view; other platforms keep the portable cancellation
// assertions above this call.
func assertNoOwnedGitChildProcesses(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(gitChildProcessIDs()) == 0 {
			time.Sleep(100 * time.Millisecond)
			if len(gitChildProcessIDs()) == 0 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("context cancellation left owned Git child processes %v", gitChildProcessIDs())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSubjectMeasurementWitness measures the production Subject computation of
// a real checkout through the public workspace entry point. It is opt-in so
// routine runs and package suites are unaffected, and it reports the canonical
// digest, the committed blob count, the Git process trace and the wall time as
// one JSON record for the recorded measurement.
func TestSubjectMeasurementWitness(t *testing.T) {
	repository := os.Getenv("SYMMETRY_SUBJECT_WITNESS_REPOSITORY")
	commit := os.Getenv("SYMMETRY_SUBJECT_WITNESS_COMMIT")
	resourceID := os.Getenv("SYMMETRY_SUBJECT_WITNESS_RESOURCE_ID")
	if repository == "" || commit == "" || resourceID == "" {
		t.Skip("subject measurement witness requires SYMMETRY_SUBJECT_WITNESS_REPOSITORY, SYMMETRY_SUBJECT_WITNESS_COMMIT and SYMMETRY_SUBJECT_WITNESS_RESOURCE_ID")
	}
	tracePath := filepath.Join(t.TempDir(), "git-trace2-events.json")
	t.Setenv("GIT_TRACE2_EVENT", tracePath)
	manager := New(map[string]config.Workspace{
		"measurement": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	started := time.Now()
	subject, err := manager.MaterializeSubject(ctx, "measurement", resourceID, commit)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("MaterializeSubject() for %s error = %v after %s", commit, err, elapsed)
	}
	// The process trace is read before any other Git command runs, so it
	// records exactly the production Subject computation.
	starts := gitTrace2Starts(t, tracePath)
	blobCount := subjectTreeBlobCount(t, repository, commit)
	if blobCount < 1001 {
		t.Fatalf("subject measurement witness requires a checkout with more than a thousand committed blobs, %s has %d", repository, blobCount)
	}
	measurement := struct {
		Commit           string `json:"commit"`
		TreeDigest       string `json:"tree_digest"`
		BlobCount        int    `json:"blob_count"`
		CatFileProcesses int    `json:"cat_file_processes"`
		GitProcesses     int    `json:"git_processes"`
		ElapsedMillis    int64  `json:"elapsed_ms"`
	}{
		Commit: commit, TreeDigest: subject.TreeDigest, BlobCount: blobCount,
		CatFileProcesses: gitTrace2StartCount(starts, "cat-file"), GitProcesses: len(starts),
		ElapsedMillis: elapsed.Milliseconds(),
	}
	encoded, err := json.Marshal(measurement)
	if err != nil {
		t.Fatalf("encode subject measurement: %v", err)
	}
	t.Logf("subject measurement: %s", encoded)
	if measurement.CatFileProcesses != 1 {
		t.Fatalf("measured Subject computation for %d blobs started %d git cat-file processes, want exactly one persistent batch stream", blobCount, measurement.CatFileProcesses)
	}
}

// subjectTreeBlobCount counts committed tree records with independent Git
// plumbing for the measurement witness.
func subjectTreeBlobCount(t *testing.T, repository, commit string) int {
	t.Helper()
	output := gitOutputBytes(t, repository, "ls-tree", "--full-tree", "-r", "-z", commit)
	return bytes.Count(output, []byte{0})
}

// subjectGitCommandOutput runs the bounded workspace Git command the digest
// uses for its committed tree listing, so an injected batch opener is always
// paired with a real committed tree.
func subjectGitCommandOutput(ctx context.Context, repository string, maxBytes int, limitErr error, arguments ...string) ([]byte, error) {
	command := gitNoReplaceObjectsCommand(ctx, repository, arguments...)
	return gitCommandOutputLimited(ctx, command, maxBytes, limitErr)
}

// workspaceBatchHelperCommand starts this test binary as a Git batch stream
// double for the batch framing and teardown regressions.
func workspaceBatchHelperCommand(t *testing.T, ctx context.Context, arguments ...string) *exec.Cmd {
	t.Helper()
	command := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestWorkspaceBatchHelper$", "--"}, arguments...)...)
	command.Env = append(os.Environ(), "SYMMETRY_WORKSPACE_BATCH_HELPER=1")
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() error = %v", err)
	}
	return command
}

// TestWorkspaceBatchHelper is the child half of the batch regressions. It
// serves only when the parent sets its environment marker.
func TestWorkspaceBatchHelper(t *testing.T) {
	if os.Getenv("SYMMETRY_WORKSPACE_BATCH_HELPER") != "1" {
		return
	}
	arguments := workspaceBatchHelperArguments()
	switch {
	case len(arguments) == 3 && arguments[0] == "frame":
		serveWorkspaceBatchFrames(arguments[1], []byte(arguments[2]))
	case len(arguments) == 1 && arguments[0] == "blocked-header":
		// The malformed response stays in the pipe and the child never exits on
		// its own, so only the digest's own teardown can end it.
		_, _ = os.Stdout.WriteString("not-a-batch-header\n")
		for {
			time.Sleep(time.Second)
		}
	default:
		fmt.Fprintln(os.Stderr, "invalid workspace batch helper arguments")
		os.Exit(2)
	}
}

func workspaceBatchHelperArguments() []string {
	for index, argument := range os.Args {
		if argument == "--" {
			return os.Args[index+1:]
		}
	}
	return nil
}

// serveWorkspaceBatchFrames answers batch requests with the named framing
// variant. Only the valid variant keeps serving; every malformed variant
// answers one request and exits so its child is reaped either way.
func serveWorkspaceBatchFrames(variant string, content []byte) {
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	for {
		request, err := reader.ReadString('\n')
		if err != nil {
			os.Exit(0)
		}
		objectID := strings.TrimSuffix(request, "\n")
		switch variant {
		case "valid":
			fmt.Fprintf(writer, "%s blob %d\n", objectID, len(content))
			_, _ = writer.Write(content)
			_ = writer.WriteByte('\n')
		case "wrong-oid":
			fmt.Fprintf(writer, "%s blob %d\n", strings.Repeat("0", len(objectID)), len(content))
			_, _ = writer.Write(content)
			_ = writer.WriteByte('\n')
		case "wrong-type":
			fmt.Fprintf(writer, "%s tree %d\n", objectID, len(content))
			_, _ = writer.Write(content)
			_ = writer.WriteByte('\n')
		case "malformed-size":
			fmt.Fprintf(writer, "%s blob twelve\n", objectID)
		case "negative-size":
			fmt.Fprintf(writer, "%s blob %d\n", objectID, -len(content))
		case "malformed-header":
			fmt.Fprintf(writer, "%s\n", objectID)
		case "truncated-header":
			fmt.Fprintf(writer, "%s blob %d", objectID, len(content))
		case "partial-content":
			fmt.Fprintf(writer, "%s blob %d\n", objectID, len(content)+1)
			_, _ = writer.Write(content)
		case "bad-trailer":
			fmt.Fprintf(writer, "%s blob %d\n", objectID, len(content))
			_, _ = writer.Write(content)
			_ = writer.WriteByte('X')
		case "missing-trailer":
			fmt.Fprintf(writer, "%s blob %d\n", objectID, len(content))
			_, _ = writer.Write(content)
		default:
			fmt.Fprintln(os.Stderr, "invalid workspace batch helper variant")
			os.Exit(2)
		}
		if err := writer.Flush(); err != nil {
			os.Exit(0)
		}
		if variant != "valid" {
			os.Exit(0)
		}
	}
}

// TestSubjectBatchStreamRejectsMalformedResponses pins the batch framing
// contract against a real committed tree and a helper child: a malformed
// response never yields a digest, the digest fails, and the owned child is
// waited for and reaped. The valid row is the control that the same wiring
// yields the canonical manifest digest.
func TestSubjectBatchStreamRejectsMalformedResponses(t *testing.T) {
	repository := newSubjectRepository(t)
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	blob := gitOutputNoReplace(t, repository, "rev-parse", commit+":README.md")
	content := gitOutputBytes(t, repository, "cat-file", "blob", blob)
	want := subjectManifestDigest(t, repository, commit)

	for _, variant := range []string{
		"valid", "wrong-oid", "wrong-type", "malformed-size", "negative-size",
		"malformed-header", "truncated-header", "partial-content", "bad-trailer", "missing-trailer",
	} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			command := workspaceBatchHelperCommand(t, ctx, "frame", variant, string(content))
			var stream *subjectGitBatchStream
			digest, err := gitTreeDigestWithReaders(ctx, repository, commit, subjectGitCommandOutput,
				func(openContext context.Context, streamRepository string) (*subjectGitBatchStream, error) {
					opened, openErr := startSubjectGitBatch(openContext, command, nil, nil)
					stream = opened
					return opened, openErr
				})
			if variant == "valid" {
				if err != nil {
					t.Fatalf("subject digest for a valid batch frame error = %v", err)
				}
				if digest != want {
					t.Fatalf("subject digest for a valid batch frame = %q, want canonical digest %q", digest, want)
				}
			} else if err == nil {
				t.Fatalf("subject digest for malformed batch frame %q = %q, want an error", variant, digest)
			}
			if stream == nil {
				t.Fatal("batch opener was not used")
			}
			state := stream.command.ProcessState
			if state == nil || command.Process == nil || state.Pid() != command.Process.Pid {
				t.Fatalf("batch helper child was not waited for and reaped: %v", state)
			}
		})
	}
}

// TestSubjectBatchStreamCancelFailureFallsBackToKill pins the desynced
// teardown contract: when the path's cancellation fails, the owned batch child
// is still killed and reaped promptly, and the framing failure, the failed
// cancellation and the path's unresolved-process fence all stay observable.
// The test does not cancel its own context before asserting, so a prompt
// return proves the child was stopped by the digest's own teardown.
func TestSubjectBatchStreamCancelFailureFallsBackToKill(t *testing.T) {
	repository := newSubjectRepository(t)
	commit := gitOutput(t, repository, "rev-parse", "HEAD")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelFailure := errors.New("injected process-group stop failure")
	fence := errors.New("injected unresolved batch process fence")

	command := workspaceBatchHelperCommand(t, ctx, "blocked-header")
	// The timeout branch and deferred kill own helper teardown.
	// WaitDelay bounds I/O cleanup, not the test's execution time.
	command.WaitDelay = 10 * time.Second
	defer func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}()
	var cancelCalls atomic.Int32
	command.Cancel = func() error {
		if cancelCalls.Add(1) == 1 {
			return cancelFailure
		}
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return command.Process.Kill()
	}

	type digestOutcome struct {
		digest string
		err    error
	}
	// Unbuffered handshake: the opener cannot return until this test has taken
	// the stream, so the digest cannot publish an outcome while the select below
	// still has the unused-stream arm to consider. With a buffered stream channel
	// a digest that finished early made both arms ready and the select could
	// report the injected stream as unused after it had been used.
	opened := make(chan *subjectGitBatchStream)
	result := make(chan digestOutcome, 1)
	go func() {
		digest, err := gitTreeDigestWithReaders(ctx, repository, commit, subjectGitCommandOutput,
			func(openContext context.Context, streamRepository string) (*subjectGitBatchStream, error) {
				stream, openErr := startSubjectGitBatch(openContext, command, nil, fence)
				if stream != nil {
					opened <- stream
				}
				return stream, openErr
			})
		result <- digestOutcome{digest: digest, err: err}
	}()

	var stream *subjectGitBatchStream
	select {
	case stream = <-opened:
	case outcome := <-result:
		t.Fatalf("subject digest did not use the injected batch stream: %v", outcome.err)
	}
	var outcome digestOutcome
	select {
	case outcome = <-result:
	case <-time.After(5 * time.Second):
		cancel()
		outcome = <-result
		t.Fatalf("subject digest did not stop after a failed batch cancellation: %v (cancellation attempts %d)", outcome.err, cancelCalls.Load())
	}
	if outcome.err == nil {
		t.Fatalf("subject digest for a desynced batch stream = %q, want an error", outcome.digest)
	}
	if ctx.Err() != nil {
		t.Fatalf("subject digest stopped only after the test cancelled its context")
	}
	if cancelCalls.Load() < 1 {
		t.Fatal("batch stream cancellation was never attempted")
	}
	if !errors.Is(outcome.err, cancelFailure) {
		t.Fatalf("subject digest error = %v, want the failed cancellation error joined", outcome.err)
	}
	if !errors.Is(outcome.err, fence) {
		t.Fatalf("subject digest error = %v, want the unresolved-process fence joined", outcome.err)
	}
	state := stream.command.ProcessState
	if state == nil || command.Process == nil || state.Pid() != command.Process.Pid {
		t.Fatalf("batch helper child was not waited for and reaped: %v", state)
	}
	if state.Success() {
		t.Fatalf("batch helper child exited on its own instead of being killed: %v", state)
	}
}
