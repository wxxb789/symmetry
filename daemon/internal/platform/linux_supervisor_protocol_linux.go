//go:build linux

package platform

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	linuxSupervisorProtocolVersion = 1
	linuxSupervisorMaxFrame        = 16 << 10
)

var (
	errLinuxSupervisorFrameTooLarge = errors.New("linux supervisor frame is too large")
	errLinuxSupervisorSequence      = errors.New("linux supervisor sequence is stale")
)

// linuxSupervisorRequest is the local, authenticated control shape used by a
// per-run helper. Secrets are transported only over an inherited/private FD or
// an already-authenticated local connection; this type is never encoded into
// argv, environment, or a socket name.
type linuxSupervisorRequest struct {
	Version            int    `json:"version"`
	Operation          string `json:"operation"`
	LaunchToken        string `json:"launch_token,omitempty"`
	Sequence           uint64 `json:"sequence,omitempty"`
	DeadlineMS         int64  `json:"deadline_ms,omitempty"`
	TargetPID          int    `json:"target_pid,omitempty"`
	TargetIdentity     string `json:"target_identity,omitempty"`
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContext       string `json:"owner_context,omitempty"`
	SupervisorPID      int    `json:"supervisor_pid,omitempty"`
	SupervisorIdentity string `json:"supervisor_identity,omitempty"`
	Token              string `json:"token,omitempty"`
	JobID              string `json:"job_id,omitempty"`
	Secret             string `json:"secret,omitempty"`
}

type linuxSupervisorResponse struct {
	Version            int    `json:"version"`
	Operation          string `json:"operation"`
	Status             string `json:"status"`
	LaunchToken        string `json:"launch_token,omitempty"`
	Sequence           uint64 `json:"sequence,omitempty"`
	TargetPID          int    `json:"target_pid,omitempty"`
	TargetIdentity     string `json:"target_identity,omitempty"`
	OwnerKind          string `json:"owner_kind,omitempty"`
	OwnerContext       string `json:"owner_context,omitempty"`
	SupervisorPID      int    `json:"supervisor_pid,omitempty"`
	SupervisorIdentity string `json:"supervisor_identity,omitempty"`
	Token              string `json:"token,omitempty"`
	JobID              string `json:"job_id,omitempty"`
	ActiveProcesses    uint32 `json:"active_processes,omitempty"`
	TargetPGRP         int64  `json:"target_pgrp,omitempty"`
	TargetSession      int64  `json:"target_session,omitempty"`
	TargetStartTime    uint64 `json:"target_start_time,omitempty"`
	ExitCode           int    `json:"exit_code,omitempty"`
	ExitError          string `json:"exit_error,omitempty"`
	Error              string `json:"error,omitempty"`
}

func writeLinuxSupervisorFrame(w io.Writer, value any) error {
	if w == nil {
		return errors.New("linux supervisor frame writer is nil")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal linux supervisor frame: %w", err)
	}
	if len(payload)+1 > linuxSupervisorMaxFrame {
		return errLinuxSupervisorFrameTooLarge
	}
	payload = append(payload, '\n')
	for len(payload) > 0 {
		written, writeErr := w.Write(payload)
		if writeErr != nil {
			return fmt.Errorf("write linux supervisor frame: %w", writeErr)
		}
		if written <= 0 || written > len(payload) {
			return errors.New("write linux supervisor frame made no progress")
		}
		payload = payload[written:]
	}
	return nil
}

func readLinuxSupervisorFrame(reader *bufio.Reader, destination any) error {
	if reader == nil || destination == nil {
		return errors.New("linux supervisor frame reader and destination are required")
	}
	line, err := readLinuxSupervisorBoundedLine(reader)
	if err != nil {
		return fmt.Errorf("read linux supervisor frame: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(line[:len(line)-1]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode linux supervisor frame: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("linux supervisor frame has trailing JSON")
		}
		return fmt.Errorf("decode trailing linux supervisor frame data: %w", err)
	}
	return nil
}

func readLinuxSupervisorBoundedLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, linuxSupervisorMaxFrame)
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > linuxSupervisorMaxFrame {
			return nil, errLinuxSupervisorFrameTooLarge
		}
		line = append(line, part...)
		if err == nil {
			if len(line) == 0 {
				return nil, errors.New("linux supervisor frame is empty")
			}
			return line, nil
		}
		if err != bufio.ErrBufferFull {
			return nil, err
		}
	}
}

// resume executes the authorization check and the ptrace detach under one
// lease-state lock. A timer callback cannot latch expiry between the check and
// the detach operation.
func (state *linuxSupervisorLeaseState) resume(ownerLost func() bool, detach func() error) error {
	if state == nil || detach == nil {
		return ErrLinuxSupervisorLeaseExpired
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.latchExpiredLocked(time.Now())
	if !state.armed {
		return ErrLinuxSupervisorLeaseUnarmed
	}
	if state.stopped || state.ownerLost || state.expiredGen != 0 || (ownerLost != nil && ownerLost()) {
		return ErrLinuxSupervisorLeaseExpired
	}
	return detach()
}

type linuxSupervisorOwnerWatchdog struct {
	file      *os.File
	lost      chan struct{}
	done      chan struct{}
	onLost    func()
	mu        sync.Mutex
	disarmed  bool
	closeOnce sync.Once
	lostOnce  sync.Once
}

func newLinuxSupervisorOwnerWatchdog(file *os.File, onLost ...func()) *linuxSupervisorOwnerWatchdog {
	var callback func()
	if len(onLost) != 0 {
		callback = onLost[0]
	}
	watchdog := &linuxSupervisorOwnerWatchdog{file: file, lost: make(chan struct{}), done: make(chan struct{}), onLost: callback}
	go watchdog.watch()
	return watchdog
}

func (watchdog *linuxSupervisorOwnerWatchdog) watch() {
	defer close(watchdog.done)
	if watchdog == nil || watchdog.file == nil {
		return
	}
	var buffer [1]byte
	for {
		if _, err := watchdog.file.Read(buffer[:]); err != nil {
			watchdog.mu.Lock()
			disarmed := watchdog.disarmed
			onLost := watchdog.onLost
			watchdog.mu.Unlock()
			if !disarmed {
				watchdog.lostOnce.Do(func() {
					if onLost != nil {
						onLost()
					}
					close(watchdog.lost)
				})
			}
			return
		}
	}
}

func (watchdog *linuxSupervisorOwnerWatchdog) ownerLost() <-chan struct{} {
	if watchdog == nil {
		return nil
	}
	return watchdog.lost
}

func (watchdog *linuxSupervisorOwnerWatchdog) disarm() {
	if watchdog == nil {
		return
	}
	watchdog.mu.Lock()
	watchdog.disarmed = true
	watchdog.mu.Unlock()
	watchdog.closeOnce.Do(func() {
		if watchdog.file != nil {
			_ = watchdog.file.Close()
		}
	})
	select {
	case <-watchdog.done:
	case <-time.After(time.Second):
	}
}
