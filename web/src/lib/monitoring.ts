import { createSignal, onCleanup } from "solid-js";
import { api } from "./api";
import { createQuery } from "./query";

export function createMonitoringQuery<T>(path: () => string) {
  const [pendingPaths, setPendingPaths] = createSignal<string[]>([]);
  const [receivedAt, setReceivedAt] = createSignal(0);
  const [now, setNow] = createSignal(Date.now());
  const timer = window.setInterval(() => setNow(Date.now()), 5000);
  onCleanup(() => window.clearInterval(timer));
  const inFlight = new Map<string, Promise<T>>();
  const query = createQuery(path, (url) => {
    const existing = inFlight.get(url);
    if (existing) return existing;
    setPendingPaths((paths) => [...paths, url]);
    const request = api.get<T>(url).then((result) => {
      if (url === path()) setReceivedAt(Date.now());
      return result;
    }).finally(() => {
      inFlight.delete(url);
      setPendingPaths((paths) => paths.filter((pendingPath) => pendingPath !== url));
    });
    inFlight.set(url, request);
    return request;
  }, { pollMs: 15000 });
  return { ...query, pending: () => pendingPaths().includes(path()), receivedAt, now };
}
