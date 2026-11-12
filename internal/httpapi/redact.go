package httpapi

import (
	"net/http"

	"pier/internal/identity"
)

// seesSecrets reports whether the caller may receive secret material. It is
// the same rule requireSensitive enforces, for responses that mix ordinary
// data with secrets: there the secret part is dropped instead of refusing.
func seesSecrets(r *http.Request) bool {
	return principalFrom(r.Context()).Can(identity.AbilityWrite)
}

// secretsHider is a value that knows how it looks without its secrets.
type secretsHider[T any] interface {
	WithoutSecrets() T
}

// forCaller returns value as the caller may see it.
func forCaller[T secretsHider[T]](r *http.Request, value T) T {
	if seesSecrets(r) {
		return value
	}
	return value.WithoutSecrets()
}

// allForCaller returns values as the caller may see them.
func allForCaller[T secretsHider[T]](r *http.Request, values []T) []T {
	if seesSecrets(r) {
		return values
	}
	visible := make([]T, len(values))
	for index, value := range values {
		visible[index] = value.WithoutSecrets()
	}
	return visible
}
