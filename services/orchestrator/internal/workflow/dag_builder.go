package workflow

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrNodeExists    = errors.New("node already exists")
	ErrNodeMissing   = errors.New("node missing")
	ErrEdgeInvalid   = errors.New("edge invalid")
	ErrCycleDetected = errors.New("cycle detected")
)

// type NodeID string

type Node struct {
	ID     NodeID            `json:"id"`
	Kind   string            `json:"kind"`
	Params map[string]string `json:"params,omitempty"`
}
type Edge struct {
	From NodeID `json:"from"`
	To   NodeID `json:"to"`
}
type DAG struct {
	Nodes map[NodeID]Node `json:"nodes"`
	Edges []Edge          `json:"edges"`
}

func NewDAG() *DAG {
	return &DAG{
		Nodes: make(map[NodeID]Node),
		Edges: make([]Edge, 0),
	}
}
func (d *DAG) AddNode(n Node) error {
	if d == nil {
		return errors.New("dag is nil")
	}
	if n.ID == "" {
		return fmt.Errorf("%w: empty id", ErrEdgeInvalid)
	}
	if strings.TrimSpace(n.Kind) == "" {
		return fmt.Errorf("%w: kind required", ErrEdgeInvalid)
	}
	if _, ok := d.Nodes[n.ID]; ok {
		return fmt.Errorf("%w: %s", ErrNodeExists, n.ID)
	}
	d.Nodes[n.ID] = n
	return nil
}
func (d *DAG) AddEdge(from, to NodeID) error {
	if d == nil {
		return errors.New("dag is nil")
	}
	if from == "" || to == "" {
		return fmt.Errorf("%w: empty endpoint", ErrEdgeInvalid)
	}
	if from == to {
		return fmt.Errorf("%w: self edge", ErrEdgeInvalid)
	}
	if _, ok := d.Nodes[from]; !ok {
		return fmt.Errorf("%w: from=%s", ErrNodeMissing, from)
	}
	if _, ok := d.Nodes[to]; !ok {
		return fmt.Errorf("%w: to=%s", ErrNodeMissing, to)
	}
	d.Edges = append(d.Edges, Edge{From: from, To: to})
	return nil
}
func (d *DAG) Validate() error {
	if d == nil {
		return errors.New("dag is nil")
	}
	for _, e := range d.Edges {
		if _, ok := d.Nodes[e.From]; !ok {
			return fmt.Errorf("%w: from=%s", ErrNodeMissing, e.From)
		}
		if _, ok := d.Nodes[e.To]; !ok {
			return fmt.Errorf("%w: to=%s", ErrNodeMissing, e.To)
		}
		if e.From == e.To {
			return fmt.Errorf("%w: self edge", ErrEdgeInvalid)
		}
	}
	_, err := d.TopoSort()
	return err
}

// TopoSort returns a deterministic stable topological ordering using Kahn's algorithm.
// Stability: when multiple nodes are available, choose lexicographically by NodeID.
func (d *DAG) TopoSort() ([]NodeID, error) {
	if d == nil {
		return nil, errors.New("dag is nil")
	}
	adj := make(map[NodeID][]NodeID, len(d.Nodes))
	indeg := make(map[NodeID]int, len(d.Nodes))
	for id := range d.Nodes {
		adj[id] = nil
		indeg[id] = 0
	}
	for _, e := range d.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		indeg[e.To]++
	}
	for k := range adj {
		sort.Slice(adj[k], func(i, j int) bool { return adj[k][i] < adj[k][j] })
	}
	zeros := make([]NodeID, 0)
	for id, n := range indeg {
		if n == 0 {
			zeros = append(zeros, id)
		}
	}
	sort.Slice(zeros, func(i, j int) bool { return zeros[i] < zeros[j] })
	out := make([]NodeID, 0, len(d.Nodes))
	for len(zeros) > 0 {
		id := zeros[0]
		zeros = zeros[1:]

		out = append(out, id)
		for _, to := range adj[id] {
			indeg[to]--
			if indeg[to] == 0 {
				zeros = append(zeros, to)
			}
		}
		sort.Slice(zeros, func(i, j int) bool { return zeros[i] < zeros[j] })
	}
	if len(out) != len(d.Nodes) {
		return nil, ErrCycleDetected
	}
	return out, nil
}
func (d *DAG) ReverseTopoSort() ([]NodeID, error) {
	order, err := d.TopoSort()
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}
	return order, nil
}
func (d *DAG) Dependents(id NodeID) []NodeID {
	if d == nil {
		return nil
	}
	set := make(map[NodeID]struct{})
	for _, e := range d.Edges {
		if e.From == id {
			set[e.To] = struct{}{}
		}
	}
	return sortedKeys(set)
}
func (d *DAG) Dependencies(id NodeID) []NodeID {
	if d == nil {
		return nil
	}
	set := make(map[NodeID]struct{})
	for _, e := range d.Edges {
		if e.To == id {
			set[e.From] = struct{}{}
		}
	}
	return sortedKeys(set)
}
func sortedKeys(m map[NodeID]struct{}) []NodeID {
	out := make([]NodeID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

////////////////////////////////////////////////////////////////////////////////
// Builder (plan -> DAG conversion)
////////////////////////////////////////////////////////////////////////////////

type JobPlan struct {
	JobType  string     `json:"job_type"`
	SourceID string     `json:"source_id"`
	Steps    []PlanStep `json:"steps"`
}
type PlanStep struct {
	ID     string            `json:"id"`
	Kind   string            `json:"kind"`
	After  []string          `json:"after,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// BuildFromPlan builds a DAG from a job plan.
// This is a pure transform + validation; execution is handled elsewhere.
func BuildFromPlan(p JobPlan) (*DAG, error) {
	d := NewDAG()
	for _, s := range p.Steps {
		nid := NodeID(strings.TrimSpace(s.ID))
		if nid == "" {
			return nil, fmt.Errorf("%w: empty step id", ErrEdgeInvalid)
		}
		kind := strings.TrimSpace(s.Kind)
		if kind == "" {
			return nil, fmt.Errorf("%w: step kind required", ErrEdgeInvalid)
		}
		if err := d.AddNode(Node{ID: nid, Kind: kind, Params: s.Params}); err != nil {
			return nil, err
		}
	}
	for _, s := range p.Steps {
		to := NodeID(strings.TrimSpace(s.ID))
		for _, dep := range s.After {
			from := NodeID(strings.TrimSpace(dep))
			if from == "" {
				return nil, fmt.Errorf("%w: empty dependency", ErrEdgeInvalid)
			}
			if err := d.AddEdge(from, to); err != nil {
				return nil, err
			}
		}
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
}
