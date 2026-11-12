package application

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/platform/shell"
)

// placementEngine is scoped to a single rollout; no mutable runtime state is shared.
type placementEngine struct {
	effective  *Application
	d          *Deployer
	deployment Deployment
	log        *deployLog
	jobs       map[string]*job
	previous   map[string][]docker.Container
	files      map[string][]byte
	backups    map[string]string
}

func (r *placementEngine) Architecture(ctx context.Context, id string) (string, error) {
	if c, ok := r.d.connector.(interface {
		CheckDeployment(context.Context, string) error
	}); ok {
		if err := c.CheckDeployment(ctx, id); err != nil {
			return "", err
		}
	}
	ex, _, err := r.d.connector.Executor(ctx, id)
	if err != nil {
		return "", err
	}
	return ex.Run(ctx, "docker info --format '{{.Architecture}}'")
}
func (r *placementEngine) Prepare(ctx context.Context, a Application, id, image string) error {
	if r.effective != nil {
		a = *r.effective
	}
	ex, srv, err := r.d.connector.Executor(ctx, id)
	if err != nil {
		return err
	}
	if srv.Proxy.Entry().Edge || len(a.Config.Network.PortMappings) > 0 {
		return apperr.Invalid("Placement exige proxy direto e portas sem mapeamento no host.")
	}
	vars, err := r.d.env.ListForDeploy(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: a.ID}, a.EnvironmentID, id)
	if err != nil {
		return err
	}
	env := map[string]string{}
	for _, v := range vars {
		env[v.Key] = v.Value
		r.log.redact(v.Value)
	}
	r.log.redact(srv.ProxyToken)
	a.ServerID = id
	j := &job{app: a, deployment: r.deployment, server: srv, ex: ex, log: r.log, env: vars, domains: r.d.domains}
	r.jobs[id] = j
	previous, err := docker.ListByResource(ctx, ex, a.ID)
	if err != nil {
		return err
	}
	r.previous[id] = previous
	if err = docker.EnsureNetwork(ctx, ex); err != nil {
		return err
	}
	name := docker.ReadableName(a.Name, a.ID) + "-" + docker.ShortID(r.deployment.ID)
	file, err := composeFor(releaseSpec{App: a, Image: image, ContainerName: name, DeploymentID: r.deployment.ID, Env: env, Entry: srv.Proxy.Entry(), Server: srv, PendingCertificates: j.pendingCertificates(ctx)})
	if err != nil {
		return err
	}
	dynamic, err := placementRoutes(file, a.Config.Network.Port)
	if err != nil {
		return err
	}
	r.files[id] = dynamic
	for _, s := range file.Services {
		for k := range s.Labels {
			if strings.HasPrefix(k, "traefik.") {
				delete(s.Labels, k)
			}
		}
		s.Labels["traefik.enable"] = "false"
		s.Networks[docker.Network].Aliases = nil
	}
	reps := replicasOf(a.Config, name)
	if err = j.runRelease(ctx, file, primaryReplica(reps).Name); err != nil {
		return err
	}
	data, err := file.Marshal()
	if err != nil {
		return err
	}
	p := r.project(id)
	if err = p.Write(ctx, ex, data, nil); err != nil {
		return err
	}
	return p.Up(ctx, ex, r.log.Output)
}
func (r *placementEngine) project(id string) compose.Project {
	j := r.jobs[id]
	return compose.Project{Name: j.app.ID + "-" + r.deployment.ID, Dir: path.Join(j.server.ResourceDir(docker.TypeApplication, j.app.ID), r.deployment.ID)}
}
func (r *placementEngine) routePath(id string) string {
	return path.Join(r.jobs[id].server.ProxyDir(), "dynamic", "placement-"+r.jobs[id].app.ID+".json")
}
func (r *placementEngine) Ready(ctx context.Context, a Application, id, image string) error {
	j := r.jobs[id]
	a = j.app
	name := docker.ReadableName(a.Name, a.ID) + "-" + docker.ShortID(r.deployment.ID)
	for _, rep := range replicasOf(a.Config, name) {
		if err := j.waitUntilReady(ctx, rep.Name); err != nil {
			return err
		}
	}
	return nil
}
func (r *placementEngine) Activate(ctx context.Context, a Application, id, image string) error {
	j := r.jobs[id]
	file := r.routePath(id)
	backup, err := j.ex.Run(ctx, "if test -f "+shell.Quote(file)+"; then cat "+shell.Quote(file)+"; fi")
	if err != nil {
		return err
	}
	r.backups[id] = backup
	if err = j.ex.WriteFile(ctx, file+".pending", r.files[id], 0600); err != nil {
		return err
	}
	_, err = j.ex.Run(ctx, shell.Join("mv", "-f", file+".pending", file))
	return err
}
func (r *placementEngine) Restore(ctx context.Context, a Application, id, image string) error {
	j := r.jobs[id]
	file := r.routePath(id)
	backup, known := r.backups[id]
	if !known {
		return apperr.Unavailable("Rollback parcial: verifique os destinos.")
	}
	if backup != "" {
		if err := j.ex.WriteFile(ctx, file+".pending", []byte(backup), 0600); err != nil {
			return err
		}
		_, err := j.ex.Run(ctx, shell.Join("mv", "-f", file+".pending", file))
		return err
	}
	_, err := j.ex.Run(ctx, shell.Join("rm", "-f", file))
	return err
}
func (r *placementEngine) Discard(ctx context.Context, a Application, id, image string) error {
	if r.jobs[id] == nil {
		return nil
	}
	p := r.project(id)
	if err := p.Down(ctx, r.jobs[id].ex, false); err != nil {
		return err
	}
	return p.RemoveDir(ctx, r.jobs[id].ex)
}
func (r *placementEngine) Retire(ctx context.Context, a Application, id string) error {
	return r.jobs[id].retire(ctx, r.previous[id])
}

// Convert the existing publication renderer into a file-provider configuration.
// Candidate containers never advertise Docker routers before global readiness.
func placementRoutes(file *compose.File, port int) ([]byte, error) {
	http := map[string]any{}
	services := map[string]any{}
	urls := []any{}
	for _, s := range file.Services {
		if s.Labels[LabelProcess] != WebProcess {
			continue
		}
		urls = append(urls, map[string]string{"url": fmt.Sprintf("http://%s:%d", s.ContainerName, port)})
		for k, v := range s.Labels {
			if !strings.HasPrefix(k, "traefik.http.") {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(k, "traefik.http."), ".")
			if parts[0] == "services" {
				services[parts[1]] = true
				continue
			}
			node := http
			for _, part := range parts[:len(parts)-1] {
				next, ok := node[part].(map[string]any)
				if !ok {
					next = map[string]any{}
					node[part] = next
				}
				node = next
			}
			key := parts[len(parts)-1]
			var value any = v
			switch key {
			case "entrypoints", "middlewares":
				value = strings.Split(v, ",")
			case "attempts":
				value, _ = strconv.Atoi(v)
			case "compress":
				value = map[string]any{}
			case "tls":
				if _, ok := node[key]; ok {
					continue
				}
				value = map[string]any{}
			}
			node[key] = value
		}
	}
	rendered := map[string]any{}
	for name := range services {
		rendered[name] = map[string]any{"loadBalancer": map[string]any{"servers": urls}}
	}
	http["services"] = rendered
	if routers, ok := http["routers"].(map[string]any); ok {
		for _, v := range routers {
			router := v.(map[string]any)
			router["priority"] = 100000 + len(router["rule"].(string))
		}
	}
	return json.Marshal(map[string]any{"http": http})
}
