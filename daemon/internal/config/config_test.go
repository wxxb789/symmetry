package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLoadAcceptsValidConfiguration(t *testing.T) {
	path := writeConfig(t, validConfig(t, `"https://control.example.test/api"`))

	actual, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if actual.ControlPlaneURL != "https://control.example.test/api" {
		t.Errorf("ControlPlaneURL = %q", actual.ControlPlaneURL)
	}
	if actual.Runtime.Capacity != 4 {
		t.Errorf("Runtime.Capacity = %d", actual.Runtime.Capacity)
	}
	if actual.AgentProfiles["default"].InputMode != InputModeGoal {
		t.Errorf("AgentProfiles[default].InputMode = %q", actual.AgentProfiles["default"].InputMode)
	}
	if actual.Workspaces["primary"].Policy != WorkspacePolicyExistingCheckout {
		t.Errorf("Workspaces[primary].Policy = %q", actual.Workspaces["primary"].Policy)
	}
}

func TestLoadRejectsUnknownRuntimeBindings(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		field    string
	}{
		{
			name:     "agent profile",
			contents: strings.Replace(validConfig(t, `"https://control.example.test"`), `"agent_profile": "default"`, `"agent_profile": "missing"`, 1),
			field:    "runtime.agent_profile",
		},
		{
			name:     "workspace",
			contents: strings.Replace(validConfig(t, `"https://control.example.test"`), `"workspace": "primary"`, `"workspace": "missing"`, 1),
			field:    "runtime.workspace",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, test.contents))
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Load() error = %v, want %s validation error", err, test.field)
			}
		})
	}
}

func TestLoadRejectsInvalidLocalBindings(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(map[string]any)
		wantField string
	}{
		{
			name: "empty command",
			mutate: func(value map[string]any) {
				value["agent_profiles"].(map[string]any)["default"].(map[string]any)["command"] = " "
			},
			wantField: "agent_profiles.default.command",
		},
		{
			name: "unknown input mode",
			mutate: func(value map[string]any) {
				value["agent_profiles"].(map[string]any)["default"].(map[string]any)["input_mode"] = "shell"
			},
			wantField: "agent_profiles.default.input_mode",
		},
		{
			name: "unknown event format",
			mutate: func(value map[string]any) {
				value["agent_profiles"].(map[string]any)["default"].(map[string]any)["event_format"] = "shell"
			},
			wantField: "agent_profiles.default.event_format",
		},
		{
			name: "relative existing checkout path",
			mutate: func(value map[string]any) {
				value["workspaces"].(map[string]any)["primary"].(map[string]any)["path"] = "relative/path"
			},
			wantField: "workspaces.primary.path",
		},
		{
			name: "unknown workspace policy",
			mutate: func(value map[string]any) {
				value["workspaces"].(map[string]any)["primary"].(map[string]any)["policy"] = "unsafe"
			},
			wantField: "workspaces.primary.policy",
		},
		{
			name: "relative worktree root",
			mutate: func(value map[string]any) {
				value["workspaces"].(map[string]any)["primary"] = map[string]any{
					"policy": "git_worktree", "repository": t.TempDir(), "root": "relative/root", "ref": "HEAD", "cleanup": "always",
				}
			},
			wantField: "workspaces.primary.root",
		},
		{
			name: "missing worktree ref",
			mutate: func(value map[string]any) {
				value["workspaces"].(map[string]any)["primary"] = map[string]any{
					"policy": "git_worktree", "repository": t.TempDir(), "root": t.TempDir(), "ref": "", "cleanup": "always",
				}
			},
			wantField: "workspaces.primary.ref",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal([]byte(validConfig(t, `"https://control.example.test"`)), &value); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			test.mutate(value)
			contents, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			_, err = Load(writeConfig(t, string(contents)))
			if err == nil || !strings.Contains(err.Error(), test.wantField) {
				t.Fatalf("Load() error = %v, want %s validation error", err, test.wantField)
			}
		})
	}
}

func TestLoadRejectsInvalidControlPlaneURL(t *testing.T) {
	for _, controlPlaneURL := range []string{`"not a url"`, `"https://control.example.test/api?debug=1"`, `"https://control.example.test/api#fragment"`} {
		_, err := Load(writeConfig(t, validConfig(t, controlPlaneURL)))
		if err == nil || !strings.Contains(err.Error(), "control_plane_url") {
			t.Fatalf("Load(%s) error = %v, want control_plane_url validation error", controlPlaneURL, err)
		}
	}
}

func TestLoadRequiresExplicitOptInForPlainHTTP(t *testing.T) {
	contents := validConfig(t, `"http://control.example.test"`)
	if _, err := Load(writeConfig(t, contents)); err == nil || !strings.Contains(err.Error(), "allow_insecure_http") {
		t.Fatalf("Load() error = %v, want explicit insecure HTTP opt-in", err)
	}

	var value map[string]any
	if err := json.Unmarshal([]byte(contents), &value); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	value["allow_insecure_http"] = true
	allowed, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if _, err := Load(writeConfig(t, string(allowed))); err != nil {
		t.Fatalf("Load() with explicit opt-in error = %v", err)
	}
}

func TestLoadRejectsZeroRuntimeCapacity(t *testing.T) {
	path := writeConfig(t, validConfigWithCapacity(t, 0))

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "runtime.capacity") {
		t.Fatalf("Load() error = %v, want runtime.capacity validation error", err)
	}
}

func TestLoadRejectsEmptyRequiredValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		field  string
	}{
		{
			name:   "state directory",
			mutate: func(value map[string]any) { value["state_dir"] = " " },
			field:  "state_dir",
		},
		{
			name:   "machine name",
			mutate: func(value map[string]any) { value["machine_name"] = "" },
			field:  "machine_name",
		},
		{
			name:   "runtime key",
			mutate: func(value map[string]any) { value["runtime"].(map[string]any)["runtime_key"] = "" },
			field:  "runtime.runtime_key",
		},
		{
			name:   "runtime name",
			mutate: func(value map[string]any) { value["runtime"].(map[string]any)["name"] = "" },
			field:  "runtime.name",
		},
		{
			name:   "agent profile",
			mutate: func(value map[string]any) { value["runtime"].(map[string]any)["agent_profile"] = "" },
			field:  "runtime.agent_profile",
		},
		{
			name:   "workspace",
			mutate: func(value map[string]any) { value["runtime"].(map[string]any)["workspace"] = "" },
			field:  "runtime.workspace",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validConfigObject(t)
			test.mutate(value)
			contents, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			_, err = Load(writeConfig(t, string(contents)))
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Load() error = %v, want %s validation error", err, test.field)
			}
		})
	}
}

func TestLoadRejectsUnknownJSONFields(t *testing.T) {
	value := validConfigObject(t)
	value["runtime"].(map[string]any)["unexpected"] = true
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	path := writeConfig(t, string(contents))

	_, err = Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Load() error = %v, want unknown field error", err)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "daemon.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func validConfig(t *testing.T, controlPlaneURL string) string {
	t.Helper()
	return `{
  "control_plane_url": ` + controlPlaneURL + `,
  "state_dir": ".symmetry/state",
  "machine_name": "builder-01",
  "agent_profiles": {
    "default": {
      "command": "codex",
      "args": [],
      "input_mode": "goal",
      "env_allowlist": []
    }
  },
  "workspaces": {
    "primary": {
      "policy": "existing_checkout",
      "path": ` + strconv.Quote(t.TempDir()) + `,
      "cleanup": "never"
    }
  },
  "runtime": {
    "runtime_key": "default",
    "name": "docker",
    "capacity": 4,
    "agent_profile": "default",
    "workspace": "primary"
  }
}`
}

func validConfigWithCapacity(t *testing.T, capacity int) string {
	return strings.Replace(validConfig(t, `"https://control.example.test"`), `"capacity": 4`, `"capacity": `+strconv.Itoa(capacity), 1)
}

func validConfigObject(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(validConfig(t, `"https://control.example.test"`)), &value); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return value
}
