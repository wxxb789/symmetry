package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

func TestPrepareExistingCheckoutResolvesAbsolutePath(t *testing.T) {
	checkout := t.TempDir()
	link := filepath.Join(t.TempDir(), "checkout-link")
	if err := os.Symlink(checkout, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy:  config.WorkspacePolicyExistingCheckout,
			Path:    link,
			Cleanup: config.CleanupNever,
		},
	})

	prepared, err := manager.Prepare(context.Background(), "primary", RunRef{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	want, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	if prepared.Path != want {
		t.Errorf("Prepared.Path = %q, want %q", prepared.Path, want)
	}
	if err := manager.Cleanup(context.Background(), prepared, true); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := os.Stat(checkout); err != nil {
		t.Fatalf("existing checkout was removed: %v", err)
	}
}

func TestPrepareRejectsUnknownBindingAndUnsafeRunID(t *testing.T) {
	manager := New(map[string]config.Workspace{
		"primary": {Policy: config.WorkspacePolicyExistingCheckout, Path: t.TempDir(), Cleanup: config.CleanupNever},
	})

	if _, err := manager.Prepare(context.Background(), "missing", RunRef{RunID: "run-1", Generation: 1}); err == nil || !strings.Contains(err.Error(), "workspace binding") {
		t.Fatalf("Prepare() error = %v, want unknown binding error", err)
	}
	if _, err := manager.Prepare(context.Background(), "primary", RunRef{RunID: "../../escape", Generation: 1}); err == nil || !strings.Contains(err.Error(), "run_id") {
		t.Fatalf("Prepare() error = %v, want invalid run_id error", err)
	}
}

func TestPrepareWorktreeAndCleanupPolicies(t *testing.T) {
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	manager := New(map[string]config.Workspace{
		"always": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
		"success": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupOnSuccess,
		},
		"never": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupNever,
		},
	})

	always, err := manager.Prepare(context.Background(), "always", RunRef{RunID: "run-always", Generation: 2})
	if err != nil {
		t.Fatalf("Prepare(always) error = %v", err)
	}
	if !pathWithin(root, always.Path) {
		t.Fatalf("worktree path %q escapes root %q", always.Path, root)
	}
	retry, err := manager.Prepare(context.Background(), "always", RunRef{RunID: "run-always", Generation: 2})
	if err != nil {
		t.Fatalf("Prepare retry error = %v", err)
	}
	if retry.Path != always.Path {
		t.Errorf("retry path = %q, want %q", retry.Path, always.Path)
	}
	if err := manager.Cleanup(context.Background(), always, false); err != nil {
		t.Fatalf("Cleanup(always) error = %v", err)
	}
	if _, err := os.Stat(always.Path); !os.IsNotExist(err) {
		t.Fatalf("always cleanup left worktree at %q, stat error = %v", always.Path, err)
	}
	if _, err := os.Stat(always.reservation); !os.IsNotExist(err) {
		t.Fatalf("always cleanup left reservation at %q, stat error = %v", always.reservation, err)
	}

	onSuccess, err := manager.Prepare(context.Background(), "success", RunRef{RunID: "run-success", Generation: 3})
	if err != nil {
		t.Fatalf("Prepare(success) error = %v", err)
	}
	if err := manager.Cleanup(context.Background(), onSuccess, false); err != nil {
		t.Fatalf("Cleanup(on_success failure) error = %v", err)
	}
	if _, err := os.Stat(onSuccess.Path); err != nil {
		t.Fatalf("on_success cleanup removed failed worktree: %v", err)
	}
	if err := manager.Cleanup(context.Background(), onSuccess, true); err != nil {
		t.Fatalf("Cleanup(on_success success) error = %v", err)
	}
	if _, err := os.Stat(onSuccess.Path); !os.IsNotExist(err) {
		t.Fatalf("on_success cleanup left successful worktree: %v", err)
	}
	if _, err := os.Stat(onSuccess.reservation); !os.IsNotExist(err) {
		t.Fatalf("on_success cleanup left reservation: %v", err)
	}

	never, err := manager.Prepare(context.Background(), "never", RunRef{RunID: "run-never", Generation: 4})
	if err != nil {
		t.Fatalf("Prepare(never) error = %v", err)
	}
	if err := manager.Cleanup(context.Background(), never, true); err != nil {
		t.Fatalf("Cleanup(never) error = %v", err)
	}
	if _, err := os.Stat(never.Path); err != nil {
		t.Fatalf("never cleanup removed worktree: %v", err)
	}
}

func TestConcurrentPrepareUsesOneReservation(t *testing.T) {
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	bindings := map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	}
	managers := []*Manager{New(bindings), New(bindings)}
	start := make(chan struct{})
	results := make(chan prepareResult, len(managers))
	var group sync.WaitGroup
	for _, manager := range managers {
		group.Add(1)
		go func(manager *Manager) {
			defer group.Done()
			<-start
			prepared, err := manager.Prepare(context.Background(), "primary", RunRef{RunID: "run-concurrent", Generation: 1})
			results <- prepareResult{prepared: prepared, err: err}
		}(manager)
	}
	close(start)
	group.Wait()
	close(results)

	var prepared []Prepared
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent Prepare() error = %v", result.err)
		}
		prepared = append(prepared, result.prepared)
	}
	if len(prepared) != 2 || prepared[0].Path != prepared[1].Path {
		t.Fatalf("concurrent Prepare() paths = %#v, want one shared worktree", prepared)
	}
	if err := managers[0].Cleanup(context.Background(), prepared[0], true); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestPrepareAdoptsReservedWorktreeAfterJournalLoss(t *testing.T) {
	repository := newRepository(t)
	bindings := map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	}
	run := RunRef{RunID: "run-recover", Generation: 1}
	first, err := New(bindings).Prepare(context.Background(), "primary", run)
	if err != nil {
		t.Fatalf("initial Prepare() error = %v", err)
	}
	if err := os.Remove(first.journal); err != nil {
		t.Fatalf("Remove(journal) error = %v", err)
	}

	second, err := New(bindings).Prepare(context.Background(), "primary", run)
	if err != nil {
		t.Fatalf("recovery Prepare() error = %v", err)
	}
	if second.Path != first.Path {
		t.Errorf("recovery path = %q, want %q", second.Path, first.Path)
	}
	if _, err := os.Stat(second.journal); err != nil {
		t.Fatalf("recovery did not restore journal: %v", err)
	}
	if err := New(bindings).Cleanup(context.Background(), second, true); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestConcurrentRecoveryAdoptsOneWorktree(t *testing.T) {
	repository := newRepository(t)
	bindings := map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	}
	run := RunRef{RunID: "run-concurrent-recovery", Generation: 1}
	first, err := New(bindings).Prepare(context.Background(), "primary", run)
	if err != nil {
		t.Fatalf("initial Prepare() error = %v", err)
	}
	if err := os.Remove(first.journal); err != nil {
		t.Fatalf("Remove(journal) error = %v", err)
	}

	managers := []*Manager{New(bindings), New(bindings)}
	start := make(chan struct{})
	results := make(chan prepareResult, len(managers))
	var group sync.WaitGroup
	for _, manager := range managers {
		group.Add(1)
		go func(manager *Manager) {
			defer group.Done()
			<-start
			prepared, err := manager.Prepare(context.Background(), "primary", run)
			results <- prepareResult{prepared: prepared, err: err}
		}(manager)
	}
	close(start)
	group.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			t.Fatalf("recovery Prepare() error = %v", result.err)
		}
		if result.prepared.Path != first.Path {
			t.Errorf("recovery path = %q, want %q", result.prepared.Path, first.Path)
		}
	}
	if err := New(bindings).Cleanup(context.Background(), first, true); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestPrepareRefusesForeignTargetWithReservation(t *testing.T) {
	repository := newRepository(t)
	bindings := map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	}
	run := RunRef{RunID: "run-foreign", Generation: 1}
	first, err := New(bindings).Prepare(context.Background(), "primary", run)
	if err != nil {
		t.Fatalf("initial Prepare() error = %v", err)
	}
	if err := os.Remove(first.journal); err != nil {
		t.Fatalf("Remove(journal) error = %v", err)
	}
	runGit(t, repository, "worktree", "remove", "--force", first.Path)
	if err := os.MkdirAll(first.Path, 0o700); err != nil {
		t.Fatalf("MkdirAll(foreign target) error = %v", err)
	}

	_, err = New(bindings).Prepare(context.Background(), "primary", run)
	if err == nil || !strings.Contains(err.Error(), "foreign") {
		t.Fatalf("Prepare() error = %v, want foreign target refusal", err)
	}
	if _, err := os.Stat(first.Path); err != nil {
		t.Fatalf("foreign target was removed: %v", err)
	}
}

func TestPrepareReleasesCreatedReservationAfterFailedAdd(t *testing.T) {
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	run := RunRef{RunID: "run-retry", Generation: 1}
	bindings := map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(bindings).Prepare(ctx, "primary", run)
	if err != context.Canceled {
		t.Fatalf("Prepare() error = %v, want context.Canceled", err)
	}
	reservation := filepath.Join(root, reservationDirectory, "binding-primary", "run-run-retry", "generation-1.json")
	target := filepath.Join(root, "binding-primary", "run-run-retry", "generation-1")
	if _, err := os.Stat(reservation); !os.IsNotExist(err) {
		t.Fatalf("failed Prepare() left reservation at %q, stat error = %v", reservation, err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("failed Prepare() left managed target at %q, stat error = %v", target, err)
	}

	prepared, err := New(bindings).Prepare(context.Background(), "primary", run)
	if err != nil {
		t.Fatalf("retry Prepare() error = %v", err)
	}
	if err := New(bindings).Cleanup(context.Background(), prepared, true); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestPrepareSyncsReservationAndJournal(t *testing.T) {
	repository := newRepository(t)
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: repository, Root: filepath.Join(t.TempDir(), "worktrees"), Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	fileSyncs := 0
	directories := make(map[string]int)
	manager.syncFile = func(*os.File) error {
		fileSyncs++
		return nil
	}
	manager.syncDirectory = func(path string) error {
		directories[path]++
		return nil
	}

	prepared, err := manager.Prepare(context.Background(), "primary", RunRef{RunID: "run-sync", Generation: 1})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if fileSyncs < 2 {
		t.Errorf("file sync count = %d, want reservation and journal syncs", fileSyncs)
	}
	if directories[filepath.Dir(prepared.journal)] == 0 {
		t.Errorf("journal parent %q was not synced", filepath.Dir(prepared.journal))
	}
	if directories[filepath.Dir(prepared.reservation)] == 0 {
		t.Errorf("reservation parent %q was not synced", filepath.Dir(prepared.reservation))
	}
	if err := manager.Cleanup(context.Background(), prepared, true); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

type prepareResult struct {
	prepared Prepared
	err      error
}

func TestCleanupRefusesWorktreeWithoutOwnershipJournal(t *testing.T) {
	repository := newRepository(t)
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy:     config.WorkspacePolicyGitWorktree,
			Repository: repository,
			Root:       filepath.Join(t.TempDir(), "worktrees"),
			Ref:        "HEAD",
			Cleanup:    config.CleanupAlways,
		},
	})
	prepared, err := manager.Prepare(context.Background(), "primary", RunRef{RunID: "run-owned", Generation: 1})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if err := os.Remove(prepared.journal); err != nil {
		t.Fatalf("Remove(journal) error = %v", err)
	}

	err = manager.Cleanup(context.Background(), prepared, true)
	if err == nil || !strings.Contains(err.Error(), "ownership file is missing") {
		t.Fatalf("Cleanup() error = %v, want missing ownership journal error", err)
	}
	if _, err := os.Stat(prepared.Path); err != nil {
		t.Fatalf("Cleanup() removed unowned worktree: %v", err)
	}
	runGit(t, repository, "worktree", "remove", "--force", prepared.Path)
}

func TestPrepareRejectsSymlinkRootEscape(t *testing.T) {
	base := t.TempDir()
	escape := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	parent := filepath.Join(root, "binding-primary")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Symlink(escape, filepath.Join(parent, "run-escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	manager := New(map[string]config.Workspace{
		"primary": {
			Policy: config.WorkspacePolicyGitWorktree, Repository: newRepository(t), Root: root, Ref: "HEAD", Cleanup: config.CleanupAlways,
		},
	})
	_, err := manager.Prepare(context.Background(), "primary", RunRef{RunID: "escape", Generation: 1})
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("Prepare() error = %v, want root escape error", err)
	}
}

func newRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	runGit(t, repository, "init")
	runGit(t, repository, "config", "user.email", "tests@example.test")
	runGit(t, repository, "config", "user.name", "Symmetry Tests")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repository, "add", "README.md")
	runGit(t, repository, "commit", "-m", "initial")
	return repository
}

func runGit(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
