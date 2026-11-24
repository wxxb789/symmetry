package harness

import "testing"

func TestControlCapabilityMapsEachControlKind(t *testing.T) {
	for kind, want := range map[ControlKind]Capability{
		ControlCancel:                 CapabilityCancel,
		ControlPause:                  CapabilityPause,
		ControlResume:                 CapabilityResume,
		ControlGuidance:               CapabilityGuidance,
		ControlApprovalResponse:       CapabilityApprovalResponse,
		ControlKind("usage"):          CapabilityUsage,
		ControlKind("future_control"): Capability("future_control"),
	} {
		if got := ControlCapability(kind); got != want {
			t.Fatalf("ControlCapability(%q) = %q, want %q", kind, got, want)
		}
	}
}
