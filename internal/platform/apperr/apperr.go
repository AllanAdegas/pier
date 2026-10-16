// Package apperr defines the errors that cross the domain/HTTP boundary.
// Messages are user-facing and therefore written in Portuguese.
package apperr

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Kind classifies an error so the transport layer can pick a status code.
type Kind string

const (
	KindInvalid      Kind = "validation_failed"
	KindNotFound     Kind = "not_found"
	KindConflict     Kind = "conflict"
	KindUnauthorized Kind = "unauthorized"
	KindForbidden    Kind = "forbidden"
	KindUnavailable  Kind = "unavailable"
)

// Error is an expected failure with a message safe to show to the user.
type Error struct {
	Kind    Kind
	Message string
	Fields  map[string]string
}

func (e *Error) Error() string {
	if len(e.Fields) == 0 {
		return e.Message
	}
	parts := make([]string, 0, len(e.Fields))
	for field, message := range e.Fields {
		parts = append(parts, field+": "+message)
	}
	sort.Strings(parts)
	return e.Message + " (" + strings.Join(parts, "; ") + ")"
}

// Invalid reports a request the user must correct.
func Invalid(format string, args ...any) *Error {
	return &Error{Kind: KindInvalid, Message: fmt.Sprintf(format, args...)}
}

// NotFound reports a missing resource, e.g. NotFound("Aplicação").
func NotFound(what string) *Error {
	return &Error{Kind: KindNotFound, Message: what + " não encontrado(a)."}
}

// Conflict reports a state that prevents the operation.
func Conflict(format string, args ...any) *Error {
	return &Error{Kind: KindConflict, Message: fmt.Sprintf(format, args...)}
}

// Unauthorized reports missing or wrong credentials.
func Unauthorized(message string) *Error {
	return &Error{Kind: KindUnauthorized, Message: message}
}

// Forbidden reports an authenticated caller without permission.
func Forbidden(message string) *Error {
	return &Error{Kind: KindForbidden, Message: message}
}

// Unavailable reports an infrastructure dependency that could not be reached.
func Unavailable(format string, args ...any) *Error {
	return &Error{Kind: KindUnavailable, Message: fmt.Sprintf(format, args...)}
}

// As extracts an *Error from an error chain.
func As(err error) (*Error, bool) {
	var target *Error
	ok := errors.As(err, &target)
	return target, ok
}

// Fields accumulates per-field validation problems.
type Fields map[string]string

// Add records a problem for a field, keeping the first one reported.
func (f Fields) Add(field, format string, args ...any) {
	if _, exists := f[field]; !exists {
		f[field] = fmt.Sprintf(format, args...)
	}
}

// Err returns nil when no problem was recorded.
func (f Fields) Err() error {
	if len(f) == 0 {
		return nil
	}
	return &Error{Kind: KindInvalid, Message: "Revise os campos destacados.", Fields: f}
}
