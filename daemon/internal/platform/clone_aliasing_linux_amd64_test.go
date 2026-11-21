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
		},
			// The clone truncates the exit proof's own HelperExit by design
			// (anchor_server.go:44-49); the case populates every other field.
			"HelperExit"),
		cloneAliasingCaseFor("LinuxOwnerAnchorProducerFence.Clone", func(value *LinuxOwnerAnchorProducerFence) any {
			return value.Clone()
		},
			// cloneLinuxOwnerAnchorExitProof truncates the fence's nested
			// LauncherExit.HelperExit (anchor_types.go:118).
			"LauncherExit.HelperExit"),
		cloneAliasingCaseFor("LinuxOwnerAnchorStopEvidence.Clone", func(value *LinuxOwnerAnchorStopEvidence) any {
			return value.Clone()
		},
			// The evidence's HelperExit (anchor_types.go:252) and its producer
			// fence's LauncherExit (anchor_types.go:118 through :249) both go
			// through cloneLinuxOwnerAnchorExitProof.
			"HelperExit.HelperExit",
			"ProducerFence.LauncherExit.HelperExit"),
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
		},
			// Every exit proof this clone copies through
			// cloneLinuxOwnerAnchorExitProof (helper_v2_stop.go:1570-1572 for
			// the top proof, :1558 for the pre-gate launcher, :1565 for the
			// pre-gate proof; the AnchorStop clones at :1543 and :1554 reach
			// anchor_types.go:118 and :252) keeps only the outer exit: the
			// nested HelperExit is truncated. PreGate.Proof.LauncherExit's
			// HelperExit is not re-cloned at all - the PreGate.Proof struct
			// copy at :1564 leaves it shared - and a helper-kind pre-gate
			// proof requires it nil anyway (authority.go:1220, helper branch
			// at :1232-1234, through ValidForOwnedDescendant :1180). The
			// RunReapEvidence exits are deliberately not listed: the
			// deep-copy loop at :1547-1550 must make them independent, and
			// removing that loop turns this case red.
			"AnchorStop.HelperExit.HelperExit",
			"AnchorStop.ProducerFence.LauncherExit.HelperExit",
			"PreGate.AnchorStop.HelperExit.HelperExit",
			"PreGate.AnchorStop.ProducerFence.LauncherExit.HelperExit",
			"PreGate.LauncherExit.HelperExit",
			"PreGate.Proof.HelperExit.HelperExit",
			"PreGate.Proof.LauncherExit.HelperExit",
			"Proof.GateExit.HelperExit",
			"Proof.HelperExit.HelperExit",
			"Proof.LauncherExit.HelperExit"),
	} {
		t.Run(testCase.name, func(t *testing.T) { runCloneAliasingCase(t, testCase) })
	}
}
