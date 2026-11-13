package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode"

	"pier/internal/identity"
)

// referenceKinds maps body keys that name a project resource to its kind.
// Keys are compared case-folded, because encoding/json matches struct
// fields case-insensitively: {"Environment_ID": ...} must not slip through.
var referenceKinds = foldedKeys(map[string]string{
	"project_id": "projects", "environment_id": "environments", "target_environment_id": "environments",
	"application_id": "applications", "target_application_id": "applications", "source_application_id": "applications",
	"database_id": "databases", "stack_id": "stacks", "deployment_id": "deployments", "task_id": "tasks",
	"backup_id": "backups",
})

// sharedTargets are the servers resources run on. Choosing one needs only
// the ability on the resource's project: servers are a pool shared by every
// project. Any other *_id (keys, registries, users...) is instance-level.
var sharedTargets = foldedKeys(map[string]string{"server_id": "", "build_server_id": ""})

func foldedKeys(source map[string]string) map[string]string {
	folded := make(map[string]string, len(source))
	for key, kind := range source {
		folded[foldKey(key)] = kind
	}
	return folded
}

// foldKey maps every rune to the smallest rune of its case-folding orbit,
// the equivalence encoding/json uses (it also folds the Kelvin sign and the
// long s onto k and s).
func foldKey(key string) string {
	return strings.Map(func(r rune) rune {
		smallest := r
		for folded := unicode.SimpleFold(r); folded != r; folded = unicode.SimpleFold(folded) {
			smallest = min(smallest, folded)
		}
		return smallest
	}, key)
}

var (
	foldedIDSuffix  = foldKey("_id")
	foldedIDsSuffix = foldKey("_ids")
)

// requestBody returns the first JSON value of the body, decoded the way the
// handlers' decode does, and puts the bytes back for the handler. ok is false
// when there is nothing the handler could decode as JSON.
func requestBody(r *http.Request) (value any, ok bool, err error) {
	if r.Body == nil || r.Body == http.NoBody || r.Method == http.MethodGet {
		return nil, false, nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return nil, false, err
	}
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(raw), r.Body), r.Body}
	if len(raw) > maxBodyBytes {
		// decode refuses it (413) before reading any field.
		return nil, false, nil
	}
	if json.NewDecoder(bytes.NewReader(raw)).Decode(&value) != nil {
		return nil, false, nil
	}
	return value, true, nil
}

// checkBodyReferences demands the route ability on every resource the body
// names, so a request on project A cannot reach into project B (promote to a
// foreign environment, move a resource, reference a foreign database...).
func (a *API) checkBodyReferences(r *http.Request, p identity.Principal, ability identity.Ability) error {
	body, ok, err := requestBody(r)
	if err != nil || !ok {
		return err
	}
	return a.walkReferences(r, p, ability, body, nil)
}

// bodyProject returns the project of the first project reference of a
// creation body. Creating without one is an instance-level operation.
func (a *API) bodyProject(r *http.Request) (string, error) {
	body, ok, err := requestBody(r)
	if err != nil || !ok {
		return "", err
	}
	project := ""
	found := false
	err = a.walkReferences(r, identity.Principal{}, "", body, func(kind, id string) error {
		if found {
			return nil
		}
		found = true
		resolved, resolveErr := a.resolveProject(r.Context(), kind, id)
		project = resolved
		return resolveErr
	})
	return project, err
}

// walkReferences visits every reference of the body. With visit nil, it
// authorizes each one; otherwise it hands project references to visit.
func (a *API) walkReferences(r *http.Request, p identity.Principal, ability identity.Ability, value any,
	visit func(kind, id string) error) error {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if err := a.walkReferences(r, p, ability, item, visit); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, item := range typed {
			if err := a.checkReference(r, p, ability, foldKey(key), item, visit); err != nil {
				return err
			}
			if err := a.walkReferences(r, p, ability, item, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *API) checkReference(r *http.Request, p identity.Principal, ability identity.Ability, key string, item any,
	visit func(kind, id string) error) error {
	id, isString := item.(string)
	kind, isProject := referenceKinds[key]
	if visit != nil {
		if isProject && isString && id != "" {
			return visit(kind, id)
		}
		return nil
	}
	_, isShared := sharedTargets[key]
	switch {
	case isShared:
		return nil
	case isProject && isString && id != "":
		project, err := a.resolveProject(r.Context(), kind, id)
		if err != nil {
			return err
		}
		_, err = a.authorizeScope(r.Context(), p, project, ability, "")
		return err
	case strings.HasSuffix(key, foldedIDsSuffix) || (strings.HasSuffix(key, foldedIDSuffix) && isString && id != ""):
		_, err := a.authorizeScope(r.Context(), p, "", ability, "")
		return err
	}
	return nil
}
