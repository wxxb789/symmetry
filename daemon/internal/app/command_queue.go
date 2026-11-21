package app

import (
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// commandQueue owns the daemon's command queue: the arrival FIFO, the dedupe
// index keyed by command key, and the per-run serialization lanes, together with
// the acknowledgement-receipt bookkeeping (commandReceipts) that fences late
// control-plane responses against delivered acknowledgements.
//
// Lock contract: every method requires the caller to hold daemon.mu. The queue
// never locks and never performs I/O; daemon.commandReceiptMu, which serializes a
// journal write against queue mutation, stays on the daemon and keeps the
// acquisition points it had before this module existed. A commandQueue may be
// used as a zero value: every map it reads is created on first use.
type commandQueue struct {
	pending  []*queuedCommand
	byKey    map[commandKey]*queuedCommand
	lanes    map[state.RunKey]*commandLane
	receipts commandReceipts
}

// newCommandQueue returns an empty queue. startBackground constructs one per
// background lifetime, replacing the fields the daemon used to reset there.
func newCommandQueue() commandQueue {
	return commandQueue{
		byKey: make(map[commandKey]*queuedCommand),
		lanes: make(map[state.RunKey]*commandLane),
		receipts: commandReceipts{
			requests: make(map[uint64]map[commandKey]struct{}),
		},
	}
}

// queuedCommand is one queued command: the wire command, its dedupe key, the
// channel waiters block on until the command completes, and the two receipt
// flags that decide when its dedupe entry may be evicted. Registered entries are
// reached through commandQueue.byKey; completed entries may stay registered
// while their durable acknowledgement is still pending delivery.
type queuedCommand struct {
	command   protocol.Command
	key       commandKey
	done      chan struct{}
	completed bool
	delivered bool
}

// commandKey identifies a queued command: one run generation plus the control
// plane's command id. Dedupe is exact: a replay of the same command key never
// queues a second entry.
type commandKey struct {
	run state.RunKey
	id  string
}

// commandLane serializes the commands of one run: they execute in arrival order,
// one at a time, while a cancellation bypasses the lane and is executed by the
// executor directly. A lane exists exactly while a drain goroutine owns it.
type commandLane struct {
	pending []*queuedCommand
	running bool
}

// enqueue registers command under its command key and appends it to the arrival
// FIFO. It reports created=false when that key is already queued, returning the
// existing entry: the caller hands the existing completion channel to its waiter
// and appends nothing.
func (queue *commandQueue) enqueue(command protocol.Command) (*queuedCommand, bool) {
	key := commandKey{run: state.RunKey{RunID: command.RunID, Generation: command.Generation}, id: command.CommandID}
	if queue.byKey == nil {
		queue.byKey = make(map[commandKey]*queuedCommand)
	}
	if queued, exists := queue.byKey[key]; exists {
		return queued, false
	}
	queued := &queuedCommand{command: command, key: key, done: make(chan struct{})}
	queue.byKey[key] = queued
	queue.pending = append(queue.pending, queued)
	return queued, true
}

// next pops the head of the arrival FIFO. The slot is cleared before the slice
// is re-headed so a popped entry is not retained by the backing array.
func (queue *commandQueue) next() (*queuedCommand, bool) {
	if len(queue.pending) == 0 {
		return nil, false
	}
	queued := queue.pending[0]
	queue.pending[0] = nil
	queue.pending = queue.pending[1:]
	return queued, true
}

// lane appends queued to its run's serialization lane. It reports whether the
// caller must start the lane's drain goroutine: the first command appended to an
// idle run's lane starts it, a command appended behind a draining lane does not.
func (queue *commandQueue) lane(key state.RunKey, queued *queuedCommand) bool {
	if queue.lanes == nil {
		queue.lanes = make(map[state.RunKey]*commandLane)
	}
	lane := queue.lanes[key]
	if lane == nil {
		lane = &commandLane{}
		queue.lanes[key] = lane
	}
	lane.pending = append(lane.pending, queued)
	if lane.running {
		return false
	}
	lane.running = true
	return true
}

// nextInLane pops the head of the run's lane and drops the lane once drained
// (also when the run has no lane at all, which is a no-op delete). Draining is
// how a run's serialization ends: the drain goroutine returns when this reports
// false.
func (queue *commandQueue) nextInLane(key state.RunKey) (*queuedCommand, bool) {
	lane := queue.lanes[key]
	if lane == nil || len(lane.pending) == 0 {
		delete(queue.lanes, key)
		return nil, false
	}
	queued := lane.pending[0]
	lane.pending[0] = nil
	lane.pending = lane.pending[1:]
	return queued, true
}

// current reports whether queued is still the entry registered for its key. A
// released run evicts its entries while the arrival FIFO and the lanes may still
// hold pointers to them, so every consumer checks current before acting on a
// popped entry and drops the entry when this reports false.
func (queue *commandQueue) current(queued *queuedCommand) bool {
	return queue.byKey[queued.key] == queued
}

// finish completes the entry registered for key. When registered is non-nil it
// must be that exact entry: a released run may have re-enqueued the same key with
// a new entry, and completing the new entry from a stale caller would settle a
// live command. A nil registered means "the entry key currently maps to" and is
// the deferred-completion path.
//
// Completion closes the entry's done channel once, marks it completed, and
// evicts it unless a durable acknowledgement is still pending delivery
// (receipt true) that has not been delivered yet.
func (queue *commandQueue) finish(key commandKey, registered *queuedCommand, receipt bool) {
	current := queue.byKey[key]
	if current == nil || (registered != nil && current != registered) {
		return
	}
	if !current.completed {
		close(current.done)
	}
	current.completed = true
	if !receipt || current.delivered {
		delete(queue.byKey, key)
	}
}

// dropRun releases every entry registered for a run: waiters are completed and
// the dedupe entries are evicted, while entries of other runs are untouched.
// Entries already popped into the arrival FIFO's consumers or into a lane are
// not removed here; their consumers drop them through current.
func (queue *commandQueue) dropRun(key state.RunKey) {
	for queuedKey, queued := range queue.byKey {
		if queuedKey.run == key {
			if !queued.completed {
				queued.completed = true
				close(queued.done)
			}
			delete(queue.byKey, queuedKey)
		}
	}
}

// markDelivered records that the durable acknowledgement for key reached the
// control plane. A completed entry is evicted once its receipt is delivered; an
// entry still in flight keeps its registration so a replay of the command finds
// the same completion channel.
func (queue *commandQueue) markDelivered(key commandKey) {
	queued := queue.byKey[key]
	if queued == nil {
		return
	}
	queued.delivered = true
	if queued.completed {
		delete(queue.byKey, key)
	}
}

// commandReceipts owns the acknowledgement-receipt bookkeeping: the suppression
// windows opened around control-plane responses and the tombstones that an
// acknowledgement delivery writes into every live window.
//
// It is a value inside commandQueue rather than more queue methods because its
// tombstones and delivered marks are written by the outbox delivery path across
// a journal write, outside any queue call, and the mutex that wires those two
// phases (daemon.commandReceiptMu) is held while that write may block. That
// mutex cannot become part of this module's lock-free contract, so the receipt
// state stays a sibling value rather than four more queue methods: one intent per
// type, every write to the bookkeeping inside this file, and the module's total
// stays at the contract's binding budget (at most 12 methods: the 8 queue methods
// above plus the 4 receipt methods below). Lock contract and zero-value tolerance
// are commandQueue's.
type commandReceipts struct {
	requests    map[uint64]map[commandKey]struct{}
	nextRequest uint64
}

// begin opens a suppression window and returns its id. A command whose
// acknowledgement reaches the control plane while the window is open is
// tombstoned for it, so the response that window fences cannot enqueue that
// command again.
func (receipts *commandReceipts) begin() uint64 {
	if receipts.requests == nil {
		receipts.requests = make(map[uint64]map[commandKey]struct{})
	}
	receipts.nextRequest++
	requestID := receipts.nextRequest
	receipts.requests[requestID] = make(map[commandKey]struct{})
	return requestID
}

// finish closes a window. Closing is how a response stops fencing: a delivery
// that happens afterwards is invisible to it.
func (receipts *commandReceipts) finish(requestID uint64) {
	delete(receipts.requests, requestID)
}

// suppressed reports whether this window carries a tombstone for key.
func (receipts *commandReceipts) suppressed(requestID uint64, key commandKey) bool {
	_, suppressed := receipts.requests[requestID][key]
	return suppressed
}

// suppress tombstones key in every live window: that command's acknowledgement
// reached the control plane, so no in-flight response may enqueue it again. A
// window whose set is nil is given one, matching the defensive handling the
// delivery path had before this module existed.
func (receipts *commandReceipts) suppress(key commandKey) {
	for requestID, tombstones := range receipts.requests {
		if tombstones == nil {
			tombstones = make(map[commandKey]struct{})
		}
		tombstones[key] = struct{}{}
		receipts.requests[requestID] = tombstones
	}
}
