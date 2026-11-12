import { TRIGGER_LABEL } from "../../lib/format";
import type { Application, Project } from "../../lib/types";
import { t } from "../../i18n";

const ENGINE_TRIGGER_LABEL: Record<string, string> = {
  archive: t("Envio de código"),
  promotion: t("Promoção"),
};

/** Label of a deployment trigger, including the ones added after the shared map. */
export function triggerLabel(trigger: string): string {
  return ENGINE_TRIGGER_LABEL[trigger] ?? TRIGGER_LABEL[trigger] ?? trigger;
}

export interface PromotionTarget {
  application: Application;
  environmentName: string;
  sameServer: boolean;
}

/**
 * Applications of the other environments of the same project, the ones with
 * the same name first: promotion usually goes staging/api → production/api.
 * Targets on another server need a registry and are listed last.
 */
export function promotionTargets(source: Application, projects: Project[], applications: Application[]): PromotionTarget[] {
  const project = projects.find((candidate) => candidate.environments.some((environment) => environment.id === source.environment_id));
  if (!project) return [];
  const environments = new Map(
    project.environments.filter((environment) => environment.id !== source.environment_id).map((environment) => [environment.id, environment.name]),
  );
  const rank = (target: PromotionTarget) => (target.sameServer ? 0 : 2) + (target.application.name === source.name ? 0 : 1);
  return applications
    .filter((application) => application.id !== source.id && environments.has(application.environment_id))
    .map((application) => ({
      application,
      environmentName: environments.get(application.environment_id) ?? "",
      sameServer: application.server_id === source.server_id,
    }))
    .sort((a, b) => rank(a) - rank(b) || a.environmentName.localeCompare(b.environmentName) || a.application.name.localeCompare(b.application.name));
}
