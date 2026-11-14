/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { feedbackInput, githubCloneURL, manifestCallback, needsToken, pickStillApplies, suggestedProvider } from "./gitLinks.ts";
import type { GitFeedbackSettings } from "../../lib/types.ts";

Deno.test("a picked repository is cloned over HTTPS from github.com", () => {
  assertEquals(githubCloneURL("acme/app"), "https://github.com/acme/app.git");
});

Deno.test("the pick only applies while the typed address is the picked repository", () => {
  const pick = { installation_id: 1, repository: "Acme/App" };
  assertEquals(pickStillApplies(pick, "https://github.com/acme/app"), true);
  assertEquals(pickStillApplies(pick, " https://github.com/Acme/App.git "), true);
  assertEquals(pickStillApplies(pick, "https://github.com/acme/other.git"), false);
  assertEquals(pickStillApplies(null, "https://github.com/acme/app.git"), false);
});

Deno.test("the manifest callback needs both code and state", () => {
  assertEquals(manifestCallback("?code=abc&state=xyz"), { code: "abc", state: "xyz" });
  assertEquals(manifestCallback("?code=abc"), null);
  assertEquals(manifestCallback(""), null);
});

Deno.test("only token providers send a token, and an empty one keeps the stored token", () => {
  const settings: GitFeedbackSettings = {
    provider: "gitlab", commit_status: true, pull_request_comments: true, token_set: true, saved: true, token: "old",
  };
  assertEquals(feedbackInput(settings, "  ").token, undefined);
  assertEquals(feedbackInput(settings, " new ").token, "new");
  assertEquals(feedbackInput({ ...settings, provider: "github" }, "new").token, undefined);
  assertEquals(needsToken("bitbucket"), true);
  assertEquals(needsToken("none"), false);
});

Deno.test("the provider is suggested from the repository host", () => {
  assertEquals(suggestedProvider("https://github.com/a/b.git", true), "github");
  assertEquals(suggestedProvider("https://github.com/a/b.git", false), "none");
  assertEquals(suggestedProvider("git@bitbucket.org:ws/app.git", false), "bitbucket");
  assertEquals(suggestedProvider("https://gitlab.com/g/app.git", false), "gitlab");
  assertEquals(suggestedProvider("https://codeberg.org/a/b.git", false), "gitea");
});

Deno.test("provider suggestions use exact parsed hosts, not URL substrings", () => {
  assertEquals(suggestedProvider("ssh://git@BITBUCKET.ORG/ws/app.git", false), "bitbucket");
  assertEquals(suggestedProvider("git@gitlab.com:g/app.git", false), "gitlab");
  assertEquals(suggestedProvider("git@codeberg.org:a/b.git", false), "gitea");
  for (const host of ["github.com", "bitbucket.org", "codeberg.org", "gitlab.com"]) {
    for (const address of [
      `https://${host}.evil.test/a/b.git`,
      `https://not${host}/a/b.git`,
      `https://${host}@evil.test/a/b.git`,
      `https://evil.test/${host}/a.git`,
      `https://evil.test/a.git?q=${host}#${host}`,
      `git@${host}.evil.test:a/b.git`,
    ]) assertEquals(suggestedProvider(address, false), "none", address);
  }
  for (const address of ["https://gitlab.example.com/g/a.git", "https://gitea.example.com/a.git", "", "not a repository", "https://", "file:///bitbucket.org/a.git"]) {
    assertEquals(suggestedProvider(address, false), "none", address);
  }
});

Deno.test("connection picks use the chosen clone URL and stop applying after source edits", () => {
  const pick = {installation_id: 0, connection_id: "forge", repository: "group/repo", clone_url: "https://forge.example/group/repo.git"};
  assertEquals(pickStillApplies(pick, "https://forge.example/group/repo.git"), true);
  assertEquals(pickStillApplies(pick, "https://other.example/group/repo.git"), false);
});
