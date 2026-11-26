//go:build linux && amd64

package linuxreadback

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestProductionCommandsDoNotImportReadback pins that no production cmd links
// this test-only package into an installed daemon or helper binary.
func TestProductionCommandsDoNotImportReadback(t *testing.T) {
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the readback import guard source")
	}
	daemonRoot := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), "..", ".."))
	command := exec.Command("go", "list", "-deps", "./cmd/...")
	command.Dir = daemonRoot
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list -deps ./cmd/...: %v", err)
	}
	const forbidden = "github.com/wxxb789/symmetry/daemon/internal/linuxreadback"
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == forbidden || strings.HasPrefix(line, forbidden+"/") {
			t.Fatalf("a production cmd depends on the test-only readback package: %s", line)
		}
	}
}
