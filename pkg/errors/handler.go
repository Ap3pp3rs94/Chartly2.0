package errors

import (
"encoding/json"
"net/http"
"sort"
"strings"
)

const (
MaxMessageLen    = 512
MaxDetails       = 32
MaxDetailKeyLen  = 64
MaxDetailValLen  = 256
MaxJSONBytes     = 32 * 1024
)

type KV struct {
K string `json:"k"`
V string `json:"v"`
}

type ErrorBody struct {
Code      Code   `json:"code"`
Message   string `json:"message"`
Retryable bool   `json:"retryable"`
Kind      string `json:"kind,omitempty"`

RequestID string `json:"request_id,omitempty"`
TraceID   string `json:"trace_id,omitempty"`

Details []KV `json:"details,omitempty"`
}

type ErrorEnvelope struct {
Error ErrorBody `json:"error"`
}

// NewEnvelope builds a safe, bounded error envelope.
// details are encoded deterministically as sorted KV pairs.
func NewEnvelope(code Code, msg string, reqID string, traceID string, details map[string]any) ErrorEnvelope {
meta, ok := Meta(code)
if !ok {
// unknown => internal
meta = CodeMeta{HTTPStatus: 500, Retryable: true, Kind: "server", Description: "unknown error code"}
code = Internal


}body := ErrorBody{
Code:      code,
Message:   sanitize(msg, MaxMessageLen),
Retryable: meta.Retryable,
Kind:      meta.Kind,
RequestID: sanitize(reqID, 128),
TraceID:   sanitize(traceID, 128),


}if details != nil && len(details) > 0 {
keys := make([]string, 0, len(details))
for k := range details {
k2 := strings.TrimSpace(k)
if k2 == "" {
continue

}keys = append(keys, k2)

}sort.Strings(keys)

out := make([]KV, 0, minInt(len(keys), MaxDetails))
for _, k := range keys {
if len(out) >= MaxDetails {
out = append(out, KV{K: "details_truncated", V: "true"})
break

}if len(k) > MaxDetailKeyLen {
continue

}out = append(out, KV{
K: k,
V: sanitize(toString(details[k]), MaxDetailValLen),
})

}if len(out) > 0 {
body.Details = out

}

}return ErrorEnvelope{Error: body}
}

func HTTPStatusFor(code Code) int {
if m, ok := Meta(code); ok && m.HTTPStatus > 0 {
return m.HTTPStatus

}return 500
}

// FromError converts an error into an envelope.
// If err is nil, returns fallback code with a generic message.
func FromError(err error, fallback Code, reqID, traceID string) ErrorEnvelope {
if err == nil {
return NewEnvelope(fallback, "unknown error", reqID, traceID, nil)

}// Best-effort: if caller passed fallback known code, use it; else Internal.
if !Known(fallback) {
fallback = Internal

}return NewEnvelope(fallback, err.Error(), reqID, traceID, nil)
}

// WriteHTTP writes the envelope as JSON with bounded payload.
// If marshaled bytes exceed MaxJSONBytes, writes a minimal fallback.
func WriteHTTP(w http.ResponseWriter, status int, env ErrorEnvelope) {
if w == nil {
return

}b, err := json.Marshal(env)
if err != nil || len(b) > MaxJSONBytes {
status = 500
b = []byte(`{"error":{"code":"internal","message":"internal error","retryable":true,"kind":"server"}}`)

}w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
_, _ = w.Write(b)
}

func sanitize(s string, max int) string {
s = strings.TrimSpace(s)
if len(s) > max {
s = s[:max]

}// strip control chars
out := make([]rune, 0, len(s))
for _, r := range s {
if r < 0x20 || r == 0x7f {
continue

}out = append(out, r)

}return string(out)
}

func toString(v any) string {
if v == nil {
return ""

}switch x := v.(type) {
case string:
return x
case []byte:
return string(x)
default:
b, err := json.Marshal(x)
if err != nil {
return ""

}return string(b)

}}

func minInt(a, b int) int {
if a < b {
return a

}return b
}
