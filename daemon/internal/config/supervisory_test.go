package config

import (
	"strings"
	"testing"
)

func TestSupervisoryControlRequiresCooperativeWire(t *testing.T) {
	for _, test := range []struct {
		name    string
		profile AgentProfile
		valid   bool
	}{
		{"cooperative", AgentProfile{Command: "agent", InputMode: InputModeJSON, Interactive: true, EventFormat: EventFormatJSONL, SupervisoryControl: true}, true},
		{"closed stdin", AgentProfile{Command: "agent", InputMode: InputModeJSON, EventFormat: EventFormatJSONL, SupervisoryControl: true}, false},
		{"raw output", AgentProfile{Command: "agent", InputMode: InputModeJSON, Interactive: true, EventFormat: EventFormatRaw, SupervisoryControl: true}, false},
		{"goal input", AgentProfile{Command: "agent", InputMode: InputModeGoal, EventFormat: EventFormatJSONL, SupervisoryControl: true}, false},
		{"legacy", AgentProfile{Command: "agent", InputMode: InputModeGoal, EventFormat: EventFormatRaw}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateAgentProfiles(map[string]AgentProfile{"local": test.profile})
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && (err == nil || !strings.Contains(err.Error(), "supervisory_control")) {
				t.Fatalf("invalid supervisory profile accepted: %v", err)
			}
		})
	}
}
