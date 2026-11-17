package omp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

// The native client answers with reasoning blocks beside the textual answer. A
// normally stopped final message with exactly one text block must still yield
// the strict TaskResult from that block, while a message without exactly one
// textual payload must be rejected without emitting any TaskResult.
func TestFinalAssistantMessageAcceptsReasoningBlocksBesideSingleTextBlock(t *testing.T) {
	text := validTaskText(t)
	var mutex sync.Mutex
	taskResults := 0
	session, _ := testSession(t, func(request map[string]any) []any {
		if request["type"] != "prompt" {
			return nil
		}
		values := terminalFrames(request, text, "completed")
		message := values[2].(map[string]any)["message"].(map[string]any)
		message["content"] = []any{
			map[string]any{"type": "thinking", "thinking": "internal reasoning"},
			map[string]any{"type": "text", "text": text},
		}
		return values
	}, harness.EventSinkFunc(func(_ context.Context, event harness.Event) error {
		mutex.Lock()
		defer mutex.Unlock()
		if event.Kind == harness.EventTaskResult {
			taskResults++
		}
		return nil
	}), nil)
	startTestTurn(t, session)
	ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
	defer cancel()
	mustSettle(t, "WaitTurn", session.WaitTurn(ctx))
	mustSettle(t, "Close", session.Close(ctx))
	result, err := session.Wait(ctx)
	if ctx.Err() != nil {
		t.Fatalf("Wait did not settle before the %s hang guard expired", testHangGuard)
	}
	if err != nil {
		t.Fatalf("reasoning block beside the text block rejected: %v", err)
	}
	if result.Semantic == nil {
		t.Fatalf("result=%+v, want the strict TaskResult carried by the single text block", result)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if taskResults != 1 {
		t.Fatalf("reasoning-beside-text final message emitted %d TaskResult events, want one", taskResults)
	}
}

func TestFinalAssistantMessageRejectsNonTextualOrMultipleTextPayloads(t *testing.T) {
	text, err := json.Marshal(validTaskText(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		message string
	}{
		{"thinking-only", `{"role":"assistant","stopReason":"stop","content":[{"type":"thinking","thinking":"internal reasoning"}]}`},
		{"two-text-blocks", `{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":` + string(text) + `},{"type":"text","text":` + string(text) + `}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertFinalAssistantMessageRejected(t, json.RawMessage(test.message))
		})
	}
}
