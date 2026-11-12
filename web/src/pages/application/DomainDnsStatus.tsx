import { createSignal, For, Show } from "solid-js";
import { Alert } from "../../components/Feedback";
import { api } from "../../lib/api";
import type { DomainCheck } from "../../lib/types";
import { t } from "../../i18n";
import { certificateHosts } from "./domainCheck";

interface HostResult {
  host: string;
  check?: DomainCheck;
  failed?: boolean;
}

/**
 * DNS check of the domains of an application (call `check` on blur). A host
 * that does not point to the server yet is published over HTTP only, so the
 * result says which record to create before the certificate is requested.
 */
export function createDomainDnsCheck(serverId: () => string) {
  const [results, setResults] = createSignal<HostResult[]>([]);
  let generation = 0;

  const check = async (domains: string[]) => {
    const current = ++generation;
    const hosts = certificateHosts(domains);
    if (!serverId() || hosts.length === 0) {
      setResults([]);
      return;
    }
    const checked = await Promise.all(hosts.map(async (host): Promise<HostResult> => {
      try {
        return { host, check: await api.post<DomainCheck>(`/servers/${serverId()}/check-domain`, { domain: host }) };
      } catch {
        return { host, failed: true };
      }
    }));
    if (current === generation) setResults(checked);
  };

  const View = () => (
    <Show when={results().length > 0}>
      <div class="stack-sm" aria-live="polite">
        <For each={results()}>
          {(result) => (
            <Alert tone={result.failed ? "neutral" : result.check?.ok ? "success" : "warning"}>
              <Show when={!result.failed} fallback={t("Não foi possível verificar o DNS de {host} agora.", { host: result.host })}>
                <Show
                  when={result.check?.ok}
                  fallback={
                    <>
                      {t("{host} ainda não aponta para este servidor: será publicado só em HTTP até o DNS ser corrigido.", { host: result.host })}{" "}
                      {result.check?.record_hint}
                    </>
                  }
                >
                  {t("{host} aponta para este servidor.", { host: result.host })}
                </Show>
              </Show>
            </Alert>
          )}
        </For>
      </div>
    </Show>
  );

  return { check, View };
}
