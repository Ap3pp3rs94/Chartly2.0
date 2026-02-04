package telemetry

// Carrier is a simple header map used for trace propagation.
type Carrier map[string]string

// Propagator injects a SpanContext into a Carrier.
// Implementations are provider-neutral and deterministic.
type Propagator interface {
    Inject(carrier Carrier, sc SpanContext) error
}

// NopPropagator is a no-op propagator.
type NopPropagator struct{}

// Inject performs no action and returns nil.
func (NopPropagator) Inject(carrier Carrier, sc SpanContext) error {
    return nil
}
