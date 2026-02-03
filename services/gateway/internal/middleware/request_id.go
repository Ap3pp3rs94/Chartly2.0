package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"unicode"
)

const requestIDHeader = "X-Request-Id"

func validRequestID(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		// Restrict to visible ASCII-ish printables to keep logs and headers safe.
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// fallback: still deterministic-ish, but should never happen
		return "req_fallback"
	}
	return "req_" + hex.EncodeToString(b[:])
}
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}

		// Set on request for downstream handlers
		r.Header.Set(requestIDHeader, id)
		// Set on response for client correlation
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r)
	})
}
