package stack

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The catalog quality bar: every template pins its images, carries no
// literal credential and describes itself in both languages. The checks read
// the embedded templates only; nothing touches the network or docker.

var composeImageLine = regexp.MustCompile(`(?m)^\s+image:\s*["']?([^"'\s#]+)["']?\s*(?:#.*)?$`)

// imageDefault captures the default of a tag taken from a variable, as in
// image:${VERSION:-latest}.
var imageDefault = regexp.MustCompile(`^(.+?):\$\{[A-Za-z0-9_]+:?-([^}]*)\}$`)

var floatingTags = map[string]bool{
	"latest": true, "main": true, "master": true, "edge": true, "nightly": true,
	"stable": true, "release": true, "dev": true, "develop": true, "canary": true,
}

func isFloatingTag(tag string) bool {
	tag = strings.ToLower(tag)
	return floatingTags[tag] || strings.HasPrefix(tag, "latest") || strings.HasSuffix(tag, "-latest")
}

// imageProblem explains why an image reference does not pin a version, or
// returns "" when it does.
func imageProblem(reference string) string {
	if match := imageDefault.FindStringSubmatch(reference); match != nil {
		if match[2] == "" || isFloatingTag(match[2]) {
			return reference + " defaults to a floating tag"
		}
		return ""
	}
	if strings.Contains(reference, "@sha256:") {
		return ""
	}
	last := reference[strings.LastIndex(reference, "/")+1:]
	tag, hasTag := last[strings.LastIndex(last, ":")+1:], strings.Contains(last, ":")
	if !hasTag {
		return reference + " has no tag"
	}
	if strings.Contains(tag, "$") {
		return ""
	}
	if isFloatingTag(tag) {
		return reference + " uses a floating tag"
	}
	return ""
}

func reportOffenders(t *testing.T, rule string, offenders map[string][]string) {
	t.Helper()
	if len(offenders) == 0 {
		return
	}
	slugs := make([]string, 0, len(offenders))
	for slug := range offenders {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	var report strings.Builder
	for _, slug := range slugs {
		report.WriteString("\n  " + slug + ": " + strings.Join(offenders[slug], "; "))
	}
	t.Errorf("%d templates break the rule %q:%s", len(offenders), rule, report.String())
}

func TestEveryTemplateImageIsPinnedToAVersion(t *testing.T) {
	templates, _ := Templates()
	offenders := map[string][]string{}
	for _, template := range templates {
		for _, match := range composeImageLine.FindAllStringSubmatch(template.Compose, -1) {
			if problem := imageProblem(match[1]); problem != "" {
				offenders[template.Slug] = append(offenders[template.Slug], problem)
			}
		}
	}
	reportOffenders(t, "pin every image to a version tag", offenders)
}

func TestImageProblemRecognisesFloatingReferences(t *testing.T) {
	floating := []string{
		"nginx", "nginx:latest", "ghcr.io/a/b:main", "a/b:edge", "a/b:nightly", "a/b:master",
		"a/b:${TAG:-latest}", "a/b:${TAG-main}", "a/b:${TAG:-}", "a/b:latest-ce", "a/b:core-latest",
		"localhost:5000/app",
	}
	for _, reference := range floating {
		if imageProblem(reference) == "" {
			t.Errorf("%s must be rejected", reference)
		}
	}
	pinned := []string{
		"nginx:1.27", "ghcr.io/a/b:v1.2.3", "a/b:${TAG:-1.2.3}", "a/b:${TAG}", "localhost:5000/app:2.0",
		"a/b:1.0@sha256:abc",
	}
	for _, reference := range pinned {
		if problem := imageProblem(reference); problem != "" {
			t.Errorf("%s must be accepted: %s", reference, problem)
		}
	}
}

// literalSecrets are values no installation may share: published defaults
// and hashes that would turn into the same password everywhere.
var literalSecrets = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"bcrypt hash", regexp.MustCompile(`\$2[aby]\$\d{2}\$[./A-Za-z0-9]{20,}`)},
	{"admin123", regexp.MustCompile(`(?i)admin123`)},
	{"changeme", regexp.MustCompile(`(?i)change[-_]?me`)},
	{"admin:admin credentials", regexp.MustCompile(`(?i)\badmin/admin\b|://admin:admin@`)},
	{"insecure token", regexp.MustCompile(`(?i)unleash-insecure|insecure-(?:frontend-)?api-token`)},
	{"literal default password", regexp.MustCompile(
		`(?i)(?:PASS|PASSWORD|PASSWD|PWD)[A-Z_]*\s*[=:]\s*["']?(?:admin|password|passw0rd|root|test|secret|123456|12345678|postgres|adminPassword)["']?\s*$`)},
	{"literal default password in a variable", regexp.MustCompile(
		`(?i)\$\{[A-Z0-9_]*(?:PASS|PASSWORD|PWD)[A-Z0-9_]*:?-(?:admin|password|passw0rd|root|test|secret|123456|postgres|adminPassword|plane|penpot|mypassword)\}`)},
}

func TestNoTemplateShipsALiteralCredential(t *testing.T) {
	templates, _ := Templates()
	for _, rule := range literalSecrets {
		offenders := map[string][]string{}
		for _, template := range templates {
			for _, line := range strings.Split(template.Compose, "\n") {
				if rule.pattern.MatchString(line) && !isSMTPPlaceholder(line) {
					offenders[template.Slug] = append(offenders[template.Slug], strings.TrimSpace(line))
				}
			}
		}
		reportOffenders(t, "no "+rule.name, offenders)
	}
}

// isSMTPPlaceholder tells outgoing-mail settings, which are placeholders for
// the person's own provider and unused until they configure one.
func isSMTPPlaceholder(line string) bool {
	upper := strings.ToUpper(line)
	return strings.Contains(upper, "SMTP") || strings.Contains(upper, "EMAIL_HOST_PASSWORD") ||
		strings.Contains(upper, "MAIL_PASSWORD")
}

func TestEveryTemplateIsDescribed(t *testing.T) {
	templates, _ := Templates()
	missing := map[string][]string{}
	for _, template := range templates {
		fields := map[string]string{
			"description":    template.Description,
			"description_en": template.DescriptionEN,
			"category":       template.Category,
			"website":        template.Website,
		}
		for field, value := range fields {
			if strings.TrimSpace(value) == "" {
				missing[template.Slug] = append(missing[template.Slug], field)
			}
		}
		sort.Strings(missing[template.Slug])
	}
	reportOffenders(t, "describe the template: description, description_en, category and website", missing)
}

// Embedded files must never sit below a workload-writable directory, even
// when a second read-only file mount happens to mask the entry today.
func TestCatalogContentFilesStayOutsideWritableBinds(t *testing.T) {
	templates, err := Templates()
	if err != nil {
		t.Fatal(err)
	}
	for _, template := range templates {
		doc, _ := parseCompose(template.Compose)
		services := doc["services"].(map[string]any)
		type mount struct {
			source      string
			ro, content bool
		}
		mounts := []mount{}
		for _, definition := range services {
			service := definition.(map[string]any)
			volumes, _ := service["volumes"].([]any)
			for _, v := range volumes {
				source, _, ro := bindParts(v)
				m, _ := v.(map[string]any)
				_, content := m["content"]
				if strings.HasPrefix(source, "./") {
					mounts = append(mounts, mount{path.Clean(source), ro, content})
				}
			}
		}
		for _, file := range mounts {
			if !file.content {
				continue
			}
			for _, dir := range mounts {
				if dir.content || dir.ro {
					continue
				}
				if file.source == dir.source || strings.HasPrefix(file.source, dir.source+"/") {
					t.Errorf("%s: content %s inside writable %s", template.Slug, file.source, dir.source)
				}
			}
		}
	}
}

func TestCatalogInitializationVolumesReachConsumers(t *testing.T) {
	templates, _ := Templates()
	for _, template := range templates {
		doc, _ := parseCompose(template.Compose)
		services := doc["services"].(map[string]any)
		for name, definition := range services {
			service := definition.(map[string]any)
			if !strings.Contains(name, "init") && name != "setup-config" {
				continue
			}
			if fmt.Sprint(service["restart"]) != "no" {
				continue
			}
			volumes, _ := service["volumes"].([]any)
			for _, v := range volumes {
				source, _, _ := bindParts(v)
				if namedVolume(source+":/data") == "" {
					continue
				}
				shared := false
				for other, def := range services {
					if other == name {
						continue
					}
					mounts, _ := def.(map[string]any)["volumes"].([]any)
					for _, m := range mounts {
						s, _, _ := bindParts(m)
						if source == s {
							shared = true
						}
					}
				}
				if !shared {
					t.Errorf("%s: init volume %s has no consumer", template.Slug, source)
				}
			}
		}
	}
}

func TestCatalogAbsoluteBindsAreDisclosed(t *testing.T) {
	templates, _ := Templates()
	for _, template := range templates {
		doc, _ := parseCompose(template.Compose)
		services := doc["services"].(map[string]any)
		for _, def := range services {
			mounts, _ := def.(map[string]any)["volumes"].([]any)
			for _, v := range mounts {
				s, target, ro := bindParts(v)
				if strings.HasPrefix(s, "/") && !allowedHostBind(s, target, ro) && !slices.Contains(template.Risks, "host-path") {
					t.Errorf("%s: undisclosed host bind %s", template.Slug, s)
				}
			}
		}
	}
}

func TestCatalogGeneratedCredentialsAreWired(t *testing.T) {
	templates, _ := Templates()
	secretKey := regexp.MustCompile(`(?i)^(DB_ENC_KEY|SECRET_KEY_BASE|CREDS_KEY|CREDS_IV)$`)
	for _, template := range templates {
		doc, _ := parseCompose(template.Compose)
		services := doc["services"].(map[string]any)
		for _, def := range services {
			env := catalogEnvironment(def.(map[string]any)["environment"])
			for key, v := range env {
				value := fmt.Sprint(v)
				if secretKey.MatchString(key) && value != "" && !strings.Contains(value, "${PIER_") && !strings.Contains(value, "$PIER_") {
					t.Errorf("%s: %s must receive a generated credential", template.Slug, key)
				}
				if strings.Contains(value, "${SECRET_PASSWORD_") {
					t.Errorf("%s: non-Pier generated secret %s", template.Slug, key)
				}
			}
		}
	}
}

func catalogEnvironment(value any) map[string]any {
	if mapping, ok := value.(map[string]any); ok {
		return mapping
	}
	result := map[string]any{}
	for _, entry := range stringList(value) {
		key, v, _ := strings.Cut(entry, "=")
		result[key] = v
	}
	return result
}
