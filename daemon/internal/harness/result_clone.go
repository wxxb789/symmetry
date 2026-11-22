package harness

import (
	"encoding/json"
	"slices"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// Returns a result sharing no pointer or slice with the input; a nil slice stays nil
// and an empty slice stays non-nil, so a clone marshals to the same JSON bytes.
func CloneTaskResult(result TaskResult) TaskResult {
	cloned := result
	if result.Semantic != nil {
		semantic := cloneProtocolTaskResult(*result.Semantic)
		cloned.Semantic = &semantic
	}
	if result.Reason != nil {
		reason := *result.Reason
		cloned.Reason = &reason
	}
	return cloned
}

func CloneTaskResultPointer(result *TaskResult) *TaskResult {
	if result == nil {
		return nil
	}
	cloned := CloneTaskResult(*result)
	return &cloned
}

func cloneProtocolTaskResult(result protocol.TaskResult) protocol.TaskResult {
	cloned := result
	cloned.EvidenceRefs = slices.Clone(result.EvidenceRefs)
	cloned.Diagnostics = cloneDiagnostics(result.Diagnostics)
	cloned.Blocker = cloneBlocker(result.Blocker)
	if result.ProposedNextAction != nil {
		action := *result.ProposedNextAction
		action.Blocker = cloneBlocker(result.ProposedNextAction.Blocker)
		cloned.ProposedNextAction = &action
	}
	if result.Proposal != nil {
		proposal := slices.Clone(*result.Proposal)
		cloned.Proposal = &proposal
	}
	if result.Reason != nil {
		reason := *result.Reason
		cloned.Reason = &reason
	}
	return cloned
}

func cloneBlocker(blocker *protocol.Blocker) *protocol.Blocker {
	if blocker == nil {
		return nil
	}
	value := *blocker
	value.WorkItemIDs = slices.Clone(value.WorkItemIDs)
	return &value
}

func cloneDiagnostics(diagnostics []protocol.Diagnostic) []protocol.Diagnostic {
	if diagnostics == nil {
		return nil
	}
	cloned := make([]protocol.Diagnostic, len(diagnostics))
	for index, diagnostic := range diagnostics {
		cloned[index] = cloneDiagnostic(diagnostic)
	}
	return cloned
}

// Rebuilds a diagnostic through its own JSON form, which restores the protocol
// package's private parsed-field map and copies the severity pointer.
func cloneDiagnostic(diagnostic protocol.Diagnostic) protocol.Diagnostic {
	encoded, err := json.Marshal(diagnostic)
	if err == nil {
		var cloned protocol.Diagnostic
		if json.Unmarshal(encoded, &cloned) == nil {
			return cloned
		}
	}
	cloned := diagnostic
	if diagnostic.Severity != nil {
		severity := *diagnostic.Severity
		cloned.Severity = &severity
	}
	return cloned
}
