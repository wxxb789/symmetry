//go:build linux

package platform

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

// The platform package keeps hand-written deep-clone functions for authority
// and evidence values (P-08 in the phase-0 audit). A clone that copies a
// pointer, slice or map field shallowly makes the clone share mutable state
// with its source, and a field added later without extending the clone turns
// that sharing into an aliasing authority bug no existing test or linter sees.
//
// These tests convert the invariant into a gate: for every covered clone they
// populate all fields of the input by reflection, clone it through the
// production function, then write the mutation marker through every pointer,
// slice element and map entry the original still holds and require the clone's
// rendered content to be unchanged. A field the clone copies shallowly is
// exactly a field whose mutation becomes visible inside the clone, so a newly
// added pointer/slice/map field fails this test red until its clone copies it.
//
// The covered clones split by build universe. This file carries the harness and
// the three clones compiled under //go:build linux; the seven clones compiled
// under //go:build linux && amd64 live in clone_aliasing_linux_amd64_test.go,
// and cloneContainmentSessionID (//go:build windows) in
// clone_aliasing_windows_test.go.

const (
	// cloneAliasingPopulateMarker fills every string field of a covered value.
	cloneAliasingPopulateMarker = "clone-aliasing-marker"
	// cloneAliasingMutatedMarker replaces every string scalar written through
	// a reachable reference.
	cloneAliasingMutatedMarker = "clone-aliasing-mutated"
	// cloneAliasingRenderDepth bounds the rendering of a (cyclic) value graph.
	cloneAliasingRenderDepth = 64
)

var (
	cloneAliasingTimeType      = reflect.TypeOf(time.Time{})
	cloneAliasingExitProofType = reflect.TypeOf(authority.LinuxCgroupExitProof{})
	cloneAliasingFixedTime     = time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
)

// cloneAliasingFieldOverrides pins the fields whose value must satisfy a
// covered type's own Validate so that the Validate-gated clone
// cloneLinuxOwnerGateIdentityIfValid copies instead of returning nil
// (LinuxCgroupGateIdentity.Validate, authority/authority.go:1317). Every other
// field - including any field added later - is populated by kind, so these
// entries pin value formats, not clone coverage.
var cloneAliasingFieldOverrides = map[string]any{
	"authority.LinuxCgroupParentIdentity.BootID":  "12345678-9abc-def0-1234-56789abcdef0",
	"authority.LinuxCgroupParentIdentity.Path":    "/sys/fs/cgroup/clone-aliasing-marker",
	"authority.LinuxCgroupIdentity.Version":       authority.LinuxCgroupIdentityVersion,
	"authority.LinuxCgroupIdentity.LeafName":      strings.Repeat("ab", authority.TokenBytes),
	"authority.LinuxCgroupGateIdentity.BirthKind": authority.LinuxCgroupTargetBirthTrustedAGate,
}

// cloneAliasingCase is one covered clone: fresh builds a zero *T whose pointee
// the driver populates, clone runs the production clone on that value.
type cloneAliasingCase struct {
	name  string
	fresh func() any
	clone func(original reflect.Value) any
}

// cloneAliasingCaseFor registers a clone that takes a *T (or a value the case
// dereferences itself) and returns the cloned value.
func cloneAliasingCaseFor[T any](name string, clone func(value *T) any) cloneAliasingCase {
	return cloneAliasingCase{
		name:  name,
		fresh: func() any { return new(T) },
		clone: func(original reflect.Value) any { return clone(original.Interface().(*T)) },
	}
}

// TestCloneAliasingLinuxCgroupEvidence covers the three cgroup-v2 clone
// functions compiled under //go:build linux.
func TestCloneAliasingLinuxCgroupEvidence(t *testing.T) {
	for _, testCase := range []cloneAliasingCase{
		cloneAliasingCaseFor("cloneLinuxCgroupV2RemovalAuthorization", func(value *LinuxCgroupV2RemovalAuthorization) any {
			return cloneLinuxCgroupV2RemovalAuthorization(*value)
		}),
		cloneAliasingCaseFor("cloneLinuxCgroupV2PreGateRemovalAuthorization", func(value *LinuxCgroupV2PreGateRemovalAuthorization) any {
			return cloneLinuxCgroupV2PreGateRemovalAuthorization(*value)
		}),
		cloneAliasingCaseFor("cloneLinuxCgroupStopReceipt", func(value *authority.StopReceipt) any {
			return cloneLinuxCgroupStopReceipt(*value)
		}),
	} {
		t.Run(testCase.name, func(t *testing.T) { runCloneAliasingCase(t, testCase) })
	}
}

// runCloneAliasingCase is the shared driver: populate, clone, verify content,
// mutate through every reference the original still holds, verify the clone
// again.
func runCloneAliasingCase(t *testing.T, testCase cloneAliasingCase) {
	t.Helper()
	original := reflect.ValueOf(testCase.fresh())
	if original.Kind() != reflect.Ptr || original.IsNil() {
		t.Fatalf("case did not provide a *T original")
	}
	if err := cloneAliasingPopulate(original.Elem()); err != nil {
		t.Fatalf("populate the covered value: %v", err)
	}
	cloned := reflect.ValueOf(testCase.clone(original))
	if !cloned.IsValid() || (cloned.Kind() == reflect.Ptr && cloned.IsNil()) {
		t.Fatalf("clone returned an empty value for a populated original")
	}
	// Both sides are dereferenced once so that a value-returning clone and a
	// pointer-returning clone compare the same covered value.
	before := cloneAliasingRender(cloneAliasingDereference(original))
	if after := cloneAliasingRender(cloneAliasingDereference(cloned)); after != before {
		t.Fatalf("clone content differs from the populated original\n clone: %s\norigin: %s", after, before)
	}
	writes := cloneAliasingMutateReferences(original)
	if writes == 0 {
		t.Fatalf("the mutation exercised no write site; the case cannot observe aliasing")
	}
	if after := cloneAliasingRender(cloneAliasingDereference(cloned)); after != before {
		t.Fatalf("clone shares state with the original: %d write(s) in the original changed the clone\n before: %s\n  after: %s", writes, before, after)
	}
}

// cloneAliasingDereference strips one pointer level so that the covered value
// is compared regardless of whether the clone returns a value or a pointer.
func cloneAliasingDereference(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Ptr && !v.IsNil() {
		return v.Elem()
	}
	return v
}

// cloneAliasingPopulate fills every settable field of v with a non-zero,
// deterministic value: pointers allocate and recurse, slices get two populated
// elements, maps one populated entry, structs recurse field by field. Unknown
// kinds and unexported fields are errors, so a field added later forces a
// harness decision instead of a silent skip.
func cloneAliasingPopulate(v reflect.Value) error {
	if v.Type() == cloneAliasingTimeType {
		if !v.CanSet() {
			return fmt.Errorf("time.Time field is not settable")
		}
		v.Set(reflect.ValueOf(cloneAliasingFixedTime))
		return nil
	}
	switch v.Kind() {
	case reflect.Ptr:
		v.Set(reflect.New(v.Type().Elem()))
		return cloneAliasingPopulate(v.Elem())
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for index := range v.Len() {
			if err := cloneAliasingPopulate(v.Index(index)); err != nil {
				return fmt.Errorf("slice element %d: %w", index, err)
			}
		}
		return nil
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		key, err := cloneAliasingPopulatedValue(v.Type().Key())
		if err != nil {
			return fmt.Errorf("map key: %w", err)
		}
		value, err := cloneAliasingPopulatedValue(v.Type().Elem())
		if err != nil {
			return fmt.Errorf("map value: %w", err)
		}
		v.SetMapIndex(key, value)
		return nil
	case reflect.Struct:
		return cloneAliasingPopulateStruct(v)
	case reflect.Interface:
		marker := reflect.ValueOf(cloneAliasingPopulateMarker)
		if !marker.Type().AssignableTo(v.Type()) {
			return fmt.Errorf("interface type %s cannot hold the marker string", v.Type())
		}
		v.Set(marker)
		return nil
	case reflect.Bool:
		v.SetBool(true)
		return nil
	case reflect.String:
		v.SetString(cloneAliasingPopulateMarker)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(cloneAliasingIntegerSentinel(v.Type()))
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v.SetUint(uint64(cloneAliasingIntegerSentinel(v.Type())))
		return nil
	case reflect.Float32, reflect.Float64:
		v.SetFloat(4242)
		return nil
	default:
		return fmt.Errorf("kind %s is not supported by the aliasing harness", v.Kind())
	}
}

// cloneAliasingPopulateStruct populates one struct value field by field.
func cloneAliasingPopulateStruct(v reflect.Value) error {
	t := v.Type()
	for index := range t.NumField() {
		field, target := t.Field(index), v.Field(index)
		if !target.CanSet() {
			return fmt.Errorf("struct %s field %s is not settable; extend the aliasing harness", t, field.Name)
		}
		if t == cloneAliasingExitProofType && field.Name == "HelperExit" {
			// Deliberately nil: LinuxCgroupExitProof.Validate rejects a nested
			// helper exit (authority/authority.go:1164), so the shape is
			// unreachable, and the platform clones that receive an exit proof
			// truncate the field by design (anchor_server.go:44-49). The outer
			// HelperExit pointers on the evidence structs are populated.
			continue
		}
		if pinned, ok := cloneAliasingFieldOverrides[t.String()+"."+field.Name]; ok {
			value := reflect.ValueOf(pinned)
			if !value.Type().AssignableTo(field.Type) {
				return fmt.Errorf("override for %s.%s is not assignable", t, field.Name)
			}
			target.Set(value)
			continue
		}
		if err := cloneAliasingPopulate(target); err != nil {
			return fmt.Errorf("%s.%s: %w", t, field.Name, err)
		}
	}
	return nil
}

// cloneAliasingPopulatedValue returns a new addressable value of type t filled
// by cloneAliasingPopulate.
func cloneAliasingPopulatedValue(t reflect.Type) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	if err := cloneAliasingPopulate(v); err != nil {
		return reflect.Value{}, err
	}
	return v, nil
}

// cloneAliasingIntegerSentinel returns a non-zero value small enough that
// populating and incrementing it cannot overflow the signed or unsigned type.
func cloneAliasingIntegerSentinel(t reflect.Type) int64 {
	if t.Bits() > 16 {
		return 4242
	}
	return 1 << (t.Bits() - 2)
}

// cloneAliasingRender renders v into a deterministic deep text image with
// pointers dereferenced, so two values compare as equal exactly when their
// content is equal and a shared pointee, backing array or map shows up as a
// changed image.
func cloneAliasingRender(v reflect.Value) string {
	var builder strings.Builder
	cloneAliasingRenderValue(&builder, v, 0)
	return builder.String()
}

func cloneAliasingRenderValue(builder *strings.Builder, v reflect.Value, depth int) {
	if !v.IsValid() {
		builder.WriteString("<invalid>")
		return
	}
	if depth > cloneAliasingRenderDepth {
		builder.WriteString("<deep>")
		return
	}
	if v.Type() == cloneAliasingTimeType && v.CanInterface() {
		fmt.Fprintf(builder, "time(%s)", v.Interface().(time.Time).UTC().Format(time.RFC3339Nano))
		return
	}
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			builder.WriteString("nil")
			return
		}
		builder.WriteString("*")
		cloneAliasingRenderValue(builder, v.Elem(), depth+1)
	case reflect.Slice, reflect.Array:
		fmt.Fprintf(builder, "%s[len=%d]{", v.Type(), v.Len())
		for index := range v.Len() {
			if index > 0 {
				builder.WriteString(",")
			}
			cloneAliasingRenderValue(builder, v.Index(index), depth+1)
		}
		builder.WriteString("}")
	case reflect.Map:
		entries := make([]string, 0, v.Len())
		iterator := v.MapRange()
		for iterator.Next() {
			var entry strings.Builder
			cloneAliasingRenderValue(&entry, iterator.Key(), depth+1)
			entry.WriteString(":")
			cloneAliasingRenderValue(&entry, iterator.Value(), depth+1)
			entries = append(entries, entry.String())
		}
		sort.Strings(entries)
		fmt.Fprintf(builder, "%s[len=%d]{%s}", v.Type(), v.Len(), strings.Join(entries, ","))
	case reflect.Struct:
		fmt.Fprintf(builder, "%s{", v.Type())
		for index := range v.NumField() {
			if index > 0 {
				builder.WriteString(",")
			}
			builder.WriteString(v.Type().Field(index).Name)
			builder.WriteString(":")
			cloneAliasingRenderValue(builder, v.Field(index), depth+1)
		}
		builder.WriteString("}")
	case reflect.Bool:
		fmt.Fprintf(builder, "%t", v.Bool())
	case reflect.String:
		fmt.Fprintf(builder, "%q", v.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Fprintf(builder, "%d", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		fmt.Fprintf(builder, "%d", v.Uint())
	case reflect.Float32, reflect.Float64:
		fmt.Fprintf(builder, "%v", v.Float())
	default:
		fmt.Fprintf(builder, "<%s>", v.Kind())
	}
}

// cloneAliasingMutateReferences writes the mutation marker through the case's
// *T handle and every pointer, slice element and map entry reachable from it,
// recursing into pointees so that shallow-copied inner pointers are caught too.
// It returns the number of write sites; a zero count means the case exercised
// nothing that a clone could alias.
func cloneAliasingMutateReferences(original reflect.Value) int {
	if original.Kind() != reflect.Ptr || original.IsNil() {
		return 0
	}
	return cloneAliasingMutateValue(original.Elem())
}

// cloneAliasingMutateValue mutates one reachable value in place: every pointer
// is followed, every slice element and map entry is written, and every settable
// scalar is replaced, so a clone that shares any of them changes its rendering.
func cloneAliasingMutateValue(v reflect.Value) int {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return 0
		}
		return cloneAliasingMutateValue(v.Elem())
	case reflect.Slice:
		writes := 0
		for index := range v.Len() {
			writes += cloneAliasingMutateValue(v.Index(index))
		}
		return writes
	case reflect.Map:
		if v.IsNil() || v.Len() == 0 {
			return 0
		}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return cloneAliasingRender(keys[i]) < cloneAliasingRender(keys[j]) })
		v.SetMapIndex(keys[0], reflect.Value{})
		return 1
	case reflect.Interface:
		if v.IsNil() {
			return 0
		}
		return cloneAliasingMutateValue(v.Elem())
	case reflect.Struct, reflect.Array:
		writes := 0
		for index := range v.NumField() {
			writes += cloneAliasingMutateValue(v.Field(index))
		}
		return writes
	default:
		if !v.CanSet() {
			return 0
		}
		v.Set(cloneAliasingMutatedScalar(v))
		return 1
	}
}

// cloneAliasingMutatedScalar returns the marker value written through a
// reference, chosen so that it differs from the populated value.
func cloneAliasingMutatedScalar(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Bool:
		return reflect.ValueOf(!v.Bool())
	case reflect.String:
		return reflect.ValueOf(cloneAliasingMutatedMarker)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value := reflect.New(v.Type()).Elem()
		value.SetInt(v.Int() + 1)
		return value
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		value := reflect.New(v.Type()).Elem()
		value.SetUint(v.Uint() + 1)
		return value
	case reflect.Float32, reflect.Float64:
		value := reflect.New(v.Type()).Elem()
		value.SetFloat(v.Float() + 1)
		return value
	default:
		return reflect.Value{}
	}
}
