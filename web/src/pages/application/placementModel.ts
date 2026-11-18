import { t } from "../../i18n";
import type { StatusView } from "../../lib/format";

// Placement of an application: where it is built and which servers run it.

export interface Placement {
  swarm_cluster_id?: string;
  build_server_id: string;
  registry_id: string;
  repository: string;
  destinations: string[];
}

export interface Registry {
  id: string;
  name: string;
  host: string;
  username: string;
  created_at: string;
}

export interface RegistryInput {
  name: string;
  host: string;
  username: string;
  password: string;
}

export interface Rollout {
  server_id: string;
  deployment_id: string;
  image: string;
  previous_image: string;
  status: string;
  message: string;
  updated_at: string;
}

export interface PlacementContainer {
  name: string;
  state: string;
  status: string;
  cpu_percent: string;
  memory_usage: string;
  memory_percent: string;
  network_io: string;
}

export interface PlacementServer {
  server_id: string;
  name: string;
  role: "primary" | "destination" | "leaving";
  reachable: boolean;
  rollout: Rollout | null;
  containers: PlacementContainer[];
}

/** Hosts offered when adding a registry; any OCI registry host works. */
export const REGISTRY_PRESETS = [
  { label: "Docker Hub", host: "docker.io" },
  { label: "GitHub (GHCR)", host: "ghcr.io" },
  { label: "GitLab", host: "registry.gitlab.com" },
];

const ROLLOUT_STATUS: Record<string, StatusView> = {
  deploying: { label: t("Implantando"), tone: "info", pulse: true },
  healthy: { label: t("Versão atual no ar"), tone: "success" },
  failed: { label: t("Falhou; versão anterior mantida"), tone: "danger" },
  rolled_back: { label: t("Revertido para a versão anterior"), tone: "warning" },
  rollback_failed: { label: t("Reversão falhou"), tone: "danger" },
  interrupted: { label: t("Interrompido pela reinicialização"), tone: "warning" },
};

export function rolloutStatus(rollout: Rollout | null): StatusView {
  if (!rollout) {
    return { label: t("Ainda sem deploy aqui"), tone: "neutral" };
  }
  return ROLLOUT_STATUS[rollout.status] ?? { label: rollout.status, tone: "neutral" };
}

export function roleLabel(role: PlacementServer["role"]): string {
  if (role === "primary") return t("Servidor da aplicação");
  if (role === "leaving") return t("Saindo no próximo deploy");
  return t("Destino");
}

/** Toggles a destination; the application's own server always stays. */
export function toggleDestination(placement: Placement, primary: string, serverId: string, on: boolean): Placement {
  const others = placement.destinations.filter((id) => id !== primary && id !== serverId);
  return { ...placement, destinations: [primary, ...others, ...(on && serverId !== primary ? [serverId] : [])] };
}

/** A build leaves its server when another server builds or runs it. */
export function needsRegistry(placement: Placement, primary: string, imagePack: boolean): boolean {
  if (imagePack) return false;
  if (placement.swarm_cluster_id) return true;
  const build = placement.build_server_id || primary;
  return placement.destinations.some((id) => id !== build) || build !== primary;
}

/** host/repo@sha256:0123456789ab… → host/repo@0123456789ab */
export function shortImage(image: string): string {
  const [name, digest] = image.split("@sha256:");
  return digest ? `${name}@${digest.slice(0, 12)}` : image;
}
