package harness

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

func TestCodexParseVersionToleratesBoundedLeadingWarnings(t *testing.T) {
	version := CodexTestedVersion
	warning := "WARNING: proceeding, even though we could not create PATH aliases: " +
		"Refusing to create helper binaries under temporary dir \"C:\\\\Users\\\\lhan\\\\\""
	for _, output := range []string{
		"codex-cli " + version + "\n",
		warning + "\ncodex-cli " + version + "\n",
		warning + "\n" + warning + "\ncodex-cli " + version + "\n",
	} {
		if got := ParseCodexVersion(output); got != version {
			t.Fatalf("ParseCodexVersion(%q) = %q, want %q", output, got, version)
		}
	}
}

func TestCodexParseVersionRejectsExcessOrForeignLeadingLines(t *testing.T) {
	version := CodexTestedVersion
	warning := "WARNING: proceeding, even though we could not create PATH aliases: reason"
	excess := strings.Repeat(warning+"\n", codexVersionWarningLimit+1) + "codex-cli " + version + "\n"
	foreign := "not a codex version line\ncodex-cli " + version + "\n"
	for _, output := range []string{excess, foreign} {
		if got := ParseCodexVersion(output); got != "" {
			t.Fatalf("ParseCodexVersion(%q) = %q, want empty", output, got)
		}
	}
	// A warning after the version line does not invalidate an already complete
	// version line.
	versionFirst := "codex-cli " + version + "\n" + warning + "\n"
	if got := ParseCodexVersion(versionFirst); got != version {
		t.Fatalf("ParseCodexVersion(%q) = %q, want %q", versionFirst, got, version)
	}
}

func TestCodexProbeAcceptsWarningPrefixedVersionOutput(t *testing.T) {
	warning := "WARNING: proceeding, even though we could not create PATH aliases: reason"
	runner := &codexFixtureRunner{responses: map[string][]byte{
		"--version":         []byte(warning + "\ncodex-cli " + CodexTestedVersion + "\n"),
		"app-server --help": []byte("codex app-server stdio transport\n"),
	}, schemaDigest: CodexTestedSchemaHash}

	capabilities, err := probeCodexExecutableWithRunner(context.Background(), "codex", runner)
	if !errors.Is(err, ErrNativeUnverified) {
		t.Fatalf("probe error = %v, want ErrNativeUnverified", err)
	}
	if capabilities.NativeVersion != CodexTestedVersion || !capabilities.VersionKnown || !capabilities.TransportVerified {
		t.Fatalf("capabilities = %+v, want version and transport evidence from warning-prefixed output", capabilities)
	}
	if capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel {
		t.Fatalf("capabilities = %+v, want no promoted operations", capabilities)
	}
}

func TestCodexProbeKnownVersionHelpAndSchemaFailsClosed(t *testing.T) {
	runner := &codexFixtureRunner{responses: map[string][]byte{
		"--version":         []byte("codex-cli " + CodexTestedVersion + "\n"),
		"app-server --help": []byte("codex app-server stdio transport\n"),
	}, schemaDigest: CodexTestedSchemaHash}

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

func TestCodexProbeRejectsNonExactVersionOutput(t *testing.T) {
	for _, versionOutput := range []string{
		"codex-cli " + CodexTestedVersion + "-alpha.1\n",
		"codex-cli " + CodexTestedVersion + "+build.1\n",
		"codex-cli " + CodexTestedVersion + " trailing-token\n",
	} {
		t.Run(versionOutput, func(t *testing.T) {
			runner := &codexFixtureRunner{responses: map[string][]byte{"--version": []byte(versionOutput)}}
			capabilities, err := probeCodexExecutableWithRunner(context.Background(), "codex", runner)
			if !errors.Is(err, ErrUnsupportedVersion) {
				t.Fatalf("probe error = %v, want ErrUnsupportedVersion", err)
			}
			if capabilities.VersionKnown || capabilities.NativeVersion != "" || capabilities.TransportVerified {
				t.Fatalf("capabilities = %+v, want no version or transport evidence", capabilities)
			}
			if got, want := runner.calls, []string{"--version"}; !sameStrings(got, want) {
				t.Fatalf("runner calls = %#v, want %#v", got, want)
			}
		})
	}
}

func TestCodexProbeSchemaMismatchFailsClosed(t *testing.T) {
	runner := &codexFixtureRunner{
		responses: map[string][]byte{
			"--version":         []byte("codex-cli " + CodexTestedVersion + "\n"),
			"app-server --help": []byte("codex app-server stdio transport\n"),
		},
		schemaDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	capabilities, err := probeCodexExecutableWithRunner(context.Background(), "codex", runner)
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("probe error = %v, want ErrUnsupportedVersion", err)
	}
	if !capabilities.VersionKnown || !capabilities.TransportVerified || capabilities.Verified || capabilities.Start {
		t.Fatalf("capabilities = %+v, want known transport but no native operations", capabilities)
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
			responses := map[string][]byte{"--version": []byte("codex-cli " + CodexTestedVersion + "\n")}
			if test.stage == "version" {
				results["--version"] = codexFixtureResult{output: []byte("codex-cli " + CodexTestedVersion + "\n"), err: test.err}
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
	responses    map[string][]byte
	results      map[string]codexFixtureResult
	calls        []string
	schemaDigest string
}

type codexFixtureResult struct {
	output []byte
	err    error
}

func (runner *codexFixtureRunner) SchemaDigest(_ context.Context, _ string) (string, error) {
	if runner.schemaDigest == "" {
		return "", errors.New("fixture schema digest not configured")
	}
	return runner.schemaDigest, nil
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
