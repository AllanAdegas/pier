import type { StatusView } from "../lib/format";
import { t } from "../i18n";

/** Outcome of the last IndexNow submission of a host. */
export interface IndexNowOutcome {
  attempted_at: string | null;
  submitted_at: string | null;
  status: number;
  message: string;
  url_count: number;
  problem: boolean;
}

/** One https host of a resource (GET .../indexnow). */
export interface IndexNowHost {
  host: string;
  enabled: boolean;
  key_location: string;
  last: IndexNowOutcome | null;
}

/** Search engine notification of an application or stack. */
export interface IndexNowOverview {
  available: boolean;
  reason: string;
  hosts: IndexNowHost[];
  warning: string;
}

/** What the badge of a host says: off, waiting for a deploy, accepted or a problem. */
export function hostStatus(host: IndexNowHost): StatusView {
  if (!host.enabled) return { label: t("Desligado"), tone: "neutral" };
  if (!host.last?.attempted_at) return { label: t("Aguardando o próximo deploy"), tone: "info" };
  if (host.last.problem) return { label: t("Com problema"), tone: "warning" };
  return { label: t("Avisado"), tone: "success" };
}

/** The switch only changes hosts that can serve keys, or turns one off. */
export function canToggle(overview: IndexNowOverview, host: IndexNowHost): boolean {
  return overview.available || host.enabled;
}
