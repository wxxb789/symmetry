//go:build !windows

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExistingDirectoryResolvesDirectoryAndSymlink(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}

	resolved, err := ResolveExistingDirectory(directory)
	if err != nil {
		t.Fatalf("ResolveExistingDirectory(directory) error = %v", err)
	}
	if resolved != directory {
		t.Fatalf("ResolveExistingDirectory(directory) = %q, want %q", resolved, directory)
	}

	link := filepath.Join(root, "directory-link")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatalf("create directory symlink: %v", err)
	}
	resolved, err = ResolveExistingDirectory(link)
	if err != nil {
		t.Fatalf("ResolveExistingDirectory(link) error = %v", err)
	}
	if resolved != directory {
		t.Fatalf("ResolveExistingDirectory(link) = %q, want %q", resolved, directory)
	}
}
