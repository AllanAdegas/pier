/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { draftFrom, inputFrom, parseLines, shortCommit, usesSsh, withAcceptedRisks } from "./gitSource.ts";
import type { StackGitSource } from "../../lib/types.ts";

const source: StackGitSource = {
  repository: "https://***@github.com/a/b.git",
  branch: "main",
  tag: "",
  base_directory: "deploy",
  compose_path: "compose.yaml",
  private_key_id: "",
  watch_paths: ["deploy/**", "compose.yaml"],
  auto_deploy: true,
  accepted_risks: ["host-ports"],
  pending_risks: ["privileged", "host-ports"],
  last_ref: "main",
  last_commit: "0123456789abcdef",
  last_commit_message: "add shop",
  last_deployed_at: null,
};

Deno.test("watch paths are one glob per line and blanks are dropped", () => {
  assertEquals(parseLines(" deploy/** \n\n compose.yaml\n"), ["deploy/**", "compose.yaml"]);
});

Deno.test("only non-HTTP repositories need a deploy key", () => {
  assertEquals(usesSsh("git@github.com:a/b.git"), true);
  assertEquals(usesSsh("ssh://git@host/a/b.git"), true);
  assertEquals(usesSsh("https://github.com/a/b.git"), false);
  assertEquals(usesSsh(""), false);
});

Deno.test("the draft never carries the stored address, so a token is not overwritten by the masked one", () => {
  const draft = draftFrom(source);
  assertEquals(draft.repository, "");
  assertEquals(inputFrom(draft, []).repository, "");
  assertEquals(inputFrom(draft, []).watch_paths, ["deploy/**", "compose.yaml"]);
});

Deno.test("accepting pending risks keeps the earlier ones once", () => {
  assertEquals(withAcceptedRisks(source), ["host-ports", "privileged"]);
});

Deno.test("commits are shown short", () => {
  assertEquals(shortCommit(source.last_commit), "0123456");
});
