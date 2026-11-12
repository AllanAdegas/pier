/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import type { UpdateStatus } from "../lib/types.ts";
import { readDismissed, safeReleaseUrl, saveDismissed, shouldShowBanner, upgradeCommands } from "./updateRules.ts";

const available: UpdateStatus = {
  enabled: true,
  current: "0.5.0",
  latest: "0.6.0",
  update_available: true,
  url: "https://github.com/AllanAdegas/pier/releases/tag/v0.6.0",
  checked_at: "2026-11-11T12:00:00Z",
};

Deno.test("the banner shows only for a newer version that was not dismissed", () => {
  assertEquals(shouldShowBanner(available, null), true);
  assertEquals(shouldShowBanner(available, "0.6.0"), false);
  assertEquals(shouldShowBanner(available, "0.5.0"), true);
  assertEquals(shouldShowBanner({ ...available, latest: "0.7.0" }, "0.6.0"), true);
  assertEquals(shouldShowBanner({ ...available, update_available: false }, null), false);
  assertEquals(shouldShowBanner({ ...available, enabled: false }, null), false);
  assertEquals(shouldShowBanner(undefined, null), false);
});

Deno.test("a malformed version never reaches the banner or the commands", () => {
  assertEquals(shouldShowBanner({ ...available, latest: "0.6.0; rm -rf /" }, null), false);
  assertEquals(upgradeCommands("0.6.0; rm -rf /"), null);
  assertEquals(upgradeCommands("v0.6.0"), null);
});

Deno.test("the upgrade commands pin the tag and run the installer", () => {
  assertEquals(
    upgradeCommands("0.6.0"),
    "git fetch --depth 1 origin tag v0.6.0\ngit checkout v0.6.0\n./install.sh",
  );
});

Deno.test("only GitHub links of the Pier repository are linked", () => {
  assertEquals(safeReleaseUrl(available.url), available.url);
  assertEquals(safeReleaseUrl("javascript:alert(1)"), null);
  assertEquals(safeReleaseUrl("https://evil.example/AllanAdegas/pier/"), null);
  assertEquals(safeReleaseUrl(undefined), null);
});

Deno.test("dismissal survives a broken storage", () => {
  const memory = new Map<string, string>();
  const storage = { getItem: (k: string) => memory.get(k) ?? null, setItem: (k: string, v: string) => void memory.set(k, v) };
  saveDismissed(storage, "0.6.0");
  assertEquals(readDismissed(storage), "0.6.0");
  const broken = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } };
  saveDismissed(broken, "0.6.0");
  assertEquals(readDismissed(broken), null);
});
