import { A } from "@solidjs/router";
import { For, Show } from "solid-js";
import { Alert, EmptyState, StatusBadge } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { DeploymentList } from "../../components/Rows";
import { BUILD_PACK_LABEL } from "../../lib/format";
import type { Application, Container, Deployment } from "../../lib/types";

function containerTone(container: Container) {
  if (container.state !== "running") {
    return { label: container.status, tone: "neutral" as const };
  }
  if (container.status.includes("unhealthy")) {
    return { label: container.status, tone: "danger" as const };
  }
  return { label: container.status, tone: "success" as const };
}

export function OverviewTab(props: { application: Application; containers: Container[]; deployments: Deployment[] }) {
  const config = () => props.application.config;
  const neverDeployed = () => props.application.current_image === "" && props.deployments.length === 0;

  return (
    <div class="stack-lg">
      <Show when={neverDeployed()}>
        <Alert tone="info">
          Esta aplicação ainda não foi implantada. Revise a configuração e clique em <strong>Implantar</strong>.
        </Alert>
      </Show>

      <Section title="Resumo">
        <dl class="kv">
          <dt>Origem</dt>
          <dd class="mono">
            <Show when={config().build.pack === "image"} fallback={`${config().source.repository} (${config().source.branch})`}>
              {config().source.image}
            </Show>
          </dd>
          <dt>Build</dt>
          <dd>{BUILD_PACK_LABEL[config().build.pack]}</dd>
          <dt>Porta</dt>
          <dd>{config().network.port}</dd>
          <dt>Imagem em execução</dt>
          <dd class="mono">{props.application.current_image || "—"}</dd>
          <dt>Endereço interno</dt>
          <dd class="mono">
            {props.application.id}:{config().network.port}
          </dd>
          <dt>Deploy automático</dt>
          <dd>{config().auto_deploy ? "Ativado (push na branch)" : "Desativado"}</dd>
        </dl>
      </Section>

      <Section title="Containers" description="Durante um rolling update, a versão antiga e a nova aparecem juntas.">
        <Show
          when={props.containers.length > 0}
          fallback={<p class="muted">Nenhum container no servidor.</p>}
        >
          <div class="list">
            <For each={props.containers}>
              {(container) => (
                <div class="list-item">
                  <div class="grow stack-sm" style={{ gap: "0" }}>
                    <span class="mono truncate">{container.name}</span>
                    <span class="small muted mono truncate">{container.image}</span>
                  </div>
                  <StatusBadge status={containerTone(container)} />
                </div>
              )}
            </For>
          </div>
        </Show>
      </Section>

      <section class="stack">
        <div class="row-between">
          <h2>Últimos deploys</h2>
          <A href="?tab=deploys">Ver histórico</A>
        </div>
        <Show
          when={props.deployments.length > 0}
          fallback={
            <EmptyState icon="rocket" title="Nenhum deploy">
              O histórico aparece aqui depois do primeiro deploy.
            </EmptyState>
          }
        >
          <DeploymentList deployments={props.deployments.slice(0, 5)} />
        </Show>
      </section>
    </div>
  );
}
