package engine

import (

	"errors"

	"sort"

	"strings"
)

var (

	ErrSchemaExists  = errors.New("schema exists")

	ErrSchemaMissing = errors.New("schema missing")
)

type FieldRule struct {

	Path     string `json:"path"`

	Type     string `json:"type"` // string|number|bool|object|array|null|any

	Required bool   `json:"required"`
}

type Schema struct {

	ID      string      `json:"id"`

	Version string      `json:"version"`

	Fields  []FieldRule `json:"fields"`
}

type Validator struct {

	schemas map[string]Schema
}

func NewValidator() *Validator {

	return &Validator{schemas: make(map[string]Schema)}
}

func (v *Validator) Register(s Schema) error {

	id := strings.TrimSpace(s.ID)

	if id == "" {


		return errors.New("schema id empty")

	}

	if v.schemas == nil {


		v.schemas = make(map[string]Schema)

	}

	if _, ok := v.schemas[id]; ok {


		return ErrSchemaExists

	}

	// normalize

	for i := range s.Fields {


		s.Fields[i].Path = strings.TrimSpace(s.Fields[i].Path)


		s.Fields[i].Type = strings.ToLower(strings.TrimSpace(s.Fields[i].Type))


		if s.Fields[i].Type == "" {



			s.Fields[i].Type = "any"


		}

	}

	v.schemas[id] = s

	return nil
}

func (v *Validator) Validate(schemaID string, doc map[string]any) ([]string, error) {

	schemaID = strings.TrimSpace(schemaID)

	s, ok := v.schemas[schemaID]

	if !ok {


		return nil, ErrSchemaMissing

	}

	violations := make([]string, 0)


	for _, f := range s.Fields {


		if f.Path == "" {



			continue


		}


		val, exists := Get(doc, f.Path)


		if !exists {



			if f.Required {




				violations = append(violations, "missing:"+f.Path)



			}



			continue


		}


		want := f.Type


		if want == "" {



			want = "any"


		}


		got := InferType(val)


		if !typeMatches(want, got) {



			violations = append(violations, "type_mismatch:"+f.Path+":"+want+":"+got)


		}

	}


	sort.Strings(violations)

	return violations, nil
}

func InferType(x any) string {

	if x == nil {


		return "null"

	}

	switch x.(type) {

	case string:


		return "string"

	case bool:


		return "bool"

	case float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:


		return "number"

	case map[string]any:


		return "object"

	case []any:


		return "array"

	default:


		return "any"

	}
}

func typeMatches(want, got string) bool {

	want = strings.ToLower(strings.TrimSpace(want))

	got = strings.ToLower(strings.TrimSpace(got))


	if want == "any" {


		return true

	}

	if want == got {


		return true

	}

	if want == "number" && got == "number" {


		return true

	}

	return false
}
