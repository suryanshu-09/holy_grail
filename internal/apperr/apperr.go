package apperr

import "errors"

// ErrNotFound is returned by services when the requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ErrInvalidUpload is returned by services when an uploaded file fails validation.
var ErrInvalidUpload = errors.New("invalid upload")

// ErrDuplicate is returned when a resource already exists (e.g. re-registration).
var ErrDuplicate = errors.New("duplicate")

// ErrUnauthorized is returned when credentials are missing or invalid.
var ErrUnauthorized = errors.New("unauthorized")

// ErrForbidden is returned when the caller may not access the resource.
var ErrForbidden = errors.New("forbidden")
