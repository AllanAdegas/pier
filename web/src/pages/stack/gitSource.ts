import type { StackGitInput, StackGitSource } from "../../lib/types.ts";

/** One glob per line; blank lines are dropped. */
export function parseLines(text: string): string[] {
  return text.split("\n").map((line) => line.trim()).filter((line) => line !== "");
}

/** SSH repositories (git@host:… or ssh://) need a deploy key; HTTPS ones do not. */
export function usesSsh(repository: string): boolean {
  const trimmed = repository.trim();
  return trimmed !== "" && !trimmed.startsWith("http");
}

/** What the repository settings form edits. */
export interface GitSettingsDraft {
  repository: string;
  branch: string;
  tag: string;
  baseDirectory: string;
  composePath: string;
  privateKeyId: string;
  watchPaths: string;
  autoDeploy: boolean;
}

export function draftFrom(source: StackGitSource): GitSettingsDraft {
  return {
    repository: "",
    branch: source.branch,
    tag: source.tag,
    baseDirectory: source.base_directory,
    composePath: source.compose_path,
    privateKeyId: source.private_key_id,
    watchPaths: source.watch_paths.join("\n"),
    autoDeploy: source.auto_deploy,
  };
}

/**
 * The payload of a settings change. An empty repository keeps the stored one:
 * the address may carry a token the screen does not know.
 */
export function inputFrom(draft: GitSettingsDraft, acceptedRisks: string[]): StackGitInput {
  return {
    repository: draft.repository.trim(),
    branch: draft.branch.trim(),
    tag: draft.tag.trim(),
    base_directory: draft.baseDirectory.trim(),
    compose_path: draft.composePath.trim(),
    private_key_id: draft.privateKeyId,
    watch_paths: parseLines(draft.watchPaths),
    auto_deploy: draft.autoDeploy,
    accepted_risks: acceptedRisks,
  };
}

/** Accepting the pending risks adds them to what was accepted, without duplicates. */
export function withAcceptedRisks(source: StackGitSource): string[] {
  return [...new Set([...source.accepted_risks, ...source.pending_risks])];
}

/** The short form of a commit SHA for display. */
export function shortCommit(sha: string): string {
  return sha.slice(0, 7);
}
