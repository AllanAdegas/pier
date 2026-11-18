/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { journalStatus, rolloutProgress, stepStatus, type RolloutJournal } from "./rolloutModel.ts";

const journal = (servers: RolloutJournal["servers"]): RolloutJournal => ({
  application_id: "app", deployment_id: "d1", image: "registry/app:1", state: "releasing", interrupted_in: "",
  automatic_recoveries: 0, message: "", started_at: "", updated_at: "", servers,
});

Deno.test("rollout steps follow the release order and count released servers", () => {
  const progress = rolloutProgress(journal([
    { server_id: "b", position: 2, previous_image: "", state: "pending", updated_at: "" },
    { server_id: "a", position: 1, previous_image: "", state: "released", updated_at: "" },
  ]));
  assertEquals(progress.steps.map((step) => step.server_id), ["a", "b"]);
  assertEquals([progress.released, progress.total], [1, 2]);
});

Deno.test("interrupted and failed rollouts never look healthy", () => {
  assertEquals(journalStatus("interrupted").tone, "warning");
  assertEquals(journalStatus("failed").tone, "danger");
  assertEquals(stepStatus("restore_failed").tone, "danger");
  assertEquals(journalStatus("something-new").label, "something-new");
});
