package app

import (
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// This file exercises the command-queue module (command_queue.go) without a
// daemon: every invariant the queue and its receipt bookkeeping own is asserted
// at their own interface. The daemon-level behaviour oracle stays the existing
// app tests, which drive the same invariants through the executor. Each test
// names the red mutant recorded for it in the slice's evidence file
// (docs/evidence/rf-w4-17a-command-queue-module-2026-11-21.md).

func queuedTestCommand(runID string, generation int64, commandID string) protocol.Command {
	return protocol.Command{CommandID: commandID, RunID: runID, Generation: generation, Kind: "provide_input"}
}

func queuedTestKey(runID string, generation int64, commandID string) commandKey {
	return commandKey{run: state.RunKey{RunID: runID, Generation: generation}, id: commandID}
}

func requireClosedDone(t *testing.T, queued *queuedCommand, context string) {
	t.Helper()
	select {
	case <-queued.done:
	default:
		t.Fatalf("%s: completion channel is still open", context)
	}
}

func requireOpenDone(t *testing.T, queued *queuedCommand, context string) {
	t.Helper()
	select {
	case <-queued.done:
		t.Fatalf("%s: completion channel is closed", context)
	default:
	}
}

// Red mutant: drop the byKey existence check in enqueue (always build a new
// entry) -> "duplicate command key created a second entry" fails.
func TestCommandQueueDeduplicatesByCommandKey(t *testing.T) {
	queue := newCommandQueue()
	first, created := queue.enqueue(queuedTestCommand("run-1", 1, "input-1"))
	if !created {
		t.Fatal("first enqueue did not create an entry")
	}
	duplicate, created := queue.enqueue(queuedTestCommand("run-1", 1, "input-1"))
	if created {
		t.Fatal("duplicate command key created a second entry")
	}
	if duplicate != first {
		t.Fatalf("duplicate enqueue returned %p, want the registered entry %p", duplicate, first)
	}
	if len(queue.pending) != 1 {
		t.Fatalf("pending entries after a duplicate enqueue = %d, want 1", len(queue.pending))
	}
	other, created := queue.enqueue(queuedTestCommand("run-1", 1, "input-2"))
	if !created {
		t.Fatal("second command id was treated as a duplicate")
	}
	if other == first {
		t.Fatalf("second command id reused entry %p", first)
	}
	if len(queue.pending) != 2 {
		t.Fatalf("pending entries after two distinct keys = %d, want 2", len(queue.pending))
	}
}

// Red mutant: append the lane head instead of popping it (LIFO) -> the second
// lane pop returns input-1 and "lane order = ..." fails.
func TestCommandQueueLanePreservesFIFOOrderPerRun(t *testing.T) {
	queue := newCommandQueue()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	first, _ := queue.enqueue(queuedTestCommand("run-1", 1, "input-1"))
	second, _ := queue.enqueue(queuedTestCommand("run-1", 1, "input-2"))
	if !queue.lane(key, first) {
		t.Fatal("first lane append did not report that the lane must be started")
	}
	if queue.lane(key, second) {
		t.Fatal("append behind a draining lane reported that the lane must be started")
	}
	popped, ok := queue.nextInLane(key)
	if !ok || popped != first {
		t.Fatalf("first lane pop = %p/%v, want %p", popped, ok, first)
	}
	popped, ok = queue.nextInLane(key)
	if !ok || popped != second {
		t.Fatalf("second lane pop = %p/%v, want %p", popped, ok, second)
	}
	if popped, ok := queue.nextInLane(key); ok {
		t.Fatalf("drained lane yielded %p", popped)
	}
	if _, exists := queue.lanes[key]; exists {
		t.Fatal("drained lane stayed in the lane map")
	}
	third, _ := queue.enqueue(queuedTestCommand("run-1", 1, "input-3"))
	if !queue.lane(key, third) {
		t.Fatal("a lane built after the previous drain was not reported as needing a start")
	}
}

// Red mutant: key the lane map by the run id only (ignore the generation) ->
// the next generation of run-a reuses the previous generation's lane and stops
// reporting a start, so "reused the previous generation's lane" fails.
func TestCommandQueueArrivalOrderAndLanesAreIndependentPerRun(t *testing.T) {
	queue := newCommandQueue()
	runA := state.RunKey{RunID: "run-a", Generation: 1}
	runARestart := state.RunKey{RunID: "run-a", Generation: 2}
	runB := state.RunKey{RunID: "run-b", Generation: 1}
	a1, _ := queue.enqueue(queuedTestCommand("run-a", 1, "input-1"))
	b1, _ := queue.enqueue(queuedTestCommand("run-b", 1, "input-1"))
	a2, _ := queue.enqueue(queuedTestCommand("run-a", 2, "input-1"))
	for index, want := range []*queuedCommand{a1, b1, a2} {
		popped, ok := queue.next()
		if !ok || popped != want {
			t.Fatalf("arrival pop %d = %p/%v, want %p", index, popped, ok, want)
		}
	}
	if _, ok := queue.next(); ok {
		t.Fatal("arrival queue yielded a fourth command")
	}
	if !queue.lane(runA, a1) {
		t.Fatal("run-a's first lane append did not report a start")
	}
	if !queue.lane(runB, b1) {
		t.Fatal("run-b's first lane append did not report a start: the runs share one lane")
	}
	if !queue.lane(runARestart, a2) {
		t.Fatal("the next generation of the same run id reused the previous generation's lane")
	}
	poppedA, ok := queue.nextInLane(runA)
	if !ok || poppedA != a1 {
		t.Fatalf("run-a's first lane pop = %p/%v, want %p", poppedA, ok, a1)
	}
	if popped, ok := queue.nextInLane(runA); ok {
		t.Fatalf("run-a's drained lane yielded %p", popped)
	}
	poppedB, ok := queue.nextInLane(runB)
	if !ok || poppedB != b1 {
		t.Fatalf("run-b's lane pop = %p/%v, want %p", poppedB, ok, b1)
	}
	poppedRestart, ok := queue.nextInLane(runARestart)
	if !ok || poppedRestart != a2 {
		t.Fatalf("run-a's next-generation lane pop = %p/%v, want %p", poppedRestart, ok, a2)
	}
}

// Red mutant: leave completed entries registered in dropRun (skip the eviction)
// -> "run-a left N registered entries" fails.
func TestCommandQueueDropRunReleasesExactlyOneRun(t *testing.T) {
	queue := newCommandQueue()
	runA := state.RunKey{RunID: "run-a", Generation: 1}
	runB := state.RunKey{RunID: "run-b", Generation: 1}
	a1, _ := queue.enqueue(queuedTestCommand("run-a", 1, "input-1"))
	a2, _ := queue.enqueue(queuedTestCommand("run-a", 1, "input-2"))
	b1, _ := queue.enqueue(queuedTestCommand("run-b", 1, "input-1"))
	if !queue.lane(runA, a1) {
		t.Fatal("run-a's lane append did not report a start")
	}
	// a2 finished with a receipt still pending: completed but registered.
	queue.finish(a2.key, true)
	if !queue.current(a2) {
		t.Fatal("an entry with a pending receipt was evicted by finish")
	}
	queue.dropRun(runA)
	requireClosedDone(t, a1, "run-a's queued entry after dropRun")
	requireClosedDone(t, a2, "run-a's completed entry after dropRun")
	requireOpenDone(t, b1, "run-b's queued entry after run-a's dropRun")
	if len(queue.byKey) != 1 || !queue.current(b1) {
		t.Fatalf("registered entries after dropping run-a = %d, want only run-b's", len(queue.byKey))
	}
	if queue.current(a1) || queue.current(a2) {
		t.Fatal("a released entry stayed current")
	}
	// The arrival FIFO and the lane may still hold the released pointers, and
	// their consumers drop them through current: that is the base behavior.
	if popped, ok := queue.next(); !ok || popped != a1 || queue.current(popped) {
		t.Fatalf("arrival pop after dropRun = %p/%v, want the released entry %p marked not current", popped, ok, a1)
	}
	if popped, ok := queue.nextInLane(runA); !ok || popped != a1 || queue.current(popped) {
		t.Fatalf("lane pop after dropRun = %p/%v, want the released entry %p marked not current", popped, ok, a1)
	}
	queue.finish(a1.key, false)
	if !queue.current(b1) || len(queue.byKey) != 1 {
		t.Fatal("finishing a released entry disturbed the surviving run")
	}
	queue.dropRun(runB)
	requireClosedDone(t, b1, "run-b's entry after its own dropRun")
	if len(queue.byKey) != 0 {
		t.Fatalf("registered entries after dropping run-b = %d, want none", len(queue.byKey))
	}
}

// Red mutant: ignore the delivered flag in finish (always evict a receipted
// entry) -> "entry with a pending receipt was evicted" fails.
// Red mutant 2: drop the nil-entry guard in markDelivered -> the absent-key
// resolution panics instead of being a no-op.
func TestCommandQueueFinishHonoursReceiptAndDelivery(t *testing.T) {
	queue := newCommandQueue()
	withoutReceipt, _ := queue.enqueue(queuedTestCommand("run-1", 1, "cancel-1"))
	queue.finish(withoutReceipt.key, false)
	requireClosedDone(t, withoutReceipt, "entry finished without a receipt")
	if queue.current(withoutReceipt) {
		t.Fatal("entry finished without a receipt stayed registered")
	}

	withReceipt, _ := queue.enqueue(queuedTestCommand("run-1", 1, "input-1"))
	queue.finish(withReceipt.key, true)
	requireClosedDone(t, withReceipt, "entry finished with a receipt")
	if !queue.current(withReceipt) {
		t.Fatal("entry with a pending receipt was evicted before delivery")
	}
	queue.markDelivered(withReceipt.key)
	if queue.current(withReceipt) {
		t.Fatal("delivered receipt did not evict the completed entry")
	}

	inflight, _ := queue.enqueue(queuedTestCommand("run-1", 1, "input-2"))
	queue.markDelivered(inflight.key)
	if !queue.current(inflight) {
		t.Fatal("delivered receipt evicted an entry that is still in flight")
	}
	// A receipt delivered while the command was still in flight is settled by
	// its completion: the durable acknowledgement is already gone, so the
	// dedupe entry goes with it.
	deliveredFirst, _ := queue.enqueue(queuedTestCommand("run-1", 1, "input-3"))
	queue.markDelivered(deliveredFirst.key)
	queue.finish(deliveredFirst.key, true)
	requireClosedDone(t, deliveredFirst, "entry finished after its receipt was delivered")
	if queue.current(deliveredFirst) {
		t.Fatal("completion kept an entry whose receipt was already delivered")
	}
	queue.finish(inflight.key, false)
	replay, created := queue.enqueue(queuedTestCommand("run-1", 1, "input-2"))
	if !created {
		t.Fatal("replay after eviction was treated as a duplicate")
	}
	if queue.current(inflight) {
		t.Fatal("the evicted entry still reports current after its key was re-enqueued")
	}
	// Completion is by key, so a caller holding a stale pointer checks current
	// first -- the guard daemon.finishQueuedCommand composes around finish. The
	// stale entry fails that guard, so the replay must survive.
	if queue.current(inflight) {
		queue.finish(inflight.key, false)
	}
	if !queue.current(replay) {
		t.Fatal("a stale instance settled the re-enqueued command key")
	}
	queue.finish(replay.key, false)
	if queue.current(replay) {
		t.Fatal("deferred completion did not complete the registered entry")
	}
	queue.finish(queuedTestKey("run-9", 1, "absent"), true)
	queue.markDelivered(queuedTestKey("run-9", 1, "absent"))
	if len(queue.byKey) != 0 {
		t.Fatalf("registered entries after resolving an absent key = %d, want none", len(queue.byKey))
	}
}

// Red mutant: make suppress write only into the first window it visits -> the
// second live window's "live windows were not both tombstoned" fails.
func TestCommandQueueRequestWindowsAccountTombstones(t *testing.T) {
	receipts := commandReceipts{}
	key := queuedTestKey("run-1", 1, "input-1")
	first := receipts.begin()
	second := receipts.begin()
	if first == second {
		t.Fatal("begin reused a request id for a second window")
	}
	if receipts.suppressed(first, key) || receipts.suppressed(second, key) {
		t.Fatal("a window suppressed a command before any acknowledgement delivery")
	}
	receipts.suppress(key)
	if !receipts.suppressed(first, key) || !receipts.suppressed(second, key) {
		t.Fatal("live windows were not both tombstoned")
	}
	receipts.finish(first)
	if receipts.suppressed(first, key) {
		t.Fatal("a finished window kept its tombstone")
	}
	if !receipts.suppressed(second, key) {
		t.Fatal("finishing one window dropped another window's tombstone")
	}
	late := receipts.begin()
	if receipts.suppressed(late, key) {
		t.Fatal("a window opened after the delivery suppressed the command")
	}
	receipts.finish(second)
	receipts.finish(late)
	if len(receipts.requests) != 0 {
		t.Fatalf("live windows after finishing all of them = %d, want none", len(receipts.requests))
	}
}

// Red mutant: drop the byKey nil guard in enqueue (or the lanes nil guard in
// lane) -> the assignment into the nil map panics with "assignment to entry in
// nil map".
func TestCommandQueueZeroValueCreatesItsMaps(t *testing.T) {
	var queue commandQueue
	queued, created := queue.enqueue(queuedTestCommand("run-1", 1, "input-1"))
	if !created {
		t.Fatal("the zero-value queue did not register a command")
	}
	if !queue.current(queued) {
		t.Fatal("the zero-value queue's dedupe index did not register the entry")
	}
	if popped, ok := queue.next(); !ok || popped != queued {
		t.Fatalf("the zero-value queue's arrival pop = %p/%v, want %p", popped, ok, queued)
	}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	if !queue.lane(key, queued) {
		t.Fatal("the zero-value queue's first lane append did not report a start")
	}
	if popped, ok := queue.nextInLane(key); !ok || popped != queued {
		t.Fatalf("the zero-value queue's lane pop = %p/%v, want %p", popped, ok, queued)
	}
	if requestID := queue.receipts.begin(); requestID == 0 {
		t.Fatal("the zero-value queue's receipt bookkeeping did not open a window")
	}
}
