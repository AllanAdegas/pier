import type { Container, Process } from "../../lib/types";

export const WEB_PROCESS = "web";

/** The processes an application runs: none configured means a single web. */
export function effectiveProcesses(processes: Process[] | null | undefined): Process[] {
  return processes && processes.length > 0
    ? processes
    : [{ name: WEB_PROCESS, command: "", replicas: 1, limits: { cpus: "", memory: "" } }];
}

/** A new worker, named so it does not clash with the existing ones. */
export function newProcess(existing: Process[]): Process {
  const names = new Set(existing.map((process) => process.name));
  let name = "worker";
  for (let number = 2; names.has(name); number++) name = `worker-${number}`;
  return { name, command: "", replicas: 1, limits: { cpus: "", memory: "" } };
}

/** Container names with the process, for pickers that show only the name. */
export function withProcessNames(containers: Container[]): Container[] {
  return containers.map((container) =>
    container.process ? { ...container, name: `${container.name} (${container.process})` } : container
  );
}
