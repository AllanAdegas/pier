import { A, useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { createSignal, For, Match, Show, Switch } from "solid-js";
import { createStore } from "solid-js/store";
import { Button } from "../components/Button";
import { Alert, QueryView, StatusBadge } from "../components/Feedback";
import { Icon } from "../components/Icon";
import { ConfirmDelete, Modal } from "../components/Modal";
import { PageHeader, Section, Tabs } from "../components/Page";
import { api } from "../lib/api";
import { formatRelative, serverStatus } from "../lib/format";
import { createAction, createQuery } from "../lib/query";
import type { PrivateKey, ProxyInfo, ProxySettings, Server, ServerInput } from "../lib/types";
import { ServerMonitoring } from "../components/ServerMonitoring";
import { InfrastructureTabs } from "./Clusters";
import { ServerChecks } from "./server/ServerChecks";
import { DockerTab } from "./server/DockerTab";
import { EdgeFields, EdgeSyncCard } from "./server/EdgeProxy";
import { Trans } from "../i18n/Trans";
import { t } from "../i18n";
import { NumberInput, Select, TextArea, TextInput } from "../components/Form";

function toInput(server?: Server): ServerInput {
  return {
    name: server?.name ?? "",
    description: server?.description ?? "",
    host: server?.host ?? "",
    port: server?.port ?? 22,
    user: server?.user ?? "root",
    private_key_id: server?.private_key_id ?? "",
    data_dir: server?.data_dir ?? "",
    proxy: proxySettings(server),
  };
}

function proxySettings(server?: Server): ProxySettings {
  const proxy: Partial<ProxySettings> = server?.proxy ?? {};
  return {
    http_port: proxy.http_port ?? 80,
    https_port: proxy.https_port ?? 443,
    acme_email: proxy.acme_email ?? "",
    edge: {
      container: proxy.edge?.container ?? "",
      caddyfile: proxy.edge?.caddyfile ?? "",
    },
  };
}

/** Connection fields shared by the "add server" dialog and the settings tab. */
function ConnectionFields(props: {
  input: ServerInput;
  set: <K extends keyof ServerInput>(key: K, value: ServerInput[K]) => void;
  errors: Record<string, string>;
  keys: PrivateKey[];
}) {
  return (
    <>
      <div class="grid-2">
        <TextInput
          label="Host"
          required
          mono
          placeholder="203.0.113.10"
          value={props.input.host}
          onValue={(value) => props.set("host", value)}
          error={props.errors.host}
        />
        <NumberInput
          label={t("Porta SSH")}
          min={1}
          max={65535}
          value={props.input.port}
          onValue={(value) => props.set("port", value)}
          error={props.errors.port}
        />
      </div>
      <div class="grid-2">
        <TextInput
          label={t("Usuário")}
          required
          mono
          hint={t("Precisa ter permissão para usar o Docker.")}
          value={props.input.user}
          onValue={(value) => props.set("user", value)}
          error={props.errors.user}
        />
        <Select
          label={t("Chave privada")}
          placeholder={t("Selecione uma chave")}
          hint={t("Cadastre chaves em Segurança e adicione a pública ao authorized_keys do servidor.")}
          value={props.input.private_key_id}
          onValue={(value) => props.set("private_key_id", value)}
          options={props.keys.map((key) => ({
            value: key.id,
            label: key.name,
          }))}
          error={props.errors.private_key_id}
        />
      </div>
    </>
  );
}

function NewServerModal(props: { onClose: () => void; onSaved: (server: Server) => void }) {
  const [input, setInput] = createStore<ServerInput>(toInput());
  const keys = createQuery(
    () => true,
    () => api.get<PrivateKey[]>("/keys"),
  );
  const create = createAction(
    async () => {
      const server = await api.post<Server>("/servers", input);
      // Validate right away so the user learns immediately whether it works.
      return api.post<Server>(`/servers/${server.id}/validate`);
    },
    { onSuccess: props.onSaved },
  );
  return (
    <Modal title={t("Adicionar servidor")} onClose={props.onClose} wide>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void create.run();
        }}
      >
        <TextInput
          label={t("Nome")}
          required
          placeholder={t("producao-1")}
          value={input.name}
          onValue={(value) => setInput("name", value)}
          error={create.errors().name}
        />
        <ConnectionFields input={input} set={setInput} errors={create.errors()} keys={keys.data() ?? []} />
        <Show when={keys.data()?.length === 0}>
          <Alert tone="warning">
            <Trans
              text="Nenhuma chave cadastrada. {link} antes de adicionar o servidor."
              values={{ link: <A href="/security">{t("Crie uma chave em Segurança")}</A> }}
            />
          </Alert>
        </Show>
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" pending={create.pending()}>{t("Adicionar e validar")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

export function ServersPage() {
  const navigate = useNavigate();
  const servers = createQuery(
    () => true,
    () => api.get<Server[]>("/servers"),
    { pollMs: 10000 },
  );
  const [adding, setAdding] = createSignal(false);

  return (
    <div class="page">
      <PageHeader
        title={t("Infraestrutura")}
        description={t("Máquinas com Docker onde seus recursos rodam. O Pier só precisa de acesso SSH.")}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setAdding(true)}>{t("Adicionar servidor")}
          </Button>
        }
      />
      <InfrastructureTabs current="servers" />
      <QueryView query={servers}>
        {(list) => (
          <div class="list">
            <For each={list()}>
              {(server) => (
                <A href={`/servers/${server.id}`} class="list-item">
                  <span class="muted">
                    <Icon name="server" size={20} />
                  </span>
                  <div class="grow stack-sm" style={{ gap: "0" }}>
                    <strong class="truncate">{server.name}</strong>
                    <span class="small muted truncate">
                      {server.kind === "local" ? t("Máquina local") : `${server.user}@${server.host}:${server.port}`}
                      <Show when={server.docker_version}> · Docker {server.docker_version}</Show>
                      <Show when={server.last_checked_at}> · {t("verificado {when}", { when: formatRelative(server.last_checked_at) })}</Show>
                    </span>
                  </div>
                  <StatusBadge status={serverStatus(server.status)} />
                  <span class="muted">
                    <Icon name="chevron" size={16} />
                  </span>
                </A>
              )}
            </For>
          </div>
        )}
      </QueryView>
      <Show when={adding()}>
        <NewServerModal onClose={() => setAdding(false)} onSaved={(server) => navigate(`/servers/${server.id}`)} />
      </Show>
    </div>
  );
}

const TABS = [
  { id: "general", label: t("Geral") },
  { id: "docker", label: "Docker" },
  { id: "proxy", label: t("Proxy") },
  { id: "usage", label: t("Uso de recursos") },
  { id: "maintenance", label: t("Manutenção") },
];

export function ServerDetailPage() {
  const params = useParams<{ id: string }>();
  const [search, setSearch] = useSearchParams<{ tab?: string }>();
  const tab = () => search.tab ?? "general";
  const server = createQuery(
    () => params.id,
    (id) => api.get<Server>(`/servers/${id}`),
  );
  const validate = createAction(() => api.post<Server>(`/servers/${params.id}/validate`), {
    onSuccess: (updated) => server.set(updated),
    success: (updated) =>
      updated.status === "reachable"
        ? t("Servidor validado: Docker respondeu.")
        : t("O servidor não respondeu. Veja o detalhe abaixo."),
  });

  return (
    <div class="page">
      <QueryView query={server}>
        {(data) => (
          <>
            <PageHeader
              crumbs={[{ label: t("Servidores"), href: "/servers" }, { label: data().name }]}
              title={
                <span class="row">
                  {data().name}
                  <StatusBadge status={serverStatus(data().status)} />
                </span>
              }
              description={
                data().kind === "local" ? t("Máquina onde o Pier está instalado.") : `${data().user}@${data().host}:${data().port}`
              }
              actions={
                <Button variant="primary" icon="check" pending={validate.pending()} onClick={() => void validate.run()}>{t("Validar conexão")}
                </Button>
              }
            />
            <Show when={data().status === "unreachable" && data().status_message}>
              <Alert tone="danger">{data().status_message}</Alert>
            </Show>
            <ServerChecks checks={data().checks ?? []} />
            <Tabs tabs={TABS} selected={tab()} onSelect={(id) => setSearch({ tab: id })} />
            <Switch>
              <Match when={tab() === "general"}>
                <GeneralTab server={data()} onSaved={(saved) => server.set(saved)} />
              </Match>
              <Match when={tab() === "docker"}>
                <DockerTab serverId={data().id} />
              </Match>
              <Match when={tab() === "proxy"}>
                <ProxyTab server={data()} onSaved={(saved) => server.set(saved)} />
              </Match>
              <Match when={tab() === "usage"}>
                <UsageTab serverId={data().id} />
              </Match>
              <Match when={tab() === "maintenance"}>
                <MaintenanceTab server={data()} />
              </Match>
            </Switch>
          </>
        )}
      </QueryView>
    </div>
  );
}

function GeneralTab(props: { server: Server; onSaved: (server: Server) => void }) {
  const [input, setInput] = createStore<ServerInput>(toInput(props.server));
  const keys = createQuery(
    () => props.server.kind === "ssh",
    () => api.get<PrivateKey[]>("/keys"),
  );
  const save = createAction(() => api.patch<Server>(`/servers/${props.server.id}`, input), {
    success: t("Servidor salvo."),
    onSuccess: props.onSaved,
  });
  return (
    <form
      class="card stack"
      onSubmit={(event) => {
        event.preventDefault();
        void save.run();
      }}
    >
      <TextInput
        label={t("Nome")}
        required
        value={input.name}
        onValue={(value) => setInput("name", value)}
        error={save.errors().name}
      />
      <TextArea label={t("Descrição")} rows={2} value={input.description} onValue={(value) => setInput("description", value)} />
      <Show when={props.server.kind === "ssh"}>
        <ConnectionFields input={input} set={setInput} errors={save.errors()} keys={keys.data() ?? []} />
      </Show>
      <TextInput
        label={t("Diretório de dados")}
        mono
        hint={t("Onde o Pier guarda arquivos compose, builds, backups e certificados neste servidor.")}
        value={input.data_dir}
        onValue={(value) => setInput("data_dir", value)}
        error={save.errors().data_dir}
      />
      <dl class="kv">
        <dt>Docker</dt>
        <dd>{props.server.docker_version || "—"}</dd>
        <dt>{t("Última verificação")}</dt>
        <dd>{formatRelative(props.server.last_checked_at)}</dd>
      </dl>
      <div class="form-actions">
        <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar")}
        </Button>
      </div>
    </form>
  );
}

function ProxyTab(props: { server: Server; onSaved: (server: Server) => void }) {
  const proxy = createQuery(
    () => props.server.id,
    (id) => api.get<ProxyInfo>(`/servers/${id}/proxy`),
    { pollMs: 6000 },
  );
  const [settings, setSettings] = createStore(proxySettings(props.server));
  const [logs, setLogs] = createSignal<string | null>(null);

  const operate = createAction(
    (action: "start" | "stop" | "restart") => api.post(`/servers/${props.server.id}/proxy/${action}`),
    {
      success: t("Proxy atualizado."),
      onSuccess: () => void proxy.refetch(),
    },
  );
  const save = createAction(
    () =>
      api.patch<Server>(`/servers/${props.server.id}`, {
        ...toInput(props.server),
        proxy: settings,
      }),
    {
      success: () =>
        settings.edge.container.trim()
          ? t("Configuração salva. Reinicie o proxy e reimplante os recursos com domínio para aplicar.")
          : t("Configuração do proxy salva. Reinicie o proxy para aplicar."),
      onSuccess: props.onSaved,
    },
  );
  const loadLogs = createAction(() => api.get<{ logs: string }>(`/servers/${props.server.id}/proxy/logs`), {
    onSuccess: (result) => setLogs(result.logs || t("(sem linhas)")),
  });

  return (
    <div class="stack-lg">
      <QueryView query={proxy}>
        {(info) => (
          <Section
            title="Traefik"
            description={t("Recebe o tráfego HTTP/HTTPS e o encaminha para os containers, emitindo certificados Let's Encrypt.")}
            actions={
              <StatusBadge
                status={
                  info().state.running ? { label: info().state.status, tone: "success" } : { label: t("Parado"), tone: "neutral" }
                }
              />
            }
          >
            <Show when={!info().state.running}>
              <Alert tone="warning">{t("Sem o proxy em execução, nenhum domínio deste servidor responde.")}</Alert>
            </Show>
            <div class="row">
              <Show
                when={info().state.running}
                fallback={
                  <Button variant="primary" icon="play" pending={operate.pending()} onClick={() => void operate.run("start")}>{t("Iniciar proxy")}
                  </Button>
                }
              >
                <Button icon="refresh" pending={operate.pending()} onClick={() => void operate.run("restart")}>{t("Reiniciar")}
                </Button>
                <Button icon="stop" pending={operate.pending()} onClick={() => void operate.run("stop")}>{t("Parar")}
                </Button>
              </Show>
              <Button icon="terminal" pending={loadLogs.pending()} onClick={() => void loadLogs.run()}>{t("Ver logs")}
              </Button>
            </div>
            <Show when={logs() !== null}>
              <pre class="log" style={{ height: "auto", "max-height": "320px", margin: "0" }}>
                {logs()}
              </pre>
            </Show>
            <details>
              <summary class="small muted" style={{ cursor: "pointer" }}>{t("Ver configuração gerada (docker-compose)")}
              </summary>
              <pre
                class="log"
                style={{
                  height: "auto",
                  "max-height": "360px",
                  "margin-top": "var(--space-3)",
                }}
              >
                {info().configuration}
              </pre>
            </details>
          </Section>
        )}
      </QueryView>

      <Show when={props.server.proxy.edge?.container}>
        <EdgeSyncCard server={props.server} />
      </Show>

      <form
        class="card stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <h2>{t("Portas e certificados")}</h2>
        <div class="grid-2">
          <NumberInput
            label={t("Porta HTTP")}
            min={1}
            max={65535}
            value={settings.http_port}
            onValue={(value) => setSettings("http_port", value)}
            error={save.errors()["proxy.http_port"]}
          />
          <NumberInput
            label={t("Porta HTTPS")}
            min={1}
            max={65535}
            value={settings.https_port}
            onValue={(value) => setSettings("https_port", value)}
            error={save.errors()["proxy.https_port"]}
          />
        </div>
        <Show when={!settings.edge.container.trim()}>
          <TextInput
            label={t("E-mail para o Let's Encrypt")}
            type="email"
            hint={t("Recebe avisos de expiração. A emissão de certificados exige as portas 80 e 443 acessíveis pela internet.")}
            value={settings.acme_email}
            onValue={(value) => setSettings("acme_email", value)}
            error={save.errors()["proxy.acme_email"]}
          />
        </Show>
        <EdgeFields
          candidates={proxy.data()?.edge_candidates ?? []}
          edge={settings.edge}
          onChange={(field, value) => setSettings("edge", field, value)}
          errors={save.errors()}
        />
        <div class="form-actions">
          <Button type="submit" pending={save.pending()}>{t("Salvar")}
          </Button>
        </div>
      </form>
    </div>
  );
}

function UsageTab(props: { serverId: string }) {
  return <ServerMonitoring serverId={props.serverId} />;
}

function MaintenanceTab(props: { server: Server }) {
  const navigate = useNavigate();
  const [report, setReport] = createSignal("");
  const [deleting, setDeleting] = createSignal(false);
  const cleanup = createAction(() => api.post<{ report: string }>(`/servers/${props.server.id}/cleanup`), {
    success: t("Limpeza concluída."),
    onSuccess: (result) => setReport(result.report),
  });
  const resetHostKey = createAction(() => api.post(`/servers/${props.server.id}/reset-host-key`), {
    success: t("Chave de host redefinida. Valide a conexão para fixar a nova."),
  });
  const remove = createAction(() => api.delete(`/servers/${props.server.id}`), {
    success: t("Servidor removido."),
    onSuccess: () => navigate("/servers"),
  });

  return (
    <div class="stack-lg">
      <Section
        title={t("Liberar espaço")}
        description={t("Remove containers parados criados pelo Pier, imagens sem tag, cache de build e diretórios de build antigos. Nada que o Pier não criou é tocado.")}
      >
        <div>
          <Button icon="trash" pending={cleanup.pending()} onClick={() => void cleanup.run()}>{t("Executar limpeza")}
          </Button>
        </div>
        <Show when={report()}>
          <pre class="log" style={{ height: "auto", "max-height": "320px", margin: "0" }}>
            {report()}
          </pre>
        </Show>
      </Section>
      <Show when={props.server.kind === "ssh"}>
        <Section
          title={t("Chave de host SSH")}
          description={t("O Pier fixa a chave do servidor no primeiro acesso e recusa conexões se ela mudar. Redefina apenas se a máquina foi reinstalada.")}
        >
          <dl class="kv">
            <dt>{t("Chave fixada")}</dt>
            <dd class="mono">{props.server.host_key || t("Ainda não fixada")}</dd>
          </dl>
          <div>
            <Button pending={resetHostKey.pending()} onClick={() => void resetHostKey.run()}>{t("Redefinir chave de host")}
            </Button>
          </div>
        </Section>
        <Section title={t("Zona de perigo")} description={t("Só é possível remover servidores sem recursos.")}>
          <div>
            <Button variant="danger" icon="trash" onClick={() => setDeleting(true)}>{t("Remover servidor")}
            </Button>
          </div>
        </Section>
      </Show>
      <Show when={deleting()}>
        <ConfirmDelete
          title={t("Remover servidor")}
          resourceName={props.server.name}
          description={t("O Pier deixa de gerenciar este servidor. Nada é apagado na máquina.")}
          pending={remove.pending()}
          onConfirm={() => void remove.run()}
          onClose={() => setDeleting(false)}
        />
      </Show>
    </div>
  );
}
