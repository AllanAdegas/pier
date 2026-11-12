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

/** Suggests the provider from the repository host. */
export function suggestedProvider(repository: string, linked: boolean): GitFeedbackProvider {
  const value = repository.toLowerCase();
  if (linked || value.includes("github.com")) {
    return linked ? "github" : "none";
  }
  if (value.includes("bitbucket.org")) {
    return "bitbucket";
  }
  if (value.includes("gitlab")) {
    return "gitlab";
  }
  if (value.includes("gitea") || value.includes("forgejo") || value.includes("codeberg.org")) {
    return "gitea";
  }
  return "none";
}
