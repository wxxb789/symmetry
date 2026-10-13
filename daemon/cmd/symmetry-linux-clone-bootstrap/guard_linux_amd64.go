//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"golang.org/x/sys/unix"
)

const cloneBootstrapLimit = 2 * time.Minute

var (
	errOwnerStopped = errors.New("A checkout owner exited during B bootstrap")
	errLeaseExpired = errors.New("acknowledged RELEASE lease expired during B bootstrap")
)

func monotonicNowNS() (uint64, error) {
	var now unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &now); err != nil {
		return 0, fmt.Errorf("read CLOCK_MONOTONIC: %w", err)
	}
	if now.Sec < 0 || now.Nsec < 0 || now.Nsec >= 1_000_000_000 {
		return 0, errors.New("CLOCK_MONOTONIC returned an invalid timespec")
	}
	const nanosecondsPerSecond = uint64(1_000_000_000)
	seconds := uint64(now.Sec)
	if seconds > ^uint64(0)/nanosecondsPerSecond {
		return 0, errors.New("CLOCK_MONOTONIC seconds overflow")
	}
	baseNS := seconds * nanosecondsPerSecond
	nanoseconds := uint64(now.Nsec)
	if nanoseconds > ^uint64(0)-baseNS {
		return 0, errors.New("CLOCK_MONOTONIC nanoseconds overflow")
	}
	return baseNS + nanoseconds, nil
}

func addDurationNS(base uint64, duration time.Duration) (uint64, error) {
	if duration <= 0 {
		return 0, errors.New("monotonic duration must be positive")
	}
	delta := uint64(duration.Nanoseconds())
	if delta > ^uint64(0)-base {
		return 0, errors.New("monotonic deadline addition overflows uint64")
	}
	return base + delta, nil
}

type ownerLeaseGuard struct {
	ctx         context.Context
	cancel      context.CancelCauseFunc
	stop        chan struct{}
	done        chan struct{}
	ownerPIDFD  int
	ownerPipeFD int
	deadlineNS  uint64
}

func newOwnerLeaseGuard(ownerPIDFD, ownerPipeFD int, leaseDeadlineNS uint64) (*ownerLeaseGuard, error) {
	nowNS, err := monotonicNowNS()
	if err != nil {
		return nil, err
	}
	if leaseDeadlineNS <= nowNS {
		return nil, errLeaseExpired
	}
	limitNS, err := addDurationNS(nowNS, cloneBootstrapLimit)
	if err != nil {
		return nil, err
	}
	if leaseDeadlineNS < limitNS {
		limitNS = leaseDeadlineNS
	}
	if err := checkOwnerDescriptors(ownerPIDFD, ownerPipeFD); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	guard := &ownerLeaseGuard{
		ctx: ctx, cancel: cancel, stop: make(chan struct{}), done: make(chan struct{}),
		ownerPIDFD: ownerPIDFD, ownerPipeFD: ownerPipeFD, deadlineNS: limitNS,
	}
	go guard.monitor()
	return guard, nil
}

func (guard *ownerLeaseGuard) check() error {
	if guard == nil {
		return errors.New("clone bootstrap owner guard is unavailable")
	}
	if guard.ctx.Err() != nil {
		return guard.cause()
	}
	nowNS, err := monotonicNowNS()
	if err != nil {
		guard.cancel(err)
		return err
	}
	if nowNS >= guard.deadlineNS {
		guard.cancel(errLeaseExpired)
		return errLeaseExpired
	}
	if err := checkOwnerDescriptors(guard.ownerPIDFD, guard.ownerPipeFD); err != nil {
		guard.cancel(err)
		return err
	}
	return nil
}

func (guard *ownerLeaseGuard) remaining() (time.Duration, error) {
	if err := guard.check(); err != nil {
		return 0, err
	}
	nowNS, err := monotonicNowNS()
	if err != nil {
		guard.cancel(err)
		return 0, err
	}
	delta := guard.deadlineNS - nowNS
	if delta > uint64(math.MaxInt64) {
		return 0, errors.New("remaining clone bootstrap lease overflows time.Duration")
	}
	return time.Duration(delta), nil
}

func (guard *ownerLeaseGuard) cause() error {
	if cause := context.Cause(guard.ctx); cause != nil {
		return cause
	}
	if guard.ctx.Err() != nil {
		return guard.ctx.Err()
	}
	return errors.New("clone bootstrap owner guard was canceled")
}

func (guard *ownerLeaseGuard) monitor() {
	defer close(guard.done)
	for {
		select {
		case <-guard.stop:
			return
		default:
		}
		remaining, err := guard.remainingWithoutCheck()
		if err != nil {
			guard.cancel(err)
			return
		}
		pollFor := 100 * time.Millisecond
		if remaining < pollFor {
			pollFor = remaining
		}
		millis := int((pollFor + time.Millisecond - 1) / time.Millisecond)
		fds := []unix.PollFd{
			{Fd: int32(guard.ownerPIDFD), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR},
			{Fd: int32(guard.ownerPipeFD), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR},
		}
		count, err := unix.Poll(fds, millis)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			guard.cancel(fmt.Errorf("poll A owner-liveness descriptors: %w", err))
			return
		}
		if count != 0 && (fds[0].Revents != 0 || fds[1].Revents != 0) {
			guard.cancel(errOwnerStopped)
			return
		}
	}
}

func (guard *ownerLeaseGuard) remainingWithoutCheck() (time.Duration, error) {
	if guard.ctx.Err() != nil {
		return 0, guard.cause()
	}
	nowNS, err := monotonicNowNS()
	if err != nil {
		return 0, err
	}
	if nowNS >= guard.deadlineNS {
		return 0, errLeaseExpired
	}
	delta := guard.deadlineNS - nowNS
	if delta > uint64(math.MaxInt64) {
		return 0, errors.New("remaining clone bootstrap lease overflows time.Duration")
	}
	return time.Duration(delta), nil
}

func (guard *ownerLeaseGuard) close() {
	if guard == nil {
		return
	}
	select {
	case <-guard.done:
		return
	default:
	}
	close(guard.stop)
	guard.cancel(context.Canceled)
	<-guard.done
}

func checkOwnerDescriptors(ownerPIDFD, ownerPipeFD int) error {
	fds := []unix.PollFd{
		{Fd: int32(ownerPIDFD), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR},
		{Fd: int32(ownerPipeFD), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR},
	}
	count, err := unix.Poll(fds, 0)
	if err != nil {
		return fmt.Errorf("poll A owner-liveness descriptors: %w", err)
	}
	if count != 0 && (fds[0].Revents != 0 || fds[1].Revents != 0) {
		return errOwnerStopped
	}
	return nil
}
