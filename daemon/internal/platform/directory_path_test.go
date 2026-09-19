package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExistingDirectoryRejectsInvalidRawPaths(t *testing.T) {
	directory := t.TempDir()
	separator := string(os.PathSeparator)
	for _, test := range []struct {
		name string
		path string
	}{
		{name: "empty", path: ""},
		{name: "relative", path: "directory"},
		{name: "NUL", path: directory + "\x00"},
		{name: "invalid UTF-8", path: directory + string([]byte{0xff})},
		{name: "current directory component", path: directory + separator + "."},
		{name: "parent directory component", path: directory + separator + ".."},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ResolveExistingDirectory(test.path); err == nil {
				t.Fatalf("ResolveExistingDirectory(%q) error = nil", test.path)
			}
		})
	}
}

func TestResolveExistingDirectoryRejectsFilesAndMissingPaths(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	for _, path := range []string{file, filepath.Join(directory, "missing")} {
		if _, err := ResolveExistingDirectory(path); err == nil {
			t.Fatalf("ResolveExistingDirectory(%q) error = nil", path)
		}
	}
}
