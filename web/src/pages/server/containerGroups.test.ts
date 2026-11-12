/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import type { DockerContainer, Project, Resources } from "../../lib/types";
import { groupContainers, placementLabel, placements } from "./containerGroups";
import { t } from "../../i18n";

function container(name: string, extra: Partial<DockerContainer> = {}): DockerContainer {
  return {
    id: name, name, image: "x", command: "", state: "running", status: "Up", ports: "", networks: "", created: "",
    managed: false, owner: null, compose_project: "", ...extra,
  };
}

const projects = [
  { id: "p1", name: "Concord", description: "", created_at: "", environments: [{ id: "e1", project_id: "p1", name: "produção", created_at: "" }] },
  { id: "p2", name: "Blog", description: "", created_at: "", environments: [{ id: "e2", project_id: "p2", name: "staging", created_at: "" }] },
] as Project[];
const resources = {
  applications: [{ id: "app1", name: "API", environment_id: "e2" }],
  databases: [],
  stacks: [{ id: "stk1", name: "Concord", environment_id: "e1" }],
} as unknown as Resources;

Deno.test("containers are grouped by Pier project, then infrastructure, compose projects and loose ones", () => {
  const index = placements(resources, projects);
  const groups = groupContainers(
    [
      container("loose"),
      container("cvat_ui", { compose_project: "cvat" }),
      container("frontend", { managed: true, owner: { type: "stack", id: "stk1" }, compose_project: "docker" }),
      container("livekit", { managed: true, owner: { type: "stack", id: "stk1" }, state: "exited" }),
      container("api", { managed: true, owner: { type: "application", id: "app1" } }),
      container("pier-proxy", { managed: true, owner: { type: "proxy", id: "proxy" } }),
      container("ghost", { managed: true, owner: { type: "stack", id: "gone" } }),
    ],
    index,
  );
  assertEquals(
    groups.map((group) => `${group.title}:${group.running}/${group.containers.length}`),
    ["Blog:1/1", "Concord:1/2", `${t("Infraestrutura do Pier")}:1/1`, `${t("Recursos sem registro")}:1/1`, "cvat:1/1", `${t("Containers avulsos")}:1/1`],
  );
  assertEquals(placementLabel(groups[1].containers[0], index), "Concord · produção");
});

Deno.test("without resources loaded, managed containers are not lost", () => {
  const groups = groupContainers([container("frontend", { managed: true, owner: { type: "stack", id: "stk1" } })], placements(undefined, undefined));
  assertEquals(groups.map((group) => group.title), [t("Recursos sem registro")]);
});
