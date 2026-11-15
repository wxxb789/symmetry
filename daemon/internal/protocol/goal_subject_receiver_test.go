package protocol

import (
	"encoding/json"
	"testing"
)

// The replacement identity differs from the shared test identity in every
// field, so per-field mutation of a receiver is visible during decoding.
const (
	receiverReplacementUUID   = testUUIDTwo
	receiverReplacementCommit = "fedcba9876543210fedcba9876543210fedcba98"
	receiverReplacementDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

func TestSubjectUnmarshalPreservesReceiverOnFailure(t *testing.T) {
	original := Subject{ResourceID: testUUID, Commit: testCommit, TreeDigest: testTreeDigest}

	for name, input := range map[string]string{
		"empty input":             "",
		"array instead of object": `["` + receiverReplacementUUID + `"]`,
		"truncated object":        `{"resource_id":"` + receiverReplacementUUID + `","commit":`,
		"non-string tree digest":  `{"resource_id":"` + receiverReplacementUUID + `","commit":"` + receiverReplacementCommit + `","tree_digest":true}`,
		"missing commit":          `{"resource_id":"` + receiverReplacementUUID + `","tree_digest":"` + receiverReplacementDigest + `"}`,
		"missing tree digest":     `{"resource_id":"` + receiverReplacementUUID + `","commit":"` + receiverReplacementCommit + `"}`,
		"unknown field":           `{"resource_id":"` + receiverReplacementUUID + `","commit":"` + receiverReplacementCommit + `","tree_digest":"` + receiverReplacementDigest + `","unexpected":true}`,
		"duplicate member":        `{"resource_id":"` + receiverReplacementUUID + `","resource_id":"` + receiverReplacementUUID + `","commit":"` + receiverReplacementCommit + `","tree_digest":"` + receiverReplacementDigest + `"}`,
		"invalid resource id":     `{"resource_id":"not-a-uuid","commit":"` + receiverReplacementCommit + `","tree_digest":"` + receiverReplacementDigest + `"}`,
		"invalid commit":          `{"resource_id":"` + receiverReplacementUUID + `","commit":"abc123","tree_digest":"` + receiverReplacementDigest + `"}`,
		"invalid tree digest":     `{"resource_id":"` + receiverReplacementUUID + `","commit":"` + receiverReplacementCommit + `","tree_digest":"sha256:zzzz"}`,
	} {
		t.Run(name, func(t *testing.T) {
			receiver := original
			if err := json.Unmarshal([]byte(input), &receiver); err == nil {
				t.Fatalf("decoded invalid subject %s without error", input)
			}
			if receiver != original {
				t.Fatalf("rejected input %s mutated receiver: got %+v, want %+v", input, receiver, original)
			}
		})
	}
}

func TestSubjectUnmarshalReplacesReceiverOnSuccess(t *testing.T) {
	original := Subject{ResourceID: testUUID, Commit: testCommit, TreeDigest: testTreeDigest}
	replacement := Subject{
		ResourceID: receiverReplacementUUID,
		Commit:     receiverReplacementCommit,
		TreeDigest: receiverReplacementDigest,
	}
	document := `{"resource_id":"` + receiverReplacementUUID + `","commit":"` + receiverReplacementCommit + `","tree_digest":"` + receiverReplacementDigest + `"}`

	receiver := original
	if err := json.Unmarshal([]byte(document), &receiver); err != nil {
		t.Fatalf("rejected valid subject %s: %v", document, err)
	}
	if receiver != replacement {
		t.Fatalf("receiver = %+v, want fully replaced %+v", receiver, replacement)
	}
	if receiver.ResourceID == original.ResourceID || receiver.Commit == original.Commit || receiver.TreeDigest == original.TreeDigest {
		t.Fatalf("receiver kept a pre-existing field: got %+v, want every field replaced from %+v", receiver, original)
	}
}
