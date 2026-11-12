import { A } from "@solidjs/router";
import { Show } from "solid-js";
import { IconButton } from "../../components/Button";
import { StatusBadge } from "../../components/Feedback";
import { containerStatus } from "../../lib/format";
import type { Action } from "../../lib/query";
import type { DockerContainer } from "../../lib/types";
import { t } from "../../i18n";

export type ContainerDialog = { kind: "run" } | { kind: "logs" | "inspect" | "exec" | "remove"; container: DockerContainer };

const OWNER_LABEL: Record<string, string> = {
  application: t("aplicação"),
  database: t("banco"),
  stack: "stack",
  proxy: "proxy",
  cluster: "cluster",
};

const OWNER_PATH: Record<string, string> = {
  application: "/applications/",
  database: "/databases/",
  stack: "/stacks/",
  cluster: "/clusters/",
};

export /** One container with its owner, state and actions. */
function ContainerRow(props: {
  container: DockerContainer;
  placement: string;
  /** In the per-project view the group already names the compose project. */
  hideComposeTag?: boolean;
  act: Action<[DockerContainer, string], unknown>;
  onDialog: (dialog: ContainerDialog) => void;
}) {
  return (
    <div class="list-item" style={{ "flex-wrap": "wrap" }}>
      <div class="stack-sm" style={{ gap: "0", flex: "1 1 220px", "min-width": "0" }}>
        <span class="mono truncate">
          <strong>{props.container.name}</strong>
        </span>
        <span class="small muted mono truncate">
          {props.container.image}
          <Show when={props.container.ports}> · {props.container.ports}</Show>
        </span>
      </div>
      <Show when={props.placement}>
        <span class="tag">{props.placement}</span>
      </Show>
      <Show when={props.container.managed && props.container.owner}>
        {(owner) => (
          <Show
            when={OWNER_PATH[owner().type]}
            fallback={<span class="tag">Pier: {OWNER_LABEL[owner().type] ?? owner().type}</span>}
          >
            <A class="tag" href={OWNER_PATH[owner().type] + owner().id}>
              Pier: {OWNER_LABEL[owner().type]}
            </A>
          </Show>
        )}
      </Show>
      <Show when={!props.hideComposeTag && !props.container.managed && props.container.compose_project}>
        <span class="tag">compose: {props.container.compose_project}</span>
      </Show>
      <StatusBadge status={containerStatus(props.container)} />
      <div class="row" style={{ gap: "var(--space-1)" }}>
        <Show
          when={props.container.state === "running"}
          fallback={
            <IconButton
              icon="play"
              label={t("Iniciar {name}", { name: props.container.name })}
              pending={props.act.pending()}
              onClick={() => void props.act.run(props.container, "start")}
            />
          }
        >
          <IconButton
            icon="refresh"
            label={t("Reiniciar {name}", { name: props.container.name })}
            pending={props.act.pending()}
            onClick={() => void props.act.run(props.container, "restart")}
          />
          <IconButton
            icon="stop"
            label={t("Parar {name}", { name: props.container.name })}
            pending={props.act.pending()}
            onClick={() => void props.act.run(props.container, "stop")}
          />
        </Show>
        <IconButton
          icon="lines"
          label={t("Logs de {name}", { name: props.container.name })}
          onClick={() => props.onDialog({ kind: "logs", container: props.container })}
        />
        <IconButton
          icon="eye"
          label={t("Inspecionar {name}", { name: props.container.name })}
          onClick={() => props.onDialog({ kind: "inspect", container: props.container })}
        />
        <IconButton
          icon="terminal"
          label={t("Executar comando em {name}", { name: props.container.name })}
          onClick={() => props.onDialog({ kind: "exec", container: props.container })}
        />
        <IconButton
          icon="trash"
          label={props.container.managed ? t("Gerenciado pelo Pier: remova pelo recurso") : t("Remover {name}", { name: props.container.name })}
          disabled={props.container.managed}
          onClick={() => props.onDialog({ kind: "remove", container: props.container })}
        />
      </div>
    </div>
  );
}
