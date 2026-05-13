package server

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string, detail any) {
	writeJSON(w, status, map[string]any{
		"ok":     false,
		"code":   code,
		"error":  message,
		"detail": detail,
	})
}
