package app

import (
	"github.com/wxxb789/symmetry/daemon/internal/app/apptest"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

var _ journalStore = (*apptest.FaultStore)(nil)
var _ ControlAPI = (*apptest.ControlAPI)(nil)
var _ Process = (*apptest.Process)(nil)
var _ harness.EventSink = (*apptest.EventSink)(nil)
