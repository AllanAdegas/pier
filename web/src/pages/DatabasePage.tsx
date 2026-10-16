import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { createSignal, For, Match, Show, Switch } from "solid-js";
import { createStore, unwrap } from "solid-js/store";
import { Button, IconButton } from "../components/Button";
import { Alert, EmptyState, QueryView, StatusBadge } from "../components/Feedback";
import { Checkbox, NumberInput, TextArea, TextInput } from "../components/Form";
import { ContainerLogs } from "../components/LogViewer";
import { ConfirmDelete } from "../components/Modal";
import { CopyField, PageHeader, Section, Tabs } from "../components/Page";
import { api } from "../lib/api";
import { backupStatus, formatBytes, formatDateTime, resourceStatus, TRIGGER_LABEL } from "../lib/format";
import { createAction, createQuery } from "../lib/query";
import type { BackupExecution, Container, Database, DatabaseConfig, DatabaseConnection, EngineInfo } from "../lib/types";

const TABS = [
  { id: "overview", label: "Conexão" },
  { id: "logs", label: "Logs" },
  { id: "backups", label: "Backups" },
  { id: "config", label: "Configuração" },
];

export function DatabasePage() {
  const params = useParams<{ id: string }>();
  const [search, setSearch] = useSearchParams<{ tab?: string }>();
  const tab = () => search.tab ?? "overview";

  const database = createQuery(
    () => params.id,
    (id) => api.get<Database>(`/databases/${id}`),
    { pollMs: 5000 },
  );
  const engines = createQuery(
    () => true,
    () => api.get<EngineInfo[]>("/database-engines"),
  );
  const containers = createQuery(
    () => tab() === "logs" && params.id,
    (id) => api.get<Container[]>(`/databases/${id}/containers`),
    { pollMs: 8000 },
  );
  const engine = () => engines.data()?.find((item) => item.engine === database.data()?.engine);

  const operate = createAction(
    (action: "start" | "stop" | "restart") => api.post<Database>(`/databases/${params.id}/actions/${action}`),
    { success: "Operação concluída.", onSuccess: (updated) => database.set(updated) },
  );

  return (
    <div class="page">
      <QueryView query={database}>
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
              description={`${engine()?.label ?? data().engine} · ${data().config.image}`}
              actions={
                <Show
                  when={data().status === "stopped"}
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
                  <Button variant="primary" icon="play" pending={operate.pending()} onClick={() => void operate.run("start")}>
                    Iniciar
                  </Button>
                </Show>
              }
            />
            <Tabs tabs={TABS} selected={tab()} onSelect={(id) => setSearch({ tab: id })} />
            <Switch>
              <Match when={tab() === "overview"}>
                <ConnectionTab database={data()} engine={engine()} />
              </Match>
              <Match when={tab() === "logs"}>
                <QueryView query={containers}>
                  {(list) => <ContainerLogs serverId={data().server_id} containers={list()} />}
                </QueryView>
              </Match>
              <Match when={tab() === "backups"}>
                <BackupsTab database={data()} supported={engine()?.supports_backup ?? false} onSaved={(saved) => database.set(saved)} />
              </Match>
              <Match when={tab() === "config"}>
                <ConfigTab database={data()} onSaved={(saved) => database.set(saved)} />
              </Match>
            </Switch>
          </>
        )}
      </QueryView>
    </div>
  );
}

function ConnectionTab(props: { database: Database; engine?: EngineInfo }) {
  const connection = createQuery(
    () => props.database.id,
    (id) => api.get<DatabaseConnection>(`/databases/${id}/connection`),
  );
  return (
    <QueryView query={connection}>
      {(info) => (
        <div class="stack-lg">
          <Section
            title="Conexão interna"
            description="Use em aplicações do mesmo servidor. O host é o nome do container na rede do Pier."
          >
            <CopyField label="URL" value={info().internal_url} secret />
            <div class="grid-2">
              <CopyField label="Host" value={info().host} />
              <CopyField label="Porta" value={String(info().port)} />
              <Show when={props.engine?.uses_account}>
                <CopyField label="Usuário" value={info().credentials.username} />
                <CopyField label="Banco" value={info().credentials.database} />
              </Show>
              <CopyField label="Senha" value={info().credentials.password} secret />
            </div>
          </Section>
          <Section title="Conexão externa">
            <Show
              when={info().public_url}
              fallback={
                <Alert tone="info">
                  O banco não está exposto para fora do servidor. Para acessá-lo da sua máquina, defina uma porta pública em
                  Configuração e reinicie.
                </Alert>
              }
            >
              <CopyField label="URL pública" value={info().public_url} secret />
              <Alert tone="warning">
                A porta está aberta na interface do servidor. Restrinja o acesso com um firewall quando possível.
              </Alert>
            </Show>
          </Section>
        </div>
      )}
    </QueryView>
  );
}

function BackupsTab(props: { database: Database; supported: boolean; onSaved: (database: Database) => void }) {
  const backups = createQuery(
    () => props.database.id,
    (id) => api.get<BackupExecution[]>(`/databases/${id}/backups`),
    { pollMs: 4000 },
  );
  const initial = props.database.config.backup;
  const [enabled, setEnabled] = createSignal(initial.enabled);
  const [cron, setCron] = createSignal(initial.cron);
  const [retention, setRetention] = createSignal(initial.retention);

  const savePolicy = createAction(
    () =>
      api.patch<Database>(`/databases/${props.database.id}`, {
        name: props.database.name,
        description: props.database.description,
        config: { ...props.database.config, backup: { enabled: enabled(), cron: cron(), retention: retention() } },
      }),
    { success: "Política de backup salva.", onSuccess: props.onSaved },
  );
  const runNow = createAction(() => api.post(`/databases/${props.database.id}/backups`), {
    success: "Backup iniciado.",
    onSuccess: () => void backups.refetch(),
  });
  const remove = createAction((id: string) => api.delete(`/backups/${id}`), {
    success: "Backup removido.",
    onSuccess: () => void backups.refetch(),
  });

  return (
    <Show
      when={props.supported}
      fallback={<Alert tone="info">Esta engine ainda não tem backup lógico no Pier. Os dados ficam no volume Docker do banco.</Alert>}
    >
      <div class="stack-lg">
        <Section title="Backup automático" description="Os arquivos ficam no diretório de dados do Pier, no próprio servidor.">
          <form
            class="stack"
            onSubmit={(event) => {
              event.preventDefault();
              void savePolicy.run();
            }}
          >
            <Checkbox label="Fazer backup de forma agendada" checked={enabled()} onChange={setEnabled} />
            <div class="grid-2">
              <TextInput
                label="Agenda (cron)"
                mono
                hint="Ex.: 0 3 * * * roda todo dia às 3h (horário do servidor do Pier)."
                value={cron()}
                onValue={setCron}
                error={savePolicy.errors()["config.backup.cron"]}
              />
              <NumberInput
                label="Backups mantidos"
                hint="Os mais antigos são apagados automaticamente."
                min={1}
                max={365}
                value={retention()}
                onValue={setRetention}
                error={savePolicy.errors()["config.backup.retention"]}
              />
            </div>
            <div class="form-actions">
              <Button type="submit" pending={savePolicy.pending()}>
                Salvar política
              </Button>
            </div>
          </form>
        </Section>

        <section class="stack">
          <div class="row-between">
            <h2>Backups</h2>
            <Button variant="primary" icon="download" pending={runNow.pending()} onClick={() => void runNow.run()}>
              Fazer backup agora
            </Button>
          </div>
          <QueryView query={backups}>
            {(list) => (
              <Show
                when={list().length > 0}
                fallback={
                  <EmptyState icon="database" title="Nenhum backup">
                    Faça um backup manual ou ative a agenda acima.
                  </EmptyState>
                }
              >
                <div class="list">
                  <For each={list()}>
                    {(backup) => (
                      <div class="list-item">
                        <StatusBadge status={backupStatus(backup.status)} />
                        <div class="grow stack-sm" style={{ gap: "0" }}>
                          <span>
                            {formatDateTime(backup.started_at)} · {TRIGGER_LABEL[backup.trigger] ?? backup.trigger}
                          </span>
                          <span class="small muted truncate">
                            <Show when={backup.status === "failed"} fallback={formatBytes(backup.size_bytes)}>
                              {backup.message}
                            </Show>
                          </span>
                        </div>
                        <Show when={backup.status === "succeeded"}>
                          <a class="btn btn-sm" href={`/api/v1/backups/${backup.id}/download`} download="">
                            Baixar
                          </a>
                        </Show>
                        <Show when={backup.status !== "running"}>
                          <IconButton icon="trash" label="Remover backup" onClick={() => void remove.run(backup.id)} />
                        </Show>
                      </div>
                    )}
                  </For>
                </div>
              </Show>
            )}
          </QueryView>
        </section>
      </div>
    </Show>
  );
}

function ConfigTab(props: { database: Database; onSaved: (database: Database) => void }) {
  const navigate = useNavigate();
  const initial = props.database;
  const [name, setName] = createSignal(initial.name);
  const [description, setDescription] = createSignal(initial.description);
  const [config, setConfig] = createStore<DatabaseConfig>(structuredClone(unwrap(initial.config)));
  const [deleting, setDeleting] = createSignal(false);

  const save = createAction(
    () => api.patch<Database>(`/databases/${initial.id}`, { name: name(), description: description(), config }),
    { success: "Configuração salva. Reinicie o banco para aplicar.", onSuccess: props.onSaved },
  );
  const remove = createAction(
    (options: { volumes: boolean; force: boolean }) =>
      api.delete(`/databases/${initial.id}?volumes=${options.volumes ? 1 : 0}&force=${options.force ? 1 : 0}`),
    { success: "Banco de dados removido.", onSuccess: () => navigate("/projects") },
  );

  return (
    <form
      class="stack-lg"
      onSubmit={(event) => {
        event.preventDefault();
        void save.run();
      }}
    >
      <Section title="Geral">
        <TextInput label="Nome" required value={name()} onValue={setName} error={save.errors().name} />
        <TextArea label="Descrição" rows={2} value={description()} onValue={setDescription} />
        <TextInput
          label="Imagem"
          mono
          hint="Trocar de versão maior pode exigir migração dos dados. Faça um backup antes."
          value={config.image}
          onValue={(value) => setConfig("image", value)}
          error={save.errors()["config.image"]}
        />
      </Section>
      <Section title="Rede e limites">
        <NumberInput
          label="Porta pública"
          placeholder="não exposto"
          hint="Entre 1024 e 65535. Deixe vazio para manter o banco acessível apenas pela rede interna."
          value={config.public_port}
          onValue={(value) => setConfig("public_port", value)}
          error={save.errors()["config.public_port"]}
        />
        <div class="grid-2">
          <TextInput
            label="CPUs"
            mono
            placeholder="sem limite"
            value={config.cpus}
            onValue={(value) => setConfig("cpus", value)}
            error={save.errors()["config.cpus"]}
          />
          <TextInput
            label="Memória"
            mono
            placeholder="sem limite"
            hint="Ex.: 512m ou 2g."
            value={config.memory}
            onValue={(value) => setConfig("memory", value)}
            error={save.errors()["config.memory"]}
          />
        </div>
      </Section>
      <div class="form-actions">
        <Button type="submit" variant="primary" pending={save.pending()}>
          Salvar configuração
        </Button>
      </div>

      <Section title="Zona de perigo" description="Remove o container do banco. O volume de dados e os backups só são apagados se você pedir.">
        <div>
          <Button variant="danger" icon="trash" onClick={() => setDeleting(true)}>
            Remover banco de dados
          </Button>
        </div>
      </Section>
      <Show when={deleting()}>
        <ConfirmDelete
          title="Remover banco de dados"
          resourceName={initial.name}
          description="Aplicações que dependem deste banco vão parar de funcionar. Esta ação não pode ser desfeita."
          volumesLabel="Apagar também o volume de dados e os backups"
          allowForce
          pending={remove.pending()}
          onConfirm={(options) => void remove.run(options)}
          onClose={() => setDeleting(false)}
        />
      </Show>
    </form>
  );
}
