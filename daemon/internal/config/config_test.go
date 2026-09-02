package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLoadAcceptsValidConfiguration(t *testing.T) {
	path := writeConfig(t, `{
  "control_plane_url": "https://control.example.test/api",
  "state_dir": ".symmetry/state",
  "machine_name": "builder-01",
  "runtime": {
    "name": "docker",
    "capacity": 4,
    "agent_profile": "default",
    "workspace": "Q:/workspaces"
  }
}`)

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
}

func TestLoadRejectsInvalidControlPlaneURL(t *testing.T) {
	path := writeConfig(t, validConfig(`"not a url"`))

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "control_plane_url") {
		t.Fatalf("Load() error = %v, want control_plane_url validation error", err)
	}
}

func TestLoadRejectsZeroRuntimeCapacity(t *testing.T) {
	path := writeConfig(t, validConfigWithCapacity(0))

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "runtime.capacity") {
		t.Fatalf("Load() error = %v, want runtime.capacity validation error", err)
	}
}

func TestLoadRejectsEmptyRequiredValues(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		field    string
	}{
		{
			name: "state directory",
			contents: `{
  "control_plane_url": "https://control.example.test",
  "state_dir": " ",
  "machine_name": "builder-01",
  "runtime": { "name": "docker", "capacity": 4, "agent_profile": "default", "workspace": "Q:/workspaces" }
}`,
			field: "state_dir",
		},
		{
			name: "machine name",
			contents: `{
  "control_plane_url": "https://control.example.test",
  "state_dir": ".symmetry/state",
  "machine_name": "",
  "runtime": { "name": "docker", "capacity": 4, "agent_profile": "default", "workspace": "Q:/workspaces" }
}`,
			field: "machine_name",
		},
		{
			name: "runtime name",
			contents: `{
  "control_plane_url": "https://control.example.test",
  "state_dir": ".symmetry/state",
  "machine_name": "builder-01",
  "runtime": { "name": "", "capacity": 4, "agent_profile": "default", "workspace": "Q:/workspaces" }
}`,
			field: "runtime.name",
		},
		{
			name: "agent profile",
			contents: `{
  "control_plane_url": "https://control.example.test",
  "state_dir": ".symmetry/state",
  "machine_name": "builder-01",
  "runtime": { "name": "docker", "capacity": 4, "agent_profile": "", "workspace": "Q:/workspaces" }
}`,
			field: "runtime.agent_profile",
		},
		{
			name: "workspace",
			contents: `{
  "control_plane_url": "https://control.example.test",
  "state_dir": ".symmetry/state",
  "machine_name": "builder-01",
  "runtime": { "name": "docker", "capacity": 4, "agent_profile": "default", "workspace": "" }
}`,
			field: "runtime.workspace",
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

func TestLoadRejectsUnknownJSONFields(t *testing.T) {
	path := writeConfig(t, `{
  "control_plane_url": "https://control.example.test",
  "state_dir": ".symmetry/state",
  "machine_name": "builder-01",
  "runtime": {
    "name": "docker",
    "capacity": 4,
    "agent_profile": "default",
    "workspace": "Q:/workspaces",
    "unexpected": true
  }
}`)

	_, err := Load(path)
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

func validConfig(controlPlaneURL string) string {
	return `{
  "control_plane_url": ` + controlPlaneURL + `,
  "state_dir": ".symmetry/state",
  "machine_name": "builder-01",
  "runtime": {
    "name": "docker",
    "capacity": 4,
    "agent_profile": "default",
    "workspace": "Q:/workspaces"
  }
}`
}

func validConfigWithCapacity(capacity int) string {
	return `{
  "control_plane_url": "https://control.example.test",
  "state_dir": ".symmetry/state",
  "machine_name": "builder-01",
  "runtime": {
    "name": "docker",
    "capacity": ` + strconv.Itoa(capacity) + `,
    "agent_profile": "default",
    "workspace": "Q:/workspaces"
  }
}`
}
