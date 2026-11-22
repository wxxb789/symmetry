package app

import (
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// Lock contract: every method requires the caller to hold daemon.mu; the queue never locks,
// never performs I/O, and daemon.commandReceiptMu stays on the daemon.
type commandQueue struct {
	pending  []*queuedCommand
	byKey    map[commandKey]*queuedCommand
	lanes    map[state.RunKey]*commandLane
	receipts commandReceipts
}

func newCommandQueue() commandQueue {
	return commandQueue{
		byKey: make(map[commandKey]*queuedCommand),
		lanes: make(map[state.RunKey]*commandLane),
		receipts: commandReceipts{
			requests: make(map[uint64]map[commandKey]struct{}),
		},
	}
}

type queuedCommand struct {
	command   protocol.Command
	key       commandKey
	done      chan struct{}
	completed bool
	delivered bool
}

type commandKey struct {
	run state.RunKey
	id  string
}

// Serializes one run's commands: arrival order, one at a time; cancellation bypasses the lane
// and is executed by the executor directly.
type commandLane struct {
	pending []*queuedCommand
	running bool
}

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

// A released run leaves stale pointers in the arrival FIFO and lanes, so every consumer
// checks current before acting on a popped entry and drops the entry when this reports false.
func (queue *commandQueue) current(queued *queuedCommand) bool {
	return queue.byKey[queued.key] == queued
}

// registered, when non-nil, must be the exact entry: a released run may have re-enqueued the
// same key, and completing the new entry from a stale caller would settle a live command.
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

// Completed entries are evicted once their receipt is delivered; an in-flight entry keeps
// its registration so a replay finds the same completion channel.
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

// Lock order: commandReceiptMu + daemon.mu. The delivery journal write releases both locks
// and re-takes them for markDelivered, so commandReceiptMu never spans that write.
type commandReceipts struct {
	requests    map[uint64]map[commandKey]struct{}
	nextRequest uint64
}

// Opens a suppression window: a command whose acknowledgement reaches the control plane
// while it is open is tombstoned, so the response the window fences cannot re-enqueue it.
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

func (receipts *commandReceipts) suppressed(requestID uint64, key commandKey) bool {
	_, suppressed := receipts.requests[requestID][key]
	return suppressed
}

// Tombstones key in every live window: that command's acknowledgement reached the control
// plane, so no in-flight response may enqueue it again.
func (receipts *commandReceipts) suppress(key commandKey) {
	for requestID, tombstones := range receipts.requests {
		if tombstones == nil {
			tombstones = make(map[commandKey]struct{})
		}
		tombstones[key] = struct{}{}
		receipts.requests[requestID] = tombstones
	}
}
