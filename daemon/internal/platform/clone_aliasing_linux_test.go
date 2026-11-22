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

const (
	cloneAliasingPopulateMarker = "clone-aliasing-marker"
	cloneAliasingMutatedMarker  = "clone-aliasing-mutated"
	cloneAliasingRenderDepth    = 64
)

var (
	cloneAliasingTimeType             = reflect.TypeOf(time.Time{})
	cloneAliasingExitProofType        = reflect.TypeOf(authority.LinuxCgroupExitProof{})
	cloneAliasingExitProofPointerType = reflect.TypeOf((*authority.LinuxCgroupExitProof)(nil))
	cloneAliasingFixedTime            = time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
)

// These entries pin value formats, not clone coverage: every value must satisfy
// Validate (authority/authority.go:1317) so the Validate-gated clone copies instead of returning nil.
var cloneAliasingFieldOverrides = map[string]any{
	"authority.LinuxCgroupParentIdentity.BootID":  "12345678-9abc-def0-1234-56789abcdef0",
	"authority.LinuxCgroupParentIdentity.Path":    "/sys/fs/cgroup/clone-aliasing-marker",
	"authority.LinuxCgroupIdentity.Version":       authority.LinuxCgroupIdentityVersion,
	"authority.LinuxCgroupIdentity.LeafName":      strings.Repeat("ab", authority.TokenBytes),
	"authority.LinuxCgroupGateIdentity.BirthKind": authority.LinuxCgroupTargetBirthTrustedAGate,
}

type cloneAliasingCase struct {
	name           string
	fresh          func() any
	clone          func(original reflect.Value) any
	skipExitProofs []string
}

func cloneAliasingCaseFor[T any](name string, clone func(value *T) any, skipExitProofs ...string) cloneAliasingCase {
	return cloneAliasingCase{
		name:           name,
		fresh:          func() any { return new(T) },
		clone:          func(original reflect.Value) any { return clone(original.Interface().(*T)) },
		skipExitProofs: skipExitProofs,
	}
}

func TestCloneAliasingLinuxCgroupEvidence(t *testing.T) {
	for _, testCase := range []cloneAliasingCase{
		cloneAliasingCaseFor("cloneLinuxCgroupV2RemovalAuthorization", func(value *LinuxCgroupV2RemovalAuthorization) any {
			return cloneLinuxCgroupV2RemovalAuthorization(*value)
		},
			// The Receipt's LinuxProof nested exits go through cloneLinuxCgroupStopReceipt,
			// which shares them with the input (linux_cgroup_v2_linux.go:1449-1453).
			"Receipt.LinuxProof.GateExit.HelperExit",
			"Receipt.LinuxProof.HelperExit.HelperExit",
			"Receipt.LinuxProof.LauncherExit.HelperExit"),
		cloneAliasingCaseFor("cloneLinuxCgroupV2PreGateRemovalAuthorization", func(value *LinuxCgroupV2PreGateRemovalAuthorization) any {
			return cloneLinuxCgroupV2PreGateRemovalAuthorization(*value)
		}),
		cloneAliasingCaseFor("cloneLinuxCgroupStopReceipt", func(value *authority.StopReceipt) any {
			return cloneLinuxCgroupStopReceipt(*value)
		},
			// The LinuxProof struct copy at linux_cgroup_v2_linux.go:1449 shares GateExit
			// and LauncherExit HelperExit with the input; the stored HelperExit copy (:1450-1453) shares one level deeper.
			"LinuxProof.GateExit.HelperExit",
			"LinuxProof.HelperExit.HelperExit",
			"LinuxProof.LauncherExit.HelperExit"),
	} {
		t.Run(testCase.name, func(t *testing.T) { runCloneAliasingCase(t, testCase) })
	}
}

// With one skip path removed the pass must fail, or the skip hides a path
// production now deep-copies and has to go (docs/TODO.md, "rf-w2-16 clone-aliasing gate").
func runCloneAliasingCase(t *testing.T, testCase cloneAliasingCase) {
	t.Helper()
	skipExitProofs := cloneAliasingSkipSet(testCase.skipExitProofs)
	original, err := cloneAliasingPrepareCase(testCase, skipExitProofs)
	if err != nil {
		t.Fatalf("prepare the covered case: %v", err)
	}
	if err := cloneAliasingAssertCase(testCase, original); err != nil {
		t.Fatal(err)
	}
	for path := range skipExitProofs {
		reduced := cloneAliasingSkipSet(testCase.skipExitProofs)
		delete(reduced, path)
		reducedOriginal, err := cloneAliasingPrepareCase(testCase, reduced)
		if err != nil {
			t.Fatalf("removing skip path %q does not populate: %v", path, err)
		}
		if err := cloneAliasingAssertCase(testCase, reducedOriginal); err == nil {
			t.Fatalf("skip path %q is no longer needed; remove it", path)
		}
	}
}

func cloneAliasingSkipSet(paths []string) map[string]bool {
	set := make(map[string]bool, len(paths))
	for _, path := range paths {
		set[path] = false
	}
	return set
}

func cloneAliasingPrepareCase(testCase cloneAliasingCase, skipExitProofs map[string]bool) (reflect.Value, error) {
	original := reflect.ValueOf(testCase.fresh())
	if original.Kind() != reflect.Ptr || original.IsNil() {
		return reflect.Value{}, fmt.Errorf("case did not provide a *T original")
	}
	if err := cloneAliasingPopulate(original.Elem(), "", 0, skipExitProofs); err != nil {
		return reflect.Value{}, fmt.Errorf("populate the covered value: %v", err)
	}
	for path, reached := range skipExitProofs {
		if !reached {
			return reflect.Value{}, fmt.Errorf("skip path %q was never reached; fix or drop the entry", path)
		}
	}
	return original, nil
}

func cloneAliasingAssertCase(testCase cloneAliasingCase, original reflect.Value) error {
	cloned := reflect.ValueOf(testCase.clone(original))
	if !cloned.IsValid() || (cloned.Kind() == reflect.Ptr && cloned.IsNil()) {
		return fmt.Errorf("clone returned an empty value for a populated original")
	}
	// Both sides are dereferenced once so a value-returning and a pointer-returning
	// clone compare the same covered value.
	before := cloneAliasingRender(cloneAliasingDereference(original))
	if after := cloneAliasingRender(cloneAliasingDereference(cloned)); after != before {
		return fmt.Errorf("clone content differs from the populated original\n clone: %s\norigin: %s", after, before)
	}
	writes := cloneAliasingMutateReferences(original)
	if writes == 0 {
		return fmt.Errorf("the mutation exercised no write site; the case cannot observe aliasing")
	}
	if after := cloneAliasingRender(cloneAliasingDereference(cloned)); after != before {
		return fmt.Errorf("clone shares state with the original: %d write(s) in the original changed the clone\n before: %s\n  after: %s", writes, before, after)
	}
	return nil
}

func cloneAliasingDereference(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Ptr && !v.IsNil() {
		return v.Elem()
	}
	return v
}

// Fills every settable field with a non-zero, deterministic value; unknown kinds
// and unexported fields are errors, so a field added later forces a harness decision.
func cloneAliasingPopulate(v reflect.Value, path string, helperDepth int, skipPaths map[string]bool) error {
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
		return cloneAliasingPopulate(v.Elem(), path, helperDepth, skipPaths)
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for index := range v.Len() {
			if err := cloneAliasingPopulate(v.Index(index), path, helperDepth, skipPaths); err != nil {
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
		return cloneAliasingPopulateStruct(v, path, helperDepth, skipPaths)
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

// A skip path must name a *authority.LinuxCgroupExitProof and is marked reached.
// HelperExit nests only at helperDepth zero: Validate accepts one level at most (authority/authority.go:1164).
func cloneAliasingPopulateStruct(v reflect.Value, path string, helperDepth int, skipPaths map[string]bool) error {
	t := v.Type()
	for index := range t.NumField() {
		field, target := t.Field(index), v.Field(index)
		if !target.CanSet() {
			return fmt.Errorf("struct %s field %s is not settable; extend the aliasing harness", t, field.Name)
		}
		fieldPath := field.Name
		if path != "" {
			fieldPath = path + "." + field.Name
		}
		if _, ok := skipPaths[fieldPath]; ok {
			if target.Type() != cloneAliasingExitProofPointerType {
				return fmt.Errorf("skip path %s names %s, not *authority.LinuxCgroupExitProof", fieldPath, target.Type())
			}
			skipPaths[fieldPath] = true
			continue
		}
		if t == cloneAliasingExitProofType && field.Name == "HelperExit" {
			if helperDepth > 0 {
				continue
			}
			if err := cloneAliasingPopulate(target, fieldPath, helperDepth+1, skipPaths); err != nil {
				return fmt.Errorf("%s.%s: %w", t, field.Name, err)
			}
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
		if err := cloneAliasingPopulate(target, fieldPath, helperDepth, skipPaths); err != nil {
			return fmt.Errorf("%s.%s: %w", t, field.Name, err)
		}
	}
	return nil
}

func cloneAliasingPopulatedValue(t reflect.Type) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	if err := cloneAliasingPopulate(v, "", 0, map[string]bool{}); err != nil {
		return reflect.Value{}, err
	}
	return v, nil
}

// Small enough that populating and incrementing it cannot overflow the signed or unsigned type.
func cloneAliasingIntegerSentinel(t reflect.Type) int64 {
	if t.Bits() > 16 {
		return 4242
	}
	return 1 << (t.Bits() - 2)
}

// Deterministic deep text image with pointers dereferenced: a shared pointee, backing array or map changes it.
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
	case reflect.Slice:
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

func cloneAliasingMutateReferences(original reflect.Value) int {
	if original.Kind() != reflect.Ptr || original.IsNil() {
		return 0
	}
	return cloneAliasingMutateValue(original.Elem())
}

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
		writes := 0
		for _, key := range keys {
			// A map entry's value is not addressable, but a pointer, slice or interface
			// in it still reaches a writable pointee, so an entry-wise copy cannot hide behind a fresh map header.
			writes += cloneAliasingMutateValue(v.MapIndex(key))
		}
		// Deleting one key catches a clone that still shares the original's map
		// header, which the recursion above cannot see through a value-only entry.
		v.SetMapIndex(keys[0], reflect.Value{})
		return writes + 1
	case reflect.Interface:
		if v.IsNil() {
			return 0
		}
		return cloneAliasingMutateValue(v.Elem())
	case reflect.Struct:
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
		// Reached only if the harness's kind sets drift apart; panics with the kind so the gap is self-locating.
		panic(fmt.Sprintf("cloneAliasingMutatedScalar: kind %s is not handled by the aliasing harness", v.Kind()))
	}
}
