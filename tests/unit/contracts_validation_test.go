package unit

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestContracts_ProfileValidate_ErrorTokensStable(t *testing.T) {
	allowed := []string{
		"name is required",
		"tenant_id invalid",
		"base_url is required",
		"base_url must start",
		"default_headers key",
		"default_headers value",
		"request_ms out of range",
		"connect_ms out of range",
	}

	cases := []struct {
		name   string
		mutate func(p *Profile)
	}{
		{
			name: "name required",
			mutate: func(p *Profile) {
				p.Name = " "
			},
		},
		{
			name: "tenant invalid",
			mutate: func(p *Profile) {
				p.TenantID = "bad!"
			},
		},
		{
			name: "base url required",
			mutate: func(p *Profile) {
				p.BaseURL = ""
			},
		},
		{
			name: "base url must start",
			mutate: func(p *Profile) {
				p.BaseURL = "ftp://example.com"
			},
		},
		{
			name: "default_headers key invalid",
			mutate: func(p *Profile) {
				p.DefaultHeaders = map[string]string{"X": "ok"}
			},
		},
		{
			name: "default_headers value invalid",
			mutate: func(p *Profile) {
				p.DefaultHeaders = map[string]string{"x-test": "ok🎉"}
			},
		},
		{
			name: "request_ms out of range",
			mutate: func(p *Profile) {
				p.Timeouts.RequestMS = 10
			},
		},
		{
			name: "connect_ms out of range",
			mutate: func(p *Profile) {
				p.Timeouts.RequestMS = 100
				p.Timeouts.ConnectMS = 200
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := goodProfile()
			tc.mutate(&p)
			err := p.Validate()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			msg := err.Error()

			ok := false
			for _, tok := range allowed {
				if strings.Contains(msg, tok) {
					ok = true
					break
				}
			}
			if !ok {
				t.Fatalf("error token not stable: %q", msg)
			}
		})
	}
}

func TestContracts_Normalizer_UnsupportedTypePrefix(t *testing.T) {
	_, err := normalize(func() {})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if !strings.HasPrefix(err.Error(), "unsupported type:") {
		t.Fatalf("expected prefix 'unsupported type:', got %q", err.Error())
	}
}

func TestContracts_Connector_HealthClosedIsErrNotOpen(t *testing.T) {
	c := newFakeConnector("fake")
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected error when closed")
	}
	if !errors.Is(err, errNotOpen) {
		t.Fatalf("expected errNotOpen, got: %v", err)
	}
}
