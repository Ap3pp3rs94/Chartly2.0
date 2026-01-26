package coordinator

import (
	"context"
	"errors"
)

var (
	ErrRejected = errors.New("job rejected")
	ErrDeferred = errors.New("job deferred")
)

type PoolStatsProvider interface {
	Stats() Stats
}

type Enqueuer interface {
	EnqueueLocal(ctx context.Context, tenantID string, jobID string, payload any) error
}

type Decision struct {
	Action string            `json:"action"` // route_local|defer|reject
	Reason string            `json:"reason"`
	Shard  int               `json:"shard"`
	Tags   map[string]string `json:"tags,omitempty"`
}

type Router struct {
	sharder Sharder
	elector *LeaderElector

	stats PoolStatsProvider
	enq   Enqueuer

	maxQueue        int
	deferThreshold  int
	rejectThreshold int

	logger LoggerFn
}

func NewRouter(shards int, stats PoolStatsProvider, enq Enqueuer, logger LoggerFn) *Router {
	if logger == nil {
		logger = func(string, string, map[string]any) {}
	}
	r := &Router{
		sharder:         NewSharder(shards),
		stats:           stats,
		enq:             enq,
		maxQueue:        1000,
		deferThreshold:  800,
		rejectThreshold: 950,
		logger:          logger,
	}
	return r
}

func (r *Router) WithLeaderElector(e *LeaderElector) *Router {
	r.elector = e
	return r
}

func (r *Router) WithThresholds(maxQueue, deferAt, rejectAt int) *Router {
	// validate monotonic thresholds; keep previous if invalid
	if maxQueue < 1 || deferAt < 0 || rejectAt < 0 {
		return r
	}
	if deferAt > rejectAt || rejectAt > maxQueue {
		return r
	}
	r.maxQueue = maxQueue
	r.deferThreshold = deferAt
	r.rejectThreshold = rejectAt
	return r
}

func (r *Router) Decide(tenantID, jobID string) Decision {
	shard := r.sharder.ShardFor(tenantID, jobID)

	// leadership gating
	if r.elector != nil && !r.elector.IsLeader() {
		return Decision{
			Action: "defer",
			Reason: "not_leader",
			Shard:  shard,
			Tags: map[string]string{
				"leader": "false",
			},
		}
	}

	// queue-aware decisions
	st := Stats{}
	if r.stats != nil {
		st = r.stats.Stats()
	}
	queued := st.Queued
	if queued >= r.rejectThreshold {
		return Decision{
			Action: "reject",
			Reason: "queue_overloaded",
			Shard:  shard,
			Tags: map[string]string{
				"queued": itoa(queued),
			},
		}
	}

	if queued >= r.deferThreshold {
		return Decision{
			Action: "defer",
			Reason: "queue_high",
			Shard:  shard,
			Tags: map[string]string{
				"queued": itoa(queued),
			},
		}
	}

	return Decision{
		Action: "route_local",
		Reason: "ok",
		Shard:  shard,
		Tags: map[string]string{
			"queued": itoa(queued),
		},
	}
}

func (r *Router) Route(ctx context.Context, tenantID, jobID string, payload any) (Decision, error) {
	d := r.Decide(tenantID, jobID)

	st := Stats{}
	if r.stats != nil {
		st = r.stats.Stats()
	}

	// log decision
	r.logger("info", "route_decision", map[string]any{
		"event":     "route_decision",
		"tenant_id": tenantID,
		"job_id":    jobID,
		"action":    d.Action,
		"reason":    d.Reason,
		"shard":     d.Shard,
		"running":   st.Running,
		"queued":    st.Queued,
		"completed": st.Completed,
		"failed":    st.Failed,
		"rejected":  st.Rejected,
	})

	switch d.Action {
	case "route_local":
		if r.enq == nil {
			return d, errors.New("enqueuer is nil")
		}
		if err := r.enq.EnqueueLocal(ctx, tenantID, jobID, payload); err != nil {
			return d, err
		}
		return d, nil
	case "defer":
		return d, ErrDeferred
	case "reject":
		return d, ErrRejected
	default:
		return d, errors.New("unknown decision action")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	buf := make([]byte, 0, 16)
	for i > 0 {
		d := byte(i%10) + '0'
		buf = append(buf, d)
		i /= 10
	}
	// reverse
	for a, b := 0, len(buf)-1; a < b; a, b = a+1, b-1 {
		buf[a], buf[b] = buf[b], buf[a]
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
