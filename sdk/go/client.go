package chartly

import (
"bytes"
"context"
"encoding/json"
"errors"
"fmt"
"io"
"net/http"
"strings"
"time"

chartlyerrors "github.com/Ap3pp3rs94/Chartly2.0/pkg/errors"
"github.com/Ap3pp3rs94/Chartly2.0/pkg/telemetry"
)

// Thin Go SDK client for Chartly services.
//
// Design goals:
// - stdlib-only HTTP
// - consistent headers (tenant, request id, trace propagation)
// - bounded IO for safety
// - consistent error envelope decoding (pkg/errors)
//
// This SDK intentionally does NOT assume service-specific endpoints beyond /health and /ready.

const (
DefaultTenantHeader  = "X-Tenant-Id"
DefaultRequestHeader = "X-Request-Id"

DefaultMaxRequestBytes  = int64(4 * 1024 * 1024)  // 4 MiB
DefaultMaxResponseBytes = int64(8 * 1024 * 1024)  // 8 MiB
DefaultTimeout          = 15 * time.Second
)

// Client is a thin HTTP client wrapper with safe defaults.
type Client struct {
BaseURL string

// Default headers/policy
TenantHeader  string
RequestHeader string

// Default tenant to use when ctx does not provide tenant_id.
// If empty, no tenant header is set unless ctx has tenant_id.
DefaultTenant string

// Optional static headers applied to every request.
StaticHeaders map[string]string

// HTTP client; if nil, a safe default client is used.
HTTP *http.Client

// Safety bounds
MaxRequestBytes  int64
MaxResponseBytes int64

// Trace propagation helper (W3C trace-context)
Propagator telemetry.Propagator
}

// NewClient constructs a client with safe defaults.
func NewClient(baseURL string) *Client {
baseURL = strings.TrimSpace(baseURL)
return &Client{
BaseURL:          strings.TrimRight(baseURL, "/"),
TenantHeader:     DefaultTenantHeader,
RequestHeader:    DefaultRequestHeader,
HTTP:             &http.Client{Timeout: DefaultTimeout},
MaxRequestBytes:  DefaultMaxRequestBytes,
MaxResponseBytes: DefaultMaxResponseBytes,
StaticHeaders:    map[string]string{},

}}

// RequestOption mutates an outgoing request configuration.
type RequestOption func(*requestCfg)

type requestCfg struct {
tenantID   string
requestID  string
headers    map[string]string
traceState telemetry.SpanContext
haveTrace  bool
}

// WithTenant forces a tenant header value for this request.
func WithTenant(tenant string) RequestOption {
return func(c *requestCfg) { c.tenantID = strings.TrimSpace(tenant) }
}

// WithRequestID forces a request id header for this request.
func WithRequestID(reqID string) RequestOption {
return func(c *requestCfg) { c.requestID = strings.TrimSpace(reqID) }
}

// WithHeader sets an extra header for this request.
func WithHeader(k, v string) RequestOption {
return func(c *requestCfg) {
if c.headers == nil {
c.headers = map[string]string{}

}c.headers[strings.TrimSpace(k)] = strings.TrimSpace(v)

}}

// WithSpanContext forces a trace context for this request (overrides ctx trace).
func WithSpanContext(sc telemetry.SpanContext) RequestOption {
return func(c *requestCfg) {
c.traceState = sc
c.haveTrace = true

}}

// Health calls GET /health and returns the raw body (bounded) for display/debug.
// It does not assume a specific response schema.
func (c *Client) Health(ctx context.Context, opts ...RequestOption) ([]byte, error) {
return c.doRaw(ctx, http.MethodGet, "/health", nil, opts...)
}

// Ready calls GET /ready and returns the raw body (bounded) for display/debug.
// It does not assume a specific response schema.
func (c *Client) Ready(ctx context.Context, opts ...RequestOption) ([]byte, error) {
return c.doRaw(ctx, http.MethodGet, "/ready", nil, opts...)
}

// DoJSON performs an HTTP request with an optional JSON body and optionally decodes a JSON response into out.
// - If out is nil, the response body is discarded (still bounded).
// - If the response is non-2xx, attempts to parse Chartly error envelope and returns *APIError.
func (c *Client) DoJSON(ctx context.Context, method, path string, body any, out any, opts ...RequestOption) error {
if ctx == nil {
ctx = context.Background()

}raw, err := c.doRaw(ctx, method, path, body, opts...)
if err != nil {
return err

}if out == nil || len(raw) == 0 {
return nil

}dec := json.NewDecoder(bytes.NewReader(raw))
dec.UseNumber()
if err := dec.Decode(out); err != nil {
return fmt.Errorf("chartly sdk: decode response json: %w", err)

}return nil
}

// ---- errors ----

// APIError is returned for non-2xx responses when an error envelope is present (or synthesized).
type APIError struct {
Status     int
Code       chartlyerrors.Code
Message    string
Retryable  bool
Kind       string
RequestID  string
TraceID    string
RawBody    []byte // bounded
}

func (e *APIError) Error() string {
code := string(e.Code)
if code == "" {
code = "unknown"

}msg := e.Message
if msg == "" {
msg = "request failed"

}return fmt.Sprintf("chartly api error: status=%d code=%s retryable=%t msg=%s", e.Status, code, e.Retryable, msg)
}

// ---- internal request execution ----

func (c *Client) doRaw(ctx context.Context, method, path string, body any, opts ...RequestOption) ([]byte, error) {
if ctx == nil {
ctx = context.Background()

}if c == nil {
return nil, errors.New("chartly sdk: nil client")

}if c.HTTP == nil {
c.HTTP = &http.Client{Timeout: DefaultTimeout}

}if c.TenantHeader == "" {
c.TenantHeader = DefaultTenantHeader

}if c.RequestHeader == "" {
c.RequestHeader = DefaultRequestHeader

}if c.MaxRequestBytes <= 0 {
c.MaxRequestBytes = DefaultMaxRequestBytes

}if c.MaxResponseBytes <= 0 {
c.MaxResponseBytes = DefaultMaxResponseBytes


}base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
if base == "" {
return nil, errors.New("chartly sdk: base url required")


}method = strings.ToUpper(strings.TrimSpace(method))
if method == "" {
return nil, errors.New("chartly sdk: method required")


}// path join without assuming url.URL parsing to keep it simple & deterministic.
p := strings.TrimSpace(path)
if p == "" {
p = "/"

}if !strings.HasPrefix(p, "/") {
p = "/" + p

}url := base + p

cfg := requestCfg{}
for _, o := range opts {
if o != nil {
o(&cfg)

}

}// Derive tenant/request id from ctx if not explicitly set.
if cfg.tenantID == "" {
if v := ctx.Value("tenant_id"); v != nil {
if s, ok := v.(string); ok {
cfg.tenantID = strings.TrimSpace(s)

}
}if cfg.tenantID == "" {
cfg.tenantID = strings.TrimSpace(c.DefaultTenant)

}
}if cfg.requestID == "" {
if v := ctx.Value("request_id"); v != nil {
if s, ok := v.(string); ok {
cfg.requestID = strings.TrimSpace(s)

}
}

}var reqBody io.Reader
if body != nil && method != http.MethodGet && method != http.MethodHead {
b, err := json.Marshal(body)
if err != nil {
return nil, fmt.Errorf("chartly sdk: encode request json: %w", err)

}if int64(len(b)) > c.MaxRequestBytes {
return nil, fmt.Errorf("chartly sdk: request body too large (%d>%d)", len(b), c.MaxRequestBytes)

}reqBody = bytes.NewReader(b)


}req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
if err != nil {
return nil, err


}// Content type for JSON body.
if reqBody != nil {
req.Header.Set("Content-Type", "application/json")


}// Static headers
for k, v := range c.StaticHeaders {
k = strings.TrimSpace(k)
if k == "" {
continue

}req.Header.Set(k, strings.TrimSpace(v))


}// Per-request headers
for k, v := range cfg.headers {
k = strings.TrimSpace(k)
if k == "" {
continue

}req.Header.Set(k, strings.TrimSpace(v))


}// Tenancy/request correlation headers
if cfg.tenantID != "" && c.TenantHeader != "" {
req.Header.Set(c.TenantHeader, cfg.tenantID)

}if cfg.requestID != "" && c.RequestHeader != "" {
req.Header.Set(c.RequestHeader, cfg.requestID)


}// Tracing propagation:
// - If WithSpanContext provided, use it.
// - Else if ctx contains SpanContext, use it.
sc := telemetry.SpanContext{}
if cfg.haveTrace {
sc = cfg.traceState
} else if got, ok := telemetry.SpanContextFromContext(ctx); ok {
sc = got

}if sc.TraceID != "" && sc.SpanID != "" {
carrier := telemetry.Carrier{}
_ = c.Propagator.Inject(carrier, sc)
// Apply carrier headers
for hk, hv := range carrier {
if hk == "" || hv == "" {
continue

}req.Header.Set(hk, hv)

}

}resp, err := c.HTTP.Do(req)
if err != nil {
return nil, err

}defer resp.Body.Close()

// Bounded read
lr := io.LimitReader(resp.Body, c.MaxResponseBytes+1)
raw, err := io.ReadAll(lr)
if err != nil {
return nil, err

}if int64(len(raw)) > c.MaxResponseBytes {
return nil, fmt.Errorf("chartly sdk: response body too large (%d>%d)", len(raw), c.MaxResponseBytes)


}if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
return raw, nil


}// Try parse Chartly error envelope.
apiErr := parseErrorEnvelope(resp.StatusCode, raw)
return nil, apiErr
}

type errorEnvelope struct {
Error struct {
Code      string `json:"code"`
Message   string `json:"message"`
Retryable bool   `json:"retryable"`
Kind      string `json:"kind"`
RequestID string `json:"request_id"`
TraceID   string `json:"trace_id"`
} `json:"error"`
}

func parseErrorEnvelope(status int, raw []byte) *APIError {
// Default fallback
out := &APIError{
Status:  status,
Code:    chartlyerrors.Internal,
Message: "request failed",
Retryable: true,
Kind:    "server",
RawBody: raw,


}// Attempt decode
var env errorEnvelope
dec := json.NewDecoder(bytes.NewReader(raw))
dec.UseNumber()
if err := dec.Decode(&env); err != nil {
return out


}if env.Error.Code != "" {
out.Code = chartlyerrors.Code(env.Error.Code)
if meta, ok := chartlyerrors.Meta(out.Code); ok {
out.Retryable = meta.Retryable
out.Kind = meta.Kind

}
}if env.Error.Message != "" {
out.Message = env.Error.Message

}if env.Error.Kind != "" {
out.Kind = env.Error.Kind

}if env.Error.RequestID != "" {
out.RequestID = env.Error.RequestID

}if env.Error.TraceID != "" {
out.TraceID = env.Error.TraceID

}// If unknown code, keep internal meta.
if !chartlyerrors.Known(out.Code) {
out.Code = chartlyerrors.Internal
out.Retryable = true
out.Kind = "server"

}return out
}
