// Pure rules of the update banner, kept apart from the component so they can
// be tested without a DOM.

import type { UpdateStatus } from "../lib/types";

export const DISMISSED_KEY = "pier.update.dismissed";

const STABLE_VERSION = /^\d{1,9}\.\d{1,9}\.\d{1,9}$/;

/** The banner shows for a newer, well-formed version the user has not dismissed. */
export function shouldShowBanner(status: UpdateStatus | undefined, dismissedVersion: string | null): boolean {
  if (!status?.enabled || !status.update_available || !status.latest) {
    return false;
  }
  return STABLE_VERSION.test(status.latest) && status.latest !== dismissedVersion;
}

/**
 * The commands that upgrade an installation made by install.sh. The version
 * comes from the network, so anything but MAJOR.MINOR.PATCH yields no commands
 * instead of text a person would paste into a shell.
 */
export function upgradeCommands(latest: string): string | null {
  if (!STABLE_VERSION.test(latest)) {
    return null;
  }
  return [`git fetch --depth 1 origin tag v${latest}`, `git checkout v${latest}`, "./install.sh"].join("\n");
}

/** Release page of the banner: only https links to GitHub are rendered. */
export function safeReleaseUrl(url: string | undefined): string | null {
  return url !== undefined && url.startsWith("https://github.com/AllanAdegas/pier/") ? url : null;
}

export function readDismissed(storage: Pick<Storage, "getItem">): string | null {
  try {
    return storage.getItem(DISMISSED_KEY);
  } catch {
    return null;
  }
}

export function saveDismissed(storage: Pick<Storage, "setItem">, version: string): void {
  try {
    storage.setItem(DISMISSED_KEY, version);
  } catch {
    // Private mode or blocked storage: the banner just comes back next load.
  }
}
