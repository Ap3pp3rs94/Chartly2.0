package logging

// Structured logger (stable JSON)  stdlib only.
//
// This logger emits deterministic JSON by converting map fields into a sorted KV array:
//   [{"k":"level","v":"info"},{"k":"event","v":"startup"},{"k":"addr","v":"0.0.0.0:8086"}]
//
// Determinism:
//   - Field keys are sorted lexicographically.
//   - Strings are normalized (trim, remove NUL).
//
// Safety:
//   - Logger never panics; failures to marshal/write are ignored (best-effort).

import (
	"encoding/json"
	"io"
	"sort"
	"strings"
	"sync"
)

type Logger struct {
	out io.Writer
	mu  sync.Mutex
}

type kv struct {
	K string `json:"k"`
	V any    `json:"v"`
}

func New(out io.Writer) *Logger {
	return &Logger{out: out}
}

func (l *Logger) Log(level, event string, fields map[string]any) {
	if l == nil || l.out == nil {
		return
	}

	level = norm(level)
	event = norm(event)

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	arr := make([]kv, 0, len(keys)+2)
	arr = append(arr, kv{K: "level", V: level})
	arr = append(arr, kv{K: "event", V: event})
	for _, k := range keys {
		arr = append(arr, kv{K: norm(k), V: normalizeAny(fields[k])})
	}

	b, err := json.Marshal(arr)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write(append(b, '\n'))
}

func norm(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	return s
}

func normalizeAny(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return norm(t)
	default:
		return v
	}
}
