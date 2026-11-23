package platform

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// selfInspectionTokens are the shapes the deleted Go-test-binary fork used to
// decide the containment route; a shipped file containing one re-introduces
// runtime test-runner detection.
var selfInspectionTokens = []string{
	"flag.CommandLine.Lookup",
	"test.v",
	"isGoTestBinary",
	`".test`,
}

func TestShippedContainmentCodeHasNoTestBinarySelfInspection(t *testing.T) {
	roots := []string{".", filepath.Join("..", "execution")}
	shipped := 0
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
			shipped++
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
	if shipped < 50 {
		t.Fatalf("shipped Go sources scanned = %d, want the platform and execution trees", shipped)
	}
}

func TestInProcessContainmentCapabilityStaysTestOnly(t *testing.T) {
	allowed := map[string]bool{
		"internal/platform/containment_supervisor_testing_windows.go": true,
		"internal/platform/containment_supervisor_testing_other.go":   true,
		"internal/execution/inprocess_containment_testing.go":         true,
	}
	root := filepath.Join("..", "..")
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
		if !bytes.Contains(contents, []byte("UseInProcessContainmentSupervisorForTesting")) {
			return nil
		}
		found++
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if !allowed[filepath.ToSlash(relative)] {
			t.Errorf("production source %s references the in-process containment test capability", filepath.ToSlash(relative))
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk shipped sources under %s: %v", root, walkErr)
	}
	if found != len(allowed) {
		t.Fatalf("capability references found = %d, want the %d defining files", found, len(allowed))
	}
}
