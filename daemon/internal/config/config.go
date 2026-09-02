// Package config loads and validates daemon configuration files.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

// Config is the daemon's local configuration.
type Config struct {
	ControlPlaneURL string  `json:"control_plane_url"`
	StateDir        string  `json:"state_dir"`
	MachineName     string  `json:"machine_name"`
	Runtime         Runtime `json:"runtime"`
}

// Runtime declares the execution environment available on this machine.
type Runtime struct {
	RuntimeKey   string `json:"runtime_key"`
	Name         string `json:"name"`
	Capacity     int    `json:"capacity"`
	AgentProfile string `json:"agent_profile"`
	Workspace    string `json:"workspace"`
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
func (value Config) Validate() error {
	parsedURL, err := url.ParseRequestURI(value.ControlPlaneURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" || parsedURL.User != nil ||
		(parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.RawQuery != "" || parsedURL.ForceQuery || parsedURL.Fragment != "" || strings.Contains(value.ControlPlaneURL, "#") {
		return fmt.Errorf("control_plane_url must be an absolute http or https URL without user credentials, query, or fragment")
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
	return nil
}

func requireNotEmpty(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	return nil
}
