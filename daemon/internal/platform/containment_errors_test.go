//go:build linux && amd64

package platform

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
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

// TestLinuxOwnerTypedMismatchReachableSites drives three of those sites through
// production code, so a site that stops returning its sentinel fails here even
// though the expression pins above cannot observe the site itself.
func TestLinuxOwnerTypedMismatchReachableSites(t *testing.T) {
	t.Run("verifyLinuxOwnerAProcess", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("requires an unprivileged uid for the Linux owner A process check")
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
}
