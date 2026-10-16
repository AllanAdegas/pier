package stack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pier/internal/compose"
	"pier/internal/platform/idgen"
)

// Magic variables are names a compose file references to ask Pier for a
// value: a generated secret, the public address of a service, or the folder
// of the stack on the server.
//
// Generated once per stack and never regenerated (changing the password of an
// initialized database would lock the stack out of it):
//
//	PIER_PASSWORD[_32|_64|_128]_<NAME>  letters and digits, 32 by default
//	PIER_USER_<NAME>                    a lowercase user name
//	PIER_SECRET_<NAME>                  64 hexadecimal characters
//	PIER_HEX[_32|_64|_128]_<NAME>       hexadecimal, 32 characters by default
//	PIER_BASE64[_32|_64|_128]_<NAME>    base64 of that many random bytes, 32 by default
//	PIER_SUPABASEANON_<NAME>            Supabase JWTs (roles anon and service_role),
//	PIER_SUPABASESERVICE_<NAME>         signed with PIER_PASSWORD_JWT
//
// From the routes of the stack: PIER_URL_<SERVICE> (https://host/path) and
// PIER_HOST_<SERVICE> (host), where <SERVICE> is the service name in upper
// case with anything but letters and digits turned into "_".
//
// PIER_STACK_DIR is the folder of the stack on the server.

// magicName matches any identifier that may be a magic variable.
var magicName = regexp.MustCompile(`(?:PIER|CAIS)_[A-Za-z0-9_]+`)

// legacyPrefix is the prefix of Pier's own variables before the project was
// renamed from Cais. Compose files written then still use it, so it stays an
// alias: the same values are generated and published under both names.
const legacyPrefix = "CAIS_"

// directoryVariable is the folder of the stack on the server, for files that
// keep data next to the compose file.
const directoryVariable = "PIER_STACK_DIR"

// signingKeyVariable is the secret the Supabase tokens are signed with.
const signingKeyVariable = "PIER_PASSWORD_JWT"

// generatedKinds lists the kinds, longest first so that a kind is never
// mistaken for a shorter one it starts with.
var generatedKinds = []string{"SUPABASESERVICE", "SUPABASEANON", "PASSWORD", "SECRET", "BASE64", "USER", "HEX"}

// sizedKinds accept a length between the kind and the name.
var sizedKinds = map[string]bool{"PASSWORD": true, "BASE64": true, "HEX": true}

// generated describes one generated variable: what to create and how long.
type generated struct {
	kind   string
	length int
}

// classify tells whether a name asks for a generated value.
func classify(name string) (generated, bool) {
	if rest, isLegacy := strings.CutPrefix(name, legacyPrefix); isLegacy {
		name = "PIER_" + rest
	}
	rest, isPier := strings.CutPrefix(name, "PIER_")
	if !isPier {
		return generated{}, false
	}
	for _, kind := range generatedKinds {
		tail, matches := strings.CutPrefix(rest, kind+"_")
		if !matches || tail == "" {
			continue
		}
		length := 0
		if sizedKinds[kind] {
			length = declaredLength(tail)
		}
		return generated{kind: kind, length: length}, true
	}
	return generated{}, false
}

// declaredLength reads the optional size that precedes the name, as in
// PIER_PASSWORD_64_APIKEY. Only 32, 64 and 128 are sizes, so a name that
// merely starts with digits is not mistaken for one.
func declaredLength(tail string) int {
	first, rest, hasName := strings.Cut(tail, "_")
	if !hasName || rest == "" {
		return 0
	}
	switch first {
	case "32", "64", "128":
		length, _ := strconv.Atoi(first)
		return length
	}
	return 0
}

func (g generated) value(signingKey string, now time.Time) string {
	length := g.length
	if length == 0 {
		length = 32
	}
	switch g.kind {
	case "PASSWORD":
		return idgen.Password(length)
	case "USER":
		return "u" + strings.ToLower(idgen.Password(11))
	case "SECRET":
		return idgen.Token(32)
	case "BASE64":
		return idgen.Base64(length)
	case "HEX":
		return idgen.Token(length / 2)
	case "SUPABASEANON":
		return supabaseToken(signingKey, "anon", now)
	case "SUPABASESERVICE":
		return supabaseToken(signingKey, "service_role", now)
	}
	return ""
}

// derived kinds are computed from another generated value, so they are
// created after it.
func (g generated) derived() bool { return g.kind == "SUPABASEANON" || g.kind == "SUPABASESERVICE" }

// generatedDefaults returns a fresh value for every generated variable the
// compose file references and that has no value yet.
func generatedDefaults(rawCompose string, existing map[string]string) map[string]string {
	defaults := map[string]string{}
	wanted := map[string]generated{}
	for _, name := range magicName.FindAllString(rawCompose, -1) {
		if _, has := existing[name]; has {
			continue
		}
		if kind, ok := classify(name); ok {
			wanted[name] = kind
		}
	}
	now := time.Now()
	for name, kind := range wanted {
		if !kind.derived() {
			defaults[name] = kind.value("", now)
		}
	}
	signingKey := existing[signingKeyVariable]
	if signingKey == "" {
		signingKey = defaults[signingKeyVariable]
	}
	for name, kind := range wanted {
		if kind.derived() && signingKey != "" {
			defaults[name] = kind.value(signingKey, now)
		}
	}
	return defaults
}

// supabaseToken signs the long-lived JWT the Supabase stack expects: HS256,
// issuer "supabase", valid for 100 years.
func supabaseToken(signingKey, role string, now time.Time) string {
	issued := now.Truncate(time.Minute)
	encode := func(value any) string {
		raw, _ := json.Marshal(value)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	unsigned := encode(map[string]string{"typ": "JWT", "alg": "HS256"}) + "." + encode(map[string]any{
		"iss": "supabase", "iat": issued.Unix(), "exp": issued.AddDate(100, 0, 0).Unix(), "role": role,
	})
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// routeVariables publishes the address of every routed service as
// PIER_URL_<SERVICE> and PIER_HOST_<SERVICE>. The first domain of a service
// is its address; the others are aliases.
func routeVariables(routes []Route) map[string]string {
	variables := map[string]string{}
	seen := map[string]bool{}
	for _, route := range routes {
		parsed, err := compose.ParseRoute(route.Domain)
		if err != nil || seen[route.Service] {
			continue
		}
		seen[route.Service] = true
		suffix := variableSuffix(route.Service)
		for _, prefix := range []string{"PIER_", legacyPrefix} {
			variables[prefix+"URL_"+suffix] = parsed.String()
			variables[prefix+"HOST_"+suffix] = parsed.Host
		}
	}
	return variables
}

var addressReference = regexp.MustCompile(`(?:PIER|CAIS)_(?:URL|HOST)_([A-Za-z0-9_]+)`)

// unpublished names the services whose address the compose file uses but
// that have no domain yet, so those variables stay empty.
func unpublished(rawCompose string, addresses map[string]string) []string {
	missing := map[string]bool{}
	for _, match := range addressReference.FindAllStringSubmatch(rawCompose, -1) {
		if _, set := addresses[match[0]]; !set {
			missing[strings.ToLower(match[1])] = true
		}
	}
	services := make([]string, 0, len(missing))
	for service := range missing {
		services = append(services, service)
	}
	sort.Strings(services)
	return services
}

var nonAlphanumeric = regexp.MustCompile(`[^A-Z0-9]`)

func variableSuffix(serviceName string) string {
	return nonAlphanumeric.ReplaceAllString(strings.ToUpper(serviceName), "_")
}
