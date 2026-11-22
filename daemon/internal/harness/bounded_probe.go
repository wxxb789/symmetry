package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

// The delegate receives a context derived from ctx with timeout, so a hung executable
// cannot outlive the bound; a late or cancelled result is never capability evidence.
func RunBoundedProbe[T any](ctx context.Context, timeout time.Duration, run func(context.Context) (T, error)) (T, error) {
	var zero T
	if ctx == nil {
		return zero, errors.New("harness probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	probeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	value, err := run(probeContext)
	if contextErr := ctx.Err(); contextErr != nil {
		return zero, contextErr
	}
	if contextErr := probeContext.Err(); contextErr != nil {
		return zero, contextErr
	}
	return value, err
}

// Probe fixtures inject their own seam so a test never launches a native executable;
// the invoker owns the output bound.
type CommandInvoker func(context.Context, execution.Invocation, int) ([]byte, error)

func RunProbeCommand(ctx context.Context, invocation execution.Invocation, limit int, invoker CommandInvoker) ([]byte, error) {
	environment, err := execution.BuildEnvironment()
	if err != nil {
		return nil, err
	}
	invocation.Env = environment
	return invoker(ctx, invocation, limit)
}

// Generates the Codex app-server v2 schema bundle into a temporary directory; a
// caller that cannot capture the exact bundle never receives a digest.
func CodexSchemaDigest(ctx context.Context, executable string, limit int, invoker CommandInvoker) (digest string, resultErr error) {
	if ctx == nil {
		return "", errors.New("codex schema probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp("", "symmetry-codex-schema-")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", contextErr
		}
		return "", fmt.Errorf("create Codex schema temp directory: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(directory); cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove Codex schema temp directory: %w", cleanupErr))
		}
	}()
	output, err := RunProbeCommand(ctx, execution.Invocation{
		Program: executable,
		Args:    []string{"app-server", "generate-json-schema", "--out", directory},
	}, limit, invoker)
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	if err != nil {
		return "", fmt.Errorf("generate Codex app-server schema: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	bundle, err := os.ReadFile(filepath.Join(directory, "codex_app_server_protocol.v2.schemas.json"))
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	if err != nil {
		return "", fmt.Errorf("read generated Codex v2 schema: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	digestBytes := sha256.Sum256(bundle)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(digestBytes[:]), nil
}
