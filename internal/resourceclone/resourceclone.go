// Package resourceclone copies the definition of databases and stacks into
// another environment. Applications clone themselves (application.Service.Clone);
// this package exists because the stack context depends on the application one.
package resourceclone

import (
	"context"

	"pier/internal/application"
	"pier/internal/database"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/stack"
)

// Service clones databases and stacks.
type Service struct {
	Databases *database.Service
	Stacks    *stack.Service
	Env       *envvar.Store
}

// CloneDatabase creates an empty database with the same engine, image, user,
// database name, limits and backup policy. Data is never copied, passwords
// are generated anew and public access starts closed (its host port belongs
// to the source).
func (s *Service) CloneDatabase(ctx context.Context, sourceID string, options application.CloneOptions) (database.Database, error) {
	source, err := s.Databases.Get(ctx, sourceID)
	if err != nil {
		return database.Database{}, err
	}
	plan, err := application.PlanClone(ctx, s.Env, source.EnvironmentID, source.Name, options)
	if err != nil {
		return database.Database{}, err
	}
	connection, err := s.Databases.Connection(ctx, sourceID)
	if err != nil {
		return database.Database{}, err
	}
	clone, err := s.Databases.Create(ctx, database.CreateInput{
		EnvironmentID: plan.Options.TargetEnvironmentID, ServerID: source.ServerID, Name: plan.Name,
		Description: source.Description, Engine: source.Engine, Image: source.Config.Image,
		Username: connection.Credentials.Username, Database: connection.Credentials.Database,
	})
	if err != nil {
		return database.Database{}, err
	}
	config := source.Config
	config.PublicPort = 0
	clone, err = s.Databases.Update(ctx, clone.ID, database.UpdateInput{Name: clone.Name, Description: clone.Description, Config: config})
	if err != nil {
		return database.Database{}, err
	}
	owner := func(id string) envvar.Owner { return envvar.Owner{Type: envvar.OwnerDatabase, ID: id} }
	return clone, plan.CopyVariables(ctx, s.Env, owner(source.ID), owner(clone.ID))
}

// CloneStack creates a stack running the same compose file (or the same Git
// source) in the target environment. Routes are copied only with CopyDomains.
// A stack that runs from a folder of the server is refused: both would share
// the same files and compose project.
func (s *Service) CloneStack(ctx context.Context, sourceID string, options application.CloneOptions) (stack.Stack, error) {
	source, err := s.Stacks.Get(ctx, sourceID)
	if err != nil {
		return stack.Stack{}, err
	}
	if source.FromDirectory() && !source.FromGit() {
		return stack.Stack{}, apperr.Conflict("Stacks de uma pasta do servidor não podem ser clonadas: a cópia usaria os mesmos arquivos.")
	}
	plan, err := application.PlanClone(ctx, s.Env, source.EnvironmentID, source.Name, options)
	if err != nil {
		return stack.Stack{}, err
	}
	input := stack.CreateInput{
		EnvironmentID: plan.Options.TargetEnvironmentID, ServerID: source.ServerID, Name: plan.Name,
		Description: source.Description, Routes: []stack.Route{},
	}
	if plan.CopyDomains {
		input.Routes = source.Routes
	}
	if source.FromGit() {
		input.Git = cloneGitInput(*source.Git, plan)
	} else {
		input.Compose = source.Compose
	}
	clone, err := s.Stacks.Create(ctx, input)
	if err != nil {
		return stack.Stack{}, err
	}
	owner := func(id string) envvar.Owner { return envvar.Owner{Type: envvar.OwnerStack, ID: id} }
	return clone, plan.CopyVariables(ctx, s.Env, owner(source.ID), owner(clone.ID))
}

// cloneGitInput copies the Git source. Accepted risks are an operator's
// decision about one stack and are not carried over: the clone asks again.
func cloneGitInput(source stack.GitSource, plan application.ClonePlan) *stack.GitInput {
	autoDeploy := source.AutoDeploy
	return &stack.GitInput{
		Repository: plan.CloneRepository(source.Repository), Branch: source.Branch, Tag: source.Tag,
		BaseDirectory: source.BaseDirectory, ComposePath: source.ComposePath, PrivateKeyID: source.PrivateKeyID,
		WatchPaths: append([]string{}, source.WatchPaths...), AutoDeploy: &autoDeploy,
	}
}
