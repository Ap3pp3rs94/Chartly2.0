package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type FetchRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Query   map[string]any
	Body    any
	Retry   ConnectorRetry
	Rate    ConnectorRate
}

func FetchSample(client *http.Client, req FetchRequest, limit int, retries int, backoff time.Duration) ([]any, []string, error) {
	missing := []string{}
	resolvedURL, missingURL := expandVars(req.URL)
	if len(missingURL) > 0 {
		missing = append(missing, missingURL...)
		return nil, missing, errors.New("missing_vars")
	}

	u, err := url.Parse(resolvedURL)
	if err != nil { return nil, missing, err }
	q := u.Query()
	for k, v := range req.Query {
		q.Set(k, toString(v))
	}
	u.RawQuery = q.Encode()

	headers := map[string]string{}
	for k, v := range req.Headers {
		val, miss := expandVars(v)
		if len(miss) > 0 {
			missing = append(missing, miss...)
			continue
		}
		headers[k] = val
	}
	missing = uniqueSorted(missing)

	bodyBytes := []byte(nil)
	if req.Body != nil {
		bodyBytes, _ = json.Marshal(req.Body)
	}

	var out []any
	err = DoWithRetry(retries, backoff, func() error {
		hreq, _ := http.NewRequest(req.Method, u.String(), bytes.NewReader(bodyBytes))
		hreq.Header.Set("User-Agent", "Chartly-Runner/1.0")
		if len(bodyBytes) > 0 {
			hreq.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			hreq.Header.Set(k, v)
		}
		resp, err := client.Do(hreq)
		if err != nil { return err }
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 { return errors.New("fetch_status") }
		b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if err != nil { return err }
		var parsed any
		if err := json.Unmarshal(b, &parsed); err != nil { return err }
		out = extractRecords(parsed, limit)
		return nil
	})
	return out, missing, err
}

func extractRecords(parsed any, limit int) []any {
	if arr, ok := parsed.([]any); ok {
		return limitRecords(arr, limit)
	}
	if obj, ok := parsed.(map[string]any); ok {
		for k, v := range obj {
			if strings.EqualFold(k, "results") {
				if arr, ok := v.([]any); ok {
					return limitRecords(arr, limit)
				}
			}
		}
		return []any{obj}
	}
	return []any{parsed}
}

func limitRecords(arr []any, n int) []any {
	if len(arr) <= n { return arr }
	return arr[:n]
}

var envRe = regexpMust(`\$\{([A-Z0-9_]+)\}`)

func expandVars(s string) (string, []string) {
	out := s
	missing := []string{}
	matches := envRe.FindAllStringSubmatch(s, -1)
	for _, m := range matches {
		key := m[1]
		val := strings.TrimSpace(os.Getenv(key))
		if val == "" {
			missing = append(missing, key)
			continue
		}
		out = strings.ReplaceAll(out, m[0], val)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
	}
	return out, missing
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return trimFloat(t)
	case int:
		return strconv.Itoa(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func uniqueSorted(in []string) []string {
	m := map[string]bool{}
	for _, s := range in { if s != "" { m[s] = true } }
	out := make([]string, 0, len(m))
	for k := range m { out = append(out, k) }
	sort.Strings(out)
	return out
}

func trimFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', -1, 64), "0"), ".")
}

func regexpMust(expr string) *regexp.Regexp {
	r, _ := regexp.Compile(expr)
	return r
}
