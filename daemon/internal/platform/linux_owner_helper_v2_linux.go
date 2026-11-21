//go:build linux && amd64

package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

var (
	ErrLinuxOwnerHelperV2OwnerLost         = errors.New("Linux owner helper observed daemon-owner loss")
	ErrLinuxOwnerHelperV2NotCommitted      = errors.New("Linux owner helper has no durable handoff acknowledgement")
	ErrLinuxOwnerHelperV2LeaseUnarmed      = errors.New("Linux owner helper has no acknowledged initial lease")
	ErrLinuxOwnerHelperV2ReleaseUsed       = errors.New("Linux owner helper RELEASE is single-use")
	ErrLinuxOwnerHelperV2StopUnproven      = errors.New("Linux owner helper stop is unproven")
	ErrLinuxOwnerHelperV2RecoveryOnly      = errors.New("Linux owner helper is latched to recovery-only mode")
	ErrLinuxOwnerHelperV2ReleaseUnresolved = errors.New("Linux owner helper release outcome is unresolved")
	errLinuxOwnerHelperV2WitnessExit       = errors.New("Linux owner helper exited at the selected post-RELEASE witness boundary")
	errLinuxOwnerProcessChildrenChanged    = errors.New("Linux process child census snapshot changed")
)

// LinuxOwnerHelperV2Prepared is evidence from the helper's actual parked-gate
// observation. PIDFD is an independently transferred pin for the daemon's
// emergency mirror; the helper retains its own pidfd inside PreparedGate.
type LinuxOwnerHelperV2Prepared struct {
	Gate          authority.LinuxCgroupGateIdentity
	Launcher      authority.LinuxCgroupProcessIdentity
	LauncherExit  authority.LinuxCgroupExitProof
	UserNamespace LinuxUserNamespaceIdentity
	PIDFD         *os.File
}

func (prepared *LinuxOwnerHelperV2Prepared) Close() error {
	if prepared == nil || prepared.PIDFD == nil {
		return nil
	}
	err := prepared.PIDFD.Close()
	prepared.PIDFD = nil
	return err
}

// LinuxOwnerHelperV2PrepareError preserves exact producer evidence on an
// unsuccessful preparation. A returned PIDFD is owned by the caller and is
// never treated as proof of a successful gate or exec.
type LinuxOwnerHelperV2PrepareError struct {
	Code         string
	Launcher     *authority.LinuxCgroupProcessIdentity
	LauncherExit *authority.LinuxCgroupExitProof
	GateProcess  *authority.LinuxCgroupProcessIdentity
	Gate         *authority.LinuxCgroupGateIdentity
	GateOutcome  *authority.LinuxCgroupGateProductionResult
	PIDFD        *os.File
}

func (failure *LinuxOwnerHelperV2PrepareError) Error() string {
	if failure == nil {
		return "Linux owner helper preparation failed"
	}
	return fmt.Sprintf("Linux owner helper preparation failed (%s)", failure.Code)
}

func (failure *LinuxOwnerHelperV2PrepareError) Unwrap() error {
	if failure != nil && failure.GateOutcome != nil {
		return linuxgate.ErrGateNeverProduced
	}
	if failure != nil && failure.Code == "launch_outcome_unknown" {
		return linuxgate.ErrOutcomeUnknown
	}
	return nil
}

func (failure *LinuxOwnerHelperV2PrepareError) Close() error {
	if failure == nil || failure.PIDFD == nil {
		return nil
	}
	err := failure.PIDFD.Close()
	failure.PIDFD = nil
	return err
}

type LinuxOwnerHelperV2Status struct {
	RunKey                LinuxOwnerAnchorRunKey
	Fence                 protocol.Fence
	Helper                authority.LinuxCgroupProcessIdentity
	State                 string
	OwnerLost             bool
	CommitAcknowledged    bool
	ReleaseAttempted      bool
	Released              bool
	LeaseLive             bool
	LeaseSequence         uint64
	StopRequested         bool
	StopAttempted         bool
	ReleaseInFlight       bool
	WitnessDropObserved   bool
	WitnessDropOperation  string
	WitnessActionObserved bool
	WitnessAction         string
	Exit                  *LinuxOwnerHelperV2Exit
	Launcher              *authority.LinuxCgroupProcessIdentity
	LauncherExit          *authority.LinuxCgroupExitProof
	Gate                  *authority.LinuxCgroupGateIdentity
	UserNamespace         *LinuxUserNamespaceIdentity
	PIDFD                 *os.File
	StopEvidence          *LinuxOwnerHelperV2StopEvidence
	ErrorCode             string
}

// LinuxOwnerHelperV2Exit reports a kernel waitid(P_PIDFD) result from the
// helper, which is the gate's actual parent/reaper.
type LinuxOwnerHelperV2Exit struct {
	Process    authority.LinuxCgroupProcessIdentity
	Exited     bool
	ExitCode   int
	Signal     int
	CoreDumped bool
	Proof      authority.LinuxCgroupExitProof
}

type linuxOwnerHelperV2StatusSnapshot struct {
	LeaseLive       bool   `json:"lease_live"`
	LeaseSequence   uint64 `json:"lease_sequence"`
	StopRequested   bool   `json:"stop_requested"`
	StopAttempted   bool   `json:"stop_attempted"`
	ReleaseInFlight bool   `json:"release_in_flight"`
}

// LinuxOwnerHelperV2Client is the daemon's authenticated per-run IPC handle.
// Closing it only closes this connection; the separate OwnerFD is the actual
// daemon-death latch returned by StartHelper.
type LinuxOwnerHelperV2Client struct {
	mu           sync.Mutex
	connection   *net.UnixConn
	grant        LinuxOwnerAnchorGrant
	helper       authority.LinuxCgroupProcessIdentity
	client       authority.LinuxCgroupProcessIdentity
	owner        linuxgate.OwnerConfig
	recoveryOnly bool
	closed       bool
}

func startLinuxOwnerAnchorHelperV2(anchor *linuxOwnerAnchor, grant LinuxOwnerAnchorGrant, ownerProcess authority.LinuxCgroupProcessIdentity, leafFD, ownerPipeFD int, witness *linuxOwnerAnchorWitnessConfig, beforeFork func() error) (*linuxOwnerAnchorRun, error) {
	if anchor == nil || grant.Validate() != nil || ownerProcess.Validate() != nil || leafFD < 0 || ownerPipeFD < 0 ||
		!grant.Anchor.Equal(anchor.identity) || !grant.Parent.Equal(anchor.parent.Identity()) || beforeFork == nil {
		return nil, errors.New("Linux owner anchor helper launch grant is incomplete")
	}
	if witness != nil && (witness.RunID != grant.RunKey.RunID || witness.Generation != grant.RunKey.Generation) {
		return nil, errors.New("Linux owner anchor witness selector differs from the exact RunKey")
	}
	if err := verifyLinuxOwnerHelperEndpointVacant(grant.RecoveryEndpoint, anchor.owner.AUID); err != nil {
		return nil, err
	}
	leaf, err := anchor.parent.AdoptLeaf(grant.Leaf.LeafName, leafFD, grant.Leaf)
	if err != nil {
		return nil, fmt.Errorf("anchor adopt exact helper leaf pin: %w", err)
	}
	defer leaf.Close()
	leafFile, err := leaf.DuplicateFD()
	if err != nil {
		return nil, err
	}
	parentFile, err := anchor.parent.DuplicateFD()
	if err != nil {
		_ = leafFile.Close()
		return nil, err
	}
	ownerRead, err := unix.FcntlInt(uintptr(ownerPipeFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		closeLinuxOwnerHelperFiles(leafFile, parentFile)
		return nil, fmt.Errorf("duplicate daemon owner-death pipe for helper: %w", err)
	}
	ownerFile := os.NewFile(uintptr(ownerRead), "symmetry-owner-helper-owner-latch")
	if ownerFile == nil {
		_ = unix.Close(ownerRead)
		closeLinuxOwnerHelperFiles(leafFile, parentFile)
		return nil, errors.New("wrap daemon owner-death pipe for helper")
	}
	bootFDs, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		closeLinuxOwnerHelperFiles(leafFile, parentFile, ownerFile)
		return nil, fmt.Errorf("create fixed helper bootstrap channel: %w", err)
	}
	anchorBoot := os.NewFile(uintptr(bootFDs[0]), "symmetry-owner-anchor-helper-bootstrap")
	helperBoot := os.NewFile(uintptr(bootFDs[1]), "symmetry-owner-helper-bootstrap")
	if anchorBoot == nil || helperBoot == nil {
		_ = unix.Close(bootFDs[0])
		_ = unix.Close(bootFDs[1])
		closeLinuxOwnerHelperFiles(leafFile, parentFile, ownerFile)
		return nil, errors.New("wrap fixed helper bootstrap channel")
	}
	command := exec.Command("/proc/self/exe", "-linux-owner-helper-v2")
	command.Args = []string{"symmetry-daemon", "-linux-owner-helper-v2"}
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	command.Dir = "/"
	command.ExtraFiles = []*os.File{helperBoot, leafFile, parentFile, ownerFile}
	helperPIDFD := -1
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, PidFD: &helperPIDFD}
	if err := beforeFork(); err != nil {
		closeLinuxOwnerHelperFiles(anchorBoot, helperBoot, leafFile, parentFile, ownerFile)
		return nil, fmt.Errorf("reserve exact RunKey before helper fork: %w", err)
	}
	if err := command.Start(); err != nil {
		closeLinuxOwnerHelperFiles(anchorBoot, helperBoot, leafFile, parentFile, ownerFile)
		return nil, fmt.Errorf("start the fixed hidden Linux owner helper: %w", err)
	}
	closeLinuxOwnerHelperFiles(helperBoot, leafFile, parentFile, ownerFile)
	if helperPIDFD < 0 {
		_ = anchorBoot.Close()
		return quarantineLinuxOwnerAnchorRun(grant, anchor.owner, authority.LinuxCgroupProcessIdentity{}, nil, leaf),
			errors.New("Linux owner helper was started without an exact pidfd; retain the RunKey as unresolved")
	}
	helperIdentity, identityErr := ProcessIdentity(command.Process.Pid)
	if identityErr != nil {
		file := os.NewFile(uintptr(helperPIDFD), "symmetry-owner-anchor-unresolved-helper-pidfd")
		quarantine := quarantineLinuxOwnerAnchorRun(grant, anchor.owner, authority.LinuxCgroupProcessIdentity{}, file, leaf)
		if file != nil {
			_ = pidfdKillAndReapUnidentified(int(file.Fd()))
			_ = file.Close()
		}
		_ = anchorBoot.Close()
		return quarantine, fmt.Errorf("capture exact new helper identity before granting it the RunKey: %w", identityErr)
	}
	helper := authority.LinuxCgroupProcessIdentity{PID: command.Process.Pid, Identity: helperIdentity}
	pidfdFile := os.NewFile(uintptr(helperPIDFD), "symmetry-owner-anchor-helper-pidfd")
	if pidfdFile == nil {
		_ = anchorBoot.Close()
		return nil, errors.New("wrap exact Linux owner helper pidfd")
	}
	run, err := newLinuxOwnerAnchorRun(grant, helper, pidfdFile, leaf, anchor.owner)
	if err != nil {
		quarantine := quarantineLinuxOwnerAnchorRun(grant, anchor.owner, helper, pidfdFile, leaf)
		_ = pidfdKillAndReap(pidfdFile, helper)
		_ = pidfdFile.Close()
		_ = anchorBoot.Close()
		return quarantine, fmt.Errorf("retain exact anchor/helper/leaf ownership before grant: %w", err)
	}
	_ = pidfdFile.Close()
	bootstrap := linuxOwnerHelperV2Bootstrap{
		Version: linuxOwnerHelperV2Version, Grant: grant, AnchorSocket: anchor.socketPath,
		OwnerProcess: ownerProcess,
	}
	if witness != nil {
		bootstrap.WitnessDropResponse = witness.DropResponse
		bootstrap.WitnessHelperAction = witness.HelperAction
		bootstrap.WitnessHoldForReleaseFile = witness.HoldForReleaseFile
		bootstrap.WitnessRunID = witness.RunID
		bootstrap.WitnessGeneration = witness.Generation
	}
	encoded, err := json.Marshal(bootstrap)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		_ = anchorBoot.Close()
		return run, errors.Join(errors.New("encode bounded Linux owner helper bootstrap"), err)
	}
	if err := writeLinuxOwnerHelperV2Bootstrap(anchorBoot, encoded); err != nil {
		_ = anchorBoot.Close()
		return run, fmt.Errorf("send authenticated fixed helper bootstrap: %w", err)
	}
	ready, err := readLinuxOwnerHelperV2BootstrapReady(anchorBoot, linuxOwnerHelperV2StartupWait)
	_ = anchorBoot.Close()
	if err != nil {
		return run, fmt.Errorf("wait for exact helper startup acknowledgement: %w", err)
	}
	if ready.Version != linuxOwnerHelperV2Version || ready.Status != "ready" || !ready.Helper.Equal(helper) || ready.Endpoint != grant.RecoveryEndpoint {
		return run, errors.New("Linux owner helper startup acknowledgement differs from the exact child and endpoint")
	}
	if err := verifyLinuxOwnerAProcess(helper.PID, helper.Identity, anchor.owner.AUID, anchor.owner.AGID, anchor.identity.PID, anchor.managerPath); err != nil {
		return run, fmt.Errorf("verify launched helper credentials and manager membership: %w", err)
	}
	if err := verifyLinuxOwnerAnchorPIDFD(int(run.helperPIDFD.Fd()), helper); err != nil {
		return run, fmt.Errorf("verify launched helper pidfd after bootstrap: %w", err)
	}
	return run, nil
}

func (client *LinuxOwnerHelperV2Client) newRequest(operation string) linuxOwnerHelperV2Request {
	return linuxOwnerHelperV2Request{
		Version: linuxOwnerHelperV2Version, Operation: operation,
		ClientPID: client.client.PID, ClientIdentity: client.client.Identity,
		Grant: client.grant,
	}
}

// LinuxOwnerHelperV2ReleaseUnresolvedError preserves the measured physical
// stop evidence from a post-RELEASE failure. The caller still owns the
// unresolved run, but no longer has to treat an executed native target as if
// no stop evidence existed.
type LinuxOwnerHelperV2ReleaseUnresolvedError struct {
	Code     string
	Evidence *LinuxOwnerHelperV2StopEvidence
}

func (failure *LinuxOwnerHelperV2ReleaseUnresolvedError) Error() string {
	if failure == nil {
		return ErrLinuxOwnerHelperV2ReleaseUnresolved.Error()
	}
	if failure.Code == "" {
		return ErrLinuxOwnerHelperV2ReleaseUnresolved.Error()
	}
	return fmt.Sprintf("%s (%s)", ErrLinuxOwnerHelperV2ReleaseUnresolved, failure.Code)
}

func (failure *LinuxOwnerHelperV2ReleaseUnresolvedError) Unwrap() error {
	return ErrLinuxOwnerHelperV2ReleaseUnresolved
}

func (client *LinuxOwnerHelperV2Client) setDeadline(ctx context.Context, timeout time.Duration) error {
	if client == nil || ctx == nil || client.connection == nil || client.closed {
		return errors.New("Linux owner helper client is closed")
	}
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	if ctxDeadline, ok := ctx.Deadline(); ok && (deadline.IsZero() || ctxDeadline.Before(deadline)) {
		deadline = ctxDeadline
	}
	return client.connection.SetDeadline(deadline)
}

func (client *LinuxOwnerHelperV2Client) request(ctx context.Context, request linuxOwnerHelperV2Request, fds ...int) (linuxOwnerHelperV2Response, []int, error) {
	if client == nil || ctx == nil {
		return linuxOwnerHelperV2Response{}, nil, errors.New("Linux owner helper request is incomplete")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.setDeadline(ctx, linuxOwnerHelperV2Timeout); err != nil {
		return linuxOwnerHelperV2Response{}, nil, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = client.connection.Close() })
	defer stopClose()
	if err := writeLinuxOwnerHelperV2Request(client.connection, request, fds...); err != nil {
		return linuxOwnerHelperV2Response{}, nil, err
	}
	response, rights, err := readLinuxOwnerHelperV2Response(client.connection)
	if err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return linuxOwnerHelperV2Response{}, nil, err
	}
	if !response.matches(client.grant, client.helper) || response.Operation != request.Operation {
		closeLinuxOwnerAnchorFDs(rights)
		return linuxOwnerHelperV2Response{}, nil, errors.New("Linux owner helper response differs from the exact request grant")
	}
	_ = client.connection.SetDeadline(time.Time{})
	return response, rights, nil
}

func (client *LinuxOwnerHelperV2Client) Prepare(ctx context.Context, onLauncherStart func(context.Context, authority.LinuxCgroupProcessIdentity) error) (*LinuxOwnerHelperV2Prepared, error) {
	if client == nil || onLauncherStart == nil {
		return nil, errors.New("Linux owner helper prepare requires a durable launcher-ack callback")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.setDeadline(ctx, linuxOwnerHelperV2Timeout); err != nil {
		return nil, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = client.connection.Close() })
	defer stopClose()
	if err := writeLinuxOwnerHelperV2Request(client.connection, client.newRequest("prepare")); err != nil {
		return nil, err
	}
	var callbackErr error
	for {
		response, rights, err := readLinuxOwnerHelperV2Response(client.connection)
		if err != nil {
			closeLinuxOwnerAnchorFDs(rights)
			return nil, err
		}
		if !response.matches(client.grant, client.helper) {
			closeLinuxOwnerAnchorFDs(rights)
			return nil, errors.New("Linux owner helper prepare event differs from the exact RunKey and helper")
		}
		switch response.Operation {
		case "launcher-start":
			if len(rights) != 0 || response.Launcher == nil || response.Launcher.Validate() != nil {
				closeLinuxOwnerAnchorFDs(rights)
				return nil, errors.New("Linux owner helper launcher event is incomplete")
			}
			callbackErr = onLauncherStart(ctx, *response.Launcher)
			ack := client.newRequest("launcher-ack")
			ack.Launcher = response.Launcher
			ack.Accepted = callbackErr == nil
			if err := writeLinuxOwnerHelperV2Request(client.connection, ack); err != nil {
				return nil, err
			}
		case "gate-ready":
			if response.Status != "ready" || response.Gate == nil || response.Gate.Validate() != nil ||
				response.Launcher == nil || response.LauncherExit == nil || response.UserNamespace == nil || len(rights) != 1 {
				closeLinuxOwnerAnchorFDs(rights)
				return nil, errors.New("Linux owner helper returned an incomplete parked-gate event")
			}
			if err := verifyLinuxOwnerAnchorPIDFD(rights[0], response.Gate.Process); err != nil {
				closeLinuxOwnerAnchorFDs(rights)
				return nil, fmt.Errorf("verify daemon mirror pidfd for parked A gate: %w", err)
			}
			pidfd := os.NewFile(uintptr(rights[0]), "symmetry-daemon-linux-owner-gate-pidfd")
			if pidfd == nil {
				closeLinuxOwnerAnchorFDs(rights)
				return nil, errors.New("wrap transferred parked-gate pidfd")
			}
			prepared := &LinuxOwnerHelperV2Prepared{
				Gate: *response.Gate, Launcher: *response.Launcher, LauncherExit: *response.LauncherExit,
				UserNamespace: *response.UserNamespace, PIDFD: pidfd,
			}
			if err := prepared.ValidateFor(client.grant, client.helper); err != nil {
				_ = prepared.Close()
				return nil, err
			}
			_ = client.connection.SetDeadline(time.Time{})
			return prepared, nil
		case "prepare-failed":
			failure := &LinuxOwnerHelperV2PrepareError{
				Code: response.ErrorCode, Launcher: cloneLinuxOwnerProcessIdentity(response.Launcher),
				LauncherExit: cloneLinuxOwnerAnchorExitProof(response.LauncherExit),
				GateProcess:  cloneLinuxOwnerProcessIdentity(response.GateProcess),
				Gate:         cloneLinuxOwnerGateIdentity(response.Gate), GateOutcome: response.GateOutcome,
			}
			if response.GateProcess != nil {
				if len(rights) != 1 || verifyLinuxOwnerAnchorPIDFDIdentity(rights[0], *response.GateProcess) != nil {
					closeLinuxOwnerAnchorFDs(rights)
					return nil, errors.New("unresolved Linux owner gate did not carry its exact pidfd")
				}
				failure.PIDFD = os.NewFile(uintptr(rights[0]), "symmetry-unresolved-linux-owner-gate-pidfd")
				if failure.PIDFD == nil {
					closeLinuxOwnerAnchorFDs(rights)
					return nil, errors.New("wrap unresolved Linux owner gate pidfd")
				}
			} else if len(rights) != 0 {
				closeLinuxOwnerAnchorFDs(rights)
				return nil, errors.New("gate-free Linux owner preparation carried an unexpected pidfd")
			}
			if failure.GateOutcome != nil {
				if failure.Launcher == nil || failure.LauncherExit == nil || failure.GateOutcome.ValidateFor(client.grant.LaunchToken, *failure.Launcher) != nil ||
					!failure.LauncherExit.ValidForOwnedDescendant(*failure.Launcher, client.helper, client.grant.Anchor, nil) {
					_ = failure.Close()
					return nil, errors.New("Linux no-gate result lacks exact authenticated producer-exit evidence")
				}
			}
			_ = client.connection.SetDeadline(time.Time{})
			if callbackErr != nil {
				return nil, errors.Join(fmt.Errorf("durably bind and arm exact Linux root launcher: %w", callbackErr), failure)
			}
			return nil, failure
		default:
			closeLinuxOwnerAnchorFDs(rights)
			return nil, fmt.Errorf("unexpected Linux owner helper prepare event %q", response.Operation)
		}
	}
}

func (prepared *LinuxOwnerHelperV2Prepared) ValidateFor(grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity) error {
	if prepared == nil || grant.Validate() != nil || helper.Validate() != nil || prepared.Gate.Validate() != nil ||
		prepared.Launcher.Validate() != nil || prepared.LauncherExit.Validate() != nil ||
		!prepared.LauncherExit.ValidForOwnedDescendant(prepared.Launcher, helper, grant.Anchor, nil) ||
		prepared.Gate.Process.Validate() != nil || prepared.Gate.HostUID != grant.OwnerUID ||
		!prepared.Gate.Cgroup.Equal(grant.Leaf) || !prepared.UserNamespace.valid() || prepared.PIDFD == nil {
		return errors.New("Linux owner helper prepared-gate evidence is not bound to the exact run")
	}
	return verifyLinuxOwnerAnchorPIDFD(int(prepared.PIDFD.Fd()), prepared.Gate.Process)
}

func (client *LinuxOwnerHelperV2Client) Commit(ctx context.Context, prepared LinuxOwnerHelperV2Prepared) error {
	if err := prepared.ValidateFor(client.grant, client.helper); err != nil {
		return err
	}
	request := client.newRequest("commit")
	request.Gate = &prepared.Gate
	request.Launcher = &prepared.Launcher
	request.LauncherExit = &prepared.LauncherExit
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if (response.Status != "committed" && response.Status != "already_committed") || len(rights) != 0 {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

// AcknowledgeHelper opens the one RunKey-scoped launch phase. The caller must
// invoke it only after its Store callback durably bound this exact helper and
// grant; the helper has no Store access and will not infer that ACK from a
// returned PID or socket connection.
func (client *LinuxOwnerHelperV2Client) AcknowledgeHelper(ctx context.Context) error {
	request := client.newRequest("helper-bound")
	request.Helper = cloneLinuxOwnerProcessIdentity(&client.helper)
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "bound" && response.Status != "already_bound" {
		return errors.New("Linux owner helper did not accept the durable helper/grant acknowledgement")
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) RenewLease(ctx context.Context, deadline time.Duration, sequence uint64) error {
	if deadline <= 0 || sequence == 0 {
		return errors.New("Linux owner helper lease deadline and sequence must be positive")
	}
	request := client.newRequest("renew-lease")
	request.LeaseNanos = int64(deadline)
	request.Sequence = sequence
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "acknowledged" && response.Status != "already_acknowledged" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	if response.LeaseSequence != sequence {
		return errors.New("Linux owner helper lease acknowledgement sequence differs")
	}
	return nil
}

// Release requests one guarded production clone or retained-reuse release. A
// clone callback must durably persist and read back the exact checkout receipt;
// no B exec proceeds unless it returns nil, and an unknown result never retries
// RELEASE.
func (client *LinuxOwnerHelperV2Client) Release(
	ctx context.Context,
	command linuxgate.NativeCommand,
	onCheckoutReady func(context.Context, authority.LinuxCloneCheckoutReceipt) error,
) error {
	if client == nil || ctx == nil {
		return errors.New("Linux owner helper release client is incomplete")
	}
	if (command.Clone == nil && command.Reuse == nil) || (command.Clone == nil && onCheckoutReady != nil) ||
		!linuxOwnerHelperV2CloneGrantMatchesRun(command, client.grant, command.Dir) {
		return errors.New("production Linux owner helper release requires exactly one exact B-owned clone or reuse grant")
	}
	if err := command.Validate(); err != nil {
		return err
	}
	var cloneGrant *authority.LinuxCloneGrant
	if command.Clone != nil {
		copy := command.Clone.Clone()
		cloneGrant = &copy
	}
	var reuse *authority.LinuxCloneReuseGrant
	if command.Reuse != nil {
		copy := command.Reuse.Clone()
		reuse = &copy
	}
	wire := &linuxOwnerHelperV2Command{
		Path: command.Path, Args: append([]string(nil), command.Args...),
		Env: append([]string(nil), command.Env...), Dir: command.Dir, Clone: cloneGrant, Reuse: reuse,
	}
	request := client.newRequest("release")
	request.Command = wire
	fds := []int{-1, -1, -1}
	if command.Stdin != nil {
		fds[0] = int(command.Stdin.Fd())
	}
	if command.Stdout != nil {
		fds[1] = int(command.Stdout.Fd())
	}
	if command.Stderr != nil {
		fds[2] = int(command.Stderr.Fd())
	}
	return client.releaseWithCheckoutReady(ctx, request, fds, cloneGrant, onCheckoutReady)
}

func (client *LinuxOwnerHelperV2Client) releaseWithCheckoutReady(
	ctx context.Context,
	request linuxOwnerHelperV2Request,
	fds []int,
	cloneGrant *authority.LinuxCloneGrant,
	onCheckoutReady func(context.Context, authority.LinuxCloneCheckoutReceipt) error,
) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.setDeadline(ctx, linuxOwnerHelperV2Timeout); err != nil {
		return err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = client.connection.Close() })
	defer stopClose()
	if err := writeLinuxOwnerHelperV2Request(client.connection, request, fds...); err != nil {
		client.invalidateLocked()
		return err
	}
	checkoutSeen := false
	for {
		response, rights, err := readLinuxOwnerHelperV2Response(client.connection)
		closeLinuxOwnerAnchorFDs(rights)
		if err != nil {
			client.invalidateLocked()
			return err
		}
		if !response.matches(client.grant, client.helper) || len(rights) != 0 {
			client.invalidateLocked()
			return errors.New("Linux owner helper release event differs from the exact request grant")
		}
		switch response.Operation {
		case "checkout-ready":
			if cloneGrant == nil || checkoutSeen || response.Status != "ready" || response.CheckoutReceipt == nil ||
				!linuxOwnerHelperV2CheckoutReceiptMatches(*response.CheckoutReceipt, *cloneGrant) {
				client.invalidateLocked()
				return errors.New("Linux owner helper checkout-ready receipt differs from the exact clone grant")
			}
			checkoutSeen = true
			receipt := response.CheckoutReceipt.Clone()
			var callbackErr error
			if response.OwnerLost {
				callbackErr = ErrLinuxOwnerHelperV2OwnerLost
			} else if !response.CommitAcknowledged || !response.ReleaseAttempted || response.Released {
				callbackErr = errors.New("Linux owner helper checkout event is outside the committed one-use release phase")
			} else {
				callbackErr = onCheckoutReady(ctx, receipt)
			}
			ack := client.newRequest("checkout-ready-ack")
			ack.Accepted = callbackErr == nil
			ack.CheckoutReceipt = &receipt
			if err := writeLinuxOwnerHelperV2Request(client.connection, ack); err != nil {
				client.invalidateLocked()
				return errors.Join(callbackErr, err)
			}
			if callbackErr != nil {
				final, finalRights, readErr := readLinuxOwnerHelperV2Response(client.connection)
				closeLinuxOwnerAnchorFDs(finalRights)
				if readErr != nil {
					client.invalidateLocked()
					return errors.Join(callbackErr, readErr)
				}
				if !final.matches(client.grant, client.helper) || len(finalRights) != 0 || final.Operation != "release" {
					client.invalidateLocked()
					return errors.Join(callbackErr, errors.New("Linux owner helper omitted the terminal release result after checkout rejection"))
				}
				_ = client.connection.SetDeadline(time.Time{})
				if final.Status == "released" {
					return errors.Join(callbackErr, errors.New("Linux owner helper released the Clone after checkout callback rejection"))
				}
				return callbackErr
			}
		case "release":
			if response.Status != "released" {
				_ = client.connection.SetDeadline(time.Time{})
				return linuxOwnerHelperV2ResponseError(response)
			}
			if (cloneGrant != nil && !checkoutSeen) || response.OwnerLost || !response.CommitAcknowledged || !response.ReleaseAttempted ||
				!response.Released || response.CheckoutReceipt != nil {
				client.invalidateLocked()
				return errors.New("Linux owner helper release response violates the durable Clone checkout contract")
			}
			_ = client.connection.SetDeadline(time.Time{})
			return nil
		default:
			client.invalidateLocked()
			return fmt.Errorf("Linux owner helper sent unexpected release event %q", response.Operation)
		}
	}
}

func (client *LinuxOwnerHelperV2Client) invalidateLocked() {
	if client == nil || client.closed {
		return
	}
	client.closed = true
	if client.connection != nil {
		_ = client.connection.Close()
	}
}

func (client *LinuxOwnerHelperV2Client) Stop(ctx context.Context) (LinuxOwnerHelperV2StopEvidence, error) {
	if client == nil || ctx == nil {
		return LinuxOwnerHelperV2StopEvidence{}, errors.New("Linux owner helper stop client is incomplete")
	}
	stopClient, err := client.openParallelControl(ctx)
	if err != nil {
		return LinuxOwnerHelperV2StopEvidence{}, err
	}
	defer stopClient.Close()
	request := stopClient.newRequest("stop")
	response, rights, err := stopClient.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return LinuxOwnerHelperV2StopEvidence{}, err
	}
	if response.Status != "stopped" || response.StopEvidence == nil {
		return LinuxOwnerHelperV2StopEvidence{}, errors.Join(ErrLinuxOwnerHelperV2StopUnproven, linuxOwnerHelperV2ResponseError(response))
	}
	if response.StopEvidence.PreGate != nil {
		if err := response.StopEvidence.ValidateFor(client.grant, client.helper, authority.LinuxCgroupProcessIdentity{}, authority.LinuxCgroupGateIdentity{}); err != nil {
			return LinuxOwnerHelperV2StopEvidence{}, err
		}
		return *response.StopEvidence, nil
	}
	var launcher authority.LinuxCgroupProcessIdentity
	var gate authority.LinuxCgroupGateIdentity
	if response.Launcher != nil {
		launcher = *response.Launcher
	}
	if response.Gate != nil {
		gate = *response.Gate
	}
	if err := response.StopEvidence.ValidateFor(client.grant, client.helper, launcher, gate); err != nil {
		return LinuxOwnerHelperV2StopEvidence{}, err
	}
	return *response.StopEvidence, nil
}

// FenceBeforeAbort terminalizes a helper whose protocol.Prepare entrypoint was
// never called. App then waits/reaps the helper through the anchor before it
// asks State to abort the reserved leaf; an empty cgroup alone is not enough.
func (client *LinuxOwnerHelperV2Client) FenceBeforeAbort(ctx context.Context) (LinuxOwnerHelperV2PreGateStopEvidence, error) {
	request := client.newRequest("fence-before-abort")
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return LinuxOwnerHelperV2PreGateStopEvidence{}, err
	}
	if response.Status != "fenced_before_abort" || response.StopEvidence == nil || response.StopEvidence.PreGate == nil {
		return LinuxOwnerHelperV2PreGateStopEvidence{}, linuxOwnerHelperV2ResponseError(response)
	}
	if err := response.StopEvidence.PreGate.ValidateFor(client.grant, client.helper); err != nil || response.StopEvidence.PreGate.PrepareAttempted {
		return LinuxOwnerHelperV2PreGateStopEvidence{}, errors.New("helper fence evidence is not the exact no-Prepare reservation branch")
	}
	return *response.StopEvidence.PreGate, nil
}

// Wait blocks for exact helper-owned gate wait/reap. The pidfd readiness
// notification alone is never returned as an exit result.
func (client *LinuxOwnerHelperV2Client) Wait(ctx context.Context) (LinuxOwnerHelperV2Exit, error) {
	if client == nil || ctx == nil {
		return LinuxOwnerHelperV2Exit{}, errors.New("Linux owner helper wait is incomplete")
	}
	waitClient, err := client.openParallelControl(ctx)
	if err != nil {
		return LinuxOwnerHelperV2Exit{}, err
	}
	defer waitClient.Close()
	waitClient.mu.Lock()
	defer waitClient.mu.Unlock()
	if err := waitClient.setDeadline(ctx, 0); err != nil {
		return LinuxOwnerHelperV2Exit{}, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = waitClient.connection.Close() })
	defer stopClose()
	request := waitClient.newRequest("wait")
	if err := writeLinuxOwnerHelperV2Request(waitClient.connection, request); err != nil {
		return LinuxOwnerHelperV2Exit{}, err
	}
	response, rights, err := readLinuxOwnerHelperV2Response(waitClient.connection)
	if err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Exit{}, err
	}
	if !response.matches(waitClient.grant, waitClient.helper) || response.Operation != "wait" || len(rights) != 0 ||
		response.Status != "waited" || response.ExitResult == nil || response.Gate == nil {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Exit{}, linuxOwnerHelperV2ResponseError(response)
	}
	result := *response.ExitResult
	if err := result.Proof.Validate(); err != nil || !result.Process.Equal(response.Gate.Process) ||
		!result.Proof.ValidForOwnedDescendant(result.Process, waitClient.helper, waitClient.grant.Anchor, nil) {
		return LinuxOwnerHelperV2Exit{}, errors.New("Linux owner helper wait result lacks exact gate reaping evidence")
	}
	_ = waitClient.connection.SetDeadline(time.Time{})
	return result, nil
}

func (client *LinuxOwnerHelperV2Client) openParallelControl(ctx context.Context) (*LinuxOwnerHelperV2Client, error) {
	if client == nil || ctx == nil {
		return nil, errors.New("Linux owner helper parallel control connection is incomplete")
	}
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil || identity != client.client.Identity {
		return nil, fmt.Errorf("authenticate the original daemon for parallel helper control: %w", errOr(err, errors.New("daemon creation identity changed")))
	}
	connected, err := (&net.Dialer{Timeout: linuxOwnerHelperV2Timeout}).DialContext(ctx, "unixpacket", client.grant.RecoveryEndpoint)
	if err != nil {
		return nil, sanitizeLinuxOwnerAnchorSocketError(err)
	}
	connection, ok := connected.(*net.UnixConn)
	if !ok {
		_ = connected.Close()
		return nil, errors.New("Linux owner helper endpoint is not a Unix seqpacket socket")
	}
	peer, err := linuxOwnerHelperPeerFromConn(connection, client.owner)
	if err != nil || peer.PID != client.helper.PID || peer.Identity != client.helper.Identity {
		_ = connection.Close()
		return nil, fmt.Errorf("authenticate exact helper for parallel wait: %w", errOr(err, errors.New("helper identity differs")))
	}
	parallel := &LinuxOwnerHelperV2Client{
		connection: connection, grant: client.grant, helper: client.helper,
		client: client.client, owner: client.owner, recoveryOnly: client.recoveryOnly,
	}
	if err := parallel.setDeadline(ctx, linuxOwnerHelperV2Timeout); err != nil {
		_ = connection.Close()
		return nil, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopClose()
	if err := writeLinuxOwnerHelperV2Request(connection, parallel.newRequest("hello")); err != nil {
		_ = connection.Close()
		return nil, err
	}
	response, rights, err := readLinuxOwnerHelperV2Response(connection)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil || !response.matches(client.grant, client.helper) || response.Operation != "hello" || len(rights) != 0 ||
		(response.Status != "ready" && response.Status != "recovery_only") {
		_ = connection.Close()
		return nil, fmt.Errorf("authenticate parallel Linux owner helper session: %w", errOr(err, errors.New("helper denied the exact run grant")))
	}
	parallel.recoveryOnly = response.Status == "recovery_only"
	_ = connection.SetDeadline(time.Time{})
	return parallel, nil
}

func (client *LinuxOwnerHelperV2Client) Status(ctx context.Context) (LinuxOwnerHelperV2Status, error) {
	if client == nil || ctx == nil {
		return LinuxOwnerHelperV2Status{}, errors.New("Linux owner helper status client is incomplete")
	}
	statusClient, err := client.openParallelControl(ctx)
	if err != nil {
		return LinuxOwnerHelperV2Status{}, err
	}
	defer statusClient.Close()
	request := statusClient.newRequest("status")
	response, rights, err := statusClient.request(ctx, request)
	if err != nil {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Status{}, err
	}
	snapshot := response.StatusSnapshot
	if snapshot == nil || snapshot.LeaseSequence != response.LeaseSequence ||
		snapshot.LeaseLive && snapshot.LeaseSequence == 0 ||
		snapshot.StopAttempted && !snapshot.StopRequested ||
		snapshot.ReleaseInFlight && !response.ReleaseAttempted || response.Released && !response.ReleaseAttempted {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Status{}, errors.New("Linux owner helper Status lacks a consistent fresh lease/stop snapshot")
	}
	if response.WitnessActionObserved &&
		(response.WitnessAction != linuxOwnerHelperV2WitnessCrashAfterRelease || !response.ReleaseAttempted || !response.Released) ||
		!response.WitnessActionObserved && response.WitnessAction != "" {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Status{}, errors.New("Linux owner helper Status has an invalid post-RELEASE witness action")
	}
	status := LinuxOwnerHelperV2Status{
		RunKey: response.RunKey, Fence: response.Fence, Helper: response.Helper,
		State: response.Status, OwnerLost: response.OwnerLost,
		CommitAcknowledged: response.CommitAcknowledged,
		ReleaseAttempted:   response.ReleaseAttempted, Released: response.Released,
		LeaseLive: snapshot.LeaseLive, LeaseSequence: snapshot.LeaseSequence,
		StopRequested: snapshot.StopRequested, StopAttempted: snapshot.StopAttempted,
		ReleaseInFlight:     snapshot.ReleaseInFlight,
		WitnessDropObserved: response.WitnessDropObserved, WitnessDropOperation: response.WitnessDropOperation,
		WitnessActionObserved: response.WitnessActionObserved, WitnessAction: response.WitnessAction,
		Exit:          response.ExitResult,
		Launcher:      cloneLinuxOwnerProcessIdentity(response.Launcher),
		LauncherExit:  cloneLinuxOwnerAnchorExitProof(response.LauncherExit),
		Gate:          cloneLinuxOwnerGateIdentity(response.Gate),
		UserNamespace: response.UserNamespace,
		StopEvidence:  response.StopEvidence, ErrorCode: response.ErrorCode,
	}
	if response.Gate != nil {
		if len(rights) == 1 {
			if err := verifyLinuxOwnerAnchorPIDFDIdentity(rights[0], response.Gate.Process); err != nil {
				closeLinuxOwnerAnchorFDs(rights)
				return LinuxOwnerHelperV2Status{}, err
			}
			status.PIDFD = os.NewFile(uintptr(rights[0]), "symmetry-recovered-linux-owner-gate-pidfd")
			if status.PIDFD == nil {
				closeLinuxOwnerAnchorFDs(rights)
				return LinuxOwnerHelperV2Status{}, errors.New("wrap recovered Linux owner gate pidfd")
			}
			captured, err := CaptureLinuxUserNamespaceIdentity(response.Gate.Process.PID, response.Gate.Process.Identity, status.PIDFD)
			if err != nil || response.UserNamespace == nil || captured != *response.UserNamespace {
				_ = status.PIDFD.Close()
				return LinuxOwnerHelperV2Status{}, fmt.Errorf("verify recovered gate user namespace: %w", errOr(err, errors.New("namespace identity differs")))
			}
		} else if len(rights) != 0 || response.Status != "stopped" {
			closeLinuxOwnerAnchorFDs(rights)
			return LinuxOwnerHelperV2Status{}, errors.New("Linux owner helper recovery status lacks the exact retained gate pidfd")
		}
	} else if len(rights) != 0 {
		closeLinuxOwnerAnchorFDs(rights)
		return LinuxOwnerHelperV2Status{}, errors.New("gate-free helper status carried an unexpected pidfd")
	}
	return status, nil
}

func (status *LinuxOwnerHelperV2Status) Close() error {
	if status == nil || status.PIDFD == nil {
		return nil
	}
	err := status.PIDFD.Close()
	status.PIDFD = nil
	return err
}

func (client *LinuxOwnerHelperV2Client) AcknowledgeStopReceipt(ctx context.Context, receipt authority.StopReceipt) error {
	request := client.newRequest("ack-stop-receipt")
	request.Receipt = &receipt
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "receipt_acknowledged" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) AcknowledgePreGateStopReceipt(ctx context.Context, receipt authority.LinuxCgroupPreGateStopReceipt) error {
	request := client.newRequest("ack-stop-receipt")
	request.PreGateReceipt = &receipt
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "receipt_acknowledged" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) ReleaseAfterClear(ctx context.Context, proof authority.SupervisorHandoffReleaseProof) error {
	request := client.newRequest("release-run")
	request.ReleaseProof = &proof
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "released_after_clear" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) ReleaseAfterPreGateClear(ctx context.Context, proof authority.LinuxCgroupPreGateReleaseProof) error {
	request := client.newRequest("release-run")
	request.PreGateReleaseProof = &proof
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "released_after_clear" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) ReleaseAfterReservationAbort(ctx context.Context, proof authority.LinuxCgroupReservationAbortProof) error {
	request := client.newRequest("release-run")
	request.AbortProof = &proof
	response, rights, err := client.request(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Status != "released_after_clear" {
		return linuxOwnerHelperV2ResponseError(response)
	}
	return nil
}

func (client *LinuxOwnerHelperV2Client) Close() error {
	if client == nil {
		return nil
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closed {
		return nil
	}
	client.closed = true
	if client.connection == nil {
		return nil
	}
	err := client.connection.Close()
	client.connection = nil
	return err
}

func newLinuxOwnerHelperV2Lease() *linuxOwnerHelperV2Lease {
	return &linuxOwnerHelperV2Lease{wake: make(chan struct{}, 1)}
}

func (client *LinuxOwnerAnchorClient) RetireHelper(ctx context.Context, grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity, proof authority.SupervisorHandoffReleaseProof) error {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil || proof.Validate() != nil {
		return errors.New("Linux owner anchor retirement is incomplete")
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationRetire,
		Grant: grant, Helper: &helper, ReleaseProof: &proof,
	}
	response, rights, err := client.exchange(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if !response.matches(grant, linuxOwnerAnchorOperationRetire) || (response.Status != "retired" && response.Status != "already_retired") || len(rights) != 0 {
		return errors.New("Linux owner anchor retained the helper or leaf after retirement request")
	}
	return nil
}

func (client *LinuxOwnerAnchorClient) RetirePreGateHelper(ctx context.Context, grant LinuxOwnerAnchorGrant, helper authority.LinuxCgroupProcessIdentity, proof authority.LinuxCgroupPreGateReleaseProof) error {
	if client == nil || ctx == nil || grant.Validate() != nil || helper.Validate() != nil || proof.Validate() != nil {
		return errors.New("Linux owner anchor pre-gate retirement is incomplete")
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationRetire,
		Grant: grant, Helper: &helper, PreGateReleaseProof: &proof,
	}
	response, rights, err := client.exchange(ctx, request)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if !response.matches(grant, linuxOwnerAnchorOperationRetire) || (response.Status != "retired" && response.Status != "already_retired") || len(rights) != 0 {
		return errors.New("Linux owner anchor retained pre-gate helper pins after retirement")
	}
	return nil
}

func (client *LinuxOwnerAnchorClient) RetireClearedRun(ctx context.Context, key LinuxOwnerAnchorRunKey, fence protocol.Fence, helper authority.LinuxCgroupProcessIdentity, release LinuxCgroupV2Release) error {
	normalProof, normalIssued := release.Snapshot()
	preGateProof, preGateIssued := release.SnapshotPreGate()
	if normalIssued == preGateIssued {
		return errors.New("Linux cgroup release capability is absent or ambiguous")
	}
	if normalIssued {
		return client.retireClearedRunWithProofs(ctx, key, fence, helper, &normalProof, nil)
	}
	return client.retireClearedRunWithProofs(ctx, key, fence, helper, nil, &preGateProof)
}

// RetireClearedRunWithProof is the restart-safe path for the exact durable
// post-clear normal release tombstone. It does not manufacture an opaque
// release capability; the anchor independently verifies its retained pins.
func (client *LinuxOwnerAnchorClient) RetireClearedRunWithProof(
	ctx context.Context,
	key LinuxOwnerAnchorRunKey,
	fence protocol.Fence,
	helper authority.LinuxCgroupProcessIdentity,
	proof authority.SupervisorHandoffReleaseProof,
) error {
	if proof.Validate() != nil || proof.CgroupIdentity == nil {
		return errors.New("Linux cleared-run normal release proof is incomplete")
	}
	proofCopy := proof.Clone()
	return client.retireClearedRunWithProofs(ctx, key, fence, helper, &proofCopy, nil)
}

// RetirePreGateClearedRunWithProof is the restart-safe path for the exact
// durable post-clear PID-zero release tombstone.
func (client *LinuxOwnerAnchorClient) RetirePreGateClearedRunWithProof(
	ctx context.Context,
	key LinuxOwnerAnchorRunKey,
	fence protocol.Fence,
	helper authority.LinuxCgroupProcessIdentity,
	proof authority.LinuxCgroupPreGateReleaseProof,
) error {
	if proof.Validate() != nil {
		return errors.New("Linux cleared-run pre-gate release proof is incomplete")
	}
	proofCopy := proof.Clone()
	return client.retireClearedRunWithProofs(ctx, key, fence, helper, nil, &proofCopy)
}

func (client *LinuxOwnerAnchorClient) retireClearedRunWithProofs(
	ctx context.Context,
	key LinuxOwnerAnchorRunKey,
	fence protocol.Fence,
	helper authority.LinuxCgroupProcessIdentity,
	normalProof *authority.SupervisorHandoffReleaseProof,
	preGateProof *authority.LinuxCgroupPreGateReleaseProof,
) error {
	if client == nil || ctx == nil || !validLinuxOwnerAnchorRunID(key.RunID) || key.Generation <= 0 ||
		fence.Generation != key.Generation || helper.Validate() != nil || (normalProof == nil) == (preGateProof == nil) {
		return errors.New("Linux cleared-run retirement identity is incomplete")
	}
	var anchor authority.LinuxCgroupProcessIdentity
	var parent authority.LinuxCgroupParentIdentity
	var ownerUID uint32
	if normalProof != nil {
		if normalProof.Validate() != nil || normalProof.CgroupIdentity == nil {
			return errors.New("normal Linux release capability lacks its exact cgroup receipt")
		}
		anchor = authority.LinuxCgroupProcessIdentity{PID: normalProof.AnchorPID, Identity: normalProof.AnchorIdentity}
		parent = normalProof.CgroupIdentity.Parent
		ownerUID = normalProof.OwnerUID
	} else {
		if preGateProof.Validate() != nil {
			return errors.New("pre-gate Linux release capability lacks its exact reservation receipt")
		}
		anchor = preGateProof.Receipt.Anchor
		parent = preGateProof.Receipt.CgroupIdentity.Parent
		ownerUID = preGateProof.Receipt.OwnerUID
	}
	if anchor.Validate() != nil || parent.Validate() != nil || ownerUID != client.owner.AUID {
		return errors.New("Linux release capability differs from the fixed anchor owner")
	}
	connection, stopClose, err := client.dial(ctx, linuxOwnerAnchorWaitTimeout)
	if err != nil {
		return err
	}
	defer connection.Close()
	defer stopClose()
	if err := verifyLinuxOwnerAnchorPeer(connection, client.owner, anchor); err != nil {
		return fmt.Errorf("authenticate anchor for cleared-run retirement: %w", err)
	}
	clientIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationRetireCleared,
		ClientPID: os.Getpid(), ClientIdentity: clientIdentity,
		RunKey: key, Fence: fence, Helper: &helper,
	}
	if normalProof != nil {
		request.ReleaseProof = normalProof
	} else {
		request.PreGateReleaseProof = preGateProof
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return errors.Join(errors.New("encode bounded cleared-run retirement request"), err)
	}
	if written, _, err := connection.WriteMsgUnix(encoded, nil, nil); err != nil || written != len(encoded) {
		return fmt.Errorf("send cleared-run retirement request: %w", errOr(err, io.ErrShortWrite))
	}
	response, rights, err := readLinuxOwnerAnchorResponse(connection)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Version != linuxOwnerAnchorProtocolVersion || response.Operation != linuxOwnerAnchorOperationRetireCleared ||
		(response.Status != "retired" && response.Status != "already_retired") || response.RunKey != key || response.Fence != fence ||
		!response.Anchor.Equal(anchor) || !response.Parent.Equal(parent) || response.OwnerUID != ownerUID || response.ManagerPath != parent.Path ||
		response.Helper == nil || !response.Helper.Equal(helper) {
		return errors.New("anchor retirement response differs from the exact cleared RunKey/fence and release capability")
	}
	return nil
}

func (client *LinuxOwnerAnchorClient) RetireAbortedHelper(ctx context.Context, key LinuxOwnerAnchorRunKey, fence protocol.Fence, helper authority.LinuxCgroupProcessIdentity, proof authority.LinuxCgroupReservationAbortProof) error {
	if client == nil || ctx == nil || !validLinuxOwnerAnchorRunID(key.RunID) || key.Generation <= 0 ||
		fence.Generation != key.Generation || helper.Validate() != nil || proof.Validate() != nil {
		return errors.New("Linux aborted-helper retirement is incomplete")
	}
	connection, stopClose, err := client.dial(ctx, linuxOwnerAnchorWaitTimeout)
	if err != nil {
		return err
	}
	defer connection.Close()
	defer stopClose()
	anchor := proof.Anchor
	if err := verifyLinuxOwnerAnchorPeer(connection, client.owner, anchor); err != nil {
		return fmt.Errorf("authenticate anchor for aborted-helper retirement: %w", err)
	}
	clientIdentity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		return err
	}
	request := linuxOwnerAnchorRequest{
		Version: linuxOwnerAnchorProtocolVersion, Operation: linuxOwnerAnchorOperationRetireAborted,
		ClientPID: os.Getpid(), ClientIdentity: clientIdentity,
		RunKey: key, Fence: fence, Helper: &helper, AbortProof: &proof,
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > linuxOwnerAnchorMaxFrame {
		return errors.Join(errors.New("encode bounded aborted-helper retirement request"), err)
	}
	if written, _, err := connection.WriteMsgUnix(encoded, nil, nil); err != nil || written != len(encoded) {
		return fmt.Errorf("send aborted-helper retirement request: %w", errOr(err, io.ErrShortWrite))
	}
	response, rights, err := readLinuxOwnerAnchorResponse(connection)
	closeLinuxOwnerAnchorFDs(rights)
	if err != nil {
		return err
	}
	if response.Version != linuxOwnerAnchorProtocolVersion || response.Operation != linuxOwnerAnchorOperationRetireAborted ||
		(response.Status != "retired" && response.Status != "already_retired") || response.RunKey != key || response.Fence != fence ||
		!response.Anchor.Equal(anchor) || response.Helper == nil || !response.Helper.Equal(helper) {
		return errors.New("anchor did not retire the exact aborted helper proof")
	}
	return nil
}
