import type { GitFeedbackProvider, GitFeedbackSettings } from "../../lib/types.ts";

/** A repository chosen from a GitHub App installation in a creation form. */
export interface GitHubPick {
  installation_id: number;
  repository: string;
}

/** The HTTPS address a resource linked to the GitHub App must clone. */
export function githubCloneURL(repository: string): string {
  return `https://github.com/${repository}.git`;
}

/**
 * Whether the repository typed in the form is still the picked one: if the
 * person edited the address afterwards, the link would only fail the deploy.
 */
export function pickStillApplies(pick: GitHubPick | null, repository: string): pick is GitHubPick {
  if (!pick) {
    return false;
  }
  const normalize = (value: string) => value.trim().replace(/\/+$/, "").replace(/\.git$/, "").toLowerCase();
  return normalize(repository) === normalize(githubCloneURL(pick.repository));
}

/** The code and state GitHub appends when it returns from creating the App. */
export function manifestCallback(search: string): { code: string; state: string } | null {
  const params = new URLSearchParams(search);
  const code = params.get("code") ?? "";
  const state = params.get("state") ?? "";
  return code && state ? { code, state } : null;
}

/** Providers that need an access token stored by Pier. */
export function needsToken(provider: GitFeedbackProvider): boolean {
  return provider === "gitlab" || provider === "gitea" || provider === "bitbucket";
}

/** The body saved for the feedback settings; an empty token keeps the stored one. */
export function feedbackInput(settings: GitFeedbackSettings, token: string): GitFeedbackSettings {
  const input: GitFeedbackSettings = { ...settings };
  delete input.token;
  if (needsToken(settings.provider) && token.trim() !== "") {
    input.token = token.trim();
  }
  return input;
}

/** Reads a HTTPS/SSH URL or Git's SCP-style address without trusting its path. */
function repositoryHost(repository: string): string {
  const value = repository.trim();
  try {
    if (value.includes("://")) {
      const url = new URL(value);
      return url.protocol === "https:" || url.protocol === "ssh:" ? url.hostname.toLowerCase() : "";
    }
    const scp = /^(?:[^/@:\s]+@)?([A-Za-z0-9.-]+):[^\s]+$/.exec(value);
    return scp ? new URL(`ssh://${scp[1]}`).hostname.toLowerCase() : "";
  } catch {
    return "";
  }
}

/** Suggests only known provider hosts; self-hosted instances are chosen explicitly. */
export function suggestedProvider(repository: string, linked: boolean): GitFeedbackProvider {
  if (linked) return "github";
  switch (repositoryHost(repository)) {
    case "bitbucket.org": return "bitbucket";
    case "gitlab.com": return "gitlab";
    case "codeberg.org": return "gitea";
    default: return "none";
  }
}
