package protocol

import "testing"

func TestAdapterOperationsValidateRejectsMissingPrerequisites(t *testing.T) {
	valid := AdapterOperations{
		Guidance: GuidanceUnsupported,
		Pause:    PauseUnsupported,
		Usage:    UsageUnknown,
	}
	for _, test := range []struct {
		name   string
		mutate func(*AdapterOperations)
	}{
		{
			name: "resume without start",
			mutate: func(value *AdapterOperations) {
				value.Resume = true
			},
		},
		{
			name: "events without start",
			mutate: func(value *AdapterOperations) {
				value.Events = true
			},
		},
		{
			name: "cancel without start",
			mutate: func(value *AdapterOperations) {
				value.Cancel = true
			},
		},
		{
			name: "cancel without events",
			mutate: func(value *AdapterOperations) {
				value.Start = true
				value.Cancel = true
			},
		},
		{
			name: "approval response without start",
			mutate: func(value *AdapterOperations) {
				value.ApprovalResponse = true
			},
		},
		{
			name: "approval response without events",
			mutate: func(value *AdapterOperations) {
				value.Start = true
				value.ApprovalResponse = true
			},
		},
		{
			name: "handoff without start",
			mutate: func(value *AdapterOperations) {
				value.Handoff = true
			},
		},
		{
			name: "handoff without events",
			mutate: func(value *AdapterOperations) {
				value.Start = true
				value.Handoff = true
			},
		},
		{
			name: "handoff without cancel",
			mutate: func(value *AdapterOperations) {
				value.Start = true
				value.Events = true
				value.Handoff = true
			},
		},
		{
			name: "native guidance without start",
			mutate: func(value *AdapterOperations) {
				value.Guidance = GuidanceNativeSteer
			},
		},
		{
			name: "safe pause without resume",
			mutate: func(value *AdapterOperations) {
				value.Pause = PauseSafeBoundary
			},
		},
		{
			name: "reported usage without events",
			mutate: func(value *AdapterOperations) {
				value.Usage = UsageReported
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			operations := valid
			test.mutate(&operations)
			if err := operations.Validate(); err == nil {
				t.Fatalf("AdapterOperations.Validate accepted invalid operations: %+v", operations)
			}
		})
	}
}

func TestAdapterOperationsValidateAcceptsMinimalAndAdditiveCapabilities(t *testing.T) {
	for _, test := range []struct {
		name       string
		operations AdapterOperations
	}{
		{
			name: "minimal unsupported",
			operations: AdapterOperations{
				Guidance: GuidanceUnsupported,
				Pause:    PauseUnsupported,
				Usage:    UsageUnknown,
			},
		},
		{
			name: "start",
			operations: AdapterOperations{
				Start:    true,
				Guidance: GuidanceUnsupported,
				Pause:    PauseUnsupported,
				Usage:    UsageUnknown,
			},
		},
		{
			name: "start and events",
			operations: AdapterOperations{
				Start:    true,
				Events:   true,
				Guidance: GuidanceUnsupported,
				Pause:    PauseUnsupported,
				Usage:    UsageUnknown,
			},
		},
		{
			name: "cancel",
			operations: AdapterOperations{
				Start:    true,
				Events:   true,
				Cancel:   true,
				Guidance: GuidanceUnsupported,
				Pause:    PauseUnsupported,
				Usage:    UsageUnknown,
			},
		},
		{
			name: "approval response",
			operations: AdapterOperations{
				Start:            true,
				Events:           true,
				ApprovalResponse: true,
				Guidance:         GuidanceUnsupported,
				Pause:            PauseUnsupported,
				Usage:            UsageUnknown,
			},
		},
		{
			name: "handoff",
			operations: AdapterOperations{
				Start:    true,
				Events:   true,
				Cancel:   true,
				Handoff:  true,
				Guidance: GuidanceUnsupported,
				Pause:    PauseUnsupported,
				Usage:    UsageUnknown,
			},
		},
		{
			name: "resume and safe pause",
			operations: AdapterOperations{
				Start:    true,
				Resume:   true,
				Guidance: GuidanceUnsupported,
				Pause:    PauseSafeBoundary,
				Usage:    UsageUnknown,
			},
		},
		{
			name: "native guidance",
			operations: AdapterOperations{
				Start:    true,
				Guidance: GuidanceNativeSteer,
				Pause:    PauseUnsupported,
				Usage:    UsageUnknown,
			},
		},
		{
			name: "reported usage",
			operations: AdapterOperations{
				Start:    true,
				Events:   true,
				Guidance: GuidanceUnsupported,
				Pause:    PauseUnsupported,
				Usage:    UsageReported,
			},
		},
		{
			name: "hard cost limit",
			operations: AdapterOperations{
				Guidance:      GuidanceUnsupported,
				Pause:         PauseUnsupported,
				Usage:         UsageUnknown,
				HardCostLimit: true,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.operations.Validate(); err != nil {
				t.Fatalf("AdapterOperations.Validate rejected valid operations: %v; operations = %+v", err, test.operations)
			}
		})
	}
}
