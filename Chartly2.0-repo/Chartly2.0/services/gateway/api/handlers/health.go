package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

type healthResp struct {
	Status    string `json:"status"`
	Service   string `json:"service"`
	TS        string `json:"ts"`
	RequestID string `json:"request_id"`
}

func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	reqID := r.Header.Get("X-Request-Id")
	resp := healthResp{
		Status:    "ok",
		Service:   "gateway",
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		RequestID: reqID,
	}
	_ = json.NewEncoder(w).Encode(resp)
}
