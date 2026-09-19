package pi

import (
	"reflect"
	"strings"
	"testing"
)

func TestPiRPCArgsPreservesAuditedProfilePairs(t *testing.T) {
	profile := []string{
		"--provider", "openai",
		"--model", "gpt-5.6",
		"--session-dir", "C:/sessions",
		"--tools", "read,grep",
		"-t", "find,ls",
		"--exclude-tools", "write",
		"-xt", "edit",
	}

	got, err := piRPCArgs(profile, nil)
	if err != nil {
		t.Fatalf("piRPCArgs() error = %v", err)
	}
	want := append([]string{"--mode", "rpc"}, profile...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("piRPCArgs() = %#v, want %#v", got, want)
	}
}

func TestPiRPCArgsAllowsAuditedConfigurationToggles(t *testing.T) {
	profile := []string{
		"--models", "openai/*",
		"--thinking", "high",
		"--name", "release audit",
		"-n", "second name",
		"--no-tools", "-nt", "--no-builtin-tools", "-nbt",
		"--no-extensions", "-ne", "--no-skills", "-ns",
		"--no-prompt-templates", "-np", "--no-themes",
		"--no-context-files", "-nc", "--no-approve", "-na",
		"--offline", "--verbose",
	}

	got, err := piRPCArgs(profile, nil)
	if err != nil {
		t.Fatalf("piRPCArgs() error = %v", err)
	}
	want := append([]string{"--mode", "rpc"}, profile...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("piRPCArgs() = %#v, want %#v", got, want)
	}
}

func TestPiRPCArgsRejectsNonRPCAndPromptArguments(t *testing.T) {
	tests := []struct {
		name    string
		profile []string
	}{
		{name: "export", profile: []string{"--export", "session.jsonl"}},
		{name: "print", profile: []string{"--print"}},
		{name: "print short", profile: []string{"-p"}},
		{name: "help", profile: []string{"--help"}},
		{name: "help short", profile: []string{"-h"}},
		{name: "version", profile: []string{"--version"}},
		{name: "version short", profile: []string{"-v"}},
		{name: "list models", profile: []string{"--list-models"}},
		{name: "package command", profile: []string{"update"}},
		{name: "config command", profile: []string{"config"}},
		{name: "auth command", profile: []string{"auth", "check"}},
		{name: "prompt", profile: []string{"summarize this repository"}},
		{name: "file prompt", profile: []string{"@prompt.md"}},
		{name: "separator", profile: []string{"--", "prompt"}},
		{name: "mode", profile: []string{"--mode", "text"}},
		{name: "history continue", profile: []string{"--continue"}},
		{name: "history continue short", profile: []string{"-c"}},
		{name: "history resume", profile: []string{"--resume"}},
		{name: "history resume short", profile: []string{"-r"}},
		{name: "session", profile: []string{"--session", "session.jsonl"}},
		{name: "session id", profile: []string{"--session-id", "session-id"}},
		{name: "fork", profile: []string{"--fork", "session.jsonl"}},
		{name: "no session", profile: []string{"--no-session"}},
		{name: "api key", profile: []string{"--api-key", "secret-value"}},
		{name: "extension loader", profile: []string{"--extension", "extension.ts"}},
		{name: "unknown", profile: []string{"--unreviewed-option"}},
		{name: "unknown value", profile: []string{"--unreviewed-option", "value"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := piRPCArgs(test.profile, nil); err == nil {
				t.Fatal("piRPCArgs() error = nil, want rejected profile")
			}
		})
	}
}

func TestPiRPCArgsRejectsEqualsAndJoinedShortFlags(t *testing.T) {
	tests := []string{
		"--provider=openai",
		"--model=gpt-5.6",
		"--session-dir=C:/sessions",
		"--tools=read,grep",
		"--exclude-tools=write",
		"--mode=rpc",
		"--continue=latest",
		"--resume=session",
		"--session=session.jsonl",
		"--session-id=session-id",
		"--fork=session.jsonl",
		"--unknown=value",
		"-tread,grep",
		"-xtwrite",
		"-nrelease",
		"-pmessage",
		"-cresume",
		"-rsession",
		"-necustom",
		"-nccustom",
		"-nacustom",
		"-ntcustom",
		"-nbtcustom",
	}

	for _, argument := range tests {
		t.Run(argument, func(t *testing.T) {
			if _, err := piRPCArgs([]string{argument}, nil); err == nil {
				t.Fatal("piRPCArgs() error = nil, want rejected profile")
			}
		})
	}
}

func TestPiRPCArgsRequiresNonOptionNonBlankValues(t *testing.T) {
	options := []string{
		"--provider", "--model", "--models", "--thinking", "--session-dir",
		"--tools", "-t", "--exclude-tools", "-xt", "--name", "-n",
	}
	for _, option := range options {
		for _, value := range [][]string{nil, {""}, {" \t "}, {"--another-option"}, {"--version"}, {"-v"}} {
			name := option + "/missing"
			if len(value) > 0 {
				name = option + "/invalid-value"
			}
			t.Run(name, func(t *testing.T) {
				profile := append([]string{option}, value...)
				if _, err := piRPCArgs(profile, nil); err == nil {
					t.Fatal("piRPCArgs() error = nil, want missing or invalid value rejection")
				}
			})
		}
	}
}

func TestPiRPCArgsRejectsNULInOptionOrValue(t *testing.T) {
	tests := []struct {
		name    string
		profile []string
	}{
		{name: "option", profile: []string{"--provider\x00", "openai"}},
		{name: "value", profile: []string{"--provider", "openai\x00"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := piRPCArgs(test.profile, nil); err == nil {
				t.Fatal("piRPCArgs() error = nil, want NUL rejection")
			}
		})
	}
}

func TestPiRPCArgsRejectsTrailingTokenAfterAuditedPairs(t *testing.T) {
	profile := []string{"--provider", "openai", "--model", "gpt-5.6", "unexpected"}
	if _, err := piRPCArgs(profile, nil); err == nil {
		t.Fatal("piRPCArgs() error = nil, want trailing token rejection")
	}
}

func TestPiRPCArgsDoesNotIncludeSensitiveValuesInErrors(t *testing.T) {
	const secret = "not-for-error-output"
	_, err := piRPCArgs([]string{"--api-key", secret}, nil)
	if err == nil {
		t.Fatal("piRPCArgs() error = nil, want rejected profile")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("piRPCArgs() error leaked profile value: %v", err)
	}
}
