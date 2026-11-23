package harness

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

type LaunchGrant struct {
	invocation execution.Invocation
	granted    bool
}

func NewLaunchGrant(invocation execution.Invocation) LaunchGrant {
	return LaunchGrant{invocation: invocation, granted: true}
}

func (request StartRequest) grant() LaunchGrant {
	if request.Grant.granted {
		return request.Grant
	}
	// A direct construction with no grant folds the three request-level members
	// onto the frozen invocation members; the runner reads those only when the
	// invocation carries no recorder (W6-04 deletes them, docs/TODO.md).
	invocation := request.Invocation
	invocation.PersistProcess = request.PersistProcess
	invocation.PersistProcessAuthority = request.PersistProcessAuthority
	invocation.PersistContainmentStopReceipt = request.PersistContainmentStopReceipt
	return LaunchGrant{invocation: invocation}
}

// PopulateOpencodeRecorderMembers mirrors one request's recorder onto the frozen
// members daemon/internal/harness/opencode/adapter.go reads: the invocation's
// recorder members and the three request-level persist members. W6-04 deletes
// this with them (docs/TODO.md).
func PopulateOpencodeRecorderMembers(request StartRequest) StartRequest {
	request.Invocation.MirrorRecorderMembers()
	recorder := request.Invocation.Recorder
	if recorder == nil {
		return request
	}
	request.PersistProcess = request.Invocation.PersistProcess
	request.PersistProcessAuthority = request.Invocation.PersistProcessAuthority
	request.PersistContainmentStopReceipt = request.Invocation.PersistContainmentStopReceipt
	return request
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

type ProcessDescription struct {
	Program                string
	Args                   []string
	Dir                    string
	Env                    []string
	InitialInput           []byte
	CloseInputAfterInitial bool
}

type StagedHooks interface {
	// The runner persists the process identity through the invocation callbacks
	// before it exposes the process.
	LaunchTransport(ctx context.Context, invocation execution.Invocation) error
	OpenNativeSession(ctx context.Context) (NativeSessionHandle, error)
	AdmitNativeTurn(ctx context.Context, request TurnRequest) error
	// CloseNativeSession returns a bare PermanentCloseFailure result when the
	// failure cannot be retried; a wrapped marker stays retryable.
	CloseNativeSession(ctx context.Context) error
}

var (
	ErrStagedSessionOpening   = errors.New("staged native session is already opening")
	ErrStagedSessionNotOpened = errors.New("staged native session has not opened its native handle")
)

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

func (driver *StagedDriver) Launch(ctx context.Context, request StartRequest, description ProcessDescription) error {
	return driver.hooks.LaunchTransport(ctx, request.grant().merged(description))
}

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

func (driver *StagedDriver) Admit(ctx context.Context, request TurnRequest) error {
	driver.mutex.Lock()
	opened := driver.opened
	driver.mutex.Unlock()
	if !opened {
		return ErrStagedSessionNotOpened
	}
	return driver.hooks.AdmitNativeTurn(ctx, request)
}

func (driver *StagedDriver) OpenedLocked() bool { return driver.opened }

func (driver *StagedDriver) OpeningLocked() bool { return driver.opening }

func (driver *StagedDriver) Close(ctx context.Context) error {
	return driver.closeGate.Run(ctx, driver.hooks.CloseNativeSession)
}

func (driver *StagedDriver) CloseDetached(ctx context.Context, budget time.Duration, start func()) error {
	return driver.closeGate.RunDetached(ctx, budget, start, driver.hooks.CloseNativeSession)
}
