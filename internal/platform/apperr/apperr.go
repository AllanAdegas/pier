// Package apperr defines the errors that cross the domain/HTTP boundary.
// Messages are user-facing: written in Portuguese and translated by i18n.
package apperr

import (
	"errors"
	"sort"
	"strings"

	"pier/internal/platform/i18n"
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
// Message and Fields are the Portuguese text; Localize renders them in the
// language of whoever reads the answer.
type Error struct {
	Kind    Kind
	Message string
	Fields  map[string]string

	text   i18n.Message
	fields Fields
}

func newError(kind Kind, text i18n.Message) *Error {
	return &Error{Kind: kind, Message: text.String(), text: text}
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

// Text is the message, still to be translated.
func (e *Error) Text() i18n.Message { return e.text }

// Localize renders the message in a language; it lets an error be the
// argument of another message.
func (e *Error) Localize(language i18n.Language) string { return e.text.Localize(language) }

// LocalizedFields renders the message of each field in a language.
func (e *Error) LocalizedFields(language i18n.Language) map[string]string {
	if len(e.fields) == 0 {
		return nil
	}
	localized := make(map[string]string, len(e.fields))
	for field, message := range e.fields {
		localized[field] = message.Localize(language)
	}
	return localized
}

// Invalid reports a request the user must correct.
func Invalid(format string, args ...any) *Error {
	return newError(KindInvalid, i18n.M(format, args...))
}

// NotFound reports a missing resource, e.g. NotFound("Aplicação").
func NotFound(what string) *Error {
	return newError(KindNotFound, i18n.M("%s não encontrado(a).", i18n.Text(what)))
}

// Conflict reports a state that prevents the operation.
func Conflict(format string, args ...any) *Error {
	return newError(KindConflict, i18n.M(format, args...))
}

// Unauthorized reports missing or wrong credentials.
func Unauthorized(format string, args ...any) *Error {
	return newError(KindUnauthorized, i18n.M(format, args...))
}

// Forbidden reports an authenticated caller without permission.
func Forbidden(format string, args ...any) *Error {
	return newError(KindForbidden, i18n.M(format, args...))
}

// Unavailable reports an infrastructure dependency that could not be reached.
func Unavailable(format string, args ...any) *Error {
	return newError(KindUnavailable, i18n.M(format, args...))
}

// As extracts an *Error from an error chain.
func As(err error) (*Error, bool) {
	var target *Error
	ok := errors.As(err, &target)
	return target, ok
}

// Fields accumulates per-field validation problems.
type Fields map[string]i18n.Message

// Add records a problem for a field, keeping the first one reported.
func (f Fields) Add(field, format string, args ...any) {
	f.Set(field, i18n.M(format, args...))
}

// Set records a problem already written as a message.
func (f Fields) Set(field string, message i18n.Message) {
	if _, exists := f[field]; !exists {
		f[field] = message
	}
}

// Merge records the field problems of another validation error.
func (f Fields) Merge(err *Error) {
	for field, message := range err.fields {
		f.Set(field, message)
	}
}

// Err returns nil when no problem was recorded.
func (f Fields) Err() error {
	if len(f) == 0 {
		return nil
	}
	err := newError(KindInvalid, i18n.M("Revise os campos destacados."))
	err.fields = f
	err.Fields = make(map[string]string, len(f))
	for field, message := range f {
		err.Fields[field] = message.String()
	}
	return err
}
