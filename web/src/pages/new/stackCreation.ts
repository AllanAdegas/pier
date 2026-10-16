import { api } from "../../lib/api";
import type { Stack } from "../../lib/types";

/** Creates a stack and, unless told otherwise, starts its first deployment. */
export async function createStack(body: Record<string, unknown>, deploy = true): Promise<Stack> {
  const stack = await api.post<Stack>("/stacks", body);
  if (deploy) {
    // The stack exists either way; a failed first deploy is shown on its page.
    await api.post(`/stacks/${stack.id}/actions/deploy`).catch(() => undefined);
  }
  return stack;
}
