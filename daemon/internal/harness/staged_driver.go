package harness

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

// LaunchGrant is one opaque durable launch authority for a staged session. The
// app merges the invocation members and the request-level persistence callbacks
// into it; an adapter hands it back to the driver without reading it.
type LaunchGrant struct {
	invocation execution.Invocation
	granted    bool
}

// NewLaunchGrant records the app's merged launch authority.
func NewLaunchGrant(invocation execution.Invocation) LaunchGrant {
	return LaunchGrant{invocation: invocation, granted: true}
}

// grant resolves one request's launch authority. A request that carries no grant
// is its own grant: the legacy members stay the frozen OpenCode adapter's input
// and keep direct-construction callers unchanged.
func (request StartRequest) grant() LaunchGrant {
	if request.Grant.granted {
		return request.Grant
	}
	invocation := request.Invocation
	if request.PersistProcess != nil {
		invocation.PersistProcess = request.PersistProcess
	}
	if request.PersistProcessAuthority != nil {
		invocation.PersistProcessAuthority = request.PersistProcessAuthority
	}
	if request.PersistContainmentStopReceipt != nil {
		invocation.PersistContainmentStopReceipt = request.PersistContainmentStopReceipt
	}
	return LaunchGrant{invocation: invocation}
}

func (grant LaunchGrant) merged(description ProcessDescription) execution.Invocation {
	invocation := grant.invocation
	invocation.Program = description.Program
	invocation.Args = description.Args
	invocation.Dir = description.Dir
	invocation.Env = description.Env
	invocation.InitialInput = description.InitialInput
	invocation.CloseInputAfterInitial = description.CloseInputAfterInitial
	return invocation
}

// ProcessDescription is everything an adapter decides about the process. Durable
// launch authority is not part of it.
type ProcessDescription struct {
	Program                string
	Args                   []string
	Dir                    string
	Env                    []string
	InitialInput           []byte
	CloseInputAfterInitial bool
}

// StagedHooks is the one adapter-supplied interface the staged driver calls.
// Adapter state machines stay behind these methods.
type StagedHooks interface {
	// LaunchTransport starts this adapter's transport process, refuses a
	// typed-nil owner, and installs the process under the adapter's delivery
	// lock discipline. The runner persists the process identity through the
	// invocation callbacks before it exposes the process.
	LaunchTransport(ctx context.Context, invocation execution.Invocation) error
	// OpenNativeSession runs this adapter's native handshake and returns the
	// handle the driver records only on success.
	OpenNativeSession(ctx context.Context) (NativeSessionHandle, error)
	// AdmitNativeTurn writes this adapter's native turn.
	AdmitNativeTurn(ctx context.Context, request TurnRequest) error
	// CloseNativeSession runs this adapter's bounded cleanup and returns a bare
	// PermanentCloseFailure result when the failure cannot be retried.
	CloseNativeSession(ctx context.Context) error
}

// The driver returns these refusals bare; each adapter maps them to its own text.
var (
	ErrStagedSessionOpening   = errors.New("staged native session is already opening")
	ErrStagedSessionNotOpened = errors.New("staged native session has not opened its native handle")
)

// StagedDriver owns the staged ordering of one session: the launch that persists
// the process identity inside the runner, the recorded native handle, the
// admitted turn, and the single-flight close. It takes the session's own mutex,
// as ProcessForwarder takes the session's process field.
type StagedDriver struct {
	hooks StagedHooks
	mutex *sync.Mutex

	opening   bool
	opened    bool
	handle    NativeSessionHandle
	closeGate CloseGate
}

func NewStagedDriver(hooks StagedHooks, mutex *sync.Mutex) *StagedDriver {
	return &StagedDriver{hooks: hooks, mutex: mutex}
}

// Launch merges the request's grant with the adapter's process description and
// runs the one transport launch of this session.
func (driver *StagedDriver) Launch(ctx context.Context, request StartRequest, description ProcessDescription) error {
	return driver.hooks.LaunchTransport(ctx, request.grant().merged(description))
}

// Open records the opaque handle only after the native handshake succeeded; an
// opened session returns the recorded handle and a concurrent open is refused.
func (driver *StagedDriver) Open(ctx context.Context) (NativeSessionHandle, error) {
	driver.mutex.Lock()
	if driver.opened {
		handle := driver.handle
		driver.mutex.Unlock()
		return handle, nil
	}
	if driver.opening {
		driver.mutex.Unlock()
		return NativeSessionHandle{}, ErrStagedSessionOpening
	}
	driver.opening = true
	driver.mutex.Unlock()
	handle, err := driver.hooks.OpenNativeSession(ctx)
	driver.mutex.Lock()
	driver.opening = false
	if err == nil {
		driver.handle, driver.opened = handle, true
	}
	driver.mutex.Unlock()
	if err != nil {
		return NativeSessionHandle{}, err
	}
	return handle, nil
}

// Admit runs the one turn admission of this session, and only after the native
// handle was recorded.
func (driver *StagedDriver) Admit(ctx context.Context, request TurnRequest) error {
	driver.mutex.Lock()
	opened := driver.opened
	driver.mutex.Unlock()
	if !opened {
		return ErrStagedSessionNotOpened
	}
	return driver.hooks.AdmitNativeTurn(ctx, request)
}

// OpenedLocked reports the recorded-handle latch to a caller that already holds
// the session mutex.
func (driver *StagedDriver) OpenedLocked() bool { return driver.opened }

// OpeningLocked reports the in-flight open latch to a caller that already holds
// the session mutex.
func (driver *StagedDriver) OpeningLocked() bool { return driver.opening }

// Close runs at most one in-flight shutdown on the caller's context.
func (driver *StagedDriver) Close(ctx context.Context) error {
	return driver.closeGate.Run(ctx, driver.closeAttempt)
}

// CloseDetached runs the attempt under context.WithoutCancel plus budget, so a
// caller that leaves cannot abandon it; start runs on the caller before the
// detached goroutine starts.
func (driver *StagedDriver) CloseDetached(ctx context.Context, budget time.Duration, start func()) error {
	return driver.closeGate.RunDetached(ctx, budget, start, driver.closeAttempt)
}

// Only a bare PermanentCloseFailure is permanent; the gate interprets the marker.
func (driver *StagedDriver) closeAttempt(ctx context.Context) error {
	return driver.hooks.CloseNativeSession(ctx)
}
