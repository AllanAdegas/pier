import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { createSignal, For, Match, Show, Switch } from "solid-js";
import { createStore } from "solid-js/store";
import { Button, IconButton } from "../components/Button";
import { EnvEditor } from "../components/EnvEditor";
import { Alert, QueryView, StatusBadge } from "../components/Feedback";
import { Icon } from "../components/Icon";
import { ContainerLogs } from "../components/LogViewer";
import { ConfirmDelete, deleteQuery, type DeleteOptions } from "../components/Modal";
import { PageHeader, Section, Tabs } from "../components/Page";
import { api } from "../lib/api";
import { resourceStatus } from "../lib/format";
import { createAction, createQuery, type Query } from "../lib/query";
import type { Container, OperationSnapshot, Stack, StackRoute } from "../lib/types";
import { createDomainDnsCheck } from "./application/DomainDnsStatus";
import { DirectorySource } from "./stack/DirectorySource";
import { ExpectedVariables } from "./stack/ExpectedVariables";
import { t } from "../i18n";
import { NumberInput, Select, TextArea, TextInput } from "../components/Form";

const TABS = [
  { id: "overview", label: t("Visão geral") },
  { id: "compose", label: t("Compose e domínios") },
  { id: "env", label: t("Variáveis") },
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
              crumbs={[{ label: t("Projetos"), href: "/projects" }, { label: data().name }]}
              title={
                <span class="row">
                  {data().name}
                  <StatusBadge status={resourceStatus(data().status)} />
                </span>
              }
              description={
                <Show when={data().routes.length > 0} fallback={data().description || t("Sem domínio público.")}>
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
                  <Button icon="refresh" disabled={busy()} onClick={() => void operate.run("restart")}>{t("Reiniciar")}
                  </Button>
                  <Button icon="stop" disabled={busy()} onClick={() => void operate.run("stop")}>{t("Parar")}
                  </Button>
                  <Button variant="primary" icon="rocket" pending={busy()} onClick={() => void operate.run("deploy")}>{t("Implantar")}
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
                    note={t("Disponíveis no compose como ${NOME}. Senhas e chaves geradas (PIER_*) ficam aqui. Implante para aplicar.")}
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
          <Section title={operation().title} description={operation().running ? t("Em andamento…") : t("Última operação.")}>
            <Show when={operation().error}>
              <Alert tone="danger">{operation().error}</Alert>
            </Show>
            <pre class="log" style={{ height: "auto", "max-height": "320px", margin: "0" }}>
              {operation().lines.join("\n")}
            </pre>
          </Section>
        )}
      </Show>
      <Section title={t("Containers")}>
        <Show when={props.containers.length > 0} fallback={<p class="muted">{t("Nenhum container. Clique em “Implantar”.")}</p>}>
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
      success: t("Stack salva. Implante para aplicar."),
      onSuccess: (saved) => {
        props.onSaved(saved);
        void services.refetch();
      },
    },
  );
  const remove = createAction(
    (options: DeleteOptions) =>
      api.delete(`/stacks/${initial.id}?${deleteQuery(options)}`),
    { success: t("Stack removida."), onSuccess: () => navigate("/projects") },
  );
  const dns = createDomainDnsCheck(() => initial.server_id);
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
      <Section title={t("Definição")}>
        <TextInput label={t("Nome")} required value={name()} onValue={setName} error={save.errors().name} />
        <Show
          when={fromDirectory}
          fallback={
            <TextArea
              label="docker-compose.yaml"
              mono
              rows={18}
              hint={t("O Pier adiciona labels de identificação e a rede do proxy aos serviços com domínio; o resto é respeitado como está.")}
              value={compose()}
              onValue={setCompose}
              error={save.errors().compose}
            />
          }
        >
          <div class="grid-2">
            <TextInput
              label={t("Pasta no servidor")}
              mono
              hint={t("O compose, o .env e os arquivos de segredo são lidos daqui a cada deploy; nada é copiado.")}
              value={directory()}
              onValue={setDirectory}
              error={save.errors().directory}
            />
            <TextInput label={t("Arquivo compose")} mono value={composeFile()} onValue={setComposeFile} error={save.errors().compose_file} />
          </div>
          <Show when={initial.project_name}>
            <p class="small muted">
              {t("Projeto compose:")} <span class="mono">{initial.project_name}</span>
            </p>
          </Show>
          <DirectorySource stackId={initial.id} />
        </Show>
      </Section>

      <Section
        title={t("Domínios")}
        description={t("Publica um serviço do compose pelo proxy, com HTTPS automático.")}
        actions={
          <Button small icon="plus" onClick={addRoute}>{t("Adicionar")}
          </Button>
        }
      >
        <Show when={routes.length > 0} fallback={<p class="muted">{t("Nenhum serviço publicado.")}</p>}>
          <div class="stack" onFocusOut={() => void dns.check(routes.map((route) => route.domain))}>
          <For each={routes}>
            {(route, index) => (
              <div class="stack-sm">
                <div class="grid-3">
                  <Select
                    label={t("Serviço")}
                    value={route.service}
                    onValue={(value) => setRoutes(index(), "service", value)}
                    options={(services.data() ?? [route.service]).map((service) => ({ value: service, label: service }))}
                  />
                  <NumberInput label={t("Porta")} min={1} max={65535} value={route.port} onValue={(value) => setRoutes(index(), "port", value)} />
                  <TextInput
                    label={t("Domínio")}
                    mono
                    placeholder={t("https://servico.exemplo.com")}
                    value={route.domain}
                    onValue={(value) => setRoutes(index(), "domain", value)}
                  />
                </div>
                <div class="row-between">
                  <span class="field-error">{save.errors()[`routes.${index()}`]}</span>
                  <IconButton
                    icon="trash"
                    label={t("Remover domínio")}
                    onClick={() => setRoutes((list) => list.filter((_, position) => position !== index()))}
                  />
                </div>
              </div>
            )}
          </For>
          </div>
          <dns.View />
        </Show>
      </Section>

      <div class="form-actions">
        <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar stack")}
        </Button>
      </div>

      <Section title={t("Zona de perigo")} description={t("Derruba todos os containers da stack e remove o registro.")}>
        <div>
          <Button variant="danger" icon="trash" onClick={() => setDeleting(true)}>{t("Remover stack")}
          </Button>
        </div>
      </Section>
      <Show when={deleting()}>
        <ConfirmDelete
          title={t("Remover stack")}
          resourceName={initial.name}
          environmentId={initial.environment_id}
          description={
            fromDirectory
              ? t("Todos os serviços da stack saem do ar. A pasta do projeto e os arquivos dela não são tocados.")
              : t("Todos os serviços da stack saem do ar. Esta ação não pode ser desfeita.")
          }
          volumesLabel={t("Apagar também os volumes da stack")}
          allowForce
          pending={remove.pending()}
          onConfirm={(options) => void remove.run(options)}
          onClose={() => setDeleting(false)}
        />
      </Show>
    </form>
  );
}
