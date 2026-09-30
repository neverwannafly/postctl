package postmanlocal

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"math/big"
	"reflect"
	"time"
)

// maxShapeDepth bounds jsonShape so a self-referencing V8 object graph (the
// decoder resolves object references to the same map) cannot recurse forever.
const maxShapeDepth = 256

// jsonShape returns a deep copy of value that holds only what encoding/json
// produces when it decodes into an any: nil, bool, float64, string, []any and
// map[string]any.
//
// The extractor builds documents from V8 values (int64, uint64, time.Time,
// []byte, ...) and from Go code (for example strings.Split). The importer
// reads documents through type assertions on the JSON shapes, so a Go-typed
// value that is not one of them is silently treated as absent. Emitting only
// JSON shapes makes the in-memory documents behave exactly like the staged
// JSON files.
//
// Conversions: integers become float64; NaN and infinities become nil (JSON
// cannot carry them); time.Time becomes an RFC 3339 string; []byte becomes a
// base64 string; *big.Int becomes its decimal string; other slices, arrays
// and string-keyed maps are copied element-wise; anything else becomes nil.
func jsonShape(value any) any {
	return shape(value, 0)
}

func shape(value any, depth int) any {
	if depth > maxShapeDepth {
		return nil
	}
	switch typed := value.(type) {
	case nil:
		return nil
	case string, bool:
		return typed
	case float64:
		return finite(typed)
	case float32:
		return finite(float64(typed))
	case int:
		return float64(typed)
	case int8:
		return float64(typed)
	case int16:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint:
		return float64(typed)
	case uint8:
		return float64(typed)
	case uint16:
		return float64(typed)
	case uint32:
		return float64(typed)
	case uint64:
		return float64(typed)
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return nil
		}
		return finite(number)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case []byte:
		return base64.StdEncoding.EncodeToString(typed)
	case *big.Int:
		if typed == nil {
			return nil
		}
		return typed.String()
	case []any:
		if typed == nil {
			return nil
		}
		out := make([]any, len(typed))
		for i, entry := range typed {
			out[i] = shape(entry, depth+1)
		}
		return out
	case map[string]any:
		return shapeMap(typed, depth)
	case Document:
		return shapeMap(map[string]any(typed), depth)
	}
	return shapeReflect(reflect.ValueOf(value), depth)
}

func shapeMap(source map[string]any, depth int) any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, entry := range source {
		out[key] = shape(entry, depth+1)
	}
	return out
}

// shapeReflect handles typed slices, arrays, maps and pointers that the fast
// paths above do not name.
func shapeReflect(v reflect.Value, depth int) any {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return shape(v.Elem().Interface(), depth+1)
	case reflect.Slice:
		if v.IsNil() {
			return nil
		}
		fallthrough
	case reflect.Array:
		out := make([]any, v.Len())
		for i := range out {
			out[i] = shape(v.Index(i).Interface(), depth+1)
		}
		return out
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil
		}
		if v.IsNil() {
			return nil
		}
		out := make(map[string]any, v.Len())
		iterator := v.MapRange()
		for iterator.Next() {
			out[iterator.Key().String()] = shape(iterator.Value().Interface(), depth+1)
		}
		return out
	case reflect.Bool:
		return v.Bool()
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint())
	case reflect.Float32, reflect.Float64:
		return finite(v.Float())
	default:
		return nil
	}
}

func finite(number float64) any {
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return nil
	}
	return number
}

// shapeDocument returns document as a JSON-shaped Document.
func shapeDocument(document Document) Document {
	if document == nil {
		return nil
	}
	shaped, _ := shape(map[string]any(document), 0).(map[string]any)
	return Document(shaped)
}
