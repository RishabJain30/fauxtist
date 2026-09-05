package server

import (
	"encoding/json"
	"net/http"
)

// apiError is the structured JSON body every API error response uses: a
// human-readable message plus a stable, machine-readable code a client
// can branch on without parsing text.
type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// writeJSONError writes a structured error response. Credential-bearing
// endpoints never cache an error either, so this always sets
// Cache-Control: no-store rather than making callers remember to.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiError{Error: message, Code: code})
}
