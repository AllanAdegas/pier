import { createEffect, createSignal, For, Show } from "solid-js";
import { TerminalButton } from "../../components/Terminal";
import { Button } from "../../components/Button";
import { Alert, EmptyState, QueryView, StatusBadge } from "../../components/Feedback";
import { Checkbox, Select, TextInput } from "../../components/Form";
import { LogStream } from "../../components/LogViewer";
import { Section } from "../../components/Page";
import { t } from "../../i18n";
import { api } from "../../lib/api";
import { containerStatus, formatRelative } from "../../lib/format";
import { createAction, createQuery } from "../../lib/query";
import type { Application, Server } from "../../lib/types";
import {
  needsRegistry,
  type Placement,
  type PlacementServer,
  type Registry,
  roleLabel,
  rolloutStatus,
  shortImage,
  toggleDestination,
} from "./placementModel";
import { serverOperationPath } from "./serverOperations";
import { RegistriesSection } from "./PlacementRegistries";

/** Where the application is built and which servers run it, with their live state. */
export function PlacementTab(props: { application: Application }) {
  return (
    <div class="stack-lg">
      <PlacementSettings application={props.application} />
      <RolloutRecovery applicationId={props.application.id} />
      <PlacementServers applicationId={props.application.id} />
      <RegistriesSection />
    </div>
  );
}

function PlacementSettings(props: { application: Application }) {
  const primary = () => props.application.server_id;
  const imagePack = () => props.application.config.build.pack === "image";
  const servers = createQuery(() => true, () => api.get<Server[]>("/servers"));
  const registries = createQuery(() => true, () => api.get<Registry[]>("/registries"));
  const saved = createQuery(() => props.application.id, (id) => api.get<Placement>(`/applications/${id}/placement`));
  const [draft, setDraft] = createSignal<Placement>();
  createEffect(() => {
    const current = saved.data();
    if (current && !draft()) setDraft({ ...current, destinations: current.destinations.length ? current.destinations : [primary()] });
  });
  const save = createAction(() => api.put<Placement>(`/applications/${props.application.id}/placement`, draft()), {
    success: t("Destinos salvos. Valem a partir do próximo deploy."),
    onSuccess: (placement) => setDraft(placement),
  });
  const change = (patch: Partial<Placement>) => setDraft({ ...draft()!, ...patch });
  const serverName = (id: string) => servers.data()?.find((server) => server.id === id)?.name ?? id;

  return (
    <Section
      title={t("Servidores e build")}
      description={t("Cada servidor roda a mesma imagem, fixada por digest, atrás do próprio proxy e TLS. Aponte o DNS ou um balanceador externo para os servidores.")}
      actions={
        <Button variant="primary" pending={save.pending()} disabled={!draft()} onClick={() => void save.run()}>{t("Salvar destinos")}
        </Button>
      }
    >
      <QueryView query={servers}>
        {(list) => (
          <Show when={draft()}>
            {(current) => (
              <div class="stack">
                <TextInput label={t("ID do alvo Swarm")} value={current().swarm_cluster_id ?? ""} onValue={(value) => change({ swarm_cluster_id: value.trim() })} />
                <fieldset class="stack-sm">
                  <legend class="field-label">{t("Servidores que rodam a aplicação")}</legend>
                  <For each={list()}>
                    {(server) => (
                      <Checkbox
                        label={server.name}
                        hint={server.id === primary() ? t("Servidor da aplicação: sempre incluído e atualizado primeiro.") : undefined}
                        checked={current().destinations.includes(server.id)}
                        disabled={server.id === primary()}
                        onChange={(on) => setDraft(toggleDestination(current(), primary(), server.id, on))}
                      />
                    )}
                  </For>
                  <Show when={save.errors().destinations}>
                    <span class="field-error" role="alert">{save.errors().destinations}</span>
                  </Show>
                </fieldset>
                <Select
                  label={t("Servidor de build")}
                  hint={t("Constrói a imagem e a envia ao registro; os servidores de destino só baixam.")}
                  placeholder={t("O próprio servidor da aplicação")}
                  value={current().build_server_id}
                  error={save.errors().build_server_id}
                  options={list().filter((server) => server.id !== primary()).map((server) => ({ value: server.id, label: server.name }))}
                  onValue={(value) => change({ build_server_id: value })}
                />
                <Select
                  label={t("Registro de imagens")}
                  hint={needsRegistry(current(), primary(), imagePack())
                    ? t("Obrigatório: a imagem construída precisa chegar aos outros servidores.")
                    : t("Opcional enquanto a imagem não sai do servidor em que é construída.")}
                  placeholder={t("Nenhum")}
                  value={current().registry_id}
                  error={save.errors().registry_id}
                  options={(registries.data() ?? []).map((registry) => ({ value: registry.id, label: `${registry.name} (${registry.host})` }))}
                  onValue={(value) => change({ registry_id: value })}
                />
                <TextInput
                  label={t("Repositório de imagens")}
                  hint={t("Caminho no registro, como empresa/api. O host vem do registro escolhido.")}
                  placeholder={t("empresa/api")}
                  mono
                  value={current().repository}
                  error={save.errors().repository}
                  onValue={(value) => change({ repository: value.trim() })}
                />
                <Show when={current().destinations.length > 1 && props.application.config.storages.length > 0}>
                  <Alert tone="warning">
                    <span>{t("Volumes e pastas não são compartilhados: cada servidor terá os próprios dados.")}</span>
                  </Alert>
                </Show>
                <Show when={current().destinations.length > 1}>
                  <p class="muted small">
                    {t("Ordem do deploy: {servers}. Se um servidor falhar, os já atualizados voltam para a versão anterior e os seguintes não são tocados. O comando de release roda só no primeiro.", {
                      servers: current().destinations.map(serverName).join(" → "),
                    })}
                  </p>
                </Show>
              </div>
            )}
          </Show>
        )}
      </QueryView>
    </Section>
  );
}

function PlacementServers(props: { applicationId: string }) {
  const servers = createQuery(
    () => props.applicationId,
    (id) => api.get<PlacementServer[]>(`/applications/${id}/placement/servers`),
    { pollMs: 8000 },
  );
  return (
    <Section title={t("Estado por servidor")} description={t("Versão, containers, uso de recursos e logs em cada servidor.")}>
      <QueryView query={servers}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={<EmptyState icon="server" title={t("Nenhum servidor ainda")}>{t("Faça um deploy para ver o estado aqui.")}</EmptyState>}
          >
            <div class="stack">
              <For each={list()}>{(server) => <ServerCard applicationId={props.applicationId} server={server} />}</For>
            </div>
          </Show>
        )}
      </QueryView>
    </Section>
  );
}

function ServerCard(props: { applicationId: string; server: PlacementServer }) {
  const [logsOf, setLogsOf] = createSignal("");
  const rollout = () => props.server.rollout;
  return (
    <article class="card stack-sm">
      <div class="row-between">
        <div class="stack-sm">
          <strong>{props.server.name}</strong>
          <span class="muted small">{roleLabel(props.server.role)}</span>
        </div>
        <StatusBadge status={rolloutStatus(rollout())} />
      </div>
      <Show when={rollout()}>
        {(state) => (
          <dl class="small">
            <dt>{t("Imagem")}</dt>
            <dd class="mono">{state().image ? shortImage(state().image) : "—"}</dd>
            <Show when={state().previous_image}>
              <dt>{t("Versão anterior")}</dt>
              <dd class="mono">{shortImage(state().previous_image)}</dd>
            </Show>
            <dt>{t("Atualizado")}</dt>
            <dd>{formatRelative(state().updated_at)}</dd>
          </dl>
        )}
      </Show>
      <Show when={rollout()?.message}>
        <Alert tone="danger"><span>{rollout()!.message}</span></Alert>
      </Show>
      <Show
        when={props.server.reachable}
        fallback={<Alert tone="warning"><span>{t("Servidor inacessível agora: sem containers nem métricas.")}</span></Alert>}
      >
        <Show when={props.server.containers.length > 0} fallback={<p class="muted small">{t("Nenhum container da aplicação neste servidor.")}</p>}>
          <ul class="stack-sm">
            <For each={props.server.containers}>
              {(container) => (
                <li class="row-between">
                  <span class="row">
                    <StatusBadge status={containerStatus(container)} />
                    <span class="mono small">{container.name}</span>
                  </span>
                  <span class="row small muted">
                    <span>CPU {container.cpu_percent || "—"}</span>
                    <span>{t("Memória")} {container.memory_usage || "—"}</span>
                    <TerminalButton endpoint={serverOperationPath(props.applicationId, props.server.server_id, "terminal")} container={container.name} />
                    <Button small icon="terminal" onClick={() => setLogsOf(logsOf() === container.name ? "" : container.name)}>
                      {logsOf() === container.name ? t("Ocultar logs") : t("Ver logs")}
                    </Button>
                  </span>
                </li>
              )}
            </For>
          </ul>
          <Show when={logsOf()}>
            <LogStream
              path={serverOperationPath(props.applicationId, props.server.server_id, `containers/${encodeURIComponent(logsOf())}/logs`) + "&tail=300"}
              emptyText={t("Aguardando saída do container…")}
            />
          </Show>
        </Show>
      </Show>
    </article>
  );
}

function RolloutRecovery(props: { applicationId: string }) {
 const journal = createQuery(() => props.applicationId, id => api.get<{state: string; message: string} | null>(`/applications/${id}/placement/rollout`), {pollMs: 4000});
 const resume = createAction(() => api.post(`/applications/${props.applicationId}/placement/rollout/resume`, {}));
 const rollback = createAction(() => api.post(`/applications/${props.applicationId}/placement/rollout/rollback`, {}));
 return <Show when={journal.data()?.state === "interrupted"}>
  <Section title={t("Rollout interrompido")} description={t("Retome a imagem fixada ou restaure a versão anterior. Comandos de release podem exigir verificação manual.")}>
   <p>{journal.data()?.message}</p>
   <div class="row"><Button pending={resume.pending()} disabled={rollback.pending()} onClick={() => void resume.run()}>{t("Retomar rollout")}</Button>
   <Button pending={rollback.pending()} disabled={resume.pending()} onClick={() => void rollback.run()}>{t("Reverter rollout")}</Button></div>
  </Section>
 </Show>;
}
