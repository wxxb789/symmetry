package contracts

import (
	"encoding/json"
	"testing"
)

// Re-marshalled to pin the wire shape, since the schema admits additional members.
func TestAttentionReadServesNoGoalUnavailableMember(t *testing.T) {
	var read SymmetryAttentionReadV1
	if err := DecodeSchema(
		EnvelopeAttentionRead,
		readContractFixture(t, "valid/attention-read.basic.json"),
		&read,
	); err != nil {
		t.Fatalf("the served attention read did not decode: %v", err)
	}

	encoded, err := json.Marshal(read)
	if err != nil {
		t.Fatalf("re-marshal the decoded attention read: %v", err)
	}

	var roundTrip map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("decode the re-marshalled attention read: %v", err)
	}

	var goalAttention map[string]json.RawMessage
	if err := json.Unmarshal(roundTrip["goal_attention"], &goalAttention); err != nil {
		t.Fatalf("decode the re-marshalled Goal attention feed: %v", err)
	}

	if _, present := goalAttention["unavailable"]; present {
		t.Fatalf("the Goal attention feed carries an unavailable member: %s", encoded)
	}
	if _, present := goalAttention["entries"]; !present {
		t.Fatalf("the Goal attention feed lost its entries member: %s", encoded)
	}
}
