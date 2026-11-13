// Pure helpers of the server-extras screens (cleanup, cloud, log drains),
// kept apart from the components so they can be tested without a DOM.
import { t } from "../../i18n";
import type { StatusView } from "../../lib/format";
import type { CleanupKind, CleanupObject, CloudMachineStatus, LogDrain } from "../../lib/types";

/** Parses "name=value" lines; blank lines are ignored. */
export function parseLabels(text: string): { labels: Record<string, string>; invalid: string[] } {
  const labels: Record<string, string> = {};
  const invalid: string[] = [];
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (!line) continue;
    const separator = line.indexOf("=");
    if (separator <= 0) {
      invalid.push(line);
      continue;
    }
    labels[line.slice(0, separator).trim()] = line.slice(separator + 1).trim();
  }
  return { labels, invalid };
}

/** Inverse of parseLabels, sorted by name. */
export function formatLabels(labels: Record<string, string>): string {
  return Object.keys(labels).sort().map((name) => `${name}=${labels[name]}`).join("\n");
}

/** Counts objects per kind, in a stable order. */
export function countByKind(objects: CleanupObject[]): Record<CleanupKind, number> {
  const counts: Record<CleanupKind, number> = { container: 0, image: 0, build_cache: 0 };
  for (const object of objects) counts[object.kind]++;
  return counts;
}

const MACHINE_STATUS: Record<CloudMachineStatus, StatusView> = {
  creating: { label: t("Criando"), tone: "info", pulse: true },
  waiting_ssh: { label: t("Aguardando SSH"), tone: "info", pulse: true },
  installing: { label: t("Instalando o Docker"), tone: "info", pulse: true },
  ready: { label: t("Pronto"), tone: "success" },
  failed: { label: t("Falhou"), tone: "danger" },
};

export function machineStatus(status: CloudMachineStatus): StatusView {
  return MACHINE_STATUS[status] ?? { label: status, tone: "neutral" };
}

/** True while provisioning still changes the machine (the page polls). */
export function machineBusy(status: CloudMachineStatus): boolean {
  return status === "creating" || status === "waiting_ssh" || status === "installing";
}

/**
 * Health of a drain from its counters: a failure newer than the last
 * delivery means the sink is failing now; drops alone mean pressure.
 */
export function drainHealth(drain: LogDrain): StatusView {
  if (!drain.enabled) return { label: t("Desligado"), tone: "neutral" };
  const { last_error_at: failedAt, last_sent_at: sentAt, dropped } = drain.status;
  if (failedAt && (!sentAt || Date.parse(failedAt) > Date.parse(sentAt))) return { label: t("Falhando"), tone: "danger" };
  if (dropped > 0) return { label: t("Com descartes"), tone: "warning" };
  return { label: t("Ativo"), tone: "success" };
}
