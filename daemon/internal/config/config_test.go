package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	if actual.Runtime.Capacity != 1 {
		t.Errorf("Runtime.Capacity = %d", actual.Runtime.Capacity)
	}
	if actual.AgentProfiles["default"].InputMode != InputModeGoal {
		t.Errorf("AgentProfiles[default].InputMode = %q", actual.AgentProfiles["default"].InputMode)
	}
	if actual.AgentProfiles["default"].EventFormat != EventFormatRaw {
		t.Errorf("AgentProfiles[default].EventFormat = %q, want %q", actual.AgentProfiles["default"].EventFormat, EventFormatRaw)
	}
	if actual.Workspaces["primary"].Policy != WorkspacePolicyExistingCheckout {
		t.Errorf("Workspaces[primary].Policy = %q", actual.Workspaces["primary"].Policy)
	}
	if actual.CleanupTimeoutMS != defaultCleanupTimeoutMS {
		t.Errorf("CleanupTimeoutMS = %d, want %d", actual.CleanupTimeoutMS, defaultCleanupTimeoutMS)
	}
}

func TestPiProviderExtensionShareConfigValidation(t *testing.T) {
	share := t.TempDir()
	value := validConfigObject(t)
	value["pi_provider_extension_dir"] = share
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(writeConfig(t, string(contents)))
	if err != nil {
		t.Fatalf("Load() with absolute provider extension share error = %v", err)
	}
	if loaded.PiProviderExtensionDir != share {
		t.Fatalf("PiProviderExtensionDir = %q, want %q", loaded.PiProviderExtensionDir, share)
	}

	value["pi_provider_extension_dir"] = "relative/share"
	contents, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(writeConfig(t, string(contents))); err == nil || !strings.Contains(err.Error(), "pi_provider_extension_dir") {
		t.Fatalf("Load() with relative provider extension share error = %v, want path validation", err)
	}
}

func TestLinuxPiProviderAccessRequiresExtensionShareConfig(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific provider extension configuration")
	}
	value := validConfigObject(t)
	runtimeConfig := value["runtime"].(map[string]any)
	runtimeConfig["harness_kind"] = RuntimeHarnessPi
	runtimeConfig["harness_version"] = "0.87.1"
	runtimeConfig["adapter_version"] = "symmetry-daemon:test"
	runtimeConfig["adapter_protocol_version"] = 1
	runtimeConfig["repository_resource_id"] = "00000000-0000-4000-8000-000000000001"
	profile := value["agent_profiles"].(map[string]any)["default"].(map[string]any)
	profile["input_mode"] = string(InputModeJSON)
	profile["provider_access"] = true
	value["workspaces"].(map[string]any)["primary"] = map[string]any{
		"policy": WorkspacePolicyGitWorktree, "repository": t.TempDir(), "root": t.TempDir(), "ref": "HEAD", "cleanup": CleanupAlways,
	}
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(writeConfig(t, string(contents))); err == nil || !strings.Contains(err.Error(), "pi_provider_extension_dir") {
		t.Fatalf("Load() without Linux Pi provider share error = %v, want required share", err)
	}

	value["pi_provider_extension_dir"] = t.TempDir()
	contents, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(writeConfig(t, string(contents))); err != nil {
		t.Fatalf("Load() with Linux Pi provider share error = %v", err)
	}
}

func TestLoadNormalizesAndBoundsCleanupTimeout(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		want    int64
		wantErr bool
	}{
		{name: "omitted", want: defaultCleanupTimeoutMS},
		{name: "zero", value: 0, want: defaultCleanupTimeoutMS},
		{name: "minimum", value: minimumCleanupTimeoutMS, want: minimumCleanupTimeoutMS},
		{name: "maximum", value: maximumCleanupTimeoutMS, want: maximumCleanupTimeoutMS},
		{name: "below minimum", value: minimumCleanupTimeoutMS - 1, wantErr: true},
		{name: "above maximum", value: maximumCleanupTimeoutMS + 1, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validConfigObject(t)
			if test.value != nil {
				value["cleanup_timeout_ms"] = test.value
			}
			contents, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := Load(writeConfig(t, string(contents)))
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "cleanup_timeout_ms") {
					t.Fatalf("Load() error = %v, want cleanup_timeout_ms validation error", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if actual.CleanupTimeoutMS != test.want {
				t.Fatalf("CleanupTimeoutMS = %d, want %d", actual.CleanupTimeoutMS, test.want)
			}
		})
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
			name: "interactive goal input mode",
			mutate: func(value map[string]any) {
				profile := value["agent_profiles"].(map[string]any)["default"].(map[string]any)
				profile["interactive"] = true
				profile["input_mode"] = "goal"
			},
			wantField: "agent_profiles.default",
		},
		{
			name: "provider access with goal input mode",
			mutate: func(value map[string]any) {
				profile := value["agent_profiles"].(map[string]any)["default"].(map[string]any)
				profile["provider_access"] = true
				profile["input_mode"] = "goal"
			},
			wantField: "agent_profiles.default",
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

func TestLoadRejectsConcurrentExistingCheckout(t *testing.T) {
	_, err := Load(writeConfig(t, validConfigWithCapacity(t, 2)))
	if err == nil || !strings.Contains(err.Error(), "runtime.capacity") {
		t.Fatalf("Load() error = %v, want existing_checkout capacity validation error", err)
	}
}

func TestLoadAllowsConcurrentGitWorktree(t *testing.T) {
	value := validConfigObject(t)
	value["runtime"].(map[string]any)["capacity"] = 4
	value["workspaces"].(map[string]any)["primary"] = map[string]any{
		"policy":     "git_worktree",
		"repository": t.TempDir(),
		"root":       t.TempDir(),
		"ref":        "HEAD",
		"cleanup":    "always",
	}
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(writeConfig(t, string(contents))); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestGitCloneV2WorkspaceRequiresDisjointCanonicalPaths(t *testing.T) {
	valid := Workspace{
		Policy:     WorkspacePolicyGitCloneV2,
		Repository: filepath.Join(t.TempDir(), "source"),
		Root:       filepath.Join(t.TempDir(), "checkouts"),
		SeedRoot:   filepath.Join(t.TempDir(), "seeds"),
		Cleanup:    CleanupOnSuccess,
	}
	if err := validateWorkspaces(map[string]Workspace{"native": valid}); err != nil {
		t.Fatalf("validateWorkspaces(valid clone binding) error = %v", err)
	}

	tests := []struct {
		name     string
		mutate   func(*Workspace)
		contains string
	}{
		{name: "missing seed root", mutate: func(value *Workspace) { value.SeedRoot = "" }, contains: "seed_root"},
		{name: "relative seed root", mutate: func(value *Workspace) { value.SeedRoot = "seeds" }, contains: "seed_root"},
		{name: "noncanonical root", mutate: func(value *Workspace) {
			value.Root += string(filepath.Separator) + ".." + string(filepath.Separator) + "checkouts"
		}, contains: "root"},
		{name: "source overlaps checkout root", mutate: func(value *Workspace) { value.Root = filepath.Join(value.Repository, "checkouts") }, contains: "disjoint"},
		{name: "seed overlaps source", mutate: func(value *Workspace) { value.SeedRoot = filepath.Join(value.Repository, "seeds") }, contains: "disjoint"},
		{name: "path is forbidden", mutate: func(value *Workspace) { value.Path = valid.Repository }, contains: "path"},
		{name: "ref is forbidden", mutate: func(value *Workspace) { value.Ref = "HEAD" }, contains: "ref"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := validateWorkspaces(map[string]Workspace{"native": value}); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("validateWorkspaces() error = %v, want %q validation", err, test.contains)
			}
		})
	}
}

func TestGitCloneV2RuntimeSelectionIsLinuxAMD64NativeOnly(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		goarch      string
		harnessKind string
		wantErr     bool
	}{
		{name: "Linux amd64 native", goos: "linux", goarch: "amd64", harnessKind: RuntimeHarnessPi},
		{name: "Windows amd64", goos: "windows", goarch: "amd64", harnessKind: RuntimeHarnessPi, wantErr: true},
		{name: "Linux 386", goos: "linux", goarch: "386", harnessKind: RuntimeHarnessPi, wantErr: true},
		{name: "Linux generic", goos: "linux", goarch: "amd64", harnessKind: RuntimeHarnessGeneric, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRuntimeWorkspacePolicy(test.goos, test.goarch, test.harnessKind, WorkspacePolicyGitCloneV2)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateRuntimeWorkspacePolicy() error = %v, wantErr=%t", err, test.wantErr)
			}
		})
	}
	if err := validateRuntimeWorkspacePolicy("windows", "amd64", RuntimeHarnessGeneric, WorkspacePolicyExistingCheckout); err != nil {
		t.Fatalf("legacy Windows checkout policy was rejected: %v", err)
	}
	if err := validateRuntimeWorkspacePolicy("linux", "amd64", RuntimeHarnessPi, WorkspacePolicyGitWorktree); err != nil {
		t.Fatalf("legacy Linux worktree policy was rejected: %v", err)
	}
}

func TestLoadAcceptsLinuxNativeGitCloneV2Binding(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("git_clone_v2 is Linux/amd64 only")
	}
	value := validConfigObject(t)
	runtimeConfig := value["runtime"].(map[string]any)
	runtimeConfig["harness_kind"] = RuntimeHarnessPi
	runtimeConfig["harness_version"] = "0.87.1"
	runtimeConfig["adapter_version"] = "symmetry-daemon:test"
	runtimeConfig["adapter_protocol_version"] = 1
	runtimeConfig["repository_resource_id"] = "00000000-0000-4000-8000-000000000001"
	profile := value["agent_profiles"].(map[string]any)["default"].(map[string]any)
	profile["input_mode"] = string(InputModeJSON)
	value["workspaces"].(map[string]any)["primary"] = map[string]any{
		"policy":     WorkspacePolicyGitCloneV2,
		"repository": filepath.Join(t.TempDir(), "source"),
		"root":       filepath.Join(t.TempDir(), "checkouts"),
		"seed_root":  filepath.Join(t.TempDir(), "seeds"),
		"cleanup":    CleanupAlways,
	}
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(writeConfig(t, string(contents)))
	if err != nil {
		t.Fatalf("Load() clone-v2 config error = %v", err)
	}
	if loaded.Workspaces["primary"].Policy != WorkspacePolicyGitCloneV2 || loaded.Workspaces["primary"].SeedRoot == "" {
		t.Fatalf("loaded clone binding = %#v", loaded.Workspaces["primary"])
	}
}

func TestLegacyWorkspaceJSONDoesNotGainSeedRoot(t *testing.T) {
	encoded, err := json.Marshal(Workspace{Policy: WorkspacePolicyExistingCheckout, Path: filepath.Join(t.TempDir(), "checkout"), Cleanup: CleanupNever})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "seed_root") {
		t.Fatalf("legacy workspace JSON unexpectedly contains seed_root: %s", encoded)
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
    "capacity": 1,
    "agent_profile": "default",
    "workspace": "primary"
  }
}`
}

func validConfigWithCapacity(t *testing.T, capacity int) string {
	return strings.Replace(validConfig(t, `"https://control.example.test"`), `"capacity": 1`, `"capacity": `+strconv.Itoa(capacity), 1)
}

func validConfigObject(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(validConfig(t, `"https://control.example.test"`)), &value); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return value
}
