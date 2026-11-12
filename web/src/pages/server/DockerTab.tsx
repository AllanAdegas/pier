import { createMemo, createSignal, For, Match, Show, Switch } from "solid-js";
import { Button } from "../../components/Button";
import { Alert, EmptyState, QueryView } from "../../components/Feedback";
import { LogStream } from "../../components/LogViewer";
import { ConfirmDelete, Modal } from "../../components/Modal";
import { Tabs } from "../../components/Page";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { Images, Networks, Volumes } from "./DockerObjects";
import type { DockerContainer, Project, Resources } from "../../lib/types";
import { type ContainerDialog, ContainerRow } from "./ContainerRow";
import { groupContainers, placementLabel, placements } from "./containerGroups";
import { Trans } from "../../i18n/Trans";
import { t } from "../../i18n";
import { Checkbox, LinesInput, Select, TextInput } from "../../components/Form";

const SECTIONS = [
  { id: "containers", label: t("Containers") },
  { id: "images", label: t("Imagens") },
  { id: "volumes", label: t("Volumes") },
  { id: "networks", label: t("Redes") },
];

/**
 * Plain Docker: everything that exists on the server, including what Pier
 * did not create. Docker is the source of truth here; nothing is stored.
 */
export function DockerTab(props: { serverId: string }) {
  const [section, setSection] = createSignal("containers");
  const base = () => `/servers/${props.serverId}/docker`;
  return (
    <div class="stack">
      <Tabs tabs={SECTIONS} selected={section()} onSelect={setSection} />
      <Switch>
        <Match when={section() === "containers"}>
          <Containers base={base()} />
        </Match>
        <Match when={section() === "images"}>
          <Images base={base()} />
        </Match>
        <Match when={section() === "volumes"}>
          <Volumes base={base()} />
        </Match>
        <Match when={section() === "networks"}>
          <Networks base={base()} />
        </Match>
      </Switch>
    </div>
  );
}

type Grouping = "project" | "list";

const GROUPING_KEY = "pier.docker.grouping";

function savedGrouping(): Grouping {
  try {
    return localStorage.getItem(GROUPING_KEY) === "list" ? "list" : "project";
  } catch {
    return "project";
  }
}

function Containers(props: { base: string }) {
  const containers = createQuery(
    () => props.base,
    (base) => api.get<DockerContainer[]>(`${base}/containers`),
    { pollMs: 6000 },
  );
  const [filter, setFilter] = createSignal("");
  const [showStopped, setShowStopped] = createSignal(true);
  const [dialog, setDialog] = createSignal<ContainerDialog | null>(null);
  const [grouping, setGrouping] = createSignal<Grouping>(savedGrouping());
  const chooseGrouping = (value: string) => {
    const next: Grouping = value === "list" ? "list" : "project";
    setGrouping(next);
    try {
      localStorage.setItem(GROUPING_KEY, next);
    } catch {
      // Private windows may refuse storage; the choice just is not remembered.
    }
  };
  // Pier resources and projects, to know the project of each managed container.
  const resources = createQuery(
    () => grouping() === "project",
    () => api.get<Resources>("/resources"),
  );
  const projects = createQuery(
    () => grouping() === "project",
    () => api.get<Project[]>("/projects"),
  );
  const index = createMemo(() => placements(resources.data(), projects.data()));

  const visible = createMemo(() => {
    const term = filter().trim().toLowerCase();
    return (containers.data() ?? []).filter(
      (container) =>
        (showStopped() || container.state === "running") &&
        (term === "" || `${container.name} ${container.image} ${container.compose_project}`.toLowerCase().includes(term)),
    );
  });
  const groups = createMemo(() => groupContainers(visible(), index()));
  const act = createAction(
    (container: DockerContainer, action: string) => api.post(`${props.base}/containers/${container.id}/actions/${action}`),
    { success: t("Operação concluída."), onSuccess: () => void containers.refetch() },
  );
  const remove = createAction((container: DockerContainer) => api.delete(`${props.base}/containers/${container.id}?force=1`), {
    success: t("Container removido."),
    onSuccess: () => {
      setDialog(null);
      void containers.refetch();
    },
  });
  const selected = <K extends ContainerDialog["kind"]>(kind: K) => {
    const current = dialog();
    return current?.kind === kind && "container" in current ? current.container : undefined;
  };

  return (
    <div class="stack">
      <div class="row-between">
        <div class="row grow">
          <div class="grow" style={{ "max-width": "360px" }}>
            <TextInput
              label={t("Filtrar")}
              type="search"
              placeholder={t("nome, imagem ou projeto compose")}
              value={filter()}
              onValue={setFilter}
            />
          </div>
          <Checkbox label={t("Mostrar parados")} checked={showStopped()} onChange={setShowStopped} />
          <div style={{ "min-width": "160px" }}>
            <Select
              label={t("Agrupar")}
              value={grouping()}
              onValue={chooseGrouping}
              options={[
                { value: "project", label: t("Por projeto") },
                { value: "list", label: t("Lista única") },
              ]}
            />
          </div>
        </div>
        <Button variant="primary" icon="play" onClick={() => setDialog({ kind: "run" })}>{t("Executar container")}
        </Button>
      </div>
      <QueryView query={containers}>
        {() => (
          <Show
            when={visible().length > 0}
            fallback={
              <EmptyState icon="box" title={t("Nenhum container")}>{t("Nada corresponde ao filtro, ou o servidor ainda não tem containers.")}
              </EmptyState>
            }
          >
            <Show
              when={grouping() === "project"}
              fallback={
                <div class="list">
                  <For each={visible()}>
                    {(container) => (
                      <ContainerRow terminalBase={props.base}
                        container={container}
                        act={act}
                        onDialog={setDialog}
                        placement={placementLabel(container, index())}
                      />
                    )}
                  </For>
                </div>
              }
            >
              <div class="stack">
                <For each={groups()}>
                  {(group) => (
                    <details class="card stack-sm" open style={{ padding: "var(--space-3)" }}>
                      <summary class="row-between" style={{ cursor: "pointer" }}>
                        <span class="stack-sm" style={{ gap: "0" }}>
                          <strong>{group.title}</strong>
                          <span class="small muted">{group.kind}</span>
                        </span>
                        <span class="small muted">
                          {t("{running} de {count} rodando", { running: group.running, count: group.containers.length })}
                        </span>
                      </summary>
                      <div class="list">
                        <For each={group.containers}>
                          {(container) => (
                            <ContainerRow terminalBase={props.base} hideComposeTag
                              container={container}
                              act={act}
                              onDialog={setDialog}
                              placement={placementLabel(container, index())}
                            />
                          )}
                        </For>
                      </div>
                    </details>
                  )}
                </For>
              </div>
            </Show>
          </Show>
        )}
      </QueryView>

      <Show when={dialog()?.kind === "run"}>
        <RunForm
          base={props.base}
          onClose={() => setDialog(null)}
          onStarted={() => {
            setDialog(null);
            void containers.refetch();
          }}
        />
      </Show>
      <Show when={selected("logs")}>
        {(container) => (
          <Modal title={t("Logs — {name}", { name: container().name })} onClose={() => setDialog(null)} wide>
            <LogStream path={`${props.base}/containers/${container().id}/logs?tail=300`} />
          </Modal>
        )}
      </Show>
      <Show when={selected("inspect")}>
        {(container) => <InspectModal base={props.base} container={container()} onClose={() => setDialog(null)} />}
      </Show>
      <Show when={selected("exec")}>
        {(container) => <ExecModal base={props.base} container={container()} onClose={() => setDialog(null)} />}
      </Show>
      <Show when={selected("remove")}>
        {(container) => (
          <ConfirmDelete
            title={t("Remover container")}
            resourceName={container().name}
            description={t("O container é parado e removido. Volumes nomeados são mantidos; dados gravados só no container são perdidos.")}
            pending={remove.pending()}
            onConfirm={() => void remove.run(container())}
            onClose={() => setDialog(null)}
          />
        )}
      </Show>
    </div>
  );
}

function RunForm(props: { base: string; onClose: () => void; onStarted: () => void }) {
  const [name, setName] = createSignal("");
  const [image, setImage] = createSignal("");
  const [command, setCommand] = createSignal("");
  const [ports, setPorts] = createSignal<string[]>([]);
  const [env, setEnv] = createSignal<string[]>([]);
  const [volumes, setVolumes] = createSignal<string[]>([]);
  const [network, setNetwork] = createSignal("");
  const [restart, setRestart] = createSignal("unless-stopped");
  const run = createAction(
    () =>
      api.post(`${props.base}/containers`, {
        name: name(),
        image: image(),
        command: command(),
        ports: ports(),
        env: env(),
        volumes: volumes(),
        network: network(),
        restart: restart(),
      }),
    { success: t("Container iniciado."), onSuccess: props.onStarted },
  );
  const errors = run.errors;

  return (
    <Modal title={t("Executar container")} onClose={props.onClose} wide>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void run.run();
        }}
      >
        <Alert tone="info">
          <Trans
            text="Equivale a {command}. O container não vira um recurso do Pier: sem domínio, deploy ou backup automáticos. Para isso, crie uma aplicação em um projeto."
            values={{ command: <code>docker run -d</code> }}
          />
        </Alert>
        <div class="grid-2">
          <TextInput
            label={t("Nome")}
            required
            mono
            placeholder="redis-cache"
            value={name()}
            onValue={setName}
            error={errors().name}
          />
          <TextInput
            label={t("Imagem")}
            required
            mono
            placeholder="redis:7.4-alpine"
            value={image()}
            onValue={setImage}
            error={errors().image}
          />
        </div>
        <TextInput
          label={t("Comando")}
          mono
          placeholder={t("padrão da imagem")}
          hint={t("Opcional. Separado por espaços; cada palavra vira um argumento.")}
          value={command()}
          onValue={setCommand}
        />
        <div class="grid-2">
          <LinesInput
            label={t("Portas")}
            hint={t("host:container, uma por linha.")}
            placeholder="8080:80"
            value={ports()}
            onValue={setPorts}
            error={errors().ports}
          />
          <LinesInput
            label={t("Volumes")}
            hint={t("volume:/caminho ou /host:/caminho[:ro].")}
            placeholder={t("dados:/data")}
            value={volumes()}
            onValue={setVolumes}
            error={errors().volumes}
          />
        </div>
        <LinesInput
          label={t("Variáveis de ambiente")}
          hint={t("CHAVE=valor, uma por linha.")}
          placeholder={t("TZ=America/Sao_Paulo")}
          value={env()}
          onValue={setEnv}
          error={errors().env}
        />
        <div class="grid-2">
          <TextInput
            label={t("Rede")}
            mono
            placeholder={t("bridge (padrão)")}
            value={network()}
            onValue={setNetwork}
            error={errors().network}
          />
          <Select
            label={t("Reinício")}
            value={restart()}
            onValue={setRestart}
            options={[
              { value: "unless-stopped", label: t("Sempre, exceto se parado manualmente") },
              { value: "always", label: t("Sempre") },
              { value: "on-failure", label: t("Só em falha") },
              { value: "no", label: t("Nunca") },
            ]}
          />
        </div>
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" icon="play" pending={run.pending()}>{t("Executar")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function InspectModal(props: { base: string; container: DockerContainer; onClose: () => void }) {
  const document = createQuery(
    () => props.container.id,
    (id) => api.get<unknown>(`${props.base}/containers/${id}`),
  );
  return (
    <Modal title={t("Inspecionar — {name}", { name: props.container.name })} onClose={props.onClose} wide>
      <QueryView query={document}>
        {(data) => (
          <pre class="log" style={{ margin: "0" }}>
            {JSON.stringify(data(), null, 2)}
          </pre>
        )}
      </QueryView>
    </Modal>
  );
}

function ExecModal(props: { base: string; container: DockerContainer; onClose: () => void }) {
  const [command, setCommand] = createSignal("");
  const [output, setOutput] = createSignal<string | null>(null);
  const exec = createAction(
    () => api.post<{ output: string }>(`${props.base}/containers/${props.container.id}/exec`, { command: command() }),
    { onSuccess: (result) => setOutput(result.output || t("(sem saída)")) },
  );
  return (
    <Modal title={t("Executar comando — {name}", { name: props.container.name })} onClose={props.onClose} wide>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void exec.run();
        }}
      >
        <TextInput
          label={t("Comando")}
          mono
          required
          placeholder="ls -la /app"
          hint={t("Roda com sh -c dentro do container e devolve a saída (limite de 60 s). Não é um terminal interativo.")}
          value={command()}
          onValue={setCommand}
        />
        <div class="form-actions">
          <Button type="submit" variant="primary" icon="terminal" pending={exec.pending()}>{t("Executar")}
          </Button>
        </div>
        <Show when={output() !== null}>
          <pre class="log" style={{ height: "auto", "max-height": "360px", margin: "0" }}>
            {output()}
          </pre>
        </Show>
      </form>
    </Modal>
  );
}
