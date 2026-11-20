package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestCanonicalizeJSONLineSeparatorsMatchRawAndEscapedForms(t *testing.T) {
	actual := []byte("{\"value\":\"\u2028\u2029\"}")
	sourceEscaped := []byte(`{"value":"\u2028\u2029"}`)
	want := []byte("{\"value\":\"\u2028\u2029\"}")

	canonicalActual, err := CanonicalizeJSON(actual)
	if err != nil {
		t.Fatal(err)
	}
	canonicalEscaped, err := CanonicalizeJSON(sourceEscaped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonicalActual, want) {
		t.Fatalf("canonical raw line separators = %q, want %q", canonicalActual, want)
	}
	if !bytes.Equal(canonicalEscaped, want) {
		t.Fatalf("canonical escaped line separators = %q, want %q", canonicalEscaped, want)
	}

	digest := sha256.Sum256(canonicalActual)
	gotHash := "sha256:" + hex.EncodeToString(digest[:])
	const wantHash = "sha256:e4b5c75b2252f67fcdd6283f4fffd8df9f34ef2463fecd357bdcfe78cd42dbbd"
	if gotHash != wantHash {
		t.Fatalf("canonical line-separator hash = %s, want %s", gotHash, wantHash)
	}
	digest = sha256.Sum256(canonicalEscaped)
	if escapedHash := "sha256:" + hex.EncodeToString(digest[:]); escapedHash != wantHash {
		t.Fatalf("escaped line-separator hash = %s, want %s", escapedHash, wantHash)
	}
}

func TestCanonicalizeJSONPreservesLiteralUnicodeEscapeText(t *testing.T) {
	input := []byte(`{"value":"\\u2028\\u2029"}`)
	want := []byte(`{"value":"\\u2028\\u2029"}`)

	canonical, err := CanonicalizeJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, want) {
		t.Fatalf("canonical literal escapes = %q, want %q", canonical, want)
	}

	var decoded map[string]string
	if err := json.Unmarshal(canonical, &decoded); err != nil {
		t.Fatalf("decode canonical literal escapes: %v", err)
	}
	if decoded["value"] != `\u2028\u2029` {
		t.Fatalf("decoded literal escapes = %q, want %q", decoded["value"], `\u2028\u2029`)
	}
}

func TestCanonicalizeJSONStringEscapingAndUnicodeCoverage(t *testing.T) {
	input := []byte(`{"unicode":"é/你好/😀","html":"<>&","quote":"\"","control":"\u0000\b\f\n\r\t"}`)
	want := []byte(`{"control":"\u0000\b\f\n\r\t","html":"<>&","quote":"\"","unicode":"é/你好/😀"}`)

	canonical, err := CanonicalizeJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, want) {
		t.Fatalf("canonical string coverage = %q, want %q", canonical, want)
	}
}

func TestDecodeStrictObjectRejectsEscapedDuplicateMembers(t *testing.T) {
	var target struct {
		Value int `json:"value"`
	}
	if err := decodeStrictObject([]byte(`{"value":1,"\u0076alue":2}`), &target, "value"); err == nil {
		t.Fatalf("decodeStrictObject accepted escaped duplicate member: %+v", target)
	}
}

func TestCanonicalizeJSONNormalizesNumericNegativeZeroOnly(t *testing.T) {
	negativeZero, err := CanonicalizeJSON([]byte(`{"value":-0}`))
	if err != nil {
		t.Fatal(err)
	}
	zero, err := CanonicalizeJSON([]byte(`{"value":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(negativeZero, zero) {
		t.Fatalf("canonical -0 = %q, canonical 0 = %q", negativeZero, zero)
	}
	if string(negativeZero) != `{"value":0}` {
		t.Fatalf("canonical -0 = %q, want %q", negativeZero, `{"value":0}`)
	}

	stringNegativeZero, err := CanonicalizeJSON([]byte(`{"value":"-0"}`))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(stringNegativeZero, zero) {
		t.Fatal("canonical string \"-0\" unexpectedly matched numeric 0")
	}
	if string(stringNegativeZero) != `{"value":"-0"}` {
		t.Fatalf("canonical string -0 = %q", stringNegativeZero)
	}
}

// The control plane hashes its canonical JSON with Jason
// (control/lib/symmetry_control/request_hash.ex, mirrored by
// contracts/ts/semantics.ts and its boundary test), and Jason spells every C0
// control without a shorthand escape as an uppercase \u00XX escape. The escape
// spelling is part of the bytes a digest covers, so a Go lowercase spelling
// would give one logical value two canonical digests.
func TestCanonicalizeJSONEscapesControlCharactersLikeTheControlPlane(t *testing.T) {
	want := []byte(`{"value":"\u001F"}`)
	for _, test := range []struct{ name, input string }{
		{"uppercase input escape", `{"value":"\u001F"}`},
		{"lowercase input escape", `{"value":"\u001f"}`},
	} {
		canonical, err := CanonicalizeJSON([]byte(test.input))
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if !bytes.Equal(canonical, want) {
			t.Fatalf("canonical %s = %q, want %q", test.name, canonical, want)
		}
		digest := sha256.Sum256(canonical)
		const wantHash = "sha256:520bd0e56ee01fe3ec0b1c8d5ac7cfc0c2aea0999e9de295553abe6e45dcff63"
		if gotHash := "sha256:" + hex.EncodeToString(digest[:]); gotHash != wantHash {
			t.Fatalf("canonical control-character hash = %s, want %s", gotHash, wantHash)
		}
	}

	hexLetters, err := CanonicalizeJSON([]byte(`{"value":"\u000b\u000e\u000f\u001a\u001b\u001c\u001d\u001e\u001f"}`))
	if err != nil {
		t.Fatal(err)
	}
	wantHexLetters := []byte(`{"value":"\u000B\u000E\u000F\u001A\u001B\u001C\u001D\u001E\u001F"}`)
	if !bytes.Equal(hexLetters, wantHexLetters) {
		t.Fatalf("canonical hex-letter escapes = %q, want %q", hexLetters, wantHexLetters)
	}

	// The document the contracts boundary test pins (contracts/scripts/
	// boundary.test.mjs: "canonical JSON escapes control characters exactly as
	// the control plane's Jason does"); the digest below is Jason 1.4.5's over
	// the same canonical text.
	document, err := CanonicalizeJSON([]byte(`{"schema_version":"symmetry.plan.v1","items":[{"description":"before\u000Bafter\u001Bend"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	wantDocument := []byte(`{"items":[{"description":"before\u000Bafter\u001Bend"}],"schema_version":"symmetry.plan.v1"}`)
	if !bytes.Equal(document, wantDocument) {
		t.Fatalf("canonical boundary document = %q, want %q", document, wantDocument)
	}
	documentDigest := sha256.Sum256(document)
	const wantDocumentHash = "sha256:401bf74e8085ac0cce504b465cabbc82043e4fc8c6ec73625d60a43292656fd8"
	if gotHash := "sha256:" + hex.EncodeToString(documentDigest[:]); gotHash != wantDocumentHash {
		t.Fatalf("canonical boundary document hash = %s, want %s", gotHash, wantDocumentHash)
	}

	literalEscapeText, err := CanonicalizeJSON([]byte(`{"value":"\\u001f"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(literalEscapeText, []byte(`{"value":"\\u001f"}`)) {
		t.Fatalf("canonical literal escape text = %q, want %q", literalEscapeText, `{"value":"\\u001f"}`)
	}
}

// The reachable seam: the daemon recomputes this canonical text before it
// accepts a plan proposal and refuses it when the digest differs from the one
// the control plane stamped with Jason
// (goal_contract_semantics.go canonicalPlanProposalSHA256, "proposal_hash does
// not match canonical proposal"; the same rule for control-bearing free text
// in a context snapshot at goal_schema.go validateContextSnapshotContentHash
// and control/validation.go validateGoalContextContentHash). The expectation is
// the control plane's digest already recorded by the frontend pin
// (frontend/src/features/goals/goal-attention.test.tsx:751-790).
func TestCanonicalizeJSONPlanProposalHashMatchesTheControlPlaneDigest(t *testing.T) {
	canonical, err := CanonicalizeJSON([]byte(`{"schema_version":"symmetry.plan.v1","items":[{"description":"before\u000Bafter\u001Bend","integration":false,"change_target":null}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"items":[{"change_target":null,"description":"before\u000Bafter\u001Bend","integration":false}],"schema_version":"symmetry.plan.v1"}`)
	if !bytes.Equal(canonical, want) {
		t.Fatalf("canonical plan proposal = %q, want %q", canonical, want)
	}
	digest := sha256.Sum256(canonical)
	const wantHash = "sha256:6a41b487e2e260e048b11ea7e3221f53aa9705fc5a9d943a353e6b8c39d202e5"
	if gotHash := "sha256:" + hex.EncodeToString(digest[:]); gotHash != wantHash {
		t.Fatalf("canonical plan proposal hash = %s, want %s", gotHash, wantHash)
	}
}
