package execution

import (
	"slices"
	"testing"
)

func TestLinuxV2AckStageVocabularyIsComplete(t *testing.T) {
	want := []string{
		"helper_bound", "launcher_bound", "gate_bound", "handoff_prepared", "handoff_committed",
		"lease_armed", "checkout_ready", "stop_receipt_durable", "native_release", "journal_cleared",
	}
	if !slices.Equal(LinuxV2AckStages[:], want) {
		t.Fatalf("LinuxV2AckStages = %#v, want the ten live ACK-loss stages %#v", LinuxV2AckStages, want)
	}
}
