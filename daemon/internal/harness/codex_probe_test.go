package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

func TestCodexProbeKnownVersionAndHelpFailsClosed(t *testing.T) {
	runner := &codexFixtureRunner{responses: map[string][]byte{
		"--version":         []byte("codex-cli 0.153.4\n"),
		"app-server --help": []byte("codex app-server stdio transport\n"),
	}}

	capabilities, err := probeCodexExecutableWithRunner(context.Background(), "codex", runner)
	if !errors.Is(err, ErrNativeUnverified) {
		t.Fatalf("probe error = %v, want ErrNativeUnverified", err)
	}
	if capabilities.Kind != KindCodex || capabilities.NativeVersion != testedCodexVersion || !capabilities.VersionKnown {
		t.Fatalf("capabilities identity = %+v, want Codex %s with known version", capabilities, testedCodexVersion)
	}
	if !capabilities.TransportVerified || capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel {
		t.Fatalf("capabilities = %+v, want help-only transport evidence and no operations", capabilities)
	}
	if got, want := runner.calls, []string{"--version", "app-server --help"}; !sameStrings(got, want) {
		t.Fatalf("runner calls = %#v, want %#v", got, want)
	}
}

func TestCodexProbeDoesNotParseFailedCommandOutput(t *testing.T) {
	for _, test := range []struct {
		name  string
		stage string
		err   error
	}{
		{name: "version output limit", stage: "version", err: execution.ErrOutputLimitExceeded},
		{name: "version termination", stage: "version", err: errors.New("bounded process termination failed")},
		{name: "help output limit", stage: "help", err: execution.ErrOutputLimitExceeded},
		{name: "help termination", stage: "help", err: errors.New("bounded process termination failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			results := map[string]codexFixtureResult{}
			responses := map[string][]byte{"--version": []byte("codex-cli 0.153.4\n")}
			if test.stage == "version" {
				results["--version"] = codexFixtureResult{output: []byte("codex-cli 0.153.4\n"), err: test.err}
			} else {
				responses["app-server --help"] = []byte("codex app-server stdio transport\n")
				results["app-server --help"] = codexFixtureResult{output: []byte("codex app-server stdio transport\n"), err: test.err}
			}
			runner := &codexFixtureRunner{responses: responses, results: results}
			capabilities, err := probeCodexExecutableWithRunner(context.Background(), "codex", runner)
			if err == nil {
				t.Fatal("probeCodexExecutable() error = nil, want bounded command failure")
			}
			if test.stage == "version" {
				if capabilities.VersionKnown || capabilities.NativeVersion != "" || capabilities.TransportVerified || capabilities.Verified {
					t.Fatalf("capabilities = %+v, want no capability parsed from failed version output", capabilities)
				}
				return
			}
			if !capabilities.VersionKnown || capabilities.NativeVersion != testedCodexVersion || capabilities.TransportVerified || capabilities.Verified {
				t.Fatalf("capabilities = %+v, want version-only evidence after failed help output", capabilities)
			}
		})
	}
}

type codexFixtureRunner struct {
	responses map[string][]byte
	results   map[string]codexFixtureResult
	calls     []string
}

type codexFixtureResult struct {
	output []byte
	err    error
}

func (runner *codexFixtureRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := ""
	for index, arg := range args {
		if index > 0 {
			key += " "
		}
		key += arg
	}
	runner.calls = append(runner.calls, key)
	if result, ok := runner.results[key]; ok {
		return result.output, result.err
	}
	if response, ok := runner.responses[key]; ok {
		return response, nil
	}
	return nil, errors.New("fixture command not found")
}
