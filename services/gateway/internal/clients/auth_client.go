package clients

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

type AuthClient struct {
	BaseURL string
	HTTP    *http.Client
}

func NewAuthClientFromEnv() AuthClient {
	base := strings.TrimSpace(os.Getenv("AUTH_BASE_URL"))
	if base == "" {
		base = "http://localhost:8087"
	}
	return AuthClient{
		BaseURL: strings.TrimRight(base, "/"),
		HTTP: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}
func (c AuthClient) Health(ctx context.Context) error {
	if c.HTTP == nil {
		return errors.New("auth client http is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("auth healthcheck failed")
	}
	return nil
}
