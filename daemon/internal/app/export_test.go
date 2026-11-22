package app

import (
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

// Run does not close the store.
func withStore(store *state.Store) Options {
	return func(value *options) {
		if store == nil {
			value.store = nil
			return
		}
		value.store = store
	}
}

func withControl(client ControlAPI) Options { return func(value *options) { value.control = client } }

func withEnrollment(client EnrollmentAPI) Options {
	return func(value *options) { value.enrollment = client }
}

func withWorkspace(service workspace.Service) Options {
	return func(value *options) { value.workspace = service }
}

func withStartProcess(start StartProcess) Options {
	return func(value *options) { value.start = start }
}
