import { A, useParams } from "@solidjs/router";
import { createEffect, on, onCleanup, Show } from "solid-js";
import { Button } from "../components/Button";
import { Alert, EmptyState, QueryView, StatusBadge } from "../components/Feedback";
import { createLineBuffer, LogViewer } from "../components/LogViewer";
import { PageHeader } from "../components/Page";
import { DeploymentList } from "../components/Rows";
import { api, openStream } from "../lib/api";
import { deploymentStatus, formatDateTime, formatDuration, shortSha, TRIGGER_LABEL } from "../lib/format";
import { createAction, createQuery, type Query } from "../lib/query";
import type { Deployment, LogEntry } from "../lib/types";

export function DeploymentsPage() {
  const deployments = createQuery(
    () => true,
    () => api.get<Deployment[]>("/deployments"),
    { pollMs: 5000 },
  );
  return (
    <div class="page">
      <PageHeader title="Deploys" description="Os 50 deploys mais recentes de todas as aplicações." />
      <QueryView query={deployments}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={
              <EmptyState icon="rocket" title="Nenhum deploy ainda">
                Os deploys aparecem aqui quando você implanta uma aplicação, manualmente ou por webhook.
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
    success: "Cancelamento solicitado.",
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
                  Deploy de {data().application_name}
                  <StatusBadge status={deploymentStatus(data().status)} />
                </span>
              }
              description={data().commit_message || undefined}
              actions={
                <>
                  <A href={`/applications/${data().application_id}`} class="btn">
                    Abrir aplicação
                  </A>
                  <Show when={isActive(data())}>
                    <Button variant="danger" icon="stop" pending={cancel.pending()} onClick={() => void cancel.run()}>
                      Cancelar deploy
                    </Button>
                  </Show>
                </>
              }
            />
            <Show when={data().status === "failed" && data().error}>
              <Alert tone="danger">
                <strong>O deploy falhou.</strong> {data().error} A versão anterior continua no ar.
              </Alert>
            </Show>
            <Show when={data().status === "finished"}>
              <Alert tone="success">
                <strong>Deploy concluído.</strong> A nova versão está recebendo tráfego.
              </Alert>
            </Show>
            <dl class="kv card">
              <dt>Origem</dt>
              <dd>{TRIGGER_LABEL[data().trigger]}</dd>
              <dt>Commit</dt>
              <dd class="mono">{data().commit_sha || "—"}</dd>
              <dt>Imagem</dt>
              <dd class="mono">{data().image || "—"}</dd>
              <dt>Criado em</dt>
              <dd>{formatDateTime(data().created_at)}</dd>
              <dt>Duração</dt>
              <dd>{formatDuration(data().started_at, data().finished_at)}</dd>
            </dl>
          </>
        )}
      </QueryView>
      <LogViewer lines={buffer.lines()} emptyText="Aguardando o início do deploy…" />
    </div>
  );
}
