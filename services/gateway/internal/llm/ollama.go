package llm

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
)

type ollamaClient struct {
	baseURL string
	model   string
	timeout int
	httpc   *http.Client
}

func newOllamaClient(cfg Config) *ollamaClient {
	t := cfg.TimeoutSec
	if t <= 0 {
		t = 60
	}
	return &ollamaClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		timeout: t,
		httpc:   &http.Client{Timeout: time.Duration(t) * time.Second},
	}
}

// Generate calls Ollama POST /api/generate with stream=false and returns the "response" text.
func (c *ollamaClient) Generate(ctx context.Context, prompt string, system string, temperature *float64) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", gwErr("validation_error", "prompt is required")
	}

	reqBody := map[string]any{
		"model":  c.model,
		"prompt": prompt,
		"stream": false,
	}
	if strings.TrimSpace(system) != "" {
		reqBody["system"] = system
	}
	if temperature != nil {
		reqBody["options"] = map[string]any{"temperature": *temperature}
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", gwErr("encode_error", "failed to encode request")
	}

	url := c.baseURL + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", gwErr("request_error", "failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return "", gwErr("upstream_error", "failed to call ollama")
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", gwErr("upstream_error", "failed to read ollama response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", gwErr("upstream_error", fmt.Sprintf("ollama returned non-2xx (%d)", resp.StatusCode))
	}

	var parsed struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", gwErr("decode_error", "failed to parse ollama response")
	}

	return parsed.Response, nil
}

// gwErr formats errors to match {"error":{"code","message"}} semantics via the error message.
func gwErr(code, message string) error {
	if code == "" {
		code = "error"
	}
	if message == "" {
		message = "unknown error"
	}
	return errors.New(fmt.Sprintf(`{"error":{"code":"%s","message":"%s"}}`, code, message))
}
