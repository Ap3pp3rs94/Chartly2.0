package engine

import (

	"errors"

	"fmt"

	"strconv"

	"strings"
)

var (

	ErrInvalidPath     = errors.New("invalid path")

	ErrTypeMismatch    = errors.New("type mismatch")

	ErrIndexOutOfRange = errors.New("index out of range")
)

type Op struct {

	Kind  string `json:"kind"`           // set|delete|rename|copy

	From  string `json:"from,omitempty"` // for rename/copy/delete

	To    string `json:"to,omitempty"`   // for set/rename/copy

	Value any    `json:"value,omitempty"`
}

func Apply(doc map[string]any, ops []Op) error {

	if doc == nil {


		return errors.New("doc is nil")

	}

	for _, op := range ops {


		switch strings.ToLower(strings.TrimSpace(op.Kind)) {


		case "set":



			if err := Set(doc, op.To, op.Value); err != nil {




				return err



			}


		case "delete":



			_ = Delete(doc, op.From)


		case "rename":



			if !Rename(doc, op.From, op.To) {




				// no-op if missing



			}


		case "copy":



			if !Copy(doc, op.From, op.To) {




				// no-op if missing



			}


		default:



			return fmt.Errorf("%w: unknown op kind", ErrInvalidPath)


		}

	}

	return nil
}

type segment struct {

	key    string

	hasIdx bool

	idx    int
}

func parsePath(path string) ([]segment, error) {

	path = strings.TrimSpace(path)

	if path == "" {


		return nil, ErrInvalidPath

	}


	parts := strings.Split(path, ".")

	out := make([]segment, 0, len(parts))


	for _, p := range parts {


		p = strings.TrimSpace(p)


		if p == "" {



			return nil, ErrInvalidPath


		}


		// support key[index]

		if strings.Contains(p, "[") {



			k, rest, ok := strings.Cut(p, "[")



			if !ok || strings.TrimSpace(k) == "" {




				return nil, ErrInvalidPath



			}



			if !strings.HasSuffix(rest, "]") {




				return nil, ErrInvalidPath



			}



			idxStr := strings.TrimSuffix(rest, "]")



			i, err := strconv.Atoi(idxStr)



			if err != nil || i < 0 {




				return nil, ErrInvalidPath



			}



			out = append(out, segment{key: k, hasIdx: true, idx: i})


		} else {



			out = append(out, segment{key: p})


		}

	}

	return out, nil
}

func Get(doc map[string]any, path string) (any, bool) {

	segs, err := parsePath(path)

	if err != nil {


		return nil, false

	}


	var cur any = doc

	for _, s := range segs {


		m, ok := cur.(map[string]any)


		if !ok {



			return nil, false


		}


		next, ok := m[s.key]


		if !ok {



			return nil, false


		}


		if s.hasIdx {



			arr, ok := next.([]any)



			if !ok {




				return nil, false



			}



			if s.idx < 0 || s.idx >= len(arr) {




				return nil, false



			}



			cur = arr[s.idx]


		} else {



			cur = next


		}

	}

	return cur, true
}

func Set(doc map[string]any, path string, value any) error {

	segs, err := parsePath(path)

	if err != nil {


		return err

	}


	if len(segs) == 0 {


		return ErrInvalidPath

	}


	var cur any = doc


	for i := 0; i < len(segs); i++ {


		s := segs[i]


		last := (i == len(segs)-1)



		m, ok := cur.(map[string]any)


		if !ok {



			return ErrTypeMismatch


		}


		if last {



			if s.hasIdx {




				// ensure array exists




				v, exists := m[s.key]




				if !exists {





					m[s.key] = make([]any, s.idx+1)





					v = m[s.key]




				}




				arr, ok := v.([]any)




				if !ok {





					return ErrTypeMismatch




				}




				if s.idx >= len(arr) {





					// extend





					ext := make([]any, s.idx+1)





					copy(ext, arr)





					arr = ext





					m[s.key] = arr




				}




				arr[s.idx] = value




				return nil



			}



			m[s.key] = value



			return nil


		}



		// intermediate


		next, exists := m[s.key]


		if !exists {



			// create container



			if s.hasIdx {




				arr := make([]any, s.idx+1)




				arr[s.idx] = make(map[string]any)




				m[s.key] = arr




				cur = arr[s.idx]




				continue


			}



			nm := make(map[string]any)


			m[s.key] = nm


			cur = nm


			continue


		}



		if s.hasIdx {



			arr, ok := next.([]any)



			if !ok {




				return ErrTypeMismatch



			}



			if s.idx < 0 {




				return ErrInvalidPath



			}



			if s.idx >= len(arr) {




				ext := make([]any, s.idx+1)




				copy(ext, arr)




				arr = ext




				m[s.key] = arr



			}



			if arr[s.idx] == nil {




				arr[s.idx] = make(map[string]any)



			}



			cur = arr[s.idx]



			continue


		}


		cur = next

	}

	return nil
}

func Delete(doc map[string]any, path string) bool {

	segs, err := parsePath(path)

	if err != nil || len(segs) == 0 {


		return false

	}


	// navigate to parent

	var cur any = doc

	for i := 0; i < len(segs)-1; i++ {


		s := segs[i]


		m, ok := cur.(map[string]any)


		if !ok {



			return false


		}


		next, ok := m[s.key]


		if !ok {



			return false


		}


		if s.hasIdx {



			arr, ok := next.([]any)



			if !ok || s.idx < 0 || s.idx >= len(arr) {




				return false



			}



			cur = arr[s.idx]


		} else {



			cur = next


		}

	}


	parentSeg := segs[len(segs)-1]

	pm, ok := cur.(map[string]any)

	if !ok {


		return false

	}


	if parentSeg.hasIdx {


		v, ok := pm[parentSeg.key]


		if !ok {



			return false


		}


		arr, ok := v.([]any)


		if !ok || parentSeg.idx < 0 || parentSeg.idx >= len(arr) {



			return false


		}


		arr[parentSeg.idx] = nil


		return true

	}


	_, existed := pm[parentSeg.key]

	delete(pm, parentSeg.key)

	return existed
}

func Rename(doc map[string]any, from, to string) bool {

	v, ok := Get(doc, from)

	if !ok {


		return false

	}


	if err := Set(doc, to, v); err != nil {


		return false

	}


	_ = Delete(doc, from)

	return true
}

func Copy(doc map[string]any, from, to string) bool {

	v, ok := Get(doc, from)

	if !ok {


		return false

	}


	if err := Set(doc, to, v); err != nil {


		return false

	}

	return true
}
