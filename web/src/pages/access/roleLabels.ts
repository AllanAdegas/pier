import { t } from "../../i18n";
import type { Role } from "./can";

/** Display name of each access role; read at render time so the language can change. */
export function roleLabels(): Record<Role, string> {
  return { owner: t("Proprietário"), admin: t("Administrador"), developer: t("Desenvolvedor"), viewer: t("Somente leitura") };
}
