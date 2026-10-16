import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { createSignal, For, Match, Show, Switch } from "solid-js";
import { createStore } from "solid-js/store";
import { Button, IconButton } from "../components/Button";
import { EnvEditor } from "../components/EnvEditor";
import { Alert, QueryView, StatusBadge } from "../components/Feedback";
import { NumberInput, Select, TextArea, TextInput } from "../components/Form";
import { Icon } from "../components/Icon";
import { ContainerLogs } from "../components/LogViewer";
import { ConfirmDelete } from "../components/Modal";
import { PageHeader, Section, Tabs } from "../components/Page";
import { api } from "../lib/api";
import { resourceStatus } from "../lib/format";
import { createAction, createQuery, type Query } from "../lib/query";
import type { Container, OperationSnapshot, Stack, StackRoute } from "../lib/types";
import { DirectorySource } from "./stack/DirectorySource";
import { ExpectedVariables } from "./stack/ExpectedVariables";

const TABS = [
  { id: "overview", label: "Visão geral" },
  { id: "compose", label: "Compose e domínios" },
  { id: "env", label: "Variáveis" },
  { id: "logs", label: "Logs" },
];

export function StackPage() {
  const params = useParams<{ id: string }>();
  const [search, setSearch] = useSearchParams<{ tab?: string }>();
  const tab = () => search.tab ?? "overview";

  const stack = createQuery(
    () => params.id,
    (id) => api.get<Stack>(`/stacks/${id}`),
    { pollMs: 5000 },
  );
  // Typed explicitly because the polling interval reads the query itself.
  const operation: Query<OperationSnapshot> = createQuery(
    () => params.id,
    (id) => api.get<OperationSnapshot>(`/stacks/${id}/operation`),
    { pollMs: (): number => (operation.data()?.running ? 1500 : 6000) },
  );
  const containers = createQuery(
    () => (tab() === "overview" || tab() === "logs") && params.id,
    (id) => api.get<Container[]>(`/stacks/${id}/containers`),
    { pollMs: 6000 },
  );

  const operate = createAction(
    (action: "deploy" | "stop" | "restart") => api.post<OperationSnapshot>(`/stacks/${params.id}/actions/${action}`),
    {
      onSuccess: (snapshot) => {
        operation.set(snapshot);
        setSearch({ tab: "overview" });
      },
    },
  );
  const busy = () => operate.pending() || (operation.data()?.running ?? false);

  return (
    <div class="page">
      <QueryView query={stack}>
        {(data) => (
          <>
            <PageHeader
              crumbs={[{ label: "Projetos", href: "/projects" }, { label: data().name }]}
              title={
                <span class="row">
                  {data().name}
                  <StatusBadge status={resourceStatus(data().status)} />
                </span>
              }
              description={
                <Show when={data().routes.length > 0} fallback={data().description || "Sem domínio público."}>
                  <span class="row">
                    <For each={data().routes}>
                      {(route) => (
                        <a href={route.domain} target="_blank" rel="noreferrer">
                          {route.domain} <Icon name="external" size={12} />
                        </a>
                      )}
                    </For>
                  </span>
                </Show>
              }
              actions={
                <>
                  <Button icon="refresh" disabled={busy()} onClick={() => void operate.run("restart")}>
                    Reiniciar
                  </Button>
                  <Button icon="stop" disabled={busy()} onClick={() => void operate.run("stop")}>
                    Parar
                  </Button>
                  <Button variant="primary" icon="rocket" pending={busy()} onClick={() => void operate.run("deploy")}>
                    Implantar
                  </Button>
                </>
              }
            />
            <Tabs tabs={TABS} selected={tab()} onSelect={(id) => setSearch({ tab: id })} />
            <Switch>
              <Match when={tab() === "overview"}>
                <OverviewTab containers={containers.data() ?? []} operation={operation.data()} />
              </Match>
              <Match when={tab() === "compose"}>
                <ComposeTab stack={data()} onSaved={(saved) => stack.set(saved)} />
              </Match>
              <Match when={tab() === "env"}>
                <div class="stack-lg">
                  <ExpectedVariables stackId={data().id} />
                  <EnvEditor
                    basePath={`/stacks/${data().id}`}
                    note="Disponíveis no compose como ${NOME}. Senhas e chaves geradas (PIER_* e SERVICE_*) ficam aqui. Implante para aplicar."
                  />
                </div>
              </Match>
              <Match when={tab() === "logs"}>
                <QueryView query={containers}>
                  {(list) => <ContainerLogs serverId={data().server_id} containers={list()} />}
                </QueryView>
              </Match>
            </Switch>
          </>
        )}
      </QueryView>
    </div>
  );
}

function OverviewTab(props: { containers: Container[]; operation?: OperationSnapshot }) {
  return (
    <div class="stack-lg">
      <Show when={props.operation && props.operation.lines.length > 0 ? props.operation : undefined}>
        {(operation) => (
          <Section title={operation().title} description={operation().running ? "Em andamento…" : "Última operação."}>
            <Show when={operation().error}>
              <Alert tone="danger">{operation().error}</Alert>
            </Show>
            <pre class="log" style={{ height: "auto", "max-height": "320px", margin: "0" }}>
              {operation().lines.join("\n")}
            </pre>
          </Section>
        )}
      </Show>
      <Section title="Containers">
        <Show when={props.containers.length > 0} fallback={<p class="muted">Nenhum container. Clique em “Implantar”.</p>}>
          <div class="list">
            <For each={props.containers}>
              {(container) => (
                <div class="list-item">
                  <div class="grow stack-sm" style={{ gap: "0" }}>
                    <span class="mono truncate">{container.name}</span>
                    <span class="small muted mono truncate">{container.image}</span>
                  </div>
                  <StatusBadge
                    status={{
                      label: container.status,
                      tone: container.state !== "running" ? "neutral" : container.status.includes("unhealthy") ? "danger" : "success",
                    }}
                  />
                </div>
              )}
            </For>
          </div>
        </Show>
      </Section>
    </div>
  );
}

function ComposeTab(props: { stack: Stack; onSaved: (stack: Stack) => void }) {
  const navigate = useNavigate();
  const initial = props.stack;
  const [name, setName] = createSignal(initial.name);
  const [compose, setCompose] = createSignal(initial.compose);
  const fromDirectory = initial.directory !== "";
  const [directory, setDirectory] = createSignal(initial.directory);
  const [composeFile, setComposeFile] = createSignal(initial.compose_file);
  const [routes, setRoutes] = createStore<StackRoute[]>(initial.routes.map((route) => ({ ...route })));
  const [deleting, setDeleting] = createSignal(false);
  const services = createQuery(
    () => initial.id,
    (id) => api.get<string[]>(`/stacks/${id}/services`),
  );

  const save = createAction(
    () =>
      api.patch<Stack>(`/stacks/${initial.id}`, {
        name: name(),
        description: initial.description,
        compose: compose(),
        directory: directory(),
        compose_file: composeFile(),
        routes,
      }),
    {
      success: "Stack salva. Implante para aplicar.",
      onSuccess: (saved) => {
        props.onSaved(saved);
        void services.refetch();
      },
    },
  );
  const remove = createAction(
    (options: { volumes: boolean; force: boolean }) =>
      api.delete(`/stacks/${initial.id}?volumes=${options.volumes ? 1 : 0}&force=${options.force ? 1 : 0}`),
    { success: "Stack removida.", onSuccess: () => navigate("/projects") },
  );
  // A new domain usually publishes the same service as the existing one, so it
  // starts from its service and port (the right port is rarely 80).
  const addRoute = () =>
    setRoutes(routes.length, {
      service: routes[0]?.service ?? services.data()?.[0] ?? "",
      port: routes[0]?.port ?? 80,
      domain: "",
    });

  return (
    <form
      class="stack-lg"
      onSubmit={(event) => {
        event.preventDefault();
        void save.run();
      }}
    >
      <Section title="Definição">
        <TextInput label="Nome" required value={name()} onValue={setName} error={save.errors().name} />
        <Show
          when={fromDirectory}
          fallback={
            <TextArea
              label="docker-compose.yaml"
              mono
              rows={18}
              hint="O Pier adiciona labels de identificação e a rede do proxy aos serviços com domínio; o resto é respeitado como está."
              value={compose()}
              onValue={setCompose}
              error={save.errors().compose}
            />
          }
        >
          <div class="grid-2">
            <TextInput
              label="Pasta no servidor"
              mono
              hint="O compose, o .env e os arquivos de segredo são lidos daqui a cada deploy; nada é copiado."
              value={directory()}
              onValue={setDirectory}
              error={save.errors().directory}
            />
            <TextInput label="Arquivo compose" mono value={composeFile()} onValue={setComposeFile} error={save.errors().compose_file} />
          </div>
          <Show when={initial.project_name}>
            <p class="small muted">
              Projeto compose: <span class="mono">{initial.project_name}</span>
            </p>
          </Show>
          <DirectorySource stackId={initial.id} />
        </Show>
      </Section>

      <Section
        title="Domínios"
        description="Publica um serviço do compose pelo proxy, com HTTPS automático."
        actions={
          <Button small icon="plus" onClick={addRoute}>
            Adicionar
          </Button>
        }
      >
        <Show when={routes.length > 0} fallback={<p class="muted">Nenhum serviço publicado.</p>}>
          <For each={routes}>
            {(route, index) => (
              <div class="stack-sm">
                <div class="grid-3">
                  <Select
                    label="Serviço"
                    value={route.service}
                    onValue={(value) => setRoutes(index(), "service", value)}
                    options={(services.data() ?? [route.service]).map((service) => ({ value: service, label: service }))}
                  />
                  <NumberInput label="Porta" min={1} max={65535} value={route.port} onValue={(value) => setRoutes(index(), "port", value)} />
                  <TextInput
                    label="Domínio"
                    mono
                    placeholder="https://servico.exemplo.com"
                    value={route.domain}
                    onValue={(value) => setRoutes(index(), "domain", value)}
                  />
                </div>
                <div class="row-between">
                  <span class="field-error">{save.errors()[`routes.${index()}`]}</span>
                  <IconButton
                    icon="trash"
                    label="Remover domínio"
                    onClick={() => setRoutes((list) => list.filter((_, position) => position !== index()))}
                  />
                </div>
              </div>
            )}
          </For>
        </Show>
      </Section>

      <div class="form-actions">
        <Button type="submit" variant="primary" pending={save.pending()}>
          Salvar stack
        </Button>
      </div>

      <Section title="Zona de perigo" description="Derruba todos os containers da stack e remove o registro.">
        <div>
          <Button variant="danger" icon="trash" onClick={() => setDeleting(true)}>
            Remover stack
          </Button>
        </div>
      </Section>
      <Show when={deleting()}>
        <ConfirmDelete
          title="Remover stack"
          resourceName={initial.name}
          description={
            fromDirectory
              ? "Todos os serviços da stack saem do ar. A pasta do projeto e os arquivos dela não são tocados."
              : "Todos os serviços da stack saem do ar. Esta ação não pode ser desfeita."
          }
          volumesLabel="Apagar também os volumes da stack"
          allowForce
          pending={remove.pending()}
          onConfirm={(options) => void remove.run(options)}
          onClose={() => setDeleting(false)}
        />
      </Show>
    </form>
  );
}
