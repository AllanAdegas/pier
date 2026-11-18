package httpapi

import (
	"net/http"
	"strings"

	rbac "pier/internal/access"
	"pier/internal/identity"
)

// scopeKind says where a route finds the project (or the instance) whose role
// authorizes the caller. Every registered route must resolve to a declared
// policy; TestEveryRouteDeclaresAbilityAndScope enforces it.
type scopeKind int

const (
	// scopeUndeclared is the fail-closed fallback: instance owners only.
	scopeUndeclared scopeKind = iota
	// scopePublic routes carry their own protection (login, signed webhooks).
	scopePublic
	// scopeSelf acts on the caller's own account or credentials, or returns
	// static catalogs with no tenant data; no role is needed.
	scopeSelf
	// scopeInstance needs the instance role (servers, settings, users...).
	scopeInstance
	// scopeProject resolves the project from the path wildcard, through
	// persisted rows.
	scopeProject
	// scopeCreate takes the project from the references in the body
	// (environment_id...) and needs at least one.
	scopeCreate
	// scopeCollection lists everything to instance readers and only the
	// items of the caller's projects to everyone else.
	scopeCollection
	// scopeTerminal is the WebSocket of a terminal ticket: the handler
	// re-authorizes the ticket's resource (authorizeTerminal).
	scopeTerminal
)

// routePolicy is the authorization declaration of one route.
type routePolicy struct {
	scope scopeKind
	// kind is the resource kind of the path wildcard (scopeProject) or of
	// the listed items (scopeCollection).
	kind string
	// param names the wildcard holding the id; "id" when empty.
	param string
	// kindParam names a wildcard that holds the kind instead of kind.
	kindParam string
	// minimum is a role floor beyond what the route ability maps to.
	minimum rbac.Role
	// guard is an extra check run once the caller is authorized.
	guard func(*API, *http.Request, identity.Principal) error
}

func (p routePolicy) idParam() string {
	if p.param == "" {
		return "id"
	}
	return p.param
}

var (
	publicPolicy   = routePolicy{scope: scopePublic}
	selfPolicy     = routePolicy{scope: scopeSelf}
	instancePolicy = routePolicy{scope: scopeInstance}
	ownerPolicy    = routePolicy{scope: scopeInstance, minimum: rbac.Owner}
)

func projectPolicy(kind string) routePolicy { return routePolicy{scope: scopeProject, kind: kind} }

func collectionPolicy(kind string) routePolicy {
	return routePolicy{scope: scopeCollection, kind: kind}
}

// exactPolicies override the prefix rules for one route (method + path).
var exactPolicies = map[string]routePolicy{
	"GET /api/v1/projects":                              collectionPolicy("projects"),
	"POST /api/v1/projects":                             instancePolicy,
	"DELETE /api/v1/projects/{id}":                      {scope: scopeProject, kind: "projects", minimum: rbac.Owner},
	"GET /api/v1/applications":                          collectionPolicy("applications"),
	"POST /api/v1/applications":                         {scope: scopeCreate},
	"GET /api/v1/applications/defaults":                 selfPolicy,
	"POST /api/v1/applications/image-ports":             instancePolicy,
	"GET /api/v1/databases":                             collectionPolicy("databases"),
	"POST /api/v1/databases":                            {scope: scopeCreate},
	"GET /api/v1/database-engines":                      selfPolicy,
	"GET /api/v1/stacks":                                collectionPolicy("stacks"),
	"POST /api/v1/stacks":                               {scope: scopeCreate},
	"GET /api/v1/stack-templates":                       selfPolicy,
	"GET /api/v1/stack-templates/{slug}":                selfPolicy,
	"GET /api/v1/deployments":                           collectionPolicy("deployments"),
	"GET /api/v1/dashboard":                             instancePolicy,
	"GET /api/v1/resources":                             instancePolicy,
	"GET /api/v1/resources/{type}/{id}/metrics/history": {scope: scopeProject, kindParam: "type"},
	"GET /api/v1/git-provider/{kind}/{id}":              {scope: scopeProject, kindParam: "kind"},
	"PUT /api/v1/git-provider/{kind}/{id}":              {scope: scopeProject, kindParam: "kind"},
	"DELETE /api/v1/git-provider/{kind}/{id}":           {scope: scopeProject, kindParam: "kind"},
	"GET /api/v1/terminal":                              {scope: scopeTerminal},
	"POST /api/v1/users":                                ownerPolicy,
	"DELETE /api/v1/users/{id}":                         {scope: scopeInstance, minimum: rbac.Owner, guard: (*API).guardAccountRemoval},
	"DELETE /api/v1/tokens/{id}":                        {scope: scopeSelf, guard: (*API).guardOwnToken},
	"POST /api/v1/instance/backups":                     ownerPolicy,
	"GET /api/v1/instance/backups/config":               ownerPolicy,
	"PATCH /api/v1/instance/backups/config":             ownerPolicy,
	"POST /api/v1/cloud/credentials":                    ownerPolicy,
	"DELETE /api/v1/cloud/credentials/{id}":             ownerPolicy,
	"GET /api/v1/access/oidc/start":                     publicPolicy,
	"GET /api/v1/access/oidc/callback":                  publicPolicy,
	"POST /api/v1/access/accept":                        selfPolicy,
	"POST /webhooks/git-provider/github":                publicPolicy,
}

// prefixRule declares the policy of every route under a path prefix.
type prefixRule struct {
	prefix string
	policy routePolicy
}

// prefixRules are matched in order, on whole path segments.
var prefixRules = []prefixRule{
	{"/api/v1/auth", publicPolicy},
	{"/webhooks", publicPolicy},
	{"/api/v1/me", selfPolicy},
	{"/api/v1/tokens", selfPolicy},
	{"/api/v1/access/instance", ownerPolicy},
	{"/api/v1/access/oidc", ownerPolicy},
	{"/api/v1/projects/{id}", projectPolicy("projects")},
	{"/api/v1/environments/{id}", projectPolicy("environments")},
	{"/api/v1/applications/{id}", projectPolicy("applications")},
	{"/api/v1/databases/{id}", projectPolicy("databases")},
	{"/api/v1/stacks/{id}", projectPolicy("stacks")},
	{"/api/v1/deployments/{id}", projectPolicy("deployments")},
	{"/api/v1/tasks/{id}", projectPolicy("tasks")},
	{"/api/v1/backups/{id}", projectPolicy("backups")},
	{"/api/v1/volume-backups/{id}", projectPolicy("volume-backups")},
	{"/api/v1/servers", instancePolicy},
	{"/api/v1/clusters", instancePolicy},
	{"/api/v1/swarm", instancePolicy},
	{"/api/v1/keys", instancePolicy},
	{"/api/v1/registries", instancePolicy},
	{"/api/v1/git-provider", instancePolicy},
	{"/api/v1/git-connections", instancePolicy},
	{"/api/v1/settings", instancePolicy},
	{"/api/v1/notifications", instancePolicy},
	{"/api/v1/notification-events", instancePolicy},
	{"/api/v1/monitoring", instancePolicy},
	{"/api/v1/instance", instancePolicy},
	{"/api/v1/updates", instancePolicy},
	{"/api/v1/audit", instancePolicy},
	{"/api/v1/users", instancePolicy},
	// Log drains forward logs to any URL, even when scoped to one project:
	// instance-level, like the cloud accounts that bill the operator.
	{"/api/v1/log-drains", instancePolicy},
	{"/api/v1/cloud", instancePolicy},
}

// kindAliases maps the kind wildcards (singular or plural) to resource kinds.
var kindAliases = map[string]string{
	"application": "applications", "applications": "applications",
	"database": "databases", "databases": "databases",
	"stack": "stacks", "stacks": "stacks",
}

// policyFor returns the declaration of a registered pattern ("GET /path").
// The second result is false for an undeclared route.
func policyFor(pattern string) (routePolicy, bool) {
	if policy, ok := exactPolicies[pattern]; ok {
		return policy, true
	}
	_, path, found := strings.Cut(pattern, " ")
	if !found {
		path = pattern
	}
	for _, rule := range prefixRules {
		if path == rule.prefix || strings.HasPrefix(path, rule.prefix+"/") {
			return rule.policy, true
		}
	}
	return routePolicy{scope: scopeUndeclared, minimum: rbac.Owner}, false
}

// registeredRoute is what a.route and a.public record at registration.
type registeredRoute struct {
	pattern  string
	required access
	public   bool
}
