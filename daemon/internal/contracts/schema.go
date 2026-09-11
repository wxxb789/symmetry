// Package contracts contains generated Goal 0006 wire DTOs and their embedded
// Draft 7 schema bundle. It is deliberately transport-only.
package contracts

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"
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

// Decode validates raw JSON against the selected schema before unmarshalling it
// into a generated DTO. Existing protocol types may then convert that DTO into
// semantic types and apply cross-field invariants.
func Decode(envelope Envelope, data []byte, target any) error {
	if err := Validate(envelope, data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s generated DTO: %w", envelope, err)
	}
	return nil
}

// Validate is the fail-closed raw JSON boundary for Goal 0006 envelopes.
func Validate(envelope Envelope, data []byte) error {
	if err := ValidateSchema(envelope, data); err != nil {
		return err
	}

	instance, err := decodeJSON(data)
	if err != nil {
		return fmt.Errorf("decode %s JSON: %w", envelope, err)
	}
	switch envelope {
	case EnvelopeAdmission:
		if err := validateProviderScope(instance); err != nil {
			return fmt.Errorf("validate %s provider_scope: %w", envelope, err)
		}
	case EnvelopeContextSnapshot:
		if err := validateContextSnapshotPredicateIDs(instance); err != nil {
			return fmt.Errorf("validate %s predicate IDs: %w", envelope, err)
		}
	case EnvelopeGoalCreate:
		if err := validateGoalCreatePredicateIDs(instance); err != nil {
			return fmt.Errorf("validate %s predicate IDs: %w", envelope, err)
		}
	case EnvelopeGoalCommand:
		if err := validateGoalCommandPredicateIDs(instance); err != nil {
			return fmt.Errorf("validate %s predicate IDs: %w", envelope, err)
		}
	case EnvelopeGoalRevision:
		if err := validateGoalRevisionContractSemantics(instance); err != nil {
			return fmt.Errorf("validate %s contract: %w", envelope, err)
		}
	case EnvelopePlanProposal:
		if err := validatePlanProposalPredicateIDs(instance); err != nil {
			return fmt.Errorf("validate %s predicate IDs: %w", envelope, err)
		}
	}
	return nil
}

// ValidateSchema validates only the portable Draft 7 contract. Fixtures use
// this to distinguish structural validity from documented cross-field
// semantics that are enforced by Validate at an executable boundary.
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

// validateProviderScope retains the cross-field invariant that Draft 7 cannot
// express with the repository's portable schema subset: every scoped resource
// has exactly one independently bounded operation set, and no other resource
// receives an operation grant.
func validateProviderScope(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("admission must be an object")
	}

	rawScope, present := root["provider_scope"]
	if !present || rawScope == nil {
		return nil
	}
	scope, ok := rawScope.(map[string]any)
	if !ok {
		return fmt.Errorf("must be an object or null")
	}

	rawResourceIDs, ok := scope["resource_ids"].([]any)
	if !ok {
		return fmt.Errorf("resource_ids must be an array")
	}
	resourceIDs := make(map[string]struct{}, len(rawResourceIDs))
	for _, rawResourceID := range rawResourceIDs {
		resourceID, ok := rawResourceID.(string)
		if !ok {
			return fmt.Errorf("resource_ids must contain strings")
		}
		resourceIDs[resourceID] = struct{}{}
	}

	rawOperations, ok := scope["operations_by_resource"].(map[string]any)
	if !ok {
		return fmt.Errorf("operations_by_resource must be an object")
	}
	if len(resourceIDs) != len(rawOperations) {
		return fmt.Errorf("operations_by_resource keys must equal resource_ids")
	}
	for resourceID := range rawOperations {
		if _, ok := resourceIDs[resourceID]; !ok {
			return fmt.Errorf("operations_by_resource key %q is not a scoped resource", resourceID)
		}
	}
	for resourceID := range resourceIDs {
		if _, ok := rawOperations[resourceID]; !ok {
			return fmt.Errorf("scoped resource %q has no operation set", resourceID)
		}
	}
	return validateProviderChangeTarget(scope["change_target"])
}

func validateGoalCreatePredicateIDs(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("goal create must be an object")
	}
	revision, ok := root["initial_revision"].(map[string]any)
	if !ok {
		return fmt.Errorf("initial_revision must be an object")
	}
	return validateGoalRevisionContractSemantics(revision)
}

func validateContextSnapshotPredicateIDs(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("context snapshot must be an object")
	}
	workContract, ok := root["work_contract"].(map[string]any)
	if !ok {
		return fmt.Errorf("work_contract must be an object")
	}
	if err := validateAcceptancePredicateIDs(workContract["acceptance"]); err != nil {
		return err
	}
	return validateProviderChangeTarget(workContract["change_target"])
}

func validateGoalCommandPredicateIDs(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("goal command must be an object")
	}
	payload, ok := root["payload"].(map[string]any)
	if !ok {
		return fmt.Errorf("payload must be an object")
	}
	switch root["kind"] {
	case "amend":
		revision, ok := payload["revision_contract"].(map[string]any)
		if !ok {
			return fmt.Errorf("revision_contract must be an object")
		}
		return validateGoalRevisionContractSemantics(revision)
	case "accept_plan":
		if err := validatePlanProposalPredicateIDs(payload["proposal"]); err != nil {
			return err
		}
		proposalHash, ok := payload["proposal_hash"].(string)
		if !ok {
			return fmt.Errorf("proposal_hash must be a string")
		}
		expected, err := canonicalPlanProposalSHA256(payload["proposal"])
		if err != nil {
			return fmt.Errorf("canonicalize proposal: %w", err)
		}
		if proposalHash != expected {
			return fmt.Errorf("proposal_hash does not match canonical proposal")
		}
		return nil
	case "request_decision":
		if payload["kind"] == "plan" {
			return validatePlanProposalPredicateIDs(payload["proposal"])
		}
		return nil
	default:
		return nil
	}
}

func validatePlanProposalPredicateIDs(value any) error {
	proposal, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("plan proposal must be an object")
	}
	items, ok := proposal["items"].([]any)
	if !ok {
		return fmt.Errorf("plan items must be an array")
	}
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return fmt.Errorf("plan item %d must be an object", index)
		}
		if err := validateAcceptancePredicateIDs(item["acceptance"]); err != nil {
			return fmt.Errorf("plan item %d: %w", index, err)
		}
		if err := validateProviderChangeTarget(item["change_target"]); err != nil {
			return fmt.Errorf("plan item %d: %w", index, err)
		}
	}
	return nil
}

func validateProviderChangeTarget(value any) error {
	if value == nil {
		return nil
	}
	target, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("provider change target must be an object")
	}
	if target["kind"] == "branches" && target["source_branch"] == target["target_branch"] {
		return fmt.Errorf("provider change target source_branch and target_branch must differ")
	}
	return nil
}

func validateAcceptancePredicateIDs(value any) error {
	acceptance, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("acceptance contract must be an object")
	}
	predicates, ok := acceptance["predicates"].([]any)
	if !ok {
		return fmt.Errorf("acceptance predicates must be an array")
	}
	seen := make(map[string]struct{}, len(predicates))
	for _, rawPredicate := range predicates {
		predicate, ok := rawPredicate.(map[string]any)
		if !ok {
			return fmt.Errorf("acceptance predicate must be an object")
		}
		id, ok := predicate["id"].(string)
		if !ok {
			return fmt.Errorf("acceptance predicate id must be a string")
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate acceptance predicate id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateGoalRevisionContractSemantics(value any) error {
	revision, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("goal revision contract must be an object")
	}
	acceptance, ok := revision["acceptance_contract"].(map[string]any)
	if !ok {
		return fmt.Errorf("acceptance_contract must be an object")
	}
	if err := validateAcceptancePredicateIDs(acceptance); err != nil {
		return err
	}
	executionPolicy, ok := revision["execution_policy"].(map[string]any)
	if !ok {
		return fmt.Errorf("execution_policy must be an object")
	}
	authorityPolicy, ok := revision["authority_policy"].(map[string]any)
	if !ok {
		return fmt.Errorf("authority_policy must be an object")
	}
	if executionPolicy["final_acceptance"] != "deterministic" || authorityPolicy["operator_required_for_completion"] == true {
		return nil
	}
	predicates, ok := acceptance["predicates"].([]any)
	if !ok {
		return fmt.Errorf("acceptance predicates must be an array")
	}
	for _, rawPredicate := range predicates {
		predicate, ok := rawPredicate.(map[string]any)
		if !ok {
			return fmt.Errorf("acceptance predicate must be an object")
		}
		kind, ok := predicate["kind"].(string)
		if !ok || (kind != "check" && kind != "artifact") {
			return fmt.Errorf("deterministic acceptance includes a non-machine predicate")
		}
	}
	return nil
}

func canonicalSHA256(value any) (string, error) {
	encoded, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest), nil
}

func canonicalPlanProposalSHA256(value any) (string, error) {
	normalized, err := normalizePlanProposal(value)
	if err != nil {
		return "", err
	}
	return canonicalSHA256(normalized)
}

func normalizePlanProposal(value any) (map[string]any, error) {
	proposal, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("plan proposal must be an object")
	}
	normalized := make(map[string]any, len(proposal))
	for key, item := range proposal {
		normalized[key] = item
	}
	rawItems, ok := proposal["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("plan items must be an array")
	}
	items := make([]any, len(rawItems))
	for index, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("plan item %d must be an object", index)
		}
		normalizedItem := make(map[string]any, len(item)+1)
		for key, child := range item {
			normalizedItem[key] = child
		}
		if _, present := normalizedItem["integration"]; !present {
			normalizedItem["integration"] = false
		}
		items[index] = normalizedItem
	}
	normalized["items"] = items
	return normalized, nil
}

func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	if err := appendCanonicalJSON(&buffer, value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func appendCanonicalJSON(buffer *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case nil:
		buffer.WriteString("null")
	case bool:
		buffer.WriteString(fmt.Sprintf("%t", value))
	case string:
		return appendCanonicalJSONString(buffer, value)
	case json.Number:
		if value.String() == "-0" {
			buffer.WriteByte('0')
		} else {
			buffer.WriteString(value.String())
		}
	case []any:
		buffer.WriteByte('[')
		for index, item := range value {
			if index > 0 {
				buffer.WriteByte(',')
			}
			if err := appendCanonicalJSON(buffer, item); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buffer.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				buffer.WriteByte(',')
			}
			if err := appendCanonicalJSONString(buffer, key); err != nil {
				return err
			}
			buffer.WriteByte(':')
			if err := appendCanonicalJSON(buffer, value[key]); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON value %T", value)
	}
	return nil
}

func appendCanonicalJSONString(buffer *bytes.Buffer, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("string is not valid UTF-8")
	}
	buffer.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"':
			buffer.WriteString(`\"`)
		case '\\':
			buffer.WriteString(`\\`)
		case '\b':
			buffer.WriteString(`\b`)
		case '\f':
			buffer.WriteString(`\f`)
		case '\n':
			buffer.WriteString(`\n`)
		case '\r':
			buffer.WriteString(`\r`)
		case '\t':
			buffer.WriteString(`\t`)
		default:
			if character < 0x20 {
				fmt.Fprintf(buffer, `\u%04x`, character)
			} else {
				buffer.WriteRune(character)
			}
		}
	}
	buffer.WriteByte('"')
	return nil
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
