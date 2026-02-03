package telemetry

import "context"

// SpanContext is a minimal tracing context used for log enrichment.
type SpanContext struct {
    TraceID      string
    SpanID       string
    ParentSpanID string
    Sampled      bool
}

type spanContextKey struct{}

// ContextWithSpanContext returns a context carrying the provided SpanContext.
func ContextWithSpanContext(ctx context.Context, sc SpanContext) context.Context {
    if ctx == nil {
        ctx = context.Background()
    }
    return context.WithValue(ctx, spanContextKey{}, sc)
}

// SpanContextFromContext extracts a SpanContext from ctx if present.
func SpanContextFromContext(ctx context.Context) (SpanContext, bool) {
    if ctx == nil {
        return SpanContext{}, false
    }
    v := ctx.Value(spanContextKey{})
    sc, ok := v.(SpanContext)
    if !ok {
        return SpanContext{}, false
    }
    if sc.TraceID == "" && sc.SpanID == "" && sc.ParentSpanID == "" && !sc.Sampled {
        return SpanContext{}, false
    }
    return sc, true
}
