package stack

import (
	"context"
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
)

// A Git stack runs a compose file written by whoever controls the repository.
// Everything the file points at (env files, build contexts, bind mounts,
// included and extended files, secrets) must stay inside the checkout. The
// checks are lexical, on the compose files as the repository ships them; the
// symbolic links of the checkout are checked separately, on the server.

const (
	maxComposeFiles = 20
	maxIncludeDepth = 5
)

// composeUnit is one file of the compose tree (the main file plus everything it includes or extends).
type composeUnit struct {
	// file is the path of the file relative to the root of the checkout.
	file string
	doc  map[string]any
}

func (u composeUnit) dir() string { return path.Dir(u.file) }

// loadComposeTree reads the compose file of a checkout and the files it
// includes or extends, refusing any that sits outside the checkout.
func loadComposeTree(ctx context.Context, ex executor.Executor, root, file string) ([]composeUnit, error) {
	units := []composeUnit{}
	seen := map[string]bool{}
	var load func(file string, depth int) error
	load = func(file string, depth int) error {
		if seen[file] {
			return nil
		}
		if depth > maxIncludeDepth || len(seen) >= maxComposeFiles {
			return apperr.Invalid("O compose inclui arquivos demais ou em profundidade demais.")
		}
		seen[file] = true
		raw, err := readSource(ctx, ex, path.Join(root, file))
		if err != nil {
			return apperr.Invalid("Não foi possível ler %s no repositório: %s.", file, lastLine(err))
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			return apperr.Invalid("O arquivo %s não é um YAML válido: %s", file, err)
		}
		unit := composeUnit{file: file, doc: doc}
		units = append(units, unit)
		references, err := unit.referencedFiles()
		if err != nil {
			return err
		}
		for _, reference := range references {
			if err := load(reference, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return units, load(file, 0)
}

// referencedFiles lists the compose files this one includes or extends, as
// paths relative to the root of the checkout.
func (u composeUnit) referencedFiles() ([]string, error) {
	raw := []string{}
	switch include := u.doc["include"].(type) {
	case []any:
		for _, entry := range include {
			raw = append(raw, includePaths(entry)...)
		}
	case string:
		raw = append(raw, include)
	}
	for _, definition := range services(u.doc) {
		if extends, ok := definition["extends"].(map[string]any); ok {
			if file, ok := extends["file"].(string); ok {
				raw = append(raw, file)
			}
		}
	}
	files := []string{}
	for _, reference := range raw {
		resolved, err := u.inside("include", reference)
		if err != nil {
			return nil, err
		}
		files = append(files, resolved)
	}
	return files, nil
}

func includePaths(entry any) []string {
	switch typed := entry.(type) {
	case string:
		return []string{typed}
	case map[string]any:
		switch paths := typed["path"].(type) {
		case string:
			return []string{paths}
		case []any:
			return stringList(paths)
		}
	}
	return nil
}

func services(doc map[string]any) map[string]map[string]any {
	declared, _ := doc["services"].(map[string]any)
	result := map[string]map[string]any{}
	for name, definition := range declared {
		if service, ok := definition.(map[string]any); ok {
			result[name] = service
		}
	}
	return result
}

// inside resolves a path written in this file against its folder and
// refuses it when it leaves the checkout.
func (u composeUnit) inside(owner, reference string) (string, error) {
	resolved, ok := resolveInside(u.dir(), reference)
	if !ok {
		return "", apperr.Invalid("O caminho %q (%s) sai do repositório ou depende de uma variável; "+
			"use caminhos relativos dentro dele.", reference, owner)
	}
	return resolved, nil
}

// resolveInside joins a relative path to a folder of the checkout. It fails
// for absolute paths, paths that climb out, "~" and unresolvable variables.
func resolveInside(dir, reference string) (string, bool) {
	reference = hostBindSource(reference)
	if reference == "" || strings.ContainsAny(reference, "$~\x00\n") || path.IsAbs(reference) {
		return "", false
	}
	joined := path.Clean(path.Join(dir, reference))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", false
	}
	return joined, true
}

// checkContained verifies every path of every file of the tree.
func checkContained(units []composeUnit) error {
	for _, unit := range units {
		for name, service := range services(unit.doc) {
			if err := unit.checkService(name, service); err != nil {
				return err
			}
		}
		for _, section := range []string{"secrets", "configs"} {
			declared, _ := unit.doc[section].(map[string]any)
			for name, definition := range declared {
				entry, _ := definition.(map[string]any)
				if file, ok := entry["file"].(string); ok {
					if _, err := unit.inside(fmt.Sprintf("%s %s", section, name), file); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (u composeUnit) checkService(name string, service map[string]any) error {
	owner := "service " + name
	for _, file := range envFilePaths(service["env_file"]) {
		if _, err := u.inside(owner+", env_file", file); err != nil {
			return err
		}
	}
	if err := u.checkBuild(owner, service["build"]); err != nil {
		return err
	}
	volumes, _ := service["volumes"].([]any)
	for _, volume := range volumes {
		if source, isPath := bindSource(volume); isPath {
			if _, err := u.inside(owner+", volumes", source); err != nil {
				return err
			}
		}
	}
	return nil
}

func envFilePaths(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		files := []string{}
		for _, item := range typed {
			switch entry := item.(type) {
			case string:
				files = append(files, entry)
			case map[string]any:
				if file, ok := entry["path"].(string); ok {
					files = append(files, file)
				}
			}
		}
		return files
	}
	return nil
}

func (u composeUnit) checkBuild(owner string, build any) error {
	var context, dockerfile string
	switch typed := build.(type) {
	case string:
		context = typed
	case map[string]any:
		context, _ = typed["context"].(string)
		dockerfile, _ = typed["dockerfile"].(string)
		if err := u.checkAdditionalContexts(owner, typed["additional_contexts"]); err != nil {
			return err
		}
	default:
		return nil
	}
	if context == "" || isRemoteContext(context) {
		return nil
	}
	resolved, err := u.inside(owner+", build", context)
	if err != nil {
		return err
	}
	if dockerfile != "" {
		_, err = (composeUnit{file: path.Join(resolved, "x")}).inside(owner+", dockerfile", dockerfile)
	}
	return err
}

func (u composeUnit) checkAdditionalContexts(owner string, value any) error {
	entries := []string{}
	switch typed := value.(type) {
	case map[string]any:
		for _, entry := range typed {
			entries = append(entries, fmt.Sprint(entry))
		}
	case []any:
		for _, entry := range typed {
			if _, location, found := strings.Cut(fmt.Sprint(entry), "="); found {
				entries = append(entries, location)
			}
		}
	}
	for _, location := range entries {
		if isRemoteContext(location) || strings.Contains(location, "://") || !looksLikePath(location) {
			continue
		}
		if _, err := u.inside(owner+", additional_contexts", location); err != nil {
			return err
		}
	}
	return nil
}

func isRemoteContext(context string) bool {
	return strings.Contains(context, "://") || strings.HasPrefix(context, "git@") || strings.HasPrefix(context, "github.com/")
}

func looksLikePath(value string) bool {
	return strings.HasPrefix(value, ".") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") ||
		strings.HasPrefix(value, "$")
}

// bindSource returns the host side of a volume entry when it is a path (and
// not a named volume). Absolute paths are not returned: they are host paths
// the operator accepts or not through the "host-path" risk.
func bindSource(volume any) (source string, isPath bool) {
	source, _, _ = bindParts(volume)
	if entry, ok := volume.(map[string]any); ok && entry["type"] != "bind" {
		return "", false
	}
	if strings.HasPrefix(source, "/") {
		return "", false
	}
	return source, looksLikePath(source)
}

// combinedDocument gathers the services of every file of the tree in one
// document, so risks are computed over everything that will run.
func combinedDocument(units []composeUnit) document {
	combined := map[string]any{}
	for _, unit := range units {
		for name, service := range services(unit.doc) {
			combined[unit.file+"#"+name] = service
		}
	}
	return document{"services": combined}
}
