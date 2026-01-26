package Ollama

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

type Client struct {
	baseURL string
	model   string
	httpc   *http.Client
}

func NewClient(baseURL string, model string) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	m := strings.TrimSpace(model)
	if m == "" {
		m = "llama3.1:8b"
	}
	return &Client{
		baseURL: base,
		model:   m,
		httpc:   &http.Client{Timeout: 60 * time.Second},
	}
}

// Generate calls Ollama POST /api/generate with stream=false and returns the "response" text.
func (c *Client) Generate(ctx context.Context, prompt string, system string, temperature *float64) (string, error) {
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", gwErr("request_error", "failed to create request")
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := c.httpc.Do(httpReq)
	if err != nil {
		return "", gwErr("upstream_error", "failed to call ollama")
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", gwErr("upstream_error", "failed to read ollama response")
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", gwErr("upstream_error", fmt.Sprintf("ollama returned non-2xx (%d)", res.StatusCode))
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
