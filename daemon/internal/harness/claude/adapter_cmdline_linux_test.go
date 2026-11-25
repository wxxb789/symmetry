//go:build linux && amd64

package claude

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

// The Linux owner wire admits one argv element only up to protocol.maxCommandString,
// and --json-schema carries the whole TaskResult schema as one argv element.
func TestClaudeCandidateLinuxCommandArgvFitsOwnerWireBound(t *testing.T) {
	var invocation execution.Invocation
	process := newCandidateFakeProcess()
	request := candidateStartRequest(t)
	session, err := newCandidateTestAdapter(process, &invocation).Start(context.Background(), request, &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	executable := "/home/runneradmin/.local/share/mise/installs/claude-code/" + testedVersion + "/claude"
	stdin, stdout, stderr, owner := candidateLinuxOwnerWirePipes(t)
	command := linuxgate.NativeCommand{
		Path: executable, Dir: request.Workspace, Args: append([]string{executable}, invocation.Args...), Env: invocation.Env,
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
	}
	if err := command.Validate(); err != nil {
		t.Fatalf("the fixed Claude argv is refused by the Linux owner native command contract: %v", err)
	}
	var token [32]byte
	guard := linuxgate.ReleaseGuard{OwnerPipe: owner, LeaseDeadline: time.Now().Add(time.Minute)}
	packet, err := linuxgate.EncodeRelease(token, command, guard)
	if err != nil {
		t.Fatalf("encode the fixed Claude argv as a guarded RELEASE: %v", err)
	}
	schemaIndex := slices.Index(invocation.Args, "--json-schema")
	if schemaIndex < 0 || schemaIndex+1 >= len(invocation.Args) {
		t.Fatal("the fixed Claude argv lost its --json-schema element")
	}
	schemaBytes := len(invocation.Args[schemaIndex+1])
	if schemaBytes <= 16<<10 {
		t.Fatalf("the fixed Claude argv --json-schema element = %d bytes, want above the pre-widening 16 KiB cap so the mutant clause stays red", schemaBytes)
	}
	t.Logf("Claude candidate --json-schema element = %d bytes, guarded RELEASE packet = %d bytes", schemaBytes, len(packet))
}

func candidateLinuxOwnerWirePipes(t *testing.T) (stdin, stdout, stderr, owner *os.File) {
	t.Helper()
	newPipe := func() (*os.File, *os.File) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("create candidate owner-wire pipe: %v", err)
		}
		return reader, writer
	}
	stdinRead, stdinWrite := newPipe()
	stdoutRead, stdoutWrite := newPipe()
	stderrRead, stderrWrite := newPipe()
	ownerRead, ownerWrite := newPipe()
	t.Cleanup(func() {
		for _, file := range []*os.File{stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite, ownerRead, ownerWrite} {
			_ = file.Close()
		}
	})
	return stdinRead, stdoutWrite, stderrWrite, ownerRead
}
