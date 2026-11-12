// Pure permission logic mirroring the backend (internal/access/role.go). The
// UI uses it only to hide or disable what the caller cannot do; the API
// still decides on every request.

export type Role = "owner" | "admin" | "developer" | "viewer";
export type Ability = "read" | "deploy" | "write";

/** GET /api/v1/me/access */
export type AccessSummary = {
  instance_role: Role | "";
  projects: { project_id: string; role: Role }[];
  abilities: Ability[];
};

const rank: Record<Role, number> = { viewer: 1, developer: 2, admin: 3, owner: 4 };
const needs: Record<Ability, Role> = { read: "viewer", deploy: "developer", write: "admin" };

function stronger(a: Role | "", b: Role | ""): Role | "" {
  if (!a) return b;
  if (!b) return a;
  return rank[b] > rank[a] ? b : a;
}

/** The caller's role on a project (instance role included); no project = the instance. */
export function roleIn(summary: AccessSummary | undefined, projectId?: string): Role | "" {
  if (!summary) return "";
  const member = projectId ? summary.projects.find((item) => item.project_id === projectId)?.role ?? "" : "";
  return stronger(summary.instance_role, member);
}

/** Whether the role reaches minimum. */
export function atLeast(role: Role | "", minimum: Role): boolean {
  return role !== "" && rank[role] >= rank[minimum];
}

/** Whether the credential and the role both allow ability (and reach minimum, when given). */
export function can(summary: AccessSummary | undefined, ability: Ability, projectId?: string, minimum?: Role): boolean {
  // Like the backend, a credential with "write" holds every ability.
  if (!summary || !(summary.abilities.includes(ability) || summary.abilities.includes("write"))) return false;
  const role = roleIn(summary, projectId);
  return atLeast(role, needs[ability]) && (!minimum || atLeast(role, minimum));
}

/** Roles a member may grant: never more than their own, never owner unless owner. */
export function grantableRoles(summary: AccessSummary | undefined, projectId: string): Role[] {
  const role = roleIn(summary, projectId);
  if (!atLeast(role, "admin")) return [];
  return (["viewer", "developer", "admin", "owner"] as Role[]).filter((candidate) => atLeast(role, candidate));
}
