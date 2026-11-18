import { TerminalButton } from "../../components/Terminal";
import { A } from "@solidjs/router";
import { For, Show } from "solid-js";
import { Alert, EmptyState, StatusBadge } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { MetricsHistory } from "../../components/charts/MetricsHistory";
import { DeploymentList } from "../../components/Rows";
import { BUILD_PACK_LABEL } from "../../lib/format";
import type { Application, Container, Deployment } from "../../lib/types";
import { Trans } from "../../i18n/Trans";
import { t } from "../../i18n";

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
 <TerminalButton endpoint={`/applications/${props.application.id}/terminal`} />
      <Show when={neverDeployed()}>
        <Alert tone="info">
          <Trans
            text="Esta aplicação ainda não foi implantada. Revise a configuração e clique em {action}."
            values={{ action: <strong>{t("Implantar")}</strong> }}
          />
        </Alert>
      </Show>

      <Section title={t("Resumo")}>
        <dl class="kv">
          <dt>{t("Origem")}</dt>
          <dd class="mono">
            <Show when={config().build.pack === "image"} fallback={`${config().source.repository} (${config().source.branch})`}>
              {config().source.image}
            </Show>
          </dd>
          <dt>{t("Build")}</dt>
          <dd>{BUILD_PACK_LABEL[config().build.pack]}</dd>
          <dt>{t("Porta")}</dt>
          <dd>{config().network.port}</dd>
          <dt>{t("Imagem em execução")}</dt>
          <dd class="mono">{props.application.current_image || "—"}</dd>
          <dt>{t("Endereço interno")}</dt>
          <dd class="mono">
            {props.application.id}:{config().network.port}
          </dd>
          <dt>{t("Deploy automático")}</dt>
          <dd>{config().auto_deploy ? t("Ativado (push na branch)") : t("Desativado")}</dd>
        </dl>
      </Section>

      <Section title={t("Containers")} description={t("Durante um rolling update, a versão antiga e a nova aparecem juntas.")}>
        <Show
          when={props.containers.length > 0}
          fallback={<p class="muted">{t("Nenhum container no servidor.")}</p>}
        >
          <div class="list">
            <For each={props.containers}>
              {(container) => (
                <div class="list-item">
                  <div class="grow stack-xs">
                    <span class="row">
                      <span class="mono truncate">{container.name}</span>
                      <Show when={container.process}>
                        <span class="tag">{container.process}</span>
                      </Show>
                    </span>
                    <span class="small muted mono truncate">{container.image}</span>
                  </div>
                  <StatusBadge status={containerTone(container)} />
                </div>
              )}
            </For>
          </div>
        </Show>
      </Section>

      <MetricsHistory path={`/resources/application/${props.application.id}/metrics/history`} />

      <section class="stack">
        <div class="row-between">
          <h2>{t("Últimos deploys")}</h2>
          <A href="?tab=deploys">{t("Ver histórico")}</A>
        </div>
        <Show
          when={props.deployments.length > 0}
          fallback={
            <EmptyState icon="rocket" title={t("Nenhum deploy")}>{t("O histórico aparece aqui depois do primeiro deploy.")}
            </EmptyState>
          }
        >
          <DeploymentList deployments={props.deployments.slice(0, 5)} />
        </Show>
      </section>
    </div>
  );
}
