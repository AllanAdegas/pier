// Package diagnose explains failed deployments and unreachable applications.
// It is pure: callers pass the text they already have (a build log, the
// /proc/net tables, a docker image inspection) and receive hints to show.
package diagnose

import "pier/internal/platform/i18n"

// Hint is one actionable explanation. Code is stable for clients; Title and
// Fix are written in Portuguese, the source language, and translated with
// Localize or, for listener hints, with the language the caller asks for.
type Hint struct {
	Code   string
	Title  string
	Fix    string
	DocURL string
}

// Localize returns the hint in language. Every Title and Fix is catalogued in
// the i18n engine area, so Portuguese stays as written.
func (h Hint) Localize(language i18n.Language) Hint {
	h.Title = language.Text(h.Title)
	h.Fix = language.Text(h.Fix)
	return h
}
