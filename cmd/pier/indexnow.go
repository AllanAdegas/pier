package main

import (
	"context"

	"pier/internal/application"
	"pier/internal/indexnow"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/server"
	"pier/internal/stack"
)

// searchResources tells the IndexNow service the domains of an application
// or stack and the servers that publish them.
type searchResources struct {
	applications *application.Service
	multi        *application.MultiServer
	stacks       *stack.Service
}

func (r searchResources) Resource(ctx context.Context, kind, id string) (indexnow.Resource, error) {
	if kind == indexnow.KindStack {
		current, err := r.stacks.Get(ctx, id)
		if err != nil {
			return indexnow.Resource{}, gone(err)
		}
		domains := make([]string, 0, len(current.Routes))
		for _, route := range current.Routes {
			domains = append(domains, route.Domain)
		}
		return indexnow.Resource{Domains: domains, ServerIDs: []string{current.ServerID}, Proxied: true}, nil
	}
	app, err := r.applications.Get(ctx, id)
	if err != nil {
		return indexnow.Resource{}, gone(err)
	}
	resource := indexnow.Resource{Domains: app.Config.Network.Domains, ServerIDs: []string{app.ServerID}, Proxied: true}
	placement, err := r.multi.Settings(ctx, id)
	if err != nil {
		return indexnow.Resource{}, err
	}
	// A Swarm service is routed by the swarm, not by Pier's proxy.
	resource.Proxied = placement.SwarmClusterID == ""
	servers, err := r.multi.Servers(ctx, id)
	if err != nil {
		return indexnow.Resource{}, err
	}
	resource.ServerIDs = []string{}
	for _, placed := range servers {
		if placed.Role != application.RoleLeaving {
			resource.ServerIDs = append(resource.ServerIDs, placed.ServerID)
		}
	}
	return resource, nil
}

func (r searchResources) List(ctx context.Context) ([]indexnow.ResourceRef, error) {
	apps, err := r.applications.List(ctx, "")
	if err != nil {
		return nil, err
	}
	stacks, err := r.stacks.List(ctx, "")
	if err != nil {
		return nil, err
	}
	refs := make([]indexnow.ResourceRef, 0, len(apps)+len(stacks))
	for _, app := range apps {
		refs = append(refs, indexnow.ResourceRef{Kind: indexnow.KindApplication, ID: app.ID, Name: app.Name})
	}
	for _, current := range stacks {
		refs = append(refs, indexnow.ResourceRef{Kind: indexnow.KindStack, ID: current.ID, Name: current.Name})
	}
	return refs, nil
}

// gone tells a resource that no longer exists from any other failure.
func gone(err error) error {
	if failure, ok := apperr.As(err); ok && failure.Kind == apperr.KindNotFound {
		return indexnow.ErrResourceGone
	}
	return err
}

// searchProxies reaches the servers whose proxy serves the keys.
type searchProxies struct {
	servers   *server.Service
	connector *server.Connector
}

func (p searchProxies) Get(ctx context.Context, id string) (server.Server, error) {
	return p.servers.Get(ctx, id)
}

func (p searchProxies) Executor(ctx context.Context, id string) (executor.Executor, server.Server, error) {
	return p.connector.Executor(ctx, id)
}
