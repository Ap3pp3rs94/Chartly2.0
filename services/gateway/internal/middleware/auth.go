package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type errResp struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	var e errResp
	e.Error.Code = code
	e.Error.Message = msg
	_ = json.NewEncoder(w).Encode(e)
}

func authEnabled() bool {
	v := strings.TrimSpace(os.Getenv("AUTH_ENABLED"))
	return strings.EqualFold(v, "true")
}

func b64urlDecode(s string) ([]byte, error) {
	// Add padding if missing
	if s == "" {
		return nil, base64.CorruptInputError(0)
	}
	pad := len(s) % 4
	if pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return base64.URLEncoding.DecodeString(s)
}

func hmacSHA256(key []byte, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(data)
	return m.Sum(nil)
}

func claimString(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
}

func claimNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}

func audMatches(aud any, expected string) bool {
	if expected == "" {
		return true
	}
	// aud can be string or array
	if s, ok := aud.(string); ok {
		return s == expected
	}
	if arr, ok := aud.([]any); ok {
		for _, it := range arr {
			if s, ok := it.(string); ok && s == expected {
				return true
			}
		}
	}
	return false
}

func issMatches(iss any, expected string) bool {
	if expected == "" {
		return true
	}
	if s, ok := iss.(string); ok {
		return s == expected
	}
	return false
}

func verifyJWT(token string) (tenantID string, ok bool, msg string) {
	keyStr := os.Getenv("AUTH_JWT_SIGNING_KEY")
	if strings.TrimSpace(keyStr) == "" {
		return "", false, "auth signing key not configured"
	}
	key := []byte(keyStr)

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false, "invalid token"
	}

	_, err := b64urlDecode(parts[0])
	if err != nil {
		return "", false, "invalid token"
	}

	payloadB, err := b64urlDecode(parts[1])
	if err != nil {
		return "", false, "invalid token"
	}

	sigB, err := b64urlDecode(parts[2])
	if err != nil {
		return "", false, "invalid token"
	}

	// Verify signature
	signingInput := []byte(parts[0] + "." + parts[1])
	expectedSig := hmacSHA256(key, signingInput)
	if !hmac.Equal(sigB, expectedSig) {
		return "", false, "invalid token signature"
	}

	// Parse claims
	var claims map[string]any
	if err := json.Unmarshal(payloadB, &claims); err != nil {
		return "", false, "invalid token claims"
	}

	// Validate iss/aud/exp
	expRaw, _ := claims["exp"]
	expNum, okNum := claimNumber(expRaw)
	if !okNum {
		return "", false, "missing exp"
	}

	// exp is seconds since epoch
	exp := time.Unix(int64(expNum), 0)
	if time.Now().UTC().After(exp.Add(30 * time.Second)) {
		return "", false, "token expired"
	}

	issExpected := strings.TrimSpace(os.Getenv("AUTH_JWT_ISSUER"))
	audExpected := strings.TrimSpace(os.Getenv("AUTH_JWT_AUDIENCE"))

	if !issMatches(claims["iss"], issExpected) {
		return "", false, "invalid issuer"
	}
	if !audMatches(claims["aud"], audExpected) {
		return "", false, "invalid audience"
	}

	tenantRaw, _ := claims["tenant_id"]
	tid, okT := claimString(tenantRaw)
	if !okT {
		return "", false, "missing tenant_id"
	}

	return tid, true, ""
}

func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authEnabled() {
			next.ServeHTTP(w, r)
			return
		}

		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		if authz == "" || !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}

		token := strings.TrimSpace(authz[len("bearer "):])
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}

		tenantID, ok, msg := verifyJWT(token)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "unauthorized", msg)
			return
		}

		// Enforce tenant header consistency
		existing := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
		if existing == "" {
			r.Header.Set("X-Tenant-Id", tenantID)
		} else if existing != tenantID {
			writeErr(w, http.StatusForbidden, "unauthorized", "tenant mismatch")
			return
		}

		next.ServeHTTP(w, r)
	})
}
