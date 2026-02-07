package runner

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Profile struct {
	ID          string
	Name        string
	Version     string
	Description string
	Mapping     map[string]string
	Source      LegacySource
	Connector   Connector
	RateLimit   int
}

type LegacySource struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
	Auth string `yaml:"auth"`
}

type Connector struct {
	Type     string         `yaml:"type"`
	Request  ConnectorReq   `yaml:"request"`
	Retry    ConnectorRetry `yaml:"retry"`
	Rate     ConnectorRate  `yaml:"rateLimit"`
	Paging   ConnectorPage  `yaml:"pagination"`
}

type ConnectorReq struct {
	Method  string            `yaml:"method"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
	Query   map[string]any    `yaml:"query"`
	Body    any               `yaml:"body"`
}

type ConnectorRetry struct {
	MaxAttempts int `yaml:"maxAttempts"`
	BackoffMs   int `yaml:"backoffMs"`
}

type ConnectorRate struct {
	MaxRPM int `yaml:"maxRequestsPerMinute"`
}

type ConnectorPage struct {
	Mode string `yaml:"mode"`
}

type profileDoc struct {
	ID          string            `yaml:"id"`
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description"`
	Source      LegacySource      `yaml:"source"`
	Mapping     map[string]string `yaml:"mapping"`
	Connector   Connector          `yaml:"connector"`
}

func LoadProfiles(root string, allow []string) ([]Profile, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	allowSet := map[string]bool{}
	for _, a := range allow {
		allowSet[strings.TrimSpace(a)] = true
	}

	files := make([]string, 0)
	for _, e := range entries {
		if e.IsDir() { continue }
		n := e.Name()
		if strings.HasSuffix(strings.ToLower(n), ".yaml") {
			files = append(files, n)
		}
	}
	sort.Strings(files)

	out := make([]Profile, 0)
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil { continue }
		var doc profileDoc
		dec := yaml.NewDecoder(strings.NewReader(string(b)))
		dec.KnownFields(false)
		if err := dec.Decode(&doc); err != nil { continue }
		id := strings.TrimSpace(doc.ID)
		if id == "" { continue }
		if len(allowSet) > 0 && !allowSet[id] { continue }

		p := Profile{
			ID:          id,
			Name:        strings.TrimSpace(doc.Name),
			Version:     strings.TrimSpace(doc.Version),
			Description: strings.TrimSpace(doc.Description),
			Mapping:     doc.Mapping,
			Source:      doc.Source,
			Connector:   doc.Connector,
			RateLimit:   doc.Connector.Rate.MaxRPM,
		}
		out = append(out, p)
	}
	return out, nil
}

func BuildFetchRequest(p Profile) (FetchRequest, error) {
	if p.Connector.Type != "" || p.Connector.Request.URL != "" {
		if p.Connector.Request.URL == "" {
			return FetchRequest{}, errors.New("missing_connector_url")
		}
		method := strings.ToUpper(strings.TrimSpace(p.Connector.Request.Method))
		if method == "" { method = "GET" }
		return FetchRequest{
			Method:  method,
			URL:     p.Connector.Request.URL,
			Headers: p.Connector.Request.Headers,
			Query:   p.Connector.Request.Query,
			Body:    p.Connector.Request.Body,
			Retry:   p.Connector.Retry,
			Rate:    p.Connector.Rate,
		}, nil
	}
	if p.Source.URL == "" {
		return FetchRequest{}, errors.New("missing_source_url")
	}
	return FetchRequest{Method: "GET", URL: p.Source.URL}, nil
}
