//go:build linux && amd64

package platform

import (
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func TestCloneAliasingLinuxAMD64Evidence(t *testing.T) {
	for _, testCase := range []cloneAliasingCase{
		cloneAliasingCaseFor("cloneLinuxOwnerAnchorExitProof", func(value *authority.LinuxCgroupExitProof) any {
			return cloneLinuxOwnerAnchorExitProof(value)
		},
			// cloneLinuxOwnerAnchorExitProof truncates the exit proof's own HelperExit by design (anchor_server.go:44-49).
			"HelperExit"),
		cloneAliasingCaseFor("LinuxOwnerAnchorProducerFence.Clone", func(value *LinuxOwnerAnchorProducerFence) any {
			return value.Clone()
		},
			// The fence's nested LauncherExit.HelperExit is truncated by cloneLinuxOwnerAnchorExitProof (anchor_types.go:118).
			"LauncherExit.HelperExit"),
		cloneAliasingCaseFor("LinuxOwnerAnchorStopEvidence.Clone", func(value *LinuxOwnerAnchorStopEvidence) any {
			return value.Clone()
		},
			// The evidence's HelperExit and its producer fence's LauncherExit
			// both pass through cloneLinuxOwnerAnchorExitProof, which truncates the nested HelperExit (anchor_types.go:118, :252).
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
			// The listed paths reach cloneLinuxOwnerAnchorExitProof (nested HelperExit truncated) or the
			// PreGate.Proof struct copy (helper_v2_stop.go:1564); the RunReapEvidence exits stay unlisted, kept independent by the loop at :1547-1550.
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
