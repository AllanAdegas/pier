/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { effectiveProcesses, newProcess, withProcessNames } from "./processes.ts";
import type { Container } from "../../lib/types.ts";

Deno.test("no processes means a single web process", () => {
  assertEquals(effectiveProcesses(null).map((process) => [process.name, process.replicas]), [["web", 1]]);
  assertEquals(effectiveProcesses([]).length, 1);
});

Deno.test("new workers get a free name", () => {
  const web = effectiveProcesses(null);
  assertEquals(newProcess(web).name, "worker");
  assertEquals(newProcess([...web, newProcess(web)]).name, "worker-2");
});

Deno.test("container names show their process", () => {
  const containers = [{ id: "a", name: "app-1", process: "worker" }, { id: "b", name: "app-2" }] as Container[];
  assertEquals(withProcessNames(containers).map((container) => container.name), ["app-1 (worker)", "app-2"]);
});
