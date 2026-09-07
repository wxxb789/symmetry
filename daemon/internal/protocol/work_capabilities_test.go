package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestWorkRequiredCapabilitiesRoundTrip(t *testing.T) {
	for _, capabilities := range []map[string]bool{nil, {}, {"supervisory_control": true, "future_capability": false}} {
		original := Work{Goal: "goal", AgentProfile: "agent", Workspace: "local", Input: json.RawMessage(`{}`), RequiredCapabilities: capabilities}
		encoded, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "required_capabilities") != (len(capabilities) > 0) {
			t.Fatalf("empty requirement map was not omitted: %s", encoded)
		}
		var restored Work
		if err := json.Unmarshal(encoded, &restored); err != nil {
			t.Fatal(err)
		}
		if len(capabilities) > 0 && !reflect.DeepEqual(capabilities, restored.RequiredCapabilities) {
			t.Fatalf("capabilities = %#v, want %#v", restored.RequiredCapabilities, capabilities)
		}
	}
}

func TestWorkRequiredCapabilitiesRejectNonBooleanValues(t *testing.T) {
	for _, value := range []string{`{"supervisory_control":null}`, `{"supervisory_control":"true"}`, `{"supervisory_control":1}`, `{"future_capability":{}}`, `[]`} {
		var work Work
		if err := json.Unmarshal([]byte(`{"required_capabilities":`+value+`}`), &work); err == nil {
			t.Errorf("accepted invalid capabilities %s", value)
		}
	}
}
