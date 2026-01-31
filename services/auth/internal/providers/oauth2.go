package providers

// OAuth2 client utilities (stdlib only, deterministic).
//
// This file provides a minimal, production-grade OAuth2 token client intended for
// service-to-service authentication (e.g., client credentials). It avoids external
// dependencies and keeps encoding/ordering deterministic.
//
// Determinism guarantees:
//   - No randomness.
//   - No time.Now usage (caller provides timestamps if needed).
//   - Stable ordering for scopes and extra parameters.
//   - Deterministic request encoding.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrOAuth2        = errors.New("oauth2 failed")
	ErrOAuth2Invalid = errors.New("oauth2 invalid")
	ErrOAuth2HTTP    = errors.New("oauth2 http error")
	ErrOAuth2Token   = errors.New("oauth2 token error")
)

type Client struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
	Audience     string
	ExtraParams  map[string]string
	HTTPTimeout  time.Duration
	MaxBodyBytes int64
}

type Token struct {
	AccessToken  string
	TokenType    string
	ExpiresIn    int64
	Scope        string
	RefreshToken string
	IDToken      string
	ExpiresAt    string // optional RFC3339/RFC3339Nano if provided by server
	Raw          map[string]any
}

func (c Client) FetchToken(ctx context.Context) (Token, error) {
	cl, err := normalizeClient(c)
	if err != nil {
		return Token{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	form := make(map[string]string)
	form["grant_type"] = "client_credentials"
	if cl.Audience != "" {
		form["audience"] = cl.Audience
	}
	if len(cl.Scopes) > 0 {
		form["scope"] = strings.Join(normalizeScopes(cl.Scopes), " ")
	}
	for k, v := range normalizeStringMap(cl.ExtraParams) {
		form[k] = v
	}

	body := encodeForm(form)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cl.TokenURL, bytes.NewReader([]byte(body)))
	if err != nil {
		return Token{}, fmt.Errorf("%w: %w: new request", ErrOAuth2, ErrOAuth2Invalid)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if cl.ClientID != "" {
		req.Header.Set("Authorization", basicAuth(cl.ClientID, cl.ClientSecret))
	}

	hc := &http.Client{Timeout: cl.HTTPTimeout}
	resp, err := hc.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("%w: %w: %v", ErrOAuth2, ErrOAuth2HTTP, err)
	}
	defer resp.Body.Close()

	var r io.Reader = resp.Body
	if cl.MaxBodyBytes > 0 {
		r = io.LimitReader(resp.Body, cl.MaxBodyBytes+1)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return Token{}, fmt.Errorf("%w: %w: read body", ErrOAuth2, ErrOAuth2HTTP)
	}
	if cl.MaxBodyBytes > 0 && int64(len(b)) > cl.MaxBodyBytes {
		return Token{}, fmt.Errorf("%w: %w: response too large", ErrOAuth2, ErrOAuth2HTTP)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(b))
		return Token{}, fmt.Errorf("%w: %w: status=%d body=%s", ErrOAuth2, ErrOAuth2HTTP, resp.StatusCode, msg)
	}

	tok, err := parseTokenResponse(b)
	if err != nil {
		return Token{}, err
	}
	return tok, nil
}

////////////////////////////////////////////////////////////////////////////////
// Parsing
////////////////////////////////////////////////////////////////////////////////

func parseTokenResponse(b []byte) (Token, error) {
	if len(b) == 0 {
		return Token{}, fmt.Errorf("%w: %w: empty response", ErrOAuth2, ErrOAuth2Token)
	}

	// decode known fields
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return Token{}, fmt.Errorf("%w: %w: invalid json", ErrOAuth2, ErrOAuth2Token)
	}

	getS := func(k string) string {
		if v, ok := raw[k]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
		return ""
	}

	getI := func(k string) int64 {
		if v, ok := raw[k]; ok {
			switch t := v.(type) {
			case float64:
				return int64(t)
			case string:
				if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
					return n
				}
			}
		}
		return 0
	}

	access := getS("access_token")
	if access == "" {
		return Token{}, fmt.Errorf("%w: %w: access_token missing", ErrOAuth2, ErrOAuth2Token)
	}

	tok := Token{
		AccessToken:  access,
		TokenType:    strings.ToLower(getS("token_type")),
		ExpiresIn:    getI("expires_in"),
		Scope:        getS("scope"),
		RefreshToken: getS("refresh_token"),
		IDToken:      getS("id_token"),
		ExpiresAt:    getS("expires_at"),
		Raw:          raw,
	}

	if tok.TokenType == "" {
		tok.TokenType = "bearer"
	}

	return tok, nil
}

////////////////////////////////////////////////////////////////////////////////
// Helpers
////////////////////////////////////////////////////////////////////////////////

func normalizeClient(c Client) (Client, error) {
	cc := c
	cc.TokenURL = strings.TrimSpace(cc.TokenURL)
	cc.ClientID = strings.TrimSpace(cc.ClientID)
	cc.ClientSecret = strings.TrimSpace(cc.ClientSecret)
	cc.Audience = strings.TrimSpace(cc.Audience)

	if cc.TokenURL == "" {
		return Client{}, fmt.Errorf("%w: %w: token_url required", ErrOAuth2, ErrOAuth2Invalid)
	}
	if cc.HTTPTimeout <= 0 {
		cc.HTTPTimeout = 15 * time.Second
	}
	if cc.MaxBodyBytes <= 0 {
		cc.MaxBodyBytes = 1 * 1024 * 1024
	}

	// Validate URL
	if u, err := url.Parse(cc.TokenURL); err != nil || u.Scheme == "" || u.Host == "" {
		return Client{}, fmt.Errorf("%w: %w: invalid token_url", ErrOAuth2, ErrOAuth2Invalid)
	}

	return cc, nil
}

func normalizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	tmp := make([]string, 0, len(scopes))
	for _, s := range scopes {
		n := normCollapse(s)
		if n == "" {
			continue
		}
		tmp = append(tmp, n)
	}
	sort.Strings(tmp)
	out := make([]string, 0, len(tmp))
	var last string
	for _, s := range tmp {
		if s != last {
			out = append(out, s)
			last = s
		}
	}
	return out
}

func normalizeStringMap(m map[string]string) map[string]string {
	if m == nil || len(m) == 0 {
		return map[string]string{}
	}
	keys := make([]string, 0, len(m))
	tmp := make(map[string]string, len(m))
	for k, v := range m {
		kk := normCollapse(k)
		if kk == "" {
			continue
		}
		tmp[kk] = normCollapse(v)
		keys = append(keys, kk)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(tmp))
	for _, k := range keys {
		out[k] = tmp[k]
	}
	return out
}

func encodeForm(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	first := true
	for _, k := range keys {
		v := m[k]
		if !first {
			b.WriteByte('&')
		}
		first = false
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(v))
	}
	return b.String()
}

func basicAuth(user, pass string) string {
	cred := user + ":" + pass
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(cred))
}

func normCollapse(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
