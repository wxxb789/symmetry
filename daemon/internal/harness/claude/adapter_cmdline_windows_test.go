//go:build windows

package claude

import (
	"context"
	"testing"
	"unicode/utf16"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"golang.org/x/sys/windows"
)

// CreateProcessW rejects command lines longer than 32767 UTF-16 units, and
// --json-schema carries the whole TaskResult schema as one argv element.
func TestClaudeCandidateWindowsCommandLineFitsCreateProcessLimit(t *testing.T) {
	var invocation execution.Invocation
	process := newCandidateFakeProcess()
	session, err := newCandidateTestAdapter(process, &invocation).Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	executable := `C:\Users\runneradmin\AppData\Local\mise\installs\claude-code\` + testedVersion + `\claude.exe`
	commandLine := windows.ComposeCommandLine(append([]string{executable}, invocation.Args...))
	if units := len(utf16.Encode([]rune(commandLine))); units >= 32767 {
		t.Fatalf("Claude candidate command line = %d UTF-16 units, want < 32767", units)
	} else {
		t.Logf("Claude candidate command line = %d UTF-16 units (headroom %d)", units, 32767-units)
	}
}
