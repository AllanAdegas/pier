/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { promotionTargets, triggerLabel } from "./release.ts";
import type { Application, Project } from "../../lib/types.ts";

const app = (id: string, name: string, environment_id: string, server_id = "s1") => ({ id, name, environment_id, server_id }) as Application;

Deno.test("promotion targets are the other environments of the same project, same name and server first", () => {
  const projects = [
    { id: "p1", environments: [{ id: "staging", name: "staging" }, { id: "prod", name: "production" }, { id: "qa", name: "qa" }] },
    { id: "p2", environments: [{ id: "other", name: "other" }] },
  ] as Project[];
  const source = app("a", "api", "staging");
  const targets = promotionTargets(source, projects, [
    source,
    app("b", "web", "prod"),
    app("c", "api", "prod"),
    app("d", "api", "qa", "s2"),
    app("e", "api", "other"),
    app("f", "worker", "staging"),
  ]);
  assertEquals(targets.map((target) => [target.application.id, target.environmentName, target.sameServer]), [
    ["c", "production", true],
    ["b", "production", true],
    ["d", "qa", false],
  ]);
  assertEquals(promotionTargets(app("x", "x", "unknown"), projects, []), []);
});

Deno.test("unknown triggers fall back to their identifier", () => {
  assertEquals(triggerLabel("rollback"), "Rollback");
  assertEquals(triggerLabel("future"), "future");
});
