package omp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

const TestedVersion = "18.20.0"
const ImplementationVersion = "omp-rpc-v2"

type binaryPin struct {
	size   int64
	digest string
}

var binaryPins = map[string]binaryPin{
	"windows": {251502592, "1b6c7bda8241bc7d948495539f90ec47e6f30d1fec72b6ee097a67a1fc8d8e48"},
	"linux":   {287484080, "c8694be8c6e6a1f45e3636dd8a8e9eaa78878ad629a89000b403ec5e456d3f89"},
}

func observedCapabilities() harness.Capabilities {
	capabilities := harness.UnsupportedCapabilities(harness.KindOMP, "successful native repository turn and control acceptance remain unverified")
	capabilities.NativeVersion, capabilities.ImplementationVersion, capabilities.ProtocolVersion = TestedVersion, ImplementationVersion, 2
	capabilities.VersionKnown, capabilities.TransportVerified = true, true
	capabilities.Unsupported[string(harness.CapabilityProviderAccess)] = "Control provider broker bridge is unsupported; native model authentication is separate"
	return capabilities
}

func pinnedExecutable(executable, platform string) (string, error) {
	pin, known := binaryPins[platform]
	if !known {
		return "", fmt.Errorf("%w: OMP platform is not pinned", harness.ErrUnsupportedVersion)
	}
	resolved, err := resolveExecutable(executable)
	if err != nil {
		return "", err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != pin.size {
		return "", fmt.Errorf("%w: OMP executable blob is not pinned", harness.ErrUnsupportedVersion)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	if hex.EncodeToString(hash.Sum(nil)) != pin.digest {
		return "", fmt.Errorf("%w: OMP executable digest is not pinned", harness.ErrUnsupportedVersion)
	}
	return resolved, nil
}

func resolveExecutable(executable string) (string, error) {
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return "", fmt.Errorf("%w: OMP executable", harness.ErrHarnessUnavailable)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(resolved)
}

// Probe observes an initial v1 ready frame and a correlated successful v2
// negotiation in a real, prompt-free RPC process. Help text alone proves no
// operation. Every probe process is stopped and drained before returning.
func (adapter *Adapter) Probe(ctx context.Context) (harness.Capabilities, error) {
	capabilities := harness.UnsupportedCapabilities(harness.KindOMP, "OMP executable/version/protocol has not been verified")
	capabilities.ImplementationVersion, capabilities.ProtocolVersion = ImplementationVersion, 2
	if adapter == nil || ctx == nil {
		return capabilities, errors.New("OMP probe requires adapter and context")
	}
	if adapter.probe != nil {
		return adapter.probe(ctx)
	}
	if err := ctx.Err(); err != nil {
		return capabilities, err
	}
	if runtime.GOARCH != "amd64" {
		return capabilities, fmt.Errorf("%w: OMP architecture is not pinned", harness.ErrUnsupportedVersion)
	}
	executable := adapter.executable
	if strings.TrimSpace(executable) == "" {
		executable = "omp"
	}
	executable, err := pinnedExecutable(executable, runtime.GOOS)
	if err != nil {
		return capabilities, err
	}
	environment, err := execution.BuildEnvironment("PI_CODING_AGENT_DIR")
	if err != nil {
		return capabilities, err
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	version, err := execution.RunBoundedCommand(bounded, execution.Invocation{Program: executable, Args: []string{"--version"}, Env: environment}, 64<<10)
	if err != nil {
		return capabilities, err
	}
	if strings.TrimSpace(string(version)) != "omp/"+TestedVersion {
		return capabilities, fmt.Errorf("%w: unknown OMP version line", harness.ErrUnsupportedVersion)
	}
	help, err := execution.RunBoundedCommand(bounded, execution.Invocation{Program: executable, Args: []string{"--help"}, Env: environment}, 256<<10)
	if err != nil {
		return capabilities, err
	}
	if !auditedHelp(string(help)) {
		return capabilities, fmt.Errorf("%w: unknown OMP RPC help", harness.ErrUnsupportedVersion)
	}
	directory, err := os.MkdirTemp("", "symmetry-omp-probe-")
	if err != nil {
		return capabilities, err
	}
	defer os.RemoveAll(directory)
	processContext, stopProcess := context.WithCancel(bounded)
	defer stopProcess()
	session := newSession(processContext, stopProcess, harness.EventSinkFunc(func(context.Context, harness.Event) error { return nil }), directory, nil)
	args := rpcArgs([]string{"--session-dir", directory, "--no-extensions", "--no-skills", "--no-rules", "--no-context-files", "--no-lsp", "--no-pty", "--no-title"}, "")
	process, startErr := startProcess(processContext, execution.Invocation{Program: executable, Args: args, Dir: directory, Env: environment}, execution.SinkFunc(session.output))
	if process == nil {
		return capabilities, startErr
	}
	session.process = process
	if startErr != nil {
		session.fail(startErr)
	}
	close(session.launchReady)
	go session.watchProcess()
	var protocolErr error
	if startErr == nil {
		select {
		case <-session.ready:
			_, protocolErr = session.call(bounded, "negotiate_protocol", map[string]any{"protocolVersion": 2})
		case <-session.physicalDone:
			protocolErr = errors.New("OMP probe exited before ready")
		case <-processContext.Done():
			protocolErr = processContext.Err()
		}
	}
	cleanup, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	closeErr := session.Close(cleanup)
	cleanupCancel()
	if err := errors.Join(startErr, protocolErr, closeErr); err != nil {
		return capabilities, err
	}
	session.mu.Lock()
	result, negotiated, failure := session.physical, session.negotiated, session.failure
	session.mu.Unlock()
	if failure != nil {
		return capabilities, failure
	}
	if !negotiated || !result.Success() {
		return capabilities, errors.New("OMP probe did not negotiate and drain cleanly")
	}
	return observedCapabilities(), harness.ErrNativeUnverified
}

func auditedHelp(help string) bool {
	for _, line := range strings.Split(help, "\n") {
		if strings.Contains(line, "--mode=<value>") && strings.Contains(line, "rpc") {
			return true
		}
	}
	return false
}
