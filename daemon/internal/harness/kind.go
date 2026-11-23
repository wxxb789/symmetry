package harness

import (
	"fmt"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// ParseKind maps the configured wire spelling to the internal adapter kind; it is the
// single wire-to-internal table consumed by config, app and linuxpreflight.
func ParseKind(value string) (Kind, error) {
	wire, ok := protocol.ParseHarnessKind(value)
	if !ok {
		return "", fmt.Errorf("runtime.harness_kind %q is unsupported", value)
	}
	switch wire {
	case protocol.HarnessKindGeneric:
		return KindGeneric, nil
	case protocol.HarnessKindCodex:
		return KindCodex, nil
	case protocol.HarnessKindClaudeCode:
		return KindClaude, nil
	case protocol.HarnessKindPi:
		return KindPi, nil
	case protocol.HarnessKindOMP:
		return KindOMP, nil
	case protocol.HarnessKindOmO:
		return KindOmO, nil
	case protocol.HarnessKindOpenCode:
		return KindOpenCode, nil
	default:
		return "", fmt.Errorf("runtime.harness_kind %q is unsupported", value)
	}
}

// NativeGoal reports whether the kind's launch contract is staged; the generic adapter
// deliberately remains outside, and an unknown kind stays fail-closed.
func (kind Kind) NativeGoal() bool {
	switch kind {
	case KindCodex, KindClaude, KindPi, KindOMP, KindOmO, KindOpenCode:
		return true
	default:
		return false
	}
}

// UnavailableProbe reports whether an absent harness executable is the expected
// probe outcome for the kind rather than a fatal daemon error.
func (kind Kind) UnavailableProbe() bool {
	switch kind {
	case KindClaude, KindPi, KindOMP, KindOmO, KindOpenCode:
		return true
	default:
		return false
	}
}
