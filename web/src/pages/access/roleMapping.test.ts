/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { parseRoleMappings } from "./roleMapping.ts";

Deno.test("role mapping lines map groups to roles and reject owner", () => {
  const parsed = parseRoleMappings("pier-devs=developer\n\n pier-admins = admin \npier-owners=owner\nbroken\n=viewer");
  assertEquals(parsed.mappings, { "pier-devs": "developer", "pier-admins": "admin" });
  assertEquals(parsed.invalid, ["pier-owners=owner", "broken", "=viewer"]);
});
