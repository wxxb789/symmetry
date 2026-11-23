package harness

import (
	"fmt"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// internalKindByWire is the single wire→internal table; a wire value with no row is refused,
// and claude_code deliberately maps to the internal "claude" spelling persisted to Control.
var internalKindByWire = map[protocol.HarnessKind]Kind{
	protocol.HarnessKindGeneric:    KindGeneric,
	protocol.HarnessKindCodex:      KindCodex,
	protocol.HarnessKindClaudeCode: KindClaude,
	protocol.HarnessKindPi:         KindPi,
	protocol.HarnessKindOMP:        KindOMP,
	protocol.HarnessKindOmO:        KindOmO,
	protocol.HarnessKindOpenCode:   KindOpenCode,
}

func ParseKind(value string) (Kind, error) {
	wire, ok := protocol.ParseHarnessKind(value)
	if !ok {
		return "", fmt.Errorf("runtime.harness_kind %q is unsupported", value)
	}
	kind, ok := internalKindByWire[wire]
	if !ok {
		return "", fmt.Errorf("runtime.harness_kind %q is unsupported", value)
	}
	return kind, nil
}

// NativeGoal keeps generic outside the staged native launch family; unknown kinds fail closed.
func (kind Kind) NativeGoal() bool {
	switch kind {
	case KindCodex, KindClaude, KindPi, KindOMP, KindOmO, KindOpenCode:
		return true
	default:
		return false
	}
}

func (kind Kind) UnavailableProbe() bool {
	switch kind {
	case KindClaude, KindPi, KindOMP, KindOmO, KindOpenCode:
		return true
	default:
		return false
	}
}
