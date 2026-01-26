package queue

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

var (
	ErrQueueFull = errors.New("queue full")
)

type Job struct {
	JobID       string            `json:"job_id"`
	TenantID    string            `json:"tenant_id"`
	SourceID    string            `json:"source_id"`
	JobType     string            `json:"job_type"`
	RequestedAt string            `json:"requested_at"` // RFC3339Nano
	Attempt     int               `json:"attempt"`
	Payload     map[string]string `json:"payload,omitempty"`
}

type Envelope struct {
	Version string `json:"version"`
	Type    string `json:"type"` // "job"
	SentAt  string `json:"sent_at"`
	Job     Job    `json:"job"`
}

type Publisher interface {
	Publish(ctx context.Context, job Job) error
}

type Encoder interface {
	Encode(job Job) ([]byte, error)
}

type JSONEncoder struct{}

func (JSONEncoder) Encode(job Job) ([]byte, error) {
	env := Envelope{
		Version: "v1",
		Type:    "job",
		SentAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Job:     job,
	}
	return json.Marshal(env)
}

func Decode(b []byte) (Envelope, error) {
	var env Envelope
	dec := json.NewDecoder(bytesReader(b))
	dec.DisallowUnknownFields()
	err := dec.Decode(&env)
	return env, err
}

// bytesReader avoids importing bytes for a simple Reader.
type bytesReader []byte

func (b bytesReader) Read(p []byte) (int, error) {
	if len(b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b)
	if n < len(b) {
		b = b[n:]
		return n, nil
	}
	return n, io.EOF
}

type InMemoryPublisher struct {
	ch  chan Envelope
	enc Encoder
}

func NewInMemoryPublisher(buf int) *InMemoryPublisher {
	if buf < 1 {
		buf = 1
	}
	return &InMemoryPublisher{
		ch:  make(chan Envelope, buf),
		enc: JSONEncoder{},
	}
}

func (p *InMemoryPublisher) Channel() <-chan Envelope { return p.ch }

func (p *InMemoryPublisher) Publish(ctx context.Context, job Job) error {
	// Encode to validate envelope shape, even if we publish the struct.
	_, _ = p.enc.Encode(job)

	env := Envelope{
		Version: "v1",
		Type:    "job",
		SentAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Job:     job,
	}

	select {
	case p.ch <- env:
		return nil
	case <-ctx.Done():
		return ErrQueueFull
	}
}
