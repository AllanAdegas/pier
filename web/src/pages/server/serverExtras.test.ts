/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { countByKind, drainHealth, formatLabels, machineBusy, parseLabels } from "./serverExtras.ts";
import type { LogDrain } from "../../lib/types.ts";

Deno.test("labels round-trip and report malformed lines", () => {
  const parsed = parseLabels(" env = prod\n\nteam=web=1\nbroken\n=x");
  assertEquals(parsed.labels, { env: "prod", team: "web=1" });
  assertEquals(parsed.invalid, ["broken", "=x"]);
  assertEquals(formatLabels(parsed.labels), "env=prod\nteam=web=1");
});

Deno.test("cleanup objects are counted per kind", () => {
  const object = (kind: "container" | "image" | "build_cache") => ({ kind, id: "x", name: "", detail: "" });
  assertEquals(countByKind([object("image"), object("container"), object("image")]), { container: 1, image: 2, build_cache: 0 });
});

Deno.test("provisioning is busy until ready or failed", () => {
  assertEquals(["creating", "waiting_ssh", "installing"].every((status) => machineBusy(status as "creating")), true);
  assertEquals(machineBusy("ready") || machineBusy("failed"), false);
});

Deno.test("drain health prefers the newest event", () => {
  const drain = (enabled: boolean, failed: string | null, sent: string | null, dropped = 0): LogDrain => ({
    id: "d", name: "d", kind: "http", url: "https://x", has_token: false, labels: {}, scope_type: "project", scope_id: "p",
    enabled, created_at: "", updated_at: "",
    status: { buffered: 0, capacity: 5000, sent: 0, dropped, failed_batches: 0, last_error: "", last_error_at: failed, last_sent_at: sent },
  });
  assertEquals(drainHealth(drain(false, "2026-01-02", null)).tone, "neutral");
  assertEquals(drainHealth(drain(true, "2026-01-02T00:00:00Z", "2026-01-01T00:00:00Z")).tone, "danger");
  assertEquals(drainHealth(drain(true, "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z", 3)).tone, "warning");
  assertEquals(drainHealth(drain(true, null, "2026-01-02T00:00:00Z")).tone, "success");
});
