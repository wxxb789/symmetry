package execution

import (
	"slices"
	"testing"
)

// TestLinuxV2AckStageVocabularyIsComplete pins the single source of the
// ACK-loss stage vocabulary: the ten stages an acknowledgement can be dropped
// after, in declaration order and with their exact strings. The witness request
// producers and the release acceptance carry these literals, so dropping or
// renaming a stage must fail here and must fail App's witness validation, which
// derives its membership from the same list instead of repeating the names.
func TestLinuxV2AckStageVocabularyIsComplete(t *testing.T) {
	want := []string{
		"helper_bound", "launcher_bound", "gate_bound", "handoff_prepared", "handoff_committed",
		"lease_armed", "checkout_ready", "stop_receipt_durable", "native_release", "journal_cleared",
	}
	if !slices.Equal(LinuxV2AckStages[:], want) {
		t.Fatalf("LinuxV2AckStages = %#v, want the ten live ACK-loss stages %#v", LinuxV2AckStages, want)
	}
}
