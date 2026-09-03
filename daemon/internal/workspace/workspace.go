// Package workspace prepares and removes machine-local run workspaces.
package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

const (
	journalName             = ".symmetry-workspace.json"
	reservationDirectory    = ".symmetry-reservations"
	reservationPollInterval = 10 * time.Millisecond
	reservationWait         = 5 * time.Second
)

var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// RunRef identifies one fenced execution generation.
type RunRef struct {
	RunID      string
	Generation int64
}

// Prepared is a workspace resolved for a specific run.
type Prepared struct {
	Path       string
	BindingKey string
	Run        RunRef

	managed     bool
	policy      config.CleanupPolicy
	repository  string
	root        string
	journal     string
	reservation string
}

// Service is the workspace boundary used by run execution.
type Service interface {
	Prepare(context.Context, string, RunRef) (Prepared, error)
	Recover(context.Context, string, RunRef, string) (Prepared, error)
	Cleanup(context.Context, Prepared, bool) error
}

// Manager resolves bindings only from the daemon's local configuration.
type Manager struct {
	bindings      map[string]config.Workspace
	syncFile      func(*os.File) error
	syncDirectory func(string) error
	removeFile    func(string) error
}

// New creates a workspace manager from local workspace bindings.
func New(bindings map[string]config.Workspace) *Manager {
	copyOfBindings := make(map[string]config.Workspace, len(bindings))
	for key, binding := range bindings {
		copyOfBindings[key] = binding
	}
	return &Manager{
		bindings:      copyOfBindings,
		syncFile:      func(file *os.File) error { return file.Sync() },
		syncDirectory: syncDirectory,
		removeFile:    os.Remove,
	}
}

// Prepare resolves a local binding and prepares its workspace for run.
func (manager *Manager) Prepare(ctx context.Context, bindingKey string, run RunRef) (Prepared, error) {
	if manager == nil {
		return Prepared{}, errors.New("workspace manager is nil")
	}
	binding, ok := manager.bindings[bindingKey]
	if !ok {
		return Prepared{}, fmt.Errorf("workspace binding %q is not configured", bindingKey)
	}
	if err := validateRun(run); err != nil {
		return Prepared{}, err
	}

	switch binding.Policy {
	case config.WorkspacePolicyExistingCheckout:
		return prepareExisting(bindingKey, binding, run)
	case config.WorkspacePolicyGitWorktree:
		return manager.prepareWorktree(ctx, bindingKey, binding, run)
	default:
		return Prepared{}, fmt.Errorf("workspace binding %q has unsupported policy %q", bindingKey, binding.Policy)
	}
}

// Recover reconstructs an already prepared workspace without creating one.
func (manager *Manager) Recover(ctx context.Context, bindingKey string, run RunRef, persistedPath string) (Prepared, error) {
	if manager == nil {
		return Prepared{}, errors.New("workspace manager is nil")
	}
	binding, ok := manager.bindings[bindingKey]
	if !ok {
		return Prepared{}, fmt.Errorf("workspace binding %q is not configured", bindingKey)
	}
	if err := validateRun(run); err != nil {
		return Prepared{}, err
	}
	if persistedPath != "" && !filepath.IsAbs(persistedPath) {
		return Prepared{}, errors.New("persisted workspace path must be absolute")
	}

	switch binding.Policy {
	case config.WorkspacePolicyExistingCheckout:
		prepared, err := prepareExisting(bindingKey, binding, run)
		if err != nil {
			return Prepared{}, err
		}
		if persistedPath == "" {
			return prepared, nil
		}
		same, err := sameDirectory(prepared.Path, persistedPath)
		if err != nil {
			return Prepared{}, fmt.Errorf("compare persisted existing checkout: %w", err)
		}
		if !same {
			return Prepared{}, fmt.Errorf("persisted workspace path %q does not match configured existing checkout", persistedPath)
		}
		return prepared, nil
	case config.WorkspacePolicyGitWorktree:
		prepared, err := recoverableWorktree(bindingKey, binding, run, persistedPath == "")
		if err != nil {
			return Prepared{}, err
		}
		if persistedPath != "" && filepath.Clean(persistedPath) != prepared.Path {
			return Prepared{}, fmt.Errorf("persisted workspace path %q does not match configured worktree target", persistedPath)
		}
		reservationExists, err := pathExists(prepared.reservation)
		if err != nil {
			return Prepared{}, fmt.Errorf("inspect workspace reservation: %w", err)
		}
		if reservationExists {
			if err := verifyOwnedFile(prepared.reservation, prepared); err != nil {
				return Prepared{}, fmt.Errorf("verify workspace reservation: %w", err)
			}
		}
		journalExists, err := pathExists(prepared.journal)
		if err != nil {
			return Prepared{}, fmt.Errorf("inspect workspace journal: %w", err)
		}
		contains, err := worktreeContains(ctx, prepared.repository, prepared.Path)
		if err != nil {
			return Prepared{}, err
		}
		targetExists, err := pathExists(prepared.Path)
		if err != nil {
			return Prepared{}, fmt.Errorf("inspect workspace target %q: %w", prepared.Path, err)
		}
		if !contains {
			if targetExists {
				return Prepared{}, fmt.Errorf("workspace target %q is not a registered worktree", prepared.Path)
			}
			return prepared, nil
		}
		if !reservationExists {
			return Prepared{}, fmt.Errorf("verify workspace reservation: ownership file is missing")
		}
		if journalExists {
			if err := manager.verifyPrepared(ctx, prepared); err != nil {
				return Prepared{}, err
			}
			return prepared, nil
		}
		if err := manager.verifyManagedTarget(ctx, prepared); err != nil {
			return Prepared{}, err
		}
		if err := manager.writeJournal(prepared); err != nil {
			return Prepared{}, err
		}
		return prepared, nil
	default:
		return Prepared{}, fmt.Errorf("workspace binding %q has unsupported policy %q", bindingKey, binding.Policy)
	}
}

// Cleanup removes a daemon-owned worktree when its configured policy permits it.
func (manager *Manager) Cleanup(ctx context.Context, prepared Prepared, succeeded bool) error {
	if manager == nil {
		return errors.New("workspace manager is nil")
	}
	if !prepared.managed {
		return nil
	}

	remove := prepared.policy == config.CleanupAlways ||
		(prepared.policy == config.CleanupOnSuccess && succeeded)
	if !remove {
		return nil
	}
	contains, err := worktreeContains(ctx, prepared.repository, prepared.Path)
	if err != nil {
		return err
	}
	targetExists, err := pathExists(prepared.Path)
	if err != nil {
		return fmt.Errorf("inspect workspace target %q: %w", prepared.Path, err)
	}
	if !contains {
		if targetExists {
			return fmt.Errorf("workspace target %q is not a worktree of configured repository", prepared.Path)
		}
		reservationExists, err := pathExists(prepared.reservation)
		if err != nil {
			return fmt.Errorf("inspect workspace reservation: %w", err)
		}
		if !reservationExists {
			return nil
		}
		return manager.removeReservation(prepared)
	}
	if !targetExists {
		return fmt.Errorf("workspace target %q is still registered but is missing", prepared.Path)
	}
	if err := manager.verifyPrepared(ctx, prepared); err != nil {
		return err
	}

	command := exec.CommandContext(ctx, "git", "-C", prepared.repository, "worktree", "remove", "--force", prepared.Path)
	if output, err := command.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("remove git worktree %q: %w: %s", prepared.Path, err, strings.TrimSpace(string(output)))
	}
	return manager.removeReservation(prepared)
}

func prepareExisting(bindingKey string, binding config.Workspace, run RunRef) (Prepared, error) {
	path, err := resolveDirectory(binding.Path)
	if err != nil {
		return Prepared{}, fmt.Errorf("resolve existing checkout %q: %w", binding.Path, err)
	}
	return Prepared{Path: path, BindingKey: bindingKey, Run: run}, nil
}

func (manager *Manager) prepareWorktree(ctx context.Context, bindingKey string, binding config.Workspace, run RunRef) (Prepared, error) {
	repository, err := resolveDirectory(binding.Repository)
	if err != nil {
		return Prepared{}, fmt.Errorf("resolve workspace repository %q: %w", binding.Repository, err)
	}
	root, err := resolveRoot(binding.Root)
	if err != nil {
		return Prepared{}, fmt.Errorf("resolve workspace root %q: %w", binding.Root, err)
	}
	target := filepath.Join(root, "binding-"+bindingKey, "run-"+run.RunID, fmt.Sprintf("generation-%d", run.Generation))
	if err := ensureTargetParent(root, filepath.Dir(target)); err != nil {
		return Prepared{}, err
	}

	prepared := Prepared{
		Path:       target,
		BindingKey: bindingKey,
		Run:        run,
		managed:    true,
		policy:     binding.Cleanup,
		repository: repository,
		root:       root,
		journal:    filepath.Join(target, journalName),
		reservation: filepath.Join(
			root,
			reservationDirectory,
			"binding-"+bindingKey,
			"run-"+run.RunID,
			fmt.Sprintf("generation-%d.json", run.Generation),
		),
	}
	created, err := manager.reserve(prepared)
	if err != nil {
		return Prepared{}, err
	}
	if !created {
		return manager.recoverPrepared(ctx, prepared)
	}

	exists, err := pathExists(target)
	if err != nil {
		return Prepared{}, fmt.Errorf("inspect reserved worktree target %q: %w", target, err)
	}
	if exists {
		return Prepared{}, fmt.Errorf("refusing foreign worktree target %q after creating reservation", target)
	}

	command := exec.CommandContext(ctx, "git", "-C", repository, "worktree", "add", "--detach", target, binding.Ref)
	if output, err := command.CombinedOutput(); err != nil {
		return manager.handleFailedAdd(ctx, err, output, prepared)
	}
	if err := manager.verifyManagedTarget(ctx, prepared); err != nil {
		return Prepared{}, err
	}
	if err := manager.writeJournal(prepared); err != nil {
		cleanupErr := manager.removeKnownWorktree(context.Background(), prepared)
		if cleanupErr != nil {
			return Prepared{}, fmt.Errorf("write workspace journal: %w; remove unjournaled worktree: %v", err, cleanupErr)
		}
		if cleanupReservationErr := manager.removeReservation(prepared); cleanupReservationErr != nil {
			return Prepared{}, fmt.Errorf("write workspace journal: %w; remove reservation: %v", err, cleanupReservationErr)
		}
		return Prepared{}, err
	}
	return prepared, nil
}

func recoverableWorktree(bindingKey string, binding config.Workspace, run RunRef, allowMissingRoot bool) (Prepared, error) {
	repository, err := resolveDirectory(binding.Repository)
	if err != nil {
		return Prepared{}, fmt.Errorf("resolve workspace repository %q: %w", binding.Repository, err)
	}
	root, err := resolveRecoveryRoot(binding.Root, allowMissingRoot)
	if err != nil {
		return Prepared{}, fmt.Errorf("resolve workspace root %q: %w", binding.Root, err)
	}
	target := filepath.Join(root, "binding-"+bindingKey, "run-"+run.RunID, fmt.Sprintf("generation-%d", run.Generation))
	return Prepared{
		Path:       target,
		BindingKey: bindingKey,
		Run:        run,
		managed:    true,
		policy:     binding.Cleanup,
		repository: repository,
		root:       root,
		journal:    filepath.Join(target, journalName),
		reservation: filepath.Join(
			root,
			reservationDirectory,
			"binding-"+bindingKey,
			"run-"+run.RunID,
			fmt.Sprintf("generation-%d.json", run.Generation),
		),
	}, nil
}

// resolveRecoveryRoot avoids creating a configured root while reconstructing a
// workspace after a crash. A missing root is safe only for empty-path recovery,
// where the deterministic target is needed to inspect any leftover artifacts.
func resolveRecoveryRoot(path string, allowMissing bool) (string, error) {
	root, err := resolveDirectory(path)
	if err == nil || !allowMissing {
		return root, err
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	return filepath.Clean(path), nil
}

func (manager *Manager) handleFailedAdd(ctx context.Context, addErr error, output []byte, prepared Prepared) (Prepared, error) {
	contains, err := worktreeContains(context.Background(), prepared.repository, prepared.Path)
	if err != nil {
		return Prepared{}, fmt.Errorf("add git worktree %q: %w: %s; inspect worktree registration: %v", prepared.Path, addErr, strings.TrimSpace(string(output)), err)
	}
	if contains {
		return manager.recoverPrepared(context.Background(), prepared)
	}

	exists, err := pathExists(prepared.Path)
	if err != nil {
		return Prepared{}, fmt.Errorf("add git worktree %q: %w: %s; inspect target: %v", prepared.Path, addErr, strings.TrimSpace(string(output)), err)
	}
	if exists {
		return Prepared{}, fmt.Errorf("add git worktree %q: %w: %s; refusing foreign worktree target", prepared.Path, addErr, strings.TrimSpace(string(output)))
	}
	if err := manager.removeReservation(prepared); err != nil {
		return Prepared{}, fmt.Errorf("add git worktree %q: %w: %s; remove reservation: %v", prepared.Path, addErr, strings.TrimSpace(string(output)), err)
	}
	if ctx.Err() != nil {
		return Prepared{}, ctx.Err()
	}
	return Prepared{}, fmt.Errorf("add git worktree %q: %w: %s", prepared.Path, addErr, strings.TrimSpace(string(output)))
}

func (manager *Manager) recoverPrepared(ctx context.Context, prepared Prepared) (Prepared, error) {
	deadline := time.NewTimer(reservationWait)
	defer deadline.Stop()
	ticker := time.NewTicker(reservationPollInterval)
	defer ticker.Stop()
	foreignTarget := false

	for {
		adopted, targetExists, err := manager.tryRecoverPrepared(ctx, prepared)
		if err != nil {
			return Prepared{}, err
		}
		if adopted {
			return prepared, nil
		}
		foreignTarget = foreignTarget || targetExists

		select {
		case <-ctx.Done():
			return Prepared{}, ctx.Err()
		case <-deadline.C:
			if foreignTarget {
				return Prepared{}, fmt.Errorf("refusing foreign worktree target %q", prepared.Path)
			}
			return Prepared{}, fmt.Errorf("workspace reservation %q did not become recoverable", prepared.reservation)
		case <-ticker.C:
		}
	}
}

func (manager *Manager) tryRecoverPrepared(ctx context.Context, prepared Prepared) (bool, bool, error) {
	if exists, err := pathExists(prepared.journal); err != nil {
		return false, false, fmt.Errorf("inspect workspace journal %q: %w", prepared.journal, err)
	} else if exists {
		if err := manager.verifyPrepared(ctx, prepared); err != nil {
			return false, false, err
		}
		return true, false, nil
	}

	contains, err := worktreeContains(ctx, prepared.repository, prepared.Path)
	if err != nil {
		return false, false, err
	}
	if contains {
		if err := manager.verifyManagedTarget(ctx, prepared); err != nil {
			return false, false, err
		}
		if err := manager.writeJournal(prepared); err != nil {
			return false, false, err
		}
		return true, false, nil
	}

	exists, err := pathExists(prepared.Path)
	if err != nil {
		return false, false, fmt.Errorf("inspect reserved worktree target %q: %w", prepared.Path, err)
	}
	return false, exists, nil
}

func (manager *Manager) reserve(prepared Prepared) (bool, error) {
	parent := filepath.Dir(prepared.reservation)
	if err := ensureTargetParent(prepared.root, parent); err != nil {
		return false, fmt.Errorf("create workspace reservation parent: %w", err)
	}
	contents, err := encodeOwnership(prepared)
	if err != nil {
		return false, err
	}
	temporary, err := os.CreateTemp(parent, ".reservation-*")
	if err != nil {
		return false, fmt.Errorf("create workspace reservation: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return false, fmt.Errorf("protect workspace reservation: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return false, fmt.Errorf("write workspace reservation: %w", err)
	}
	if err := manager.syncFile(temporary); err != nil {
		temporary.Close()
		return false, fmt.Errorf("sync workspace reservation: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close workspace reservation: %w", err)
	}
	if err := os.Link(temporaryPath, prepared.reservation); err == nil {
		if err := manager.syncDirectory(parent); err != nil {
			return false, fmt.Errorf("sync workspace reservation parent: %w", err)
		}
		return true, nil
	} else if !errors.Is(err, os.ErrExist) {
		return false, fmt.Errorf("publish workspace reservation: %w", err)
	}

	contents, err = os.ReadFile(prepared.reservation)
	if err != nil {
		return false, fmt.Errorf("read workspace reservation %q: %w", prepared.reservation, err)
	}
	if err := verifyOwnership(contents, prepared); err != nil {
		return false, fmt.Errorf("workspace reservation %q is not owned by this run: %w", prepared.reservation, err)
	}
	return false, nil
}

func (manager *Manager) writeJournal(prepared Prepared) error {
	exists, err := pathExists(prepared.journal)
	if err != nil {
		return fmt.Errorf("inspect workspace journal: %w", err)
	}
	if exists {
		if err := verifyOwnedFile(prepared.journal, prepared); err != nil {
			return fmt.Errorf("refusing to replace workspace journal: %w", err)
		}
		return nil
	}

	contents, err := encodeOwnership(prepared)
	if err != nil {
		return err
	}
	if err := writeDurableFile(prepared.journal, contents, manager.syncFile, manager.syncDirectory); err != nil {
		if verifyErr := verifyOwnedFile(prepared.journal, prepared); verifyErr == nil {
			return nil
		}
		return fmt.Errorf("publish workspace journal: %w", err)
	}
	return nil
}

func (manager *Manager) verifyPrepared(ctx context.Context, prepared Prepared) error {
	if err := validateRun(prepared.Run); err != nil {
		return err
	}
	if prepared.journal != filepath.Join(prepared.Path, journalName) ||
		prepared.reservation == "" || !within(prepared.root, prepared.Path) || !within(prepared.root, prepared.reservation) {
		return errors.New("refusing to clean an unowned workspace path")
	}
	if err := verifyOwnedFile(prepared.reservation, prepared); err != nil {
		return fmt.Errorf("verify workspace reservation: %w", err)
	}
	if err := verifyOwnedFile(prepared.journal, prepared); err != nil {
		return fmt.Errorf("verify workspace journal: %w", err)
	}
	return manager.verifyManagedTarget(ctx, prepared)
}

func (manager *Manager) verifyManagedTarget(ctx context.Context, prepared Prepared) error {
	resolvedTarget, err := resolveDirectory(prepared.Path)
	if err != nil || !within(prepared.root, resolvedTarget) {
		return errors.New("refusing to use workspace outside configured root")
	}
	if resolvedTarget != prepared.Path {
		return errors.New("refusing to use symlinked workspace path")
	}
	contains, err := worktreeContains(ctx, prepared.repository, prepared.Path)
	if err != nil {
		return err
	}
	if !contains {
		return fmt.Errorf("workspace target %q is not a worktree of configured repository", prepared.Path)
	}
	return nil
}

func (manager *Manager) removeKnownWorktree(ctx context.Context, prepared Prepared) error {
	if err := manager.verifyManagedTarget(ctx, prepared); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "git", "-C", prepared.repository, "worktree", "remove", "--force", prepared.Path)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("remove git worktree %q: %w: %s", prepared.Path, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (manager *Manager) removeReservation(prepared Prepared) error {
	if err := verifyOwnedFile(prepared.reservation, prepared); err != nil {
		return fmt.Errorf("verify workspace reservation before removal: %w", err)
	}
	removeFile := manager.removeFile
	if removeFile == nil {
		removeFile = os.Remove
	}
	if err := removeFile(prepared.reservation); err != nil {
		return fmt.Errorf("remove workspace reservation: %w", err)
	}
	if err := manager.syncDirectory(filepath.Dir(prepared.reservation)); err != nil {
		return fmt.Errorf("sync workspace reservation removal: %w", err)
	}
	return nil
}

func validateRun(run RunRef) error {
	if !runIDPattern.MatchString(run.RunID) {
		return errors.New("run_id must be a non-path identifier")
	}
	if run.Generation <= 0 {
		return errors.New("generation must be greater than zero")
	}
	return nil
}

func resolveDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(path)
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
	return resolved, nil
}

func resolveRoot(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	return resolveDirectory(path)
}

func ensureTargetParent(root, parent string) error {
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create directory %q: %w", parent, err)
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("resolve directory %q: %w", parent, err)
	}
	if !within(root, resolvedParent) {
		return fmt.Errorf("directory %q escapes configured root %q", resolvedParent, root)
	}
	return nil
}

func within(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

type ownership struct {
	Version    int    `json:"version"`
	BindingKey string `json:"binding_key"`
	RunID      string `json:"run_id"`
	Generation int64  `json:"generation"`
	Repository string `json:"repository"`
	Target     string `json:"target"`
}

func encodeOwnership(prepared Prepared) ([]byte, error) {
	contents, err := json.Marshal(ownership{
		Version:    1,
		BindingKey: prepared.BindingKey,
		RunID:      prepared.Run.RunID,
		Generation: prepared.Run.Generation,
		Repository: prepared.repository,
		Target:     prepared.Path,
	})
	if err != nil {
		return nil, fmt.Errorf("encode workspace ownership: %w", err)
	}
	return contents, nil
}

func verifyOwnedFile(path string, prepared Prepared) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("ownership file is missing")
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("ownership file must not be a symlink")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return verifyOwnership(contents, prepared)
}

func verifyOwnership(contents []byte, prepared Prepared) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var value ownership
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode ownership file: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("ownership file must contain one JSON object")
		}
		return fmt.Errorf("decode ownership file: %w", err)
	}
	if value.Version != 1 || value.BindingKey != prepared.BindingKey || value.RunID != prepared.Run.RunID ||
		value.Generation != prepared.Run.Generation || value.Repository != prepared.repository || value.Target != prepared.Path {
		return errors.New("ownership file does not belong to this run")
	}
	return nil
}

func writeDurableFile(path string, contents []byte, syncFile func(*os.File) error, syncParent func(string) error) error {
	parent := filepath.Dir(path)
	temporary, err := os.CreateTemp(parent, filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect temporary file: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := syncFile(temporary); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("publish temporary file: %w", err)
	}
	if err := syncParent(parent); err != nil {
		return fmt.Errorf("sync parent directory: %w", err)
	}
	return nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func worktreeContains(ctx context.Context, repository, target string) (bool, error) {
	command := exec.CommandContext(ctx, "git", "-C", repository, "worktree", "list", "--porcelain")
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, fmt.Errorf("list git worktrees for %q: %w", repository, err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		path, found := strings.CutPrefix(line, "worktree ")
		if !found {
			continue
		}
		same, err := sameDirectory(path, target)
		if err != nil {
			return false, fmt.Errorf("compare git worktree %q: %w", path, err)
		}
		if same {
			return true, nil
		}
	}
	return false, nil
}

func sameDirectory(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(right)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(leftInfo, rightInfo), nil
}
