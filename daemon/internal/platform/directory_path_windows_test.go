//go:build windows

package platform

import (
	"os"
	"strings"
	"testing"
)

func TestResolveExistingDirectoryReturnsExtendedDOSPath(t *testing.T) {
	directory := t.TempDir()
	resolved, err := ResolveExistingDirectory(directory)
	if err != nil {
		t.Fatalf("ResolveExistingDirectory() error = %v", err)
	}
	if !strings.HasPrefix(resolved, `\\?\`) || !isKnownFinalWindowsDirectoryPath(resolved) {
		t.Fatalf("ResolveExistingDirectory() = %q, want an extended DOS path", resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		t.Fatalf("stat resolved directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("resolved path mode = %v, want directory", info.Mode())
	}
}

func TestResolveExistingDirectoryAcceptsExtendedDOSInput(t *testing.T) {
	directory := t.TempDir()
	resolved, err := ResolveExistingDirectory(directory)
	if err != nil {
		t.Fatalf("ResolveExistingDirectory(directory) error = %v", err)
	}
	extended, err := ResolveExistingDirectory(`\\?\` + directory)
	if err != nil {
		t.Fatalf("ResolveExistingDirectory(extended directory) error = %v", err)
	}
	if extended != resolved {
		t.Fatalf("ResolveExistingDirectory(extended directory) = %q, want %q", extended, resolved)
	}
}

func TestResolveExistingDirectoryRejectsSlashDotComponents(t *testing.T) {
	directory := t.TempDir()
	for _, path := range []string{directory + "/.", directory + "/.."} {
		if _, err := ResolveExistingDirectory(path); err == nil {
			t.Fatalf("ResolveExistingDirectory(%q) error = nil", path)
		}
	}
}

func TestResolveExistingDirectoryRejectsExtendedDriveRelativePath(t *testing.T) {
	directory := t.TempDir()
	if len(directory) < 2 || directory[1] != ':' {
		t.Skipf("temporary directory %q does not use a drive-letter path", directory)
	}
	path := `\\?\` + directory[:2] + `relative`
	if _, err := ResolveExistingDirectory(path); err == nil {
		t.Fatalf("ResolveExistingDirectory(%q) error = nil", path)
	}
}

func TestRawDirectoryPathAbsoluteWindowsForms(t *testing.T) {
	for _, test := range []struct {
		path string
		want bool
	}{
		{`C:\workspace`, true}, {`C:/workspace`, true},
		{`\\server\share\workspace`, true}, {`//server/share/workspace`, true},
		{`\\?\C:\workspace`, true}, {`\\?\UNC\server\share\workspace`, true},
		{`C:relative`, false}, {`\\?\C:relative`, false}, {`//?/C:relative`, false},
		{`\\server`, false}, {`\\?\UNC\server`, false}, {`\relative`, false},
	} {
		if got := rawDirectoryPathIsAbsolute(test.path); got != test.want {
			t.Errorf("rawDirectoryPathIsAbsolute(%q) = %t, want %t", test.path, got, test.want)
		}
	}
}

func TestStrictDirectoryPathUTF16(t *testing.T) {
	for _, test := range []struct {
		name    string
		units   []uint16
		want    string
		wantErr bool
	}{
		{"ASCII", []uint16{'a', 'b'}, "ab", false},
		{"paired", []uint16{0xD83D, 0xDE00}, "\U0001F600", false},
		{"high surrogate", []uint16{0xD83D}, "", true},
		{"low surrogate", []uint16{0xDE00}, "", true},
		{"NUL", []uint16{'a', 0, 'b'}, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := strictUTF16String(test.units)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("strictUTF16String() = %q, %v", got, err)
			}
		})
	}
}
