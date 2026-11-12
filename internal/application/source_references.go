package application

import (
	"context"
	"net/url"
	"strconv"

	"pier/internal/database"
	"pier/internal/platform/i18n"
)

// ReferenceDatabases is the database service surface needed during deployment.
type ReferenceDatabases interface {
	List(context.Context, string) ([]database.Database, error)
	Connection(context.Context, string) (database.Connection, error)
}

// DatabaseReferences adapts managed database connections to envvar.ReferenceResolver.
type DatabaseReferences struct{ Databases ReferenceDatabases }

func (r DatabaseReferences) Resolve(ctx context.Context, environmentID, serverID, name, field string) (string, error) {
	databases, err := r.Databases.List(ctx, environmentID)
	if err != nil {
		return "", err
	}
	var selected *database.Database
	// IDs take precedence over names, and names must be unambiguous.
	for i := range databases {
		candidate := &databases[i]
		if candidate.EnvironmentID == environmentID && candidate.ID == name {
			selected = candidate
			break
		}
	}
	if selected == nil {
		for i := range databases {
			candidate := &databases[i]
			if candidate.EnvironmentID != environmentID || candidate.Name != name {
				continue
			}
			if selected != nil {
				return "", i18n.Errorf("há mais de um banco com o nome %s; use o ID", name)
			}
			selected = candidate
		}
	}
	if selected == nil {
		return "", i18n.Errorf("banco %s não encontrado neste ambiente", name)
	}
	connection, err := r.Databases.Connection(ctx, selected.ID)
	if err != nil {
		return "", err
	}
	endpoint := connection.InternalURL
	host, port := connection.Host, connection.Port
	if selected.ServerID != serverID {
		if connection.PublicURL == "" {
			return "", i18n.Errorf("o banco %s está em outro servidor e não possui acesso público", name)
		}
		endpoint = connection.PublicURL
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Hostname() == "" {
			return "", i18n.Errorf("a conexão pública do banco %s é inválida", name)
		}
		host = parsed.Hostname()
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			return "", i18n.Errorf("a conexão pública do banco %s é inválida", name)
		}
	}
	switch field {
	case "url":
		return endpoint, nil
	case "host":
		return host, nil
	case "port":
		return strconv.Itoa(port), nil
	case "user":
		return connection.Credentials.Username, nil
	case "password":
		return connection.Credentials.Password, nil
	case "database":
		return connection.Credentials.Database, nil
	}
	return "", i18n.Errorf("campo de referência de banco desconhecido: %s", field)
}
