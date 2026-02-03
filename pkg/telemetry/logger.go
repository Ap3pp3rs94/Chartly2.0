package telemetry

import (
    "bytes"
    "context"
    "encoding/json"
    "io"
    "os"
    "sort"
    "strconv"
    "strings"
    "sync"
    "time"
)

// Level represents log severity.
type Level string

const (
    LevelDebug Level = "debug"
    LevelInfo  Level = "info"
    LevelWarn  Level = "warn"
    LevelError Level = "error"
)

const (
    MaxFields = 64
    MaxKeyLen = 64
    MaxValLen = 512

    logMaxMessageLen = 1024
    logMaxServiceLen = 64

    // Bound conflict reporting
    MaxConflictKeys = 8

    // Deterministic value encoding bound (before sanitize truncation)
    MaxDeterministicJSONBytes = 2048
)

// Field is a deterministic key/value field representation.
type Field struct {
    K string `json:"k"`
    V string `json:"v"`
}

// Event is a single log record (JSON line).
type Event struct {
    Ts      string  `json:"ts"`
    Level   Level   `json:"level"`
    Service string  `json:"service,omitempty"`
    Msg     string  `json:"msg"`
    Fields  []Field `json:"fields,omitempty"`
}

// Options configures the logger.
type Options struct {
    Service   string
    Level     Level
    Timestamp bool
}

// Logger is a structured JSON-lines logger (stdlib-only).
type Logger struct {
    w   io.Writer
    mu  sync.Mutex
    opt Options
}

// NopLogger is a safe no-op logger.
var NopLogger = &Logger{w: io.Discard, opt: Options{Timestamp: true, Level: LevelError}}

// NewLogger creates a logger writing JSON lines to w.
func NewLogger(w io.Writer, opt Options) *Logger {
    if w == nil {
        w = os.Stdout
    }
    opt.Service = strings.TrimSpace(opt.Service)
    if len(opt.Service) > logMaxServiceLen {
        opt.Service = opt.Service[:logMaxServiceLen]
    }
    if opt.Level == "" {
        opt.Level = LevelInfo
    }
    // Default timestamp true unless explicitly disabled.
    if !opt.Timestamp {
        // allow explicit false
    } else {
        opt.Timestamp = true
    }
    return &Logger{w: w, opt: opt}
}

// NewDefaultLogger returns an info-level logger with timestamps enabled.
func NewDefaultLogger(w io.Writer, service string) *Logger {
    return NewLogger(w, Options{Service: service, Level: LevelInfo, Timestamp: true})
}

// NewInfoLogger is an alias of NewDefaultLogger (clarity).
func NewInfoLogger(w io.Writer, service string) *Logger {
    return NewDefaultLogger(w, service)
}

func (l *Logger) Debug(ctx context.Context, msg string, fields map[string]any) {
    l.log(ctx, LevelDebug, msg, fields)
}

func (l *Logger) Info(ctx context.Context, msg string, fields map[string]any) {
    l.log(ctx, LevelInfo, msg, fields)
}

func (l *Logger) Warn(ctx context.Context, msg string, fields map[string]any) {
    l.log(ctx, LevelWarn, msg, fields)
}

func (l *Logger) Error(ctx context.Context, msg string, fields map[string]any) {
    l.log(ctx, LevelError, msg, fields)
}

func (l *Logger) enabled(level Level) bool {
    rank := func(x Level) int {
        switch x {
        case LevelDebug:
            return 1
        case LevelInfo:
            return 2
        case LevelWarn:
            return 3
        default:
            return 4
        }
    }
    return rank(level) >= rank(l.opt.Level)
}

func (l *Logger) log(ctx context.Context, level Level, msg string, fields map[string]any) {
    if l == nil {
        return
    }
    if !l.enabled(level) {
        return
    }

    ev := Event{
        Level:   level,
        Service: l.opt.Service,
        Msg:     sanitize(msg, logMaxMessageLen),
    }
    if l.opt.Timestamp {
        ev.Ts = time.Now().UTC().Format(time.RFC3339Nano)
    }

    // Merge enriched + caller fields into a single map[string]string first.
    merged := make(map[string]string, 16)
    authoritative := make(map[string]bool, 16)
    conflicts := make([]string, 0, 4)

    set := func(k, v string, auth bool) {
        k = strings.TrimSpace(k)
        if k == "" || len(k) > MaxKeyLen {
            return
        }
        v = sanitize(v, MaxValLen)
        if existing, ok := merged[k]; ok && existing != v {
            if auth {
                // overwrite caller value
                if len(conflicts) < MaxConflictKeys {
                    conflicts = append(conflicts, k)
                }
                merged[k] = v
                authoritative[k] = true
                return
            }
            if authoritative[k] {
                if len(conflicts) < MaxConflictKeys {
                    conflicts = append(conflicts, k)
                }
                return
            }
            // non-authoritative conflict: keep existing deterministically
            if len(conflicts) < MaxConflictKeys {
                conflicts = append(conflicts, k)
            }
            return
        }
        merged[k] = v
        if auth {
            authoritative[k] = true
        }
    }

    // Enrichment: tracing
    if sc, ok := SpanContextFromContext(ctx); ok {
        set("trace_id", sc.TraceID, true)
        set("span_id", sc.SpanID, true)
        if sc.ParentSpanID != "" {
            set("parent_span_id", sc.ParentSpanID, true)
        }
        set("sampled", boolString(sc.Sampled), true)
    }

    // Enrichment: request_id / tenant_id
    if ctx != nil {
        if v := ctx.Value("request_id"); v != nil {
            if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
                set("request_id", s, true)
            }
        }
        if v := ctx.Value("tenant_id"); v != nil {
            if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
                set("tenant_id", s, true)
            }
        }
    }

    // Caller fields (non-authoritative)
    if fields != nil && len(fields) > 0 {
        keys := make([]string, 0, len(fields))
        for k := range fields {
            keys = append(keys, k)
        }
        sort.Strings(keys)
        for _, k := range keys {
            k2 := strings.TrimSpace(k)
            if k2 == "" || len(k2) > MaxKeyLen {
                continue
            }
            set(k2, toString(fields[k]), false)
        }
    }

    if len(conflicts) > 0 {
        sort.Strings(conflicts)
        set("conflict_keys", strings.Join(conflicts, ","), true)
    }

    if len(merged) > 0 {
        keys := make([]string, 0, len(merged))
        for k := range merged {
            keys = append(keys, k)
        }
        sort.Strings(keys)

        fieldsOut := make([]Field, 0, minInt(len(keys), MaxFields))
        for _, k := range keys {
            if len(fieldsOut) >= MaxFields {
                break
            }
            fieldsOut = append(fieldsOut, Field{K: k, V: merged[k]})
        }
        ev.Fields = fieldsOut
    }

    b, err := json.Marshal(ev)
    if err != nil {
        return
    }
    if len(b) > MaxDeterministicJSONBytes && len(ev.Fields) > 0 {
        // Trim fields deterministically to fit size bound.
        for len(b) > MaxDeterministicJSONBytes && len(ev.Fields) > 0 {
            ev.Fields = ev.Fields[:len(ev.Fields)-1]
            b, err = json.Marshal(ev)
            if err != nil {
                return
            }
        }
    }

    l.mu.Lock()
    defer l.mu.Unlock()
    _, _ = l.w.Write(b)
    _, _ = l.w.Write([]byte("\n"))
}

func sanitize(s string, max int) string {
    s = strings.TrimSpace(s)
    if len(s) > max {
        s = s[:max]
    }
    // strip control chars
    out := make([]rune, 0, len(s))
    for _, r := range s {
        if r < 0x20 || r == 0x7f {
            continue
        }
        out = append(out, r)
    }
    return string(out)
}

func toString(v any) string {
    if v == nil {
        return ""
    }
    switch x := v.(type) {
    case string:
        return x
    case []byte:
        return string(x)
    case bool:
        return boolString(x)
    case int:
        return strconv.Itoa(x)
    case int64:
        return strconv.FormatInt(x, 10)
    case float64:
        return strconv.FormatFloat(x, 'f', -1, 64)
    default:
        b, err := json.Marshal(x)
        if err != nil {
            return ""
        }
        // Bound bytes deterministically before sanitize
        if len(b) > MaxDeterministicJSONBytes {
            b = b[:MaxDeterministicJSONBytes]
        }
        return string(bytes.TrimSpace(b))
    }
}

func boolString(v bool) string {
    if v {
        return "true"
    }
    return "false"
}

func minInt(a, b int) int {
    if a < b {
        return a
    }
    return b
}
