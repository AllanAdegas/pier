import { For, Show } from "solid-js";
import { A } from "@solidjs/router";
import { api } from "../../lib/api";
import { createQuery } from "../../lib/query";
import { t } from "../../i18n";
import { QueryView } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { SearchIndexingHosts } from "../../components/SearchIndexing";
import { resourcePath, type IndexNowResource } from "../../components/searchIndexingModel";

/**
 * Settings › Search engines: every domain of every application and stack in
 * one place. The panel and previews never appear in search engines; any
 * other domain only when marked.
 */
export function SearchEngineSettings() {
  const resources = createQuery(() => "/indexnow/sites", (path) => api.get<IndexNowResource[]>(path));
  return (
    <Section
      title={t("Buscadores")}
      description={t("Só aparece nos buscadores o domínio que você marcar: os outros, o painel e os previews respondem com noindex. Marcar ou desmarcar vale a partir do próximo deploy do recurso.")}
    >
      <QueryView query={resources}>
        {(list) => (
          <Show when={list().length > 0} fallback={<p class="small muted">{t("Nenhuma aplicação ou stack com domínio.")}</p>}>
            <div class="stack">
              <For each={list()}>
                {(resource) => (
                  <div class="stack-sm">
                    <A href={resourcePath(resource)}>
                      <strong>{resource.resource_name}</strong>
                    </A>
                    <SearchIndexingHosts
                      kind={resource.resource_kind}
                      id={resource.resource_id}
                      overview={resource}
                      onChange={() => void resources.refetch()}
                    />
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>
    </Section>
  );
}
