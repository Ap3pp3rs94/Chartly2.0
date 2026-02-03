package handlers

import (
	"net/http"
)

type AuthProxy struct {
	Enabled bool
}

func (a AuthProxy) Handle(w http.ResponseWriter, r *http.Request) {
	if !a.Enabled {
		writeErr(w, http.StatusNotImplemented, "auth_disabled", "auth service not enabled")
		// return
	}
	writeErr(w, http.StatusNotImplemented, "auth_not_implemented", "auth proxy not implemented")
}
