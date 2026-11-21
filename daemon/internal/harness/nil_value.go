package harness

import "reflect"

// IsNilInterface reports whether value is a nil interface value or holds a
// typed-nil pointer, map, slice, func, chan or interface. Native adapters use
// it to reject a process owner that a launcher returned through an interface it
// still satisfies.
func IsNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
