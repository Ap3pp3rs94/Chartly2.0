package main

import (
"context"
"flag"
"fmt"
"os"
"time"

chartly "github.com/Ap3pp3rs94/Chartly2.0/sdk/go"
"github.com/Ap3pp3rs94/Chartly2.0/pkg/telemetry"
)

func main() {
var (
baseURL   = flag.String("base", "http://localhost:8080", "Chartly base URL (typically gateway)")
tenant    = flag.String("tenant", "local", "Tenant id (header value)")
requestID = flag.String("request", "", "Request ID (optional)")
timeout   = flag.Duration("timeout", 10*time.Second, "Request timeout")

)flag.Parse()

ctx, cancel := context.WithTimeout(context.Background(), *timeout)
defer cancel()

// If request id not provided, use a deterministic fallback (timestamp-free)
// to keep logs stable in tests. For real apps, generate a UUID upstream.
rid := *requestID
if rid == "" {
rid = "req_basic_client"


}// Optional: show how to create a W3C trace context and propagate it.
// (If you already have an inbound traceparent, just forward it instead.)
tid, err := telemetry.NewTraceID()
if err != nil {
fmt.Fprintln(os.Stderr, "trace id error:", err)
os.Exit(2)

}sid, err := telemetry.NewSpanID()
if err != nil {
fmt.Fprintln(os.Stderr, "span id error:", err)
os.Exit(2)

}sc := telemetry.SpanContext{
TraceID: tid,
SpanID:  sid,
Sampled: false,

}ctx = telemetry.WithSpanContext(ctx, sc)

c := chartly.NewClient(*baseURL)

fmt.Println("== Chartly basic client ==")
fmt.Println("base:", c.BaseURL)
fmt.Println("tenant:", *tenant)
fmt.Println("request:", rid)

health, err := c.Health(ctx,
chartly.WithTenant(*tenant),
chartly.WithRequestID(rid),

)if err != nil {
fmt.Fprintln(os.Stderr, "health error:", err)
os.Exit(1)

}fmt.Println("\n/health:")
fmt.Println(string(health))

ready, err := c.Ready(ctx,
chartly.WithTenant(*tenant),
chartly.WithRequestID(rid),

)if err != nil {
fmt.Fprintln(os.Stderr, "ready error:", err)
os.Exit(1)

}fmt.Println("\n/ready:")
fmt.Println(string(ready))
}
