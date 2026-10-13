//go:build !linux || !amd64

package execution

import (
	"context"
	"strings"
	"testing"
)

func TestRunnerExplicitlyRejectsLinuxV2OnUnsupportedPlatforms(t *testing.T) {
	process, err := NewRunner().Start(context.Background(), Invocation{
		LinuxV2Required: true,
		LinuxV2:         &LinuxV2Invocation{},
	}, nil)
	if process != nil || err == nil || !strings.Contains(err.Error(), "Linux v2 process launch is supported only on Linux x86-64") {
		t.Fatalf("Start() = (%v, %v), want explicit unsupported Linux v2 error", process, err)
	}
}
