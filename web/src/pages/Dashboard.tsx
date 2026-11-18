import { A } from "@solidjs/router";
import { createSignal, For, Show } from "solid-js";
import { EmptyState, QueryView, StatusBadge } from "../components/Feedback";
import { Icon } from "../components/Icon";
import { IconButton } from "../components/Button";
import { currentUser,readOnboardingDismissals,saveOnboardingDismissals } from "../lib/state";
import { PageHeader } from "../components/Page";
import { DeploymentList } from "../components/Rows";
import { api } from "../lib/api";
import { serverStatus } from "../lib/format";
import { createQuery } from "../lib/query";
import type { Dashboard } from "../lib/types";
import { t } from "../i18n";

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
      <PageHeader title={t("Painel")} description={t("O que está no ar e o que mudou por último.")} actions={<A href="/monitoring" class="btn">{t("Monitoramento")}</A>} />
      <OnboardingChecklist />
      <QueryView query={dashboard} skeletonLines={4}>
        {(data) => (
          <Show
            when={data().projects > 0}
            fallback={
              <EmptyState
                icon="rocket"
                title={t("Nada implantado ainda")}
                action={
                  <A href="/projects" class="btn btn-primary">
                    <Icon name="plus" size={16} />{t("Criar o primeiro projeto")}
                  </A>
                }
              >{t("Um projeto agrupa aplicações, bancos de dados e stacks. Crie um e faça seu primeiro deploy.")}
              </EmptyState>
            }
          >
            <div class="stack-lg">
              <div class="grid-stats">
                <Stat label={t("Projetos")} value={data().projects} />
                <Stat label={t("Aplicações")} value={data().applications} />
                <Stat label={t("Bancos de dados")} value={data().databases} />
                <Stat label={t("Stacks")} value={data().stacks} />
                <Stat label={t("Em execução")} value={data().running} />
                <Stat label={t("Com problema")} value={data().unhealthy} tone="danger" />
              </div>

              <section class="stack">
                <div class="row-between">
                  <h2>{t("Deploys recentes")}</h2>
                  <A href="/deployments">{t("Ver todos")}</A>
                </div>
                <Show
                  when={data().deployments.length > 0}
                  fallback={<p class="muted">{t("Nenhum deploy ainda. Abra uma aplicação e clique em “Implantar”.")}</p>}
                >
                  <DeploymentList deployments={data().deployments} showApplication />
                </Show>
              </section>

              <section class="stack">
                <div class="row-between">
                  <h2>{t("Servidores")}</h2>
                  <A href="/servers">{t("Gerenciar")}</A>
                </div>
                <div class="list">
                  <For each={data().servers}>
                    {(server) => (
                      <A href={`/servers/${server.id}`} class="list-item">
                        <span class="muted">
                          <Icon name="server" size={20} />
                        </span>
                        <div class="grow stack-xs">
                          <strong class="truncate">{server.name}</strong>
                          <span class="small muted truncate">
                            {server.kind === "local" ? t("Máquina local") : `${server.user}@${server.host}`}
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

function OnboardingChecklist() {
  const status = createQuery(
    () => true,
    () => api.get<{ https: boolean; backup: boolean; notifications: boolean; deployed: boolean }>("/instance/onboarding"),
    { pollMs: 30000 },
  );
  const twoFactor = createQuery(() => true, () => api.get<{ enabled: boolean }>("/me/2fa"));
  const [dismissed, setDismissed] = createSignal(readOnboardingDismissals(currentUser()?.id ?? ""));
  const dismiss = (id: string) => {
    const items = [...dismissed(), id];
    setDismissed(items);
    saveOnboardingDismissals(currentUser()?.id ?? "", items);
  };
  // The instance backup has its own warning at the top of every page, so it is not repeated here.
  const all = () => [
    { id: "https", label: t("Ativar HTTPS no painel"), href: "/settings?tab=instance", done: !!status.data()?.https },
    { id: "notifications", label: t("Configurar notificações"), href: "/notifications", done: !!status.data()?.notifications },
    { id: "deploy", label: t("Faça seu primeiro deploy"), href: "/projects", done: !!status.data()?.deployed },
    { id: "2fa", label: t("Ativar a verificação em duas etapas"), href: "/security", done: !!twoFactor.data()?.enabled },
  ];
  const items = () => all().filter((item) => !dismissed().includes(item.id));
  const doneCount = () => all().filter((item) => item.done).length;
  return (
    <Show when={status.data() && items().some((item) => !item.done)}>
      <section class="card stack">
        <div class="row-between">
          <h2>{t("Primeiros passos")}</h2>
          <span class="small muted">{t("{done} de {total} concluídos", { done: doneCount(), total: all().length })}</span>
        </div>
        <div class="list checklist">
          <For each={items()}>
            {(item) => (
              <div class="list-item" data-done={item.done ? "true" : undefined}>
                <span class="checklist-mark" aria-hidden="true">
                  <Show when={item.done}><Icon name="check" size={16} /></Show>
                </span>
                <Show when={!item.done} fallback={<span class="grow muted">{item.label}</span>}>
                  <A href={item.href} class="grow">{item.label}</A>
                </Show>
                <IconButton icon="close" label={t("Dispensar “{item}”", { item: item.label })} onClick={() => dismiss(item.id)} />
              </div>
            )}
          </For>
        </div>
        <Show when={!status.data()?.deployed && !dismissed().includes("deploy")}>
          <p class="small muted">
            {t("Para experimentar, use o exemplo Node.js com Nixpacks:")}{" "}
            <a href="https://github.com/heroku/node-js-getting-started" target="_blank" rel="noopener noreferrer">heroku/node-js-getting-started</a>
          </p>
        </Show>
      </section>
    </Show>
  );
}
