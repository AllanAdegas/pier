/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { needsRegistry, shortImage, toggleDestination } from "./placementModel.ts";

const empty = { build_server_id: "", registry_id: "", repository: "", destinations: [] as string[] };

Deno.test("the application's own server stays first and cannot be removed", () => {
  const added = toggleDestination(empty, "own", "b", true);
  assertEquals(added.destinations, ["own", "b"]);
  assertEquals(toggleDestination(added, "own", "own", false).destinations, ["own", "b"]);
  assertEquals(toggleDestination(added, "own", "b", false).destinations, ["own"]);
});

Deno.test("a registry is needed only when a built image leaves its server", () => {
  assertEquals(needsRegistry({ ...empty, destinations: ["own"] }, "own", false), false);
  assertEquals(needsRegistry({ ...empty, destinations: ["own", "b"] }, "own", false), true);
  assertEquals(needsRegistry({ ...empty, destinations: ["own", "b"] }, "own", true), false);
  assertEquals(needsRegistry({ ...empty, build_server_id: "builder", destinations: ["own"] }, "own", false), true);
});

Deno.test("digests are shortened for display", () => {
  assertEquals(shortImage("ghcr.io/acme/api@sha256:" + "a".repeat(64)), "ghcr.io/acme/api@aaaaaaaaaaaa");
  assertEquals(shortImage("nginx:1.27"), "nginx:1.27");
});

Deno.test("Swarm builds require a registry even on the application server", () => {
 assertEquals(needsRegistry({ swarm_cluster_id: "cluster", build_server_id: "", registry_id: "", repository: "", destinations: ["primary"] }, "primary", false), true);
});
