package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestReadSubjectArtifactIgnoresDirtyCheckout(t *testing.T) {
	repository := newRepository(t)
	committed := []byte("committed artifact\n")
	artifactPath := "artifact.txt"
	if err := os.WriteFile(filepath.Join(repository, artifactPath), committed, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", artifactPath)
	runGit(t, repository, "commit", "-m", "add artifact")
	manager, subject := artifactSubjectManager(t, repository)

	if err := os.WriteFile(filepath.Join(repository, artifactPath), []byte("dirty checkout bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := manager.ReadSubjectArtifact(context.Background(), "primary", subject, artifactPath)
	if err != nil {
		t.Fatalf("ReadSubjectArtifact() error = %v", err)
	}
	if !bytes.Equal(result.Content, committed) {
		t.Fatalf("artifact content = %q, want committed bytes %q", result.Content, committed)
	}
	if result.ContentDigest != digestBytes(committed) {
		t.Fatalf("artifact digest = %q, want %q", result.ContentDigest, digestBytes(committed))
	}
}

func TestReadSubjectArtifactIgnoresGitReplaceRefs(t *testing.T) {
	repository := newRepository(t)
	originalCommit := gitOutput(t, repository, "rev-parse", "HEAD")
	manager, subject := artifactSubjectManager(t, repository)
	installSubjectReplaceRefs(t, repository, originalCommit, "README.md", "replacement artifact bytes\n")

	result, err := manager.ReadSubjectArtifact(context.Background(), "primary", subject, "README.md")
	if err != nil {
		t.Fatalf("ReadSubjectArtifact() with replace refs error = %v", err)
	}
	if !bytes.Equal(result.Content, []byte("test\n")) {
		t.Fatalf("artifact content = %q, want original bytes %q", result.Content, "test\n")
	}
}

func TestReadSubjectArtifactRejectsMissingPath(t *testing.T) {
	repository := newRepository(t)
	manager, subject := artifactSubjectManager(t, repository)

	_, err := manager.ReadSubjectArtifact(context.Background(), "primary", subject, "missing.txt")
	if err == nil || !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("ReadSubjectArtifact() error = %v, want ErrArtifactNotFound", err)
	}
}

func TestReadSubjectArtifactDoesNotNormalizeCommitPaths(t *testing.T) {
	repository := newRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "proof.txt"), []byte("proof\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "proof.txt")
	runGit(t, repository, "commit", "-m", "add proof")
	manager, subject := artifactSubjectManager(t, repository)

	for _, path := range []string{"./proof.txt", "proof.txt/"} {
		t.Run(path, func(t *testing.T) {
			_, err := manager.ReadSubjectArtifact(context.Background(), "primary", subject, path)
			if err == nil || !errors.Is(err, ErrArtifactNotFound) {
				t.Fatalf("ReadSubjectArtifact(%q) error = %v, want ErrArtifactNotFound", path, err)
			}
		})
	}
}

func TestReadSubjectArtifactRejectsUnsafePaths(t *testing.T) {
	repository := newRepository(t)
	manager, subject := artifactSubjectManager(t, repository)
	for _, path := range []string{
		"../README.md",
		"nested/../../README.md",
		"/README.md",
		"C:\\README.md",
		"nested\\README.md",
		"nested//README.md",
		"nested\x00README.md",
	} {
		t.Run(path, func(t *testing.T) {
			_, err := manager.ReadSubjectArtifact(context.Background(), "primary", subject, path)
			if err == nil || !errors.Is(err, ErrInvalidArtifactPath) {
				t.Fatalf("ReadSubjectArtifact(%q) error = %v, want ErrInvalidArtifactPath", path, err)
			}
		})
	}
}

func TestReadSubjectArtifactMatchesColonPathsFromGitTree(t *testing.T) {
	repository := newRepository(t)
	driveContent := []byte("drive-like path\n")
	magicContent := []byte("pathspec-like path\n")
	driveBlob := gitObjectFromInput(t, repository, driveContent, "hash-object", "-w", "--stdin")
	magicBlob := gitObjectFromInput(t, repository, magicContent, "hash-object", "-w", "--stdin")
	childTree := gitObjectFromInput(t, repository, []byte("100644 blob "+driveBlob+"\tproof.txt\n"), "mktree")
	rootTree := gitObjectFromInput(t, repository, []byte(fmt.Sprintf("040000 tree %s\tC:\n100644 blob %s\t:(top)proof.txt\n", childTree, magicBlob)), "mktree")
	baseCommit := gitOutput(t, repository, "rev-parse", "HEAD")
	commit := gitOutput(t, repository, "commit-tree", rootTree, "-p", baseCommit, "-m", "synthetic colon paths")
	manager, subject := artifactSubjectManagerAtCommit(t, repository, commit)

	tests := []struct {
		path    string
		content []byte
	}{
		{path: "C:/proof.txt", content: driveContent},
		{path: ":(top)proof.txt", content: magicContent},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			result, err := manager.ReadSubjectArtifact(context.Background(), "primary", subject, test.path)
			if err != nil {
				t.Fatalf("ReadSubjectArtifact(%q) error = %v", test.path, err)
			}
			if result.Path != test.path {
				t.Fatalf("artifact path = %q, want original %q", result.Path, test.path)
			}
			if !bytes.Equal(result.Content, test.content) {
				t.Fatalf("artifact content = %q, want %q", result.Content, test.content)
			}
			if result.ContentDigest != digestBytes(test.content) {
				t.Fatalf("artifact digest = %q, want %q", result.ContentDigest, digestBytes(test.content))
			}
		})
	}
}

func TestValidateSubjectArtifactPathUsesCommitPathContract(t *testing.T) {
	for _, path := range []string{"./proof.txt", "proof.txt/", "C:/proof.txt", ":", ":(top)proof.txt"} {
		t.Run(path, func(t *testing.T) {
			if err := validateSubjectArtifactPath(path); err != nil {
				t.Fatalf("validateSubjectArtifactPath(%q) error = %v, want protocol CommitPath acceptance", path, err)
			}
		})
	}
}

func TestReadSubjectArtifactMatchesLiteralWildcardFilename(t *testing.T) {
	repository := newRepository(t)
	artifactPath := "literal*artifact.txt"
	content := []byte("wildcard filename")
	if err := os.WriteFile(filepath.Join(repository, artifactPath), content, 0o600); err != nil {
		t.Skipf("platform does not permit wildcard filename %q: %v", artifactPath, err)
	}
	runGit(t, repository, "add", "--", artifactPath)
	runGit(t, repository, "commit", "-m", "add literal wildcard artifact")
	manager, subject := artifactSubjectManager(t, repository)

	result, err := manager.ReadSubjectArtifact(context.Background(), "primary", subject, artifactPath)
	if err != nil {
		t.Fatalf("ReadSubjectArtifact() error = %v", err)
	}
	if !bytes.Equal(result.Content, content) {
		t.Fatalf("artifact content = %q, want %q", result.Content, content)
	}
	if result.ContentDigest != digestBytes(content) {
		t.Fatalf("artifact digest = %q, want %q", result.ContentDigest, digestBytes(content))
	}
}

func TestReadSubjectArtifactPreservesBinaryBytesAndNoTrailingNewline(t *testing.T) {
	repository := newRepository(t)
	artifactPath := "binary.dat"
	content := []byte{0x00, 0x01, 0x7f, 0x80, 0xfe, 0xff}
	if err := os.WriteFile(filepath.Join(repository, artifactPath), content, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", artifactPath)
	runGit(t, repository, "commit", "-m", "add binary artifact")
	manager, subject := artifactSubjectManager(t, repository)

	result, err := manager.ReadSubjectArtifact(context.Background(), "primary", subject, artifactPath)
	if err != nil {
		t.Fatalf("ReadSubjectArtifact() error = %v", err)
	}
	if !bytes.Equal(result.Content, content) {
		t.Fatalf("artifact content = %v, want %v", result.Content, content)
	}
	if result.ContentDigest != digestBytes(content) {
		t.Fatalf("artifact digest = %q, want %q", result.ContentDigest, digestBytes(content))
	}
}

func artifactSubjectManager(t *testing.T, repository string) (*Manager, protocol.Subject) {
	t.Helper()
	return artifactSubjectManagerAtCommit(t, repository, gitOutput(t, repository, "rev-parse", "HEAD"))
}

func artifactSubjectManagerAtCommit(t *testing.T, repository, commit string) (*Manager, protocol.Subject) {
	t.Helper()
	manager := New(map[string]config.Workspace{
		"primary": {
			Policy:     config.WorkspacePolicyGitWorktree,
			Repository: repository,
			Root:       filepath.Join(t.TempDir(), "worktrees"),
			Ref:        "HEAD",
			Cleanup:    config.CleanupAlways,
		},
	})
	subject, err := manager.MaterializeSubject(context.Background(), "primary", subjectResourceID, commit)
	if err != nil {
		t.Fatalf("MaterializeSubject() error = %v", err)
	}
	return manager, subject
}

func gitObjectFromInput(t *testing.T, repository string, input []byte, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func digestBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}
