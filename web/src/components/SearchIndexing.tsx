import { For, Show } from "solid-js";
import { api } from "../lib/api";
import { formatRelative } from "../lib/format";
import { createAction, createQuery } from "../lib/query";
import { t } from "../i18n";
import { Button } from "./Button";
import { Alert, QueryView, StatusBadge } from "./Feedback";
import { Checkbox } from "./Form";
import { CopyField, Section } from "./Page";
import { canToggle, hostStatus, type IndexNowHost, type IndexNowOverview } from "./searchIndexingModel";

/**
 * Search engine notification (IndexNow) of the https domains of an
 * application or stack: Pier serves the key and submits the sitemap after
 * each successful deployment.
 */
export function SearchIndexing(props: { kind: "applications" | "stacks"; id: string }) {
  const base = () => `/${props.kind}/${props.id}/indexnow`;
  const overview = createQuery(() => base(), (path) => api.get<IndexNowOverview>(path));
  const toggle = createAction(
    (host: string, enabled: boolean) => api.put<IndexNowOverview>(base(), { host, enabled }),
    { onSuccess: (updated) => overview.set(updated) },
  );
  const submit = createAction((host: string) => api.post<IndexNowHost>(`${base()}/submit`, { host }), {
    success: (state) => state.last?.message ?? t("Envio concluído."),
    onSuccess: () => void overview.refetch(),
  });

  return (
    <Section
      title={t("Avisar buscadores")}
      description={t("O Pier publica a chave do IndexNow no domínio e, depois de cada deploy concluído, envia as páginas novas do sitemap ao Bing, Yandex e outros buscadores. O Google não usa IndexNow: cadastre o sitemap uma vez no Search Console.")}
    >
      <QueryView query={overview}>
        {(state) => (
          <div class="stack-sm">
            <Show when={!state().available && state().reason}>
              <Alert tone="neutral">{state().reason}</Alert>
            </Show>
            <Show when={state().warning}>
              <Alert tone="warning">{state().warning}</Alert>
            </Show>
            <Show
              when={state().hosts.length > 0}
              fallback={<p class="small muted">{t("Nenhum domínio https:// sem caminho salvo neste recurso.")}</p>}
            >
              <div class="list">
                <For each={state().hosts}>
                  {(host) => (
                    <div class="list-item">
                      <div class="grow stack-xs">
                        <Checkbox
                          label={host.host}
                          checked={host.enabled}
                          disabled={toggle.pending() || !canToggle(state(), host)}
                          onChange={(enabled) => void toggle.run(host.host, enabled)}
                        />
                        <Show when={host.enabled}>
                          <CopyField label={t("Arquivo da chave")} value={host.key_location} />
                        </Show>
                        <Show when={host.last?.attempted_at}>
                          <span class="small muted">
                            {host.last!.message} · {formatRelative(host.last!.attempted_at)}
                          </span>
                        </Show>
                      </div>
                      <StatusBadge status={hostStatus(host)} />
                      <Show when={host.enabled && state().available}>
                        <Button small pending={submit.pending()} onClick={() => void submit.run(host.host)}>
                          {t("Avisar agora")}
                        </Button>
                      </Show>
                    </div>
                  )}
                </For>
              </div>
            </Show>
          </div>
        )}
      </QueryView>
    </Section>
  );
}
