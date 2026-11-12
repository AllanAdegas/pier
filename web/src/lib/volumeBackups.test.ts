/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import type { VolumeMount } from "./types.ts";
import { exclusionReason, hasExternalMounts, includedMounts, restoreConfirmed, volumeBasePath } from "./volumeBackups.ts";

const mount = (overrides: Partial<VolumeMount>): VolumeMount => ({
  label: "data",
  kind: "volume",
  source: "data",
  destination: "/data",
  included: true,
  ...overrides,
});

Deno.test("only the exact resource name confirms a restore", () => {
  assertEquals(restoreConfirmed("My App", "My App"), true);
  assertEquals(restoreConfirmed("my app", "My App"), false);
  assertEquals(restoreConfirmed("", ""), false);
});

Deno.test("excluded mounts explain themselves and included ones do not", () => {
  assertEquals(exclusionReason(mount({})), "");
  assertEquals(exclusionReason(mount({ included: false, reason: "outside_data_dir" })).length > 0, true);
  assertEquals(exclusionReason(mount({ included: false, reason: "system_path" })).length > 0, true);
});

Deno.test("external mounts are detected and filtered", () => {
  const mounts = [mount({}), mount({ included: false, reason: "outside_data_dir" }), mount({ included: false, reason: "system_path" })];
  assertEquals(hasExternalMounts(mounts), true);
  assertEquals(hasExternalMounts([mount({})]), false);
  assertEquals(includedMounts(mounts).length, 1);
});

Deno.test("the API path encodes the resource id", () => {
  assertEquals(volumeBasePath("stacks", "abc"), "/stacks/abc");
  assertEquals(volumeBasePath("applications", "a/b"), "/applications/a%2Fb");
});
