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
