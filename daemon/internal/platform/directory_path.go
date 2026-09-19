// Package platform contains OS-specific process and filesystem primitives.
package platform

import (
	"errors"
	"os"
	"strings"
	"unicode/utf8"
)

// ResolveExistingDirectory resolves an existing absolute directory without
// accepting lexical path navigation in the supplied path.
func ResolveExistingDirectory(path string) (string, error) {
	if err := validateRawDirectoryPath(path); err != nil {
		return "", err
	}
	return resolveExistingDirectory(path)
}

func validateRawDirectoryPath(path string) error {
	if path == "" {
		return errors.New("directory path is required")
	}
	if !utf8.ValidString(path) {
		return errors.New("directory path is not valid UTF-8")
	}
	if strings.IndexByte(path, 0) >= 0 {
		return errors.New("directory path contains NUL")
	}
	if !rawDirectoryPathIsAbsolute(path) {
		return errors.New("directory path must be absolute")
	}
	if rawDirectoryPathHasDotComponent(path) {
		return errors.New("directory path contains a dot navigation component")
	}
	return nil
}

func rawDirectoryPathHasDotComponent(path string) bool {
	componentStart := 0
	for index := 0; index <= len(path); index++ {
		if index != len(path) && !os.IsPathSeparator(path[index]) {
			continue
		}
		component := path[componentStart:index]
		if component == "." || component == ".." {
			return true
		}
		componentStart = index + 1
	}
	return false
}
