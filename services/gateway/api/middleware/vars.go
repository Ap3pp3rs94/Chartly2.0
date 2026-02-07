package middleware

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey int

const varsKey ctxKey = 1

func VarsFromContext(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(varsKey).(map[string]string); ok {
		return v
	}
	return nil
}

func Vars(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		vars := map[string]string{}
		for k, vals := range r.Header {
			if !strings.HasPrefix(strings.ToLower(k), "x-chartly-var-") {
				continue
			}
			name := strings.TrimPrefix(k, "X-Chartly-Var-")
			name = normalizeVarName(name)
			if len(vals) > 0 {
				vars[name] = vals[0]
			}
		}
		ctx := context.WithValue(r.Context(), varsKey, vars)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func normalizeVarName(s string) string {
	up := strings.ToUpper(strings.TrimSpace(s))
	up = strings.ReplaceAll(up, "-", "_")
	// remove non-alnum underscore
	b := strings.Builder{}
	for _, r := range up {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
