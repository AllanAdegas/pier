/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { dangerousOptions, emptyDockerOptions, formatLabels, formatUlimits, parseLabels, parseUlimits } from "./dockerOptions.ts";

Deno.test("ulimits round-trip and a single value sets both limits", () => {
  const parsed = parseUlimits(["nofile=1024:4096", "memlock=-1"]);
  assertEquals(parsed, [{ name: "nofile", soft: 1024, hard: 4096 }, { name: "memlock", soft: -1, hard: -1 }]);
  assertEquals(formatUlimits(parsed), ["nofile=1024:4096", "memlock=-1:-1"]);
});

Deno.test("labels keep values with = and drop empty keys", () => {
  assertEquals(parseLabels(["team=web", "url=a=b", "=x", "flag"]), { team: "web", url: "a=b", flag: "" });
  assertEquals(formatLabels({ b: "2", a: "1" }), ["a=1", "b=2"]);
});

Deno.test("dangerous options are flagged like the server does", () => {
  assertEquals(dangerousOptions(emptyDockerOptions()), []);
  const options = { ...emptyDockerOptions(), privileged: true, network_mode: "host", pid_mode: "host", cap_add: ["cap_sys_admin", "NET_ADMIN"] };
  assertEquals(dangerousOptions(options), ["privileged", "network_mode: host", "pid: host", "cap_add: SYS_ADMIN"]);
});
