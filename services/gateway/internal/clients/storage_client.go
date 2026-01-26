package clients

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type StorageClient struct {
	BaseURL string
	HTTP    *http.Client
}

func NewStorageClientFromEnv() StorageClient {
	base := strings.TrimSpace(os.Getenv("STORAGE_BASE_URL"))
	if base == "" {
		base = "http://localhost:8085"
	}
	return StorageClient{
		BaseURL: strings.TrimRight(base, "/"),
		HTTP: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c StorageClient) Health(ctx context.Context) error {
	if c.HTTP == nil {
		return errors.New("storage client http is nil")
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
		return errors.New("storage healthcheck failed")
	}

	return nil
}

func (c StorageClient) QueryMetrics(ctx context.Context, tenantID, sourceID, metricName, start, end string, limit int) (*http.Response, error) {
	if c.HTTP == nil {
		return nil, errors.New("storage client http is nil")
	}
	if strings.TrimSpace(sourceID) == "" {
		return nil, errors.New("sourceID required")
	}

	u, err := url.Parse(c.BaseURL + "/metrics")
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("source_id", sourceID)
	if strings.TrimSpace(metricName) != "" {
		q.Set("metric_name", metricName)
	}
	if strings.TrimSpace(start) != "" {
		q.Set("start", start)
	}
	if strings.TrimSpace(end) != "" {
		q.Set("end", end)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(tenantID) != "" {
		req.Header.Set("X-Tenant-Id", tenantID)
	}

	return c.HTTP.Do(req)
}
