package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON serializes v as JSON and writes it with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		LogError("failed to write JSON response", err)
	}
}

type errorBody struct {
	Error errorMessage `json:"error"`
}

type errorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error writes a consistent JSON error envelope with the given status code.
func Error(w http.ResponseWriter, status int, message string) {
	code := http.StatusText(status)
	WriteJSON(w, status, errorBody{
		Error: errorMessage{Code: code, Message: message},
	})
}
