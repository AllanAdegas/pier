// Package logdrain forwards the container logs of applications and stacks
// to external sinks (a generic HTTP JSON endpoint or a Loki push API). It is
// isolated from deployments and the panel: collection is one short command
// at a time per server, every sink has a bounded buffer that drops the
// oldest records when full, and every failure stays inside the drain.
package logdrain

import (
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"pier/internal/platform/apperr"
)

// Sink kinds.
const (
	KindHTTP = "http"
	KindLoki = "loki"
)

// Scope types: everything of a project, or one resource.
const (
	ScopeProject     = "project"
	ScopeApplication = "application"
	ScopeStack       = "stack"
)

const (
	maxLabels     = 10
	maxLabelValue = 128
	maxURL        = 2048
	maxToken      = 4096
)

var labelName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,63}$`)

// Drain is a configured log forwarding. The token is sealed in the database
// and never returned; HasToken tells whether one is set.
type Drain struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	URL       string            `json:"url"`
	HasToken  bool              `json:"has_token"`
	Labels    map[string]string `json:"labels"`
	ScopeType string            `json:"scope_type"`
	ScopeID   string            `json:"scope_id"`
	Enabled   bool              `json:"enabled"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Input creates or changes a drain. A nil Token keeps the stored one; an
// empty string removes it.
type Input struct {
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	URL       string            `json:"url"`
	Token     *string           `json:"token"`
	Labels    map[string]string `json:"labels"`
	ScopeType string            `json:"scope_type"`
	ScopeID   string            `json:"scope_id"`
	Enabled   bool              `json:"enabled"`
}

// normalize validates the input; hasToken is whether a token will be set.
func (input Input) normalize(hasToken bool) (Input, error) {
	input.Name, input.URL = strings.TrimSpace(input.Name), strings.TrimSpace(input.URL)
	fields := apperr.Fields{}
	if input.Name == "" || len(input.Name) > 64 {
		fields.Add("name", "Informe um nome de até 64 caracteres.")
	}
	if input.Kind != KindHTTP && input.Kind != KindLoki {
		fields.Add("kind", "Escolha HTTP JSON ou Loki.")
	}
	if input.ScopeType != ScopeProject && input.ScopeType != ScopeApplication && input.ScopeType != ScopeStack {
		fields.Add("scope_type", "Escolha um projeto, uma aplicação ou uma stack.")
	}
	if strings.TrimSpace(input.ScopeID) == "" {
		fields.Add("scope_id", "Escolha o que encaminhar.")
	}
	if input.Token != nil {
		token := strings.TrimSpace(*input.Token)
		input.Token = &token
		hasToken = token != ""
		if len(token) > maxToken || strings.IndexFunc(token, unicode.IsSpace) >= 0 {
			fields.Add("token", "O token não pode ter espaços nem passar de 4096 caracteres.")
		}
	}
	validateURL(input.URL, hasToken, fields)
	labels, problem := normalizeLabels(input.Labels)
	if problem != "" {
		fields.Add("labels", problem)
	}
	input.Labels = labels
	return input, fields.Err()
}

func validateURL(raw string, hasToken bool, fields apperr.Fields) {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > maxURL || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		fields.Add("url", "Informe uma URL http(s) completa.")
		return
	}
	if parsed.User != nil {
		fields.Add("url", "Não coloque credenciais na URL; use o campo de token.")
		return
	}
	if hasToken && parsed.Scheme != "https" {
		fields.Add("url", "Use HTTPS para enviar o token.")
	}
}

// normalizeLabels checks names and values; pier_* names are reserved for
// the labels Pier adds itself.
func normalizeLabels(labels map[string]string) (map[string]string, string) {
	clean := map[string]string{}
	if len(labels) > maxLabels {
		return clean, "Use no máximo 10 rótulos."
	}
	for name, value := range labels {
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !labelName.MatchString(name) || strings.HasPrefix(name, "pier_") {
			return clean, "Nomes de rótulo usam letras, números e _ e não podem começar com pier_."
		}
		if len(value) > maxLabelValue || strings.IndexFunc(value, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
			return clean, "Valores de rótulo têm até 128 caracteres visíveis."
		}
		clean[name] = value
	}
	return clean, ""
}
