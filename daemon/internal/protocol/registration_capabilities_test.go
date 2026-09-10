package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeRegistrationLegacyJSONRemainsByteCompatible(t *testing.T) {
	legacy := `{"runtime_key":"default","name":"Local Codex","capacity":1,"agent_profile":"codex","workspace":"primary","capabilities":{"structured_input":true,"provider_access":true}}`
	var registration RuntimeRegistration
	if err := json.Unmarshal([]byte(legacy), &registration); err != nil {
		t.Fatal(err)
	}
	if err := registration.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != legacy {
		t.Fatalf("legacy registration JSON changed:\n got %s\nwant %s", encoded, legacy)
	}

	var withUnknown RuntimeRegistration
	if err := json.Unmarshal([]byte(strings.Replace(legacy, `,"capabilities"`, `,"future_field":true,"capabilities"`, 1)), &withUnknown); err != nil {
		t.Fatalf("legacy unknown-field tolerance changed: %v", err)
	}
}

func TestRuntimeCapabilitiesUseTheCanonicalSchemaAtTheJSONBoundary(t *testing.T) {
	for name, data := range map[string]string{
		"empty legacy map":  `{}`,
		"legacy booleans":   `{"structured_input":true,"provider_access":true}`,
		"versioned adapter": `{"structured_input":true,"provider_access":true,"adapter":{"kind":"codex","native_version":"1.2.3","implementation_version":"symmetry-adapter-1","protocol_version":1,"operations":{"start":true,"events":true,"cancel":true,"resume":false,"handoff":false,"guidance":"next_turn","pause":"unsupported","approval_response":false,"usage":"unknown","hard_cost_limit":false}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var capabilities RuntimeCapabilities
			if err := json.Unmarshal([]byte(data), &capabilities); err != nil {
				t.Fatalf("decode capabilities: %v", err)
			}
			if err := capabilities.Validate(); err != nil {
				t.Fatalf("validate capabilities: %v", err)
			}
		})
	}

	for name, data := range map[string]string{
		"unknown legacy capability": `{"structured_input":true,"future_capability":true}`,
		"unknown adapter operation": `{"adapter":{"kind":"codex","native_version":"1.2.3","implementation_version":"symmetry-adapter-1","protocol_version":1,"operations":{"start":true,"events":true,"cancel":true,"resume":false,"handoff":false,"guidance":"next_turn","pause":"unsupported","approval_response":false,"usage":"unknown","hard_cost_limit":false,"extra":true}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var capabilities RuntimeCapabilities
			if err := json.Unmarshal([]byte(data), &capabilities); err == nil {
				t.Fatalf("accepted invalid capabilities %s", data)
			}
		})
	}
}

func TestRuntimeRegistrationCarriesNativeAndGenericMetadata(t *testing.T) {
	repositoryResourceID := "00000000-0000-4000-8000-000000000001"
	operations := AdapterOperations{
		Start: true, Events: true, Cancel: true, Resume: false, Handoff: true,
		Guidance: GuidanceNextTurn, Pause: PauseUnsupported,
		ApprovalResponse: false, Usage: UsageUnknown, HardCostLimit: false,
	}
	native := RuntimeRegistration{
		RuntimeKey:             "codex",
		Name:                   "Codex",
		Capacity:               1,
		AgentProfile:           "codex",
		Workspace:              "primary",
		RepositoryResourceID:   &repositoryResourceID,
		HarnessKind:            "codex",
		HarnessVersion:         "1.2.3",
		AdapterVersion:         "symmetry-adapter-1",
		AdapterProtocolVersion: 1,
		Capabilities: RuntimeCapabilities{
			StructuredInput: true,
			ProviderAccess:  true,
			Adapter: &Adapter{
				Kind:                  "codex",
				NativeVersion:         "1.2.3",
				ImplementationVersion: "symmetry-adapter-1",
				ProtocolVersion:       1,
				Operations:            operations,
			},
		},
	}
	encoded, err := json.Marshal(native)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"harness_kind":"codex"`) || !strings.Contains(string(encoded), `"adapter"`) || !strings.Contains(string(encoded), `"repository_resource_id":"00000000-0000-4000-8000-000000000001"`) {
		t.Fatalf("native metadata missing from JSON: %s", encoded)
	}
	var restored RuntimeRegistration
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatal(err)
	}
	if restored.Capabilities.Adapter == nil || restored.Capabilities.Adapter.Operations.Guidance != GuidanceNextTurn || !restored.Capabilities.Adapter.Operations.Handoff {
		t.Fatalf("adapter operations = %#v", restored.Capabilities.Adapter)
	}

	generic := native
	generic.RuntimeKey = "generic"
	generic.HarnessKind = "generic"
	generic.HarnessVersion = "v1"
	generic.AdapterVersion = "v1"
	generic.Capabilities.Adapter = nil
	if err := generic.Validate(); err != nil {
		t.Fatalf("generic metadata rejected: %v", err)
	}
}

func TestRuntimeRegistrationAllowsOmittedOrNullRepositoryResourceID(t *testing.T) {
	for _, value := range []string{
		`{"runtime_key":"native","name":"Native","capacity":1,"agent_profile":"codex","workspace":"primary","capabilities":{}}`,
		`{"runtime_key":"native","name":"Native","capacity":1,"agent_profile":"codex","workspace":"primary","repository_resource_id":null,"capabilities":{}}`,
	} {
		var registration RuntimeRegistration
		if err := json.Unmarshal([]byte(value), &registration); err != nil {
			t.Fatalf("rejected compatible repository resource ID shape: %s: %v", value, err)
		}
		if registration.RepositoryResourceID != nil {
			t.Fatalf("repository_resource_id = %q, want nil", *registration.RepositoryResourceID)
		}
	}
}

func TestRuntimeRegistrationRejectsInvalidRepositoryResourceID(t *testing.T) {
	for _, value := range []string{
		`{"runtime_key":"native","name":"Native","capacity":1,"agent_profile":"codex","workspace":"primary","repository_resource_id":"not-a-uuid","capabilities":{}}`,
		`{"runtime_key":"native","name":"Native","capacity":1,"agent_profile":"codex","workspace":"primary","repository_resource_id":"00000000-0000-4000-8000-00000000000A","capabilities":{}}`,
		`{"runtime_key":"native","name":"Native","capacity":1,"agent_profile":"codex","workspace":"primary","repository_resource_id":1,"capabilities":{}}`,
		`{"runtime_key":"native","name":"Native","capacity":1,"agent_profile":"codex","workspace":"primary","repository_resource_id":true,"capabilities":{}}`,
		`{"runtime_key":"native","name":"Native","capacity":1,"agent_profile":"codex","workspace":"primary","repository_resource_id":{},"capabilities":{}}`,
		`{"runtime_key":"native","name":"Native","capacity":1,"agent_profile":"codex","workspace":"primary","repository_resource_id":[],"capabilities":{}}`,
	} {
		var registration RuntimeRegistration
		if err := json.Unmarshal([]byte(value), &registration); err == nil {
			t.Fatalf("accepted invalid repository resource ID: %s", value)
		}
	}
}

func TestAdapterOperationsRejectImpossibleCombinations(t *testing.T) {
	valid := func() AdapterOperations {
		return AdapterOperations{
			Start: true, Events: true, Cancel: true, Resume: false, Handoff: false,
			Guidance: GuidanceNextTurn, Pause: PauseUnsupported,
			ApprovalResponse: false, Usage: UsageUnknown, HardCostLimit: false,
		}
	}
	for name, mutate := range map[string]func(*AdapterOperations){
		"resume requires start": func(operations *AdapterOperations) {
			operations.Start = false
			operations.Events = false
			operations.Resume = true
		},
		"handoff requires start": func(operations *AdapterOperations) {
			operations.Handoff = true
			operations.Start = false
		},
		"handoff requires events": func(operations *AdapterOperations) {
			operations.Handoff = true
			operations.Events = false
		},
		"handoff requires cancel": func(operations *AdapterOperations) {
			operations.Handoff = true
			operations.Cancel = false
		},
		"events require start": func(operations *AdapterOperations) {
			operations.Start = false
		},
		"native steer requires start": func(operations *AdapterOperations) {
			operations.Start = false
			operations.Events = false
			operations.Guidance = GuidanceNativeSteer
		},
		"safe boundary requires resume": func(operations *AdapterOperations) {
			operations.Pause = PauseSafeBoundary
		},
		"reported usage requires events": func(operations *AdapterOperations) {
			operations.Events = false
			operations.Usage = UsageReported
		},
	} {
		t.Run(name, func(t *testing.T) {
			operations := valid()
			mutate(&operations)
			if err := operations.Validate(); err == nil {
				t.Fatalf("accepted impossible operations: %#v", operations)
			}
		})
	}
}

func TestRuntimeRegistrationRejectsControlIncompatibleAdapterClaims(t *testing.T) {
	registrationFor := func(harnessKind string) RuntimeRegistration {
		operations := AdapterOperations{
			Start: true, Events: true, Cancel: true, Resume: false, Handoff: false,
			Guidance: GuidanceNextTurn, Pause: PauseUnsupported,
			ApprovalResponse: false, Usage: UsageUnknown, HardCostLimit: false,
		}
		if harnessKind == "generic" {
			operations.Guidance = GuidanceUnsupported
		}
		return RuntimeRegistration{
			RuntimeKey:             harnessKind + "-runtime",
			Name:                   "Runtime",
			Capacity:               1,
			AgentProfile:           harnessKind,
			Workspace:              "primary",
			HarnessKind:            harnessKind,
			HarnessVersion:         "v1",
			AdapterVersion:         "adapter-v1",
			AdapterProtocolVersion: 1,
			Capabilities: RuntimeCapabilities{Adapter: &Adapter{
				Kind:                  harnessKind,
				NativeVersion:         "v1",
				ImplementationVersion: "adapter-v1",
				ProtocolVersion:       1,
				Operations:            operations,
			}},
		}
	}

	for name, mutate := range map[string]func(*RuntimeRegistration){
		"generic resume": func(registration *RuntimeRegistration) {
			registration.Capabilities.Adapter.Operations.Resume = true
		},
		"generic handoff": func(registration *RuntimeRegistration) {
			registration.Capabilities.Adapter.Operations.Handoff = true
		},
		"generic guidance": func(registration *RuntimeRegistration) {
			registration.Capabilities.Adapter.Operations.Guidance = GuidanceNextTurn
		},
		"generic safe pause": func(registration *RuntimeRegistration) {
			registration.Capabilities.Adapter.Operations.Resume = true
			registration.Capabilities.Adapter.Operations.Pause = PauseSafeBoundary
		},
		"generic approval response": func(registration *RuntimeRegistration) {
			registration.Capabilities.Adapter.Operations.ApprovalResponse = true
		},
		"generic reported usage": func(registration *RuntimeRegistration) {
			registration.Capabilities.Adapter.Operations.Usage = UsageReported
		},
		"generic hard cost limit": func(registration *RuntimeRegistration) {
			registration.Capabilities.Adapter.Operations.HardCostLimit = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			registration := registrationFor("generic")
			mutate(&registration)
			if err := registration.Validate(); err == nil {
				t.Fatalf("accepted control-incompatible generic registration: %#v", registration)
			}
		})
	}

	for name, mutate := range map[string]func(*RuntimeRegistration){
		"native safe pause": func(registration *RuntimeRegistration) {
			registration.Capabilities.Adapter.Operations.Resume = true
			registration.Capabilities.Adapter.Operations.Pause = PauseSafeBoundary
		},
		"native supervisory control": func(registration *RuntimeRegistration) {
			registration.Capabilities.SupervisoryControl = true
		},
		"adapter metadata mismatch": func(registration *RuntimeRegistration) {
			registration.Capabilities.Adapter.Kind = "pi"
		},
		"adapter without metadata": func(registration *RuntimeRegistration) {
			registration.HarnessKind = ""
			registration.HarnessVersion = ""
			registration.AdapterVersion = ""
			registration.AdapterProtocolVersion = 0
		},
	} {
		t.Run(name, func(t *testing.T) {
			registration := registrationFor("codex")
			mutate(&registration)
			if err := registration.Validate(); err == nil {
				t.Fatalf("accepted control-incompatible native registration: %#v", registration)
			}
		})
	}
}

func TestRuntimeRegistrationRejectsPartialOrInvalidNativeMetadata(t *testing.T) {
	legacy := `{"runtime_key":"default","name":"Local Codex","capacity":1,"agent_profile":"codex","workspace":"primary","capabilities":{"structured_input":true,"provider_access":true}}`
	for name, value := range map[string]string{
		"partial metadata":               strings.Replace(legacy, `,"capabilities"`, `,"harness_kind":"codex","capabilities"`, 1),
		"invalid protocol version":       strings.Replace(legacy, `,"capabilities"`, `,"harness_kind":"codex","harness_version":"1","adapter_version":"1","adapter_protocol_version":0,"capabilities"`, 1),
		"control protocol version limit": strings.Replace(legacy, `,"capabilities"`, `,"harness_kind":"codex","harness_version":"1","adapter_version":"1","adapter_protocol_version":2147483648,"capabilities"`, 1),
		"unknown adapter operation":      strings.Replace(legacy, `"capabilities":{"structured_input":true,"provider_access":true}`, `"capabilities":{"structured_input":true,"provider_access":true,"adapter":{"kind":"codex","native_version":"1","implementation_version":"adapter","protocol_version":1,"operations":{"start":true,"events":true,"cancel":true,"resume":false,"handoff":false,"guidance":"unknown","pause":"unsupported","approval_response":false,"usage":"unknown","hard_cost_limit":false}}}`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			var registration RuntimeRegistration
			if err := json.Unmarshal([]byte(value), &registration); err == nil {
				t.Fatalf("accepted invalid registration: %s", value)
			}
		})
	}
}
