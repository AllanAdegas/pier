import { A } from "@solidjs/router";
import { For, Show } from "solid-js";
import { EmptyState, QueryView, StatusBadge } from "../components/Feedback";
import { Icon } from "../components/Icon";
import { PageHeader } from "../components/Page";
import { DeploymentList } from "../components/Rows";
import { api } from "../lib/api";
import { serverStatus } from "../lib/format";
import { createQuery } from "../lib/query";
import type { Dashboard } from "../lib/types";

function Stat(props: { label: string; value: number; tone?: "danger" }) {
  return (
    <div class="card stat">
      <span class="stat-value" style={props.tone && props.value > 0 ? { color: "var(--danger)" } : undefined}>
        {props.value}
      </span>
      <span class="stat-label">{props.label}</span>
    </div>
  );
}

export function DashboardPage() {
  const dashboard = createQuery(
    () => true,
    () => api.get<Dashboard>("/dashboard"),
    { pollMs: 10000 },
  );

  return (
    <div class="page">
      <PageHeader title="Painel" description="O que está no ar e o que mudou por último." />
      <QueryView query={dashboard} skeletonLines={4}>
        {(data) => (
          <Show
            when={data().projects > 0}
            fallback={
              <EmptyState
                icon="rocket"
                title="Nada implantado ainda"
                action={
                  <A href="/projects" class="btn btn-primary">
                    <Icon name="plus" size={16} />
                    Criar o primeiro projeto
                  </A>
                }
              >
                Um projeto agrupa aplicações, bancos de dados e stacks. Crie um e faça seu primeiro deploy.
              </EmptyState>
            }
          >
            <div class="stack-lg">
              <div class="grid-cards" style={{ "grid-template-columns": "repeat(auto-fill, minmax(160px, 1fr))" }}>
                <Stat label="Projetos" value={data().projects} />
                <Stat label="Aplicações" value={data().applications} />
                <Stat label="Bancos de dados" value={data().databases} />
                <Stat label="Stacks" value={data().stacks} />
                <Stat label="Em execução" value={data().running} />
                <Stat label="Com problema" value={data().unhealthy} tone="danger" />
              </div>

              <section class="stack">
                <div class="row-between">
                  <h2>Deploys recentes</h2>
                  <A href="/deployments">Ver todos</A>
                </div>
                <Show
                  when={data().deployments.length > 0}
                  fallback={<p class="muted">Nenhum deploy ainda. Abra uma aplicação e clique em “Implantar”.</p>}
                >
                  <DeploymentList deployments={data().deployments} showApplication />
                </Show>
              </section>

              <section class="stack">
                <div class="row-between">
                  <h2>Servidores</h2>
                  <A href="/servers">Gerenciar</A>
                </div>
                <div class="list">
                  <For each={data().servers}>
                    {(server) => (
                      <A href={`/servers/${server.id}`} class="list-item">
                        <span class="muted">
                          <Icon name="server" size={20} />
                        </span>
                        <div class="grow stack-sm" style={{ gap: "0" }}>
                          <strong class="truncate">{server.name}</strong>
                          <span class="small muted truncate">
                            {server.kind === "local" ? "Máquina local" : `${server.user}@${server.host}`}
                            <Show when={server.docker_version}> · Docker {server.docker_version}</Show>
                          </span>
                        </div>
                        <StatusBadge status={serverStatus(server.status)} />
                      </A>
                    )}
                  </For>
                </div>
              </section>
            </div>
          </Show>
        )}
      </QueryView>
    </div>
  );
}
