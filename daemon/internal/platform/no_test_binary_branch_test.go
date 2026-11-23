package platform

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// selfInspectionTokens are the runtime test-runner detection shapes that must not
// appear in shipped containment code: they pick the route from the test runner.
var selfInspectionTokens = []string{
	"flag.CommandLine.Lookup",
	"flag.Lookup(",
	"testing.Testing",
	"test.v",
	"isGoTestBinary",
	`".test`,
}

func TestShippedContainmentCodeHasNoTestBinarySelfInspection(t *testing.T) {
	roots := []string{".", filepath.Join("..", "execution")}
	for _, root := range roots {
		found := 0
		walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			contents, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			found++
			for _, token := range selfInspectionTokens {
				if bytes.Contains(contents, []byte(token)) {
					t.Errorf("%s decides containment from the test runner via %q", filepath.ToSlash(path), token)
				}
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk shipped sources under %s: %v", root, walkErr)
		}
		if found == 0 {
			t.Fatalf("no shipped Go sources found under %s", root)
		}
	}
}

func TestInProcessContainmentCapabilityStaysTestOnly(t *testing.T) {
	capabilityTokens := map[string][]string{
		"UseInProcessContainmentSupervisorForTesting": {
			"internal/platform/containment_supervisor_testing_windows.go",
			"internal/platform/containment_supervisor_testing_other.go",
			"internal/execution/inprocess_containment_testing.go",
		},
		"installInProcessSupervisorRouteForTesting": {
			"internal/execution/inprocess_containment_testing.go",
			"internal/execution/inprocess_containment_testing_unsupported.go",
			"internal/execution/process_launcher_linux.go",
			"internal/execution/process_launcher_windows.go",
		},
	}
	root := filepath.Join("..", "..")
	for token, allowedFiles := range capabilityTokens {
		allowed := make(map[string]bool, len(allowedFiles))
		for _, file := range allowedFiles {
			allowed[file] = true
		}
		found := make(map[string]bool)
		walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			contents, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if !bytes.Contains(contents, []byte(token)) {
				return nil
			}
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			relative = filepath.ToSlash(relative)
			found[relative] = true
			if !allowed[relative] {
				t.Errorf("production source %s references the in-process containment test capability %q", relative, token)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk shipped sources under %s: %v", root, walkErr)
		}
		if len(found) != len(allowed) {
			t.Fatalf("sources referencing %q = %d, want the %d defining files", token, len(found), len(allowed))
		}
	}
}
