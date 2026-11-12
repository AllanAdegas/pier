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
  assertEquals(suggestedProvider("https://gitlab.example.com/g/app.git", false), "gitlab");
  assertEquals(suggestedProvider("https://codeberg.org/a/b.git", false), "gitea");
});
