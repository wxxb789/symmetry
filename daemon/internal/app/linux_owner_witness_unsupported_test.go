//go:build !linux || !amd64

package app

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLinuxV2NativeOwnerWitnessRejectsUnsupportedHost(t *testing.T) {
	t.Setenv(LinuxV2NativeOwnerWitnessEnvironment, "1")
	err := RunLinuxV2NativeOwnerWitness(context.Background(), "/opt/symmetry-owner-test/config.json", "/opt/symmetry-owner-test/request.json", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "supported only on Linux amd64") {
		t.Fatalf("RunLinuxV2NativeOwnerWitness() error = %v, want explicit unsupported-host refusal", err)
	}
}
