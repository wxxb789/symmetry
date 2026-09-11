// Package contracts contains generated Goal 0006 wire DTOs and their embedded
// Draft 7 schema bundle. It is deliberately transport-only.
package contracts

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

const schemaBaseURL = "https://symmetry.invalid/contracts/v1/"

// Envelope names the v1 envelope schema used at a Goal protocol boundary.
type Envelope string

const (
	EnvelopeAdapterCapabilities Envelope = "adapter-capabilities"
	EnvelopeAdmission           Envelope = "admission"
	EnvelopeContextSnapshot     Envelope = "context-snapshot"
	EnvelopeDecision            Envelope = "decision"
	EnvelopeEvidence            Envelope = "evidence"
	EnvelopeGoalCommand         Envelope = "goal-command"
	EnvelopeGoalCreate          Envelope = "goal-create"
	EnvelopeGoalRevision        Envelope = "goal-revision"
	EnvelopePlanProposal        Envelope = "plan-proposal"
	EnvelopeTaskResult          Envelope = "task-result"
	EnvelopeUsage               Envelope = "usage"
)

//go:embed schema_bundle.json
var schemaBundle []byte

var (
	schemasOnce sync.Once
	schemas     map[Envelope]*jsonschema.Schema
	schemasErr  error
	commitPath  *regexp2.Regexp
)

// DecodeSchema validates strict raw JSON against the selected schema before
// unmarshalling it into a generated DTO. Protocol owns all cross-field and
// envelope semantics.
func DecodeSchema(envelope Envelope, data []byte, target any) error {
	if _, err := decodeJSON(data); err != nil {
		return fmt.Errorf("decode %s JSON: %w", envelope, err)
	}
	if err := ValidateSchema(envelope, data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s generated DTO: %w", envelope, err)
	}
	return nil
}

// ValidateSchema validates only the portable Draft 7 contract. Fixtures use
// this to distinguish structural validity from documented cross-field
// semantics that protocol enforces at its executable boundary.
func ValidateSchema(envelope Envelope, data []byte) error {
	if err := ensureSchemas(); err != nil {
		return err
	}

	instance, err := decodeSchemaJSON(data)
	if err != nil {
		return fmt.Errorf("decode %s JSON: %w", envelope, err)
	}
	schema, ok := schemas[envelope]
	if !ok {
		return fmt.Errorf("unsupported Goal envelope schema %q", envelope)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("validate %s schema: %w", envelope, err)
	}
	if err := validateCommitPaths(instance); err != nil {
		return fmt.Errorf("validate %s CommitPath: %w", envelope, err)
	}
	return nil
}

// TaskResultSchema returns a fresh copy of the canonical embedded output
// schema used for native structured-output requests. Callers may not mutate
// the package-owned schema bundle.
func TaskResultSchema() (map[string]any, error) {
	return schemaDocument(EnvelopeTaskResult)
}

func schemaDocument(envelope Envelope) (map[string]any, error) {
	var bundle map[string]json.RawMessage
	if err := json.Unmarshal(schemaBundle, &bundle); err != nil {
		return nil, fmt.Errorf("decode embedded schema bundle: %w", err)
	}
	filename := string(envelope) + ".schema.json"
	source, ok := bundle[filename]
	if !ok {
		return nil, fmt.Errorf("unsupported Goal envelope schema %q", envelope)
	}
	var document map[string]any
	if err := json.Unmarshal(source, &document); err != nil {
		return nil, fmt.Errorf("decode embedded %s: %w", filename, err)
	}
	return document, nil
}

func ensureSchemas() error {
	schemasOnce.Do(func() {
		schemas, schemasErr = compileSchemas()
	})
	return schemasErr
}

func compileSchemas() (map[Envelope]*jsonschema.Schema, error) {
	pattern, err := regexp2.Compile(commitPathPattern, regexp2.ECMAScript)
	if err != nil {
		return nil, fmt.Errorf("compile CommitPath schema pattern: %w", err)
	}
	pattern.MatchTimeout = 10 * time.Millisecond

	var bundle map[string]json.RawMessage
	if err := json.Unmarshal(schemaBundle, &bundle); err != nil {
		return nil, fmt.Errorf("decode embedded schema bundle: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7
	for filename, source := range bundle {
		schema, err := rewriteRuntimeSchema(filename, source)
		if err != nil {
			return nil, err
		}
		if err := compiler.AddResource(schemaBaseURL+filename, bytes.NewReader(schema)); err != nil {
			return nil, fmt.Errorf("register %s: %w", filename, err)
		}
	}

	compiled := make(map[Envelope]*jsonschema.Schema, 11)
	for _, envelope := range []Envelope{
		EnvelopeAdapterCapabilities,
		EnvelopeAdmission,
		EnvelopeContextSnapshot,
		EnvelopeDecision,
		EnvelopeEvidence,
		EnvelopeGoalCommand,
		EnvelopeGoalCreate,
		EnvelopeGoalRevision,
		EnvelopePlanProposal,
		EnvelopeTaskResult,
		EnvelopeUsage,
	} {
		filename := string(envelope) + ".schema.json"
		schema, err := compiler.Compile(schemaBaseURL + filename)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", filename, err)
		}
		compiled[envelope] = schema
	}
	commitPath = pattern
	return compiled, nil
}

func rewriteRuntimeSchema(filename string, source json.RawMessage) ([]byte, error) {
	var schema map[string]any
	if err := json.Unmarshal(source, &schema); err != nil {
		return nil, fmt.Errorf("decode bundled %s: %w", filename, err)
	}

	// The source schema has relative IDs for cross-language portability. The
	// embedded compiler needs stable absolute resource IDs to resolve refs
	// without the process working directory or a network loader.
	schema["$id"] = schemaBaseURL + filename
	rewriteRuntimePatternEscapes(schema)
	definitions, ok := schema["definitions"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s definitions are missing", filename)
	}
	commitPath, ok := definitions["CommitPath"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s CommitPath is missing", filename)
	}
	// Each generated bundle member inlines common definitions. jsonschema/v5
	// delegates patterns to Go's regexp package, which does not implement
	// ECMAScript negative lookahead. Keep the source schema unchanged, use this
	// coarse form only to compile it, then apply the authoritative expression
	// below with a bounded ECMAScript engine.
	commitPath["pattern"] = "^[^\\x00]+$"
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode runtime %s: %w", filename, err)
	}
	return encoded, nil
}

func rewriteRuntimePatternEscapes(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "pattern" {
				if pattern, ok := child.(string); ok {
					value[key] = strings.ReplaceAll(pattern, `\u0000`, `\x{0}`)
				}
				continue
			}
			rewriteRuntimePatternEscapes(child)
		}
	case []any:
		for _, child := range value {
			rewriteRuntimePatternEscapes(child)
		}
	}
}

func decodeJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("JSON input is not valid UTF-8")
	}
	if err := validateUnicodeEscapes(data); err != nil {
		return nil, err
	}
	return decodeSchemaJSON(data)
}

func decodeSchemaJSON(data []byte) (any, error) {
	if err := validateJSONNumberLexemes(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

const maxJSONSafeIntegerLexeme = "9007199254740991"

// validateJSONNumberLexemes preserves the raw-number contract that a decoded
// json.Number cannot express: v1 transports use only exact JSON-safe integers.
// It mirrors contracts/scripts/check.mjs and deliberately skips string content.
func validateJSONNumberLexemes(data []byte) error {
	inString := false
	escaped := false

	for index := 0; index < len(data); index++ {
		character := data[index]
		if inString {
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == '"':
				inString = false
			}
			continue
		}
		if character == '"' {
			inString = true
			continue
		}
		if character != '-' && (character < '0' || character > '9') {
			continue
		}

		end, nonInteger, ok := scanJSONNumber(data, index)
		if !ok {
			continue
		}
		lexeme := string(data[index:end])
		if nonInteger {
			return fmt.Errorf("fractional or exponent number is not canonical: %s", lexeme)
		}
		if !isJSONSafeIntegerLexeme(lexeme) {
			return fmt.Errorf("integer exceeds the JSON safe-integer range: %s", lexeme)
		}
		index = end - 1
	}

	return nil
}

// scanJSONNumber recognizes the same valid number prefix as the Node checker.
// Invalid JSON remains the decoder's responsibility after this lexical pass.
func scanJSONNumber(data []byte, start int) (end int, nonInteger bool, ok bool) {
	index := start
	if data[index] == '-' {
		index++
		if index == len(data) {
			return 0, false, false
		}
	}

	switch character := data[index]; {
	case character == '0':
		index++
	case character >= '1' && character <= '9':
		index++
		for index < len(data) && data[index] >= '0' && data[index] <= '9' {
			index++
		}
	default:
		return 0, false, false
	}

	if index < len(data) && data[index] == '.' {
		fractionStart := index + 1
		index = fractionStart
		for index < len(data) && data[index] >= '0' && data[index] <= '9' {
			index++
		}
		if index != fractionStart {
			nonInteger = true
		} else {
			index--
		}
	}

	if index < len(data) && (data[index] == 'e' || data[index] == 'E') {
		exponentStart := index
		index++
		if index < len(data) && (data[index] == '+' || data[index] == '-') {
			index++
		}
		digitsStart := index
		for index < len(data) && data[index] >= '0' && data[index] <= '9' {
			index++
		}
		if index != digitsStart {
			nonInteger = true
		} else {
			index = exponentStart
		}
	}

	return index, nonInteger, true
}

func isJSONSafeIntegerLexeme(lexeme string) bool {
	digits := lexeme
	if digits[0] == '-' {
		digits = digits[1:]
	}
	if len(digits) != len(maxJSONSafeIntegerLexeme) {
		return len(digits) < len(maxJSONSafeIntegerLexeme)
	}
	return digits <= maxJSONSafeIntegerLexeme
}

const commitPathPattern = `^(?!/)(?!.*(?:^|/)\.\.(?:/|$))(?!.*\\)(?!.*//)[^\u0000]+$`

func validateCommitPaths(value any) error {
	if commitPath == nil {
		return fmt.Errorf("CommitPath schema pattern is unavailable")
	}
	return walkCommitPaths(value, commitPath)
}

func validateUnicodeEscapes(data []byte) error {
	inString := false
	for index := 0; index < len(data); index++ {
		if !inString {
			if data[index] == '"' {
				inString = true
			}
			continue
		}
		if data[index] == '"' {
			inString = false
			continue
		}
		if data[index] != '\\' {
			continue
		}
		index++
		if index >= len(data) {
			return fmt.Errorf("invalid JSON string escape")
		}
		if data[index] != 'u' {
			continue
		}
		codepoint, ok := decodeUnicodeEscape(data, index+1)
		if !ok {
			return fmt.Errorf("invalid JSON unicode escape")
		}
		switch {
		case codepoint >= 0xD800 && codepoint <= 0xDBFF:
			if index+7 >= len(data) || data[index+5] != '\\' || data[index+6] != 'u' {
				return fmt.Errorf("unpaired high surrogate in JSON string")
			}
			low, ok := decodeUnicodeEscape(data, index+7)
			if !ok || low < 0xDC00 || low > 0xDFFF {
				return fmt.Errorf("unpaired high surrogate in JSON string")
			}
			index += 10
		case codepoint >= 0xDC00 && codepoint <= 0xDFFF:
			return fmt.Errorf("unpaired low surrogate in JSON string")
		default:
			index += 4
		}
	}
	return nil
}

func decodeUnicodeEscape(data []byte, start int) (rune, bool) {
	if start+4 > len(data) {
		return 0, false
	}
	var value rune
	for _, character := range data[start : start+4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += rune(character - '0')
		case character >= 'a' && character <= 'f':
			value += rune(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += rune(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func walkCommitPaths(value any, pattern *regexp2.Regexp) error {
	switch value := value.(type) {
	case []any:
		for _, child := range value {
			if err := walkCommitPaths(child, pattern); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, child := range value {
			if key == "path" {
				path, ok := child.(string)
				if !ok {
					return fmt.Errorf("path must be a string")
				}
				matched, err := pattern.MatchString(path)
				if err != nil {
					return fmt.Errorf("path match failed: %w", err)
				}
				if !matched {
					return fmt.Errorf("path does not match the schema pattern")
				}
			}
			if err := walkCommitPaths(child, pattern); err != nil {
				return err
			}
		}
	}
	return nil
}
