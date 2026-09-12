package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

func TestTryDeterministicArtifactValidationQueuesExactEvidenceAndReplays(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{
		Path:          "proof.txt",
		Content:       content,
		ContentDigest: digestDeterministicArtifact(content),
	}

	result, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
	if err != nil {
		t.Fatalf("tryDeterministicArtifactValidation() error = %v", err)
	}
	if !result.Handled {
		t.Fatal("tryDeterministicArtifactValidation() returned Handled=false")
	}
	if fixture.workspace.prepareCalls != 0 || fixture.control.attachCalls != 0 {
		t.Fatalf("deterministic validation caused native side effects: prepare=%d attach=%d", fixture.workspace.prepareCalls, fixture.control.attachCalls)
	}
	if result.TaskResult.Kind != protocol.TaskResultCandidateCompletion || len(result.TaskResult.EvidenceRefs) != 1 {
		t.Fatalf("task result = %#v, want candidate_completion with one evidence ref", result.TaskResult)
	}
	evidence := result.Evidence[0]
	if evidence.ValidatorProfile == nil || *evidence.ValidatorProfile != deterministicArtifactValidatorProfile {
		t.Fatalf("top-level validator_profile = %#v, want %q", evidence.ValidatorProfile, deterministicArtifactValidatorProfile)
	}
	if evidence.EvidenceKey != deterministicArtifactEvidenceKey("artifact-proof") {
		t.Fatalf("evidence_key = %q, want deterministic bounded value", evidence.EvidenceKey)
	}
	if evidence.SourceRef.ValidatorProfile != nil {
		t.Fatalf("source_ref.validator_profile = %q, want unset", *evidence.SourceRef.ValidatorProfile)
	}
	if evidence.Subject.Commit != deterministicCommit || evidence.Payload.Commit == nil || *evidence.Payload.Commit != deterministicCommit {
		t.Fatalf("evidence commit = %#v, want exact %q", evidence, deterministicCommit)
	}
	if evidence.Payload.ContentDigest == nil || *evidence.Payload.ContentDigest != digestDeterministicArtifact(content) {
		t.Fatalf("content_digest = %#v, want %q", evidence.Payload.ContentDigest, digestDeterministicArtifact(content))
	}
	if result.Transition.State != "completed" {
		t.Fatalf("transition state = %q, want completed", result.Transition.State)
	}

	queued, err := fixture.daemon.store.LoadJournal(fixture.key)
	if err != nil {
		t.Fatal(err)
	}
	if queued.LocalState != "terminal_pending" || queued.TerminalState != "completed" {
		t.Fatalf("queued journal terminal state = %#v", queued)
	}
	if len(queued.PendingGoalDeliveries) != 1 || queued.PendingGoalDeliveries[0].Kind != state.GoalDeliveryEvidence {
		t.Fatalf("pending goal deliveries = %#v, want one evidence delivery before terminal", queued.PendingGoalDeliveries)
	}
	if queued.PendingGoalDeliveries[0].Evidence == nil || queued.PendingGoalDeliveries[0].Evidence.EvidenceID != evidence.EvidenceID {
		t.Fatalf("pending evidence = %#v, want evidence %q", queued.PendingGoalDeliveries[0].Evidence, evidence.EvidenceID)
	}
	if len(queued.PendingTransitions) != 1 || queued.PendingTransitions[0].State != "completed" {
		t.Fatalf("pending transitions = %#v, want one completed transition", queued.PendingTransitions)
	}

	replayed, err := fixture.daemon.store.QueueGoalEvidenceAndTerminalTransition(
		fixture.key,
		result.Evidence,
		result.Transition,
		fixture.now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("exact QueueGoalEvidenceAndTerminalTransition() replay error = %v", err)
	}
	if !reflect.DeepEqual(replayed, queued) {
		t.Fatalf("exact replay changed journal:\nreplayed=%#v\nqueued=%#v", replayed, queued)
	}
}

func TestTryDeterministicArtifactValidationAllowsReadOnlyChangeTargetMetadata(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	sourceBranch := "codex/goal-0006"
	targetBranch := "main"
	fixture.control.runContext.Context.WorkContract.ChangeTarget = &protocol.ProviderChangeTarget{
		Kind:         protocol.ProviderChangeTargetBranches,
		SourceBranch: &sourceBranch,
		TargetBranch: &targetBranch,
	}
	fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{
		Path: "proof.txt", Content: content, ContentDigest: digestDeterministicArtifact(content),
	}

	result, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
	if err != nil || !result.Handled {
		t.Fatalf("tryDeterministicArtifactValidation() = %#v, %v; want deterministic success with read-only target metadata", result, err)
	}
	if fixture.admission.ProviderScope != nil {
		t.Fatalf("validation admission provider scope = %#v, want nil", fixture.admission.ProviderScope)
	}
}

func TestTryDeterministicArtifactValidationPassesWireValidPathsUnchangedToReader(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	for _, test := range []struct {
		name       string
		path       string
		readErr    error
		wantHandle bool
	}{
		{name: "dot prefix", path: "./proof.txt", wantHandle: true},
		{name: "trailing slash", path: "proof.txt/", readErr: workspace.ErrArtifactNotFound},
		{name: "drive-like colon", path: "C:/proof.txt", readErr: workspace.ErrArtifactNotFound},
		{name: "pathspec-like colon", path: ":(literal)", readErr: workspace.ErrArtifactNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
				ID:         "artifact-proof",
				Kind:       "artifact",
				ResourceID: deterministicResourceID,
				Path:       test.path,
			}})
			fixture.workspace.readErr = test.readErr
			if test.readErr == nil {
				fixture.workspace.artifacts[test.path] = workspace.SubjectArtifact{
					Path: test.path, Content: content, ContentDigest: digestDeterministicArtifact(content),
				}
			}

			result, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
			if test.wantHandle {
				if err != nil || !result.Handled {
					t.Fatalf("tryDeterministicArtifactValidation() = %#v, %v; want success", result, err)
				}
				if len(result.Evidence) != 1 || result.Evidence[0].Payload.Path == nil || *result.Evidence[0].Payload.Path != test.path {
					t.Fatalf("evidence path = %#v, want raw path %q", result.Evidence[0].Payload.Path, test.path)
				}
			} else {
				if err == nil || !errors.Is(err, test.readErr) {
					t.Fatalf("tryDeterministicArtifactValidation() error = %v, want %v", err, test.readErr)
				}
				if result.Handled {
					t.Fatal("reader-reported artifact failure was handled as success")
				}
			}
			if len(fixture.workspace.readPaths) != 1 || fixture.workspace.readPaths[0] != test.path {
				t.Fatalf("reader paths = %#v, want raw path %q", fixture.workspace.readPaths, test.path)
			}
		})
	}
}

func TestTryDeterministicArtifactValidationDefersNonArtifactToNativePath(t *testing.T) {
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:               "tests",
		Kind:             "check",
		ValidatorProfile: "default-checks",
	}})

	result, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
	if err != nil || result.Handled {
		t.Fatalf("tryDeterministicArtifactValidation() = %#v, %v; want an unhandled native-path validation", result, err)
	}
	assertDeterministicValidationUnchanged(t, fixture)
	if fixture.workspace.readCalls != 0 || fixture.workspace.prepareCalls != 0 || fixture.control.attachCalls != 0 {
		t.Fatalf("non-artifact validation caused side effects: reads=%d prepare=%d attach=%d", fixture.workspace.readCalls, fixture.workspace.prepareCalls, fixture.control.attachCalls)
	}
}

func TestTryDeterministicArtifactValidationRejectsNonFreshArtifactAdmission(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	sessionID := deterministicSessionID
	bindingID := "00000000-0000-4000-8000-000000000014"
	fixture.admission.SessionMode = protocol.SessionModeResume
	fixture.admission.RequestedSessionID = &sessionID
	fixture.claim.HarnessSessionID = &sessionID
	fixture.claim.HarnessBindingID = &bindingID
	fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{
		Path: "proof.txt", Content: content, ContentDigest: digestDeterministicArtifact(content),
	}

	_, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
	if err == nil || !strings.Contains(err.Error(), "requires a fresh admission") {
		t.Fatalf("tryDeterministicArtifactValidation() error = %v, want non-fresh rejection", err)
	}
	assertDeterministicValidationUnchanged(t, fixture)
	if fixture.workspace.readCalls != 0 || fixture.workspace.prepareCalls != 0 || fixture.control.attachCalls != 0 {
		t.Fatalf("non-fresh artifact validation caused side effects: reads=%d prepare=%d attach=%d", fixture.workspace.readCalls, fixture.workspace.prepareCalls, fixture.control.attachCalls)
	}
}

func TestTryDeterministicArtifactValidationUsesBoundedKeyForLongPredicateID(t *testing.T) {
	predicateID := strings.Repeat("a", 128)
	content := []byte("committed proof at the admitted commit\n")
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         predicateID,
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{Path: "proof.txt", Content: content, ContentDigest: digestDeterministicArtifact(content)}
	result, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
	if err != nil || !result.Handled || len(result.Evidence) != 1 {
		t.Fatalf("tryDeterministicArtifactValidation() = %#v, %v", result, err)
	}
	if len(result.Evidence[0].EvidenceKey) > 128 || result.Evidence[0].EvidenceKey != deterministicArtifactEvidenceKey(predicateID) {
		t.Fatalf("evidence_key = %q, want stable bounded artifact key", result.Evidence[0].EvidenceKey)
	}
}

func TestDeterministicArtifactValidationRejectsCancelledOrExpiredRun(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	for _, test := range []struct {
		name   string
		mutate func(*deterministicValidationFixture)
	}{
		{name: "cancelled", mutate: func(fixture *deterministicValidationFixture) {
			fixture.daemon.running = map[state.RunKey]*runningRun{fixture.key: {cancelled: true}}
		}},
		{name: "stale", mutate: func(fixture *deterministicValidationFixture) {
			fixture.daemon.running[fixture.key].stale = true
		}},
		{name: "expired", mutate: func(fixture *deterministicValidationFixture) {
			fixture.claim.LeaseExpiresAt = fixture.now.Add(-time.Second)
			_, _ = fixture.store.SaveClaimGrant(fixture.key, fixture.claim)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{ID: "artifact-proof", Kind: "artifact", ResourceID: deterministicResourceID, Path: "proof.txt"}})
			fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{Path: "proof.txt", Content: content, ContentDigest: digestDeterministicArtifact(content)}
			test.mutate(fixture)
			_, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
			if err == nil || !strings.Contains(err.Error(), "before terminal evidence could be queued") {
				t.Fatalf("error = %v, want execution fence rejection", err)
			}
			assertDeterministicValidationUnchanged(t, fixture)
		})
	}
}

func TestDeterministicArtifactValidationRejectsCancellationAfterArtifactRead(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{
		Path: "proof.txt", Content: content, ContentDigest: digestDeterministicArtifact(content),
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	fixture.workspace.readEntered = entered
	fixture.workspace.readRelease = release

	type validationResult struct {
		result deterministicArtifactValidationResult
		err    error
	}
	done := make(chan validationResult, 1)
	go func() {
		result, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
		done <- validationResult{result: result, err: err}
	}()
	<-entered
	fixture.daemon.mu.Lock()
	fixture.daemon.running[fixture.key].cancelled = true
	fixture.daemon.mu.Unlock()
	close(release)

	completed := <-done
	if completed.err == nil || !strings.Contains(completed.err.Error(), "before terminal evidence could be queued") {
		t.Fatalf("tryDeterministicArtifactValidation() error = %v, want cancellation fence rejection", completed.err)
	}
	if completed.result.Handled {
		t.Fatal("cancelled deterministic validation was handled as success")
	}
	assertDeterministicValidationUnchanged(t, fixture)
}

func TestDeterministicArtifactValidationStopsGitReadOnContextCancellation(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{
		Path: "proof.txt", Content: content, ContentDigest: digestDeterministicArtifact(content),
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	fixture.workspace.readEntered = entered
	fixture.workspace.readRelease = release

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := fixture.daemon.tryDeterministicArtifactValidation(ctx, fixture.key, fixture.claim, fixture.admission)
		done <- err
	}()
	<-entered
	cancel()

	err := <-done
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("tryDeterministicArtifactValidation() error = %v, want context.Canceled", err)
	}
	assertDeterministicValidationUnchanged(t, fixture)
}

func TestDeterministicArtifactValidationRejectsExpiredAdmissionBeforeArtifactRead(t *testing.T) {
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	fixture.admission.Limits.DeadlineAt = fixture.now.Add(-time.Second).Format(time.RFC3339Nano)
	if _, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission); err == nil || !strings.Contains(err.Error(), "deadline has elapsed") {
		t.Fatalf("error = %v, want expired admission rejection", err)
	}
	assertDeterministicValidationUnchanged(t, fixture)
	if fixture.workspace.readCalls != 0 || fixture.control.contextCalls != 0 {
		t.Fatalf("expired admission caused artifact or control read: artifacts=%d context=%d", fixture.workspace.readCalls, fixture.control.contextCalls)
	}
}

func TestDeterministicArtifactValidationBoundsIOToAdmissionDeadline(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	deadline := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	fixture.admission.Limits.DeadlineAt = deadline.Format(time.RFC3339Nano)
	fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{Path: "proof.txt", Content: content, ContentDigest: digestDeterministicArtifact(content)}
	result, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
	if err != nil || !result.Handled {
		t.Fatalf("tryDeterministicArtifactValidation() = %#v, %v", result, err)
	}
	if fixture.control.contextDeadline.IsZero() || !fixture.control.contextDeadline.Equal(deadline) {
		t.Fatalf("FetchRunContext deadline = %s, want %s", fixture.control.contextDeadline, deadline)
	}
	if fixture.workspace.contextDeadline.IsZero() || !fixture.workspace.contextDeadline.Equal(deadline) {
		t.Fatalf("ReadSubjectArtifact deadline = %s, want %s", fixture.workspace.contextDeadline, deadline)
	}
}

func TestDeterministicArtifactValidationDoesNotForgeValidationFailureReason(t *testing.T) {
	err := deterministicValidationError("artifact", workspace.ErrArtifactNotFound)
	if reason, ok := taskResultFailureReason(err); ok {
		t.Fatalf("deterministic artifact error was classified as task-result reason %q", reason)
	}
}

func TestTryDeterministicArtifactValidationRejectsMissingArtifactWithoutTerminalMutation(t *testing.T) {
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "missing.txt",
	}})
	fixture.workspace.readErr = workspace.ErrArtifactNotFound

	_, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
	if err == nil || !errors.Is(err, workspace.ErrArtifactNotFound) {
		t.Fatalf("error = %v, want ErrArtifactNotFound", err)
	}
	assertDeterministicValidationUnchanged(t, fixture)
	if fixture.workspace.prepareCalls != 0 || fixture.control.attachCalls != 0 {
		t.Fatalf("missing artifact caused native side effects: prepare=%d attach=%d", fixture.workspace.prepareCalls, fixture.control.attachCalls)
	}
}

func TestTryDeterministicArtifactValidationRejectsUnsafePathAndInvalidContext(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*deterministicValidationFixture)
		want   string
	}{
		{
			name: "unsafe path",
			mutate: func(fixture *deterministicValidationFixture) {
				fixture.control.runContext.Context.WorkContract.Acceptance.Predicates[0].Path = "../proof.txt"
			},
			want: "artifact path",
		},
		{
			name: "double slash",
			mutate: func(fixture *deterministicValidationFixture) {
				fixture.control.runContext.Context.WorkContract.Acceptance.Predicates[0].Path = "proof//txt"
			},
			want: "artifact path",
		},
		{
			name: "backslash",
			mutate: func(fixture *deterministicValidationFixture) {
				fixture.control.runContext.Context.WorkContract.Acceptance.Predicates[0].Path = "proof\\txt"
			},
			want: "artifact path",
		},
		{
			name: "NUL",
			mutate: func(fixture *deterministicValidationFixture) {
				fixture.control.runContext.Context.WorkContract.Acceptance.Predicates[0].Path = "proof\x00txt"
			},
			want: "artifact path",
		},
		{
			name: "session id present",
			mutate: func(fixture *deterministicValidationFixture) {
				sessionID := deterministicSessionID
				fixture.control.runContext.SessionID = &sessionID
			},
			want: "must not contain a session_id",
		},
		{
			name: "subject mismatch",
			mutate: func(fixture *deterministicValidationFixture) {
				fixture.control.runContext.Context.Subject.Commit = strings.Repeat("b", 40)
			},
			want: "does not match admission",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
				ID:         "artifact-proof",
				Kind:       "artifact",
				ResourceID: deterministicResourceID,
				Path:       "proof.txt",
			}})
			test.mutate(fixture)

			_, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			assertDeterministicValidationUnchanged(t, fixture)
			if fixture.workspace.readCalls != 0 || fixture.workspace.prepareCalls != 0 || fixture.control.attachCalls != 0 {
				t.Fatalf("invalid validation caused side effects: reads=%d prepare=%d attach=%d", fixture.workspace.readCalls, fixture.workspace.prepareCalls, fixture.control.attachCalls)
			}
		})
	}
}

func TestStartGoalAdmissionCompletesArtifactOnlyValidationWithoutNativeLaunch(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{
		Path:          "proof.txt",
		Content:       content,
		ContentDigest: digestDeterministicArtifact(content),
	}
	fixture.daemon.running = map[state.RunKey]*runningRun{
		fixture.key: {starting: true, slotHeld: true, cleanupBlocked: true},
	}
	fixture.daemon.slots = make(chan struct{}, 1)
	fixture.daemon.slots <- struct{}{}
	workspacePersisted := false

	if err := fixture.daemon.startGoalAdmission(context.Background(), fixture.key, fixture.claim, fixture.admission, func(workspace.Prepared) {
		workspacePersisted = true
	}); err != nil {
		t.Fatalf("startGoalAdmission() error = %v", err)
	}
	active := fixture.daemon.running[fixture.key]
	if active == nil || active.starting || !active.terminal || active.cleanupBlocked {
		t.Fatalf("deterministic admission active state = %#v, want terminal non-starting cleanable run", active)
	}
	if workspacePersisted || fixture.workspace.prepareCalls != 0 || fixture.control.attachCalls != 0 {
		t.Fatalf("deterministic admission caused native launch side effects: persisted=%t prepare=%d attach=%d", workspacePersisted, fixture.workspace.prepareCalls, fixture.control.attachCalls)
	}
	journal, err := fixture.store.LoadJournal(fixture.key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || len(journal.PendingGoalDeliveries) != 1 || len(journal.PendingTransitions) != 1 {
		t.Fatalf("deterministic admission journal = %#v", journal)
	}
}

func TestFlushRunRefreshesArtifactEvidenceBeforeConcurrentTerminal(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	source := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	source.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{
		Path:          "proof.txt",
		Content:       content,
		ContentDigest: digestDeterministicArtifact(content),
	}
	result, err := source.daemon.tryDeterministicArtifactValidation(context.Background(), source.key, source.claim, source.admission)
	if err != nil || !result.Handled {
		t.Fatalf("prepare deterministic result = %#v, %v", result, err)
	}

	target := newDeterministicValidationFixture(t, nil)
	if _, err := target.store.QueueTransition(target.key, protocol.StateTransitionRequest{TransitionID: "running-transition", State: "running", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	outbox := &deterministicValidationOutboxControl{
		goalDeliveryControl: &goalDeliveryControl{fakeControl: &fakeControl{}},
		afterRunning: func() error {
			_, queueErr := target.store.QueueGoalEvidenceAndTerminalTransition(target.key, result.Evidence, result.Transition, target.now)
			return queueErr
		},
	}
	target.daemon.control = outbox
	journal, err := target.store.LoadJournal(target.key)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatalf("flushRun() error = %v", err)
	}
	if got, want := outbox.calls, []string{"transition:running", "evidence:" + result.Evidence[0].EvidenceKey, "transition:completed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("outbox call order = %#v, want %#v", got, want)
	}
}

func TestTryDeterministicArtifactValidationRejectsProviderAccessBeforeControlRead(t *testing.T) {
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	fixture.claim.ProviderAccess = &protocol.ProviderAccess{Path: "/provider-actions", Token: "secret"}

	_, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
	if err == nil || !strings.Contains(err.Error(), "does not allow provider access") {
		t.Fatalf("error = %v, want provider access rejection", err)
	}
	if fixture.control.contextCalls != 0 || fixture.workspace.readCalls != 0 {
		t.Fatalf("provider access rejection performed control or artifact I/O: context=%d reads=%d", fixture.control.contextCalls, fixture.workspace.readCalls)
	}
	assertDeterministicValidationUnchanged(t, fixture)
}

func TestTryDeterministicArtifactValidationRejectsExistingNativeExecution(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	for _, test := range []struct {
		name   string
		mutate func(*runningRun)
	}{
		{name: "process", mutate: func(active *runningRun) { active.process = &fakeProcess{} }},
		{name: "native session", mutate: func(active *runningRun) { active.nativeSession = &fakeNativeGoalSession{} }},
		{name: "competing terminal", mutate: func(active *runningRun) { active.terminalizing = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
				ID:         "artifact-proof",
				Kind:       "artifact",
				ResourceID: deterministicResourceID,
				Path:       "proof.txt",
			}})
			fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{
				Path: "proof.txt", Content: content, ContentDigest: digestDeterministicArtifact(content),
			}
			test.mutate(fixture.daemon.running[fixture.key])

			result, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
			if err == nil || result.Handled {
				t.Fatalf("result = %#v, error = %v; want fail-closed rejection", result, err)
			}
			if fixture.control.contextCalls != 0 || fixture.workspace.readCalls != 0 {
				t.Fatalf("invalid active state performed control or artifact I/O: context=%d reads=%d", fixture.control.contextCalls, fixture.workspace.readCalls)
			}
			assertDeterministicValidationUnchanged(t, fixture)
		})
	}
}

func TestTryDeterministicArtifactValidationUsesFinalTerminalPendingTime(t *testing.T) {
	content := []byte("committed proof at the admitted commit\n")
	fixture := newDeterministicValidationFixture(t, []control.AcceptancePredicate{{
		ID:         "artifact-proof",
		Kind:       "artifact",
		ResourceID: deterministicResourceID,
		Path:       "proof.txt",
	}})
	fixture.workspace.artifacts["proof.txt"] = workspace.SubjectArtifact{
		Path: "proof.txt", Content: content, ContentDigest: digestDeterministicArtifact(content),
	}
	current := fixture.now
	fixture.daemon.options.clock = func() time.Time { return current }
	finalPendingAt := fixture.now.Add(2 * time.Minute)
	fixture.workspace.afterRead = func() { current = finalPendingAt }

	result, err := fixture.daemon.tryDeterministicArtifactValidation(context.Background(), fixture.key, fixture.claim, fixture.admission)
	if err != nil || !result.Handled {
		t.Fatalf("tryDeterministicArtifactValidation() = %#v, %v; want success", result, err)
	}
	journal, err := fixture.store.LoadJournal(fixture.key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.TerminalPendingAt.Equal(finalPendingAt) {
		t.Fatalf("terminal_pending_at = %s, want final queue time %s", journal.TerminalPendingAt, finalPendingAt)
	}
	observedAt, err := time.Parse(time.RFC3339Nano, result.Evidence[0].ObservedAt)
	if err != nil {
		t.Fatalf("parse evidence observed_at: %v", err)
	}
	if !observedAt.Equal(fixture.now) {
		t.Fatalf("evidence observed_at = %s, want artifact observation time %s", observedAt, fixture.now)
	}
}

const (
	deterministicRunID       = "00000000-0000-4000-8000-000000000001"
	deterministicTaskID      = "00000000-0000-4000-8000-000000000002"
	deterministicProducerID  = "00000000-0000-4000-8000-000000000003"
	deterministicAdmissionID = "00000000-0000-4000-8000-000000000004"
	deterministicGoalID      = "00000000-0000-4000-8000-000000000005"
	deterministicWorkItemID  = "00000000-0000-4000-8000-000000000006"
	deterministicSnapshotID  = "00000000-0000-4000-8000-000000000007"
	deterministicResourceID  = "00000000-0000-4000-8000-000000000008"
	deterministicClaimID     = "00000000-0000-4000-8000-000000000009"
	deterministicSessionID   = "00000000-0000-4000-8000-000000000010"
	deterministicCommit      = "0123456789abcdef0123456789abcdef01234567"
	deterministicHash        = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

type deterministicValidationFixture struct {
	daemon    *daemon
	store     *state.Store
	control   *deterministicValidationControl
	workspace *deterministicValidationWorkspace
	key       state.RunKey
	claim     protocol.ClaimResponse
	admission protocol.Admission
	now       time.Time
}

func newDeterministicValidationFixture(t *testing.T, predicates []control.AcceptancePredicate) *deterministicValidationFixture {
	t.Helper()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	admissionDeadline := time.Now().UTC().Add(time.Hour)
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	key := state.RunKey{RunID: deterministicRunID, Generation: 1}
	subject := protocol.Subject{ResourceID: deterministicResourceID, Commit: deterministicCommit, TreeDigest: deterministicHash}
	workItemID := deterministicWorkItemID
	admission := protocol.Admission{
		SchemaVersion:     protocol.AdmissionSchemaVersion,
		AdmissionID:       deterministicAdmissionID,
		GoalID:            deterministicGoalID,
		GoalRevision:      1,
		WorkItemID:        &workItemID,
		Purpose:           protocol.AdmissionPurposeValidate,
		ContextSnapshotID: deterministicSnapshotID,
		ContextHash:       deterministicHash,
		ModelProfile:      "agent",
		SessionMode:       protocol.SessionModeFresh,
		Subject:           subject,
		Limits: protocol.AdmissionLimits{
			MaxTurns:   1,
			DeadlineAt: admissionDeadline.Format(time.RFC3339Nano),
		},
		ValidationOfTaskID: stringPointer(deterministicProducerID),
	}
	admissionInput, err := json.Marshal(admission)
	if err != nil {
		t.Fatal(err)
	}
	work := protocol.Work{Goal: "validate the admitted artifact", Input: admissionInput, AgentProfile: "agent", Workspace: "primary"}
	claim := protocol.ClaimResponse{
		RunID:          deterministicRunID,
		TaskID:         deterministicTaskID,
		Generation:     1,
		ClaimID:        deterministicClaimID,
		LeaseToken:     "lease-token",
		LeaseExpiresAt: now.Add(time.Hour),
		Work:           work,
	}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{
		Key:                 key,
		RuntimeKey:          "runtime",
		RuntimeID:           "runtime-1",
		RuntimeEpoch:        1,
		ClaimID:             deterministicClaimID,
		LocalState:          "claiming",
		Work:                work,
		WorkspaceBindingKey: "primary",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(key, claim); err != nil {
		t.Fatal(err)
	}

	controlClient := &deterministicValidationControl{fakeControl: &fakeControl{}}
	controlClient.runContext = control.GoalRunContext{
		GoalID:     deterministicGoalID,
		TaskID:     deterministicTaskID,
		RunID:      deterministicRunID,
		Generation: 1,
		Context: control.GoalContextSnapshot{
			SnapshotID:   deterministicSnapshotID,
			GoalID:       deterministicGoalID,
			GoalRevision: 1,
			WorkItemID:   &workItemID,
			ContentHash:  deterministicHash,
			Subject:      subject,
			WorkContract: control.WorkContract{
				Purpose: string(protocol.AdmissionPurposeValidate),
				Acceptance: control.AcceptanceContract{
					Predicates: predicates,
				},
			},
		},
	}
	workspaceClient := &deterministicValidationWorkspace{artifacts: make(map[string]workspace.SubjectArtifact)}
	idValues := []string{
		"00000000-0000-4000-8000-000000000011",
		"00000000-0000-4000-8000-000000000012",
		"00000000-0000-4000-8000-000000000013",
	}
	idIndex := 0
	daemonClient := &daemon{
		config:    configForDeterministicValidation(),
		store:     store,
		control:   controlClient,
		workspace: workspaceClient,
		running:   map[state.RunKey]*runningRun{key: {}},
		options: options{
			clock: func() time.Time { return now },
			newID: func() (string, error) {
				if idIndex >= len(idValues) {
					return "", errors.New("deterministic test IDs exhausted")
				}
				value := idValues[idIndex]
				idIndex++
				return value, nil
			},
		},
	}
	return &deterministicValidationFixture{
		daemon:    daemonClient,
		store:     store,
		control:   controlClient,
		workspace: workspaceClient,
		key:       key,
		claim:     claim,
		admission: admission,
		now:       now,
	}
}

func assertDeterministicValidationUnchanged(t *testing.T, fixture *deterministicValidationFixture) {
	t.Helper()
	journal, err := fixture.store.LoadJournal(fixture.key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "claimed" || journal.TerminalState != "" || len(journal.PendingGoalDeliveries) != 0 || len(journal.PendingTransitions) != 0 {
		t.Fatalf("failed deterministic validation mutated journal: %#v", journal)
	}
}

type deterministicValidationControl struct {
	*fakeControl
	runContext      control.GoalRunContext
	contextCalls    int
	contextDeadline time.Time
	attachCalls     int
}

func (client *deterministicValidationControl) FetchRunContext(ctx context.Context, _ string, _ protocol.Fence) (control.GoalRunContext, error) {
	client.contextCalls++
	client.contextDeadline, _ = ctx.Deadline()
	return client.runContext, nil
}

func (client *deterministicValidationControl) AttachHarnessSession(context.Context, string, control.GoalSessionAttachRequest) (control.GoalSessionReceipt, error) {
	client.attachCalls++
	return control.GoalSessionReceipt{}, nil
}

func (*deterministicValidationControl) AppendEvidence(context.Context, string, protocol.Fence, protocol.Evidence) (control.GoalEvidenceReceipt, error) {
	return control.GoalEvidenceReceipt{}, nil
}

func (*deterministicValidationControl) RecordUsage(context.Context, string, protocol.Fence, protocol.Usage) (control.GoalUsageReceipt, error) {
	return control.GoalUsageReceipt{}, nil
}

type deterministicValidationWorkspace struct {
	artifacts       map[string]workspace.SubjectArtifact
	readErr         error
	afterRead       func()
	readCalls       int
	readPaths       []string
	contextDeadline time.Time
	readEntered     chan struct{}
	readRelease     chan struct{}
	prepareCalls    int
}

func (service *deterministicValidationWorkspace) ReadSubjectArtifact(ctx context.Context, _ string, _ protocol.Subject, path string) (workspace.SubjectArtifact, error) {
	service.readCalls++
	service.readPaths = append(service.readPaths, path)
	service.contextDeadline, _ = ctx.Deadline()
	if service.readEntered != nil {
		close(service.readEntered)
		service.readEntered = nil
	}
	if service.readRelease != nil {
		select {
		case <-service.readRelease:
		case <-ctx.Done():
			return workspace.SubjectArtifact{}, ctx.Err()
		}
		service.readRelease = nil
	}
	if service.readErr != nil {
		return workspace.SubjectArtifact{}, service.readErr
	}
	artifact, ok := service.artifacts[path]
	if !ok {
		return workspace.SubjectArtifact{}, workspace.ErrArtifactNotFound
	}
	if service.afterRead != nil {
		afterRead := service.afterRead
		service.afterRead = nil
		afterRead()
	}
	return artifact, nil
}

func (service *deterministicValidationWorkspace) Prepare(_ context.Context, bindingKey string, run workspace.RunRef) (workspace.Prepared, error) {
	service.prepareCalls++
	return workspace.Prepared{Path: "C:\\unused", BindingKey: bindingKey, Run: run}, nil
}

func (*deterministicValidationWorkspace) Recover(context.Context, string, workspace.RunRef, string) (workspace.Prepared, error) {
	return workspace.Prepared{}, fmt.Errorf("unexpected Recover")
}

func (*deterministicValidationWorkspace) Cleanup(context.Context, workspace.Prepared, bool) error {
	return nil
}

func configForDeterministicValidation() config.Config {
	return config.Config{
		Runtime: config.Runtime{Workspace: "primary", AgentProfile: "agent", RepositoryResourceID: deterministicResourceID},
		AgentProfiles: map[string]config.AgentProfile{
			"agent": {},
		},
	}
}

func stringPointer(value string) *string {
	return &value
}

var _ workspace.Service = (*deterministicValidationWorkspace)(nil)
var _ goalControlAPI = (*deterministicValidationControl)(nil)

type deterministicValidationOutboxControl struct {
	*goalDeliveryControl
	calls        []string
	afterRunning func() error
}

func (client *deterministicValidationOutboxControl) AppendEvidence(ctx context.Context, runID string, fence protocol.Fence, evidence protocol.Evidence) (control.GoalEvidenceReceipt, error) {
	client.calls = append(client.calls, "evidence:"+evidence.EvidenceKey)
	return client.goalDeliveryControl.AppendEvidence(ctx, runID, fence, evidence)
}

func (client *deterministicValidationOutboxControl) Transition(_ context.Context, _ string, transition protocol.StateTransitionRequest) error {
	client.calls = append(client.calls, "transition:"+transition.State)
	if transition.State == "running" && client.afterRunning != nil {
		callback := client.afterRunning
		client.afterRunning = nil
		return callback()
	}
	return nil
}
