//go:build !windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

func rawDirectoryPathIsAbsolute(path string) bool {
	return filepath.IsAbs(path)
}

func resolveExistingDirectory(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect resolved directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("resolved path is not a directory")
	}
	return resolved, nil
}
