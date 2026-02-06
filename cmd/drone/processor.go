package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Profile struct {
	ID      string            `yaml:"id" json:"id"`
	Name    string            `yaml:"name" json:"name"`
	Version string            `yaml:"version" json:"version"`
	Source  SourceConfig      `yaml:"source" json:"source"`
	Mapping map[string]string `yaml:"mapping" json:"mapping"`
}

type SourceConfig struct {
	Type string `yaml:"type"` // "http_rest"
	URL  string `yaml:"url"`
	Auth string `yaml:"auth"` // "none"
}

func ProcessProfile(profile Profile) ([]map[string]interface{}, error) {
	rawURL := strings.TrimSpace(profile.Source.URL)
	if rawURL == "" {
		logProc("missing_source_url profile_id=%s", profile.ID)
		return []map[string]interface{}{}, fmt.Errorf("missing_source_url")
	}

	expandedURL, err := ExpandEnvPlaceholders(rawURL)
	if err != nil {
		logProc("missing_env_var profile_id=%s err=%s", profile.ID, err.Error())
		return []map[string]interface{}{}, err
	}

	client := &http.Client{Timeout: 30 * time.Second}

	raw, err := fetchSource(client, expandedURL)
	if err != nil {
		logProc("fetch_failed host=%s err=%s", safeHost(expandedURL), err.Error())
		return []map[string]interface{}{}, err
	}

	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		logProc("json_parse_failed host=%s err=%s", safeHost(expandedURL), err.Error())
		return []map[string]interface{}{}, err
	}

	records := normalizeToRecords(parsed)

	out := make([]map[string]interface{}, 0, len(records))
	for _, rec := range records {
		dst := make(map[string]interface{})

		for srcPath, dstPath := range profile.Mapping {
			srcPath = strings.TrimSpace(srcPath)
			dstPath = strings.TrimSpace(dstPath)
			if srcPath == "" || dstPath == "" {
				continue
			}

			val, ok := getValueByPath(rec, srcPath)
			if !ok {
				continue
			}
			setNestedValue(dst, dstPath, val)
		}

		if len(profile.Mapping) == 0 {
			if m, ok := rec.(map[string]interface{}); ok {
				dst = m
			}
		}

		// Inject crypto_id/timeframe from profile ID when available.
		injectCryptoDims(dst, profile.ID)

		// Coerce measures.* numeric strings to float64
		coerceMeasures(dst)

		// Fill dims.time.date/year/month from occurred_at
		fillTimeDims(dst)

		// Compute record_id
		recForHash := cloneMapWithoutKey(dst, "record_id")
		canon := canonicalJSONBytes(recForHash)
		sum := sha256.Sum256(canon)
		dst["record_id"] = "sha256:" + hex.EncodeToString(sum[:])

		out = append(out, dst)
	}

	return out, nil
}

func ExpandEnvPlaceholders(s string) (string, error) {
	re := regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, nil
	}

	var buf strings.Builder
	last := 0
	for _, m := range matches {
		start := m[0]
		end := m[1]
		nameStart := m[2]
		nameEnd := m[3]
		name := s[nameStart:nameEnd]
		val := strings.TrimSpace(os.Getenv(name))
		if val == "" {
			return "", fmt.Errorf("missing env var %s", name)
		}
		buf.WriteString(s[last:start])
		buf.WriteString(val)
		last = end
	}
	buf.WriteString(s[last:])
	return buf.String(), nil
}

func fetchSource(client *http.Client, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if isBlockedHost(u.Hostname()) {
		return nil, fmt.Errorf("blocked_host")
	}

	ua := userAgent()

	// Liberty: BLS timeseries endpoint requires POST; profiles may specify only URL.
	if strings.EqualFold(u.Host, "api.bls.gov") && strings.Contains(u.Path, "/publicAPI/v2/timeseries/data/") {
		payload := map[string]any{
			"seriesid": []string{"LNS14000000"},
		}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", ua)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			return nil, fmt.Errorf("http_status_%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	}

	req, _ := http.NewRequest(http.MethodGet, rawURL, nil)
	req.Header.Set("User-Agent", ua)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("http_status_%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func userAgent() string {
	ua := strings.TrimSpace(os.Getenv("CHARTLY_USER_AGENT"))
	if ua == "" {
		return "Chartly-Drone/1.0"
	}
	return ua
}

func safeHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}
	h := strings.TrimSpace(u.Hostname())
	if h == "" {
		return "unknown"
	}
	return h
}

func isBlockedHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return true
	}
	if h == "localhost" {
		return true
	}
	if h == "127.0.0.1" {
		return true
	}
	if strings.HasPrefix(h, "169.254.") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		if ip.IsLoopback() {
			return true
		}
		if strings.HasPrefix(h, "169.254.") {
			return true
		}
	}
	return false
}

func normalizeToRecords(parsed any) []any {
	if obj, ok := parsed.(map[string]any); ok {
		// CoinGecko market_chart: expand prices/market_caps/total_volumes into records.
		if pv, ok := obj["prices"].([]any); ok {
			prices := pv
			caps, _ := obj["market_caps"].([]any)
			vols, _ := obj["total_volumes"].([]any)
			max := len(prices)
			if len(caps) > 0 && len(caps) < max {
				max = len(caps)
			}
			if len(vols) > 0 && len(vols) < max {
				max = len(vols)
			}
			out := make([]any, 0, max)
			for i := 0; i < max; i++ {
				row := make(map[string]any)
				if pair, ok := prices[i].([]any); ok && len(pair) >= 2 {
					row["timestamp"] = pair[0]
					row["price"] = pair[1]
				}
				if len(caps) > 0 {
					if pair, ok := caps[i].([]any); ok && len(pair) >= 2 {
						row["market_cap"] = pair[1]
					}
				}
				if len(vols) > 0 {
					if pair, ok := vols[i].([]any); ok && len(pair) >= 2 {
						row["volume"] = pair[1]
					}
				}
				out = append(out, row)
			}
			if len(out) > 0 {
				return out
			}
		}

		// CoinGecko simple/price: map of objects -> records with crypto_id
		if allMapValues(obj) {
			out := make([]any, 0, len(obj))
			keys := make([]string, 0, len(obj))
			for k := range obj {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if mv, ok := obj[k].(map[string]any); ok {
					row := make(map[string]any)
					row["crypto_id"] = k
					for kk, vv := range mv {
						row[kk] = vv
					}
					out = append(out, row)
				}
			}
			if len(out) > 0 {
				return out
			}
		}

		// Open-Meteo: if object contains "hourly" with parallel arrays, expand into records.
		if hv, exists := obj["hourly"]; exists {
			if hmap, ok := hv.(map[string]any); ok {
				timeArr, _ := hmap["time"].([]any)
				if len(timeArr) > 0 {
					keys := make([]string, 0, len(hmap))
					for k, v := range hmap {
						if arr, ok := v.([]any); ok && len(arr) == len(timeArr) {
							keys = append(keys, k)
						}
					}
					sort.Strings(keys)
					out := make([]any, 0, len(timeArr))
					for i := 0; i < len(timeArr); i++ {
						row := make(map[string]any)
						for _, k := range keys {
							arr := hmap[k].([]any)
							row[k] = arr[i]
						}
						out = append(out, row)
					}
					return out
				}
			}
		}
	}

	// Census style: top-level array-of-arrays with header row.
	if arr, ok := parsed.([]any); ok && len(arr) > 0 {
		if isArrayOfArraysWithHeader(arr) {
			return censusToObjects(arr)
		}
		return arr
	}

	if obj, ok := parsed.(map[string]any); ok {
		for k, v := range obj {
			if strings.EqualFold(k, "results") {
				if a, ok := v.([]any); ok {
					return a
				}
			}
		}
		return []any{obj}
	}

	return []any{parsed}
}

func allMapValues(obj map[string]any) bool {
	if len(obj) == 0 {
		return false
	}
	for _, v := range obj {
		if _, ok := v.(map[string]any); !ok {
			return false
		}
	}
	return true
}

func injectCryptoDims(rec map[string]interface{}, profileID string) {
	if rec == nil {
		return
	}
	if v := getNestedValue(rec, "dims.crypto_id"); v != nil {
		return
	}
	if !strings.HasPrefix(profileID, "crypto-") {
		return
	}
	parts := strings.Split(profileID, "-")
	if len(parts) < 3 {
		return
	}
	if strings.HasPrefix(profileID, "crypto-top10") {
		return
	}
	// crypto-<id>-<timeframe>
	tf := parts[len(parts)-1]
	id := strings.Join(parts[1:len(parts)-1], "-")
	if id != "" {
		setNestedValue(rec, "dims.crypto_id", id)
	}
	if tf != "" {
		setNestedValue(rec, "dims.timeframe", tf)
	}
}

func isArrayOfArraysWithHeader(arr []any) bool {
	if len(arr) < 2 {
		return false
	}
	h, ok := arr[0].([]any)
	if !ok || len(h) == 0 {
		return false
	}
	for _, v := range h {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	_, ok = arr[1].([]any)
	return ok
}

func censusToObjects(arr []any) []any {
	headerRow := arr[0].([]any)
	headers := make([]string, 0, len(headerRow))
	for _, h := range headerRow {
		headers = append(headers, fmt.Sprintf("%v", h))
	}

	out := make([]any, 0, len(arr)-1)
	for i := 1; i < len(arr); i++ {
		row, ok := arr[i].([]any)
		if !ok {
			continue
		}
		obj := make(map[string]any)
		for j := 0; j < len(headers) && j < len(row); j++ {
			obj[headers[j]] = row[j]
		}
		out = append(out, obj)
	}
	return out
}

func setNestedValue(obj map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	cur := obj
	for i := 0; i < len(parts); i++ {
		p := strings.TrimSpace(parts[i])
		if p == "" {
			return
		}
		if i == len(parts)-1 {
			cur[p] = value
			return
		}
		next, ok := cur[p]
		if ok {
			if m, ok := next.(map[string]interface{}); ok {
				cur = m
				continue
			}
		}
		nm := make(map[string]interface{})
		cur[p] = nm
		cur = nm
	}
}

type tokenKind int

const (
	tokField tokenKind = iota
	tokIndex
)

type pathToken struct {
	kind  tokenKind
	field string
	index int
}

func getValueByPath(obj interface{}, path string) (interface{}, bool) {
	toks, ok := parsePath(path)
	if !ok {
		return nil, false
	}
	cur := obj
	for _, t := range toks {
		switch t.kind {
		case tokField:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			v, exists := m[t.field]
			if !exists {
				return nil, false
			}
			cur = v
		case tokIndex:
			a, ok := cur.([]any)
			if !ok {
				return nil, false
			}
			if t.index < 0 || t.index >= len(a) {
				return nil, false
			}
			cur = a[t.index]
		default:
			return nil, false
		}
	}
	return cur, true
}

func parsePath(path string) ([]pathToken, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false
	}

	out := make([]pathToken, 0, 8)
	i := 0
	for i < len(path) {
		if path[i] == '.' {
			i++
			continue
		}
		if path[i] == '[' {
			j := strings.IndexByte(path[i:], ']')
			if j < 0 {
				return nil, false
			}
			j = i + j
			num := strings.TrimSpace(path[i+1 : j])
			n, err := strconv.Atoi(num)
			if err != nil {
				return nil, false
			}
			out = append(out, pathToken{kind: tokIndex, index: n})
			i = j + 1
			continue
		}

		start := i
		for i < len(path) && path[i] != '.' && path[i] != '[' {
			i++
		}
		field := strings.TrimSpace(path[start:i])
		if field == "" {
			return nil, false
		}
		out = append(out, pathToken{kind: tokField, field: field})
	}
	return out, true
}

func coerceMeasures(rec map[string]interface{}) {
	m, ok := rec["measures"].(map[string]interface{})
	if !ok {
		return
	}
	for k, v := range m {
		if s, ok := v.(string); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
				m[k] = f
			}
		}
	}
	rec["measures"] = m
}

func fillTimeDims(rec map[string]interface{}) {
	var ts string
	if v := getNestedValue(rec, "dims.time.occurred_at"); v != nil {
		if s, ok := v.(string); ok {
			ts = s
		}
	}
	if ts == "" {
		if v, ok := rec["occurred_at"]; ok {
			if s, ok := v.(string); ok {
				ts = s
			}
		}
	}
	if ts == "" {
		return
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return
	}
	date := t.UTC().Format("2006-01-02")
	year, month, _ := t.Date()

	if getNestedValue(rec, "dims.time.date") == nil {
		setNestedValue(rec, "dims.time.date", date)
	}
	if getNestedValue(rec, "dims.time.year") == nil {
		setNestedValue(rec, "dims.time.year", int(year))
	}
	if getNestedValue(rec, "dims.time.month") == nil {
		setNestedValue(rec, "dims.time.month", int(month))
	}
}

func getNestedValue(obj map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	var cur interface{} = obj
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil
		}
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		v, ok := m[p]
		if !ok {
			return nil
		}
		cur = v
	}
	return cur
}

func cloneMapWithoutKey(in map[string]interface{}, key string) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		if k == key {
			continue
		}
		out[k] = v
	}
	return out
}

func canonicalJSONBytes(v any) []byte {
	b, _ := json.Marshal(v)
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err == nil {
		return append(buf.Bytes(), '\n')
	}
	return append(b, '\n')
}

func logProc(format string, args ...any) {
	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Printf("%s WARN processor %s\n", ts, fmt.Sprintf(format, args...))
}
