package llm

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"
)

// Client defines the LLM boundary used by gateway runtime.
// Implementations MUST be deterministic and auditable at the call site.
type Client interface {
	Generate(ctx context.Context, prompt string, system string, temperature *float64) (string, error)
}

// Config controls LLM selection and defaults. Ollama-only by policy.
type Config struct {
	BaseURL    string
	Model      string
	TimeoutSec int
}

// LoadConfigFromEnv reads Ollama settings from env. No OpenAI/Codex runtime coupling.
func LoadConfigFromEnv() Config {
	base := strings.TrimSpace(os.Getenv("OLLAMA_ENDPOINT"))
if base == "" {
		base = "http://127.0.0.1:11434"
	}
	model := strings.TrimSpace(os.Getenv("OLLAMA_MODEL"))
if model == "" {
		model = "llama3.1:8b"
	}

	// Optional override; keep sane default.
	t := strings.TrimSpace(os.Getenv("OLLAMA_TIMEOUT_SECONDS"))
timeout := 60
	if t != "" {
		if n, err := parseInt(t); err == nil && n > 0 {
			timeout = n
		}
	}
	return Config{BaseURL: base, Model: model, TimeoutSec: timeout}
}

// NewClient returns an Ollama-only client implementation.
func NewClient(cfg Config) (Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("llm_config_error: base_url required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("llm_config_error: model required")
	}
	return newOllamaClient(cfg), nil
}
func parseInt(s string) (int, error) {
	// minimal helper to avoid pulling strconv into consumers
	// var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid int")
		}
		n = n*10 + int(r-'0')
if n > 3600 {
			return 0, errors.New("too large")
		}
	}
	return n, nil
}

// withTimeout returns a context with timeout if configured.
func withTimeout(ctx context.Context, sec int) (context.Context, context.CancelFunc) {
	if sec <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(sec)
*time.Second)
}
