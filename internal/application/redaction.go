package application

import (
	"context"
	"net/url"
	"sort"
	"strings"

	"pier/internal/envvar"
)

// OutputRedactor applies the same secret policy to deployment and task output.
// Configure it before streaming output. Afterwards Mask is safe for concurrent readers.
type OutputRedactor struct{ secrets []string }

func (r *OutputRedactor) Redact(values ...string) {
	for _, value := range values {
		r.register(value)
		r.register(strings.TrimSpace(value))
		for _, line := range strings.FieldsFunc(value, func(c rune) bool { return c == '\r' || c == '\n' }) {
			r.register(strings.TrimSpace(line))
		}
	}
	sort.SliceStable(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
}
func (r *OutputRedactor) register(value string) {
	if len(value) >= minSecretLength {
		r.secrets = append(r.secrets, value)
	}
}
func (r *OutputRedactor) Mask(text string) string {
	text = ansiPattern.ReplaceAllString(text, "")
	for _, value := range r.secrets {
		text = strings.ReplaceAll(text, value, "••••••")
	}
	return text
}
func (r *OutputRedactor) repository(repository string) {
	parsed, err := url.Parse(repository)
	if err != nil || parsed.User == nil {
		start := strings.Index(repository, "://")
		if start >= 0 {
			authority := strings.SplitN(repository[start+3:], "/", 2)[0]
			if at := strings.LastIndex(authority, "@"); at >= 0 {
				userinfo := authority[:at]
				r.secrets = append(r.secrets, userinfo)
				if _, password, ok := strings.Cut(userinfo, ":"); ok && password != "" {
					r.secrets = append(r.secrets, password)
				}
			}
		}
		return
	}
	values := []string{parsed.User.String(), parsed.User.Username()}
	if password, ok := parsed.User.Password(); ok {
		values = append(values, password)
	}
	for _, value := range values {
		if value != "" {
			r.secrets = append(r.secrets, value)
		}
	}
	sort.SliceStable(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
}

// OutputSecrets resolves current values for task output and legacy history masking.
func (s *Service) OutputSecrets(ctx context.Context, id string) ([]string, error) {
	app, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	variables, err := s.env.ListForDeploy(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: id}, app.EnvironmentID, app.ServerID)
	if err != nil {
		return nil, err
	}
	values := make([]string, 0, len(variables))
	for _, variable := range variables {
		values = append(values, variable.Value)
	}
	return values, nil
}
