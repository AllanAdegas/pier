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

type ResourceKind = "applications" | "stacks";

/**
 * "Index in search engines" of an application or stack. Nothing Pier
 * publishes is indexed unless marked here: a marked https host answers
 * without noindex, serves its IndexNow key and has its sitemap submitted
 * after each successful deployment.
 */
export function SearchIndexing(props: { kind: ResourceKind; id: string }) {
  const overview = createQuery(() => `/${props.kind}/${props.id}/indexnow`, (path) => api.get<IndexNowOverview>(path));
  return (
    <Section
      title={t("Indexar nos buscadores")}
      description={t("Só aparece nos buscadores o domínio marcado aqui: os outros respondem com noindex. Um domínio marcado também avisa Bing, Yandex e outros buscadores (IndexNow) das páginas novas do sitemap depois de cada deploy. Marcar ou desmarcar tira ou põe o noindex a partir do próximo deploy.")}
    >
      <QueryView query={overview}>
        {(state) => (
          <SearchIndexingHosts kind={props.kind} id={props.id} overview={state()} onChange={(updated) => overview.set(updated)} />
        )}
      </QueryView>
    </Section>
  );
}

/** The hosts of one resource with their mark, state and "Notify now"; shared with Settings › Search engines. */
export function SearchIndexingHosts(props: {
  kind: ResourceKind;
  id: string;
  overview: IndexNowOverview;
  onChange: (overview: IndexNowOverview) => void;
}) {
  const base = () => `/${props.kind}/${props.id}/indexnow`;
  const toggle = createAction(
    (host: string, enabled: boolean) => api.put<IndexNowOverview>(base(), { host, enabled }),
    { onSuccess: (updated) => props.onChange(updated) },
  );
  const submit = createAction((host: string) => api.post<IndexNowHost>(`${base()}/submit`, { host }), {
    success: (state) => state.last?.message ?? t("Envio concluído."),
    onSuccess: () => void api.get<IndexNowOverview>(base()).then(props.onChange),
  });

  return (
    <div class="stack-sm">
      <Show when={!props.overview.available && props.overview.reason}>
        <Alert tone="neutral">{props.overview.reason}</Alert>
      </Show>
      <Show when={props.overview.warning}>
        <Alert tone="warning">{props.overview.warning}</Alert>
      </Show>
      <Show
        when={props.overview.hosts.length > 0}
        fallback={<p class="small muted">{t("Nenhum domínio salvo neste recurso.")}</p>}
      >
        <div class="list">
          <For each={props.overview.hosts}>
            {(host) => (
              <div class="list-item">
                <div class="grow stack-xs">
                  <Show when={host.eligible} fallback={<strong class="truncate">{host.host}</strong>}>
                    <Checkbox
                      label={host.host}
                      checked={host.enabled}
                      disabled={toggle.pending() || !canToggle(props.overview, host)}
                      onChange={(enabled) => void toggle.run(host.host, enabled)}
                    />
                  </Show>
                  <Show when={!host.eligible}>
                    <span class="small muted">{host.reason}</span>
                  </Show>
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
                <Show when={host.enabled && props.overview.available}>
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
  );
}
