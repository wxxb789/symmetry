package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/contracts"
)

// nativeUnsupportedSchemaKeywords are intentionally absent from a projected
// Codex output schema. Native structured outputs accept only a strict JSON
// Schema subset; the canonical contract (validated again after the turn) keeps
// these constraints authoritative. Removing them here never weakens admission
// because ParseTaskResult still validates the exact canonical envelope.
var nativeUnsupportedSchemaKeywords = map[string]struct{}{
	"$schema": {}, "$id": {}, "uniqueItems": {}, "minItems": {}, "maxItems": {},
	"minLength": {}, "maxLength": {}, "pattern": {}, "format": {},
	"minimum": {}, "maximum": {}, "exclusiveMinimum": {}, "exclusiveMaximum": {},
	"multipleOf": {}, "minProperties": {}, "maxProperties": {}, "propertyNames": {},
	"dependencies": {}, "dependentRequired": {}, "dependentSchemas": {},
	"if": {}, "then": {}, "else": {}, "not": {}, "contains": {},
	"prefixItems": {}, "unevaluatedProperties": {}, "unevaluatedItems": {},
	"patternProperties": {}, "deprecated": {}, "examples": {}, "default": {},
	"readOnly": {}, "writeOnly": {}, "contentEncoding": {}, "contentMediaType": {},
}

// nativeTaskResultSchema returns a strict-structured-output projection of the
// canonical TaskResult contract. It preserves object shape and property value
// domains that the native provider can express; maps and cross-field
// exclusivity stay with the canonical validator.
func nativeTaskResultSchema() (map[string]any, error) {
	canonical, err := contracts.TaskResultSchema()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("encode canonical TaskResult schema: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode canonical TaskResult schema: %w", err)
	}
	if err := projectNativeSchema(root); err != nil {
		return nil, err
	}
	return root, nil
}

// projectNativeSchema mutates one schema document into the provider's strict
// subset. It fails closed when a reachable construct has no faithful
// projection instead of silently dropping data-bearing structure.
func projectNativeSchema(root map[string]any) error {
	definitions, _ := root["definitions"].(map[string]any)
	delete(root, "definitions")
	reachable := reachableDefinitions(root, definitions)
	rewriteSchemaRefs(root)
	for _, definition := range reachable {
		rewriteSchemaRefs(definition)
	}
	if len(reachable) != 0 {
		root["$defs"] = reachable
	}
	// A top-level anyOf/oneOf is rejected outright by strict structured
	// outputs; the canonical envelope validator keeps that exclusivity.
	delete(root, "anyOf")
	delete(root, "oneOf")
	if err := normalizeNativeSchema(root); err != nil {
		return err
	}
	return nil
}

func reachableDefinitions(root map[string]any, definitions map[string]any) map[string]any {
	if len(definitions) == 0 {
		return nil
	}
	reachable := make(map[string]any)
	queue := schemaRefNames(root)
	for len(queue) != 0 {
		name := queue[0]
		queue = queue[1:]
		if _, seen := reachable[name]; seen {
			continue
		}
		definition, ok := definitions[name]
		if !ok {
			continue
		}
		reachable[name] = definition
		queue = append(queue, schemaRefNames(definition)...)
	}
	return reachable
}

func schemaRefNames(node any) []string {
	var names []string
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if ref, ok := typed["$ref"].(string); ok {
				const prefix = "#/definitions/"
				if strings.HasPrefix(ref, prefix) {
					names = append(names, strings.TrimPrefix(ref, prefix))
				}
			}
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(node)
	return names
}

func rewriteSchemaRefs(node any) {
	switch typed := node.(type) {
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok && strings.HasPrefix(ref, "#/definitions/") {
			typed["$ref"] = "#/$defs/" + strings.TrimPrefix(ref, "#/definitions/")
		}
		for _, child := range typed {
			rewriteSchemaRefs(child)
		}
	case []any:
		for _, child := range typed {
			rewriteSchemaRefs(child)
		}
	}
}

// normalizeNativeSchema visits schema positions only. Property maps and $defs
// containers are not schemas themselves and must not be rewritten.
func normalizeNativeSchema(schema any) error {
	object, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	for keyword := range nativeUnsupportedSchemaKeywords {
		delete(object, keyword)
	}
	if oneOf, ok := object["oneOf"]; ok {
		object["anyOf"] = oneOf
		delete(object, "oneOf")
	}
	if _, ok := object["type"]; !ok {
		switch {
		case object["const"] != nil:
			object["type"] = jsonSchemaKind(object["const"])
		default:
			if enum, ok := object["enum"].([]any); ok {
				if kind, uniform := uniformEnumKind(enum); uniform {
					object["type"] = kind
				}
			}
		}
	}
	if properties, ok := object["properties"].(map[string]any); ok {
		if _, exists := object["type"]; !exists {
			object["type"] = "object"
		}
		object["additionalProperties"] = false
		required := make(map[string]struct{})
		for _, name := range stringSlice(object["required"]) {
			required[name] = struct{}{}
		}
		names := make([]string, 0, len(properties))
		for name, property := range properties {
			names = append(names, name)
			if _, isRequired := required[name]; !isRequired {
				makeNativePropertyNullable(property)
			}
		}
		sort.Strings(names)
		object["required"] = names
		for _, property := range properties {
			if err := normalizeNativeSchema(property); err != nil {
				return err
			}
		}
	}
	for _, keyword := range []string{"anyOf", "oneOf", "allOf"} {
		branches, _ := object[keyword].([]any)
		for _, branch := range branches {
			if err := normalizeNativeSchema(branch); err != nil {
				return err
			}
		}
	}
	if items, ok := object["items"]; ok {
		if err := normalizeNativeSchema(items); err != nil {
			return err
		}
	}
	if additional, ok := object["additionalProperties"].(map[string]any); ok {
		if _, hasProperties := object["properties"]; !hasProperties {
			return errors.New("schema-valued additionalProperties has no strict structured-output projection")
		}
		if err := normalizeNativeSchema(additional); err != nil {
			return err
		}
	}
	if definitions, ok := object["$defs"].(map[string]any); ok {
		for name, definition := range definitions {
			if err := normalizeNativeSchema(definition); err != nil {
				return fmt.Errorf("normalize $defs/%s: %w", name, err)
			}
		}
	}
	return nil
}

func jsonSchemaKind(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case json.Number, float64, int, int64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	default:
		return "string"
	}
}

func uniformEnumKind(values []any) (string, bool) {
	kind := ""
	for _, value := range values {
		if value == nil {
			continue
		}
		current := jsonSchemaKind(value)
		if kind == "" {
			kind = current
			continue
		}
		if kind != current {
			return "", false
		}
	}
	return kind, kind != ""
}

func stringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		names := make([]string, 0, len(values))
		for _, entry := range values {
			if name, ok := entry.(string); ok {
				names = append(names, name)
			}
		}
		return names
	default:
		return nil
	}
}

func makeNativePropertyNullable(property any) {
	object, ok := property.(map[string]any)
	if !ok {
		return
	}
	if current, ok := object["type"]; ok && current == "null" {
		return
	}
	if current, ok := object["type"].(string); ok {
		object["type"] = []any{current, "null"}
		return
	}
	if current, ok := object["type"].([]any); ok {
		if !schemaContainsString(current, "null") {
			object["type"] = append(current, "null")
		}
		return
	}
	if _, ok := object["anyOf"]; ok {
		appendNullBranch(object)
		return
	}
	if enum, ok := object["enum"].([]any); ok {
		object["enum"] = append(enum, nil)
		if kind, uniform := uniformEnumKind(enum); uniform {
			object["type"] = []any{kind, "null"}
		}
		return
	}
	if constant, ok := object["const"]; ok {
		delete(object, "const")
		object["type"] = []any{jsonSchemaKind(constant), "null"}
		object["enum"] = []any{constant, nil}
		return
	}
	if ref, ok := object["$ref"]; ok {
		branch := map[string]any{"$ref": ref}
		delete(object, "$ref")
		object["anyOf"] = []any{branch, map[string]any{"type": "null"}}
		return
	}
	if _, ok := object["properties"]; ok {
		object["type"] = []any{"object", "null"}
		return
	}
	if _, ok := object["items"]; ok {
		object["type"] = []any{"array", "null"}
		return
	}
	object["type"] = []any{"string", "number", "boolean", "object", "array", "null"}
}

func appendNullBranch(object map[string]any) {
	branches, _ := object["anyOf"].([]any)
	for _, branch := range branches {
		if entry, ok := branch.(map[string]any); ok && entry["type"] == "null" {
			return
		}
	}
	object["anyOf"] = append(branches, map[string]any{"type": "null"})
}

func schemaContainsString(values []any, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// canonicalRequiredResultPaths records object properties that the canonical
// TaskResult requires at each output path. Optional properties become required
// and nullable in the native projection; nulls for those paths are removed
// before canonical validation so "absent" and "explicitly null" stay distinct.
func canonicalRequiredResultPaths() (map[string]struct{}, error) {
	canonical, err := contracts.TaskResultSchema()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}
	definitions, _ := root["definitions"].(map[string]any)
	required := make(map[string]struct{})
	var visit func(any, string, map[string]struct{})
	visit = func(node any, path string, visiting map[string]struct{}) {
		object, ok := node.(map[string]any)
		if !ok {
			return
		}
		if ref, ok := object["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/definitions/")
			if _, cycle := visiting[name]; cycle {
				return
			}
			if definition, ok := definitions[name]; ok {
				next := make(map[string]struct{}, len(visiting)+1)
				for key := range visiting {
					next[key] = struct{}{}
				}
				next[name] = struct{}{}
				visit(definition, path, next)
				return
			}
		}
		if properties, ok := object["properties"].(map[string]any); ok {
			requiredNames := make(map[string]struct{})
			for _, name := range stringSlice(object["required"]) {
				requiredNames[name] = struct{}{}
				required[path+"."+name] = struct{}{}
			}
			for name, property := range properties {
				visit(property, path+"."+name, visiting)
			}
		}
		for _, keyword := range []string{"anyOf", "oneOf", "allOf"} {
			branches, _ := object[keyword].([]any)
			for _, branch := range branches {
				visit(branch, path, visiting)
			}
		}
		if items, ok := object["items"]; ok {
			visit(items, path+"[]", visiting)
		}
		if additional, ok := object["additionalProperties"].(map[string]any); ok {
			visit(additional, path+".*", visiting)
		}
	}
	visit(root, "$", map[string]struct{}{})
	return required, nil
}

func pruneOptionalNulls(value any, path string, required map[string]struct{}) (any, bool) {
	changed := false
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := path + "." + key
			if child == nil {
				if _, keep := required[childPath]; !keep {
					delete(typed, key)
					changed = true
				}
				continue
			}
			pruned, childChanged := pruneOptionalNulls(child, childPath, required)
			typed[key] = pruned
			changed = changed || childChanged
		}
	case []any:
		for index, child := range typed {
			pruned, childChanged := pruneOptionalNulls(child, path+"[]", required)
			typed[index] = pruned
			changed = changed || childChanged
		}
	}
	return value, changed
}

// normalizeNativeTaskResultText removes strict-mode null placeholders for
// optional contract fields. The exact canonical envelope still validates the
// remainder; required nullable fields are preserved.
func normalizeNativeTaskResultText(text string) ([]byte, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	required, err := canonicalRequiredResultPaths()
	if err != nil {
		return nil, err
	}
	value, changed := pruneOptionalNulls(value, "$", required)
	if !changed {
		return []byte(text), nil
	}
	return json.Marshal(value)
}
