package harness

import (
	"fmt"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestParseKindAcceptsExactlyTheWireVocabulary(t *testing.T) {
	accepted := map[Kind]string{}
	for _, wire := range protocol.HarnessKinds() {
		kind, err := ParseKind(string(wire))
		if err != nil {
			t.Fatalf("ParseKind(%q) error = %v", wire, err)
		}
		if previous, ok := accepted[kind]; ok {
			t.Fatalf("ParseKind(%q) and ParseKind(%q) both map to kind %q", previous, wire, kind)
		}
		accepted[kind] = string(wire)
	}
	if len(accepted) != len(protocol.HarnessKinds()) {
		t.Fatalf("ParseKind() produced %d distinct kinds, want %d", len(accepted), len(protocol.HarnessKinds()))
	}
	for _, value := range []string{"", "claude", "Generic", "generic ", "future_harness", "opencode-v2"} {
		if _, err := ParseKind(value); err == nil || err.Error() != fmt.Sprintf("runtime.harness_kind %q is unsupported", value) {
			t.Fatalf("ParseKind(%q) = %v, want the exact unsupported-kind refusal", value, err)
		}
	}
}

func TestParseKindMapsEachWireValueToItsInternalKind(t *testing.T) {
	for wire, want := range map[string]Kind{
		"generic":     KindGeneric,
		"codex":       KindCodex,
		"claude_code": KindClaude,
		"pi":          KindPi,
		"omp":         KindOMP,
		"omo":         KindOmO,
		"opencode":    KindOpenCode,
	} {
		kind, err := ParseKind(wire)
		if err != nil || kind != want {
			t.Fatalf("ParseKind(%q) = (%q, %v), want %q", wire, kind, err, want)
		}
	}
}

func TestKindPredicatesAnswerTheConfiguredPolicy(t *testing.T) {
	for kind, want := range map[Kind]struct {
		nativeGoal       bool
		unavailableProbe bool
	}{
		KindGeneric:    {nativeGoal: false, unavailableProbe: false},
		KindCodex:      {nativeGoal: true, unavailableProbe: false},
		KindClaude:     {nativeGoal: true, unavailableProbe: true},
		KindPi:         {nativeGoal: true, unavailableProbe: true},
		KindOMP:        {nativeGoal: true, unavailableProbe: true},
		KindOmO:        {nativeGoal: true, unavailableProbe: true},
		KindOpenCode:   {nativeGoal: true, unavailableProbe: true},
		Kind(""):       {nativeGoal: false, unavailableProbe: false},
		Kind("future"): {nativeGoal: false, unavailableProbe: false},
	} {
		if got := kind.NativeGoal(); got != want.nativeGoal {
			t.Fatalf("Kind(%q).NativeGoal() = %t, want %t", kind, got, want.nativeGoal)
		}
		if got := kind.UnavailableProbe(); got != want.unavailableProbe {
			t.Fatalf("Kind(%q).UnavailableProbe() = %t, want %t", kind, got, want.unavailableProbe)
		}
	}
}
