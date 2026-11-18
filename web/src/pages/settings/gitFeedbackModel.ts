import type { StatusView } from "../../lib/format";
import { t } from "../../i18n";

/** A report Pier could not deliver to the Git provider yet (GET .../git-provider/reports). */
export interface FailedReport {
  id: number;
  operation: "run" | "comment" | string;
  state: "pending" | "dead" | string;
  sha?: string;
  context?: string;
  pull_request?: number;
  phase?: string;
  attempts: number;
  last_error: string;
  next_attempt_at: string;
  updated_at: string;
}

/** Retrying reports are still on their way; dead ones need a manual resend. */
export function reportStatus(report: FailedReport): StatusView {
  if (report.state === "dead") {
    return { label: t("Desistiu após {attempts} tentativas", { attempts: report.attempts }), tone: "danger" };
  }
  return { label: t("Tentando de novo ({attempts})", { attempts: report.attempts }), tone: "warning", pulse: true };
}

/** What the report was about: a commit status or a pull request comment. */
export function reportSubject(report: FailedReport): string {
  if (report.operation === "comment" && report.pull_request) {
    return t("Comentário no pull request #{number}", { number: report.pull_request });
  }
  const commit = report.sha ? report.sha.slice(0, 7) : "—";
  return report.context ? `${t("Status do commit {sha}", { sha: commit })} · ${report.context}` : t("Status do commit {sha}", { sha: commit });
}
