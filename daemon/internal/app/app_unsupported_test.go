//go:build !linux && !windows

package app

import (
	"context"
	"strings"
	"testing"
)

func TestRunFailsBeforeEnrollmentWithoutProcessIdentity(t *testing.T) {
	enrollment := &fakeEnrollment{}

	err := Run(context.Background(), testConfig(t), WithEnrollment(enrollment))
	if err == nil || !strings.Contains(err.Error(), "process identity") {
		t.Fatalf("Run() error = %v, want process identity failure", err)
	}
	if enrollment.calls != 0 {
		t.Fatalf("Enroll calls = %d, want 0", enrollment.calls)
	}
}
