package state

import (
	"errors"
	"time"
)

const NativeUsageObservationSchemaVersion = 1

// NativeUsageObservation is the latest durable cumulative usage snapshot from
// a native harness. It is independent of the ordinary run-event outbox so an
// event acknowledgement failure or restart cannot discard accounting inputs.
type NativeUsageObservation struct {
	SchemaVersion         int       `json:"schema_version"`
	InputTokens           int64     `json:"input_tokens"`
	OutputTokens          int64     `json:"output_tokens"`
	CachedInputTokens     int64     `json:"cached_input_tokens"`
	ReasoningOutputTokens int64     `json:"reasoning_output_tokens"`
	TotalTokens           int64     `json:"total_tokens"`
	ObservedAt            time.Time `json:"observed_at"`
}

func (observation NativeUsageObservation) Validate() error {
	if observation.SchemaVersion != NativeUsageObservationSchemaVersion || observation.ObservedAt.IsZero() ||
		observation.InputTokens < 0 || observation.OutputTokens < 0 || observation.CachedInputTokens < 0 ||
		observation.ReasoningOutputTokens < 0 || observation.TotalTokens < 0 {
		return errors.New("native usage observation is invalid")
	}
	return nil
}

// Covers reports whether observation contains at least the cumulative counters
// in expected. Observation time is diagnostic and does not reduce accounting.
func (observation NativeUsageObservation) Covers(expected NativeUsageObservation) bool {
	return observation.InputTokens >= expected.InputTokens &&
		observation.OutputTokens >= expected.OutputTokens &&
		observation.CachedInputTokens >= expected.CachedInputTokens &&
		observation.ReasoningOutputTokens >= expected.ReasoningOutputTokens &&
		observation.TotalTokens >= expected.TotalTokens
}

func nativeUsageObservationRegresses(previous, next NativeUsageObservation) bool {
	if !next.Covers(previous) {
		return true
	}
	return previous.Covers(next) && next.ObservedAt.Before(previous.ObservedAt)
}

// RecordNativeUsageObservation stores only a monotonic cumulative snapshot.
// Replayed or out-of-order regressions are ignored and return the retained
// journal unchanged.
func (store *Store) RecordNativeUsageObservation(key RunKey, observation NativeUsageObservation) (RunJournal, error) {
	if err := validateKey(key); err != nil {
		return RunJournal{}, err
	}
	if err := observation.Validate(); err != nil {
		return RunJournal{}, err
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.NativeUsageObservation != nil {
			previous := *journal.NativeUsageObservation
			if nativeUsageObservationRegresses(previous, observation) {
				return nil
			}
		}
		copy := observation
		journal.NativeUsageObservation = &copy
		return nil
	})
}
