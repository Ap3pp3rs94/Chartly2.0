package cleanser

import (

	"strings"
)

func DedupeStrings(in []string) []string {

	seen := make(map[string]struct{}, len(in))

	out := make([]string, 0, len(in))

	for _, s := range in {


		s = strings.TrimSpace(s)


		if s == "" {



			continue


		}


		if _, ok := seen[s]; ok {



			continue


		}


		seen[s] = struct{}{}


		out = append(out, s)

	}

	return out
}

func DedupeObjects(in []map[string]any, keyPath string) []map[string]any {

	keyPath = strings.TrimSpace(keyPath)

	if keyPath == "" {


		return in

	}

	seen := make(map[string]struct{}, len(in))

	out := make([]map[string]any, 0, len(in))


	for _, obj := range in {


		if obj == nil {



			continue


		}


		v, ok := GetPath(obj, keyPath)


		if !ok || v == nil {



			continue


		}


		key, ok := v.(string)


		if !ok {



			// stringify basic values deterministically



			key = strings.TrimSpace(toString(v))


		} else {



			key = strings.TrimSpace(key)


		}


		if key == "" {



			continue


		}


		if _, exists := seen[key]; exists {



			continue


		}


		seen[key] = struct{}{}


		out = append(out, obj)

	}


	return out
}

func GetPath(obj map[string]any, path string) (any, bool) {

	path = strings.TrimSpace(path)

	if path == "" {


		return nil, false

	}


	parts := strings.Split(path, ".")

	var cur any = obj

	for _, p := range parts {


		p = strings.TrimSpace(p)


		if p == "" {



			return nil, false


		}


		m, ok := cur.(map[string]any)


		if !ok {



			return nil, false


		}


		v, ok := m[p]


		if !ok {



			return nil, false


		}


		cur = v

	}

	return cur, true
}

func toString(v any) string {

	switch t := v.(type) {

	case string:


		return t

	case bool:


		if t {



			return "true"


		}


		return "false"

	case int, int8, int16, int32, int64:


		return "int"

	case uint, uint8, uint16, uint32, uint64:


		return "uint"

	case float32, float64:


		return "float"

	default:


		return ""

	}
}
