package apperr

import "errors"

// ErrNotFound is returned by services when the requested resource does not exist.
var ErrNotFound = errors.New("not found")
