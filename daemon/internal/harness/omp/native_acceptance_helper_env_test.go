package omp

import (
	"strings"
	"testing"
)

// The lost-ack helper runs as a separate process and re-derives its own
// acceptance configuration, so the authorization it inherits must satisfy its
// own spend gate. When it does not, the helper skips and can never reach the
// durable lost-ack boundary the parent requires.
func TestNativeAcceptanceHelperEnvironmentCarriesUsableSpendAuthorization(t *testing.T) {
	t.Setenv("SYMMETRY_OMP_OWNER_SPEND_MICROUSD", "1")
	environment := nativeAcceptanceHelperEnvironment(t, t.TempDir())
	authorization := ""
	for _, entry := range environment {
		if name, value, found := strings.Cut(entry, "="); found && name == "SYMMETRY_OMP_OWNER_SPEND_MICROUSD" {
			authorization = value
		}
	}
	if err := nativeAcceptancePromptSpendDecision(authorization); err != nil {
		t.Fatalf("helper subprocess cannot authorize the spend it was started with: %v", err)
	}
}
