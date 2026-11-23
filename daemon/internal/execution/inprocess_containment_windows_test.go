//go:build windows

package execution

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	restore := UseInProcessContainmentSupervisorForTesting()
	code := m.Run()
	restore()
	os.Exit(code)
}
