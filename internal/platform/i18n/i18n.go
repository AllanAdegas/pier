// Package i18n translates the messages Pier shows to people. Messages are
// written in Portuguese, the source language, and the Portuguese format
// string is the key of the English catalog. A message is translated where it
// is shown: an API answer in the caller's language, a deployment log or a
// notification in the language of the instance.
package i18n

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
)

// Language is a language Pier speaks, as a BCP 47 tag.
type Language string

const (
	Portuguese Language = "pt-BR"
	English    Language = "en"
)

// Languages lists the supported languages, the source language first.
var Languages = []Language{Portuguese, English}

// Parse recognizes a supported language by its tag, ignoring the region.
func Parse(tag string) (Language, bool) {
	base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
	switch base {
	case "pt":
		return Portuguese, true
	case "en":
		return English, true
	}
	return "", false
}

// FromAcceptLanguage picks the first supported language of an
// Accept-Language header, in the order the client listed them, or fallback.
func FromAcceptLanguage(header string, fallback Language) Language {
	for _, part := range strings.Split(header, ",") {
		tag, _, _ := strings.Cut(part, ";")
		if language, ok := Parse(tag); ok {
			return language
		}
	}
	return fallback
}

// Text is an argument that is itself a message, such as the name of a kind
// of resource, so it is translated along with the format it fills.
type Text string

// Localizer is implemented by arguments that know how to render themselves in
// a language, like an application error wrapped in another message.
type Localizer interface {
	Localize(language Language) string
}

// Sprintf translates the format and the translatable arguments, then formats.
func (l Language) Sprintf(format string, args ...any) string {
	return fmt.Sprintf(formatVerbs(l.Text(format)), l.args(args)...)
}

// Text translates a message without arguments.
func (l Language) Text(message string) string {
	if l != English {
		return message
	}
	if translated, ok := english[message]; ok {
		return translated
	}
	return message
}

func (l Language) args(args []any) []any {
	if len(args) == 0 {
		return args
	}
	localized := make([]any, len(args))
	for i, arg := range args {
		switch value := arg.(type) {
		case Text:
			localized[i] = l.Text(string(value))
		case Localizer:
			localized[i] = value.Localize(l)
		default:
			localized[i] = arg
		}
	}
	return localized
}

var instance atomic.Value

// Instance is the language of the instance: deployment logs, notifications
// and API answers to clients that send no Accept-Language use it.
func Instance() Language {
	if language, ok := instance.Load().(Language); ok {
		return language
	}
	return Portuguese
}

// SetInstance changes the language of the instance.
func SetInstance(language Language) { instance.Store(language) }

type contextKey struct{}

// WithLanguage stores the language of the person a request serves.
func WithLanguage(ctx context.Context, language Language) context.Context {
	return context.WithValue(ctx, contextKey{}, language)
}

// FromContext is the language of the request, or the instance's.
func FromContext(ctx context.Context) Language {
	if language, ok := ctx.Value(contextKey{}).(Language); ok {
		return language
	}
	return Instance()
}
