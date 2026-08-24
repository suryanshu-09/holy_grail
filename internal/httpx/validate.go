package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// ValidateQuery ensures every listed query parameter is present.
func ValidateQuery(r *http.Request, keys ...string) error {
	q := r.URL.Query()
	for _, key := range keys {
		if q.Get(key) == "" {
			return fmt.Errorf("missing required query parameter %q", key)
		}
	}
	return nil
}

// Pagination holds parsed limit/offset paging parameters.
type Pagination struct {
	Limit  int
	Offset int
}

// ParsePagination reads limit/offset from the query string, applying defaults
// and clamping limit to [1, MaxLimit].
func ParsePagination(r *http.Request) (Pagination, error) {
	p := Pagination{Limit: DefaultLimit}
	q := r.URL.Query()

	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return p, errors.New("limit must be a positive integer")
		}
		if n > MaxLimit {
			n = MaxLimit
		}
		p.Limit = n
	}

	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return p, errors.New("offset must be a non-negative integer")
		}
		p.Offset = n
	}

	return p, nil
}
