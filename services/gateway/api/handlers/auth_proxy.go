package handlers

import (
	"encoding/json"
	"net/http"
)

type AuthProxy struct {
	Enabled bool
}

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

func (a AuthProxy) Handle(w http.ResponseWriter, r *http.Request) {
	if !a.Enabled {
		writeErr(w, http.StatusNotImplemented, "auth_disabled", "auth service not enabled")
		return
	}

	writeErr(w, http.StatusNotImplemented, "auth_not_implemented", "auth proxy not implemented")
}
