package blob

// S3-compatible object store client (stdlib only)
// with AWS SigV4 signing.
//
// This file implements a minimal, production-grade client for S3-compatible endpoints.
// No AWS SDK is used. Requests are signed using Signature Version 4.
//
// Supported operations:
//   - Put (PUT Object)
//   - Get (GET Object)
//   - Head (HEAD Object)
//   - Delete (DELETE Object)
//
// Determinism goals:
//   - Canonical headers and signed headers are built with sorted header names.
//   - Canonical request is stable for the same inputs.
//   - Metadata header names are normalized deterministically.
//
// Notes:
//   - This is library-only: no filesystem writes, no HTTP handlers.
//   - Networking is performed via net/http to the configured endpoint.
//   - Multi-tenant safety: object keys are scoped by tenantID and a configured prefix.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
	ErrBlob         = errors.New("blob failed")
	ErrBlobInvalid  = errors.New("blob invalid input")
	ErrBlobAuth     = errors.New("blob auth failed")
	ErrBlobNotFound = errors.New("blob not found")
	ErrBlobTooLarge = errors.New("blob too large")
	ErrBlobHTTP     = errors.New("blob http error")
)

type Options struct {
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	Prefix       string
	HTTPTimeout  time.Duration
	MaxBodyBytes int64
}
type S3Store struct {
	opts Options
	hc   *http.Client
	u    *url.URL
}

func NewS3Store(opts Options) (*S3Store, error) {
	o := normalizeOptions(opts)
	if o.Endpoint == "" || o.Bucket == "" || o.AccessKey == "" || o.SecretKey == "" {
		return nil, fmt.Errorf("%w: endpoint/bucket/access/secret required", ErrBlobInvalid)
	}
	uu, err := url.Parse(o.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: endpoint parse: %v", ErrBlobInvalid, err)
	}
	if uu.Scheme != "http" && uu.Scheme != "https" {
		return nil, fmt.Errorf("%w: endpoint scheme must be http/https", ErrBlobInvalid)
	}
	hc := &http.Client{Timeout: o.HTTPTimeout}
	return &S3Store{opts: o, hc: hc, u: uu}, nil
}
func (s *S3Store) Put(ctx context.Context, tenantID, objectKey, contentType string, data []byte, meta map[string]string) error {
	tenantID = norm(tenantID)
	objectKey = strings.TrimSpace(objectKey)
	contentType = strings.TrimSpace(contentType)
	if tenantID == "" || objectKey == "" || contentType == "" {
		return fmt.Errorf("%w: tenantID/objectKey/contentType required", ErrBlobInvalid)
	}
	if data == nil {
		data = []byte{}
	}
	if s.opts.MaxBodyBytes > 0 && int64(len(data)) > s.opts.MaxBodyBytes {
		return fmt.Errorf("%w: body exceeds max bytes", ErrBlobTooLarge)
	}
	path, err := s.objectPath(tenantID, objectKey)
	if err != nil {
		return err
	}
	reqURL := s.u.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL.String(), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: new request: %v", ErrBlobHTTP, err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Content-Length", strconv.Itoa(len(data)))
	metaHeaders := buildMetaHeaders(meta)
	for k, v := range metaHeaders {
		req.Header.Set(k, v)
	}
	payloadHash := sha256Hex(data)
	if err := s.sign(req, payloadHash); err != nil {
		return err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: do: %v", ErrBlobHTTP, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w", ErrBlobNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		return fmt.Errorf("%w: put status=%d body=%s", ErrBlobHTTP, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
func (s *S3Store) Get(ctx context.Context, tenantID, objectKey string) (string, []byte, map[string]string, error) {
	tenantID = norm(tenantID)
	objectKey = strings.TrimSpace(objectKey)
	if tenantID == "" || objectKey == "" {
		return "", nil, nil, fmt.Errorf("%w: tenantID/objectKey required", ErrBlobInvalid)
	}
	path, err := s.objectPath(tenantID, objectKey)
	if err != nil {
		return "", nil, nil, err
	}
	reqURL := s.u.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", nil, nil, fmt.Errorf("%w: new request: %v", ErrBlobHTTP, err)
	}
	payloadHash := sha256Hex(nil)
	if err := s.sign(req, payloadHash); err != nil {
		return "", nil, nil, err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", nil, nil, fmt.Errorf("%w: do: %v", ErrBlobHTTP, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil, nil, fmt.Errorf("%w", ErrBlobNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		return "", nil, nil, fmt.Errorf("%w: get status=%d body=%s", ErrBlobHTTP, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var r io.Reader = resp.Body
	if s.opts.MaxBodyBytes > 0 {
		r = io.LimitReader(resp.Body, s.opts.MaxBodyBytes+1)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", nil, nil, fmt.Errorf("%w: read: %v", ErrBlobHTTP, err)
	}
	if s.opts.MaxBodyBytes > 0 && int64(len(b)) > s.opts.MaxBodyBytes {
		return "", nil, nil, fmt.Errorf("%w: body exceeds max bytes", ErrBlobTooLarge)
	}
	ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
	meta := parseMetaHeaders(resp.Header)
	return ct, b, meta, nil
}
func (s *S3Store) Head(ctx context.Context, tenantID, objectKey string) (string, int64, map[string]string, error) {
	tenantID = norm(tenantID)
	objectKey = strings.TrimSpace(objectKey)
	if tenantID == "" || objectKey == "" {
		return "", 0, nil, fmt.Errorf("%w: tenantID/objectKey required", ErrBlobInvalid)
	}
	path, err := s.objectPath(tenantID, objectKey)
	if err != nil {
		return "", 0, nil, err
	}
	reqURL := s.u.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, reqURL.String(), nil)
	if err != nil {
		return "", 0, nil, fmt.Errorf("%w: new request: %v", ErrBlobHTTP, err)
	}
	payloadHash := sha256Hex(nil)
	if err := s.sign(req, payloadHash); err != nil {
		return "", 0, nil, err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", 0, nil, fmt.Errorf("%w: do: %v", ErrBlobHTTP, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", 0, nil, fmt.Errorf("%w", ErrBlobNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		return "", 0, nil, fmt.Errorf("%w: head status=%d body=%s", ErrBlobHTTP, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
	var n int64
	if cl := strings.TrimSpace(resp.Header.Get("Content-Length")); cl != "" {
		v, err := strconv.ParseInt(cl, 10, 64)
		if err == nil && v >= 0 {
			n = v
		}
	}
	meta := parseMetaHeaders(resp.Header)
	return ct, n, meta, nil
}
func (s *S3Store) Delete(ctx context.Context, tenantID, objectKey string) error {
	tenantID = norm(tenantID)
	objectKey = strings.TrimSpace(objectKey)
	if tenantID == "" || objectKey == "" {
		return fmt.Errorf("%w: tenantID/objectKey required", ErrBlobInvalid)
	}
	path, err := s.objectPath(tenantID, objectKey)
	if err != nil {
		return err
	}
	reqURL := s.u.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL.String(), nil)
	if err != nil {
		return fmt.Errorf("%w: new request: %v", ErrBlobHTTP, err)
	}
	payloadHash := sha256Hex(nil)
	if err := s.sign(req, payloadHash); err != nil {
		return err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: do: %v", ErrBlobHTTP, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w", ErrBlobNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		return fmt.Errorf("%w: delete status=%d body=%s", ErrBlobHTTP, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Keying + paths
////////////////////////////////////////////////////////////////////////////////

func (s *S3Store) objectPath(tenantID, objectKey string) (string, error) {
	prefix := strings.Trim(strings.TrimSpace(s.opts.Prefix), "/")
	if prefix == "" {
		prefix = "chartly"
	}
	tenantID = norm(tenantID)
	objectKey = strings.Trim(strings.TrimSpace(objectKey), "/")
	if tenantID == "" || objectKey == "" {
		return "", fmt.Errorf("%w: invalid key parts", ErrBlobInvalid)
	}

	// Path-style: /<bucket>/<prefix>/<tenant>/<objectKey>
	if strings.Contains(objectKey, "..") {
		return "", fmt.Errorf("%w: objectKey may not contain '..'", ErrBlobInvalid)
	}
	parts := []string{s.opts.Bucket, prefix, tenantID}
	parts = append(parts, strings.Split(objectKey, "/")...)
	for _, p := range parts {
		if p == "" {
			return "", fmt.Errorf("%w: empty path segment", ErrBlobInvalid)
		}
	}
	escaped := make([]string, 0, len(parts))
	for _, p := range parts {
		escaped = append(escaped, url.PathEscape(p))
	}
	return "/" + strings.Join(escaped, "/"), nil
}

////////////////////////////////////////////////////////////////////////////////
// Metadata headers
////////////////////////////////////////////////////////////////////////////////

func buildMetaHeaders(meta map[string]string) map[string]string {
	out := make(map[string]string)
	if meta == nil || len(meta) == 0 {
		return out
	}
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		nk := normHeaderKey(k)
		if nk == "" {
			continue
		}
		v := norm(meta[k])
		out["x-amz-meta-"+nk] = v
	}
	return out
}
func parseMetaHeaders(h http.Header) map[string]string {
	out := make(map[string]string)
	for k, vv := range h {
		kl := strings.ToLower(strings.TrimSpace(k))
		if !strings.HasPrefix(kl, "x-amz-meta-") {
			continue
		}
		key := strings.TrimPrefix(kl, "x-amz-meta-")
		key = normHeaderKey(key)
		if key == "" {
			continue
		}
		val := strings.Join(vv, ",")
		out[key] = norm(val)
	}
	return out
}
func normHeaderKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(s, "\x00", "")))
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), "_")
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return ""
	}
	return s
}

////////////////////////////////////////////////////////////////////////////////
// SigV4 signing (deterministic)
////////////////////////////////////////////////////////////////////////////////

func (s *S3Store) sign(req *http.Request, payloadHashHex string) error {
	if req == nil {
		return fmt.Errorf("%w: request nil", ErrBlobInvalid)
	}

	// Time is required for SigV4. Use current time (transport-level).
	t := time.Now().UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")
	region := s.opts.Region
	if region == "" {
		region = "us-east-1"
	}
	service := "s3"

	// Required headers
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHashHex)
	if strings.TrimSpace(s.opts.SessionToken) != "" {
		req.Header.Set("x-amz-security-token", strings.TrimSpace(s.opts.SessionToken))
	}
	canonicalHeaders, signedHeaders := canonicalHeaders(req.Header)
	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := req.URL.RawQuery

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHashHex,
	}, "\n")
	crHash := sha256Hex([]byte(canonicalRequest))
	scope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		crHash,
	}, "\n")
	signingKey := deriveSigningKey(s.opts.SecretKey, dateStamp, region, service)
	sig := hmacSHA256Hex(signingKey, []byte(stringToSign))
	authHeader := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.opts.AccessKey,
		scope,
		signedHeaders,
		sig,
	)
	req.Header.Set("Authorization", authHeader)
	return nil
}
func canonicalHeaders(h http.Header) (canonical string, signedHeaders string) {
	names := make([]string, 0, len(h))
	seen := make(map[string]struct{}, len(h))
	for k := range h {
		kl := strings.ToLower(strings.TrimSpace(k))
		if kl == "" {
			continue
		}
		if _, ok := seen[kl]; ok {
			continue
		}
		seen[kl] = struct{}{}
		names = append(names, kl)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		vv := h.Values(name)
		if len(vv) == 0 {
			vv = headerValuesCaseInsensitive(h, name)
		}
		val := strings.Join(vv, ",")
		val = strings.TrimSpace(val)
		val = strings.Join(strings.Fields(val), " ")
		b.WriteString(name)
		b.WriteString(":")
		b.WriteString(val)
		b.WriteString("\n")
	}
	signedHeaders = strings.Join(names, ";")
	return b.String(), signedHeaders
}
func headerValuesCaseInsensitive(h http.Header, lowerName string) []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.ToLower(k) == lowerName {
			vv := h[k]
			cp := make([]string, len(vv))
			copy(cp, vv)
			return cp
		}
	}
	return nil
}
func deriveSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}
func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(data)
	return m.Sum(nil)
}
func hmacSHA256Hex(key, data []byte) string {
	sum := hmacSHA256(key, data)
	return hex.EncodeToString(sum)
}
func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

////////////////////////////////////////////////////////////////////////////////
// Defaults + normalization
////////////////////////////////////////////////////////////////////////////////

func normalizeOptions(opts Options) Options {
	o := opts
	o.Endpoint = strings.TrimSpace(o.Endpoint)
	o.Bucket = strings.TrimSpace(o.Bucket)
	o.AccessKey = strings.TrimSpace(o.AccessKey)
	o.SecretKey = strings.TrimSpace(o.SecretKey)
	o.SessionToken = strings.TrimSpace(o.SessionToken)
	if strings.TrimSpace(o.Region) == "" {
		o.Region = "us-east-1"
	}
	if strings.TrimSpace(o.Prefix) == "" {
		o.Prefix = "chartly"
	} else {
		o.Prefix = strings.Trim(strings.TrimSpace(o.Prefix), "/")
	}
	if o.HTTPTimeout <= 0 {
		o.HTTPTimeout = 20 * time.Second
	}
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = 64 * 1024 * 1024 // 64MiB
	}
	return o
}
func norm(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}
