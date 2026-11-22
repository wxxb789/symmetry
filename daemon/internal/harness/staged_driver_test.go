package harness

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

type stagedTrace struct {
	mutex  sync.Mutex
	events []string
}

func (trace *stagedTrace) record(event string) {
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	trace.events = append(trace.events, event)
}

func (trace *stagedTrace) snapshot() []string {
	trace.mutex.Lock()
	defer trace.mutex.Unlock()
	return append([]string(nil), trace.events...)
}

type stagedFakeHooks struct {
	trace  *stagedTrace
	launch func(context.Context, execution.Invocation) error
	open   func(context.Context) (NativeSessionHandle, error)
	admit  func(context.Context, TurnRequest) error
	close  func(context.Context) error
}

func (hooks *stagedFakeHooks) LaunchTransport(ctx context.Context, invocation execution.Invocation) error {
	hooks.trace.record("launch")
	if hooks.launch == nil {
		return nil
	}
	return hooks.launch(ctx, invocation)
}

func (hooks *stagedFakeHooks) OpenNativeSession(ctx context.Context) (NativeSessionHandle, error) {
	hooks.trace.record("open")
	if hooks.open == nil {
		return NativeSessionHandle{ID: "handle", Filename: "/tmp/handle"}, nil
	}
	return hooks.open(ctx)
}

func (hooks *stagedFakeHooks) AdmitNativeTurn(ctx context.Context, request TurnRequest) error {
	hooks.trace.record("admit")
	if hooks.admit == nil {
		return nil
	}
	return hooks.admit(ctx, request)
}

func (hooks *stagedFakeHooks) CloseNativeSession(ctx context.Context) error {
	hooks.trace.record("close")
	if hooks.close == nil {
		return nil
	}
	return hooks.close(ctx)
}

func newStagedFakeDriver(trace *stagedTrace) (*StagedDriver, *stagedFakeHooks) {
	hooks := &stagedFakeHooks{trace: trace}
	return NewStagedDriver(hooks, &sync.Mutex{}), hooks
}

func functionPointer(value any) uintptr {
	if value == nil {
		return 0
	}
	return reflect.ValueOf(value).Pointer()
}

func TestStagedDriverLaunchDeliversTheGrantWithTheProcessDescription(t *testing.T) {
	persistProcess := func(int, string) error { return nil }
	persistAuthority := func(int, string, *authority.Supervisor) error { return nil }
	linuxGrant := &execution.LinuxV2Invocation{}
	var captured execution.Invocation
	trace := &stagedTrace{}
	driver, hooks := newStagedFakeDriver(trace)
	hooks.launch = func(_ context.Context, invocation execution.Invocation) error {
		captured = invocation
		return nil
	}
	grant := NewLaunchGrant(execution.Invocation{
		Program:                 "prepared-program",
		Args:                    []string{"prepared-arg"},
		Dir:                     "prepared-dir",
		Env:                     []string{"PREPARED=1"},
		InitialInput:            []byte("prepared-input"),
		CloseInputAfterInitial:  true,
		PersistProcess:          persistProcess,
		PersistProcessAuthority: persistAuthority,
		InitialLeaseSequence:    7,
		LinuxV2Required:         true,
		LinuxV2:                 linuxGrant,
	})
	err := driver.Launch(context.Background(), StartRequest{Grant: grant}, ProcessDescription{
		Program: "pi",
		Args:    []string{"--mode", "rpc"},
		Dir:     "/workspace",
		Env:     []string{"ADAPTER=1"},
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if captured.Program != "pi" || captured.Dir != "/workspace" {
		t.Fatalf("process description did not win: %+v", captured)
	}
	if len(captured.Args) != 2 || captured.Args[0] != "--mode" {
		t.Fatalf("args: %v", captured.Args)
	}
	if len(captured.Env) != 1 || captured.Env[0] != "ADAPTER=1" {
		t.Fatalf("env: %v", captured.Env)
	}
	if len(captured.InitialInput) != 0 || captured.CloseInputAfterInitial {
		t.Fatalf("staged launch forwards no legacy initial input: %+v", captured)
	}
	if functionPointer(captured.PersistProcess) != functionPointer(persistProcess) {
		t.Fatal("grant persistence callback was dropped")
	}
	if functionPointer(captured.PersistProcessAuthority) != functionPointer(persistAuthority) {
		t.Fatal("grant authority callback was dropped")
	}
	if captured.InitialLeaseSequence != 7 || !captured.LinuxV2Required || captured.LinuxV2 != linuxGrant {
		t.Fatalf("durable grant members were dropped: %+v", captured)
	}
}

func TestStagedDriverRequestWithoutGrantIsItsOwnGrant(t *testing.T) {
	requestPersist := func(int, string) error { return nil }
	nestedPersist := func(int, string) error { return nil }
	nestedReceipt := func(int, string, authority.StopReceipt) error { return nil }
	var captured execution.Invocation
	trace := &stagedTrace{}
	driver, hooks := newStagedFakeDriver(trace)
	hooks.launch = func(_ context.Context, invocation execution.Invocation) error {
		captured = invocation
		return nil
	}
	err := driver.Launch(context.Background(), StartRequest{
		Invocation: execution.Invocation{
			Program:                       "legacy",
			PersistProcess:                nestedPersist,
			PersistContainmentStopReceipt: nestedReceipt,
		},
		PersistProcess: requestPersist,
	}, ProcessDescription{Program: "generic"})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if captured.Program != "generic" {
		t.Fatalf("program: %q", captured.Program)
	}
	if functionPointer(captured.PersistProcess) != functionPointer(requestPersist) {
		t.Fatal("request-level persistence callback did not win")
	}
	if functionPointer(captured.PersistContainmentStopReceipt) != functionPointer(nestedReceipt) {
		t.Fatal("a nil request-level callback cleared the nested one")
	}
}

func TestStagedDriverOrdersLaunchPersistOpenBeforeAdmission(t *testing.T) {
	trace := &stagedTrace{}
	driver, hooks := newStagedFakeDriver(trace)
	hooks.launch = func(_ context.Context, invocation execution.Invocation) error {
		return invocation.PersistProcess(11, "identity")
	}
	grant := NewLaunchGrant(execution.Invocation{
		PersistProcess: func(int, string) error {
			trace.record("persist")
			return nil
		},
	})
	ctx := context.Background()
	if err := driver.Admit(ctx, TurnRequest{Goal: "before open"}); !errors.Is(err, ErrStagedSessionNotOpened) {
		t.Fatalf("admission before open: %v", err)
	}
	if got := trace.snapshot(); len(got) != 0 {
		t.Fatalf("the admit hook ran before the handle existed: %v", got)
	}
	if err := driver.Launch(ctx, StartRequest{Grant: grant}, ProcessDescription{Program: "pi"}); err != nil {
		t.Fatalf("launch: %v", err)
	}
	handle, err := driver.Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if handle.ID != "handle" {
		t.Fatalf("handle: %+v", handle)
	}
	if err := driver.Admit(ctx, TurnRequest{Goal: "after open"}); err != nil {
		t.Fatalf("admission after open: %v", err)
	}
	want := []string{"launch", "persist", "open", "admit"}
	if got := trace.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("phase order: %v, want %v", got, want)
	}
}

func TestStagedDriverOpenIsSingleFlightAndIdempotent(t *testing.T) {
	trace := &stagedTrace{}
	driver, hooks := newStagedFakeDriver(trace)
	entered := make(chan struct{})
	release := make(chan struct{})
	opens := 0
	hooks.open = func(context.Context) (NativeSessionHandle, error) {
		opens++
		close(entered)
		<-release
		return NativeSessionHandle{ID: "native"}, nil
	}
	type result struct {
		handle NativeSessionHandle
		err    error
	}
	first := make(chan result, 1)
	go func() {
		handle, err := driver.Open(context.Background())
		first <- result{handle: handle, err: err}
	}()
	<-entered
	if _, err := driver.Open(context.Background()); !errors.Is(err, ErrStagedSessionOpening) {
		t.Fatalf("concurrent open: %v", err)
	}
	close(release)
	joined := <-first
	if joined.err != nil || joined.handle.ID != "native" {
		t.Fatalf("first open: %+v", joined)
	}
	again, err := driver.Open(context.Background())
	if err != nil {
		t.Fatalf("repeat open: %v", err)
	}
	if again.ID != "native" || opens != 1 {
		t.Fatalf("repeat open called the hook again: %+v opens=%d", again, opens)
	}
}

func TestStagedDriverFailedOpenRecordsNoHandleAndStaysRefused(t *testing.T) {
	trace := &stagedTrace{}
	driver, hooks := newStagedFakeDriver(trace)
	sentinel := errors.New("native open refused")
	opens := 0
	hooks.open = func(context.Context) (NativeSessionHandle, error) {
		opens++
		return NativeSessionHandle{ID: "leaked"}, sentinel
	}
	ctx := context.Background()
	handle, err := driver.Open(ctx)
	if !errors.Is(err, sentinel) || handle.ID != "" {
		t.Fatalf("open: %+v %v", handle, err)
	}
	if driver.OpenedLocked() {
		t.Fatal("a failed open marked the session opened")
	}
	if err := driver.Admit(ctx, TurnRequest{}); !errors.Is(err, ErrStagedSessionNotOpened) {
		t.Fatalf("admission after a failed open: %v", err)
	}
	if _, err := driver.Open(ctx); !errors.Is(err, sentinel) || opens != 2 {
		t.Fatalf("a failed open was not retried: opens=%d err=%v", opens, err)
	}
}

func TestStagedDriverHookFailuresSurfaceUnchanged(t *testing.T) {
	sentinel := errors.New("hook failed")
	cases := map[string]func(driver *StagedDriver, hooks *stagedFakeHooks, ctx context.Context) error{
		"launch": func(driver *StagedDriver, hooks *stagedFakeHooks, ctx context.Context) error {
			return driver.Launch(ctx, StartRequest{}, ProcessDescription{})
		},
		"open": func(driver *StagedDriver, _ *stagedFakeHooks, ctx context.Context) error {
			_, err := driver.Open(ctx)
			return err
		},
		"admit": func(driver *StagedDriver, _ *stagedFakeHooks, ctx context.Context) error {
			if _, err := driver.Open(ctx); err != nil {
				return err
			}
			return driver.Admit(ctx, TurnRequest{})
		},
		"close": func(driver *StagedDriver, _ *stagedFakeHooks, ctx context.Context) error {
			return driver.Close(ctx)
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			trace := &stagedTrace{}
			driver, hooks := newStagedFakeDriver(trace)
			failing := func(context.Context) error { return sentinel }
			hooks.launch = func(context.Context, execution.Invocation) error { return sentinel }
			hooks.open = func(context.Context) (NativeSessionHandle, error) { return NativeSessionHandle{}, sentinel }
			hooks.admit = func(context.Context, TurnRequest) error { return sentinel }
			hooks.close = failing
			err := call(driver, hooks, context.Background())
			if err != sentinel {
				t.Fatalf("%s surfaced %v, want the hook error verbatim", name, err)
			}
		})
	}
}

func TestStagedDriverCloseIsSingleFlightAndPermanentOnlyForBareMarker(t *testing.T) {
	permanent := errors.New("terminate failed")
	trace := &stagedTrace{}
	driver, hooks := newStagedFakeDriver(trace)
	attempts := 0
	hooks.close = func(context.Context) error {
		attempts++
		return PermanentCloseFailure(permanent)
	}
	ctx := context.Background()
	if err := driver.Close(ctx); err != permanent {
		t.Fatalf("permanent close: %v", err)
	}
	if err := driver.Close(ctx); err != permanent || attempts != 1 {
		t.Fatalf("a permanent failure was retried: err=%v attempts=%d", err, attempts)
	}

	wrappedTrace := &stagedTrace{}
	wrappedDriver, wrappedHooks := newStagedFakeDriver(wrappedTrace)
	wrappedAttempts := 0
	wrappedHooks.close = func(context.Context) error {
		wrappedAttempts++
		return fmt.Errorf("attempt wrapper: %w", PermanentCloseFailure(permanent))
	}
	if err := wrappedDriver.Close(ctx); !errors.Is(err, permanent) {
		t.Fatalf("wrapped marker: %v", err)
	}
	if err := wrappedDriver.Close(ctx); !errors.Is(err, permanent) || wrappedAttempts != 2 {
		t.Fatalf("a wrapped marker must stay retryable: attempts=%d err=%v", wrappedAttempts, err)
	}
}

func TestStagedDriverCloseDetachedRunsStartOnTheCaller(t *testing.T) {
	trace := &stagedTrace{}
	driver, hooks := newStagedFakeDriver(trace)
	attemptEntered := make(chan struct{})
	release := make(chan struct{})
	hooks.close = func(context.Context) error {
		close(attemptEntered)
		<-release
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var duringStart []string
	start := func() {
		trace.record("start")
		duringStart = trace.snapshot()
	}
	result := make(chan error, 1)
	go func() { result <- driver.CloseDetached(ctx, time.Second, start) }()
	<-attemptEntered
	if len(duringStart) != 1 || duringStart[0] != "start" {
		t.Fatalf("start did not run on the caller before the detached attempt: %v", duringStart)
	}
	close(release)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("a departed caller reports its own cancellation: %v", err)
	}
	wait := time.Now().Add(2 * time.Second)
	for time.Now().Before(wait) {
		if got := trace.snapshot(); len(got) == 2 && got[1] == "close" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the detached attempt did not complete: %v", trace.snapshot())
}
