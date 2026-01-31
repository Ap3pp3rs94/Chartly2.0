package providers

// HS256 JWT-like provider (stdlib only, deterministic).
//
// This file provides a reusable HS256 token provider for the auth service.
// It intentionally avoids external JWT libraries to keep the dependency surface minimal.
//
// Token format:
//   base64url(header).base64url(payload).base64url(signature)
//
// header:
//   {"alg":"HS256","typ":"JWT"}
//
// signature:
//   HMAC-SHA256(secret, header.payload)
//
// Determinism guarantees:
//   - No randomness.
//   - TokenID derived deterministically from canonical claim content.
//   - Scopes are sorted and deduplicated deterministically.
//   - Meta is canonicalized (normalized strings, sorted keys) before signing.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrJWT          = errors.New("jwt failed")
	ErrJWTInvalid   = errors.New("jwt invalid")
	ErrJWTSignature = errors.New("jwt signature invalid")
)

type Claims struct {
	TenantID  string            `json:"tenant_id"`
	Subject   string            `json:"subject"`
	IssuedAt  string            `json:"issued_at"`  // RFC3339Nano
	ExpiresAt string            `json:"expires_at"` // RFC3339Nano
	Scopes    []string          `json:"scopes,omitempty"`
	TokenID   string            `json:"token_id"`
	Meta      map[string]string `json:"meta,omitempty"`
}

type Provider struct {
	secret []byte
}

type tokenHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

func New(secret []byte) (*Provider, error) {
	if len(secret) == 0 {
		return nil, fmt.Errorf("%w: %w: secret required", ErrJWT, ErrJWTInvalid)
	}
	return &Provider{secret: append([]byte{}, secret...)}, nil
}

func (p *Provider) Sign(c Claims) (string, Claims, error) {
	cc, err := normalizeClaims(c)
	if err != nil {
		return "", Claims{}, err
	}

	// Compute deterministic TokenID.
	cc.TokenID = deterministicTokenID(cc)

	h := tokenHeader{Alg: "HS256", Typ: "JWT"}
	hb, err := json.Marshal(h)
	if err != nil {
		return "", Claims{}, fmt.Errorf("%w: header json: %v", ErrJWT, err)
	}

	pb, err := json.Marshal(cc)
	if err != nil {
		return "", Claims{}, fmt.Errorf("%w: payload json: %v", ErrJWT, err)
	}

	h64 := b64url(hb)
	p64 := b64url(pb)
	unsigned := h64 + "." + p64

	sig := hmacSHA256(p.secret, []byte(unsigned))
	t64 := b64url(sig)

	return unsigned + "." + t64, cc, nil
}

func (p *Provider) Verify(tok string) (Claims, error) {
	tok = strings.TrimSpace(tok)
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("%w: %w: token must have 3 parts", ErrJWT, ErrJWTInvalid)
	}

	unsigned := parts[0] + "." + parts[1]
	want := hmacSHA256(p.secret, []byte(unsigned))

	got, err := b64urlDecode(parts[2])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w: bad signature encoding", ErrJWT, ErrJWTSignature)
	}
	if !hmac.Equal(want, got) {
		return Claims{}, fmt.Errorf("%w: signature mismatch", ErrJWTSignature)
	}

	pb, err := b64urlDecode(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w: bad payload encoding", ErrJWT, ErrJWTInvalid)
	}

	var c Claims
	dec := json.NewDecoder(strings.NewReader(string(pb)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Claims{}, fmt.Errorf("%w: %w: bad claims json", ErrJWT, ErrJWTInvalid)
	}

	cc, err := normalizeClaims(c)
	if err != nil {
		return Claims{}, err
	}

	// Ensure TokenID matches deterministic recompute.
	wantID := deterministicTokenID(cc)
	if cc.TokenID == "" {
		cc.TokenID = wantID
	} else if cc.TokenID != wantID {
		return Claims{}, fmt.Errorf("%w: %w: token_id mismatch", ErrJWT, ErrJWTInvalid)
	}

	return cc, nil
}

////////////////////////////////////////////////////////////////////////////////
// Canonicalization / determinism
////////////////////////////////////////////////////////////////////////////////

func normalizeClaims(c Claims) (Claims, error) {
	cc := c
	cc.TenantID = normCollapse(cc.TenantID)
	cc.Subject = normCollapse(cc.Subject)
	cc.IssuedAt = normCollapse(cc.IssuedAt)
	cc.ExpiresAt = normCollapse(cc.ExpiresAt)
	cc.Meta = normalizeStringMap(cc.Meta)
	cc.Scopes = normalizeScopes(cc.Scopes)

	if cc.TenantID == "" || cc.Subject == "" {
		return Claims{}, fmt.Errorf("%w: %w: tenant_id/subject required", ErrJWT, ErrJWTInvalid)
	}

	ti, err := parseRFC3339(cc.IssuedAt)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w: invalid issued_at", ErrJWT, ErrJWTInvalid)
	}
	te, err := parseRFC3339(cc.ExpiresAt)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w: invalid expires_at", ErrJWT, ErrJWTInvalid)
	}
	if te.Before(ti) {
		return Claims{}, fmt.Errorf("%w: %w: expires_at < issued_at", ErrJWT, ErrJWTInvalid)
	}

	cc.IssuedAt = ti.UTC().Format(time.RFC3339Nano)
	cc.ExpiresAt = te.UTC().Format(time.RFC3339Nano)

	// TokenID is set during Sign; Verify recomputes to confirm.
	cc.TokenID = normCollapse(cc.TokenID)

	return cc, nil
}

func deterministicTokenID(c Claims) string {
	// Canonical meta bytes via ordered kv slice.
	type kv struct {
		K string `json:"k"`
		V string `json:"v"`
	}

	keys := make([]string, 0, len(c.Meta))
	for k := range c.Meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	arr := make([]kv, 0, len(keys))
	for _, k := range keys {
		arr = append(arr, kv{K: k, V: c.Meta[k]})
	}
	metaB, _ := json.Marshal(arr)

	seedParts := []string{
		c.TenantID,
		c.Subject,
		c.IssuedAt,
		c.ExpiresAt,
		strings.Join(c.Scopes, ","),
		string(metaB),
	}

	sum := sha256.Sum256([]byte(strings.Join(seedParts, "|")))
	return hex.EncodeToString(sum[:16])
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

	tmp := make(map[string]string, len(m))
	for k, v := range m {
		kk := normCollapse(k)
		if kk == "" {
			continue
		}
		tmp[kk] = normCollapse(v)
	}

	keys := make([]string, 0, len(tmp))
	for k := range tmp {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = tmp[k]
	}

	return out
}

////////////////////////////////////////////////////////////////////////////////
// Encoding + crypto
////////////////////////////////////////////////////////////////////////////////

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func hmacSHA256(secret []byte, data []byte) []byte {
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write(data)
	return m.Sum(nil)
}

func parseRFC3339(s string) (time.Time, error) {
	s = normCollapse(s)
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func normCollapse(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
