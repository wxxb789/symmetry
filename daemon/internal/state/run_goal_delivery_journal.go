package state

import (
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// RunGoalDeliveryState is the goal-delivery block of RunJournal: pending,
// delivered and retired deliveries plus the enable fence.
type RunGoalDeliveryState struct {
	PendingGoalDeliveries   []GoalDelivery           `json:"pending_goal_deliveries,omitempty"`
	DeliveredGoalDeliveries []GoalDelivery           `json:"delivered_goal_deliveries,omitempty"`
	RetiredGoalDeliveries   []goalDeliveryRetirement `json:"retired_goal_deliveries,omitempty"`
	GoalDeliveryEnabled     bool                     `json:"goal_delivery_enabled,omitempty"`
}

func (journal RunGoalDeliveryState) hasExactUsageDelivery(runID string, fence protocol.Fence, usage protocol.Usage) bool {
	delivery := GoalDelivery{Kind: GoalDeliveryUsage, DeliveryID: usage.UsageKey, Fence: fence, Usage: &usage, Ready: true}
	if err := prepareGoalDelivery(runID, &delivery); err != nil {
		return false
	}
	for _, candidate := range journal.PendingGoalDeliveries {
		if candidate.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(candidate, delivery) {
			return true
		}
	}
	for _, candidate := range journal.DeliveredGoalDeliveries {
		if candidate.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(candidate, delivery) {
			return true
		}
	}
	for _, candidate := range journal.RetiredGoalDeliveries {
		if candidate.Delivery.PayloadDigest == delivery.PayloadDigest && equalGoalDelivery(candidate.Delivery, delivery) {
			return true
		}
	}
	return false
}
