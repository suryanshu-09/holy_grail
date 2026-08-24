package apperr

import "errors"

// ErrNotFound is returned by services when the requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ErrInvalidUpload is returned by services when an uploaded file fails validation.
var ErrInvalidUpload = errors.New("invalid upload")
