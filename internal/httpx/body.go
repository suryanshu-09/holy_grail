package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
)

// MaxJSONBodyBytes caps JSON request bodies (auth, quiz, preferences).
// Uploads use the larger streaming multipart limit instead.
const MaxJSONBodyBytes = 1 << 20 // 1 MiB

// DecodeJSONBody reads at most maxBytes of the request body into dst,
// rejecting unknown fields. It returns a client-facing error message and
// whether the failure was a body-too-large (caller maps to 413).
func DecodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}, maxBytes int64) (clientMsg string, tooLarge bool) {
	if maxBytes <= 0 {
		maxBytes = MaxJSONBodyBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return "request body too large", true
		}
		return "invalid request body", false
	}
	return "", false
}
