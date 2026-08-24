package http

import (
	"net/http"
	"strings"

	httpx "github.com/suryanshu-09/holy_grail/internal/httpx"
)

// methodNotAllowed writes a 405 with an Allow header for the given methods.
func methodNotAllowed(w http.ResponseWriter, allow ...string) {
	w.Header().Set("Allow", strings.Join(allow, ", "))
	httpx.Error(w, http.StatusMethodNotAllowed, "method not allowed")
}
