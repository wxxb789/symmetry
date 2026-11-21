//go:build linux && amd64

package platform

import (
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

// TestCloneAliasingLinuxAMD64Evidence covers the seven clone functions compiled
// under //go:build linux && amd64: the anchor's exit-proof, producer-fence and
// stop-evidence clones, the helper-v2 identity and stop-evidence clones, and the
// Validate-gated gate-identity clone. The harness and the mutation semantics
// live in clone_aliasing_linux_test.go, which is compiled whenever this file is.
func TestCloneAliasingLinuxAMD64Evidence(t *testing.T) {
	for _, testCase := range []cloneAliasingCase{
		cloneAliasingCaseFor("cloneLinuxOwnerAnchorExitProof", func(value *authority.LinuxCgroupExitProof) any {
			return cloneLinuxOwnerAnchorExitProof(value)
		}),
		cloneAliasingCaseFor("LinuxOwnerAnchorProducerFence.Clone", func(value *LinuxOwnerAnchorProducerFence) any {
			return value.Clone()
		}),
		cloneAliasingCaseFor("LinuxOwnerAnchorStopEvidence.Clone", func(value *LinuxOwnerAnchorStopEvidence) any {
			return value.Clone()
		}),
		cloneAliasingCaseFor("cloneLinuxOwnerProcessIdentity", func(value *authority.LinuxCgroupProcessIdentity) any {
			return cloneLinuxOwnerProcessIdentity(value)
		}),
		cloneAliasingCaseFor("cloneLinuxOwnerGateIdentity", func(value *authority.LinuxCgroupGateIdentity) any {
			return cloneLinuxOwnerGateIdentity(value)
		}),
		cloneAliasingCaseFor("cloneLinuxOwnerGateIdentityIfValid", func(value *authority.LinuxCgroupGateIdentity) any {
			return cloneLinuxOwnerGateIdentityIfValid(*value)
		}),
		cloneAliasingCaseFor("cloneLinuxOwnerHelperV2StopEvidence", func(value *LinuxOwnerHelperV2StopEvidence) any {
			return cloneLinuxOwnerHelperV2StopEvidence(*value)
		}),
	} {
		t.Run(testCase.name, func(t *testing.T) { runCloneAliasingCase(t, testCase) })
	}
}
