import { A, useParams } from "@solidjs/router";
import { createEffect, For, on, onCleanup, Show } from "solid-js";
import { Button } from "../components/Button";
import { Alert, EmptyState, QueryView, StatusBadge } from "../components/Feedback";
import { createLineBuffer, LogViewer } from "../components/LogViewer";
import { PageHeader } from "../components/Page";
import { DeploymentList } from "../components/Rows";
import { api, openStream } from "../lib/api";
import { deploymentStatus, formatDateTime, formatDuration, shortSha } from "../lib/format";
import { triggerLabel } from "./application/release";
import { createAction, createQuery, type Query } from "../lib/query";
import type { Application, Deployment, LogEntry } from "../lib/types";
import { t } from "../i18n";
import { withSuggestedPort } from "./application/portSuggestion";

export function DeploymentsPage() {
  const deployments = createQuery(
    () => true,
    () => api.get<Deployment[]>("/deployments"),
    { pollMs: 5000 },
  );
  return (
    <div class="page">
      <PageHeader title="Deploys" description={t("Os 50 deploys mais recentes de todas as aplicações.")} />
      <QueryView query={deployments}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={
              <EmptyState icon="rocket" title={t("Nenhum deploy ainda")}>{t("Os deploys aparecem aqui quando você implanta uma aplicação, manualmente ou por webhook.")}
              </EmptyState>
            }
          >
            <DeploymentList deployments={list()} showApplication />
          </Show>
        )}
      </QueryView>
    </div>
  );
}

const isActive = (deployment: Deployment | undefined) =>
  deployment?.status === "queued" || deployment?.status === "in_progress";

/** One deployment with its log, streamed live while it runs. */
export function DeploymentPage() {
  const params = useParams<{ id: string }>();
  // Typed explicitly because the polling interval reads the query itself.
  const deployment: Query<Deployment> = createQuery(
    () => params.id,
    (id) => api.get<Deployment>(`/deployments/${id}`),
    { pollMs: (): number => (isActive(deployment.data()) ? 2000 : 0) },
  );
  const buffer = createLineBuffer();
  const applyPort = createAction(async (port: number) => {
    const id = deployment.data()!.application_id;
    const app = await api.get<Application>(`/applications/${id}`);
    return api.patch<Application>(`/applications/${id}`, { name: app.name, description: app.description, config: withSuggestedPort(app.config, port) });
  }, { success: t("Porta atualizada. Implante novamente para aplicar.") });

  createEffect(
    on(
      () => params.id,
      (id) => {
        buffer.reset();
        const close = openStream<LogEntry>(`/deployments/${id}/logs/stream`, "log", {
          onMessage: (entry) => buffer.push(entry),
          onDone: () => void deployment.refetch(),
        });
        onCleanup(close);
      },
    ),
  );

  const cancel = createAction(() => api.post(`/deployments/${params.id}/cancel`), {
    success: t("Cancelamento solicitado."),
    onSuccess: () => void deployment.refetch(),
  });

  return (
    <div class="page">
      <QueryView query={deployment}>
        {(data) => (
          <>
            <PageHeader
              crumbs={[
                { label: "Deploys", href: "/deployments" },
                { label: data().application_name, href: `/applications/${data().application_id}` },
                { label: shortSha(data().commit_sha) || data().id },
              ]}
              title={
                <span class="row">
                  {t("Deploy de {name}", { name: data().application_name })}
                  <StatusBadge status={deploymentStatus(data().status)} />
                </span>
              }
              description={data().commit_message || undefined}
              actions={
                <>
                  <A href={`/applications/${data().application_id}`} class="btn">{t("Abrir aplicação")}
                  </A>
                  <Show when={isActive(data())}>
                    <Button variant="danger" icon="stop" pending={cancel.pending()} onClick={() => void cancel.run()}>{t("Cancelar deploy")}
                    </Button>
                  </Show>
                </>
              }
            />
            <For each={data().hints ?? []}>{(hint) =>
              <Alert tone="info"><strong>{hint.Title}</strong><p>{hint.Fix}</p>
                <Show when={hint.DocURL}><a href={hint.DocURL} target="_blank" rel="noopener noreferrer">{t("Documentação")}</a></Show>
                <Show when={hint.Code === "listener_port_mismatch" && data().suggested_port}>
                  <Button small pending={applyPort.pending()} onClick={() => void applyPort.run(data().suggested_port!)}>{t("Usar a porta {port}", { port: data().suggested_port! })}</Button>
                </Show>
              </Alert>
            }</For>
            <Show when={data().status === "failed" && data().error}>
              <Alert tone="danger">
                <strong>{t("O deploy falhou.")}</strong> {data().error} {t("A versão anterior continua no ar.")}
              </Alert>
            </Show>
            <Show when={data().status === "finished"}>
              <Alert tone="success">
                <strong>{t("Deploy concluído.")}</strong> {t("A nova versão está recebendo tráfego.")}
              </Alert>
            </Show>
            <dl class="kv card">
              <dt>{t("Origem")}</dt>
              <dd>
                {triggerLabel(data().trigger)}
                <Show when={data().source}> · {data().source}</Show>
              </dd>
              <dt>Commit</dt>
              <dd class="mono">{data().commit_sha || "—"}</dd>
              <dt>{t("Imagem")}</dt>
              <dd class="mono">{data().image || "—"}</dd>
              <dt>{t("Criado em")}</dt>
              <dd>{formatDateTime(data().created_at)}</dd>
              <dt>{t("Duração")}</dt>
              <dd>{formatDuration(data().started_at, data().finished_at)}</dd>
            </dl>
            <For each={data().retired_logs ?? []}>{(log) => <details class="card"><summary>{t("Logs da versão anterior")}: <span class="mono">{log.container}</span></summary><pre style={{ "white-space": "pre-wrap", "max-height": "32rem", overflow: "auto" }}>{log.output}</pre></details>}</For>
          </>
        )}
      </QueryView>
      <LogViewer lines={buffer.lines()} emptyText={t("Aguardando o início do deploy…")} />
    </div>
  );
}
