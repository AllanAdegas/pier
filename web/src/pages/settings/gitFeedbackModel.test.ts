/// <reference lib="deno.ns" />
import { assert, assertEquals } from "jsr:@std/assert@1";
import { reportStatus, reportSubject, type FailedReport } from "./gitFeedbackModel.ts";

const report = (overrides: Partial<FailedReport>): FailedReport => ({
  id: 1, operation: "run", state: "pending", attempts: 2, last_error: "", next_attempt_at: "", updated_at: "", ...overrides,
});

Deno.test("dead reports ask for attention and retrying ones pulse", () => {
  assertEquals(reportStatus(report({ state: "dead", attempts: 8 })).tone, "danger");
  assertEquals(reportStatus(report({})).pulse, true);
});

Deno.test("a report names its commit or pull request", () => {
  const status = reportSubject(report({ sha: "0123456789abcdef", context: "pier/deploy" }));
  assert(status.includes("0123456 ") && status.endsWith(" · pier/deploy") && !status.includes("0123456789"), status);
  assert(reportSubject(report({ operation: "comment", pull_request: 42 })).endsWith("#42"));
});
