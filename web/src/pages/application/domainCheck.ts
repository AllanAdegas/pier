/**
 * Hosts whose HTTPS certificate depends on DNS: entries with https:// or no
 * scheme (the server treats those as https). Invalid entries are skipped;
 * validation reports them on save.
 */
export function certificateHosts(values: string[]): string[] {
  const hosts: string[] = [];
  for (const raw of values) {
    const value = raw.trim();
    if (value === "" || value.startsWith("http://")) continue;
    try {
      const host = new URL(value.includes("://") ? value : `https://${value}`).hostname.toLowerCase();
      if (host && !hosts.includes(host)) hosts.push(host);
    } catch {
      continue;
    }
  }
  return hosts;
}
