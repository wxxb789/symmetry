package app

import (
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

// These option constructors exist for in-package tests only: each one injects a
// concrete double for a seam that production Run wires itself. They live in a
// test-only file so a production caller cannot reach them.

// withStore supplies an already-open state store. Run does not close it.
func withStore(store *state.Store) Options { return func(value *options) { value.store = store } }

// withControl supplies the authenticated control-plane client.
func withControl(client ControlAPI) Options { return func(value *options) { value.control = client } }

// withEnrollment supplies the enrollment client.
func withEnrollment(client EnrollmentAPI) Options {
	return func(value *options) { value.enrollment = client }
}

// withWorkspace supplies the local workspace service.
func withWorkspace(service workspace.Service) Options {
	return func(value *options) { value.workspace = service }
}

// withStartProcess supplies the local process launcher.
func withStartProcess(start StartProcess) Options {
	return func(value *options) { value.start = start }
}
