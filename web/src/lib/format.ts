import { locale, t } from "../i18n";
// Presentation helpers: dates, sizes and the labels of statuses.


export type Tone = "neutral" | "success" | "warning" | "danger" | "info";

export interface StatusView {
  label: string;
  tone: Tone;
  pulse?: boolean;
}

const RESOURCE_STATUS: Record<string, StatusView> = {
  running: { label: t("Em execução"), tone: "success" },
  starting: { label: t("Iniciando"), tone: "info", pulse: true },
  unhealthy: { label: t("Não saudável"), tone: "danger" },
  restarting: { label: t("Reiniciando em loop"), tone: "danger", pulse: true },
  degraded: { label: t("Parcialmente no ar"), tone: "warning" },
  stopped: { label: t("Parado"), tone: "neutral" },
};

const DEPLOYMENT_STATUS: Record<string, StatusView> = {
  queued: { label: t("Na fila"), tone: "info", pulse: true },
  in_progress: { label: t("Em andamento"), tone: "info", pulse: true },
  finished: { label: t("Concluído"), tone: "success" },
  failed: { label: t("Falhou"), tone: "danger" },
  cancelled: { label: t("Cancelado"), tone: "neutral" },
};

const SERVER_STATUS: Record<string, StatusView> = {
  reachable: { label: t("Acessível"), tone: "success" },
  unreachable: { label: t("Inacessível"), tone: "danger" },
  unknown: { label: t("Não verificado"), tone: "neutral" },
};

const BACKUP_STATUS: Record<string, StatusView> = {
  running: { label: t("Em andamento"), tone: "info", pulse: true },
  succeeded: { label: t("Concluído"), tone: "success" },
  failed: { label: t("Falhou"), tone: "danger" },
};

const CLUSTER_STATUS: Record<string, StatusView> = {
  ready: { label: t("Pronto"), tone: "success" },
  provisioning: { label: t("Criando"), tone: "info", pulse: true },
  unreachable: { label: t("Inacessível"), tone: "danger" },
  unknown: { label: t("Não verificado"), tone: "neutral" },
};

const KUBE_HEALTH: Record<string, Tone> = { ok: "success", progressing: "info", error: "danger" };

/** Status of a Kubernetes object row, toned by the health the backend computed. */
export const kubeStatus = (row: { status: string; health: string }): StatusView => ({
  label: row.status || "—",
  tone: KUBE_HEALTH[row.health] ?? "neutral",
  pulse: row.health === "progressing",
});

/** Status of a plain Docker container, from its state and status text. */
export function containerStatus(container: { state: string; status: string }): StatusView {
  if (container.state !== "running") {
    return { label: container.status || container.state, tone: container.state === "restarting" ? "danger" : "neutral" };
  }
  if (container.status.includes("unhealthy")) {
    return { label: container.status, tone: "danger" };
  }
  return { label: container.status, tone: "success" };
}

const unknownStatus = (status: string): StatusView => ({ label: status || t("Desconhecido"), tone: "neutral" });

export const resourceStatus = (status: string) => RESOURCE_STATUS[status] ?? unknownStatus(status);
export const deploymentStatus = (status: string) => DEPLOYMENT_STATUS[status] ?? unknownStatus(status);
export const serverStatus = (status: string) => SERVER_STATUS[status] ?? unknownStatus(status);
export const backupStatus = (status: string) => BACKUP_STATUS[status] ?? unknownStatus(status);
export const clusterStatus = (status: string) => CLUSTER_STATUS[status] ?? unknownStatus(status);

export const TRIGGER_LABEL: Record<string, string> = {
  manual: "Manual",
  webhook: "Webhook",
  api: "API",
  rollback: "Rollback",
  scheduled: t("Agendado"),
  archive: t("Pasta local (pierctl up)"),
  promotion: t("Promoção"),
};

export const BUILD_PACK_LABEL: Record<string, string> = {
  dockerfile: "Dockerfile",
  nixpacks: "Nixpacks",
  railpack: "Railpack",
  static: t("Site estático"),
  image: t("Imagem Docker"),
};

const dateTime = new Intl.DateTimeFormat(locale, { dateStyle: "short", timeStyle: "short" });
const relative = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });

export function formatDateTime(value: string | null | undefined): string {
  if (!value) {
    return "—";
  }
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "—" : dateTime.format(date);
}

/** "há 5 minutos", falling back to the absolute date for old values. */
export function formatRelative(value: string | null | undefined): string {
  if (!value) {
    return "—";
  }
  const seconds = Math.round((new Date(value).getTime() - Date.now()) / 1000);
  const absolute = Math.abs(seconds);
  if (absolute < 60) {
    return relative.format(seconds, "second");
  }
  if (absolute < 3600) {
    return relative.format(Math.round(seconds / 60), "minute");
  }
  if (absolute < 86400) {
    return relative.format(Math.round(seconds / 3600), "hour");
  }
  if (absolute < 86400 * 14) {
    return relative.format(Math.round(seconds / 86400), "day");
  }
  return formatDateTime(value);
}

export function formatDuration(start: string | null, end: string | null): string {
  if (!start) {
    return "—";
  }
  const finish = end ? new Date(end).getTime() : Date.now();
  const seconds = Math.max(0, Math.round((finish - new Date(start).getTime()) / 1000));
  if (seconds < 60) {
    return `${seconds}s`;
  }
  return `${Math.floor(seconds / 60)}min ${seconds % 60}s`;
}

export function formatBytes(bytes: number): string {
  if (!bytes) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** exponent;
  return `${value.toFixed(value >= 10 || exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}

export function shortSha(sha: string): string {
  return sha ? sha.slice(0, 8) : "";
}
