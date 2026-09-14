package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestThreadStartResponseRequiresWorkspaceOnlyPolicy(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	child := filepath.Join(workspace, "artifacts")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		mutate  func(map[string]any, map[string]any)
		wantErr bool
	}{
		{name: "implicit cwd with no extra roots"},
		{name: "explicit root within workspace", mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["writableRoots"] = []string{child}
		}},
		{name: "missing writable roots", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			delete(sandbox, "writableRoots")
		}},
		{name: "null writable roots", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["writableRoots"] = nil
		}},
		{name: "non-array writable roots", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["writableRoots"] = workspace
		}},
		{name: "non-string writable root", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["writableRoots"] = []any{1}
		}},
		{name: "writable root outside workspace", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["writableRoots"] = []string{outside}
		}},
		{name: "empty writable root", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["writableRoots"] = []string{""}
		}},
		{name: "relative writable root", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["writableRoots"] = []string{"artifacts"}
		}},
		{name: "null writable root element", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["writableRoots"] = []any{nil}
		}},
		{name: "mismatched response cwd", wantErr: true, mutate: func(response map[string]any, _ map[string]any) {
			response["cwd"] = outside
		}},
		{name: "mismatched thread cwd", wantErr: true, mutate: func(response map[string]any, _ map[string]any) {
			response["thread"].(map[string]any)["cwd"] = outside
		}},
		{name: "missing response cwd", wantErr: true, mutate: func(response map[string]any, _ map[string]any) {
			delete(response, "cwd")
		}},
		{name: "missing thread cwd", wantErr: true, mutate: func(response map[string]any, _ map[string]any) {
			delete(response["thread"].(map[string]any), "cwd")
		}},
		{name: "runtime root outside workspace", wantErr: true, mutate: func(response map[string]any, _ map[string]any) {
			response["runtimeWorkspaceRoots"] = []string{outside}
		}},
		{name: "network access enabled", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["networkAccess"] = true
		}},
		{name: "network access absent", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			delete(sandbox, "networkAccess")
		}},
		{name: "network access null", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["networkAccess"] = nil
		}},
		{name: "network access wrong type", wantErr: true, mutate: func(_ map[string]any, sandbox map[string]any) {
			sandbox["networkAccess"] = "false"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := workspaceOnlyThreadResponse(workspace)
			if test.mutate != nil {
				test.mutate(response, response["sandbox"].(map[string]any))
			}
			if err := validateThreadResponseJSON(t, response, workspace); (err != nil) != test.wantErr {
				t.Fatalf("validate thread/start response = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestThreadStartResponseRequiresTemporaryRootExclusions(t *testing.T) {
	workspace := t.TempDir()
	for _, field := range []string{"excludeTmpdirEnvVar", "excludeSlashTmp"} {
		for _, value := range []struct {
			name string
			data any
		}{
			{name: "missing"},
			{name: "null", data: nil},
			{name: "false", data: false},
			{name: "string", data: "true"},
		} {
			t.Run(field+"/"+value.name, func(t *testing.T) {
				response := workspaceOnlyThreadResponse(workspace)
				sandbox := response["sandbox"].(map[string]any)
				// Isolate temporary-root checks from the empty-array case.
				sandbox["writableRoots"] = []string{workspace}
				if value.name == "missing" {
					delete(sandbox, field)
				} else {
					sandbox[field] = value.data
				}
				if err := validateThreadResponseJSON(t, response, workspace); err == nil {
					t.Fatal("thread/start accepted an unverified temporary write root")
				}
			})
		}
	}
}

func TestThreadStartResponseRejectsUnresolvableOrEscapingRoots(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(workspace, "outside-link")
	if runtime.GOOS == "windows" {
		createTestJunction(t, link, outside)
	} else if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create test symlink: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("test link was not created: %v", err)
	}
	linkInfo, err := os.Stat(link)
	if err != nil {
		t.Fatal(err)
	}
	outsideInfo, err := os.Stat(outside)
	if err != nil || !os.SameFile(linkInfo, outsideInfo) {
		t.Fatalf("test link did not point to its external target: %v", err)
	}
	for _, field := range []string{"writableRoots", "runtimeWorkspaceRoots"} {
		for _, candidate := range []struct{ name, path string }{
			{"existing external link", link},
			{"missing child of external link", filepath.Join(link, "missing")},
			{"missing ordinary path", filepath.Join(workspace, "missing")},
		} {
			t.Run(field+"/"+candidate.name, func(t *testing.T) {
				response := workspaceOnlyThreadResponse(workspace)
				if field == "writableRoots" {
					response["sandbox"].(map[string]any)[field] = []string{candidate.path}
				} else {
					response[field] = []string{candidate.path}
				}
				if err := validateThreadResponseJSON(t, response, workspace); err == nil {
					t.Fatal("thread/start accepted an unresolved or escaping writable root")
				}
			})
		}
	}
}

func createTestJunction(t *testing.T, link, target string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "cmd.exe", "/d", "/c", "mklink", "/J", link, target)
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatalf("ConfigureHeadlessProcess() for test junction: %v", err)
	}
	output, err := command.CombinedOutput()
	if err == nil {
		return
	}
	if ctx.Err() != nil {
		t.Fatalf("create test junction timed out: %v", ctx.Err())
	}
	diagnostic := strings.TrimSpace(string(output))
	if diagnostic == "" {
		diagnostic = "<no command output>"
	}
	if junctionCapabilityUnavailable(err, output) {
		t.Skipf("directory junction capability unavailable: %v: %s", err, diagnostic)
	}
	t.Fatalf("create test junction: %v: %s", err, diagnostic)
}

func junctionCapabilityUnavailable(err error, output []byte) bool {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrPermission) {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(string(output)))
	for _, marker := range []string{
		"access is denied",
		"a required privilege is not held",
		"requested operation requires elevation",
		"sufficient privilege",
		"not supported",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	// Some headless Windows environments return only exit status 1 when
	// mklink cannot create a junction, so preserve the skip contract even
	// when cmd.exe provides no diagnostic text.
	var exitErr *exec.ExitError
	return runtime.GOOS == "windows" && message == "" && errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}

func workspaceOnlyThreadResponse(workspace string) map[string]any {
	return map[string]any{
		"thread": map[string]any{"id": "native-thread", "cwd": workspace, "ephemeral": false},
		"cwd":    workspace, "model": "test-model", "modelProvider": "test-provider",
		"approvalPolicy": "on-request", "approvalsReviewer": "user",
		"runtimeWorkspaceRoots": []string{},
		"sandbox": map[string]any{
			"type": "workspaceWrite", "writableRoots": []string{}, "networkAccess": false,
			"excludeTmpdirEnvVar": true, "excludeSlashTmp": true,
		},
	}
}

func validateThreadResponseJSON(t *testing.T, value map[string]any, workspace string) error {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var response threadStartResponse
	if err := json.Unmarshal(encoded, &response); err != nil {
		return err
	}
	return response.validate(workspace)
}
