package protocol

import (
	"encoding/json"
	"reflect"
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

func TestRuntimeRegistrationRejectsInvalidCapabilitiesRawJSONBeforeConversion(t *testing.T) {
	data := []byte(`{"runtime_key":"default","name":"Local Codex","capacity":1,"agent_profile":"codex","workspace":"primary","capabilities":{"structured_input":true,"provider_access":true,"adapter":{"kind":"codex","native_version":"\uD800","implementation_version":"symmetry-adapter-1","protocol_version":1,"operations":{"start":true,"events":true,"cancel":true,"resume":false,"handoff":false,"guidance":"next_turn","pause":"unsupported","approval_response":false,"usage":"unknown","hard_cost_limit":false}}}}`)
	var registration RuntimeRegistration
	err := json.Unmarshal(data, &registration)
	if err == nil || !strings.Contains(err.Error(), "unpaired high surrogate") {
		t.Fatalf("RuntimeRegistration accepted or normalized invalid capabilities JSON: %v", err)
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

func TestSessionRegistrationRequestAcceptsUnpromotedOMP(t *testing.T) {
	request := SessionRegistrationRequest{Runtimes: []RuntimeRegistration{unpromotedOMPRegistration()}}
	if err := request.Validate(); err != nil {
		t.Fatalf("validate complete unpromoted OMP registration: %v", err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"runtimes":[{"runtime_key":"omp","name":"OMP","capacity":1,"agent_profile":"omp","workspace":"primary","harness_kind":"omp","harness_version":"18.20.0","adapter_version":"omp-rpc-v2","adapter_protocol_version":2,"capabilities":{"structured_input":false,"provider_access":false,"adapter":{"kind":"omp","native_version":"18.20.0","implementation_version":"omp-rpc-v2","protocol_version":2,"operations":{"start":false,"events":false,"cancel":false,"resume":false,"handoff":false,"guidance":"unsupported","pause":"unsupported","approval_response":false,"usage":"unknown","hard_cost_limit":false}}}}]}`
	if string(encoded) != want {
		t.Fatalf("unpromoted OMP registration JSON changed:\n got %s\nwant %s", encoded, want)
	}
	var restored SessionRegistrationRequest
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatalf("decode complete unpromoted OMP registration: %v", err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatalf("validate restored OMP registration: %v", err)
	}
	if !reflect.DeepEqual(request, restored) {
		t.Fatalf("OMP registration changed during JSON round trip: got %#v, want %#v", restored, request)
	}
}

func TestSessionRegistrationRequestRejectsInvalidOMPAdvertisement(t *testing.T) {
	for name, test := range map[string]struct {
		mutate    func(*RuntimeRegistration)
		wantError string
	}{
		"unknown harness kind": {
			mutate: func(registration *RuntimeRegistration) {
				registration.HarnessKind = "future_harness"
				registration.Capabilities.Adapter.Kind = "future_harness"
			},
			wantError: `harness_kind "future_harness" is invalid`,
		},
		"adapter metadata mismatch": {
			mutate: func(registration *RuntimeRegistration) {
				registration.Capabilities.Adapter.NativeVersion = "18.20.1"
			},
			wantError: "adapter metadata must match runtime harness metadata",
		},
		"native safe pause": {
			mutate: func(registration *RuntimeRegistration) {
				registration.Capabilities.Adapter.Operations.Start = true
				registration.Capabilities.Adapter.Operations.Resume = true
				registration.Capabilities.Adapter.Operations.Pause = PauseSafeBoundary
			},
			wantError: "native adapter registrations must advertise pause as unsupported",
		},
		"native supervisory control": {
			mutate: func(registration *RuntimeRegistration) {
				registration.Capabilities.SupervisoryControl = true
			},
			wantError: "native adapter registrations cannot advertise supervisory_control",
		},
		"invalid capability schema": {
			mutate: func(registration *RuntimeRegistration) {
				registration.Capabilities.Adapter.Operations.Guidance = GuidanceCapability("future_guidance")
			},
			wantError: "validate runtime capabilities schema",
		},
	} {
		t.Run(name, func(t *testing.T) {
			registration := unpromotedOMPRegistration()
			test.mutate(&registration)
			request := SessionRegistrationRequest{Runtimes: []RuntimeRegistration{registration}}
			err := request.Validate()
			if err == nil || !strings.Contains(err.Error(), "runtimes[0]: "+test.wantError) {
				t.Fatalf("Validate error = %v, want runtime rejection containing %q", err, test.wantError)
			}
		})
	}
}

// unpromotedOMPRegistration mirrors the runtime consumer's projection of the
// observed OMP candidate; transport observations do not promote operations.
func unpromotedOMPRegistration() RuntimeRegistration {
	return RuntimeRegistration{
		RuntimeKey:             "omp",
		Name:                   "OMP",
		Capacity:               1,
		AgentProfile:           "omp",
		Workspace:              "primary",
		HarnessKind:            "omp",
		HarnessVersion:         "18.20.0",
		AdapterVersion:         "omp-rpc-v2",
		AdapterProtocolVersion: 2,
		Capabilities: RuntimeCapabilities{Adapter: &Adapter{
			Kind:                  "omp",
			NativeVersion:         "18.20.0",
			ImplementationVersion: "omp-rpc-v2",
			ProtocolVersion:       2,
			Operations: AdapterOperations{
				Start: false, Events: false, Cancel: false, Resume: false, Handoff: false,
				Guidance: GuidanceUnsupported, Pause: PauseUnsupported,
				ApprovalResponse: false, Usage: UsageUnknown, HardCostLimit: false,
			},
		}},
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
