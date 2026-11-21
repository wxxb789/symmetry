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

// RunBoundedProbe runs one probe command under its own timeout and returns the
// value the delegate produced. The delegate receives a context derived from ctx
// with timeout, so a hung executable cannot outlive the bound; a delegate that
// answers only after the caller's context ended or after the bound fired is
// reported as that context error with no value, so a late or cancelled result is
// never promoted to capability evidence. Each provider keeps its own timeout
// constant and its own delegate; this is their single bounded-probe
// implementation.
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

// CommandInvoker runs one bounded probe command invocation and returns its
// captured output. The production implementation is
// execution.RunBoundedCommand; a package that injects its own command seam
// supplies that seam instead, so a probe fixture never launches a native
// executable. The invoker owns the output bound, so a package's own limit
// travels through this type unchanged.
type CommandInvoker func(context.Context, execution.Invocation, int) ([]byte, error)

// RunProbeCommand builds the probe environment and runs one bounded probe
// command through invoker. Every probe runner this change deduplicates shares
// this construction, so a host with an unusable environment or a spawn failure
// is reported identically by all of them.
func RunProbeCommand(ctx context.Context, invocation execution.Invocation, limit int, invoker CommandInvoker) ([]byte, error) {
	environment, err := execution.BuildEnvironment()
	if err != nil {
		return nil, err
	}
	invocation.Env = environment
	return invoker(ctx, invocation, limit)
}

// CodexSchemaDigest generates the Codex app-server v2 schema bundle into a
// temporary directory and returns the digest of the generated protocol file.
// The root registry runner and the codex package runner both route here through
// their own bounded command seam, so the schema command, its context checks and
// its error strings exist once. A caller that cannot capture the exact bundle
// never receives a digest.
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
