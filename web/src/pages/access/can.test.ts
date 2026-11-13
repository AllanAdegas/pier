/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { type AccessSummary, can, grantableRoles, roleIn } from "./can.ts";

const member: AccessSummary = {
  instance_role: "",
  projects: [{ project_id: "a", role: "developer" }],
  abilities: ["read", "deploy", "write"],
};

Deno.test("project role grants only on its project", () => {
  assertEquals(can(member, "deploy", "a"), true);
  assertEquals(can(member, "write", "a"), false);
  assertEquals(can(member, "read", "b"), false);
  assertEquals(can(member, "read"), false);
});

Deno.test("credential abilities and role are intersected", () => {
  assertEquals(can({ ...member, abilities: ["read"] }, "deploy", "a"), false);
  assertEquals(can(undefined, "read", "a"), false);
});

Deno.test("instance role covers every project and the instance", () => {
  const viewer: AccessSummary = { instance_role: "viewer", projects: [{ project_id: "a", role: "admin" }], abilities: ["write"] };
  assertEquals(roleIn(viewer, "a"), "admin");
  assertEquals(can(viewer, "read", "z"), true);
  assertEquals(can(viewer, "write", "z"), false);
  assertEquals(can(viewer, "write", "a", "owner"), false);
});

Deno.test("grantable roles never exceed the caller's", () => {
  assertEquals(grantableRoles(member, "a"), []);
  const admin: AccessSummary = { ...member, projects: [{ project_id: "a", role: "admin" }] };
  assertEquals(grantableRoles(admin, "a"), ["viewer", "developer", "admin"]);
  assertEquals(grantableRoles({ ...member, instance_role: "owner" }, "a"), ["viewer", "developer", "admin", "owner"]);
});
