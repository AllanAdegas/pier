import { A, useNavigate } from "@solidjs/router";
import { For, Show } from "solid-js";
import { Button } from "../../components/Button";
import { EmptyState, QueryView, StatusBadge } from "../../components/Feedback";
import { api } from "../../lib/api";
import { deploymentStatus, formatDuration, formatRelative, shortSha, TRIGGER_LABEL } from "../../lib/format";
import { createAction, type Query } from "../../lib/query";
import type { Application, Deployment } from "../../lib/types";

/** Deployment history with rollback to any previous successful version. */
export function DeploysTab(props: { application: Application; deployments: Query<Deployment[]> }) {
  const navigate = useNavigate();
  const rollback = createAction(
    (deploymentId: string) =>
      api.post<Deployment>(`/applications/${props.application.id}/rollback`, { deployment_id: deploymentId }),
    {
      success: "Rollback enfileirado.",
      onSuccess: (deployment) => navigate(`/deployments/${deployment.id}`),
    },
  );
  const canRollBackTo = (deployment: Deployment) =>
    deployment.status === "finished" && deployment.image !== "" && deployment.image !== props.application.current_image;

  return (
    <QueryView query={props.deployments}>
      {(list) => (
        <Show
          when={list().length > 0}
          fallback={
            <EmptyState icon="rocket" title="Nenhum deploy">
              Clique em “Implantar” para criar a primeira versão.
            </EmptyState>
          }
        >
          <div class="list">
            <For each={list()}>
              {(deployment) => (
                <div class="list-item">
                  <StatusBadge status={deploymentStatus(deployment.status)} />
                  <div class="grow stack-sm" style={{ gap: "0" }}>
                    <A href={`/deployments/${deployment.id}`} class="truncate">
                      {deployment.commit_message || `Deploy ${TRIGGER_LABEL[deployment.trigger].toLowerCase()}`}
                    </A>
                    <span class="small muted truncate">
                      {TRIGGER_LABEL[deployment.trigger]}
                      <Show when={deployment.commit_sha}>
                        {" · "}
                        <span class="mono">{shortSha(deployment.commit_sha)}</span>
                      </Show>
                      {" · "}
                      {formatRelative(deployment.created_at)}
                      <Show when={deployment.started_at}> · {formatDuration(deployment.started_at, deployment.finished_at)}</Show>
                    </span>
                  </div>
                  <Show when={deployment.image !== "" && deployment.image === props.application.current_image}>
                    <span class="tag">no ar</span>
                  </Show>
                  <Show when={canRollBackTo(deployment)}>
                    <Button small pending={rollback.pending()} onClick={() => void rollback.run(deployment.id)}>
                      Reverter para esta
                    </Button>
                  </Show>
                </div>
              )}
            </For>
          </div>
        </Show>
      )}
    </QueryView>
  );
}
