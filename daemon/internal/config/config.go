// Package config loads and validates daemon configuration files.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultCleanupTimeoutMS int64 = 120000
	minimumCleanupTimeoutMS int64 = 10000
	maximumCleanupTimeoutMS int64 = 600000
)

// Config is the daemon's local configuration.
type Config struct {
	ControlPlaneURL   string                  `json:"control_plane_url"`
	AllowInsecureHTTP bool                    `json:"allow_insecure_http"`
	StateDir          string                  `json:"state_dir"`
	MachineName       string                  `json:"machine_name"`
	AgentProfiles     map[string]AgentProfile `json:"agent_profiles"`
	Workspaces        map[string]Workspace    `json:"workspaces"`
	Runtime           Runtime                 `json:"runtime"`
	CleanupTimeoutMS  int64                   `json:"cleanup_timeout_ms"`
}

// Runtime declares the execution environment available on this machine.
type Runtime struct {
	RuntimeKey   string `json:"runtime_key"`
	Name         string `json:"name"`
	Capacity     int    `json:"capacity"`
	AgentProfile string `json:"agent_profile"`
	Workspace    string `json:"workspace"`
}

// InputMode specifies the local CLI input representation.
type InputMode string

const (
	// InputModeGoal writes only the task goal to the configured agent process.
	InputModeGoal InputMode = "goal"
	// InputModeJSON writes structured JSON to the configured agent process.
	InputModeJSON InputMode = "json"
)

// AgentProfile is a machine-local coding agent command binding.
type AgentProfile struct {
	Command        string      `json:"command"`
	Args           []string    `json:"args"`
	InputMode      InputMode   `json:"input_mode"`
	ProviderAccess bool        `json:"provider_access"`
	Interactive    bool        `json:"interactive"`
	EventFormat    EventFormat `json:"event_format"`
	EnvAllowlist   []string    `json:"env_allowlist"`
}

// EventFormat specifies how the local agent represents events on stdout.
type EventFormat string

const (
	// EventFormatRaw forwards stdout chunks as opaque base64 data.
	EventFormatRaw EventFormat = "raw"
	// EventFormatJSONL decodes one JSON object per stdout line when possible.
	EventFormatJSONL EventFormat = "jsonl"
)

// WorkspacePolicy selects how a run obtains its local working directory.
type WorkspacePolicy string

const (
	// WorkspacePolicyExistingCheckout uses an existing, machine-local checkout.
	WorkspacePolicyExistingCheckout WorkspacePolicy = "existing_checkout"
	// WorkspacePolicyGitWorktree creates a detached git worktree for each run.
	WorkspacePolicyGitWorktree WorkspacePolicy = "git_worktree"
)

// CleanupPolicy selects when a daemon-owned worktree is removed.
type CleanupPolicy string

const (
	// CleanupAlways removes a daemon-owned worktree after every completed run.
	CleanupAlways CleanupPolicy = "always"
	// CleanupOnSuccess removes a daemon-owned worktree only after success.
	CleanupOnSuccess CleanupPolicy = "on_success"
	// CleanupNever preserves a daemon-owned worktree.
	CleanupNever CleanupPolicy = "never"
)

// Workspace is a machine-local workspace binding. Fields are conditional on Policy.
type Workspace struct {
	Policy     WorkspacePolicy `json:"policy"`
	Path       string          `json:"path"`
	Repository string          `json:"repository"`
	Root       string          `json:"root"`
	Ref        string          `json:"ref"`
	Cleanup    CleanupPolicy   `json:"cleanup"`
}

// Load decodes a single, strictly-specified JSON configuration file and
// validates the values required to start the daemon.
func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	var value Config
	if err := decoder.Decode(&value); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("decode config: configuration must contain one JSON object")
		}
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	if err := value.Validate(); err != nil {
		return Config{}, err
	}
	return value, nil
}

// Validate verifies that the daemon has enough information for a local start.
func (value *Config) Validate() error {
	if value.CleanupTimeoutMS == 0 {
		value.CleanupTimeoutMS = defaultCleanupTimeoutMS
	}
	if value.CleanupTimeoutMS < minimumCleanupTimeoutMS || value.CleanupTimeoutMS > maximumCleanupTimeoutMS {
		return fmt.Errorf("cleanup_timeout_ms must be between %d and %d", minimumCleanupTimeoutMS, maximumCleanupTimeoutMS)
	}
	parsedURL, err := url.ParseRequestURI(value.ControlPlaneURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" || parsedURL.User != nil ||
		(parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.RawQuery != "" || parsedURL.ForceQuery || parsedURL.Fragment != "" || strings.Contains(value.ControlPlaneURL, "#") {
		return fmt.Errorf("control_plane_url must be an absolute http or https URL without user credentials, query, or fragment")
	}
	if parsedURL.Scheme == "http" && !value.AllowInsecureHTTP {
		return fmt.Errorf("allow_insecure_http must be true for a plain HTTP control_plane_url")
	}

	if err := requireNotEmpty("state_dir", value.StateDir); err != nil {
		return err
	}
	if err := requireNotEmpty("machine_name", value.MachineName); err != nil {
		return err
	}
	if err := requireNotEmpty("runtime.runtime_key", value.Runtime.RuntimeKey); err != nil {
		return err
	}
	if err := requireNotEmpty("runtime.name", value.Runtime.Name); err != nil {
		return err
	}
	if err := requireNotEmpty("runtime.agent_profile", value.Runtime.AgentProfile); err != nil {
		return err
	}
	if err := requireNotEmpty("runtime.workspace", value.Runtime.Workspace); err != nil {
		return err
	}

	if value.Runtime.Capacity <= 0 {
		return fmt.Errorf("runtime.capacity must be greater than zero")
	}

	if err := validateAgentProfiles(value.AgentProfiles); err != nil {
		return err
	}
	if err := validateWorkspaces(value.Workspaces); err != nil {
		return err
	}
	if _, ok := value.AgentProfiles[value.Runtime.AgentProfile]; !ok {
		return fmt.Errorf("runtime.agent_profile %q is not configured", value.Runtime.AgentProfile)
	}
	workspace, ok := value.Workspaces[value.Runtime.Workspace]
	if !ok {
		return fmt.Errorf("runtime.workspace %q is not configured", value.Runtime.Workspace)
	}
	if workspace.Policy == WorkspacePolicyExistingCheckout && value.Runtime.Capacity > 1 {
		return fmt.Errorf("runtime.capacity must be 1 when runtime.workspace uses existing_checkout")
	}
	return nil
}

// CleanupTimeout returns the bounded deadline used for one workspace cleanup.
func (value Config) CleanupTimeout() time.Duration {
	timeout := value.CleanupTimeoutMS
	if timeout == 0 {
		timeout = defaultCleanupTimeoutMS
	}
	return time.Duration(timeout) * time.Millisecond
}

var bindingKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validateAgentProfiles(profiles map[string]AgentProfile) error {
	for key, profile := range profiles {
		field := "agent_profiles." + key
		if err := requireBindingKey(field, key); err != nil {
			return err
		}
		if err := requireNotEmpty(field+".command", profile.Command); err != nil {
			return err
		}
		switch profile.InputMode {
		case InputModeGoal, InputModeJSON:
		default:
			return fmt.Errorf("%s.input_mode must be goal or json", field)
		}
		if profile.Interactive && profile.InputMode == InputModeGoal {
			return fmt.Errorf("%s.interactive requires %s.input_mode to be json", field, field)
		}
		if profile.ProviderAccess && profile.InputMode != InputModeJSON {
			return fmt.Errorf("%s.provider_access requires %s.input_mode to be json", field, field)
		}
		switch profile.EventFormat {
		case "", EventFormatRaw:
			profile.EventFormat = EventFormatRaw
		case EventFormatJSONL:
		default:
			return fmt.Errorf("%s.event_format must be raw or jsonl", field)
		}
		seen := make(map[string]struct{}, len(profile.EnvAllowlist))
		for _, name := range profile.EnvAllowlist {
			if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "=\x00\r\n") {
				return fmt.Errorf("%s.env_allowlist contains an invalid environment variable name", field)
			}
			if _, duplicate := seen[name]; duplicate {
				return fmt.Errorf("%s.env_allowlist contains duplicate environment variable %q", field, name)
			}
			seen[name] = struct{}{}
		}
	}
	return nil
}

func validateWorkspaces(workspaces map[string]Workspace) error {
	for key, workspace := range workspaces {
		field := "workspaces." + key
		if err := requireBindingKey(field, key); err != nil {
			return err
		}
		switch workspace.Cleanup {
		case CleanupAlways, CleanupOnSuccess, CleanupNever:
		default:
			return fmt.Errorf("%s.cleanup must be always, on_success, or never", field)
		}

		switch workspace.Policy {
		case WorkspacePolicyExistingCheckout:
			if err := requireAbsolutePath(field+".path", workspace.Path); err != nil {
				return err
			}
		case WorkspacePolicyGitWorktree:
			if err := requireAbsolutePath(field+".repository", workspace.Repository); err != nil {
				return err
			}
			if err := requireAbsolutePath(field+".root", workspace.Root); err != nil {
				return err
			}
			if err := requireNotEmpty(field+".ref", workspace.Ref); err != nil {
				return err
			}
			if strings.HasPrefix(strings.TrimSpace(workspace.Ref), "-") || strings.ContainsAny(workspace.Ref, "\x00\r\n") {
				return fmt.Errorf("%s.ref is invalid", field)
			}
		default:
			return fmt.Errorf("%s.policy must be existing_checkout or git_worktree", field)
		}
	}
	return nil
}

func requireNotEmpty(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	return nil
}

func requireBindingKey(field, value string) error {
	if !bindingKeyPattern.MatchString(value) {
		return fmt.Errorf("%s must be a non-path binding key", field)
	}
	return nil
}

func requireAbsolutePath(field, value string) error {
	if err := requireNotEmpty(field, value); err != nil {
		return err
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("%s must be an absolute path", field)
	}
	if filepath.Clean(value) == "." {
		return fmt.Errorf("%s must be an absolute path", field)
	}
	return nil
}
