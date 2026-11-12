import { createSignal } from "solid-js";
import { api } from "../../lib/api";
import { type Ability, type AccessSummary, can, type Role } from "./can";

// One shared copy of GET /me/access for the whole interface, loaded on first
// use (never before login). Pages call allowed(...) to hide or disable
// actions; the API remains the authority, so a failed load hides actions.
const [summary, setSummary] = createSignal<AccessSummary | undefined>();
let loading: Promise<void> | undefined;
let generation = 0;

/** Drops the previous session's roles and ignores its in-flight requests. */
export function resetAccess(): void {
 generation++;
 loading = undefined;
 setSummary(undefined);
}

/** Reloads the roles, e.g. after accepting an invitation or changing roles. */
export function refreshAccess(): Promise<void> {
  const requestedGeneration = generation;
  loading = api.get<AccessSummary>("/me/access")
    .then((value) => { if (generation === requestedGeneration) setSummary(value); })
    .catch(() => { if (generation === requestedGeneration) setSummary(undefined); });
  return loading;
}

export function accessSummary(): AccessSummary | undefined {
  if (!loading) void refreshAccess();
  return summary();
}

/** Reactive can() over the caller's current roles. */
export function allowed(ability: Ability, projectId?: string, minimum?: Role): boolean {
  return can(accessSummary(), ability, projectId, minimum);
}
