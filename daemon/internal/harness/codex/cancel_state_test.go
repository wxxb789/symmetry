package codex

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

type cancelViolationRecord struct {
	transition cancelTransition
	attemptID  uint64
}

func recordCancelViolations(t *testing.T) *[]cancelViolationRecord {
	t.Helper()
	records := &[]cancelViolationRecord{}
	previous := cancelViolationHook
	cancelViolationHook = func(transition cancelTransition, attemptID uint64) {
		*records = append(*records, cancelViolationRecord{transition: transition, attemptID: attemptID})
	}
	t.Cleanup(func() {
		cancelViolationHook = previous
	})
	return records
}

func interruptedTerminal() *pendingInterruptedTerminal {
	return newPendingInterruptedTerminal(
		Frame{Kind: FrameNotification, Sequence: 7, Method: appServerEventTurnCompleted},
		turnCompletedParams{ThreadID: "thread-1", Turn: nativeTurn{ID: "turn-1", Status: "interrupted"}},
		protocol.TaskResult{},
		nil,
		errors.New("terminal turn contains no final agentMessage TaskResult"),
	)
}

func TestCancelAttemptAcquireSharesOneFlight(t *testing.T) {
	tracker := newCancelTracker()
	firstID, owner, firstDone := tracker.acquire()
	if !owner || firstID != 1 || tracker.nextCancelAttempt != 1 {
		t.Fatalf("first acquire = (%d, %v), want the first owned flight", firstID, owner)
	}
	secondID, owner, secondDone := tracker.acquire()
	if owner || secondID != firstID || secondDone != firstDone {
		t.Fatalf("second acquire = (%d, %v), want the shared flight", secondID, owner)
	}
	if len(tracker.cancelAttempts) != 1 || tracker.cancelFlight == nil || tracker.cancelFlight.id != firstID {
		t.Fatalf("flight state = attempts=%d flight=%+v", len(tracker.cancelAttempts), tracker.cancelFlight)
	}
	tracker.markWritten(firstID)
	tracker.finish(firstID, nil)
	if tracker.cancelFlight != nil {
		t.Fatalf("finish kept the shared flight %+v", tracker.cancelFlight)
	}
	thirdID, owner, _ := tracker.acquire()
	if !owner || thirdID != 2 {
		t.Fatalf("third acquire = (%d, %v), want a new owned flight", thirdID, owner)
	}
}

func TestCancelAttemptStateTransitions(t *testing.T) {
	t.Run("written then accepted", func(t *testing.T) {
		tracker := newCancelTracker()
		id, _, _ := tracker.acquire()
		tracker.markWritten(id)
		if got := tracker.cancelAttempts[id].state; got != cancelAttemptWritten {
			t.Fatalf("state after write = %d, want written", got)
		}
		if !tracker.requested() {
			t.Fatal("a written attempt did not request cancellation")
		}
		tracker.accept(id)
		if got := tracker.cancelAttempts[id].state; got != cancelAttemptAccepted {
			t.Fatalf("state after accept = %d, want accepted", got)
		}
		if !tracker.requested() {
			t.Fatal("an accepted attempt did not request cancellation")
		}
	})

	t.Run("written then rejected", func(t *testing.T) {
		tracker := newCancelTracker()
		id, _, _ := tracker.acquire()
		tracker.markWritten(id)
		terminal, kind := tracker.reject(id)
		if terminal != nil || kind != harness.ResultFailed {
			t.Fatalf("reject = (%+v, %v), want no terminal", terminal, kind)
		}
		if got := tracker.cancelAttempts[id].state; got != cancelAttemptFailed {
			t.Fatalf("state after reject = %d, want failed", got)
		}
		if tracker.requested() {
			t.Fatal("a rejected attempt still requested cancellation")
		}
	})

	t.Run("intent fails on write failure", func(t *testing.T) {
		tracker := newCancelTracker()
		id, _, _ := tracker.acquire()
		tracker.fail(id)
		if got := tracker.cancelAttempts[id].state; got != cancelAttemptFailed {
			t.Fatalf("state after fail = %d, want failed", got)
		}
		if tracker.requested() {
			t.Fatal("a failed intent still requested cancellation")
		}
	})

	t.Run("written attempt keeps its write on failure", func(t *testing.T) {
		tracker := newCancelTracker()
		id, _, _ := tracker.acquire()
		tracker.markWritten(id)
		tracker.fail(id)
		if got := tracker.cancelAttempts[id].state; got != cancelAttemptWritten {
			t.Fatalf("state after fail = %d, want the retained written state", got)
		}
		if !tracker.requested() {
			t.Fatal("a written-but-unanswered interrupt stopped requesting cancellation")
		}
	})

	t.Run("finish settles once", func(t *testing.T) {
		tracker := newCancelTracker()
		id, _, done := tracker.acquire()
		tracker.finish(id, context.DeadlineExceeded)
		attempt := tracker.cancelAttempts[id]
		if attempt.state != cancelAttemptFailed || !errors.Is(attempt.err, context.DeadlineExceeded) {
			t.Fatalf("finished attempt = %+v, want the failed intent with its error", attempt)
		}
		select {
		case <-done:
		default:
			t.Fatal("finish did not release the shared waiter")
		}
		if tracker.cancelFlight != nil {
			t.Fatalf("finish kept the shared flight %+v", tracker.cancelFlight)
		}
		tracker.finish(id, context.Canceled)
		if !errors.Is(attempt.err, context.DeadlineExceeded) {
			t.Fatalf("second finish overwrote the settled error %v", attempt.err)
		}
	})

	t.Run("finished written attempt still classifies", func(t *testing.T) {
		tracker := newCancelTracker()
		id, _, done := tracker.acquire()
		tracker.markWritten(id)
		tracker.finish(id, context.DeadlineExceeded)
		if got := tracker.cancelAttempts[id].state; got != cancelAttemptWritten {
			t.Fatalf("state after finish = %d, want written", got)
		}
		select {
		case <-done:
		default:
			t.Fatal("finish did not release the shared waiter")
		}
		if !tracker.requested() {
			t.Fatal("a written attempt lost its cancellation evidence after finish")
		}
	})
}

func TestCancelDeferredTerminalTakeOnce(t *testing.T) {
	t.Run("intent defers as failed", func(t *testing.T) {
		tracker := newCancelTracker()
		id, _, _ := tracker.acquire()
		tracker.bindRPC(11)
		terminal := interruptedTerminal()
		if !tracker.deferTerminal(terminal) {
			t.Fatal("bound interrupt intent did not defer the interrupted terminal")
		}
		if terminal.attemptID != id {
			t.Fatalf("deferred attempt id = %d, want %d", terminal.attemptID, id)
		}
		if !tracker.cancelAttempts[id].terminalInterrupted {
			t.Fatal("deferred terminal did not mark the attempt")
		}
		got, kind := tracker.takeDeferredTerminal(id)
		if got != terminal || kind != harness.ResultFailed {
			t.Fatalf("take = (%+v, %v), want the terminal unclassified", got, kind)
		}
		if again, _ := tracker.takeDeferredTerminal(id); again != nil {
			t.Fatalf("second take returned %+v, want take-once", again)
		}
	})

	t.Run("written defers as cancelled", func(t *testing.T) {
		tracker := newCancelTracker()
		id, _, _ := tracker.acquire()
		tracker.bindRPC(12)
		tracker.markWritten(id)
		terminal := interruptedTerminal()
		if !tracker.deferTerminal(terminal) {
			t.Fatal("bound written interrupt did not defer the interrupted terminal")
		}
		got, kind := tracker.takeDeferredTerminal(id)
		if got != terminal || kind != harness.ResultCancelled {
			t.Fatalf("take = (%+v, %v), want cancelled from the written attempt", got, kind)
		}
	})

	t.Run("unbound interrupt does not defer", func(t *testing.T) {
		tracker := newCancelTracker()
		tracker.acquire()
		if tracker.deferTerminal(interruptedTerminal()) {
			t.Fatal("an unbound interrupt deferred the interrupted terminal")
		}
	})

	t.Run("observed response does not defer", func(t *testing.T) {
		tracker := newCancelTracker()
		id, _, _ := tracker.acquire()
		tracker.bindRPC(15)
		tracker.markWritten(id)
		tracker.observeResponse(id)
		if tracker.deferTerminal(interruptedTerminal()) {
			t.Fatal("an attempt whose interrupt response was already observed deferred the terminal")
		}
		if tracker.deferredTerminal != nil {
			t.Fatalf("deferred terminal = %+v, want none", tracker.deferredTerminal)
		}
	})

	t.Run("newest bound attempt wins the fallback", func(t *testing.T) {
		tracker := newCancelTracker()
		older, _, _ := tracker.acquire()
		tracker.bindRPC(16)
		tracker.markWritten(older)
		tracker.finish(older, context.DeadlineExceeded)
		newer, _, _ := tracker.acquire()
		tracker.bindRPC(17)
		tracker.markWritten(newer)
		tracker.finish(newer, context.DeadlineExceeded)
		tracker.acquire()
		terminal := interruptedTerminal()
		if !tracker.deferTerminal(terminal) {
			t.Fatal("two written attempts with unanswered interrupts did not defer")
		}
		if terminal.attemptID != newer {
			t.Fatalf("deferred attempt id = %d, want the newest eligible attempt %d", terminal.attemptID, newer)
		}
	})

	t.Run("settled written attempt beats the unbound flight", func(t *testing.T) {
		tracker := newCancelTracker()
		older, _, _ := tracker.acquire()
		tracker.bindRPC(13)
		tracker.markWritten(older)
		tracker.finish(older, context.DeadlineExceeded)
		newer, _, _ := tracker.acquire()
		terminal := interruptedTerminal()
		if !tracker.deferTerminal(terminal) {
			t.Fatal("a written attempt with an unanswered interrupt did not defer")
		}
		if terminal.attemptID != older || terminal.attemptID == newer {
			t.Fatalf("deferred attempt id = %d, want the older bound attempt %d", terminal.attemptID, older)
		}
	})
}

func TestCancelRPCBindingsTrackAttempts(t *testing.T) {
	tracker := newCancelTracker()
	if binding := tracker.forgetRPC(3); binding != 0 {
		t.Fatalf("unbound rpc id forget = %d, want 0", binding)
	}
	tracker.bindRPC(3)
	if binding := tracker.forgetRPC(3); binding != 0 {
		t.Fatalf("bindRPC without a flight bound attempt %d", binding)
	}
	id, _, _ := tracker.acquire()
	tracker.bindRPC(3)
	if binding := tracker.forgetRPC(3); binding != id {
		t.Fatalf("bound attempt = %d, want %d", binding, id)
	}
	if binding := tracker.forgetRPC(3); binding != 0 {
		t.Fatalf("second forget = %d, want the binding dropped once", binding)
	}
	if tracker.cancelAttempts[id].rpcID != 3 {
		t.Fatalf("attempt rpc id = %d, want 3", tracker.cancelAttempts[id].rpcID)
	}
	tracker.observeResponse(id)
	if !tracker.cancelAttempts[id].responseObserved {
		t.Fatal("observeResponse did not record the answer")
	}
	tracker.observeResponse(4242)
	if !tracker.cancelAttempts[id].responseObserved {
		t.Fatal("observeResponse overwrote an existing record")
	}
}

func TestCancelResolvePendingInterrupt(t *testing.T) {
	cases := []struct {
		name        string
		rpcID       uint64
		bound       bool
		apply       func(tracker *cancelTracker, id uint64)
		status      string
		wantOutcome interruptRPCOutcome
		wantMarked  bool
	}{
		{name: "intent suspends", rpcID: 21, bound: true, apply: func(*cancelTracker, uint64) {}, status: "interrupted", wantOutcome: interruptRPCSuspended, wantMarked: true},
		{name: "written suspends", rpcID: 21, bound: true, apply: (*cancelTracker).markWritten, status: "interrupted", wantOutcome: interruptRPCSuspended, wantMarked: true},
		{name: "accepted applies", rpcID: 21, bound: true, apply: func(tracker *cancelTracker, id uint64) {
			tracker.markWritten(id)
			tracker.accept(id)
		}, status: "interrupted", wantOutcome: interruptRPCApplied},
		{name: "failed is unclassified", rpcID: 21, bound: true, apply: func(tracker *cancelTracker, id uint64) {
			tracker.fail(id)
		}, status: "interrupted", wantOutcome: interruptRPCFailed},
		{name: "non-interrupted terminal is unclassified", rpcID: 21, bound: true, apply: (*cancelTracker).markWritten, status: "completed", wantOutcome: interruptRPCFailed},
		{name: "unbound rpc is unclassified", rpcID: 22, apply: (*cancelTracker).markWritten, status: "interrupted", wantOutcome: interruptRPCFailed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tracker := newCancelTracker()
			id, _, _ := tracker.acquire()
			if testCase.bound {
				tracker.bindRPC(testCase.rpcID)
			}
			testCase.apply(&tracker, id)
			attemptID, outcome := tracker.resolvePendingInterrupt(testCase.rpcID, testCase.status)
			wantAttempt := id
			if !testCase.bound {
				wantAttempt = 0
			}
			if attemptID != wantAttempt || outcome != testCase.wantOutcome {
				t.Fatalf("resolve = (%d, %d), want (%d, %d)", attemptID, outcome, wantAttempt, testCase.wantOutcome)
			}
			marked := attemptID != 0 && tracker.cancelAttempts[attemptID].terminalInterrupted
			if marked != testCase.wantMarked {
				t.Fatalf("terminalInterrupted = %v, want %v", marked, testCase.wantMarked)
			}
		})
	}
}

func TestCancelResetKeepsFlightState(t *testing.T) {
	tracker := newCancelTracker()
	id, _, done := tracker.acquire()
	tracker.bindRPC(31)
	tracker.markWritten(id)
	tracker.deferTerminal(interruptedTerminal())
	tracker.reset()
	if tracker.deferredTerminal != nil {
		t.Fatalf("reset kept the deferred terminal %+v", tracker.deferredTerminal)
	}
	if binding := tracker.forgetRPC(31); binding != 0 {
		t.Fatalf("reset kept the rpc binding %d", binding)
	}
	if tracker.cancelFlight == nil || tracker.cancelFlight.id != id {
		t.Fatalf("reset dropped the shared flight %+v", tracker.cancelFlight)
	}
	if !tracker.requested() {
		t.Fatal("reset dropped the cancellation evidence")
	}
	if tracker.cancelAttempts[id] == nil {
		t.Fatal("reset dropped the attempt record")
	}
	select {
	case <-done:
		t.Fatal("reset released the shared waiter")
	default:
	}
}

func TestCancelProcessExitSettlesDeferredTerminal(t *testing.T) {
	cases := []struct {
		name      string
		write     bool
		wantState cancelAttemptState
		wantErr   string
		wantKind  harness.ResultKind
	}{
		{name: "intent fails the attempt", wantState: cancelAttemptFailed, wantErr: "Codex process exited before the cancel request write completed", wantKind: harness.ResultFailed},
		{name: "written keeps its classification", write: true, wantState: cancelAttemptWritten, wantErr: "Codex process exited before the cancel response completed", wantKind: harness.ResultCancelled},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			session := newNativeSession(context.Background(), func() {}, &recordingHarnessSink{}, t.TempDir())
			id := session.beginCancelAttempt()
			session.mutex.Lock()
			session.cancelTracker.bindRPC(41)
			session.mutex.Unlock()
			if testCase.write {
				session.markCancelAttemptWritten(id)
			}
			if !session.deferInterruptedTerminal(interruptedTerminal()) {
				t.Fatal("bound interrupt did not defer the interrupted terminal")
			}
			session.failDeferredCancelOnProcessExit()
			session.mutex.Lock()
			attempt := session.cancelTracker.cancelAttempts[id]
			deferred := session.cancelTracker.deferredTerminal
			flight := session.cancelTracker.cancelFlight
			staged := session.terminalResult
			requested := session.cancelTracker.requested()
			session.mutex.Unlock()
			if deferred != nil {
				t.Fatalf("process exit kept the deferred terminal %+v", deferred)
			}
			if attempt == nil || attempt.state != testCase.wantState {
				t.Fatalf("attempt after process exit = %+v, want state %d", attempt, testCase.wantState)
			}
			if attempt.err == nil || attempt.err.Error() != testCase.wantErr {
				t.Fatalf("attempt error = %v, want %q", attempt.err, testCase.wantErr)
			}
			if flight != nil {
				t.Fatalf("process exit kept the shared flight %+v", flight)
			}
			select {
			case <-attempt.done:
			default:
				t.Fatal("process exit did not release the cancel waiter")
			}
			if staged == nil || staged.Kind != testCase.wantKind {
				t.Fatalf("staged terminal result = %+v, want kind %v", staged, testCase.wantKind)
			}
			if requested != testCase.write {
				t.Fatalf("cancellation evidence = %v, want %v", requested, testCase.write)
			}
		})
	}
}

func TestCancelViolationHookFiresOnIllegalTransitions(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(tracker *cancelTracker) uint64
		apply     func(tracker *cancelTracker, id uint64)
		want      cancelTransition
		wantState cancelAttemptState
	}{
		{
			name: "markWritten after accept",
			setup: func(tracker *cancelTracker) uint64 {
				id, _, _ := tracker.acquire()
				tracker.markWritten(id)
				tracker.accept(id)
				return id
			},
			apply:     (*cancelTracker).markWritten,
			want:      cancelMarkWritten,
			wantState: cancelAttemptAccepted,
		},
		{
			name: "accept before write",
			setup: func(tracker *cancelTracker) uint64 {
				id, _, _ := tracker.acquire()
				return id
			},
			apply:     (*cancelTracker).accept,
			want:      cancelAccept,
			wantState: cancelAttemptIntent,
		},
		{
			name: "reject after settle",
			setup: func(tracker *cancelTracker) uint64 {
				id, _, _ := tracker.acquire()
				tracker.markWritten(id)
				tracker.accept(id)
				return id
			},
			apply: func(tracker *cancelTracker, id uint64) {
				tracker.reject(id)
			},
			want:      cancelReject,
			wantState: cancelAttemptAccepted,
		},
		{
			name: "fail after settle",
			setup: func(tracker *cancelTracker) uint64 {
				id, _, _ := tracker.acquire()
				tracker.fail(id)
				return id
			},
			apply: func(tracker *cancelTracker, id uint64) {
				tracker.fail(id)
			},
			want:      cancelFail,
			wantState: cancelAttemptFailed,
		},
		{
			name: "finish on an unknown attempt",
			setup: func(*cancelTracker) uint64 {
				return 4242
			},
			apply: func(tracker *cancelTracker, id uint64) {
				tracker.finish(id, nil)
			},
			want:      cancelFinish,
			wantState: 0,
		},
		{
			name: "legal intent write",
			setup: func(tracker *cancelTracker) uint64 {
				id, _, _ := tracker.acquire()
				return id
			},
			apply: (*cancelTracker).markWritten,
		},
		{
			name: "legal written failure keeps its write",
			setup: func(tracker *cancelTracker) uint64 {
				id, _, _ := tracker.acquire()
				tracker.markWritten(id)
				return id
			},
			apply: func(tracker *cancelTracker, id uint64) {
				tracker.fail(id)
			},
		},
		{
			name: "legal reject of an intent",
			setup: func(tracker *cancelTracker) uint64 {
				id, _, _ := tracker.acquire()
				return id
			},
			apply: func(tracker *cancelTracker, id uint64) {
				tracker.reject(id)
			},
		},
		{
			name: "legal repeated finish",
			setup: func(tracker *cancelTracker) uint64 {
				id, _, _ := tracker.acquire()
				tracker.finish(id, context.Canceled)
				return id
			},
			apply: func(tracker *cancelTracker, id uint64) {
				tracker.finish(id, context.Canceled)
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			records := recordCancelViolations(t)
			tracker := newCancelTracker()
			id := testCase.setup(&tracker)
			testCase.apply(&tracker, id)
			if testCase.want == 0 {
				if len(*records) != 0 {
					t.Fatalf("legal transition reported %+v", *records)
				}
				return
			}
			if len(*records) != 1 || (*records)[0].transition != testCase.want || (*records)[0].attemptID != id {
				t.Fatalf("violations = %+v, want transition %d for attempt %d", *records, testCase.want, id)
			}
			if testCase.wantState == 0 {
				if tracker.cancelAttempts[id] != nil {
					t.Fatalf("illegal transition created attempt %+v", tracker.cancelAttempts[id])
				}
				return
			}
			if got := tracker.cancelAttempts[id].state; got != testCase.wantState {
				t.Fatalf("state after violation = %d, want %d unchanged", got, testCase.wantState)
			}
		})
	}
}

func TestCancelFieldsWrittenOnlyInOwnerFile(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files[name] = file
		names = append(names, name)
	}
	if _, ok := files["cancel_state.go"]; !ok {
		t.Fatal("cancel_state.go is missing from the package")
	}
	cancelTrackerFields := assertCancelTrackerShape(t, names, files)
	writes := map[string]int{}
	for _, name := range names {
		file := files[name]
		aliases := map[string]bool{}
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				aliases = map[string]bool{}
				if receiver, ok := cancelTrackerReceiver(typed); ok && receiver != "" {
					aliases[receiver] = true
				}
			case *ast.DeclStmt:
				recordTrackerAliasDecl(typed, aliases)
			case *ast.AssignStmt:
				recordTrackerAliasAssign(typed, aliases)
				for _, target := range typed.Lhs {
					recordCancelFieldWrite(t, fset, name, target, aliases, writes, cancelTrackerFields)
				}
			case *ast.IncDecStmt:
				recordCancelFieldWrite(t, fset, name, typed.X, aliases, writes, cancelTrackerFields)
			case *ast.CallExpr:
				ident, ok := typed.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				if ident.Name == "delete" && len(typed.Args) == 2 {
					recordCancelFieldWrite(t, fset, name, typed.Args[0], aliases, writes, cancelTrackerFields)
				}
				if ident.Name == "clear" && len(typed.Args) == 1 {
					recordCancelFieldWrite(t, fset, name, typed.Args[0], aliases, writes, cancelTrackerFields)
				}
			case *ast.CompositeLit:
				if ident, ok := typed.Type.(*ast.Ident); ok && ident.Name == "cancelTracker" {
					for _, element := range typed.Elts {
						keyed, ok := element.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						ident, ok := keyed.Key.(*ast.Ident)
						if !ok || !cancelTrackerFields[ident.Name] {
							continue
						}
						if name != "cancel_state.go" {
							t.Fatalf("%s:%d writes cancelTracker.%s outside cancel_state.go", name, fset.Position(keyed.Key.Pos()).Line, ident.Name)
						}
						writes[ident.Name]++
					}
				}
			}
			return true
		})
	}
	for field := range cancelTrackerFields {
		if writes[field] == 0 {
			t.Fatalf("cancel field %s has no write inside cancel_state.go", field)
		}
	}
}

func recordTrackerAliasAssign(stmt *ast.AssignStmt, aliases map[string]bool) {
	if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
		return
	}
	ident, ok := stmt.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	if trackerAliasExpr(stmt.Rhs[0], aliases) {
		aliases[ident.Name] = true
		return
	}
	delete(aliases, ident.Name)
}

func recordTrackerAliasDecl(stmt *ast.DeclStmt, aliases map[string]bool) {
	decl, ok := stmt.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range decl.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for index, ident := range value.Names {
			if index < len(value.Values) && trackerAliasExpr(value.Values[index], aliases) {
				aliases[ident.Name] = true
			}
		}
	}
}

func trackerAliasExpr(expr ast.Expr, aliases map[string]bool) bool {
	for {
		switch typed := expr.(type) {
		case *ast.ParenExpr:
			expr = typed.X
			continue
		case *ast.UnaryExpr:
			if typed.Op != token.AND {
				return false
			}
			expr = typed.X
			continue
		}
		break
	}
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name == "tracker" || aliases[typed.Name]
	case *ast.SelectorExpr:
		return typed.Sel.Name == "cancelTracker"
	}
	return false
}

func recordCancelFieldWrite(t *testing.T, fset *token.FileSet, name string, target ast.Expr, aliases map[string]bool, writes map[string]int, fields map[string]bool) {
	t.Helper()
	field, ok := cancelTrackerFieldAccess(target, fields, aliases)
	if !ok {
		return
	}
	if name != "cancel_state.go" {
		t.Fatalf("%s:%d writes cancelTracker.%s outside cancel_state.go", name, fset.Position(target.Pos()).Line, field)
	}
	writes[field]++
}

func cancelTrackerFieldAccess(target ast.Expr, fields map[string]bool, aliases map[string]bool) (string, bool) {
	for {
		switch typed := target.(type) {
		case *ast.IndexExpr:
			target = typed.X
			continue
		case *ast.StarExpr:
			target = typed.X
			continue
		case *ast.ParenExpr:
			target = typed.X
			continue
		}
		break
	}
	selector, ok := target.(*ast.SelectorExpr)
	if !ok || !fields[selector.Sel.Name] {
		return "", false
	}
	switch base := selector.X.(type) {
	case *ast.Ident:
		if base.Name == "tracker" || aliases[base.Name] {
			return selector.Sel.Name, true
		}
	case *ast.SelectorExpr:
		if base.Sel.Name == "cancelTracker" {
			return selector.Sel.Name, true
		}
	}
	return "", false
}

func cancelTrackerReceiver(decl *ast.FuncDecl) (string, bool) {
	if decl.Recv == nil || len(decl.Recv.List) != 1 {
		return "", false
	}
	receiver := decl.Recv.List[0].Type
	for {
		switch typed := receiver.(type) {
		case *ast.StarExpr:
			receiver = typed.X
			continue
		case *ast.ParenExpr:
			receiver = typed.X
			continue
		}
		break
	}
	ident, ok := receiver.(*ast.Ident)
	if !ok || ident.Name != "cancelTracker" {
		return "", false
	}
	if len(decl.Recv.List[0].Names) == 1 {
		return decl.Recv.List[0].Names[0].Name, true
	}
	return "", true
}

func assertCancelTrackerShape(t *testing.T, names []string, files map[string]*ast.File) map[string]bool {
	t.Helper()
	methods := map[string]bool{}
	fields := map[string]bool{}
	for _, name := range names {
		file := files[name]
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				if _, ok := cancelTrackerReceiver(typed); !ok {
					return true
				}
				if name != "cancel_state.go" {
					t.Fatalf("%s defines cancelTracker method %s outside cancel_state.go", name, typed.Name.Name)
				}
				methods[typed.Name.Name] = true
			case *ast.TypeSpec:
				if name != "cancel_state.go" || typed.Name.Name != "cancelTracker" {
					return true
				}
				structure, ok := typed.Type.(*ast.StructType)
				if !ok {
					t.Fatal("cancelTracker is not a struct")
				}
				for _, field := range structure.Fields.List {
					for _, fieldName := range field.Names {
						fields[fieldName.Name] = true
					}
					switch fieldType := field.Type.(type) {
					case *ast.Ident:
						if strings.Contains(fieldType.Name, "Mutex") {
							t.Fatalf("cancelTracker field %v is a lock", field.Names)
						}
					case *ast.SelectorExpr:
						if strings.Contains(fieldType.Sel.Name, "Mutex") {
							t.Fatalf("cancelTracker field %v is a lock", field.Names)
						}
					}
				}
			}
			return true
		})
	}
	if len(fields) == 0 {
		t.Fatal("cancelTracker has no fields to scan")
	}
	if len(methods) > 14 {
		t.Fatalf("cancelTracker exposes %d methods, want at most the 14 named transitions: %v", len(methods), methods)
	}
	for _, transition := range []string{"acquire", "bindRPC", "forgetRPC", "observeResponse", "markWritten", "accept", "reject", "fail", "finish", "deferTerminal", "takeDeferredTerminal", "reset", "requested", "resolvePendingInterrupt"} {
		if !methods[transition] {
			t.Fatalf("cancelTracker is missing the %s transition", transition)
		}
	}
	return fields
}
