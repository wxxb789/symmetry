package operator

import (
	"encoding/json"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

type TaskWaiting struct {
	RunID        string          `json:"run_id"`
	Generation   int64           `json:"generation"`
	TransitionID string          `json:"transition_id"`
	Question     *string         `json:"question"`
	Payload      json.RawMessage `json:"payload"`
	RecordedAt   time.Time       `json:"recorded_at"`
	present      map[string]struct{}
}

func (waiting *TaskWaiting) UnmarshalJSON(value []byte) error {
	type wire TaskWaiting
	var decoded wire
	if err := json.Unmarshal(value, &decoded); err != nil {
		return err
	}
	present, err := presentFields(value)
	if err != nil {
		return err
	}
	*waiting = TaskWaiting(decoded)
	waiting.present = present
	return nil
}

func (waiting TaskWaiting) HasField(name string) bool {
	_, ok := waiting.present[name]
	return ok
}

type Task struct {
	TaskID        string                `json:"task_id"`
	State         string                `json:"state"`
	RunID         *string               `json:"run_id"`
	Generation    *int64                `json:"generation"`
	Work          *protocol.Work        `json:"work"`
	Result        json.RawMessage       `json:"result"`
	Failure       json.RawMessage       `json:"failure"`
	Waiting       *TaskWaiting          `json:"waiting"`
	LatestCommand *protocol.TaskCommand `json:"latest_command"`
	present       map[string]struct{}
}

func (task *Task) UnmarshalJSON(value []byte) error {
	type wire Task
	var decoded wire
	if err := json.Unmarshal(value, &decoded); err != nil {
		return err
	}
	present, err := presentFields(value)
	if err != nil {
		return err
	}
	*task = Task(decoded)
	task.present = present
	return nil
}

func (task Task) HasField(name string) bool {
	_, ok := task.present[name]
	return ok
}

func presentFields(value []byte) (map[string]struct{}, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(value, &fields); err != nil {
		return nil, err
	}
	present := make(map[string]struct{}, len(fields))
	for name := range fields {
		present[name] = struct{}{}
	}
	return present, nil
}
