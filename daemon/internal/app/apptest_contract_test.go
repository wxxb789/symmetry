package app

import (
	"github.com/wxxb789/symmetry/daemon/internal/app/apptest"
)

var _ journalStore = (*apptest.FaultStore)(nil)
