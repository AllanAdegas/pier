/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { canToggle, hostStatus, type IndexNowHost, type IndexNowOverview } from "./searchIndexingModel.ts";

const host = (overrides: Partial<IndexNowHost>): IndexNowHost => ({
  host: "asigas.com", enabled: true, key_location: "https://asigas.com/k.txt", last: null, ...overrides,
});
const outcome = (problem: boolean) => ({
  attempted_at: "2026-11-23T10:00:00Z", submitted_at: problem ? null : "2026-11-23T10:00:00Z",
  status: problem ? 403 : 202, message: "", url_count: 2, problem,
});

Deno.test("a host says whether it is off, waiting, notified or in trouble", () => {
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
