package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

func TestLinuxV2NativeOwnerWitnessCommandRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS", "")
	err := runLinuxV2NativeOwnerWitnessCommand(nil)
	if err == nil || !strings.Contains(err.Error(), "opt-in is required") {
		t.Fatalf("witness command error = %v, want explicit opt-in rejection", err)
	}
}

func TestLinuxV2NativeOwnerWitnessCommandRejectsExtraArgumentsBeforeHostSetup(t *testing.T) {
	t.Setenv("SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS", "1")
	err := runLinuxV2NativeOwnerWitnessCommand([]string{
		"-config", "/opt/symmetry-owner-test/pi/config.json",
		"-request", "/opt/symmetry-owner-test/pi/request.json",
		"unexpected",
	})
	if err == nil || !strings.Contains(err.Error(), "requires only") {
		t.Fatalf("witness command error = %v, want strict argument rejection", err)
	}
}

func TestPrintStateDirResolvesAgainstRootWithoutCreatingState(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd WorkingDirectory=/ path semantics are Linux-only")
	}
	temp := t.TempDir()
	configured := strings.TrimPrefix(temp, "/") + "/state"
	configPath := writeStateDirCommandConfig(t, configured)
	var stdout bytes.Buffer
	if err := runPrintStateDirCommand([]string{"-config", configPath}, &stdout); err != nil {
		t.Fatalf("print configured StateDir: %v", err)
	}
	want := "/" + configured + "\n"
	if stdout.String() != want {
		t.Fatalf("printed StateDir = %q, want only %q", stdout.String(), want)
	}
	if _, err := os.Lstat(filepath.FromSlash(strings.TrimSuffix(want, "\n"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("StateDir print command created or changed the configured directory: %v", err)
	}
}

func TestPrintStateDirRejectsTraversalAndSymlinkWithoutOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd WorkingDirectory=/ path semantics are Linux-only")
	}
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	if err := os.Mkdir(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	for _, stateDir := range []string{
		"",
		".",
		"..",
		"../escape",
		"nested/../state",
		"./.symmetry/state",
		"/",
		"tmp//state",
		"tmp/state\twith-tab",
		"tmp/state\x7fwith-del",
		strings.Repeat("a", 4096),
		filepath.Join(link, "state"),
	} {
		t.Run(stateDir, func(t *testing.T) {
			configPath := writeStateDirCommandConfig(t, stateDir)
			var stdout bytes.Buffer
			if err := runPrintStateDirCommand([]string{"-config", configPath}, &stdout); err == nil {
				t.Fatal("unsafe StateDir was printed")
			}
			if stdout.Len() != 0 {
				t.Fatalf("failed StateDir command wrote to stdout: %q", stdout.String())
			}
		})
	}
}

func writeStateDirCommandConfig(t *testing.T, stateDir string) string {
	t.Helper()
	value := config.Config{
		ControlPlaneURL: "https://control.example.test/api", StateDir: stateDir, MachineName: "state-dir-test",
		AgentProfiles: map[string]config.AgentProfile{
			"default": {Command: "agent", InputMode: config.InputModeGoal},
		},
		Workspaces: map[string]config.Workspace{
			"primary": {Policy: config.WorkspacePolicyExistingCheckout, Path: t.TempDir(), Cleanup: config.CleanupNever},
		},
		Runtime: config.Runtime{RuntimeKey: "state-dir-test", Name: "test", Capacity: 1, AgentProfile: "default", Workspace: "primary"},
	}
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "daemon.json")
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}
