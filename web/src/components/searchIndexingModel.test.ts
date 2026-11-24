/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { canToggle, hostStatus, resourcePath, type IndexNowHost, type IndexNowOverview } from "./searchIndexingModel.ts";

const host = (overrides: Partial<IndexNowHost>): IndexNowHost => ({
  host: "asigas.com", eligible: true, reason: "", enabled: true, key_location: "https://asigas.com/k.txt", last: null, ...overrides,
});
const outcome = (problem: boolean) => ({
  attempted_at: "2026-11-23T10:00:00Z", submitted_at: problem ? null : "2026-11-23T10:00:00Z",
  status: problem ? 403 : 202, message: "", url_count: 2, problem,
});

Deno.test("a host says whether it is hidden, waiting, notified or in trouble", () => {
  assertEquals(hostStatus(host({ eligible: false, enabled: false })).label.length > 0, true);
  assertEquals(hostStatus(host({ enabled: false })).tone, "neutral");
  assertEquals(hostStatus(host({})).tone, "info");
  assertEquals(hostStatus(host({ last: outcome(false) })).tone, "success");
  assertEquals(hostStatus(host({ last: outcome(true) })).tone, "warning");
});

Deno.test("an unavailable resource can still turn notification off", () => {
  const overview: IndexNowOverview = { available: false, reason: "x", hosts: [], warning: "" };
  assertEquals(canToggle(overview, host({ enabled: false })), false);
  assertEquals(canToggle(overview, host({ enabled: true })), true);
  assertEquals(canToggle({ ...overview, available: true }, host({ enabled: false })), true);
});

Deno.test("a host that can never be indexed cannot be marked", () => {
  const overview: IndexNowOverview = { available: true, reason: "", hosts: [], warning: "" };
  assertEquals(canToggle(overview, host({ eligible: false, enabled: false })), false);
});

Deno.test("each row links to the tab where the resource is marked", () => {
  const base = { available: true, reason: "", hosts: [], warning: "", resource_name: "x" };
  assertEquals(resourcePath({ ...base, resource_kind: "stacks", resource_id: "s1" }), "/stacks/s1?tab=compose");
  assertEquals(resourcePath({ ...base, resource_kind: "applications", resource_id: "a1" }), "/applications/a1?tab=config");
});
