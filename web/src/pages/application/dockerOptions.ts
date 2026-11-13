import type { DockerOptions, Ulimit } from "../../lib/types";

/** Capabilities the server accepts without the dangerous-options switch. */
export const SAFE_CAPABILITIES = [
  "AUDIT_WRITE", "CHOWN", "DAC_OVERRIDE", "FOWNER", "FSETID", "IPC_LOCK", "KILL", "MKNOD", "NET_ADMIN",
  "NET_BIND_SERVICE", "NET_BROADCAST", "NET_RAW", "SETFCAP", "SETGID", "SETPCAP", "SETUID", "SYS_CHROOT", "SYS_NICE",
  "WAKE_ALARM",
];

export function emptyDockerOptions(): DockerOptions {
  return {
    extra_hosts: [], cap_add: [], cap_drop: [], ulimits: [], shm_size: "", labels: {}, devices: [],
    privileged: false, network_mode: "", pid_mode: "",
  };
}

export function withDefaults(options: DockerOptions | undefined): DockerOptions {
  return { ...emptyDockerOptions(), ...options, labels: options?.labels ?? {} };
}

/** "nofile=1024:4096" (or "memlock=-1") lines; invalid lines are kept for the server to report. */
export function parseUlimits(lines: string[]): Ulimit[] {
  return lines.map((line) => {
    const [name, limits = ""] = line.split("=", 2);
    const [soft, hard = soft] = limits.split(":", 2);
    return { name: name.trim(), soft: Number(soft), hard: Number(hard) };
  });
}

export function formatUlimits(ulimits: Ulimit[] | null | undefined): string[] {
  return (ulimits ?? []).map((ulimit) => `${ulimit.name}=${ulimit.soft}:${ulimit.hard}`);
}

/** "key=value" lines; a line without "=" becomes a label with an empty value. */
export function parseLabels(lines: string[]): Record<string, string> {
  const labels: Record<string, string> = {};
  for (const line of lines) {
    const index = line.indexOf("=");
    const key = (index < 0 ? line : line.slice(0, index)).trim();
    if (key) {
      labels[key] = index < 0 ? "" : line.slice(index + 1);
    }
  }
  return labels;
}

export function formatLabels(labels: Record<string, string> | null | undefined): string[] {
  return Object.entries(labels ?? {}).sort(([a], [b]) => a.localeCompare(b)).map(([key, value]) => `${key}=${value}`);
}

/** Options that need the owner's "allow dangerous options" switch. */
export function dangerousOptions(options: DockerOptions): string[] {
  const found: string[] = [];
  if (options.privileged) found.push("privileged");
  if (options.network_mode === "host") found.push("network_mode: host");
  if (options.pid_mode === "host") found.push("pid: host");
  for (const capability of options.cap_add ?? []) {
    const name = capability.toUpperCase().replace(/^CAP_/, "");
    if (!SAFE_CAPABILITIES.includes(name)) found.push(`cap_add: ${name}`);
  }
  return found;
}
