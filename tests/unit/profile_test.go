package unit

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type Profile struct {
	Name           string
	TenantID       string
	BaseURL        string
	DefaultHeaders map[string]string
	Timeouts       struct {
		RequestMS int
		ConnectMS int
	}
}

func (p Profile) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name is required")
	}

	// TenantID is optional, but if present must match: [A-Za-z0-9._-]+
	if p.TenantID != "" {
		for _, ch := range p.TenantID {
			ok := (ch >= 'a' && ch <= 'z') ||
				(ch >= 'A' && ch <= 'Z') ||
				(ch >= '0' && ch <= '9') ||
				ch == '.' || ch == '_' || ch == '-'
			if !ok {
				return fmt.Errorf("tenant_id invalid: %q", p.TenantID)
			}
		}
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return errors.New("base_url is required")
	}
	if !(strings.HasPrefix(p.BaseURL, "http://") || strings.HasPrefix(p.BaseURL, "https://")) {
		return fmt.Errorf("base_url must start with http:// or https://: %q", p.BaseURL)
	}

	// DefaultHeaders may be nil; ranging over nil map is safe and should be valid.
	for k, v := range p.DefaultHeaders {
		if k == "" {
			return errors.New("default_headers key cannot be empty")
		}
		// Header keys must be lowercase and header-safe: [a-z0-9-]+
		for i := 0; i < len(k); i++ {
			b := k[i]
			isLower := (b >= 'a' && b <= 'z')
			isDigit := (b >= '0' && b <= '9')
			isDash := (b == '-')
			if !(isLower || isDigit || isDash) {
				return fmt.Errorf("default_headers key must match [a-z0-9-]+: %q", k)
			}
		}
		// Header values must be ASCII printable bytes only (0x20-0x7E).
		for i := 0; i < len(v); i++ {
			b := v[i]
			if b < 0x20 || b > 0x7E {
				return fmt.Errorf("default_headers value must be ASCII printable: %q", k)
			}
		}
	}
	r := p.Timeouts.RequestMS
	c := p.Timeouts.ConnectMS

	if r < 100 || r > 300000 {
		return fmt.Errorf("request_ms out of range: %d", r)
	}
	if c < 50 || c > r {
		return fmt.Errorf("connect_ms out of range: %d (request_ms=%d)", c, r)
	}
	return nil
}
func goodProfile() Profile {
	var p Profile
	p.Name = "default"
	p.TenantID = "tenant_1"
	p.BaseURL = "http://localhost:8080"
	p.DefaultHeaders = map[string]string{
		"x-request-id": "test-123",
	}
	p.Timeouts.RequestMS = 15000
	p.Timeouts.ConnectMS = 1000
	return p
}
func TestProfileValidate_GoodProfilePasses(t *testing.T) {
	p := goodProfile()
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid profile, got error: %v", err)
	}
}
func TestProfileValidate_NilHeadersIsValid(t *testing.T) {
	p := goodProfile()
	p.DefaultHeaders = nil
	if err := p.Validate(); err != nil {
		t.Fatalf("expected nil DefaultHeaders to be valid, got error: %v", err)
	}
}
func TestProfileValidate_Rules(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(p *Profile) // wantError string }{
		{
			name: "name required",
			mutate: func(p *Profile) {
				p.Name = "   "
			},
			wantError: "name is required",
		},
		{
			name: "tenant id invalid char",
			mutate: func(p *Profile) {
				p.TenantID = "bad tenant!"
			},
			wantError: "tenant_id invalid",
		},
		{
			name: "base url required",
			mutate: func(p *Profile) {
				p.BaseURL = ""
			},
			wantError: "base_url is required",
		},
		{
			name: "base url must start with http(s)",
			mutate: func(p *Profile) {
				p.BaseURL = "ftp://example.com"
			},
			wantError: "base_url must start",
		},
		{
			name: "header key cannot be empty",
			mutate: func(p *Profile) {
				p.DefaultHeaders = map[string]string{"": "ok"}
			},
			wantError: "key cannot be empty",
		},
		{
			name: "header key must be lowercase token",
			mutate: func(p *Profile) {
				p.DefaultHeaders = map[string]string{"x_req": "ok"} // underscore not allowed
			},
			wantError: "key must match [a-z0-9-]+",
		},
		{
			name: "header key must be lowercase (uppercase fails)",
			mutate: func(p *Profile) {
				p.DefaultHeaders = map[string]string{"X-req": "ok"}
			},
			wantError: "key must match [a-z0-9-]+",
		},
		{
			name: "header key must not contain spaces",
			mutate: func(p *Profile) {
				p.DefaultHeaders = map[string]string{"x req": "ok"}
			},
			wantError: "key must match [a-z0-9-]+",
		},
		{
			name: "header value ascii only (emoji)",
			mutate: func(p *Profile) {
				p.DefaultHeaders = map[string]string{"x-test": "ok🎉"}
			},
			wantError: "value must be ASCII printable",
		},
		{
			name: "request_ms too small",
			mutate: func(p *Profile) {
				p.Timeouts.RequestMS = 10
			},
			wantError: "request_ms out of range",
		},
		{
			name: "request_ms too large",
			mutate: func(p *Profile) {
				p.Timeouts.RequestMS = 999999
			},
			wantError: "request_ms out of range",
		},
		{
			name: "connect_ms too small",
			mutate: func(p *Profile) {
				p.Timeouts.ConnectMS = 1
			},
			wantError: "connect_ms out of range",
		},
		{
			name: "connect_ms greater than request_ms",
			mutate: func(p *Profile) {
				p.Timeouts.RequestMS = 1000
				p.Timeouts.ConnectMS = 2000
			},
			wantError: "connect_ms out of range",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := goodProfile()
			tt.mutate(&p)
			err := p.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantError)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected error containing %q, got %q", tt.wantError, err.Error())
			}
		})
	}
}
