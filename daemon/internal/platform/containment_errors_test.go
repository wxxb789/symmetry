//go:build linux && amd64

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"syscall"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

// linuxOwnerTypedMismatchPin pins one typed mismatch class: the sentinel the
// owned platform sites return, the representative site, and the full Error()
// text that site produces when the underlying read succeeded and only the
// mismatch predicate failed.
type linuxOwnerTypedMismatchPin struct {
	site          string
	want          string
	wantTransport string
	sentinel      error
	errorf        func() error
	transport     func(inner error) error
}

// linuxOwnerTypedMismatchPins holds one row per sentinel class, each row naming
// one representative site from the w2-11-typed-errors site inventory: each
// errorf reproduces its representative site's fmt.Errorf expression verbatim,
// with err == nil.
var linuxOwnerTypedMismatchPins = []linuxOwnerTypedMismatchPin{
	{
		site:          "anchor_gate.go:995 classifyUnregisteredPreReleaseGate",
		want:          "recheck possible pre-release gate leaf membership: cgroup path changed",
		sentinel:      ErrLinuxOwnerCgroupPathChanged,
		wantTransport: "recheck possible pre-release gate leaf membership: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("recheck possible pre-release gate leaf membership: %w", errOr(nil, ErrLinuxOwnerCgroupPathChanged))
		},
		transport: func(inner error) error {
			return fmt.Errorf("recheck possible pre-release gate leaf membership: %w", errOr(inner, ErrLinuxOwnerCgroupPathChanged))
		},
	},
	{
		site:          "anchor_gate.go:376 authorizeGateRelease",
		want:          "verify parked gate identity before release authorization: creation identity changed",
		sentinel:      ErrLinuxOwnerCreationIdentityChanged,
		wantTransport: "verify parked gate identity before release authorization: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("verify parked gate identity before release authorization: %w", errOr(nil, ErrLinuxOwnerCreationIdentityChanged))
		},
		transport: func(inner error) error {
			return fmt.Errorf("verify parked gate identity before release authorization: %w", errOr(inner, ErrLinuxOwnerCreationIdentityChanged))
		},
	},
	{
		site:          "anchor_gate.go:172 trackPreReleaseGatePhysicalOnlyChild",
		want:          "verify physical-only gate identity: creation identity differs",
		sentinel:      ErrLinuxOwnerCreationIdentityDiffers,
		wantTransport: "verify physical-only gate identity: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("verify physical-only gate identity: %w", errOr(nil, ErrLinuxOwnerCreationIdentityDiffers))
		},
		transport: func(inner error) error {
			return fmt.Errorf("verify physical-only gate identity: %w", errOr(inner, ErrLinuxOwnerCreationIdentityDiffers))
		},
	},
	{
		site:          "anchor_gate.go:321 registerGate",
		want:          "recheck retained gate user namespace after registration: device or inode changed",
		sentinel:      ErrLinuxOwnerDeviceOrInodeChanged,
		wantTransport: "recheck retained gate user namespace after registration: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("recheck retained gate user namespace after registration: %w", errOr(nil, ErrLinuxOwnerDeviceOrInodeChanged))
		},
		transport: func(inner error) error {
			return fmt.Errorf("recheck retained gate user namespace after registration: %w", errOr(inner, ErrLinuxOwnerDeviceOrInodeChanged))
		},
	},
	{
		site:          "anchor_gate.go:339 registerGate",
		want:          "replayed gate image differs from its exact retained pin: device or inode differs",
		sentinel:      ErrLinuxOwnerDeviceOrInodeDiffers,
		wantTransport: "replayed gate image differs from its exact retained pin: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("replayed gate image differs from its exact retained pin: %w", errOr(nil, ErrLinuxOwnerDeviceOrInodeDiffers))
		},
		transport: func(inner error) error {
			return fmt.Errorf("replayed gate image differs from its exact retained pin: %w", errOr(inner, ErrLinuxOwnerDeviceOrInodeDiffers))
		},
	},
	{
		site:          "anchor_gate.go:81 readLinuxOwnerAnchorWitnessConfig",
		want:          "verify root-controlled /etc directory: directory ownership or mode is unsafe",
		sentinel:      ErrLinuxOwnerDirectoryOwnershipUnsafe,
		wantTransport: "verify root-controlled /etc directory: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("verify root-controlled /etc directory: %w", errOr(nil, ErrLinuxOwnerDirectoryOwnershipUnsafe))
		},
		transport: func(inner error) error {
			return fmt.Errorf("verify root-controlled /etc directory: %w", errOr(inner, ErrLinuxOwnerDirectoryOwnershipUnsafe))
		},
	},
	{
		site:          "anchor_gate.go:506 verifyPinnedGateUserNamespace",
		want:          "verify retained static gate image FD: root-owned image identity differs",
		sentinel:      ErrLinuxOwnerGateImageIdentityDiffers,
		wantTransport: "verify retained static gate image FD: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("verify retained static gate image FD: %w", errOr(nil, ErrLinuxOwnerGateImageIdentityDiffers))
		},
		transport: func(inner error) error {
			return fmt.Errorf("verify retained static gate image FD: %w", errOr(inner, ErrLinuxOwnerGateImageIdentityDiffers))
		},
	},
	{
		site:          "anchor_gate.go:318 registerGate",
		want:          "recheck parked-gate creation identity after namespace verification: identity changed",
		sentinel:      ErrLinuxOwnerIdentityChanged,
		wantTransport: "recheck parked-gate creation identity after namespace verification: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("recheck parked-gate creation identity after namespace verification: %w", errOr(nil, ErrLinuxOwnerIdentityChanged))
		},
		transport: func(inner error) error {
			return fmt.Errorf("recheck parked-gate creation identity after namespace verification: %w", errOr(inner, ErrLinuxOwnerIdentityChanged))
		},
	},
	{
		site:          "anchor_proc.go:307 verifyLinuxOwnerAProcess",
		want:          "Linux owner A process creation identity mismatch: identity differs",
		sentinel:      ErrLinuxOwnerIdentityDiffers,
		wantTransport: "Linux owner A process creation identity mismatch: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("Linux owner A process creation identity mismatch: %w", errOr(nil, ErrLinuxOwnerIdentityDiffers))
		},
		transport: func(inner error) error {
			return fmt.Errorf("Linux owner A process creation identity mismatch: %w", errOr(inner, ErrLinuxOwnerIdentityDiffers))
		},
	},
	{
		site:          "anchor_gate.go:196 trackPreReleaseGatePhysicalOnlyChild",
		want:          "physical-only gate root launcher remains live: launcher pidfd is not ready",
		sentinel:      ErrLinuxOwnerLauncherPIDFDNotReady,
		wantTransport: "physical-only gate root launcher remains live: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("physical-only gate root launcher remains live: %w", errOr(nil, ErrLinuxOwnerLauncherPIDFDNotReady))
		},
		transport: func(inner error) error {
			return fmt.Errorf("physical-only gate root launcher remains live: %w", errOr(inner, ErrLinuxOwnerLauncherPIDFDNotReady))
		},
	},
	{
		site:          "anchor_gate.go:433 verifyRegisteredGateA",
		want:          "live parked gate remains after exact run-leaf removal: pidfd is not ready",
		sentinel:      ErrLinuxOwnerPIDFDNotReady,
		wantTransport: "live parked gate remains after exact run-leaf removal: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("live parked gate remains after exact run-leaf removal: %w", errOr(nil, ErrLinuxOwnerPIDFDNotReady))
		},
		transport: func(inner error) error {
			return fmt.Errorf("live parked gate remains after exact run-leaf removal: %w", errOr(inner, ErrLinuxOwnerPIDFDNotReady))
		},
	},
	{
		site:          "helper_v2_stop.go:1049 reapAdoptedChildren",
		want:          "verify exact adopted child parent: parent differs from helper",
		sentinel:      ErrLinuxOwnerParentDiffersFromHelper,
		wantTransport: "verify exact adopted child parent: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("verify exact adopted child parent: %w", errOr(nil, ErrLinuxOwnerParentDiffersFromHelper))
		},
		transport: func(inner error) error {
			return fmt.Errorf("verify exact adopted child parent: %w", errOr(inner, ErrLinuxOwnerParentDiffersFromHelper))
		},
	},
	{
		site:          "anchor_gate.go:299 registerGate",
		want:          "verify gate user namespace is a direct child of the pinned Anchor namespace: parent namespace differs",
		sentinel:      ErrLinuxOwnerParentNamespaceDiffers,
		wantTransport: "verify gate user namespace is a direct child of the pinned Anchor namespace: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("verify gate user namespace is a direct child of the pinned Anchor namespace: %w", errOr(nil, ErrLinuxOwnerParentNamespaceDiffers))
		},
		transport: func(inner error) error {
			return fmt.Errorf("verify gate user namespace is a direct child of the pinned Anchor namespace: %w", errOr(inner, ErrLinuxOwnerParentNamespaceDiffers))
		},
	},
	{
		site:          "anchor_gate.go:176 trackPreReleaseGatePhysicalOnlyChild",
		want:          "verify physical-only gate Anchor parent: parent PID differs",
		sentinel:      ErrLinuxOwnerParentPIDDiffers,
		wantTransport: "verify physical-only gate Anchor parent: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("verify physical-only gate Anchor parent: %w", errOr(nil, ErrLinuxOwnerParentPIDDiffers))
		},
		transport: func(inner error) error {
			return fmt.Errorf("verify physical-only gate Anchor parent: %w", errOr(inner, ErrLinuxOwnerParentPIDDiffers))
		},
	},
	{
		site:          "helper_v2_run.go:276 readLinuxOwnerProcessChildrenSnapshot",
		want:          "Linux process child census snapshot changed: process TID set changed during all-thread child census: thread snapshot differs",
		sentinel:      ErrLinuxOwnerThreadSnapshotDiffers,
		wantTransport: "Linux process child census snapshot changed: process TID set changed during all-thread child census: read exact process state: transport loss",
		errorf: func() error {
			return fmt.Errorf("%w: process TID set changed during all-thread child census: %w", errLinuxOwnerProcessChildrenChanged, errOr(nil, ErrLinuxOwnerThreadSnapshotDiffers))
		},
		transport: func(inner error) error {
			return fmt.Errorf("%w: process TID set changed during all-thread child census: %w", errLinuxOwnerProcessChildrenChanged, errOr(inner, ErrLinuxOwnerThreadSnapshotDiffers))
		},
	},
}

// TestLinuxOwnerTypedMismatchSentinelTable pins the wire-visible Error() text
// per sentinel class and proves errors.Is identifies the class only when the
// mismatch predicate - not a failed read - produced the error, so callers can
// distinguish an identity change from transport loss.
func TestLinuxOwnerTypedMismatchSentinelTable(t *testing.T) {
	for _, pin := range linuxOwnerTypedMismatchPins {
		t.Run(pin.site, func(t *testing.T) {
			err := pin.errorf()
			if err == nil || err.Error() != pin.want {
				t.Fatalf("representative site %s = %v; want exactly %q", pin.site, err, pin.want)
			}
			if !errors.Is(err, pin.sentinel) {
				t.Fatalf("site %s error %v does not identify %v", pin.site, err, pin.sentinel)
			}
			transport := errors.New("read exact process state: transport loss")
			loss := pin.transport(transport)
			if loss.Error() != pin.wantTransport || !errors.Is(loss, transport) || errors.Is(loss, pin.sentinel) {
				t.Fatalf("site %s transport loss = %v; want exactly %q carrying the read error, not %v", pin.site, loss, pin.wantTransport, pin.sentinel)
			}
		})
	}
}

// platformSentinelTextPins pins the exact Error() text of every exported Err*
// value the platform package declares: C7's 22 pre-existing values (7
// supervisor, 7 helper-v2, 3 cgroup-v2, and 5 singles) plus this slice's 15
// ErrLinuxOwner* mismatch values. The rows mirror the declarations at this
// revision; the class table above additionally pins the wire composition at
// representative sites.
var platformSentinelTextPins = []struct {
	name     string
	sentinel error
	text     string
}{
	{name: "ErrLinuxSupervisorNotCommitted", sentinel: ErrLinuxSupervisorNotCommitted, text: "Linux supervisor authority is not committed"},
	{name: "ErrLinuxSupervisorOwnerLost", sentinel: ErrLinuxSupervisorOwnerLost, text: "Linux supervisor owner was lost"},
	{name: "ErrLinuxSupervisorHelperExited", sentinel: ErrLinuxSupervisorHelperExited, text: "Linux supervisor helper exited"},
	{name: "ErrLinuxSupervisorStopUnproven", sentinel: ErrLinuxSupervisorStopUnproven, text: "Linux supervisor stop is unproven"},
	{name: "ErrLinuxSupervisorReleased", sentinel: ErrLinuxSupervisorReleased, text: "Linux supervisor containment was released"},
	{name: "ErrLinuxSupervisorLeaseExpired", sentinel: ErrLinuxSupervisorLeaseExpired, text: "Linux supervisor lease expired"},
	{name: "ErrLinuxSupervisorLeaseUnarmed", sentinel: ErrLinuxSupervisorLeaseUnarmed, text: "Linux supervisor lease is not armed"},
	{name: "ErrLinuxOwnerHelperV2OwnerLost", sentinel: ErrLinuxOwnerHelperV2OwnerLost, text: "Linux owner helper observed daemon-owner loss"},
	{name: "ErrLinuxOwnerHelperV2NotCommitted", sentinel: ErrLinuxOwnerHelperV2NotCommitted, text: "Linux owner helper has no durable handoff acknowledgement"},
	{name: "ErrLinuxOwnerHelperV2LeaseUnarmed", sentinel: ErrLinuxOwnerHelperV2LeaseUnarmed, text: "Linux owner helper has no acknowledged initial lease"},
	{name: "ErrLinuxOwnerHelperV2ReleaseUsed", sentinel: ErrLinuxOwnerHelperV2ReleaseUsed, text: "Linux owner helper RELEASE is single-use"},
	{name: "ErrLinuxOwnerHelperV2StopUnproven", sentinel: ErrLinuxOwnerHelperV2StopUnproven, text: "Linux owner helper stop is unproven"},
	{name: "ErrLinuxOwnerHelperV2RecoveryOnly", sentinel: ErrLinuxOwnerHelperV2RecoveryOnly, text: "Linux owner helper is latched to recovery-only mode"},
	{name: "ErrLinuxOwnerHelperV2ReleaseUnresolved", sentinel: ErrLinuxOwnerHelperV2ReleaseUnresolved, text: "Linux owner helper release outcome is unresolved"},
	{name: "ErrLinuxCgroupV2Unresolved", sentinel: ErrLinuxCgroupV2Unresolved, text: "Linux cgroup v2 ownership is unresolved"},
	{name: "ErrLinuxCgroupV2NotDelegated", sentinel: ErrLinuxCgroupV2NotDelegated, text: "Linux cgroup v2 parent is not delegated to this process"},
	{name: "ErrLinuxCgroupV2NotCgroup2", sentinel: ErrLinuxCgroupV2NotCgroup2, text: "path is not on the verified cgroup v2 mount"},
	{name: "ErrLinuxOwnerAnchorHelperAlreadyStarted", sentinel: ErrLinuxOwnerAnchorHelperAlreadyStarted, text: "Linux cgroup-v2 owner helper already exists; attach to its exact identity instead of starting another"},
	{name: "ErrPersistedProcessGroupUnproven", sentinel: ErrPersistedProcessGroupUnproven, text: "persisted process group absence is unproven"},
	{name: "ErrLinuxDescendantContainmentUnproven", sentinel: ErrLinuxDescendantContainmentUnproven, text: "Linux descendant containment is unproven"},
	{name: "ErrLinuxPeerPIDFDMismatch", sentinel: ErrLinuxPeerPIDFDMismatch, text: "launch-retained pidfd does not name the launched process"},
	{name: "ErrLinuxSupervisorResponseLost", sentinel: ErrLinuxSupervisorResponseLost, text: "Linux supervisor response was lost after request commit"},
	{name: "ErrLinuxOwnerCgroupPathChanged", sentinel: ErrLinuxOwnerCgroupPathChanged, text: "cgroup path changed"},
	{name: "ErrLinuxOwnerCreationIdentityChanged", sentinel: ErrLinuxOwnerCreationIdentityChanged, text: "creation identity changed"},
	{name: "ErrLinuxOwnerCreationIdentityDiffers", sentinel: ErrLinuxOwnerCreationIdentityDiffers, text: "creation identity differs"},
	{name: "ErrLinuxOwnerDeviceOrInodeChanged", sentinel: ErrLinuxOwnerDeviceOrInodeChanged, text: "device or inode changed"},
	{name: "ErrLinuxOwnerDeviceOrInodeDiffers", sentinel: ErrLinuxOwnerDeviceOrInodeDiffers, text: "device or inode differs"},
	{name: "ErrLinuxOwnerDirectoryOwnershipUnsafe", sentinel: ErrLinuxOwnerDirectoryOwnershipUnsafe, text: "directory ownership or mode is unsafe"},
	{name: "ErrLinuxOwnerGateImageIdentityDiffers", sentinel: ErrLinuxOwnerGateImageIdentityDiffers, text: "root-owned image identity differs"},
	{name: "ErrLinuxOwnerIdentityChanged", sentinel: ErrLinuxOwnerIdentityChanged, text: "identity changed"},
	{name: "ErrLinuxOwnerIdentityDiffers", sentinel: ErrLinuxOwnerIdentityDiffers, text: "identity differs"},
	{name: "ErrLinuxOwnerLauncherPIDFDNotReady", sentinel: ErrLinuxOwnerLauncherPIDFDNotReady, text: "launcher pidfd is not ready"},
	{name: "ErrLinuxOwnerParentDiffersFromHelper", sentinel: ErrLinuxOwnerParentDiffersFromHelper, text: "parent differs from helper"},
	{name: "ErrLinuxOwnerParentNamespaceDiffers", sentinel: ErrLinuxOwnerParentNamespaceDiffers, text: "parent namespace differs"},
	{name: "ErrLinuxOwnerParentPIDDiffers", sentinel: ErrLinuxOwnerParentPIDDiffers, text: "parent PID differs"},
	{name: "ErrLinuxOwnerPIDFDNotReady", sentinel: ErrLinuxOwnerPIDFDNotReady, text: "pidfd is not ready"},
	{name: "ErrLinuxOwnerThreadSnapshotDiffers", sentinel: ErrLinuxOwnerThreadSnapshotDiffers, text: "thread snapshot differs"},
}

// TestPlatformSentinelErrorTexts pins every exported platform sentinel's exact
// Error() text: C7's 22 pre-existing Err* values and this slice's 15 new ones.
// errors.Is identity alone cannot detect a reworded message, so a durable or
// served string regression must fail here.
func TestPlatformSentinelErrorTexts(t *testing.T) {
	for _, pin := range platformSentinelTextPins {
		t.Run(pin.name, func(t *testing.T) {
			if pin.sentinel == nil {
				t.Fatalf("%s has no sentinel value", pin.name)
			}
			if got := pin.sentinel.Error(); got != pin.text {
				t.Fatalf("%s.Error() = %q; want exactly %q", pin.name, got, pin.text)
			}
		})
	}
}

// TestLinuxOwnerTypedMismatchReachableSites drives five converted sites
// through production code - anchor_proc.go:307 (verifyLinuxOwnerAProcess, the
// representative site of the "identity differs" pin row), anchor_gate.go:695
// (openLinuxOwnerAnchorUserNamespaceFD), anchor_run.go:202
// (verifyLinuxOwnerAnchorStopEvidenceNamespacePin), anchor_proc.go:528
// (verifyLinuxOwnerAnchorPIDFDIdentity against the process's own pidfd with a
// stale expected identity), and anchor_gate.go:995
// (classifyUnregisteredPreReleaseGate, the representative site of the "cgroup
// path changed" pin row, driven with a live nested-user-namespace gate child
// and an exited registered launcher); the middle three drive their classes at
// sites the pin rows do not name. A site that stops returning its sentinel
// fails here even though the expression pins above reproduce their sites'
// expressions instead of observing them.
func TestLinuxOwnerTypedMismatchReachableSites(t *testing.T) {
	t.Run("verifyLinuxOwnerAProcess", func(t *testing.T) {
		if os.Getuid() == 0 || os.Getgid() == 0 {
			t.Skip("requires a non-zero uid and primary gid; verifyLinuxOwnerAProcess treats uid 0 or gid 0 as incomplete credentials")
		}
		err := verifyLinuxOwnerAProcess(os.Getpid(), "linux:v2:unrelated-identity", uint32(os.Getuid()), uint32(os.Getgid()), 0, "")
		want := "Linux owner A process creation identity mismatch: identity differs"
		if err == nil || err.Error() != want || !errors.Is(err, ErrLinuxOwnerIdentityDiffers) {
			t.Fatalf("verifyLinuxOwnerAProcess = %v; want exactly %q identifying %v", err, want, ErrLinuxOwnerIdentityDiffers)
		}
	})
	t.Run("openLinuxOwnerAnchorUserNamespaceFD", func(t *testing.T) {
		identity, err := ProcessIdentity(os.Getpid())
		if err != nil {
			t.Fatalf("ProcessIdentity(self): %v", err)
		}
		process := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: identity + ":stale"}
		if _, _, err = openLinuxOwnerAnchorUserNamespaceFD(process, -1); err == nil ||
			err.Error() != "verify process identity before user-namespace pin: creation identity differs" ||
			!errors.Is(err, ErrLinuxOwnerCreationIdentityDiffers) {
			t.Fatalf("openLinuxOwnerAnchorUserNamespaceFD = %v; want the stale-identity sentinel", err)
		}
	})
	t.Run("verifyLinuxOwnerAnchorStopEvidenceNamespacePin", func(t *testing.T) {
		namespaceFD, err := os.Open("/proc/self/ns/user")
		if err != nil {
			t.Fatalf("open self user namespace: %v", err)
		}
		defer namespaceFD.Close()
		evidence := LinuxOwnerAnchorStopEvidence{
			Gate:          &authority.LinuxCgroupGateIdentity{},
			UserNamespace: &LinuxUserNamespaceIdentity{Device: 1, Inode: 1},
		}
		err = verifyLinuxOwnerAnchorStopEvidenceNamespacePin(evidence, namespaceFD)
		want := "Anchor stop evidence user namespace differs from its retained exact nsfs FD: device or inode differs"
		if err == nil || err.Error() != want || !errors.Is(err, ErrLinuxOwnerDeviceOrInodeDiffers) {
			t.Fatalf("verifyLinuxOwnerAnchorStopEvidenceNamespacePin = %v; want exactly %q identifying %v", err, want, ErrLinuxOwnerDeviceOrInodeDiffers)
		}
	})
	t.Run("verifyLinuxOwnerAnchorPIDFDIdentity", func(t *testing.T) {
		process := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: mustLinuxOwnerAnchorTestIdentity(t, os.Getpid())}
		pidfd := openLinuxOwnerAnchorTestPIDFD(t, process)
		defer unix.Close(pidfd)
		stale := authority.LinuxCgroupProcessIdentity{PID: process.PID, Identity: process.Identity + ":stale"}
		err := verifyLinuxOwnerAnchorPIDFDIdentity(pidfd, stale)
		want := "Linux owner anchor pidfd creation identity mismatch: identity differs"
		if err == nil || err.Error() != want || !errors.Is(err, ErrLinuxOwnerIdentityDiffers) {
			t.Fatalf("verifyLinuxOwnerAnchorPIDFDIdentity = %v; want exactly %q identifying %v", err, want, ErrLinuxOwnerIdentityDiffers)
		}
	})
	// classifyUnregisteredPreReleaseGate reaches its cgroup-path recheck
	// (anchor_gate.go:993-995) only after the gate child's pidfd creation
	// identity, Anchor ancestry, configured credentials, the run's sole
	// registered exited launcher, and the gate user namespace's exact parent
	// all verify. The fixture supplies each through real processes: the gate
	// child lives in its own nested user namespace whose parent is this
	// process's namespace, and the launcher pidfd is a killed and reaped
	// child, so the production readiness check observes a genuinely exited
	// registered root launcher.
	t.Run("classifyUnregisteredPreReleaseGate", func(t *testing.T) {
		if os.Getuid() == 0 || os.Getgid() == 0 {
			t.Skip("requires a non-zero uid and primary gid; the gate credential check rejects UID/GID 0")
		}
		ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
		defer cancel()
		anchorIdentity := authority.LinuxCgroupProcessIdentity{PID: os.Getpid(), Identity: mustLinuxOwnerAnchorTestIdentity(t, os.Getpid())}
		managerPath, err := readLinuxOwnerProcessCgroupPath(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		grant, err := testLinuxOwnerAnchorGrant(anchorIdentity, managerPath)
		if err != nil {
			t.Fatal(err)
		}
		helper := authority.LinuxCgroupProcessIdentity{PID: os.Getpid() + 900, Identity: "linux:v2:pre-release-leaf-helper"}
		helperExit := &authority.LinuxCgroupExitProof{
			Version: authority.LinuxCgroupIdentityVersion, Process: helper, Witness: anchorIdentity, Waited: true, Reaped: true,
		}
		gate := exec.Command(os.Args[0], "-test.run=^TestLinuxOwnerAnchorNamespaceTestProcess$")
		gate.Env = append(os.Environ(), linuxOwnerAnchorNamespaceTestMode+"=block")
		gate.SysProcAttr = linuxOwnerAnchorTestUserNamespaceSysProcAttr(os.Getuid(), os.Getgid())
		gate.Stdout = io.Discard
		gate.Stderr = io.Discard
		gateInput, err := gate.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := gate.Start(); err != nil {
			_ = gateInput.Close()
			if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.ENOSYS) {
				t.Skipf("kernel disallows unprivileged user namespaces: %v", err)
			}
			t.Fatalf("start nested-user-namespace gate process: %v", err)
		}
		t.Cleanup(func() {
			_ = gateInput.Close()
			_ = gate.Wait()
		})
		process := authority.LinuxCgroupProcessIdentity{PID: gate.Process.Pid, Identity: mustLinuxOwnerAnchorTestIdentity(t, gate.Process.Pid)}
		pidfd := openLinuxOwnerAnchorTestPIDFD(t, process)
		defer unix.Close(pidfd)
		anchorNamespace, err := readLinuxUserNamespaceIdentity(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		launcher := startLinuxOwnerAnchorTestProcess(t, ctx, "target", false)
		launcherIdentity := authority.LinuxCgroupProcessIdentity{
			PID: launcher.command.Process.Pid, Identity: mustLinuxOwnerAnchorTestIdentity(t, launcher.command.Process.Pid),
		}
		launcherPIDFD := openLinuxOwnerAnchorTestPIDFD(t, launcherIdentity)
		if err := unix.PidfdSendSignal(launcherPIDFD, unix.SIGKILL, nil, 0); err != nil {
			t.Fatalf("kill registered launcher: %v", err)
		}
		if err := launcher.command.Wait(); err == nil {
			t.Fatal("killed launcher subprocess unexpectedly exited successfully")
		}
		launcher.waited = true
		launcherFile := os.NewFile(uintptr(launcherPIDFD), "linux-owner-anchor-typed-error-launcher-pidfd")
		if launcherFile == nil {
			_ = unix.Close(launcherPIDFD)
			t.Fatal("wrap registered launcher pidfd")
		}
		defer launcherFile.Close()
		if ready, err := linuxOwnerAnchorPIDFDReady(launcherFile); err != nil || !ready {
			t.Fatalf("launcher pidfd readiness = %t, %v; want the registered launcher exited", ready, err)
		}
		registered := linuxOwnerAnchorRunChildClassification{
			Version: linuxOwnerAnchorRunChildClassificationVersion,
			RunKey:  grant.RunKey, OriginRunKey: grant.RunKey, Fence: grant.Fence, LaunchToken: grant.LaunchToken,
			Helper: helper, Process: launcherIdentity, Role: "launcher",
			Origin: linuxOwnerAnchorRunChildOriginRegisteredCurrent,
		}
		run := &linuxOwnerAnchorRun{
			grant: grant, helper: helper, helperExit: helperExit,
			owner: linuxgate.OwnerConfig{
				AUID: uint32(os.Getuid()), AGID: uint32(os.Getgid()),
				BUID: uint32(os.Getuid()) + 1, BGID: uint32(os.Getgid()) + 1,
			},
			children: map[int]linuxOwnerAnchorTrackedProcess{
				launcherIdentity.PID: {
					identity: launcherIdentity, creator: helper, classification: registered,
					role: "launcher", pidfd: launcherFile,
				},
			},
		}
		anchor := &linuxOwnerAnchor{identity: anchorIdentity, userNamespace: anchorNamespace}
		leafPath := path.Join(grant.Parent.Path, grant.Leaf.LeafName)
		classification, eligible, err := anchor.classifyUnregisteredPreReleaseGate(ctx, run, process, pidfd, leafPath)
		want := "recheck possible pre-release gate leaf membership: cgroup path changed"
		if err == nil || err.Error() != want || !errors.Is(err, ErrLinuxOwnerCgroupPathChanged) || eligible {
			t.Fatalf("classifyUnregisteredPreReleaseGate = (%+v, %t, %v); want exactly %q identifying %v", classification, eligible, err, want, ErrLinuxOwnerCgroupPathChanged)
		}
	})
}
