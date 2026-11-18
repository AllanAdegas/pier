import type { Role } from "./can";

const ssoRoles: Role[] = ["admin", "developer", "viewer"];

/** Parses "group=role" lines into the SSO role mapping; owner is never mapped. */
export function parseRoleMappings(text: string): { mappings: Record<string, Role>; invalid: string[] } {
  const mappings: Record<string, Role> = {};
  const invalid: string[] = [];
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (!line) continue;
    const separator = line.lastIndexOf("=");
    const value = line.slice(0, separator).trim();
    const role = line.slice(separator + 1).trim() as Role;
    if (separator <= 0 || !value || !ssoRoles.includes(role)) invalid.push(line);
    else mappings[value] = role;
  }
  return { mappings, invalid };
}

/** Writes a stored mapping back as sorted "group=role" lines for editing. */
export function formatRoleMappings(mappings: Record<string, string>): string {
  return Object.keys(mappings).sort().map((group) => `${group}=${mappings[group]}`).join("\n");
}
