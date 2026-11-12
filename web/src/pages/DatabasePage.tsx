import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { createSignal, For, Match, Show, Switch } from "solid-js";
import { createStore, unwrap } from "solid-js/store";
import { Button, IconButton } from "../components/Button";
import { Alert, EmptyState, QueryView, StatusBadge } from "../components/Feedback";
import { ContainerLogs } from "../components/LogViewer";
import { ConfirmDelete, deleteQuery, type DeleteOptions, Modal } from "../components/Modal";
import { CopyField, PageHeader, Section, Tabs } from "../components/Page";
import { api } from "../lib/api";
import { backupStatus, formatBytes, formatDateTime, resourceStatus, TRIGGER_LABEL } from "../lib/format";
import { createAction, createQuery } from "../lib/query";
import type { BackupExecution, Container, Database, DatabaseConfig, DatabaseConnection, EngineInfo } from "../lib/types";
import { t } from "../i18n";
import { Checkbox, NumberInput, TextArea, TextInput } from "../components/Form";

const TABS = [
  { id: "overview", label: t("Conexão") },
  { id: "logs", label: "Logs" },
  { id: "backups", label: t("Backups") },
  { id: "config", label: t("Configuração") },
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
    { success: t("Operação concluída."), onSuccess: (updated) => database.set(updated) },
  );

  return (
    <div class="page">
      <QueryView query={database}>
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
              description={`${engine()?.label ?? data().engine} · ${data().config.image}`}
              actions={
                <Show
                  when={data().status === "stopped"}
                  fallback={
                    <>
                      <Button icon="refresh" pending={operate.pending()} onClick={() => void operate.run("restart")}>{t("Reiniciar")}
                      </Button>
                      <Button icon="stop" pending={operate.pending()} onClick={() => void operate.run("stop")}>{t("Parar")}
                      </Button>
                    </>
                  }
                >
                  <Button variant="primary" icon="play" pending={operate.pending()} onClick={() => void operate.run("start")}>{t("Iniciar")}
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
            title={t("Conexão interna")}
            description={t("Use em aplicações do mesmo servidor. O host é o nome do container na rede do Pier.")}
          >
            <CopyField label="URL" value={info().internal_url} secret />
            <div class="grid-2">
              <CopyField label="Host" value={info().host} />
              <CopyField label={t("Porta")} value={String(info().port)} />
              <Show when={props.engine?.uses_account}>
                <CopyField label={t("Usuário")} value={info().credentials.username} />
                <CopyField label={t("Banco")} value={info().credentials.database} />
              </Show>
              <CopyField label={t("Senha")} value={info().credentials.password} secret />
            </div>
          </Section>
          <Section title={t("Conexão externa")}>
            <Show
              when={info().public_url}
              fallback={
                <Alert tone="info">{t("O banco não está exposto para fora do servidor. Para acessá-lo da sua máquina, defina uma porta pública em Configuração e reinicie.")}
                </Alert>
              }
            >
              <CopyField label={t("URL pública")} value={info().public_url} secret />
              <Alert tone="warning">{t("A porta está aberta na interface do servidor. Restrinja o acesso com um firewall quando possível.")}
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
  const [s3, setS3] = createSignal(initial.s3 ?? false);
  const [restoreTest, setRestoreTest] = createSignal(initial.restore_test ?? false);
  const [restoreCron, setRestoreCron] = createSignal(initial.restore_test_cron || "0 4 * * 0");
  const [restoring, setRestoring] = createSignal<BackupExecution>();
  const [confirmation, setConfirmation] = createSignal("");
  const [newName, setNewName] = createSignal("");
  const operations = createQuery(() => props.database.id, (id) => api.get<Array<{id: string; kind: string; status: string; message: string; logs: string; started_at: string}>>(`/databases/${id}/operations`), { pollMs: 4000 });
  const restore = createAction(() => api.post(`/backups/${restoring()!.id}/restore`, { confirmation: confirmation(), new_name: newName() }), { success: t("Restauração iniciada."), onSuccess: () => { setRestoring(undefined); void operations.refetch(); } });
  const testRestore = createAction(() => api.post(`/databases/${props.database.id}/restore-tests`), { success: t("Teste de restauração iniciado."), onSuccess: () => void operations.refetch() });

  const savePolicy = createAction(
    () =>
      api.patch<Database>(`/databases/${props.database.id}`, {
        name: props.database.name,
        description: props.database.description,
        config: { ...props.database.config, backup: { enabled: enabled(), cron: cron(), retention: retention(), s3: s3(), restore_test: restoreTest(), restore_test_cron: restoreCron() } },
      }),
    { success: t("Política de backup salva."), onSuccess: props.onSaved },
  );
  const runNow = createAction(() => api.post(`/databases/${props.database.id}/backups`), {
    success: t("Backup iniciado."),
    onSuccess: () => void backups.refetch(),
  });
  const remove = createAction((id: string) => api.delete(`/backups/${id}`), {
    success: t("Backup removido."),
    onSuccess: () => void backups.refetch(),
  });

  return (
    <Show
      when={props.supported}
      fallback={<Alert tone="info">{t("Esta engine ainda não tem backup lógico no Pier. Os dados ficam no volume Docker do banco.")}</Alert>}
    >
      <div class="stack-lg">
        <Section title={t("Backup automático")} description={t("Os arquivos ficam no diretório de dados do Pier, no próprio servidor.")}>
          <form
            class="stack"
            onSubmit={(event) => {
              event.preventDefault();
              void savePolicy.run();
            }}
          >
            <Checkbox label={t("Enviar cópia para S3")} checked={s3()} onChange={setS3} />
            <Checkbox label={t("Testar restauração automaticamente")} checked={restoreTest()} onChange={setRestoreTest} />
            <Show when={restoreTest()}><TextInput label={t("Agenda do teste de restauração")} value={restoreCron()} onValue={setRestoreCron} error={savePolicy.errors()["config.backup.restore_test_cron"]} /></Show>
            <Checkbox label={t("Fazer backup de forma agendada")} checked={enabled()} onChange={setEnabled} />
            <div class="grid-2">
              <TextInput
                label={t("Agenda (cron)")}
                mono
                hint={t("Ex.: 0 3 * * * roda todo dia às 3h (horário do servidor do Pier).")}
                value={cron()}
                onValue={setCron}
                error={savePolicy.errors()["config.backup.cron"]}
              />
              <NumberInput
                label={t("Backups mantidos")}
                hint={t("Os mais antigos são apagados automaticamente.")}
                min={1}
                max={365}
                value={retention()}
                onValue={setRetention}
                error={savePolicy.errors()["config.backup.retention"]}
              />
            </div>
            <div class="form-actions">
              <Button type="submit" pending={savePolicy.pending()}>{t("Salvar política")}
              </Button>
            </div>
          </form>
        </Section>

        <Section title={t("Restaurações e testes")}>
          <Button pending={testRestore.pending()} onClick={() => void testRestore.run()}>{t("Testar restauração agora")}</Button>
          <For each={operations.data() ?? []}>{(op) => <div class="stack-sm"><span>{formatDateTime(op.started_at)} · {op.kind === "restore_test" ? t("Teste de restauração") : t("Restauração")} · <StatusBadge status={backupStatus(op.status)} /></span><Show when={op.message}><Alert tone="danger">{op.message}</Alert></Show><Show when={op.logs}><pre class="small">{op.logs}</pre></Show></div>}</For>
        </Section>
        <Show when={restoring()}>
          <Modal title={t("Restaurar backup")} onClose={() => setRestoring(undefined)}>
            <form class="stack" onSubmit={(event) => {event.preventDefault(); void restore.run();}}>
              <Alert tone="warning">{t("A restauração sobrescreve os dados e interrompe as conexões durante a operação. Pause as escritas das aplicações dependentes.")}</Alert>
              <TextInput label={t("Nome do novo banco (vazio para sobrescrever o atual)")} value={newName()} onValue={setNewName} />
              <TextInput label={t("Digite \"{name}\" para confirmar", {name: props.database.name})} value={confirmation()} onValue={setConfirmation} />
              <Button type="submit" variant="danger" disabled={confirmation() !== props.database.name} pending={restore.pending()}>{t("Restaurar backup")}</Button>
            </form>
          </Modal>
        </Show>
        <section class="stack">
          <div class="row-between">
            <h2>{t("Backups")}</h2>
            <Button variant="primary" icon="download" pending={runNow.pending()} onClick={() => void runNow.run()}>{t("Fazer backup agora")}
            </Button>
          </div>
          <QueryView query={backups}>
            {(list) => (
              <Show
                when={list().length > 0}
                fallback={
                  <EmptyState icon="database" title={t("Nenhum backup")}>{t("Faça um backup manual ou ative a agenda acima.")}
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
                            <Show when={backup.message} fallback={formatBytes(backup.size_bytes)}>
                              {backup.message}
                            </Show>
                          </span>
                        </div>
                        <Show when={backup.status === "succeeded"}>
                          <Button onClick={() => {setConfirmation(""); setNewName(""); setRestoring(backup);}}>{t("Restaurar backup")}</Button>
                          <a class="btn btn-sm" href={`/api/v1/backups/${backup.id}/download`} download="">{t("Baixar")}
                          </a>
                        </Show>
                        <Show when={backup.status !== "running"}>
                          <IconButton icon="trash" label={t("Remover backup")} onClick={() => void remove.run(backup.id)} />
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
    { success: t("Configuração salva. Reinicie o banco para aplicar."), onSuccess: props.onSaved },
  );
  const remove = createAction(
    (options: DeleteOptions) =>
      api.delete(`/databases/${initial.id}?${deleteQuery(options)}`),
    { success: t("Banco de dados removido."), onSuccess: () => navigate("/projects") },
  );

  return (
    <form
      class="stack-lg"
      onSubmit={(event) => {
        event.preventDefault();
        void save.run();
      }}
    >
      <Section title={t("Geral")}>
        <TextInput label={t("Nome")} required value={name()} onValue={setName} error={save.errors().name} />
        <TextArea label={t("Descrição")} rows={2} value={description()} onValue={setDescription} />
        <TextInput
          label={t("Imagem")}
          mono
          hint={t("Trocar de versão maior pode exigir migração dos dados. Faça um backup antes.")}
          value={config.image}
          onValue={(value) => setConfig("image", value)}
          error={save.errors()["config.image"]}
        />
      </Section>
      <Section title={t("Rede e limites")}>
        <NumberInput
          label={t("Porta pública")}
          placeholder={t("não exposto")}
          hint={t("Entre 1024 e 65535. Deixe vazio para manter o banco acessível apenas pela rede interna.")}
          value={config.public_port}
          onValue={(value) => setConfig("public_port", value)}
          error={save.errors()["config.public_port"]}
        />
        <div class="grid-2">
          <TextInput
            label={t("CPUs")}
            mono
            placeholder={t("sem limite")}
            value={config.cpus}
            onValue={(value) => setConfig("cpus", value)}
            error={save.errors()["config.cpus"]}
          />
          <TextInput
            label={t("Memória")}
            mono
            placeholder={t("sem limite")}
            hint={t("Ex.: 512m ou 2g.")}
            value={config.memory}
            onValue={(value) => setConfig("memory", value)}
            error={save.errors()["config.memory"]}
          />
        </div>
      </Section>
      <div class="form-actions">
        <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar configuração")}
        </Button>
      </div>

      <Section title={t("Zona de perigo")} description={t("Remove o container do banco. O volume de dados e os backups só são apagados se você pedir.")}>
        <div>
          <Button variant="danger" icon="trash" onClick={() => setDeleting(true)}>{t("Remover banco de dados")}
          </Button>
        </div>
      </Section>
      <Show when={deleting()}>
        <ConfirmDelete
          title={t("Remover banco de dados")}
          resourceName={initial.name}
          environmentId={initial.environment_id}
          description={t("Aplicações que dependem deste banco vão parar de funcionar. Esta ação não pode ser desfeita.")}
          volumesLabel={t("Apagar também o volume de dados e os backups")}
          allowForce
          pending={remove.pending()}
          onConfirm={(options) => void remove.run(options)}
          onClose={() => setDeleting(false)}
        />
      </Show>
    </form>
  );
}
