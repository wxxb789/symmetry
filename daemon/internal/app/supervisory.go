package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func (daemon *daemon) supervisoryControl() bool {
	profile := daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile]
	return profile.SupervisoryControl && profile.Interactive && profile.InputMode == config.InputModeJSON && profile.EventFormat == config.EventFormatJSONL
}

func supervisoryRecoveryRequired(journal state.RunJournal) bool {
	return journal.LocalState == "paused" || len(journal.ControlCommandIntents) != 0
}

// A durable intent is saved before stdin is written. It remains unresolved until
// the agent reports application at a safe boundary; redelivery never writes twice.
func (daemon *daemon) handleSupervisoryCommand(ctx context.Context, key state.RunKey, journal state.RunJournal, active *runningRun, command protocol.Command) bool {
	if !daemon.supervisoryControl() ||
		(command.Kind != "guidance" && command.Kind != "pause" && command.Kind != "resume") ||
		control.ValidateSupervisoryPayload(command.Kind, command.Payload) != nil {
		return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
	}
	digest, err := canonicalInputDigest(command.Payload)
	if err != nil {
		return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
	}
	input, err := json.Marshal(protocol.AgentInputRecord{Type: protocol.AgentInputRecordType(command.Kind), CommandID: command.CommandID, Goal: journal.Work.Goal, Input: command.Payload})
	if err != nil {
		return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "failed")
	}
	transitionID, err := daemon.options.newID()
	if err != nil {
		return false
	}
	ackID, err := daemon.options.newID()
	if err != nil {
		return false
	}
	active.inputMu.Lock()
	defer active.inputMu.Unlock()
	daemon.mu.Lock()
	accepted := daemon.running[key] == active && activeRunAcceptsCommand(active)
	process := active.process
	daemon.mu.Unlock()
	if !accepted {
		return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
	}
	_, created, err := daemon.store.PrepareControlCommand(key, state.ControlCommandIntent{
		CommandID: command.CommandID, Kind: command.Kind, PayloadDigest: digest,
		TransitionID: transitionID, AckID: ackID,
	})
	if err != nil {
		current, loadErr := daemon.store.LoadJournal(key)
		if loadErr == nil {
			for _, intent := range current.ControlCommandIntents {
				if intent.CommandID == command.CommandID {
					// Never replace the original receipt with an incompatible
					// outcome when a conflicting delivery reuses its identity.
					return true
				}
			}
		}
		return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
	}
	if !created {
		daemon.signalOutboxFor(key)
		return true
	}
	if err := process.WriteInput(append(input, '\n')); err != nil {
		return daemon.completeControlWriteFailure(ctx, key, command)
	}
	return true
}

func (daemon *daemon) completeControlWriteFailure(ctx context.Context, key state.RunKey, command protocol.Command) bool {
	id, err := daemon.newIDWithRetry(ctx, key, command.CommandID, "control_failure_event")
	if err != nil {
		return false
	}
	payload, _ := json.Marshal(map[string]string{"type": "command_applied", "command_id": command.CommandID, "kind": command.Kind, "outcome": "failed"})
	for {
		_, err := daemon.store.CompleteControlCommand(key, command.CommandID, command.Kind, "failed", protocol.RunEvent{EventID: id, Kind: "command_applied", Payload: payload, OccurredAt: daemon.now()})
		if err == nil {
			daemon.signalOutboxFor(key)
			return true
		}
		if state.IsNotFound(err) || daemon.commandAcknowledgementRetired(key) {
			return false
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) queueCommandApplied(key state.RunKey, payload map[string]any, at time.Time) (bool, error) {
	if !daemon.supervisoryControl() {
		return false, nil
	}
	commandID, _ := payload["command_id"].(string)
	kind, _ := payload["kind"].(string)
	outcome, _ := payload["outcome"].(string)
	active := daemon.runningRun(key)
	if active == nil {
		return false, nil
	}
	active.inputMu.Lock()
	defer active.inputMu.Unlock()
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return false, err
	}
	matched := false
	for _, intent := range journal.ControlCommandIntents {
		if intent.CommandID == commandID && intent.Kind == kind {
			matched = true
			break
		}
	}
	if !matched {
		return false, nil
	}
	daemon.mu.Lock()
	cancelled := active.cancelled || active.stale || active.terminal
	daemon.mu.Unlock()
	if cancelled && outcome == "applied" {
		outcome = "rejected"
		payload["outcome"] = outcome
	}
	id, err := daemon.options.newID()
	if err != nil {
		return false, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	_, err = daemon.store.CompleteControlCommand(key, commandID, kind, outcome, protocol.RunEvent{EventID: id, Kind: "command_applied", Payload: encoded, OccurredAt: at})
	if err == nil {
		daemon.signalOutboxFor(key)
	}
	return true, err
}

func validDecisionPacket(payload map[string]any) bool {
	if !nonEmptyString(payload["question"]) {
		return false
	}
	decision, ok := payload["decision"].(map[string]any)
	if !ok || !nonEmptyString(decision["context"]) || !stringIn(decision["reason"], "blocked", "consequential", "irreversible", "security", "business_policy", "expensive", "product_change") {
		return false
	}
	options, ok := decision["options"].([]any)
	if !ok || len(options) < 2 || len(options) > 10 {
		return false
	}
	seen := make(map[string]bool, len(options))
	for _, value := range options {
		option, ok := value.(map[string]any)
		if !ok || !nonEmptyString(option["id"]) || !nonEmptyString(option["label"]) || !nonEmptyString(option["consequence"]) {
			return false
		}
		id := option["id"].(string)
		if seen[id] {
			return false
		}
		seen[id] = true
	}
	if recommendation, present := decision["recommended_option_id"]; present && recommendation != nil {
		id, ok := recommendation.(string)
		if !ok || !seen[id] {
			return false
		}
	}
	return true
}

func (daemon *daemon) supervisedExitFailure(key state.RunKey) error {
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return err
	}
	if journal.LocalState == "paused" {
		daemon.rememberWorkspaceRetention(key)
		daemon.persistWorkspaceRetention(key)
		return errors.New("supervisory agent exited while paused")
	}
	return nil
}

// Process fencing must never wait for a successful retention journal write.
// Remember the intent before stopping; keep it after releaseRun until durable
// journal retirement. Existing supervisory intents also protect restart cleanup.
func (daemon *daemon) rememberWorkspaceRetention(key state.RunKey) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.retainedWorkspaces == nil {
		daemon.retainedWorkspaces = make(map[state.RunKey]struct{})
	}
	daemon.retainedWorkspaces[key] = struct{}{}
}

func (daemon *daemon) workspaceRetentionRemembered(key state.RunKey) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	_, retained := daemon.retainedWorkspaces[key]
	return retained
}

func (daemon *daemon) forgetWorkspaceRetention(key state.RunKey) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	delete(daemon.retainedWorkspaces, key)
}

func (daemon *daemon) persistWorkspaceRetention(key state.RunKey) {
	retain := daemon.options.retainWorkspace
	if retain == nil {
		retain = daemon.store.RetainWorkspace
	}
	if _, err := retain(key); err != nil && !state.IsNotFound(err) && daemon.log != nil {
		daemon.log.Error("retain_supervised_workspace_failed", "run_id", key.RunID, "error", err)
	}
}

// Applied controls precede ordinary completion, so completion cannot reject a
// control that already changed execution but whose acknowledgement was pending.
func (daemon *daemon) deliverAppliedControlAcknowledgement(ctx context.Context, journal state.RunJournal) (state.RunJournal, bool, error) {
	if journal.TerminalState == "cancelled" {
		return journal, false, nil
	}
	for _, intent := range journal.ControlCommandIntents {
		if intent.Outcome != "applied" || intent.AcknowledgementDelivered || journal.HasPendingTransition(intent.TransitionID) {
			continue
		}
		for _, acknowledgement := range journal.PendingCommandAcknowledgements {
			if acknowledgement.AckID == intent.AckID {
				updated, err := daemon.deliverAcknowledgement(ctx, journal, acknowledgement)
				return updated, true, err
			}
		}
		return journal, false, errors.New("applied supervisory acknowledgement is not pending")
	}
	return journal, false, nil
}

func invalidatedSupervisoryAcknowledgement(journal state.RunJournal, acknowledgement protocol.CommandAcknowledgement, err error) bool {
	var apiError *control.APIError
	if !errors.As(err, &apiError) || apiError.Code != control.StateConflict {
		return false
	}
	for _, intent := range journal.ControlCommandIntents {
		if intent.CommandID == acknowledgement.CommandID && intent.AckID == acknowledgement.AckID {
			return true
		}
	}
	return false
}
