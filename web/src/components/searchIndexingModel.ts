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
  /** False for a host that always answers noindex; `reason` says why. */
  eligible: boolean;
  reason: string;
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

/** One resource in the instance-wide list (GET /indexnow/sites). */
export interface IndexNowResource extends IndexNowOverview {
  resource_kind: "applications" | "stacks";
  resource_id: string;
  resource_name: string;
}

/** What the badge of a host says: hidden, waiting for a deploy, notified or a problem. */
export function hostStatus(host: IndexNowHost): StatusView {
  if (!host.eligible) return { label: t("Sempre fora dos buscadores"), tone: "neutral" };
  if (!host.enabled) return { label: t("Fora dos buscadores"), tone: "neutral" };
  if (!host.last?.attempted_at) return { label: t("Aguardando o próximo deploy"), tone: "info" };
  if (host.last.problem) return { label: t("Com problema"), tone: "warning" };
  return { label: t("Avisado"), tone: "success" };
}

/** The switch only marks eligible hosts of a resource that can serve keys, and can always unmark. */
export function canToggle(overview: IndexNowOverview, host: IndexNowHost): boolean {
  return host.eligible && (overview.available || host.enabled);
}

/** Where the resource of a row lives in the panel. */
export function resourcePath(resource: IndexNowResource): string {
  const tab = resource.resource_kind === "stacks" ? "compose" : "config";
  return `/${resource.resource_kind}/${resource.resource_id}?tab=${tab}`;
}
