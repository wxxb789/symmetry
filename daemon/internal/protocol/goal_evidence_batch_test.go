package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers(t *testing.T) {
	for _, fixture := range []string{
		"evidence-batch.duplicate-outer.json",
		"evidence-batch.duplicate-item.json",
	} {
		t.Run(fixture, func(t *testing.T) {
			if err := RejectDuplicateJSONMembers(readProtocolFixture(t, fixture)); err == nil || !strings.Contains(err.Error(), "duplicate JSON object member") {
				t.Fatalf("duplicate member error = %v", err)
			}
		})
	}
}

func TestRejectDuplicateJSONMembersCanonicalizesEscapedNames(t *testing.T) {
	data := []byte(`{"payload":"provider-token-do-not-leak","\u0070ayload":"safe"}`)
	if err := RejectDuplicateJSONMembers(data); err == nil || !strings.Contains(err.Error(), "duplicate JSON object member") {
		t.Fatalf("escaped duplicate member error = %v", err)
	}
}

func readProtocolFixture(t *testing.T, name string) []byte {
	t.Helper()
	root := contractRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "daemon", "internal", "protocol", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
