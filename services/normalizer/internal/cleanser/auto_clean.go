package cleanser

import (

	"strings"
)

type Options struct {

	LowercaseKeys    bool `json:"lowercase_keys"`

	DropEmptyStrings bool `json:"drop_empty_strings"`
}

func Clean(v any, opt Options) any {

	switch t := v.(type) {

	case map[string]any:


		out := make(map[string]any, len(t))


		// deterministic: iterate keys sorted via insertion sort helper


		keys := make([]string, 0, len(t))


		for k := range t {



			keys = append(keys, k)


		}


		sortStrings(keys)


		for _, k := range keys {



			nk := k



			if opt.LowercaseKeys {




				nk = strings.ToLower(strings.TrimSpace(k))



			} else {




				nk = strings.TrimSpace(k)



			}



			if nk == "" {




				continue



			}



			cv := Clean(t[k], opt)



			// drop empty strings if enabled



			if opt.DropEmptyStrings {




				if s, ok := cv.(string); ok && strings.TrimSpace(s) == "" {





					continue




				}



			}



			out[nk] = cv


		}


		return out

	case []any:


		out := make([]any, 0, len(t))


		for _, it := range t {



			cv := Clean(it, opt)



			if opt.DropEmptyStrings {




				if s, ok := cv.(string); ok && strings.TrimSpace(s) == "" {





					continue




				}



			}



			out = append(out, cv)


		}


		return out

	case string:


		s := normalizeSpace(t)


		ls := strings.ToLower(s)


		if ls == "null" || ls == "nil" || ls == "none" || ls == "n/a" {



			return nil


		}


		if opt.DropEmptyStrings && s == "" {



			return nil


		}


		return s

	default:


		return v

	}
}

func normalizeSpace(s string) string {

	s = strings.TrimSpace(s)

	if s == "" {


		return ""

	}

	// collapse whitespace

	fields := strings.Fields(s)

	return strings.Join(fields, " ")
}

func sortStrings(a []string) {

	for i := 1; i < len(a); i++ {


		j := i


		for j > 0 && a[j] < a[j-1] {



			a[j], a[j-1] = a[j-1], a[j]



			j--


		}

	}
}
