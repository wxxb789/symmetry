package omp

import (
	"os"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

func TestMain(m *testing.M) {
	restore := execution.UseInProcessContainmentSupervisorForTesting()
	code := m.Run()
	restore()
	os.Exit(code)
}
