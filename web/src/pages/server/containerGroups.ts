import type { DockerContainer, Project, Resources } from "../../lib/types";

/** A set of containers shown together in the per-project view. */
export interface ContainerGroup {
  key: string;
  title: string;
  /** What kind of group it is, shown under the title. */
  kind: string;
  containers: DockerContainer[];
  running: number;
}

/** Where a managed resource lives: its project, environment and name. */
interface Placement {
  project: string;
  environment: string;
  resource: string;
}

/** Indexes every Pier resource by id, with its project and environment. */
export function placements(resources: Resources | undefined, projects: Project[] | undefined): Map<string, Placement> {
  const environments = new Map<string, { project: string; environment: string }>();
  for (const project of projects ?? []) {
    for (const environment of project.environments ?? []) {
      environments.set(environment.id, { project: project.name, environment: environment.name });
    }
  }
  const index = new Map<string, Placement>();
  const all = [...(resources?.applications ?? []), ...(resources?.databases ?? []), ...(resources?.stacks ?? [])];
  for (const resource of all) {
    const where = environments.get(resource.environment_id);
    if (where) index.set(resource.id, { ...where, resource: resource.name });
  }
  return index;
}

const INFRASTRUCTURE = new Set(["proxy", "cluster"]);

/**
 * Groups containers the way people think of a server: the Pier projects
 * first, then Pier's own infrastructure, then other compose projects, then
 * loose containers. Each group keeps the order of the list it came from.
 */
export function groupContainers(containers: DockerContainer[], index: Map<string, Placement>): ContainerGroup[] {
  const groups = new Map<string, ContainerGroup & { rank: number }>();
  const add = (key: string, title: string, kind: string, rank: number, container: DockerContainer) => {
    let group = groups.get(key);
    if (!group) {
      group = { key, title, kind, rank, containers: [], running: 0 };
      groups.set(key, group);
    }
    group.containers.push(container);
    if (container.state === "running") group.running++;
  };
  for (const container of containers) {
    const owner = container.managed ? container.owner : null;
    const placed = owner ? index.get(owner.id) : undefined;
    if (placed) {
      add(`project:${placed.project}`, placed.project, "Projeto do Pier", 0, container);
    } else if (owner && INFRASTRUCTURE.has(owner.type)) {
      add("pier", "Infraestrutura do Pier", "Proxy e clusters", 1, container);
    } else if (owner) {
      add("orphan", "Recursos sem registro", "Gerenciados por um Pier, mas sem recurso neste painel (removido ou de outra instalação)", 2, container);
    } else if (container.compose_project) {
      add(`compose:${container.compose_project}`, container.compose_project, "Projeto compose fora do Pier", 3, container);
    } else {
      add("loose", "Containers avulsos", "Criados sem compose nem Pier", 4, container);
    }
  }
  return [...groups.values()].sort((a, b) => a.rank - b.rank || a.title.localeCompare(b.title, "pt-BR"));
}

/** The resource and environment of a managed container, for its row. */
export function placementLabel(container: DockerContainer, index: Map<string, Placement>): string {
  const placed = container.owner ? index.get(container.owner.id) : undefined;
  return placed ? `${placed.resource} · ${placed.environment}` : "";
}
