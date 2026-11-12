// Pure rules of the volume backup screens, kept apart from the components so
// they can be tested without a DOM.

import { t } from "../i18n";
import type { VolumeMount, VolumeResourceKind } from "./types";

/** Explains, in the user's language, why a mount is not in the archive. */
export function exclusionReason(mount: VolumeMount): string {
  switch (mount.reason) {
    case "outside_data_dir":
      return t("Diretório fora dos dados do Pier: não entra no backup, a menos que você inclua os diretórios externos.");
    case "system_path":
      return t("Caminho do sistema (socket, /etc, /proc…): nunca entra no backup.");
    case "invalid_name":
      return t("Nome de volume não suportado: não entra no backup.");
    default:
      return "";
  }
}

/** Mounts the policy would leave out only because they are external. */
export function hasExternalMounts(mounts: VolumeMount[]): boolean {
  return mounts.some((mount) => mount.reason === "outside_data_dir");
}

/** The restore button enables only with the exact resource name typed. */
export function restoreConfirmed(typed: string, resourceName: string): boolean {
  return resourceName !== "" && typed === resourceName;
}

/** Path prefix of the API for a resource kind. */
export function volumeBasePath(kind: VolumeResourceKind, id: string): string {
  return `/${kind}/${encodeURIComponent(id)}`;
}

/** Mounts that will go into the next archive. */
export function includedMounts(mounts: VolumeMount[]): VolumeMount[] {
  return mounts.filter((mount) => mount.included);
}
