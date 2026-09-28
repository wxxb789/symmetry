//go:build !linux

package platform

import "github.com/wxxb789/symmetry/daemon/internal/authority"

// LinuxCgroupV2Parent keeps platform-neutral invocation shapes compilable on
// non-Linux systems. Linux v2 launch methods are intentionally unavailable.
type LinuxCgroupV2Parent struct{}

// LinuxCgroupV2Release is always unissued where the Linux cgroup implementation
// is unavailable; it can never authorize journal clearing.
type LinuxCgroupV2Release struct{}

// Snapshot never returns release authority on unsupported operating systems.
func (LinuxCgroupV2Release) Snapshot() (authority.SupervisorHandoffReleaseProof, bool) {
	return authority.SupervisorHandoffReleaseProof{}, false
}

// SnapshotPreGate never returns release authority on unsupported operating
// systems.
func (LinuxCgroupV2Release) SnapshotPreGate() (authority.LinuxCgroupPreGateReleaseProof, bool) {
	return authority.LinuxCgroupPreGateReleaseProof{}, false
}
