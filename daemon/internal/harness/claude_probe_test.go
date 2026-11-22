package harness

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBoundedProbeRejectsSuccessAfterDeadline(t *testing.T) {
	output, err := RunBoundedProbe(context.Background(), time.Nanosecond, func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return []byte("valid capability output"), nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunBoundedProbe() error = %v, want context.DeadlineExceeded", err)
	}
	if output != nil {
		t.Fatalf("RunBoundedProbe() output = %q, want nil after deadline", output)
	}
}
