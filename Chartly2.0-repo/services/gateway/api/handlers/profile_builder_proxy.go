package handlers

import (
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const profileBuilderTimeout = 20 * time.Second

// ProfileBuilderGenerate proxies POST /api/profile-builder/generate to the profile-builder service.
func ProfileBuilderGenerate(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimRight(profileBuilderURL(), "/") + "/api/profile-builder/generate"
	proxyProfileBuilder(w, r, target)
}

func profileBuilderURL() string {
	v := strings.TrimSpace(os.Getenv("PROFILE_BUILDER_URL"))
	if v != "" {
		return v
	}
	return "http://profile-builder:8085"
}

func proxyProfileBuilder(w http.ResponseWriter, r *http.Request, target string) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "proxy_error", "failed to build request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: profileBuilderTimeout}
	resp, err := client.Do(req)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "profile_builder_unavailable", "profile-builder unavailable")
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
