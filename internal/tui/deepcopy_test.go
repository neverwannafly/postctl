package tui

import "reflect"

// deepCopy returns (test helper: production code uses model Clone methods) an independent copy of v. Saves run on another goroutine,
// so they always work on a snapshot taken on the Update goroutine; that also
// keeps a later in-place edit from racing with a write in flight.
func deepCopy[T any](v T) T {
	out := copyValue(reflect.ValueOf(&v).Elem())
	return out.Interface().(T)
}

func copyValue(src reflect.Value) reflect.Value {
	switch src.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			return src
		}
		dst := reflect.New(src.Type().Elem())
		dst.Elem().Set(copyValue(src.Elem()))
		return dst
	case reflect.Interface:
		if src.IsNil() {
			return src
		}
		dst := reflect.New(src.Type()).Elem()
		dst.Set(copyValue(src.Elem()))
		return dst
	case reflect.Slice:
		if src.IsNil() {
			return src
		}
		dst := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		for i := 0; i < src.Len(); i++ {
			dst.Index(i).Set(copyValue(src.Index(i)))
		}
		return dst
	case reflect.Map:
		if src.IsNil() {
			return src
		}
		dst := reflect.MakeMapWithSize(src.Type(), src.Len())
		for _, k := range src.MapKeys() {
			dst.SetMapIndex(k, copyValue(src.MapIndex(k)))
		}
		return dst
	case reflect.Struct:
		dst := reflect.New(src.Type()).Elem()
		dst.Set(src) // unexported fields are copied shallowly
		for i := 0; i < src.NumField(); i++ {
			if dst.Field(i).CanSet() {
				dst.Field(i).Set(copyValue(src.Field(i)))
			}
		}
		return dst
	default:
		return src
	}
}
