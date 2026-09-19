//go:build symmetry_pi_control_e2e

package app

import (
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// WithPiControlE2EWitnessRegistry is compiled only into the explicitly named
// Pi Control E2E daemon child. It cannot be reached by the normal daemon
// command or by a production configuration file.
func WithPiControlE2EWitnessRegistry(registry *harness.Registry) Options {
	return func(value *options) {
		value.harnessRegistry = registry
	}
}

// WithPiControlE2EProcessObserver observes a process marker only after the
// daemon has persisted its normal process identity. It is compiled only into
// the explicitly named Pi Control E2E child.
func WithPiControlE2EProcessObserver(observer func(state.RunKey, int, string, time.Time)) Options {
	return func(value *options) {
		value.processObserver = observer
	}
}
