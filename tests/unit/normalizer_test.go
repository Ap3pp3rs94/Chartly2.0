package unit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// KV is a single ordered key/value pair.
type KV struct {
	K string
	V any
}

// Obj is an ordered JSON object (keys sorted lexicographically during normalization).
type Obj []KV

// normalize converts JSON-like Go values into a canonical, ordered representation.
// Supported inputs:
// - nil
// - bool, string
// - numeric primitives (int/uint/float) and json.Number (no coercion policy in v0)
// - maps with string keys (any value) -> Obj with keys sorted
// - slices/arrays -> []any (order preserved), elements normalized
//
// Everything else is rejected with the single honest error:
//   fmt.Errorf("unsupported type: %T", v)
func normalize(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case bool, string:
		return x, nil
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return x, nil
	case json.Number:
		return x, nil
	case []any:
		out := make([]any, len(x))
		for i := range x {
			nv, err := normalize(x[i])
			if err != nil {
				return nil, fmt.Errorf("normalize slice idx %d: %w", i, err)
			}
			out[i] = nv
		}
		return out, nil
	}

	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil, nil
	}

	switch rv.Kind() {
	case reflect.Map:
		// Only allow string keys.
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("unsupported type: %T", v)
		}
		// Collect keys, sort, and normalize values.
		keys := rv.MapKeys()
		strKeys := make([]string, 0, len(keys))
		for _, k := range keys {
			strKeys = append(strKeys, k.String())
		}
		sort.Strings(strKeys)

		out := make(Obj, 0, len(strKeys))
		for _, k := range strKeys {
			val := rv.MapIndex(reflect.ValueOf(k))
			nv, err := normalize(val.Interface())
			if err != nil {
				return nil, fmt.Errorf("normalize map key %q: %w", k, err)
			}
			out = append(out, KV{K: k, V: nv})
		}
		return out, nil

	case reflect.Slice, reflect.Array:
		n := rv.Len()
		out := make([]any, n)
		for i := 0; i < n; i++ {
			nv, err := normalize(rv.Index(i).Interface())
			if err != nil {
				return nil, fmt.Errorf("normalize slice idx %d: %w", i, err)
			}
			out[i] = nv
		}
		return out, nil

	default:
		return nil, fmt.Errorf("unsupported type: %T", v)
	}
}

// stableJSON encodes normalized values deterministically.
// It supports Obj (ordered object), []any, primitives, and nil.
func stableJSON(v any) (string, error) {
	nv, err := normalize(v)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := encodeStable(&buf, nv); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func encodeStable(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil, bool, string, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		buf.Write(b)
		return nil

	case []any:
		buf.WriteByte('[')
		for i := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encodeStable(buf, x[i]); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil

	case Obj:
		buf.WriteByte('{')
		for i, kv := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(kv.K)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			if err := encodeStable(buf, kv.V); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil

	default:
		// Contract: single honest unsupported error everywhere.
		return fmt.Errorf("unsupported type: %T", v)
	}
}

func TestNormalize_ActuallySortsKeys(t *testing.T) {
	v := map[string]any{"b": 2, "a": 1, "c": 3}

	nv, err := normalize(v)
	if err != nil {
		t.Fatalf("normalize error: %v", err)
	}

	obj, ok := nv.(Obj)
	if !ok {
		t.Fatalf("expected Obj, got %T", nv)
	}
	if len(obj) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(obj))
	}
	if obj[0].K != "a" || obj[1].K != "b" || obj[2].K != "c" {
		t.Fatalf("keys not sorted: %#v", obj)
	}
}

func TestNormalize_ReflectSliceSupport(t *testing.T) {
	// Explicit []interface{} and map[string]interface{} (common older patterns).
	v := []interface{}{
		map[string]interface{}{"b": 2, "a": 1},
	}

	nv, err := normalize(v)
	if err != nil {
		t.Fatalf("normalize error: %v", err)
	}

	arr, ok := nv.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", nv)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 element, got %d", len(arr))
	}
	obj, ok := arr[0].(Obj)
	if !ok || len(obj) != 2 || obj[0].K != "a" || obj[1].K != "b" {
		t.Fatalf("expected normalized Obj inside slice, got %#v", arr[0])
	}
}

func TestStableJSON_NestedMapOrder(t *testing.T) {
	v := map[string]any{
		"b": 2,
		"a": map[string]any{
			"z": true,
			"y": []any{map[string]any{"b": 2, "a": 1}},
		},
	}

	got, err := stableJSON(v)
	if err != nil {
		t.Fatalf("stableJSON error: %v", err)
	}

	want := `{"a":{"y":[{"a":1,"b":2}],"z":true},"b":2}`
	if got != want {
		t.Fatalf("unexpected JSON\nwant: %s\ngot:  %s", want, got)
	}

	got2, err := stableJSON(v)
	if err != nil {
		t.Fatalf("stableJSON error: %v", err)
	}
	if got2 != want {
		t.Fatalf("unexpected JSON on repeat\nwant: %s\ngot:  %s", want, got2)
	}
}

func TestStableJSON_SlicesPreserveOrder(t *testing.T) {
	v := map[string]any{
		"arr": []any{
			map[string]any{"b": 2, "a": 1},
			map[string]any{"d": 4, "c": 3},
		},
	}

	got, err := stableJSON(v)
	if err != nil {
		t.Fatalf("stableJSON error: %v", err)
	}

	want := `{"arr":[{"a":1,"b":2},{"c":3,"d":4}]}`
	if got != want {
		t.Fatalf("unexpected JSON\nwant: %s\ngot:  %s", want, got)
	}
}

func TestStableJSON_NumberPolicy_IntAndFloatSameBytes(t *testing.T) {
	a, err := stableJSON(map[string]any{"n": 1})
	if err != nil {
		t.Fatalf("stableJSON error: %v", err)
	}

	b, err := stableJSON(map[string]any{"n": float64(1)})
	if err != nil {
		t.Fatalf("stableJSON error: %v", err)
	}

	if a != b {
		t.Fatalf("expected same JSON bytes for int(1) and float64(1)\nint:   %s\nfloat: %s", a, b)
	}
}

func TestNormalize_PrimitivesPassThrough(t *testing.T) {
	cases := []any{
		nil,
		true,
		"hello",
		123,
		int64(-5),
		float64(3.14),
		json.Number("42"),
	}

	for _, c := range cases {
		_, err := normalize(c)
		if err != nil {
			t.Fatalf("normalize(%T) error: %v", c, err)
		}
	}
}

func TestNormalize_RejectsUnsupportedTypes(t *testing.T) {
	_, err := normalize(func() {})
	if err == nil {
		t.Fatal("expected error for func")
	}

	ch := make(chan int)
	_, err = normalize(ch)
	if err == nil {
		t.Fatal("expected error for chan")
	}

	type S struct{ A int }
	_, err = normalize(S{A: 1})
	if err == nil {
		t.Fatal("expected error for struct")
	}

	i := 5
	_, err = normalize(&i)
	if err == nil {
		t.Fatal("expected error for pointer")
	}
}
