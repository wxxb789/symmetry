// Package execution launches and supervises configured coding-agent processes.
package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const (
	readChunkSize             = 32 * 1024
	eventQueueCapacity        = 64
	defaultTerminationGrace   = 5 * time.Second
	linuxV2OutputDrainTimeout = 5 * time.Second
	linuxV2OutputCloseWait    = 250 * time.Millisecond
	resumeBarrierTimeout      = 30 * time.Second
	resumeBarrierPollInterval = 25 * time.Millisecond
)

const (
	resumeBarrierWitnessGateEnvironment       = "SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS"
	commitResumeBarrierReachedEnvironment     = "SYMMETRY_RUNNER_COMMIT_RESUME_BARRIER_REACHED_FILE"
	commitResumeBarrierReleaseEnvironment     = "SYMMETRY_RUNNER_COMMIT_RESUME_BARRIER_RELEASE_FILE"
	leaseArmedResumeBarrierReachedEnvironment = "SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_REACHED_FILE"
	leaseArmedResumeBarrierReleaseEnvironment = "SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_RELEASE_FILE"
)

// ErrInputClosed reports that the process input transport can no longer accept
// a complete write.
var ErrInputClosed = errors.New("process standard input is closed")

var errContainmentMarkerPending = errors.New("process marker persistence is pending")

// Stream identifies the source of an output event.
type Stream string

const (
	// Stdout is output emitted on the child process's standard output.
	Stdout Stream = "stdout"
	// Stderr is output emitted on the child process's standard error.
	Stderr Stream = "stderr"
)

// Event is one ordered chunk of process output. Data is owned by the event and
// remains valid after Handle returns.
type Event struct {
	Stream   Stream
	Sequence uint64
	At       time.Time
	Data     []byte
}

// Sink receives output events on a dedicated delivery goroutine. Handle calls
// are strictly ordered by Event.Sequence.
type Sink interface {
	Handle(context.Context, Event) error
}

// SinkFunc adapts a function into a Sink.
type SinkFunc func(context.Context, Event) error

// Handle calls the wrapped function.
func (function SinkFunc) Handle(ctx context.Context, event Event) error {
	return function(ctx, event)
}

// Invocation describes one direct process launch. Program may be an absolute
// path or a trusted configured executable name resolved through exec.LookPath;
// it is never interpreted by a shell. Env is always passed to the child
// verbatim, including when it is empty, so the daemon environment is never
// inherited implicitly.
type Invocation struct {
	Program                string
	Args                   []string
	Dir                    string
	Env                    []string
	InitialInput           []byte
	CloseInputAfterInitial bool
	// LinuxV2Required forbids the legacy Linux process-group and direct-Go-test
	// launch paths. LinuxV2 supplies the exact RunJournal grant and App-owned
	// typed journal callbacks. A non-nil LinuxV2 also requests this path.
	LinuxV2Required bool
	LinuxV2         *LinuxV2Invocation
	// PrepareSupervisorHandoff durably records the exact launch binding before
	// a Windows containment helper can outlive the daemon.
	PrepareSupervisorHandoff func(authority.SupervisorHandoff) error
	// BindSupervisorHandoff records the exact helper PID and creation identity
	// after the inherited pre-auth channel has identified the helper.
	BindSupervisorHandoff func(authority.SupervisorHandoff, int, string) error
	// CommitSupervisorHandoff promotes a fully bound handoff into the normal
	// process marker and containment authority before resume or lease arm.
	CommitSupervisorHandoff func(authority.SupervisorHandoff, time.Time) error
	// RecordSupervisorHandoffStopReceipt persists a stop proof for a pending
	// handoff before its helper is released.
	RecordSupervisorHandoffStopReceipt func(authority.SupervisorHandoff, authority.StopReceipt) error
	// ClearSupervisorHandoff removes a pending handoff only after release proof.
	ClearSupervisorHandoff func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error
	// PersistProcess runs immediately after the OS process identity is
	// captured, before output readers or the Process value are exposed.
	PersistProcess func(pid int, identity string) error
	// PersistProcessWithAuthority atomically persists the process marker and its
	// independently releasable containment authority. When supplied for an
	// authority-capable containment owner, Runner uses it instead of the two
	// legacy persistence callbacks so recovery cannot observe a marker without
	// the authority required to release that process.
	PersistProcessWithAuthority func(pid int, identity string, value *authority.Supervisor) error
	// PersistProcessAuthority runs immediately after PersistProcess for a
	// supervisor-backed containment owner. Legacy callers may leave it nil.
	PersistProcessAuthority func(pid int, identity string, value *authority.Supervisor) error
	// PersistContainmentStopReceipt runs after the process containment boundary
	// proves an empty tree and before an independent supervisor is released.
	// A failure retains that helper authority for retry or restart recovery.
	PersistContainmentStopReceipt func(pid int, identity string, receipt authority.StopReceipt) error
	// PersistContainmentUnproven retains the exact process marker when local
	// containment cleanup cannot prove the complete owned boundary stopped.
	PersistContainmentUnproven func(pid int, identity string) error
	// InitialLeaseDeadline arms an optional independent containment watchdog
	// after local authority persistence and before initial input is written. A
	// zero value leaves legacy/test containment unchanged. When
	// InitialLeaseDeadlineAt is set, it is authoritative and this field is only
	// retained as the relative-duration fallback for older callers.
	InitialLeaseDeadline time.Duration
	// InitialLeaseDeadlineAt is the local monotonic deadline established at the
	// claim request start. The runner recomputes the remaining duration immediately
	// before arming the watchdog so attach, helper handshakes and persistence can
	// only consume lease time.
	InitialLeaseDeadlineAt time.Time
	InitialLeaseSequence   uint64
}

// LinuxV2JournalCallbacks are typed App-owned mutations over the single
// existing RunJournal. Each callback is scoped to one RunKey/fence by its
// caller; execution never opens or writes state.Store itself.
type LinuxV2JournalCallbacks struct {
	Reserve                  func(state.LinuxCgroupReservation) error
	MarkCreateStarted        func(state.LinuxCgroupReservation) error
	BindLeaf                 func(state.LinuxCgroupReservation, authority.LinuxCgroupIdentity) error
	BindHelper               func(state.LinuxCgroupReservation, authority.LinuxCgroupProcessIdentity) error
	BindLauncher             func(state.LinuxCgroupReservation, authority.LinuxCgroupProcessIdentity) error
	ArmLauncherGrant         func(state.LinuxCgroupReservation) error
	RecordLauncherExit       func(state.LinuxCgroupReservation, authority.LinuxCgroupExitProof) error
	RecordHelperExit         func(state.LinuxCgroupReservation, authority.LinuxCgroupExitProof) error
	BindGate                 func(state.LinuxCgroupReservation, authority.LinuxCgroupGateIdentity) error
	PrepareHandoff           func(authority.SupervisorHandoff) error
	BindHandoffV2            func(authority.SupervisorHandoff, authority.LinuxCgroupProcessIdentity, authority.LinuxCgroupProcessIdentity) error
	CommitHandoff            func(authority.SupervisorHandoff, time.Time) error
	RecordStopReceipt        func(authority.SupervisorHandoff, authority.StopReceipt) error
	RecordPreGateStopReceipt func(state.LinuxCgroupReservation, authority.LinuxCgroupPreGateStopReceipt) error
	AbortReservation         func(state.LinuxCgroupReservation, authority.LinuxCgroupReservationAbortProof) error
	ClearHandoffV2           func(authority.SupervisorHandoff, platform.LinuxCgroupV2Release) error
	ClearProcessDetailsV2    func(int, string, platform.LinuxCgroupV2Release) error
	ClearReservationV2       func(state.LinuxCgroupReservation, platform.LinuxCgroupV2Release) error
	// RecordLinuxCloneCheckoutReady returns to the helper only after Store has
	// durably bound the exact B-created checkout; it gates native exec.
	RecordLinuxCloneCheckoutReady func(authority.LinuxCloneCheckoutReceipt) error
	// LinuxCloneProposedCommit distinguishes a candidate from failed/cancelled
	// native results. A false result requires durable workspace retention.
	LinuxCloneProposedCommit   func(context.Context, LinuxCloneRunOutcome) (commit string, proposed bool, err error)
	RetainUnpromotedLinuxClone func(context.Context) error
	// PromoteLinuxClone runs A-owned validation/promotion only after physical
	// stop proof and receipt-bound opaque leaf removal.
	PromoteLinuxClone func(context.Context, authority.LinuxCloneGrant, authority.LinuxCloneCheckoutReceipt,
		string, authority.StopReceipt, platform.LinuxCgroupV2Release) (authority.LinuxClonePromotionReceipt, error)
	// RecordLinuxClonePromotion durably records the exact promoted A-owned ref
	// before containment clear or terminal settlement.
	RecordLinuxClonePromotion func(authority.LinuxClonePromotionReceipt) error
	// FinalizeNoCheckoutLinuxCloneAfterRetirement removes only an exact
	// no-checkout clone after the corresponding no-producer cgroup-abort fence
	// or pre-gate release and any required helper/anchor retirement.
	FinalizeNoCheckoutLinuxCloneAfterRetirement func(context.Context, authority.LinuxCloneIntent,
		*authority.LinuxCloneSeedReceipt, *authority.LinuxCgroupPreGateReleaseProof,
		*authority.LinuxCgroupReservationAbortProof) error
}

// LinuxCloneRunOutcome is Runner's independent observation of the B process
// before App considers an A-owned promotion. It deliberately does not use the
// adapter's eventual Wait result, which may depend on Runner finalization.
type LinuxCloneRunOutcome struct {
	ExitCode              int
	WaitError             error
	Terminated            bool
	RunContextCancelled   bool
	TerminationError      error
	SinkError             error
	OutputError           error
	OutputTruncated       bool
	PriorContainmentError error
	StopReceiptValid      bool
	LeafRemovalProven     bool
}

func (outcome LinuxCloneRunOutcome) PromotionEligible() bool {
	return outcome.ExitCode == 0 && outcome.WaitError == nil && !outcome.Terminated &&
		!outcome.RunContextCancelled && outcome.TerminationError == nil && outcome.SinkError == nil &&
		outcome.OutputError == nil && !outcome.OutputTruncated && outcome.PriorContainmentError == nil &&
		outcome.StopReceiptValid && outcome.LeafRemovalProven
}

// LinuxV2Invocation is the one-run grant supplied by App. Parent and Anchor
// are borrowed long-lived A-owned handles; execution owns only the leaf,
// helper, gate pidfd and per-run communication handles created from them.
// Platform placeholders keep this API source-compatible on unsupported OSes;
// Runner rejects a requested launch there before any process side effect.
type LinuxV2Invocation struct {
	RunKey           state.RunKey
	Fence            protocol.Fence
	Parent           *platform.LinuxCgroupV2Parent
	Anchor           *platform.LinuxOwnerAnchorClient
	RecoveryEndpoint string
	// CloneGrant binds the one-use gate RELEASE to the A-authorized standalone
	// B-owned checkout. It is never placed in argv or environment.
	CloneGrant *authority.LinuxCloneGrant
	Callbacks  LinuxV2JournalCallbacks
}

func durableSupervisorHandoffRequested(invocation Invocation) bool {
	return invocation.PrepareSupervisorHandoff != nil ||
		invocation.BindSupervisorHandoff != nil ||
		invocation.CommitSupervisorHandoff != nil ||
		invocation.RecordSupervisorHandoffStopReceipt != nil ||
		invocation.ClearSupervisorHandoff != nil
}

func (invocation Invocation) linuxV2Requested() bool {
	return invocation.LinuxV2Required || invocation.LinuxV2 != nil
}

func durableSupervisorHandoffCallbacksComplete(invocation Invocation) bool {
	return invocation.PrepareSupervisorHandoff != nil &&
		invocation.BindSupervisorHandoff != nil &&
		invocation.CommitSupervisorHandoff != nil
}

// LeaseRenewer is an optional process capability. The app uses a type
// assertion so legacy and unsupported-platform process fakes do not acquire a
// second required lifecycle method.
type LeaseRenewer interface {
	RenewLease(deadline time.Duration, sequence uint64) error
	LeaseRenewalAvailable() bool
}

// Runner starts coding-agent process invocations.
type Runner struct {
	configureProcess   func(*exec.Cmd) error
	attachProcess      func(*os.Process) (platform.Containment, string, error)
	launchProcess      processLauncher
	now                func() time.Time
	outputDrainTimeout time.Duration
}

// containmentUnprovenCallbackSetter is implemented only by containment owners
// that can observe an unresolved descendant boundary before Close. Keeping the
// seam consumer-side lets Linux add the early durable marker without changing
// the common Containment contract or the Windows implementation.
type containmentUnprovenCallbackSetter interface {
	SetContainmentUnprovenCallback(func() error) error
}

// startedProcess is the small lifecycle boundary shared by the standard
// library launcher and the Windows native suspended launcher. The latter does
// not have an exec.Cmd-compatible ProcessState, so the process lifecycle is
// deliberately carried by functions instead of a second process abstraction.
type startedProcess struct {
	command                              *exec.Cmd
	pid                                  int
	identity                             string
	peerMutex                            sync.Mutex
	duplicateLinuxV2Peer                 func() (*LinuxV2Peer, error)
	closeLinuxV2Containment              func() error
	stopLinuxV2BeforeOutputDrain         func() error
	deferLinuxV2FinalizeUntilOutputDrain bool
	recordLinuxV2CloneOutcome            func(LinuxCloneRunOutcome)
	renewLinuxV2Lease                    func(context.Context, time.Duration, uint64) error
	containment                          platform.Containment
	containmentHandoff                   *authority.SupervisorHandoff
	containmentHandoffStartedAt          time.Time
	containmentHandoffCommitted          bool
	containmentHandoffPrepareUnknown     bool
	containmentHandoffBindUnknown        bool
	containmentHandoffCommitUnknown      bool
	commitSupervisorHandoff              func(authority.SupervisorHandoff, time.Time) error
	recordSupervisorHandoffStopReceipt   func(authority.SupervisorHandoff, authority.StopReceipt) error
	clearSupervisorHandoff               func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error
	persistProcessWithAuthority          func(int, string, *authority.Supervisor) error
	persistAuthority                     func(int, string, *authority.Supervisor) error
	containmentAuthority                 *authority.Supervisor
	persistStopReceipt                   func(int, string, authority.StopReceipt) error
	persistContainmentUnproven           func(int, string) error
	containmentUnprovenCallback          func() error
	stopReceiptRequired                  bool
	containmentAuthorityUncertain        bool
	wait                                 func() (int, error)
	kill                                 func() error
	close                                func() error
	resume                               func() error
}

type processLauncher func(
	ctx context.Context,
	command *exec.Cmd,
	invocation Invocation,
	stdinRead, stdoutWrite, stderrWrite *os.File,
) (*startedProcess, error)

var errNativeProcessLauncherUnavailable = errors.New("native suspended process launcher is unavailable")

// NewRunner creates a process runner.
func NewRunner() Runner {
	return Runner{
		configureProcess: platform.ConfigureProcess,
		attachProcess:    platform.AttachProcess,
		launchProcess:    defaultProcessLauncher,
		now:              time.Now,
	}
}

// Process is a running child process. Its fixed-size event queue decouples pipe
// draining from Sink latency. When the queue is full, readers apply bounded
// backpressure; Terminate remains independent of sink delivery and can still
// stop the entire process tree.
type Process struct {
	// PID is the operating-system process identifier of the launched agent.
	PID int
	// Identity combines the PID with platform process-creation data so a later
	// daemon instance can reject a recycled PID after restart.
	Identity string

	command                            *exec.Cmd
	backend                            *startedProcess
	outputDrainTimeout                 time.Duration
	sink                               Sink
	containment                        platform.Containment
	containmentHandoff                 *authority.SupervisorHandoff
	containmentHandoffCommitted        bool
	containmentHandoffPrepareUnknown   bool
	containmentHandoffBindUnknown      bool
	containmentHandoffCommitUnknown    bool
	commitSupervisorHandoff            func(authority.SupervisorHandoff, time.Time) error
	recordSupervisorHandoffStopReceipt func(authority.SupervisorHandoff, authority.StopReceipt) error
	clearSupervisorHandoff             func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error
	containmentHandoffReceiptSaved     bool
	containmentHandoffReleased         bool
	persistProcessWithAuthority        func(int, string, *authority.Supervisor) error
	persistAuthority                   func(int, string, *authority.Supervisor) error
	containmentAuthority               *authority.Supervisor
	persistStopReceipt                 func(int, string, authority.StopReceipt) error
	persistContainmentUnproven         func(int, string) error
	containmentUnprovenCallback        func() error
	containmentUnprovenPersisted       bool
	stopReceiptRequired                bool
	containmentAuthorityUncertain      bool
	sinkContext                        context.Context
	cancelSink                         context.CancelFunc
	runContext                         context.Context

	stdinMutex       sync.Mutex
	stdin            *os.File
	stdinWritePermit chan struct{}

	events               chan Event
	eventMutex           sync.Mutex
	eventsClosed         bool
	nextSequence         uint64
	readersLeft          atomic.Int32
	readersDone          chan struct{}
	outputReaderMutex    sync.Mutex
	outputReaders        [2]*os.File
	outputDrainAbandoned atomic.Bool
	deliveryDone         chan struct{}

	resultDone  chan struct{}
	commandDone chan struct{}
	result      Result

	errorMutex       sync.Mutex
	sinkError        error
	outputError      error
	containmentError error
	// containmentTransientError reports the early clone-stop timeout to Wait
	// without turning a later successful FinalizeContainment retry permanent.
	containmentTransientError error

	containmentFinalizeMutex       sync.Mutex
	containmentFinalized           bool
	containmentFinalizationStarted bool
	containmentCloseAttempted      bool
	containmentInitialError        error
	containmentStableError         error
	containmentReceiptSaved        bool

	terminationMutex         sync.Mutex
	terminationDone          chan struct{}
	terminationStart         bool
	terminated               bool
	cloneFinalizationStarted bool
	cloneFinalizationDone    chan struct{}
	terminationError         error
	outputStop               chan struct{}
	outputStopOnce           sync.Once
	outputTruncated          bool
}

// LinuxV2Peer owns a duplicate of the exact pidfd returned when the trusted
// gate was born. The caller must close it after the native peer is retired.
// The namespace identity must be independently authenticated while the gate
// is parked; a procfs symlink inode or nonzero device/inode is insufficient.
type LinuxV2Peer struct {
	PID           int
	Identity      string
	UserNamespace platform.LinuxUserNamespaceIdentity
	PIDFD         *os.File
}

func (peer *LinuxV2Peer) Close() error {
	if peer == nil || peer.PIDFD == nil {
		return nil
	}
	err := peer.PIDFD.Close()
	peer.PIDFD = nil
	return err
}

// Result describes the completed process without requiring callers to parse an
// exec.ExitError. WaitError is non-nil for a non-zero exit or launch wait fault.
type Result struct {
	PID        int
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
	Terminated bool

	WaitError       error
	SinkError       error
	OutputError     error
	OutputTruncated bool

	TerminationError error
	ContainmentError error
}

// Success reports whether the process exited cleanly and all output reached the
// sink without an observed error.
func (result Result) Success() bool {
	return result.ExitCode == 0 && !result.Terminated && result.WaitError == nil &&
		result.SinkError == nil && result.OutputError == nil && result.TerminationError == nil &&
		result.ContainmentError == nil && !result.OutputTruncated
}

// Start launches an invocation directly, without a command shell. It starts
// separate readers for stdout and stderr before asynchronously waiting for the
// command, and cancellation of ctx invokes the same tree termination path as
// an explicit Terminate call.
// After a successful OS launch, errors retain a non-nil Process. Callers must
// keep that owner and consume its final Wait result even when Start fails.
func (runner Runner) Start(ctx context.Context, invocation Invocation, sink Sink) (*Process, error) {
	if ctx == nil {
		return nil, errors.New("execution context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("execution context already cancelled: %w", err)
	}
	linuxV2Requested := invocation.linuxV2Requested()
	if linuxV2Requested && (runtime.GOOS != "linux" || runtime.GOARCH != "amd64") {
		return nil, errors.New("Linux v2 process launch is supported only on Linux x86-64")
	}
	if invocation.LinuxV2Required && invocation.LinuxV2 == nil {
		return nil, errors.New("Linux v2 process launch was required but its grant and callbacks are missing")
	}
	if linuxV2Requested {
		if invocation.InitialLeaseDeadlineAt.IsZero() || invocation.InitialLeaseSequence != 1 {
			return nil, errors.New("Linux v2 process launch requires the claim-derived initial lease deadline and sequence 1")
		}
		if _, _, err := runner.initialLeaseDeadline(invocation); err != nil {
			return nil, fmt.Errorf("validate claim-derived Linux v2 lease before launch: %w", err)
		}
	}
	program, err := validateInvocation(invocation)
	if err != nil {
		return nil, err
	}
	if sink == nil {
		sink = SinkFunc(func(context.Context, Event) error { return nil })
	}

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		closeFiles(stdinRead, stdinWrite)
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite)
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := ctx.Err(); err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		return nil, fmt.Errorf("execution context already cancelled: %w", err)
	}

	command := exec.Command(program, invocation.Args...)
	command.Dir = invocation.Dir
	command.Env = cloneEnvironment(invocation.Env)
	command.Stdin = stdinRead
	command.Stdout = stdoutWrite
	command.Stderr = stderrWrite
	if !linuxV2Requested {
		if err := runner.configure(command); err != nil {
			closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
			return nil, fmt.Errorf("configure process containment for %q: %w", program, err)
		}
	}

	startedAt := time.Now().UTC()
	var started *startedProcess
	var attachFailureErr error
	if linuxV2Requested && runner.launchProcess == nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		return nil, errors.New("Linux v2 process launcher is unavailable; direct process fallback is forbidden")
	}
	if runner.launchProcess != nil {
		started, err = runner.launchProcess(ctx, command, invocation, stdinRead, stdoutWrite, stderrWrite)
		if started != nil && started.closeLinuxV2Containment == nil {
			configureStartedHandoff(started, invocation)
		}
		if err != nil && (!errors.Is(err, errNativeProcessLauncherUnavailable) || linuxV2Requested) {
			if started == nil {
				closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
				return nil, fmt.Errorf("start %q: %w", invocation.Program, err)
			}
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				fmt.Errorf("start %q: %w", invocation.Program, err),
				err,
			)
		}
		if err == nil && started == nil {
			closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
			return nil, fmt.Errorf("start %q: native launcher returned no process", invocation.Program)
		}
	}
	if started == nil {
		if linuxV2Requested {
			closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
			return nil, errors.New("Linux v2 process launch did not produce an owned process; direct process fallback is forbidden")
		}
		if err := command.Start(); err != nil {
			closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
			return nil, fmt.Errorf("start %q: %w", invocation.Program, err)
		}
		containment, identity, attachErr := runner.attach(command.Process)
		started = legacyStartedProcess(command, containment, identity)
		if attachErr != nil {
			wrappedErr := fmt.Errorf("contain process tree for %q: %w", program, attachErr)
			if !canPersistRetainedAttachFailure(containment, identity) {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					wrappedErr,
					wrappedErr,
				)
			}
			// A retained, retryable containment owner has enough identity and
			// authority to enter the normal persistence fence before cleanup. This
			// is required for Linux initial-scan failures, where Close must retain
			// an unresolved owner for restart recovery.
			attachFailureErr = wrappedErr
		}
	}
	containment := started.containment
	identity := started.identity
	started.persistContainmentUnproven = invocation.PersistContainmentUnproven
	_, hasStopReceiptProvider := containment.(platform.ContainmentStopReceiptProvider)
	provider, hasAuthorityProvider := containment.(platform.ContainmentAuthorityProvider)
	setStopReceiptPersistence := func() {
		if hasStopReceiptProvider || hasAuthorityProvider {
			started.persistStopReceipt = invocation.PersistContainmentStopReceipt
			started.stopReceiptRequired = invocation.PersistContainmentStopReceipt != nil
		}
	}
	var markerPersisted atomic.Bool
	if err := installContainmentUnprovenCallback(started, &markerPersisted); err != nil {
		return cleanupFailedStartStarted(
			started,
			stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
			startedAt,
			fmt.Errorf("install containment uncertainty observer: %w", err),
			nil,
		)
	}
	authorityCapable := hasAuthorityProvider
	if capability, ok := containment.(platform.ContainmentAuthorityCapability); ok && !capability.ContainmentAuthorityAvailable() {
		authorityCapable = false
	}
	atomicPersist := invocation.PersistProcessWithAuthority
	if started.containmentHandoff != nil {
		setStopReceiptPersistence()
		commitAt := startedAt
		if !started.containmentHandoffStartedAt.IsZero() {
			commitAt = started.containmentHandoffStartedAt
		}
		if err := runner.commitStartedHandoff(started, commitAt); err != nil {
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				fmt.Errorf("commit supervisor handoff: %w", err),
				nil,
			)
		}
		if err := runner.waitForCommitResumeBarrier(ctx, started); err != nil {
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				fmt.Errorf("wait for commit/resume barrier: %w", err),
				nil,
			)
		}
		markerPersisted.Store(true)
		if started.closeLinuxV2Containment == nil {
			if value, err := started.containmentHandoff.ToSupervisor(); err == nil {
				cloned := value.Clone()
				started.containmentAuthority = &cloned
			}
		}
	} else if authorityCapable && atomicPersist != nil {
		value := provider.ContainmentAuthority()
		setStopReceiptPersistence()
		if value == nil {
			const message = "containment authority is unavailable after process launch"
			started.containmentAuthorityUncertain = true
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				errors.New(message),
				errors.New(message),
			)
		}
		// Freeze the post-authority cleanup contract before invoking the atomic
		// callback. It may return an error after its durable rename; either
		// outcome must retain the receipt/release fence and remain retryable.
		cloned := value.Clone()
		started.containmentAuthorityUncertain = true
		started.containmentAuthority = &cloned
		started.persistProcessWithAuthority = atomicPersist
		callbackAuthority := cloned.Clone()
		if persistErr := atomicPersist(started.pid, identity, &callbackAuthority); persistErr != nil {
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				fmt.Errorf("persist process and containment authority: %w", persistErr),
				nil,
			)
		}
		started.containmentAuthorityUncertain = false
		markerPersisted.Store(true)
	} else {
		if invocation.PersistProcess != nil {
			if persistErr := invocation.PersistProcess(started.pid, identity); persistErr != nil {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					fmt.Errorf("persist process identity: %w", persistErr),
					nil,
				)
			}
			markerPersisted.Store(true)
		}
		setStopReceiptPersistence()
		if authorityCapable && invocation.PersistProcessAuthority != nil {
			value := provider.ContainmentAuthority()
			// Freeze the post-authority cleanup contract before invoking the
			// legacy callback. The callback may return an error after its atomic
			// rename; either outcome must retain the receipt/release fence.
			started.containmentAuthorityUncertain = true
			if value == nil {
				const message = "containment authority is unavailable after process marker persistence"
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					errors.New(message),
					errors.New(message),
				)
			}
			authorityCopy := value.Clone()
			started.persistAuthority = invocation.PersistProcessAuthority
			started.containmentAuthority = &authorityCopy
			callbackAuthority := authorityCopy.Clone()
			if persistErr := invocation.PersistProcessAuthority(started.pid, identity, &callbackAuthority); persistErr != nil {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					fmt.Errorf("persist process containment authority: %w", persistErr),
					nil,
				)
			}
			started.containmentAuthorityUncertain = false
			markerPersisted.Store(true)
		}
	}
	if markerPersisted.Load() {
		if err := installContainmentUnprovenCallback(started, &markerPersisted); err != nil {
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				fmt.Errorf("persist containment uncertainty: %w", err),
				nil,
			)
		}
	}
	if capability, ok := containment.(platform.ContainmentAuthorityCapability); ok && !capability.ContainmentAuthorityAvailable() {
		// A legacy/noop containment owner has no independently releasable
		// authority. Its local Close still proves the owned tree stopped, but it
		// cannot produce the durable supervisor receipt used by the real helper.
		// Do not turn the absence of that optional authority into a process
		// failure when the daemon supplies the receipt callback unconditionally.
		started.persistStopReceipt = nil
		started.stopReceiptRequired = false
	}
	if attachFailureErr != nil {
		return cleanupFailedStartStarted(
			started,
			stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
			startedAt,
			attachFailureErr,
			attachFailureErr,
		)
	}
	initialLeaseDeadline, initialLeaseRequested, deadlineErr := runner.initialLeaseDeadline(invocation)
	if deadlineErr != nil {
		return cleanupFailedStartStarted(
			started,
			stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
			startedAt,
			deadlineErr,
			nil,
		)
	}
	if initialLeaseRequested {
		if started.renewLinuxV2Lease != nil {
			if invocation.InitialLeaseSequence == 0 {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					errors.New("initial containment lease sequence is missing"),
					nil,
				)
			}
			if err := started.renewLinuxV2Lease(ctx, initialLeaseDeadline, invocation.InitialLeaseSequence); err != nil {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					fmt.Errorf("arm initial Linux v2 containment lease: %w", err),
					nil,
				)
			}
			if err := runner.waitForLeaseArmedResumeBarrier(ctx, started); err != nil {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					fmt.Errorf("wait for lease-armed/resume barrier: %w", err),
					nil,
				)
			}
		} else if renewer, ok := containment.(platform.ContainmentLeaseRenewer); ok && renewer.LeaseRenewalAvailable() {
			if invocation.InitialLeaseSequence == 0 {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					errors.New("initial containment lease sequence is missing"),
					nil,
				)
			}
			if err := renewer.RenewLease(initialLeaseDeadline, invocation.InitialLeaseSequence); err != nil {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					fmt.Errorf("arm initial containment lease: %w", err),
					nil,
				)
			}
			if err := runner.waitForLeaseArmedResumeBarrier(ctx, started); err != nil {
				return cleanupFailedStartStarted(
					started,
					stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
					startedAt,
					fmt.Errorf("wait for lease-armed/resume barrier: %w", err),
					nil,
				)
			}
		}
	}
	if started.resume != nil {
		if err := ctx.Err(); err != nil {
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				fmt.Errorf("execution context cancelled before process resume: %w", err),
				nil,
			)
		}
		if err := started.resume(); err != nil {
			return cleanupFailedStartStarted(
				started,
				stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
				startedAt,
				fmt.Errorf("resume process %q: %w", program, err),
				nil,
			)
		}
	}

	// These ends belong only to the child after launch. Closing them here is
	// essential: otherwise the readers would never observe EOF.
	closeFiles(stdinRead, stdoutWrite, stderrWrite)

	process := newProcessFromStarted(started, sink, startedAt, stdinWrite, nil)
	process.outputDrainTimeout = runner.outputDrainTimeout
	process.start(stdoutRead, stderrRead, ctx)

	if len(invocation.InitialInput) > 0 {
		if err := process.WriteInputContext(ctx, invocation.InitialInput); err != nil {
			return cleanupAfterInputSetupFailure(process, fmt.Errorf("write initial input: %w", err), "initial input")
		}
	}
	if invocation.CloseInputAfterInitial {
		if err := process.CloseInput(); err != nil {
			return cleanupAfterInputSetupFailure(process, fmt.Errorf("close initial input: %w", err), "initial input close")
		}
	}
	return process, nil
}

func configureStartedHandoff(started *startedProcess, invocation Invocation) {
	if started == nil || started.containmentHandoff == nil {
		return
	}
	if started.commitSupervisorHandoff == nil {
		started.commitSupervisorHandoff = invocation.CommitSupervisorHandoff
	}
	if started.recordSupervisorHandoffStopReceipt == nil {
		started.recordSupervisorHandoffStopReceipt = invocation.RecordSupervisorHandoffStopReceipt
	}
	if started.clearSupervisorHandoff == nil {
		started.clearSupervisorHandoff = invocation.ClearSupervisorHandoff
	}
}

// commitStartedHandoff is the only transition that permits a suspended target
// to resume. A callback error is unknown, so the exact same handoff and launch
// timestamp are retried once; unresolved state remains owned for recovery.
func (runner Runner) commitStartedHandoff(started *startedProcess, startedAt time.Time) error {
	if started == nil || started.containmentHandoff == nil {
		return nil
	}
	expected := started.containmentHandoff.Clone()
	started.containmentHandoffCommitUnknown = true
	callback := started.commitSupervisorHandoff
	if callback == nil {
		return errors.New("supervisor handoff commit callback is required; atomic process persistence cannot substitute for commit")
	}
	firstErr := callback(expected.Clone(), startedAt)
	if firstErr != nil {
		if retryErr := callback(expected.Clone(), startedAt); retryErr != nil {
			return errors.Join(firstErr, retryErr)
		}
	}
	started.containmentHandoffCommitUnknown = false
	started.containmentHandoffCommitted = true
	return nil
}

// waitForCommitResumeBarrier is an opt-in crash-witness seam. It is disabled
// unless both marker paths are supplied, and it never changes the ordinary
// production lifecycle. The reached marker is created only after durable
// commit; lease arming and target resume remain behind the release marker.
func (runner Runner) waitForCommitResumeBarrier(ctx context.Context, started *startedProcess) error {
	return runner.waitForResumeBarrier(
		ctx,
		started,
		"commit/resume",
		commitResumeBarrierReachedEnvironment,
		commitResumeBarrierReleaseEnvironment,
		false,
	)
}

// waitForLeaseArmedResumeBarrier is an opt-in crash-witness seam after the
// helper accepted the initial lease and before the suspended target resumes.
func (runner Runner) waitForLeaseArmedResumeBarrier(ctx context.Context, started *startedProcess) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	return runner.waitForResumeBarrier(
		ctx,
		started,
		"lease-armed/resume",
		leaseArmedResumeBarrierReachedEnvironment,
		leaseArmedResumeBarrierReleaseEnvironment,
		true,
	)
}

func (runner Runner) waitForResumeBarrier(ctx context.Context, started *startedProcess, phase, reachedEnvironment, releaseEnvironment string, leaseArmed bool) error {
	if os.Getenv(resumeBarrierWitnessGateEnvironment) != "1" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s barrier cancelled: %w", phase, err)
	}
	reachedPath, reachedConfigured := os.LookupEnv(reachedEnvironment)
	releasePath, releaseConfigured := os.LookupEnv(releaseEnvironment)
	if !reachedConfigured && !releaseConfigured {
		return nil
	}
	if reachedConfigured != releaseConfigured {
		return fmt.Errorf("%s barrier requires both %s and %s", phase, reachedEnvironment, releaseEnvironment)
	}
	reachedPath = strings.TrimSpace(reachedPath)
	releasePath = strings.TrimSpace(releasePath)
	if reachedPath == "" || releasePath == "" {
		return fmt.Errorf("%s barrier marker paths must not be empty", phase)
	}
	if filepath.Clean(reachedPath) == filepath.Clean(releasePath) {
		return fmt.Errorf("%s barrier marker paths must be distinct", phase)
	}
	if err := assertResumeBarrierPathAbsent(reachedPath); err != nil {
		return fmt.Errorf("validate reached marker %q: %w", reachedPath, err)
	}
	if err := assertResumeBarrierPathAbsent(releasePath); err != nil {
		return fmt.Errorf("validate release marker %q: %w", releasePath, err)
	}

	reached, err := os.OpenFile(reachedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create reached marker %q: %w", reachedPath, err)
	}
	marker := fmt.Sprintf(
		"pid=%d\ntarget_pid=%d\ntime=%s\nlease_armed=%t\n",
		os.Getpid(),
		resumeBarrierTargetPID(started),
		time.Now().UTC().Format(time.RFC3339Nano),
		leaseArmed,
	)
	writeErr := error(nil)
	if _, writeErr = reached.WriteString(marker); writeErr == nil {
		writeErr = reached.Sync()
	}
	if closeErr := reached.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return fmt.Errorf("publish reached marker %q: %w", reachedPath, writeErr)
	}

	deadline := time.NewTimer(resumeBarrierTimeout)
	defer deadline.Stop()
	poll := time.NewTicker(resumeBarrierPollInterval)
	defer poll.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s barrier cancelled: %w", phase, err)
		}
		if err := resumeBarrierReleaseState(releasePath); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("observe %s barrier release marker %q: %w", phase, releasePath, err)
		}
		select {
		case <-deadline.C:
			return fmt.Errorf("%s barrier release marker %q did not appear within %s", phase, releasePath, resumeBarrierTimeout)
		case <-ctx.Done():
			return fmt.Errorf("%s barrier cancelled: %w", phase, ctx.Err())
		case <-poll.C:
		}
	}
}

func assertResumeBarrierPathAbsent(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		return errors.New("path already exists")
	}
	return err
}

func resumeBarrierReleaseState(path string) error {
	_, err := os.Lstat(path)
	return err
}

func resumeBarrierTargetPID(started *startedProcess) int {
	if started == nil || started.containmentHandoff == nil {
		return 0
	}
	return started.containmentHandoff.TargetPID
}

var errInitialLeaseDeadlineExpired = errors.New("initial containment lease deadline has elapsed")

func (runner Runner) currentTime() time.Time {
	if runner.now != nil {
		return runner.now()
	}
	return time.Now()
}

func (runner Runner) initialLeaseDeadline(invocation Invocation) (time.Duration, bool, error) {
	if !invocation.InitialLeaseDeadlineAt.IsZero() {
		remaining := invocation.InitialLeaseDeadlineAt.Sub(runner.currentTime())
		if remaining <= 0 {
			return 0, true, errInitialLeaseDeadlineExpired
		}
		return remaining, true, nil
	}
	if invocation.InitialLeaseDeadline > 0 {
		return invocation.InitialLeaseDeadline, true, nil
	}
	return 0, false, nil
}

// cleanupAfterInputSetupFailure retains the post-start Process even when the
// bounded termination completes, because PersistProcess may already have
// committed a marker that its caller must compare-and-clear.
func cleanupAfterInputSetupFailure(process *Process, inputErr error, operation string) (*Process, error) {
	terminationContext, cancel := context.WithTimeout(context.Background(), defaultTerminationGrace)
	terminationErr := process.Terminate(terminationContext, 0)
	cancel()
	if terminationErr != nil {
		return process, errors.Join(inputErr, fmt.Errorf("terminate after %s failure: %w", operation, terminationErr))
	}
	return process, inputErr
}

func (runner Runner) configure(command *exec.Cmd) error {
	if runner.configureProcess != nil {
		return runner.configureProcess(command)
	}
	return platform.ConfigureProcess(command)
}

func (runner Runner) attach(process *os.Process) (platform.Containment, string, error) {
	if runner.attachProcess != nil {
		return runner.attachProcess(process)
	}
	return platform.AttachProcess(process)
}

// canPersistRetainedAttachFailure recognizes the narrow partial-attachment
// contract that is safe to journal before cleanup. A non-empty identity alone
// is insufficient: the retained owner must expose both the uncertainty
// observer and a retryable containment authority, which excludes identity or
// process-group-anchor capture failures.
func canPersistRetainedAttachFailure(containment platform.Containment, identity string) bool {
	if containment == nil || identity == "" {
		return false
	}
	if _, ok := containment.(containmentUnprovenCallbackSetter); !ok {
		return false
	}
	retryer, ok := containment.(platform.ContainmentCloseRetryer)
	return ok && retryer.ContainmentCloseRetryable()
}

func installContainmentUnprovenCallback(started *startedProcess, markerReady *atomic.Bool) error {
	if started == nil || started.containment == nil || started.persistContainmentUnproven == nil {
		return nil
	}
	setter, ok := started.containment.(containmentUnprovenCallbackSetter)
	if !ok {
		return nil
	}
	var mutex sync.Mutex
	persisted := false
	callback := func() error {
		mutex.Lock()
		defer mutex.Unlock()
		if persisted {
			return nil
		}
		if markerReady != nil && !markerReady.Load() {
			return errContainmentMarkerPending
		}
		if err := started.persistContainmentUnproven(started.pid, started.identity); err != nil {
			return err
		}
		persisted = true
		return nil
	}
	started.containmentUnprovenCallback = callback
	if err := setter.SetContainmentUnprovenCallback(callback); err != nil && !errors.Is(err, errContainmentMarkerPending) {
		return err
	}
	return nil
}

func legacyStartedProcess(command *exec.Cmd, containment platform.Containment, identity string) *startedProcess {
	return &startedProcess{
		command:     command,
		pid:         command.Process.Pid,
		identity:    identity,
		containment: containment,
		wait: func() (int, error) {
			err := command.Wait()
			code := -1
			if command.ProcessState != nil {
				code = command.ProcessState.ExitCode()
			}
			return code, err
		},
		kill: command.Process.Kill,
	}
}

func cloneSupervisorHandoff(value *authority.SupervisorHandoff) *authority.SupervisorHandoff {
	if value == nil {
		return nil
	}
	cloned := value.Clone()
	return &cloned
}

func newProcess(command *exec.Cmd, sink Sink, containment platform.Containment, identity string, startedAt time.Time, stdin *os.File) *Process {
	return newProcessFromStarted(legacyStartedProcess(command, containment, identity), sink, startedAt, stdin, nil)
}

func newProcessFromStarted(started *startedProcess, sink Sink, startedAt time.Time, stdin *os.File, persistStopReceipt func(int, string, authority.StopReceipt) error) *Process {
	if persistStopReceipt == nil && started != nil {
		persistStopReceipt = started.persistStopReceipt
	}
	var containmentAuthority *authority.Supervisor
	if started != nil && started.containmentAuthority != nil {
		authorityCopy := started.containmentAuthority.Clone()
		containmentAuthority = &authorityCopy
	}
	sinkContext, cancelSink := context.WithCancel(context.Background())
	process := &Process{
		PID:                                started.pid,
		Identity:                           started.identity,
		command:                            started.command,
		backend:                            started,
		sink:                               sink,
		containment:                        started.containment,
		containmentHandoff:                 cloneSupervisorHandoff(started.containmentHandoff),
		containmentHandoffCommitted:        started.containmentHandoffCommitted,
		containmentHandoffPrepareUnknown:   started.containmentHandoffPrepareUnknown,
		containmentHandoffBindUnknown:      started.containmentHandoffBindUnknown,
		containmentHandoffCommitUnknown:    started.containmentHandoffCommitUnknown,
		commitSupervisorHandoff:            started.commitSupervisorHandoff,
		recordSupervisorHandoffStopReceipt: started.recordSupervisorHandoffStopReceipt,
		clearSupervisorHandoff:             started.clearSupervisorHandoff,
		persistProcessWithAuthority:        started.persistProcessWithAuthority,
		persistAuthority:                   started.persistAuthority,
		containmentAuthority:               containmentAuthority,
		persistStopReceipt:                 persistStopReceipt,
		persistContainmentUnproven:         started.persistContainmentUnproven,
		containmentUnprovenCallback:        started.containmentUnprovenCallback,
		stopReceiptRequired:                started.stopReceiptRequired,
		containmentAuthorityUncertain:      started.containmentAuthorityUncertain,
		sinkContext:                        sinkContext,
		cancelSink:                         cancelSink,
		stdin:                              stdin,
		stdinWritePermit:                   make(chan struct{}, 1),
		events:                             make(chan Event, eventQueueCapacity),
		readersDone:                        make(chan struct{}),
		deliveryDone:                       make(chan struct{}),
		resultDone:                         make(chan struct{}),
		commandDone:                        make(chan struct{}),
		terminationDone:                    make(chan struct{}),
		outputStop:                         make(chan struct{}),
		result: Result{
			PID:       started.pid,
			StartedAt: startedAt,
		},
	}
	process.stdinWritePermit <- struct{}{}
	return process
}

func (process *Process) start(stdoutRead, stderrRead *os.File, ctx context.Context) {
	process.runContext = ctx
	process.outputReaderMutex.Lock()
	process.outputReaders = [2]*os.File{stdoutRead, stderrRead}
	process.outputReaderMutex.Unlock()
	process.readersLeft.Store(2)
	go process.readOutput(stdoutRead, Stdout)
	go process.readOutput(stderrRead, Stderr)
	go process.deliverOutput()
	go process.waitForCompletion()
	go process.terminateWhenContextCancels(ctx)
}

// cleanupFailedStartStarted hands physical cleanup to Process so an
// indeterminate shutdown keeps its original process handle and is still reaped
// by one owner.
func cleanupFailedStartStarted(
	started *startedProcess,
	stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite *os.File,
	startedAt time.Time,
	startErr error,
	containmentErr error,
) (*Process, error) {
	// Do not expose pre-persistence output to the caller's sink. The cleanup
	// Process still drains the pipes and owns the child wait.
	closeFiles(stdinRead, stdoutWrite, stderrWrite)
	var process *Process
	cleanupSink := SinkFunc(func(_ context.Context, event Event) error {
		if len(event.Data) > 0 && process != nil {
			process.recordOutputTruncated()
		}
		return nil
	})
	process = newProcessFromStarted(
		started,
		cleanupSink,
		startedAt,
		stdinWrite,
		nil,
	)
	process.recordContainmentError(containmentErr)
	process.start(stdoutRead, stderrRead, context.Background())

	cleanupContext, cancel := context.WithTimeout(context.Background(), defaultTerminationGrace+time.Second)
	terminationErr := process.Terminate(cleanupContext, 0)
	cancel()
	return process, errors.Join(startErr, terminationErr)
}

// WriteInput appends human input to the agent's standard input. Concurrent
// callers are serialized, preserving write order and avoiding interleaving.
func (process *Process) WriteInput(input []byte) error {
	return process.WriteInputContext(context.Background(), input)
}

// WriteInputContext appends input while allowing a caller to cancel a blocked
// pipe write. A nil result means every byte was written. Once cancellation or
// a write failure interrupts an attempted write, the input transport is closed
// permanently so a later caller cannot mistake a partial request for success.
func (process *Process) WriteInputContext(ctx context.Context, input []byte) error {
	if ctx == nil {
		return errors.New("input context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write process input: %w", err)
	}
	if len(input) == 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("write process input: %w", ctx.Err())
	case <-process.stdinWritePermit:
	}
	defer func() { process.stdinWritePermit <- struct{}{} }()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write process input: %w", err)
	}
	process.stdinMutex.Lock()
	stdin := process.stdin
	process.stdinMutex.Unlock()
	if stdin == nil {
		return ErrInputClosed
	}

	cancellationDone := make(chan struct{})
	cancelled := false
	stopCancellation := context.AfterFunc(ctx, func() {
		cancelled = true
		_ = process.closeInputFile(stdin)
		close(cancellationDone)
	})

	writeErr := writeAll(stdin, input)
	if !stopCancellation() {
		<-cancellationDone
	}
	if cancelled {
		return fmt.Errorf("write process input cancelled; standard input is closed: %w", errors.Join(ctx.Err(), ErrInputClosed))
	}
	if writeErr != nil {
		_ = process.closeInputFile(stdin)
		return fmt.Errorf("write process input interrupted; standard input is closed: %w", errors.Join(writeErr, ErrInputClosed))
	}
	return nil
}

// CloseInput sends EOF to the agent while allowing its remaining stdout and
// stderr output to drain. It is safe to call more than once.
func (process *Process) CloseInput() error {
	process.stdinMutex.Lock()
	stdin := process.stdin
	process.stdin = nil
	process.stdinMutex.Unlock()
	if stdin == nil {
		return nil
	}
	err := stdin.Close()
	if err != nil {
		return fmt.Errorf("close process input: %w", err)
	}
	return nil
}

// closeInputFile closes stdin only when it is still the process's active
// transport. This lets a cancellation race safely with CloseInput.
func (process *Process) closeInputFile(stdin *os.File) error {
	process.stdinMutex.Lock()
	if process.stdin != stdin {
		process.stdinMutex.Unlock()
		return nil
	}
	process.stdin = nil
	process.stdinMutex.Unlock()
	return stdin.Close()
}

func writeAll(stdin *os.File, input []byte) error {
	for len(input) > 0 {
		written, err := stdin.Write(input)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		input = input[written:]
	}
	return nil
}

// Wait waits for process exit, pipe draining, and queued sink delivery. If a
// Linux v2 cgroup stop remains unproven, it cancels delivery and returns an
// unresolved, output-truncated result instead of waiting on inherited pipe
// writers. The underlying command is waited exactly once regardless of how
// many callers use Wait concurrently.
func (process *Process) Wait() Result {
	<-process.resultDone
	process.errorMutex.Lock()
	result := process.result
	if process.containmentError != nil {
		result.ContainmentError = process.containmentError
	} else {
		result.ContainmentError = process.containmentTransientError
	}
	process.errorMutex.Unlock()
	return result
}

// ResultDone returns the completion barrier that closes after the process
// result has been finalized. A nil receiver returns a nil channel.
func (process *Process) ResultDone() <-chan struct{} {
	if process == nil {
		return nil
	}
	return process.resultDone
}

// ProcessDetails returns the restart-safe identity recorded when this process
// was started. Both values remain stable for the Process lifetime.
func (process *Process) ProcessDetails() (int, string) {
	if process == nil {
		return 0, ""
	}
	return process.PID, process.Identity
}

// DuplicateLinuxV2Peer returns a caller-owned pidfd without reopening a
// numeric PID. The launch owner serializes duplication with its own Close.
// It fails closed with errors.ErrUnsupported when the parked gate's namespace
// identity cannot be independently verified.
func (process *Process) DuplicateLinuxV2Peer() (*LinuxV2Peer, error) {
	if process == nil || process.backend == nil {
		return nil, errors.ErrUnsupported
	}
	backend := process.backend
	backend.peerMutex.Lock()
	defer backend.peerMutex.Unlock()
	if backend.duplicateLinuxV2Peer == nil {
		return nil, errors.ErrUnsupported
	}
	peer, err := backend.duplicateLinuxV2Peer()
	if err != nil {
		return nil, err
	}
	if peer == nil || peer.PIDFD == nil || peer.PID != process.PID || peer.Identity != process.Identity ||
		peer.UserNamespace.Device == 0 || peer.UserNamespace.Inode == 0 {
		if peer != nil {
			_ = peer.Close()
		}
		return nil, errors.New("Linux v2 process peer identity is unavailable")
	}
	return peer, nil
}

// LeaseRenewalAvailable reports whether the process has an independent local
// containment watchdog. Unsupported and legacy containment remains usable
// without this optional capability.
func (process *Process) LeaseRenewalAvailable() bool {
	if process == nil || process.containment == nil {
		return false
	}
	renewer, ok := process.containment.(platform.ContainmentLeaseRenewer)
	return ok && renewer.LeaseRenewalAvailable()
}

// RenewLease arms the independent containment watchdog with a relative local
// deadline. The caller owns the monotonic sequence for this process authority.
func (process *Process) RenewLease(deadline time.Duration, sequence uint64) error {
	if process == nil || process.containment == nil {
		return fmt.Errorf("%w: process containment lease is unavailable", errors.ErrUnsupported)
	}
	renewer, ok := process.containment.(platform.ContainmentLeaseRenewer)
	if !ok || !renewer.LeaseRenewalAvailable() {
		return fmt.Errorf("%w: process containment lease is unavailable", errors.ErrUnsupported)
	}
	return renewer.RenewLease(deadline, sequence)
}

// Terminate requests graceful process-tree termination, waits grace, then
// forcefully stops the remaining tree. Multiple callers share one operation;
// the first grace duration wins and all calls are otherwise idempotent.
func (process *Process) Terminate(ctx context.Context, grace time.Duration) error {
	if ctx == nil {
		return errors.New("termination context must not be nil")
	}
	process.terminationMutex.Lock()
	if !process.terminationStart {
		if process.cloneFinalizationStarted {
			done := process.cloneFinalizationDone
			process.terminationMutex.Unlock()
			if done == nil {
				return errors.New("Linux v2 clone finalizer has no completion signal")
			}
			select {
			case <-done:
				process.errorMutex.Lock()
				containmentError := process.containmentError
				process.errorMutex.Unlock()
				process.terminationMutex.Lock()
				terminationError := process.terminationError
				process.terminationMutex.Unlock()
				return errors.Join(containmentError, terminationError)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		select {
		case <-process.resultDone:
			process.terminationMutex.Unlock()
			return process.closeContainment()
		default:
		}
		process.terminationStart = true
		process.terminated = true
		if process.backend != nil && process.backend.recordLinuxV2CloneOutcome != nil {
			process.backend.recordLinuxV2CloneOutcome(LinuxCloneRunOutcome{
				ExitCode: -1, Terminated: true,
				RunContextCancelled: process.runContext != nil && process.runContext.Err() != nil,
			})
		}
		process.stopOutputDelivery()
		// Closing stdin is part of termination, not post-exit cleanup. An
		// interactive child may be blocked in a read, and waiting for its exit
		// before closing this transport would make the termination barrier
		// circular when platform containment is delayed or unavailable.
		_ = process.CloseInput()
		go process.terminateTree(grace)
	}
	done := process.terminationDone
	process.terminationMutex.Unlock()

	select {
	case <-done:
		process.terminationMutex.Lock()
		terminationError := process.terminationError
		process.terminationMutex.Unlock()
		return errors.Join(terminationError, process.containmentFailure())
	case <-ctx.Done():
		return ctx.Err()
	}
}

// FinalizeContainment retries only the durable receipt/release phase after the
// process result is already final. It never waits for the child and never
// reissues physical termination. Callers that have not observed resultDone get
// an immediate error rather than creating an unbounded cleanup wait.
func (process *Process) FinalizeContainment() error {
	if process == nil {
		return errors.New("process is nil")
	}
	select {
	case <-process.resultDone:
		return process.closeContainment()
	default:
		return errors.New("process result is not complete")
	}
}

func (process *Process) readOutput(reader *os.File, stream Stream) {
	defer func() {
		if process.readersLeft.Add(-1) == 0 {
			close(process.readersDone)
		}
	}()
	defer reader.Close()

	buffer := make([]byte, readChunkSize)
	for {
		count, err := reader.Read(buffer)
		if count > 0 && !process.enqueue(stream, buffer[:count]) {
			return
		}
		if err == nil {
			continue
		}
		if !errors.Is(err, io.EOF) && !process.outputDrainAbandoned.Load() {
			process.recordOutputError(fmt.Errorf("read %s: %w", stream, err))
		}
		return
	}
}

func (process *Process) enqueue(stream Stream, data []byte) bool {
	select {
	case <-process.outputStop:
		if len(data) > 0 {
			process.recordOutputTruncated()
		}
		return false
	default:
	}

	process.eventMutex.Lock()
	if process.eventsClosed {
		process.eventMutex.Unlock()
		if len(data) > 0 {
			process.recordOutputTruncated()
		}
		return false
	}
	event := Event{
		Stream:   stream,
		Sequence: process.nextSequence + 1,
		At:       time.Now().UTC(),
		Data:     append([]byte(nil), data...),
	}
	select {
	case process.events <- event:
		process.nextSequence++
		process.eventMutex.Unlock()
		if process.outputDeliveryStopped() {
			if len(data) > 0 {
				process.recordOutputTruncated()
			}
			return false
		}
		return true
	case <-process.outputStop:
		process.eventMutex.Unlock()
		if len(data) > 0 {
			process.recordOutputTruncated()
		}
		return false
	}
}

func (process *Process) closeEvents() {
	process.eventMutex.Lock()
	if !process.eventsClosed {
		process.eventsClosed = true
		close(process.events)
	}
	process.eventMutex.Unlock()
}

func (process *Process) deliverOutput() {
	defer close(process.deliveryDone)
	for {
		var event Event
		select {
		case <-process.outputStop:
			process.discardQueuedOutput()
			return
		case next, ok := <-process.events:
			if !ok {
				return
			}
			event = next
		}
		if process.outputDeliveryStopped() {
			if len(event.Data) > 0 {
				process.recordOutputTruncated()
			}
			continue
		}
		if err := process.sink.Handle(process.sinkContext, event); err != nil {
			if process.outputDeliveryStopped() {
				if len(event.Data) > 0 {
					process.recordOutputTruncated()
				}
			} else {
				process.recordSinkError(err)
			}
		}
	}
}

func (process *Process) discardQueuedOutput() {
	for {
		select {
		case event, ok := <-process.events:
			if !ok {
				return
			}
			if len(event.Data) > 0 {
				process.recordOutputTruncated()
			}
		default:
			return
		}
	}
}

func (process *Process) waitForCompletion() {
	exitCode := -1
	var waitError error
	if process.backend == nil || process.backend.wait == nil {
		waitError = errors.New("process wait backend is unavailable")
	} else {
		exitCode, waitError = process.backend.wait()
	}
	close(process.commandDone)
	_ = process.CloseInput()

	deferLinuxV2Finalization := process.backend != nil && process.backend.deferLinuxV2FinalizeUntilOutputDrain
	if deferLinuxV2Finalization {
		// A clone run must stop its cgroup promptly, but App's final-result sink
		// supplies the proposed commit needed for promotion. Keep the release and
		// Store clear for after output delivery; this stop phase issues no receipt
		// or release capability by itself.
		var stopErr error
		if process.backend.stopLinuxV2BeforeOutputDrain == nil {
			stopErr = errors.New("Linux v2 clone cgroup stop hook is unavailable")
		} else {
			stopErr = process.backend.stopLinuxV2BeforeOutputDrain()
		}
		if stopErr != nil {
			process.recordContainmentTransientError(fmt.Errorf("stop Linux v2 clone cgroup before output drain: %w", stopErr))
			process.abandonOutputDrain()
			process.waitForOutputShutdown(linuxV2OutputCloseWait)
			process.publishResult(exitCode, waitError)
			return
		}
	} else if !process.terminationRequested() {
		process.closeContainment()
	}

	if deferLinuxV2Finalization {
		drainTimeout := process.outputDrainTimeout
		if drainTimeout <= 0 {
			drainTimeout = linuxV2OutputDrainTimeout
		}
		if !process.waitForOutputDrain(drainTimeout) {
			process.abandonOutputDrain()
			process.waitForOutputShutdown(linuxV2OutputCloseWait)
		}
	} else {
		<-process.readersDone
		process.closeEvents()
		<-process.deliveryDone
	}
	process.cancelSink()
	if deferLinuxV2Finalization && process.backend.recordLinuxV2CloneOutcome != nil {
		process.backend.recordLinuxV2CloneOutcome(process.linuxCloneRunOutcome(exitCode, waitError))
	}
	if process.terminationRequested() || deferLinuxV2Finalization {
		process.closeContainment()
	}
	if process.backend != nil && process.backend.close != nil {
		process.backend.peerMutex.Lock()
		closeErr := process.backend.close()
		process.backend.duplicateLinuxV2Peer = nil
		process.backend.peerMutex.Unlock()
		if err := closeErr; err != nil {
			// The containment receipt/release boundary is independent from local
			// process-handle cleanup. Keep this failure observable without
			// misreporting a successfully proven containment stop.
			process.recordTerminationError(fmt.Errorf("close started process: %w", err))
		}
	}

	process.publishResult(exitCode, waitError)
}

func (process *Process) waitForOutputDrain(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-process.readersDone:
		process.closeEvents()
	case <-timer.C:
		return false
	}
	select {
	case <-process.deliveryDone:
		return true
	case <-timer.C:
		return false
	}
}

func (process *Process) abandonOutputDrain() {
	process.outputDrainAbandoned.Store(true)
	process.stopOutputDelivery()
	// The sink and readers are no longer part of the process-result barrier
	// after a bounded drain. Keep containment authority independent of these
	// Runner-owned pipe ends and never close the exact cgroup/helper owner here.
	process.recordOutputTruncated()
	process.closeOutputReaders()
	process.closeEvents()
}

func (process *Process) closeOutputReaders() {
	process.outputReaderMutex.Lock()
	readers := process.outputReaders
	process.outputReaders = [2]*os.File{}
	process.outputReaderMutex.Unlock()
	for _, reader := range readers {
		if reader != nil {
			_ = reader.Close()
		}
	}
}

func (process *Process) waitForOutputShutdown(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	readersDone := process.readersDone
	deliveryDone := process.deliveryDone
	for readersDone != nil || deliveryDone != nil {
		select {
		case <-readersDone:
			readersDone = nil
		case <-deliveryDone:
			deliveryDone = nil
		case <-timer.C:
			return false
		}
	}
	return true
}

func (process *Process) publishResult(exitCode int, waitError error) {
	// Keep the termination mutex through result publication. Terminate uses the
	// same lock to decide whether it owns the transition, so a caller cannot
	// start termination after this snapshot but before the published result.
	process.terminationMutex.Lock()
	terminated := process.terminated
	terminationError := process.terminationError
	process.errorMutex.Lock()
	process.result.ExitCode = exitCode
	process.result.FinishedAt = time.Now().UTC()
	process.result.Terminated = terminated
	process.result.WaitError = waitError
	process.result.SinkError = process.sinkError
	process.result.OutputError = process.outputError
	process.result.OutputTruncated = process.outputTruncated
	process.result.TerminationError = terminationError
	process.result.ContainmentError = process.containmentError
	if process.result.ContainmentError == nil {
		process.result.ContainmentError = process.containmentTransientError
	}
	process.errorMutex.Unlock()
	close(process.resultDone)
	process.terminationMutex.Unlock()
}

func (process *Process) terminateWhenContextCancels(ctx context.Context) {
	select {
	case <-ctx.Done():
		terminationContext, cancel := context.WithTimeout(context.Background(), defaultTerminationGrace+time.Second)
		_ = process.Terminate(terminationContext, defaultTerminationGrace)
		cancel()
	case <-process.resultDone:
	}
}

func (process *Process) terminateTree(grace time.Duration) {
	defer close(process.terminationDone)
	var softTerminationError error
	if process.containment != nil {
		softTerminationError = process.containment.Terminate(false)
		if softTerminationError != nil && !isUnsupportedOnly(softTerminationError) {
			process.recordTerminationError(softTerminationError)
		}
	}

	if grace > 0 {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-process.resultDone:
			return
		case <-timer.C:
		}
	} else {
		select {
		case <-process.resultDone:
			return
		default:
		}
	}

	if process.containment == nil {
		if err := process.killProcess(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			process.recordTerminationError(err)
			return
		}
		<-process.resultDone
		return
	}

	if err := process.containment.Terminate(true); err != nil {
		// Record the containment failure before the root kill. The kill unblocks
		// command.Wait, so waitAndComplete can publish its result snapshot while
		// this goroutine is still between the kill and the record; recording
		// first keeps the published result from omitting the failure.
		process.recordTerminationError(err)
		// Containment is responsible for descendants, but it must not be the
		// sole termination mechanism for the root process. Otherwise a failed
		// job/task-kill leaves command.Wait blocked forever after a failed
		// initial-input cleanup path.
		killErr := process.killProcess()
		if !errors.Is(killErr, os.ErrProcessDone) && killErr != nil {
			process.recordTerminationError(killErr)
		}
		return
	}
	<-process.resultDone
}

func (process *Process) killProcess() error {
	if process == nil {
		return errors.New("process is nil")
	}
	if process.backend != nil && process.backend.kill != nil {
		return process.backend.kill()
	}
	if process.command == nil || process.command.Process == nil {
		return errors.New("process kill backend is unavailable")
	}
	return process.command.Process.Kill()
}

func isUnsupportedOnly(err error) bool {
	if err == nil {
		return false
	}
	type multiUnwrapper interface{ Unwrap() []error }
	if wrapped, ok := err.(multiUnwrapper); ok {
		wrappedErrors := wrapped.Unwrap()
		return len(wrappedErrors) == 1 && isUnsupportedOnly(wrappedErrors[0])
	}
	type singleUnwrapper interface{ Unwrap() error }
	if wrapped, ok := err.(singleUnwrapper); ok {
		return isUnsupportedOnly(wrapped.Unwrap())
	}
	return errors.Is(err, errors.ErrUnsupported)
}

func (process *Process) closeContainment() error {
	if process == nil {
		return nil
	}
	if process.backend != nil && process.backend.closeLinuxV2Containment != nil {
		process.containmentFinalizeMutex.Lock()
		defer process.containmentFinalizeMutex.Unlock()
		if process.backend.deferLinuxV2FinalizeUntilOutputDrain && process.backend.recordLinuxV2CloneOutcome != nil {
			process.errorMutex.Lock()
			resultPublished := !process.result.FinishedAt.IsZero()
			exitCode, waitError := process.result.ExitCode, process.result.WaitError
			process.errorMutex.Unlock()
			if resultPublished {
				process.backend.recordLinuxV2CloneOutcome(process.linuxCloneRunOutcome(exitCode, waitError))
			}
		}
		if !process.containmentFinalizationStarted {
			process.containmentFinalizationStarted = true
			process.containmentInitialError = process.containmentFailure()
		}
		if process.containmentFinalized {
			return process.containmentFinalizeError()
		}
		var finalizeErr error
		var cloneFinalizationDone chan struct{}
		if process.backend.deferLinuxV2FinalizeUntilOutputDrain {
			process.terminationMutex.Lock()
			process.cloneFinalizationStarted = true
			cloneFinalizationDone = make(chan struct{})
			process.cloneFinalizationDone = cloneFinalizationDone
			process.terminationMutex.Unlock()
			defer func() {
				process.terminationMutex.Lock()
				close(cloneFinalizationDone)
				process.terminationMutex.Unlock()
			}()
			finalizeErr = process.backend.closeLinuxV2Containment()
		} else {
			finalizeErr = process.backend.closeLinuxV2Containment()
		}
		if finalizeErr != nil {
			process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), finalizeErr))
			return finalizeErr
		}
		process.containmentFinalized = true
		finalErr := process.containmentFinalizationBaseError()
		process.setContainmentError(finalErr)
		process.backend.peerMutex.Lock()
		var closeErr error
		if process.backend.close != nil {
			closeErr = process.backend.close()
		}
		process.backend.duplicateLinuxV2Peer = nil
		process.backend.peerMutex.Unlock()
		if closeErr != nil {
			process.recordTerminationError(fmt.Errorf("close cleared Linux v2 process owner: %w", closeErr))
			return errors.Join(finalErr, closeErr)
		}
		return finalErr
	}
	if process.containment == nil && !process.pendingSupervisorHandoff() {
		return nil
	}
	process.containmentFinalizeMutex.Lock()
	defer process.containmentFinalizeMutex.Unlock()
	if !process.containmentFinalizationStarted {
		process.containmentFinalizationStarted = true
		process.containmentInitialError = process.containmentFailure()
	}
	if process.containmentFinalized {
		return process.containmentFinalizeError()
	}
	if process.pendingSupervisorHandoff() {
		provider, hasReceiptProvider := process.containmentStopReceiptProvider()
		if err := process.closeContainmentOwnerLocked(); err != nil {
			return err
		}
		return process.finalizeSupervisorHandoffLocked(provider, hasReceiptProvider)
	}
	provider, hasReceiptProvider := process.containment.(platform.ContainmentStopReceiptProvider)
	if !hasReceiptProvider && process.stopReceiptRequired {
		err := errors.New("containment stop receipt provider is missing")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}
	if !hasReceiptProvider && process.containmentAuthorityUncertain {
		err := errors.New("containment authority persistence is uncertain")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}

	closeRetryable := false
	if retryer, ok := process.containment.(platform.ContainmentCloseRetryer); ok {
		closeRetryable = retryer.ContainmentCloseRetryable()
	}
	if !process.containmentCloseAttempted || closeRetryable {
		process.containmentCloseAttempted = true
		if err := process.containment.Close(); err != nil {
			// ContainmentUnproven is a durable Linux descendant-observation
			// marker. Platform owners without the explicit observer capability
			// must retain their own authority for authenticated recovery instead
			// of being converted into an unrecoverable generic marker.
			_, supportsUnprovenObserver := process.containment.(containmentUnprovenCallbackSetter)
			responseLost := errors.Is(err, platform.ErrLinuxSupervisorResponseLost)
			if supportsUnprovenObserver && !responseLost && process.persistContainmentUnproven != nil && !process.containmentUnprovenPersisted {
				persist := process.containmentUnprovenCallback
				if persist == nil {
					persist = func() error {
						return process.persistContainmentUnproven(process.PID, process.Identity)
					}
				}
				if persistErr := persist(); persistErr == nil {
					process.containmentUnprovenPersisted = true
				} else {
					err = errors.Join(err, fmt.Errorf("persist containment uncertainty: %w", persistErr))
				}
			}
			retryableAfterFailure := false
			if retryer, ok := process.containment.(platform.ContainmentCloseRetryer); ok {
				retryableAfterFailure = retryer.ContainmentCloseRetryable()
			}
			if !retryableAfterFailure {
				process.containmentStableError = errors.Join(process.containmentStableError, err)
			}
			process.setContainmentError(errors.Join(process.containmentInitialError, err))
			return err
		}
	}

	if !hasReceiptProvider {
		process.containmentFinalized = true
		finalErr := process.containmentFinalizationBaseError()
		process.setContainmentError(finalErr)
		return finalErr
	}
	receipt, available := provider.ContainmentStopReceipt()
	if available {
		if process.containmentAuthorityUncertain {
			if (process.persistProcessWithAuthority == nil && process.persistAuthority == nil) || process.containmentAuthority == nil {
				err := errors.New("containment authority persistence callback is unavailable")
				process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
				return err
			}
			authorityCopy := process.containmentAuthority.Clone()
			callbackAuthority := authorityCopy.Clone()
			var persistErr error
			if process.persistProcessWithAuthority != nil {
				persistErr = process.persistProcessWithAuthority(process.PID, process.Identity, &callbackAuthority)
			} else {
				persistErr = process.persistAuthority(process.PID, process.Identity, &callbackAuthority)
			}
			if persistErr != nil {
				process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), fmt.Errorf("retry process containment authority persistence: %w", persistErr)))
				return persistErr
			}
			process.containmentAuthorityUncertain = false
		}
		if !process.containmentReceiptSaved {
			if process.persistStopReceipt == nil {
				if process.containmentAuthorityUncertain {
					err := errors.New("containment authority persistence is uncertain")
					process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
					return err
				}
				if process.containmentAuthority != nil {
					err := errors.New("durable containment stop receipt callback is missing")
					process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
					return err
				}
				if !process.stopReceiptRequired {
					if aborter, ok := process.containment.(platform.ContainmentStopReceiptAborter); ok {
						if err := aborter.AbortContainment(); err != nil {
							process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
							return err
						}
						process.containmentReceiptSaved = true
					} else {
						err := errors.New("pre-authority containment abort capability is missing")
						process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
						return err
					}
				} else {
					err := errors.New("durable containment stop receipt callback is missing")
					process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
					return err
				}
			}
			if process.persistStopReceipt != nil {
				if err := process.persistStopReceipt(process.PID, process.Identity, receipt); err != nil {
					process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), fmt.Errorf("persist containment stop receipt: %w", err)))
					return err
				}
				process.containmentReceiptSaved = true
			}
		}
		if process.persistStopReceipt == nil && !process.stopReceiptRequired {
			if process.containmentAuthorityUncertain {
				err := errors.New("containment authority persistence is uncertain")
				process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
				return err
			}
			// AbortContainment already performed the release.
			process.containmentFinalized = true
			finalErr := process.containmentFinalizationBaseError()
			process.setContainmentError(finalErr)
			return finalErr
		}
		if releaser, ok := process.containment.(platform.ContainmentStopReceiptReleaser); ok {
			if err := releaser.ReleaseContainment(); err != nil {
				process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), fmt.Errorf("release containment authority: %w", err)))
				return err
			}
		} else {
			err := errors.New("containment stop receipt releaser is missing")
			process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
			return err
		}
	} else if process.stopReceiptRequired || process.containmentAuthorityUncertain {
		err := errors.New("containment stop receipt is unavailable")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}
	process.containmentFinalized = true
	finalErr := process.containmentFinalizationBaseError()
	process.setContainmentError(finalErr)
	return finalErr
}

func (process *Process) pendingSupervisorHandoff() bool {
	return process != nil && process.containmentHandoff != nil && !process.containmentHandoffCommitted
}

func (process *Process) containmentStopReceiptProvider() (platform.ContainmentStopReceiptProvider, bool) {
	if process == nil || process.containment == nil {
		return nil, false
	}
	provider, ok := process.containment.(platform.ContainmentStopReceiptProvider)
	return provider, ok
}

func (process *Process) closeContainmentOwnerLocked() error {
	if process == nil || process.containment == nil {
		return nil
	}
	closeRetryable := false
	if retryer, ok := process.containment.(platform.ContainmentCloseRetryer); ok {
		closeRetryable = retryer.ContainmentCloseRetryable()
	}
	if !process.containmentCloseAttempted || closeRetryable {
		process.containmentCloseAttempted = true
		if err := process.containment.Close(); err != nil {
			retryableAfterFailure := false
			if retryer, ok := process.containment.(platform.ContainmentCloseRetryer); ok {
				retryableAfterFailure = retryer.ContainmentCloseRetryable()
			}
			if !retryableAfterFailure {
				process.containmentStableError = errors.Join(process.containmentStableError, err)
			}
			process.setContainmentError(errors.Join(process.containmentInitialError, err))
			return err
		}
	}
	return nil
}

func (process *Process) finalizeSupervisorHandoffLocked(provider platform.ContainmentStopReceiptProvider, hasReceiptProvider bool) error {
	if !process.pendingSupervisorHandoff() {
		return process.containmentFinalizeError()
	}
	handoff := process.containmentHandoff.Clone()
	if process.containmentHandoffCommitUnknown {
		if hasReceiptProvider {
			receipt, available := provider.ContainmentStopReceipt()
			if available {
				if !receipt.ValidForHandoff(handoff) {
					err := errors.New("containment stop receipt does not match pending supervisor handoff")
					process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
					return err
				}
				if err := process.recordSupervisorHandoffReceiptLocked(handoff, receipt); err != nil {
					return err
				}
			}
		}
		err := errors.New("supervisor handoff commit outcome is unknown")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}
	if process.containmentHandoffPrepareUnknown || process.containmentHandoffBindUnknown {
		err := errors.New("supervisor handoff cleanup outcome is unknown")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}
	// An unbound handoff cannot be cleared with a release proof. It requires a
	// platform-specific AbortProof after exact owner/helper/target absence has
	// been established; retaining the record is the only safe fallback here.
	if handoff.SupervisorPID == 0 {
		err := errors.New("unbound supervisor handoff requires an explicit abort proof")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}
	if process.clearSupervisorHandoff == nil {
		err := errors.New("supervisor handoff clear callback is missing")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}
	if !hasReceiptProvider {
		err := errors.New("pending supervisor handoff stop receipt provider is missing")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}
	receipt, available := provider.ContainmentStopReceipt()
	if !available {
		err := errors.New("pending supervisor handoff stop receipt is unavailable")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}
	if !receipt.ValidForHandoff(handoff) {
		err := errors.New("containment stop receipt does not match pending supervisor handoff")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}
	if err := process.recordSupervisorHandoffReceiptLocked(handoff, receipt); err != nil {
		return err
	}
	handoff = process.containmentHandoff.Clone()
	if !process.containmentHandoffReleased {
		releaser, ok := process.containment.(platform.ContainmentStopReceiptReleaser)
		if !ok {
			err := errors.New("containment stop receipt releaser is missing")
			process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
			return err
		}
		if err := releaser.ReleaseContainment(); err != nil {
			process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), fmt.Errorf("release pending containment authority: %w", err)))
			return err
		}
		process.containmentHandoffReleased = true
	}
	proof := handoff.ReleaseProof()
	if err := retrySupervisorHandoffClear(process.clearSupervisorHandoff, handoff, proof); err != nil {
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), fmt.Errorf("clear supervisor handoff: %w", err)))
		return err
	}
	process.containmentHandoff = nil
	process.containmentFinalized = true
	finalErr := process.containmentFinalizationBaseError()
	process.setContainmentError(finalErr)
	return finalErr
}

func (process *Process) recordSupervisorHandoffReceiptLocked(handoff authority.SupervisorHandoff, receipt authority.StopReceipt) error {
	if process.containmentHandoffReceiptSaved {
		return nil
	}
	if process.recordSupervisorHandoffStopReceipt == nil {
		err := errors.New("supervisor handoff stop receipt callback is missing")
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), err))
		return err
	}
	if err := retrySupervisorHandoffReceipt(process.recordSupervisorHandoffStopReceipt, handoff, receipt); err != nil {
		process.setContainmentError(errors.Join(process.containmentFinalizationBaseError(), fmt.Errorf("record supervisor handoff stop receipt: %w", err)))
		return err
	}
	withReceipt := handoff.Clone()
	withReceipt.StopReceipt = &receipt
	process.containmentHandoff = &withReceipt
	process.containmentHandoffReceiptSaved = true
	return nil
}

func retrySupervisorHandoffReceipt(callback func(authority.SupervisorHandoff, authority.StopReceipt) error, handoff authority.SupervisorHandoff, receipt authority.StopReceipt) error {
	firstErr := callback(handoff.Clone(), receipt)
	if firstErr == nil {
		return nil
	}
	if retryErr := callback(handoff.Clone(), receipt); retryErr != nil {
		return errors.Join(firstErr, retryErr)
	}
	return nil
}

func retrySupervisorHandoffClear(callback func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error, handoff authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) error {
	firstErr := callback(handoff.Clone(), proof)
	if firstErr == nil {
		return nil
	}
	if retryErr := callback(handoff.Clone(), proof); retryErr != nil {
		return errors.Join(firstErr, retryErr)
	}
	return nil
}

func (process *Process) containmentFailure() error {
	process.errorMutex.Lock()
	defer process.errorMutex.Unlock()
	return process.containmentError
}

func (process *Process) recordContainmentTransientError(err error) {
	if process == nil || err == nil {
		return
	}
	process.errorMutex.Lock()
	if process.containmentTransientError == nil {
		process.containmentTransientError = err
	}
	process.errorMutex.Unlock()
}

func (process *Process) containmentFinalizationBaseError() error {
	return errors.Join(process.containmentInitialError, process.containmentStableError)
}

func (process *Process) containmentFinalizeError() error {
	process.errorMutex.Lock()
	defer process.errorMutex.Unlock()
	return process.containmentError
}

func (process *Process) setContainmentError(err error) {
	process.errorMutex.Lock()
	process.containmentError = err
	if err == nil {
		process.containmentTransientError = nil
	}
	process.errorMutex.Unlock()
}

func (process *Process) stopOutputDelivery() {
	process.outputStopOnce.Do(func() {
		close(process.outputStop)
		process.cancelSink()
	})
}

func (process *Process) outputDeliveryStopped() bool {
	select {
	case <-process.outputStop:
		return true
	default:
		return false
	}
}

func (process *Process) terminationRequested() bool {
	process.terminationMutex.Lock()
	defer process.terminationMutex.Unlock()
	return process.terminationStart
}

func (process *Process) linuxCloneRunOutcome(exitCode int, waitError error) LinuxCloneRunOutcome {
	process.errorMutex.Lock()
	sinkError := process.sinkError
	outputError := process.outputError
	outputTruncated := process.outputTruncated
	containmentError := process.containmentError
	process.errorMutex.Unlock()
	process.terminationMutex.Lock()
	terminated := process.terminationStart || process.terminated
	terminationError := process.terminationError
	process.terminationMutex.Unlock()
	contextCancelled := process.runContext != nil && process.runContext.Err() != nil
	return LinuxCloneRunOutcome{
		ExitCode: exitCode, WaitError: waitError, Terminated: terminated,
		RunContextCancelled: contextCancelled, TerminationError: terminationError,
		SinkError: sinkError, OutputError: outputError, OutputTruncated: outputTruncated,
		PriorContainmentError: containmentError,
	}
}

func (process *Process) recordSinkError(err error) {
	process.errorMutex.Lock()
	defer process.errorMutex.Unlock()
	if process.sinkError == nil {
		process.sinkError = err
	}
}

func (process *Process) recordOutputError(err error) {
	process.errorMutex.Lock()
	defer process.errorMutex.Unlock()
	if process.outputError == nil {
		process.outputError = err
	}
}

func (process *Process) recordOutputTruncated() {
	if process == nil {
		return
	}
	process.errorMutex.Lock()
	process.outputTruncated = true
	process.errorMutex.Unlock()
}

func (process *Process) recordContainmentError(err error) {
	if err == nil {
		return
	}
	process.errorMutex.Lock()
	defer process.errorMutex.Unlock()
	if process.containmentError == nil {
		process.containmentError = err
	}
}

func (process *Process) recordTerminationError(err error) {
	if err == nil {
		return
	}
	process.terminationMutex.Lock()
	defer process.terminationMutex.Unlock()
	process.terminationError = errors.Join(process.terminationError, err)
}

func validateInvocation(invocation Invocation) (string, error) {
	if strings.TrimSpace(invocation.Program) == "" {
		return "", errors.New("program must not be empty")
	}
	program, err := exec.LookPath(invocation.Program)
	if err != nil {
		return "", fmt.Errorf("find program %q: %w", invocation.Program, err)
	}
	if runtime.GOOS == "windows" {
		extension := strings.ToLower(filepath.Ext(program))
		if extension == ".bat" || extension == ".cmd" {
			return "", errors.New("program must not be a batch script; configure a direct executable")
		}
	}
	if invocation.Dir != "" && !filepath.IsAbs(invocation.Dir) {
		return "", errors.New("working directory must be absolute when set")
	}
	for _, entry := range invocation.Env {
		key, _, found := strings.Cut(entry, "=")
		if !found || key == "" || strings.ContainsRune(key, '\x00') || strings.ContainsRune(entry, '\x00') {
			return "", fmt.Errorf("invalid environment entry %q", entry)
		}
		if isControlCredential(key) {
			return "", fmt.Errorf("environment entry %q is a reserved Symmetry control credential", key)
		}
	}
	return program, nil
}

// BuildEnvironment creates an explicit child environment containing only
// operating-system essentials and named variables from the daemon environment.
// SYMMETRY_* values are excluded unless their exact name is allowlisted; control
// credentials remain prohibited even when requested explicitly.
func BuildEnvironment(allowlist ...string) ([]string, error) {
	requested := make(map[string]struct{}, len(allowlist))
	for _, key := range allowlist {
		if key == "" || strings.ContainsAny(key, "=\x00") {
			return nil, fmt.Errorf("invalid environment allowlist key %q", key)
		}
		if isControlCredential(key) {
			return nil, fmt.Errorf("environment allowlist key %q is a reserved Symmetry control credential", key)
		}
		requested[normalizeEnvironmentKey(key)] = struct{}{}
	}

	allowed := make(map[string]struct{}, len(essentialEnvironmentKeys)+len(requested))
	for _, key := range essentialEnvironmentKeys {
		allowed[normalizeEnvironmentKey(key)] = struct{}{}
	}
	for key := range requested {
		allowed[key] = struct{}{}
	}

	values := make(map[string]string, len(allowed))
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, include := allowed[normalizeEnvironmentKey(key)]; include {
			values[key] = value
		}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment, nil
}

var essentialEnvironmentKeys = []string{
	"ComSpec", "HOME", "HOMEDRIVE", "HOMEPATH", "LANG", "LC_ALL", "LC_CTYPE", "PATH", "PATHEXT", "SystemRoot", "SYSTEMROOT", "TEMP", "TERM", "TMP", "TMPDIR", "USERPROFILE", "WINDIR",
}

func isControlCredential(key string) bool {
	normalized := strings.ToUpper(key)
	if strings.HasPrefix(normalized, "SYMMETRY_CONTROL_") || strings.HasPrefix(normalized, "SYMMETRY_AUTH_") {
		return true
	}
	return strings.HasPrefix(normalized, "SYMMETRY_") &&
		(strings.HasSuffix(normalized, "TOKEN") || strings.HasSuffix(normalized, "_SECRET") ||
			normalized == "SYMMETRY_API_KEY" || normalized == "SYMMETRY_CREDENTIAL")
}

func normalizeEnvironmentKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func cloneEnvironment(environment []string) []string {
	cloned := make([]string, len(environment))
	copy(cloned, environment)
	return cloned
}

func closeFiles(files ...*os.File) {
	for _, file := range files {
		if file != nil {
			_ = file.Close()
		}
	}
}
