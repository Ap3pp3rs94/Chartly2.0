package clients

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

type AnalyticsClient struct {
	BaseURL string
	HTTP    *http.Client
}

func NewAnalyticsClientFromEnv() AnalyticsClient {
	base := strings.TrimSpace(os.Getenv("ANALYTICS_BASE_URL"))
	if base == "" {
		base = "http://localhost:8084"
	}
	return AnalyticsClient{
		BaseURL: strings.TrimRight(base, "/"),
		HTTP: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}
func (c AnalyticsClient) Health(ctx context.Context) error {
	if c.HTTP == nil {
		return errors.New("analytics client http is nil")
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
		return errors.New("analytics healthcheck failed")
	}
	return nil
}
func (c AnalyticsClient) GenerateReport(ctx context.Context, tenantID string, body []byte) (*http.Response, error) {
	if c.HTTP == nil {
		return nil, errors.New("analytics client http is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/reports", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json; charset=utf-8")
	if strings.TrimSpace(tenantID) != "" {
		req.Header.Set("X-Tenant-Id", tenantID)
	}
	return c.HTTP.Do(req)
}
