package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyRuntimeDefaultsGenericAdapterMetadata(t *testing.T) {
	actual, err := Load(writeConfig(t, validConfig(t, `"https://control.example.test/api"`)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if actual.Runtime.HarnessKind != RuntimeHarnessGeneric || actual.Runtime.HarnessVersion != defaultHarnessVersion ||
		actual.Runtime.AdapterVersion != defaultAdapterVersion || actual.Runtime.AdapterProtocolVersion != defaultAdapterProtocolVersion {
		t.Fatalf("runtime metadata = %+v, want generic legacy defaults", actual.Runtime)
	}
}

func TestRuntimeAcceptsCompleteNativeAdapterMetadata(t *testing.T) {
	value := validConfigObject(t)
	runtime := value["runtime"].(map[string]any)
	runtime["harness_kind"] = RuntimeHarnessCodex
	runtime["harness_version"] = "0.153.4"
	runtime["adapter_version"] = "symmetry-daemon:test"
	runtime["adapter_protocol_version"] = 1
	runtime["repository_resource_id"] = "00000000-0000-4000-8000-000000000001"
	profile := value["agent_profiles"].(map[string]any)["default"].(map[string]any)
	profile["native_model"] = "gpt-6-astra"
	profile["native_model_provider"] = "openai"
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
	actual, err := Load(writeConfig(t, string(contents)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if actual.Runtime.HarnessKind != RuntimeHarnessCodex || actual.Runtime.HarnessVersion != "0.153.4" ||
		actual.Runtime.AdapterVersion != "symmetry-daemon:test" || actual.Runtime.AdapterProtocolVersion != 1 {
		t.Fatalf("runtime metadata = %+v", actual.Runtime)
	}
	if actual.AgentProfiles["default"].NativeModel != "gpt-6-astra" {
		t.Fatalf("native model = %q, want gpt-6-astra", actual.AgentProfiles["default"].NativeModel)
	}
	if actual.AgentProfiles["default"].NativeModelProvider != "openai" {
		t.Fatalf("native model provider = %q, want openai", actual.AgentProfiles["default"].NativeModelProvider)
	}
}

func TestCodexRuntimeRequiresCanonicalNativeModel(t *testing.T) {
	tests := map[string]any{
		"missing":    nil,
		"empty":      "",
		"whitespace": " gpt-6-astra",
		"invalid":    "gpt 6 astra",
	}
	for name, nativeModel := range tests {
		t.Run(name, func(t *testing.T) {
			value := validConfigObject(t)
			runtime := value["runtime"].(map[string]any)
			runtime["harness_kind"] = RuntimeHarnessCodex
			runtime["harness_version"] = "0.153.4"
			runtime["adapter_version"] = "symmetry-daemon:test"
			runtime["adapter_protocol_version"] = 1
			runtime["repository_resource_id"] = "00000000-0000-4000-8000-000000000001"
			profile := value["agent_profiles"].(map[string]any)["default"].(map[string]any)
			profile["native_model_provider"] = "openai"
			if nativeModel != nil {
				profile["native_model"] = nativeModel
			}
			value["workspaces"].(map[string]any)["primary"] = map[string]any{
				"policy": "git_worktree", "repository": t.TempDir(), "root": t.TempDir(), "ref": "HEAD", "cleanup": "always",
			}
			contents, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Load(writeConfig(t, string(contents)))
			if err == nil || !strings.Contains(err.Error(), "agent_profiles.default.native_model") {
				t.Fatalf("Load() error = %v, want native model validation", err)
			}
		})
	}
}

func TestCodexRuntimeRequiresCanonicalNativeModelProvider(t *testing.T) {
	tests := map[string]any{
		"missing":    nil,
		"empty":      "",
		"whitespace": " openai",
		"invalid":    "open ai",
	}
	for name, nativeProvider := range tests {
		t.Run(name, func(t *testing.T) {
			value := validConfigObject(t)
			runtime := value["runtime"].(map[string]any)
			runtime["harness_kind"] = RuntimeHarnessCodex
			runtime["harness_version"] = "0.153.4"
			runtime["adapter_version"] = "symmetry-daemon:test"
			runtime["adapter_protocol_version"] = 1
			runtime["repository_resource_id"] = "00000000-0000-4000-8000-000000000001"
			profile := value["agent_profiles"].(map[string]any)["default"].(map[string]any)
			profile["native_model"] = "gpt-6-astra"
			if nativeProvider != nil {
				profile["native_model_provider"] = nativeProvider
			}
			value["workspaces"].(map[string]any)["primary"] = map[string]any{
				"policy": "git_worktree", "repository": t.TempDir(), "root": t.TempDir(), "ref": "HEAD", "cleanup": "always",
			}
			contents, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Load(writeConfig(t, string(contents)))
			if err == nil || !strings.Contains(err.Error(), "agent_profiles.default.native_model_provider") {
				t.Fatalf("Load() error = %v, want native model provider validation", err)
			}
		})
	}
}

func TestUnavailableNativeRuntimeMayOmitNativeModel(t *testing.T) {
	for _, harnessKind := range []string{RuntimeHarnessClaudeCode, RuntimeHarnessPi, RuntimeHarnessOpenCode} {
		t.Run(harnessKind, func(t *testing.T) {
			value := validConfigObject(t)
			runtime := value["runtime"].(map[string]any)
			runtime["harness_kind"] = harnessKind
			runtime["harness_version"] = "unavailable"
			runtime["adapter_version"] = "symmetry-daemon:unavailable"
			runtime["adapter_protocol_version"] = 1
			runtime["repository_resource_id"] = "00000000-0000-4000-8000-000000000001"
			value["workspaces"].(map[string]any)["primary"] = map[string]any{
				"policy": "git_worktree", "repository": t.TempDir(), "root": t.TempDir(), "ref": "HEAD", "cleanup": "always",
			}
			contents, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Load(writeConfig(t, string(contents))); err != nil {
				t.Fatalf("Load() error = %v, want unavailable native runtime compatibility", err)
			}
		})
	}
}

func TestNativeRuntimeRequiresGitWorktree(t *testing.T) {
	value := validConfigObject(t)
	runtime := value["runtime"].(map[string]any)
	runtime["harness_kind"] = RuntimeHarnessCodex
	runtime["harness_version"] = "0.153.4"
	runtime["adapter_version"] = "symmetry-daemon:test"
	runtime["adapter_protocol_version"] = 1
	runtime["repository_resource_id"] = "00000000-0000-4000-8000-000000000001"
	profile := value["agent_profiles"].(map[string]any)["default"].(map[string]any)
	profile["native_model"] = "gpt-6-astra"
	profile["native_model_provider"] = "openai"
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Load(writeConfig(t, string(contents)))
	if err == nil || !strings.Contains(err.Error(), "runtime.workspace must use git_worktree") {
		t.Fatalf("Load() error = %v, want native workspace policy validation", err)
	}
}

func TestNativeRuntimeRequiresCanonicalRepositoryResourceID(t *testing.T) {
	for name, resourceID := range map[string]any{
		"missing":   "",
		"invalid":   "not-a-uuid",
		"uppercase": "00000000-0000-4000-8000-00000000000A",
	} {
		t.Run(name, func(t *testing.T) {
			value := validConfigObject(t)
			runtime := value["runtime"].(map[string]any)
			runtime["harness_kind"] = RuntimeHarnessCodex
			runtime["harness_version"] = "0.153.4"
			runtime["adapter_version"] = "symmetry-daemon:test"
			runtime["adapter_protocol_version"] = 1
			profile := value["agent_profiles"].(map[string]any)["default"].(map[string]any)
			profile["native_model"] = "gpt-6-astra"
			profile["native_model_provider"] = "openai"
			if resourceID != "" {
				runtime["repository_resource_id"] = resourceID
			}
			contents, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Load(writeConfig(t, string(contents)))
			if err == nil || !strings.Contains(err.Error(), "runtime.repository_resource_id") {
				t.Fatalf("Load() error = %v, want repository resource ID validation", err)
			}
		})
	}
}

func TestRuntimeRejectsPartialOrUnknownAdapterMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		field  string
	}{
		{
			name: "partial native metadata",
			mutate: func(value map[string]any) {
				value["runtime"].(map[string]any)["harness_kind"] = RuntimeHarnessCodex
			},
			field: "runtime adapter metadata",
		},
		{
			name: "unknown harness",
			mutate: func(value map[string]any) {
				runtime := value["runtime"].(map[string]any)
				runtime["harness_kind"] = "other"
				runtime["harness_version"] = "1"
				runtime["adapter_version"] = "1"
				runtime["adapter_protocol_version"] = 1
			},
			field: "runtime.harness_kind",
		},
		{
			name: "zero protocol",
			mutate: func(value map[string]any) {
				runtime := value["runtime"].(map[string]any)
				runtime["harness_kind"] = RuntimeHarnessGeneric
				runtime["harness_version"] = "legacy"
				runtime["adapter_version"] = "legacy"
				runtime["adapter_protocol_version"] = 0
			},
			field: "runtime adapter metadata",
		},
		{
			name: "native legacy versions",
			mutate: func(value map[string]any) {
				runtime := value["runtime"].(map[string]any)
				runtime["harness_kind"] = RuntimeHarnessCodex
				runtime["harness_version"] = "legacy"
				runtime["adapter_version"] = "legacy"
				runtime["adapter_protocol_version"] = 1
			},
			field: "native runtime adapter metadata",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validConfigObject(t)
			test.mutate(value)
			contents, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Load(writeConfig(t, string(contents)))
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Load() error = %v, want %s", err, test.field)
			}
		})
	}
}
