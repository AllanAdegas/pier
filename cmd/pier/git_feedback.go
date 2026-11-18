package main

import (
	"context"

	"pier/internal/application"
	"pier/internal/gitprovider"
	"pier/internal/platform/apperr"
	"pier/internal/stack"
)

// feedbackRepositories tells the Git feedback queue which repository a
// resource uses at send time.
type feedbackRepositories struct {
	applications *application.Service
	stacks       *stack.Service
}

func (r *feedbackRepositories) repository(ctx context.Context, kind, id string) (string, error) {
	if kind == gitprovider.KindStack {
		current, err := r.stacks.Get(ctx, id)
		if err != nil {
			return "", err
		}
		if current.Git == nil {
			return "", apperr.NotFound("Stack")
		}
		return current.Git.Repository, nil
	}
	app, err := r.applications.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return app.Config.Source.Repository, nil
}
