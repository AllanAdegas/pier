/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { formatRoleMappings, parseRoleMappings } from "./roleMapping.ts";

Deno.test("role mapping lines map groups to roles and reject owner", () => {
  const parsed = parseRoleMappings("pier-devs=developer\n\n pier-admins = admin \npier-owners=owner\nbroken\n=viewer");
  assertEquals(parsed.mappings, { "pier-devs": "developer", "pier-admins": "admin" });
  assertEquals(parsed.invalid, ["pier-owners=owner", "broken", "=viewer"]);
});

Deno.test("a stored mapping formats back to lines that parse to the same mapping", () => {
  const stored = { "pier-devs": "developer", "pier-admins": "admin" } as const;
  const text = formatRoleMappings(stored);
  assertEquals(text, "pier-admins=admin\npier-devs=developer");
  assertEquals(parseRoleMappings(text).mappings, { ...stored });
});
