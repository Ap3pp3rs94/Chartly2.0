package engine

import (
	"errors"

	"fmt"

	"strconv"

	"strings"

	"time"
)

type Document = map[string]any

type Step struct {
	ID string `json:"id"`

	Kind string `json:"kind"` // map|drop_nulls|coerce|add_meta

	Ops []Op `json:"ops,omitempty"`

	Params map[string]string `json:"params,omitempty"`
}
type Pipeline struct {
	ID string `json:"id"`

	Match map[string]string `json:"match,omitempty"` // connector_id, source_id, job_type; "*" supported

	Steps []Step `json:"steps,omitempty"`
}
type Result struct {
	PipelineID string `json:"pipeline_id"`

	AppliedSteps []string `json:"applied_steps,omitempty"`

	DroppedFields []string `json:"dropped_fields,omitempty"`

	Coercions []string `json:"coercions,omitempty"`
}

var (
	ErrNoMatch      = errors.New("no matching pipeline")
	ErrStepInvalid  = errors.New("step invalid")
	ErrCoerceFailed = errors.New("coerce failed")
)

func MatchPipeline(p Pipeline, meta map[string]string) bool {

	if p.ID == "" {

		return false

	}
	if p.Match == nil || len(p.Match) == 0 {

		return true

	}
	for k, want := range p.Match {

		got := meta[strings.ToLower(strings.TrimSpace(k))]

		if !wildMatch(strings.TrimSpace(want), strings.TrimSpace(got)) {

			return false

		}

	}
	return true
}
func RunPipeline(doc Document, p Pipeline, meta map[string]string) (Document, Result, error) {

	if doc == nil {

		return nil, Result{}, errors.New("doc is nil")

	}
	if p.ID == "" {

		return doc, Result{}, ErrNoMatch

	}
	if !MatchPipeline(p, meta) {

		return doc, Result{}, ErrNoMatch

	}
	res := Result{

		PipelineID: p.ID,

		AppliedSteps: make([]string, 0, len(p.Steps)),
	}
	for _, st := range p.Steps {

		kind := strings.ToLower(strings.TrimSpace(st.Kind))
		if kind == "" {

			return doc, res, fmt.Errorf("%w: missing kind", ErrStepInvalid)

		}
		stepID := strings.TrimSpace(st.ID)
		if stepID == "" {

			stepID = kind

		}
		switch kind {

		case "map":

			if err := Apply(doc, st.Ops); err != nil {

				return doc, res, err

			}
		case "drop_nulls":

			dropped := make([]string, 0)
			dropNulls(doc, "", &dropped)
			res.DroppedFields = append(res.DroppedFields, dropped...)
		case "coerce":

			cz, err := coerce(doc, st.Params)
			if err != nil {

				return doc, res, err

			}
			res.Coercions = append(res.Coercions, cz...)
		case "add_meta":

			addMeta(doc, meta)
		default:

			return doc, res, fmt.Errorf("%w: unknown kind %s", ErrStepInvalid, kind)

		}
		res.AppliedSteps = append(res.AppliedSteps, stepID)

	}
	return doc, res, nil
}
func wildMatch(want, got string) bool {

	if want == "*" || want == "" {

		return true

	}
	return want == got
}
func addMeta(doc Document, meta map[string]string) {

	m := map[string]any{}
	for k, v := range meta {

		m[k] = v

	}
	if _, ok := m["ts"]; !ok {

		m["ts"] = time.Now().UTC().Format(time.RFC3339Nano)

	}
	doc["_meta"] = m
}

// dropNulls removes nils from maps/slices recursively and records dropped paths.
// Deterministic DFS order based on map key sort (insertion sort helper).
func dropNulls(v any, prefix string, dropped *[]string) {

	switch t := v.(type) {

	case map[string]any:

		keys := make([]string, 0, len(t))
		for k := range t {

			keys = append(keys, k)

		}
		sortStrings(keys)
		for _, k := range keys {

			p := joinPath(prefix, k)
			if t[k] == nil {

				delete(t, k)
				*dropped = append(*dropped, p)
				continue

			}
			dropNulls(t[k], p, dropped)

		}
	case []any:

		for i := 0; i < len(t); i++ {

			p := fmt.Sprintf("%s[%d]", prefix, i)
			if t[i] == nil {

				*dropped = append(*dropped, p)
				continue

			}
			dropNulls(t[i], p, dropped)

		}

	}
}
func coerce(doc Document, params map[string]string) ([]string, error) {

	if params == nil {

		return nil, nil

	}
	out := make([]string, 0)
	for k, typ := range params {

		kk := strings.ToLower(strings.TrimSpace(k))
		if !strings.HasPrefix(kk, "path:") {

			continue

		}
		path := strings.TrimSpace(k[len("path:"):])
		if path == "" {

			continue

		}
		typ = strings.ToLower(strings.TrimSpace(typ))
		val, ok := Get(doc, path)
		if !ok {

			continue

		}
		s, ok := val.(string)
		if !ok {

			continue

		}
		s = strings.TrimSpace(s)
		switch typ {

		case "int":

			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {

				return out, fmt.Errorf("%w: %s int", ErrCoerceFailed, path)

			}
			if err := Set(doc, path, n); err != nil {

				return out, err

			}
			out = append(out, path+":int")
		case "float":

			f, err := strconv.ParseFloat(s, 64)
			if err != nil {

				return out, fmt.Errorf("%w: %s float", ErrCoerceFailed, path)

			}
			if err := Set(doc, path, f); err != nil {

				return out, err

			}
			out = append(out, path+":float")
		case "bool":

			b, err := strconv.ParseBool(strings.ToLower(s))
			if err != nil {

				return out, fmt.Errorf("%w: %s bool", ErrCoerceFailed, path)

			}
			if err := Set(doc, path, b); err != nil {

				return out, err

			}
			out = append(out, path+":bool")
		default:

			return out, fmt.Errorf("%w: unknown type %s", ErrStepInvalid, typ)

		}

	}
	return out, nil
}
func joinPath(prefix, key string) string {

	if prefix == "" {

		return key

	}
	return prefix + "." + key
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
