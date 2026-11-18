import { A } from "@solidjs/router";
import { For, Show } from "solid-js";
import { BUILD_PACK_LABEL, deploymentStatus, formatRelative, resourceStatus, shortSha, TRIGGER_LABEL } from "../lib/format";
import type { Deployment, Resources } from "../lib/types";
import { StatusBadge } from "./Feedback";
import { Icon, type IconName } from "./Icon";
import { t } from "../i18n";

interface ResourceRow {
  href: string;
  icon: IconName;
  name: string;
  detail: string;
  status: string;
}

function toRows(resources: Resources): ResourceRow[] {
  return [
    ...resources.applications.map((app) => ({
      href: `/applications/${app.id}`,
      icon: "box" as const,
      name: app.name,
      detail: app.config.network.domains[0] ?? BUILD_PACK_LABEL[app.config.build.pack] ?? t("Aplicação"),
      status: app.status,
    })),
    ...resources.databases.map((database) => ({
      href: `/databases/${database.id}`,
      icon: "database" as const,
      name: database.name,
      detail: database.config.image,
      status: database.status,
    })),
    ...resources.stacks.map((stack) => ({
      href: `/stacks/${stack.id}`,
      icon: "layers" as const,
      name: stack.name,
      detail: stack.routes[0]?.domain ?? (stack.template || t("Compose próprio")),
      status: stack.status,
    })),
  ];
}

export function resourceCount(resources: Resources): number {
  return resources.applications.length + resources.databases.length + resources.stacks.length;
}

/** Applications, databases and stacks of an environment as one list. */
export function ResourceList(props: { resources: Resources }) {
  return (
    <div class="list">
      <For each={toRows(props.resources)}>
        {(row) => (
          <A href={row.href} class="list-item">
            <span class="muted">
              <Icon name={row.icon} size={20} />
            </span>
            <div class="grow stack-xs">
              <strong class="truncate">{row.name}</strong>
              <span class="small muted truncate">{row.detail}</span>
            </div>
            <StatusBadge status={resourceStatus(row.status)} />
            <span class="muted">
              <Icon name="chevron" size={16} />
            </span>
          </A>
        )}
      </For>
    </div>
  );
}

/** Deployments as a list; each row opens the deployment log. */
export function DeploymentList(props: { deployments: Deployment[]; showApplication?: boolean }) {
  return (
    <div class="list">
      <For each={props.deployments}>
        {(deployment) => (
          <A href={`/deployments/${deployment.id}`} class="list-item">
            <StatusBadge status={deploymentStatus(deployment.status)} />
            <div class="grow stack-xs">
              <strong class="truncate">
                <Show when={props.showApplication} fallback={deployment.commit_message || TRIGGER_LABEL[deployment.trigger]}>
                  {deployment.application_name}
                </Show>
              </strong>
              <span class="small muted truncate">
                {TRIGGER_LABEL[deployment.trigger]}
                <Show when={deployment.commit_sha}>
                  {" · "}
                  <span class="mono">{shortSha(deployment.commit_sha)}</span>
                </Show>
                <Show when={props.showApplication && deployment.commit_message}> · {deployment.commit_message}</Show>
              </span>
            </div>
            <span class="small muted" style={{ "white-space": "nowrap" }}>
              {formatRelative(deployment.created_at)}
            </span>
            <span class="muted">
              <Icon name="chevron" size={16} />
            </span>
          </A>
        )}
      </For>
    </div>
  );
}
