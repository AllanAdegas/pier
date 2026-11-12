package httpapi

import (
	"context"
	"net/http"

	rbac "pier/internal/access"
	"pier/internal/identity"
	"pier/internal/platform/apperr"
)

func accessDenied() error { return apperr.Forbidden("Acesso não autorizado.") }

// grant is the outcome of authorizing one request.
type grant struct {
	// principal is reduced to what the caller's role allows.
	principal identity.Principal
	// filterKind is set when a collection must drop foreign items.
	filterKind string
}

// authorizeRequest applies the route's declared policy (access_policy.go)
// after authentication. It runs for REST and for MCP tools alike: tools call
// these same routes in process with the agent's token.
func (a *API) authorizeRequest(r *http.Request, p identity.Principal, required access) (grant, error) {
	if a.Access == nil {
		return grant{}, accessDenied()
	}
	policy, _ := policyFor(r.Pattern)
	granted, err := a.authorizePolicy(r, p, required, policy)
	if err == nil && policy.guard != nil {
		err = policy.guard(a, r, p)
	}
	return granted, err
}

func (a *API) authorizePolicy(r *http.Request, p identity.Principal, required access, policy routePolicy) (grant, error) {
	switch policy.scope {
	case scopePublic, scopeSelf, scopeTerminal:
		return grant{principal: p}, nil
	case scopeCollection:
		return a.authorizeCollection(r.Context(), p, required, policy)
	}
	project, err := a.primaryProject(r, policy)
	if err != nil {
		return grant{}, err
	}
	role, err := a.authorizeScope(r.Context(), p, project, required.ability, policy.minimum)
	if err != nil {
		return grant{}, err
	}
	if err := a.checkBodyReferences(r, p, required.ability); err != nil {
		return grant{}, err
	}
	return grant{principal: within(p, role)}, nil
}

// authorizeScope demands that the credential and the role on project ("" =
// the instance) both grant ability, and that the role reaches minimum.
func (a *API) authorizeScope(ctx context.Context, p identity.Principal, project string, ability identity.Ability, minimum rbac.Role) (rbac.Role, error) {
	if !p.Can(ability) {
		return "", accessDenied()
	}
	role, err := a.Access.Role(ctx, p.User.ID, project)
	if err != nil {
		return "", err
	}
	if !role.Allows(string(ability)) || (minimum != "" && !role.AtLeast(minimum)) {
		return "", accessDenied()
	}
	return role, nil
}

// primaryProject finds the project of the route. A resource that cannot be
// resolved (missing, or an unknown kind) falls back to the instance: only
// instance roles may then proceed, and they get the handler's 404.
func (a *API) primaryProject(r *http.Request, policy routePolicy) (string, error) {
	switch policy.scope {
	case scopeProject:
		return a.resolveProject(r.Context(), policy.resourceKind(r), r.PathValue(policy.idParam()))
	case scopeCreate:
		return a.bodyProject(r)
	}
	return "", nil
}

func (p routePolicy) resourceKind(r *http.Request) string {
	if p.kindParam != "" {
		return kindAliases[r.PathValue(p.kindParam)]
	}
	return p.kind
}

func (a *API) resolveProject(ctx context.Context, kind, id string) (string, error) {
	project, err := a.Access.Project(ctx, kind, id)
	if err == nil {
		return project, nil
	}
	if failure, ok := apperr.As(err); ok && failure.Kind == apperr.KindForbidden {
		return "", nil
	}
	return "", err
}

// guardOwnToken hides whether a foreign token exists.
func (a *API) guardOwnToken(r *http.Request, p identity.Principal) error {
	return a.Access.OwnToken(r.Context(), p.User.ID, r.PathValue("id"))
}

// guardAccountRemoval keeps the last instance owner (the database trigger
// enforces it too; this gives the clear message).
func (a *API) guardAccountRemoval(r *http.Request, _ identity.Principal) error {
	return a.Access.GuardAccountRemoval(r.Context(), r.PathValue("id"))
}

func within(p identity.Principal, role rbac.Role) identity.Principal {
	return p.Within(func(ability identity.Ability) bool { return role.Allows(string(ability)) })
}

// authorizeCollection lets instance readers list everything; anyone else gets
// the list filtered to the projects they belong to.
func (a *API) authorizeCollection(ctx context.Context, p identity.Principal, required access, policy routePolicy) (grant, error) {
	if !p.Can(required.ability) {
		return grant{}, accessDenied()
	}
	if role, err := a.Access.Role(ctx, p.User.ID, ""); err == nil && role.Allows(string(required.ability)) {
		return grant{principal: within(p, role)}, nil
	}
	return grant{principal: within(p, ""), filterKind: policy.kind}, nil
}
