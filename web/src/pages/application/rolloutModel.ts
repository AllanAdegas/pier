import type { StatusView } from "../../lib/format";
import { t } from "../../i18n";

/** Durable record of the last multi-server rollout (GET /applications/{id}/placement/rollout). */
export interface RolloutJournal {
  application_id: string;
  deployment_id: string;
  image: string;
  state: string;
  interrupted_in: string;
  automatic_recoveries: number;
  message: string;
  started_at: string;
  updated_at: string;
  servers: RolloutStep[];
}

export interface RolloutStep {
  server_id: string;
  position: number;
  previous_image: string;
  state: string;
  updated_at: string;
}

/** Status of the whole rollout; unknown states are shown as they come. */
export function journalStatus(state: string): StatusView {
  const known: Record<string, StatusView> = {
    preparing: { label: t("Preparando"), tone: "info", pulse: true },
    releasing: { label: t("Liberando nos servidores"), tone: "info", pulse: true },
    compensating: { label: t("Revertendo servidores"), tone: "warning", pulse: true },
    interrupted: { label: t("Interrompido pela reinicialização"), tone: "warning" },
    completed: { label: t("Concluído"), tone: "success" },
    failed: { label: t("Falhou"), tone: "danger" },
    rolled_back: { label: t("Revertido para a versão anterior"), tone: "warning" },
  };
  return known[state] ?? { label: state, tone: "neutral" };
}

/** Status of one server inside the rollout. */
export function stepStatus(state: string): StatusView {
  const known: Record<string, StatusView> = {
    pending: { label: t("Aguardando"), tone: "neutral" },
    releasing: { label: t("Implantando"), tone: "info", pulse: true },
    released: { label: t("Nova versão no ar"), tone: "success" },
    failed: { label: t("Falhou"), tone: "danger" },
    restored: { label: t("Versão anterior restaurada"), tone: "warning" },
    restore_failed: { label: t("Restauração falhou"), tone: "danger" },
  };
  return known[state] ?? { label: state, tone: "neutral" };
}

/** Servers in release order with how many already run the new image. */
export function rolloutProgress(journal: RolloutJournal): { steps: RolloutStep[]; released: number; total: number } {
  const steps = [...journal.servers].sort((a, b) => a.position - b.position);
  return { steps, released: steps.filter((step) => step.state === "released").length, total: steps.length };
}
