package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)
var (
	ErrNoHandler   = errors.New("no handler for step kind")
ErrStepTimeout = errors.New("step timeout")
ErrDagInvalid  = errors.New("dag invalid")
)

// StepHandler executes a single DAG node step.
type StepHandler interface {
	Run(ctx context.Context, step Node, in ExecContext) (ExecContext, error)
}

// Registry provides handlers by kind.
type Registry interface {
	Get(kind string) (StepHandler, bool)
}

// ExecContext is mutable workflow state passed between steps.
type ExecContext struct {
	TenantID string            `json:"tenant_id"`
	JobID    string            `json:"job_id"`
	SourceID string            `json:"source_id"`
	JobType  string            `json:"job_type"`
	Vars     map[string]string `json:"vars,omitempty"`
	Trace    []TraceEvent      `json:"trace,omitempty"`
}

// TraceEvent is an append-only execution trace record.
type TraceEvent struct {
	Ts         string `json:"ts"`
	StepID     string `json:"step_id"`
	Kind       string `json:"kind"`
	Status     string `json:"status"` // start|ok|error|skip
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// LoggerFn is a structured logger signature.
type LoggerFn func(level, msg string, fields map[string]any) type Executor struct {
	reg             Registry
	logger          LoggerFn
	maxStepDuration time.Duration
	hardFail        bool
}

func NewExecutor(reg Registry, logger LoggerFn) *Executor {
	if logger == nil {
		logger = func(string, string, map[string]any) {}
	}
	return &Executor{
		reg:             reg,
		logger:          logger,
		maxStepDuration: 2 * time.Minute,
		hardFail:        true,
	}
}
func (e *Executor) WithMaxStepDuration(d time.Duration) *Executor {
	if d > 0 {
		e.maxStepDuration = d
	}
	return e
}
func (e *Executor) WithHardFail(v bool) *Executor {
	e.hardFail = v
	return e
}

// Execute runs the DAG in deterministic topo order using registered handlers.
func (e *Executor) Execute(ctx context.Context, dag *DAG, ec ExecContext) (ExecContext, error) {
	if dag == nil {
		return ec, ErrDagInvalid
	}
	if err := dag.Validate(); err != nil {
		return ec, fmt.Errorf("%w: %v", ErrDagInvalid, err)
	}
	if ec.Vars == nil {
		ec.Vars = make(map[string]string)
	}
	if ec.Trace == nil {
		ec.Trace = make([]TraceEvent, 0, len(dag.Nodes)
*2)
	}
	order, err := dag.TopoSort()
if err != nil {
		return ec, fmt.Errorf("%w: %v", ErrDagInvalid, err)
	}
	for _, id := range order {
		select {
		case <-ctx.Done():
			return ec, ctx.Err()
default:
		}
		node, ok := dag.Nodes[id]
		if !ok {
			return ec, fmt.Errorf("%w: missing node %s", ErrDagInvalid, id)
		}
		kindKey := strings.ToLower(strings.TrimSpace(node.Kind))
stepID := string(node.ID)
ec = e.appendTrace(ec, TraceEvent{
			Ts:         time.Now().UTC().Format(time.RFC3339Nano),
			StepID:     stepID,
			Kind:       node.Kind,
			Status:     "start",
			DurationMs: 0,
		})
// var h StepHandler
		ok = false
		if e.reg != nil {
			h, ok = e.reg.Get(kindKey)
if !ok {
				h, ok = e.reg.Get(node.Kind)
			}
		}
		if !ok || h == nil {
			msg := fmt.Sprintf("%v: %s", ErrNoHandler, node.Kind)
e.logger("warn", "step_no_handler", map[string]any{
				"event":     "step_no_handler",
				"tenant_id": ec.TenantID,
				"job_id":    ec.JobID,
				"source_id": ec.SourceID,
				"step_id":   stepID,
				"kind":      node.Kind,
			})
ec = e.appendTrace(ec, TraceEvent{
				Ts:         time.Now().UTC().Format(time.RFC3339Nano),
				StepID:     stepID,
				Kind:       node.Kind,
				Status:     "skip",
				DurationMs: 0,
				Error:      msg,
			})
if e.hardFail {
				return ec, ErrNoHandler
			}
			continue
		}
		stepCtx, cancel := context.WithTimeout(ctx, e.maxStepDuration)
start := time.Now()
out, runErr := h.Run(stepCtx, node, ec)
cancel()
dur := time.Since(start).Milliseconds()
if runErr != nil {
			code := runErr.Error()
if errors.Is(stepCtx.Err(), context.DeadlineExceeded) {
				code = ErrStepTimeout.Error()
runErr = ErrStepTimeout
			}
			e.logger("error", "step_error", map[string]any{
				"event":       "step_error",
				"tenant_id":   ec.TenantID,
				"job_id":      ec.JobID,
				"source_id":   ec.SourceID,
				"step_id":     stepID,
				"kind":        node.Kind,
				"duration_ms": dur,
				"error":       code,
			})
ec = e.appendTrace(ec, TraceEvent{
				Ts:         time.Now().UTC().Format(time.RFC3339Nano),
				StepID:     stepID,
				Kind:       node.Kind,
				Status:     "error",
				DurationMs: dur,
				Error:      code,
			})
if e.hardFail {
				return ec, runErr
			}
			continue
		}
		ec = out
		ec = e.appendTrace(ec, TraceEvent{
			Ts:         time.Now().UTC().Format(time.RFC3339Nano),
			StepID:     stepID,
			Kind:       node.Kind,
			Status:     "ok",
			DurationMs: dur,
		})
e.logger("info", "step_ok", map[string]any{
			"event":       "step_ok",
			"tenant_id":   ec.TenantID,
			"job_id":      ec.JobID,
			"source_id":   ec.SourceID,
			"step_id":     stepID,
			"kind":        node.Kind,
			"duration_ms": dur,
		})
	}
	return ec, nil
}
func (e *Executor) appendTrace(ec ExecContext, ev TraceEvent) ExecContext {
	ec.Trace = append(ec.Trace, ev)
// return ec
}
