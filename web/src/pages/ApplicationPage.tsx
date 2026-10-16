import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { For, Match, Show, Switch } from "solid-js";
import { Button } from "../components/Button";
import { EnvEditor } from "../components/EnvEditor";
import { QueryView, StatusBadge } from "../components/Feedback";
import { Icon } from "../components/Icon";
import { ContainerLogs } from "../components/LogViewer";
import { PageHeader, Tabs } from "../components/Page";
import { api } from "../lib/api";
import { resourceStatus } from "../lib/format";
import { createAction, createQuery } from "../lib/query";
import type { Application, Container, Deployment } from "../lib/types";
import { AutomationTab } from "./application/Automation";
import { ConfigTab } from "./application/Config";
import { DeploysTab } from "./application/Deploys";
import { OverviewTab } from "./application/Overview";

const TABS = [
  { id: "overview", label: "Visão geral" },
  { id: "deploys", label: "Deploys" },
  { id: "logs", label: "Logs" },
  { id: "config", label: "Configuração" },
  { id: "env", label: "Variáveis" },
  { id: "automation", label: "Automação" },
];

export function ApplicationPage() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [search, setSearch] = useSearchParams<{ tab?: string }>();
  const tab = () => search.tab ?? "overview";

  const application = createQuery(
    () => params.id,
    (id) => api.get<Application>(`/applications/${id}`),
    { pollMs: 5000 },
  );
  const containers = createQuery(
    () => (tab() === "overview" || tab() === "logs") && params.id,
    (id) => api.get<Container[]>(`/applications/${id}/containers`),
    { pollMs: 8000 },
  );
  const deployments = createQuery(
    () => (tab() === "overview" || tab() === "deploys") && params.id,
    (id) => api.get<Deployment[]>(`/applications/${id}/deployments`),
    { pollMs: 5000 },
  );

  const deploy = createAction(() => api.post<Deployment>(`/applications/${params.id}/deploy`), {
    success: "Deploy enfileirado.",
    onSuccess: (deployment) => navigate(`/deployments/${deployment.id}`),
  });
  const operate = createAction(
    (action: "start" | "stop" | "restart") => api.post<Application>(`/applications/${params.id}/actions/${action}`),
    { success: "Operação concluída.", onSuccess: (updated) => application.set(updated) },
  );

  return (
    <div class="page">
      <QueryView query={application}>
        {(app) => (
          <>
            <PageHeader
              crumbs={[{ label: "Projetos", href: "/projects" }, { label: app().name }]}
              title={
                <span class="row">
                  {app().name}
                  <StatusBadge status={resourceStatus(app().status)} />
                </span>
              }
              description={
                <Show when={app().config.network.domains.length > 0} fallback={app().description || "Sem domínio público."}>
                  <span class="row">
                    <For each={app().config.network.domains}>
                      {(domain) => (
                        <a href={domain} target="_blank" rel="noreferrer">
                          {domain} <Icon name="external" size={12} />
                        </a>
                      )}
                    </For>
                  </span>
                </Show>
              }
              actions={
                <>
                  <Show
                    when={app().status === "stopped"}
                    fallback={
                      <>
                        <Button icon="refresh" pending={operate.pending()} onClick={() => void operate.run("restart")}>
                          Reiniciar
                        </Button>
                        <Button icon="stop" pending={operate.pending()} onClick={() => void operate.run("stop")}>
                          Parar
                        </Button>
                      </>
                    }
                  >
                    <Show when={app().current_image}>
                      <Button icon="play" pending={operate.pending()} onClick={() => void operate.run("start")}>
                        Iniciar
                      </Button>
                    </Show>
                  </Show>
                  <Button variant="primary" icon="rocket" pending={deploy.pending()} onClick={() => void deploy.run()}>
                    Implantar
                  </Button>
                </>
              }
            />
            <Tabs tabs={TABS} selected={tab()} onSelect={(id) => setSearch({ tab: id })} />
            <Switch>
              <Match when={tab() === "overview"}>
                <OverviewTab application={app()} containers={containers.data() ?? []} deployments={deployments.data() ?? []} />
              </Match>
              <Match when={tab() === "deploys"}>
                <DeploysTab application={app()} deployments={deployments} />
              </Match>
              <Match when={tab() === "logs"}>
                <QueryView query={containers}>
                  {(list) => <ContainerLogs serverId={app().server_id} containers={list()} />}
                </QueryView>
              </Match>
              <Match when={tab() === "config"}>
                <ConfigTab application={app()} onSaved={(saved) => application.set(saved)} />
              </Match>
              <Match when={tab() === "env"}>
                <EnvEditor
                  basePath={`/applications/${app().id}`}
                  allowBuildTime={app().config.build.pack !== "image"}
                  note="As alterações valem a partir do próximo deploy."
                />
              </Match>
              <Match when={tab() === "automation"}>
                <AutomationTab application={app()} onSaved={(saved) => application.set(saved)} />
              </Match>
            </Switch>
          </>
        )}
      </QueryView>
    </div>
  );
}
