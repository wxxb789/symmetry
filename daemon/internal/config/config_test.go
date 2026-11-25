package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
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
		{name: "OMP ordinary Linux owner", goos: "linux", goarch: "amd64", harnessKind: RuntimeHarnessOMP, wantErr: true},
		{name: "OmO ordinary Linux owner", goos: "linux", goarch: "amd64", harnessKind: RuntimeHarnessOmO, wantErr: true},
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

func TestDefaultStateDirResolvesPerGOOS(t *testing.T) {
	for _, test := range []struct {
		name    string
		goos    string
		env     map[string]string
		want    string
		wantErr string
	}{
		{name: "linux absolute XDG state home", goos: "linux", env: map[string]string{"XDG_STATE_HOME": "/srv/state", "HOME": "/home/u"}, want: "/srv/state/symmetry"},
		{name: "linux absolute XDG state home without home", goos: "linux", env: map[string]string{"XDG_STATE_HOME": "/srv/state"}, want: "/srv/state/symmetry"},
		{name: "linux trailing separator in XDG state home", goos: "linux", env: map[string]string{"XDG_STATE_HOME": "/srv/state/", "HOME": "/home/u"}, want: "/srv/state/symmetry"},
		{name: "linux relative XDG state home falls back to home", goos: "linux", env: map[string]string{"XDG_STATE_HOME": "state", "HOME": "/home/u"}, want: "/home/u/.local/state/symmetry"},
		{name: "linux empty XDG state home falls back to home", goos: "linux", env: map[string]string{"XDG_STATE_HOME": "", "HOME": "/home/u"}, want: "/home/u/.local/state/symmetry"},
		{name: "linux absolute home", goos: "linux", env: map[string]string{"HOME": "/home/u"}, want: "/home/u/.local/state/symmetry"},
		{name: "linux relative home is refused", goos: "linux", env: map[string]string{"XDG_STATE_HOME": "state", "HOME": "home/u"}, wantErr: "$HOME"},
		{name: "linux blank home is refused", goos: "linux", env: map[string]string{"HOME": "  "}, wantErr: "$HOME"},
		{name: "linux missing home is refused", goos: "linux", wantErr: "$HOME"},
		{name: "darwin absolute home", goos: "darwin", env: map[string]string{"HOME": "/Users/u"}, want: "/Users/u/Library/Application Support/Symmetry"},
		{name: "darwin relative home is refused", goos: "darwin", env: map[string]string{"HOME": "Users/u"}, wantErr: "$HOME"},
		{name: "darwin missing home is refused", goos: "darwin", wantErr: "$HOME"},
		{name: "windows drive-absolute local app data", goos: "windows", env: map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local`}, want: `C:\Users\u\AppData\Local\Symmetry`},
		{name: "windows lowercase drive letter", goos: "windows", env: map[string]string{"LOCALAPPDATA": `c:\Users\u\AppData\Local`}, want: `c:\Users\u\AppData\Local\Symmetry`},
		{name: "windows trailing separator is trimmed", goos: "windows", env: map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local\`}, want: `C:\Users\u\AppData\Local\Symmetry`},
		{name: "windows drive root", goos: "windows", env: map[string]string{"LOCALAPPDATA": `C:\`}, want: `C:\Symmetry`},
		{name: "windows forward-slash drive root", goos: "windows", env: map[string]string{"LOCALAPPDATA": "C:/"}, want: `C:\Symmetry`},
		{name: "windows UNC share", goos: "windows", env: map[string]string{"LOCALAPPDATA": `\\server\share`}, want: `\\server\share\Symmetry`},
		{name: "windows missing local app data is refused", goos: "windows", wantErr: "%LOCALAPPDATA%"},
		{name: "windows blank local app data is refused", goos: "windows", env: map[string]string{"LOCALAPPDATA": "  "}, wantErr: "%LOCALAPPDATA%"},
		{name: "windows lookalike variables are refused", goos: "windows", env: map[string]string{"APPDATA": `C:\Users\u\AppData\Roaming`, "USERPROFILE": `C:\Users\u`}, wantErr: "%LOCALAPPDATA%"},
		{name: "windows relative local app data is refused", goos: "windows", env: map[string]string{"LOCALAPPDATA": `Local\AppData`}, wantErr: "%LOCALAPPDATA%"},
		{name: "windows drive-relative local app data is refused", goos: "windows", env: map[string]string{"LOCALAPPDATA": `C:Local`}, wantErr: "%LOCALAPPDATA%"},
		{name: "windows bare UNC prefix is refused", goos: "windows", env: map[string]string{"LOCALAPPDATA": `\\`}, wantErr: "%LOCALAPPDATA%"},
		{name: "unsupported GOOS is refused", goos: "freebsd", env: map[string]string{"HOME": "/home/u"}, wantErr: "freebsd"},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := defaultStateDir(test.goos, func(key string) string { return test.env[key] })
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("defaultStateDir(%q) error = %v, want one naming %q", test.goos, err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("defaultStateDir(%q) error = %v", test.goos, err)
			}
			if resolved != test.want {
				t.Fatalf("defaultStateDir(%q) = %q, want %q", test.goos, resolved, test.want)
			}
			if test.goos != "windows" && !path.IsAbs(resolved) {
				t.Fatalf("defaultStateDir(%q) = %q, want an absolute path", test.goos, resolved)
			}
		})
	}
}

func TestLoadDefaultsAndRefusesStateDir(t *testing.T) {
	t.Run("explicit state_dir always wins", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("XDG_STATE_HOME", "")
		t.Setenv("LOCALAPPDATA", "")
		value := validConfigObject(t)
		value["state_dir"] = "/var/lib/symmetry"
		contents, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(writeConfig(t, string(contents)))
		if err != nil {
			t.Fatalf("Load() explicit state_dir error = %v", err)
		}
		if loaded.StateDir != "/var/lib/symmetry" {
			t.Fatalf("Load() state_dir = %q, want %q", loaded.StateDir, "/var/lib/symmetry")
		}
	})

	t.Run("absent state_dir resolves the host default", func(t *testing.T) {
		want := hostDefaultStateDir(t, t.TempDir())
		value := validConfigObject(t)
		delete(value, "state_dir")
		contents, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(writeConfig(t, string(contents)))
		if err != nil {
			t.Fatalf("Load() absent state_dir error = %v", err)
		}
		if loaded.StateDir != want {
			t.Fatalf("Load() state_dir = %q, want the host default %q", loaded.StateDir, want)
		}
	})

	t.Run("absent state_dir without a platform environment fails closed", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("XDG_STATE_HOME", "")
		t.Setenv("LOCALAPPDATA", "")
		value := validConfigObject(t)
		delete(value, "state_dir")
		contents, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Load(writeConfig(t, string(contents)))
		wantVar := map[string]string{"linux": "$HOME", "darwin": "$HOME", "windows": "%LOCALAPPDATA%"}[runtime.GOOS]
		if wantVar == "" {
			t.Skipf("no per-user state_dir default on %s", runtime.GOOS)
		}
		if err == nil || !strings.HasPrefix(err.Error(), "resolve default state_dir: ") || !strings.Contains(err.Error(), wantVar) {
			t.Fatalf("Load() error = %v, want a resolve default state_dir failure naming %s", err, wantVar)
		}
	})
}

func TestLoadInstalledRequiresAnExplicitStateDir(t *testing.T) {
	t.Run("explicit state_dir loads", func(t *testing.T) {
		value := validConfigObject(t)
		value["state_dir"] = "/var/lib/symmetry"
		contents, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadInstalled(writeConfig(t, string(contents)))
		if err != nil {
			t.Fatalf("LoadInstalled() explicit state_dir error = %v", err)
		}
		if loaded.StateDir != "/var/lib/symmetry" {
			t.Fatalf("LoadInstalled() state_dir = %q, want %q", loaded.StateDir, "/var/lib/symmetry")
		}
	})

	for _, test := range []struct {
		name string
		set  func(map[string]any)
		want string
	}{
		{name: "absent state_dir", set: func(value map[string]any) { delete(value, "state_dir") }, want: "state_dir must be explicitly configured for the installed owner path"},
		{name: "empty state_dir", set: func(value map[string]any) { value["state_dir"] = "" }, want: "state_dir must be explicitly configured for the installed owner path"},
		{name: "whitespace stays explicit", set: func(value map[string]any) { value["state_dir"] = " " }, want: "state_dir must not be empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("HOME", t.TempDir())
			t.Setenv("LOCALAPPDATA", t.TempDir())
			value := validConfigObject(t)
			test.set(value)
			contents, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			_, err = LoadInstalled(writeConfig(t, string(contents)))
			if err == nil || err.Error() != test.want {
				t.Fatalf("LoadInstalled() error = %v, want %q", err, test.want)
			}
		})
	}
}

func hostDefaultStateDir(t *testing.T, base string) string {
	t.Helper()
	switch runtime.GOOS {
	case "linux":
		t.Setenv("XDG_STATE_HOME", base)
		return path.Join(base, "symmetry")
	case "darwin":
		t.Setenv("HOME", base)
		return path.Join(base, "Library/Application Support/Symmetry")
	case "windows":
		t.Setenv("LOCALAPPDATA", base)
		return base + `\Symmetry`
	default:
		t.Skipf("no per-user state_dir default on %s", runtime.GOOS)
		return ""
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

// Every runtime.repository_resource_id rejection class folds into the canonical-UUID body,
// so every rejection row pins that one string.
func TestValidateCanonicalUUIDPinsRejectionMessages(t *testing.T) {
	const field = "runtime.repository_resource_id"
	const uuid = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	canonical := fmt.Sprintf("%s must be a canonical UUID", field)

	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "valid", value: uuid},
		{name: "too short", value: uuid[:35], want: canonical},
		{name: "36 bytes 35 runes", value: uuid[:15] + "é" + uuid[17:], want: canonical},
		{name: "misplaced dash", value: uuid[:8] + "0" + uuid[9:], want: canonical},
		{name: "uppercase hex digit", value: uuid[:15] + "A" + uuid[16:], want: canonical},
		{name: "non-hex lowercase", value: uuid[:16] + "g" + uuid[17:], want: canonical},
		{name: "non-hex uppercase outside A-F", value: uuid[:16] + "Z" + uuid[17:], want: canonical},
		{name: "unsupported version", value: uuid[:14] + "0" + uuid[15:], want: canonical},
		{name: "version six above the supported range", value: uuid[:14] + "6" + uuid[15:], want: canonical},
		{name: "unsupported variant", value: uuid[:19] + "0" + uuid[20:], want: canonical},
		{name: "variant c above the supported range", value: uuid[:19] + "c" + uuid[20:], want: canonical},
		{name: "version and variant both unsupported", value: uuid[:14] + "0" + uuid[15:19] + "0" + uuid[20:], want: canonical},
		{name: "length beats uppercase", value: uuid[:15] + "A" + uuid[16:35], want: canonical},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateCanonicalUUID(test.value, field)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validateCanonicalUUID(%q, %q) error = %v, want accept", test.value, field, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateCanonicalUUID(%q, %q) accepted, want %q", test.value, field, test.want)
			}
			if got := err.Error(); got != test.want {
				t.Fatalf("validateCanonicalUUID(%q, %q) error = %q, want %q", test.value, field, got, test.want)
			}
		})
	}
}

// The length check precedes the identifier scan, so an over-length
// agent_profiles.builder.native_model value pins the length body.
func TestValidateNativeModelPinsRejectionMessages(t *testing.T) {
	const field = "agent_profiles.builder.native_model"
	length := fmt.Sprintf("%s must contain 1..128 characters", field)
	identifier := fmt.Sprintf("%s has an invalid identifier", field)

	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty", value: "", want: length},
		{name: "one character", value: "a"},
		{name: "127 characters", value: strings.Repeat("a", 127)},
		{name: "128 characters", value: strings.Repeat("a", 128)},
		{name: "129 characters", value: strings.Repeat("a", 129), want: length},
		{name: "leading dot", value: ".goal", want: identifier},
		{name: "leading hyphen", value: "-goal", want: identifier},
		{name: "leading digit", value: "1goal"},
		{name: "slash separator", value: "goal/read", want: identifier},
		{name: "space separator", value: "goal read", want: identifier},
		{name: "non-ASCII first character", value: "\u00e9goal", want: identifier},
		{name: "non-ASCII later character", value: "goal\u00e9read", want: identifier},
		{name: "dot separator", value: "goal.read"},
		{name: "hyphen separator", value: "goal-read"},
		{name: "underscore separator", value: "goal_read"},
		{name: "colon separator", value: "goal:read"},
		{name: "mixed separators", value: "gpt-5.2-claude_code:x"},
		{name: "length beats leading dot", value: strings.Repeat("a", 128) + ".", want: length},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateNativeModel(test.value, field)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validateNativeModel(%q, %q) error = %v, want accept", test.value, field, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateNativeModel(%q, %q) accepted, want %q", test.value, field, test.want)
			}
			if got := err.Error(); got != test.want {
				t.Fatalf("validateNativeModel(%q, %q) error = %q, want %q", test.value, field, got, test.want)
			}
		})
	}
}
