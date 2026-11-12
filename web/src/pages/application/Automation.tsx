import { createSignal, For, Show } from "solid-js";
import { Button, IconButton } from "../../components/Button";
import { EmptyState, QueryView, StatusBadge } from "../../components/Feedback";
import { Modal } from "../../components/Modal";
import { CopyField, Section } from "../../components/Page";
import { api } from "../../lib/api";
import { formatRelative } from "../../lib/format";
import { createAction, createQuery } from "../../lib/query";
import type { Application, ScheduledTask, TaskExecution, WebhookInfo } from "../../lib/types";
import { t } from "../../i18n";
import { Checkbox, TextInput } from "../../components/Form";

/** Automatic deploys (webhooks) and commands that run on a schedule. */
export function AutomationTab(props: { application: Application; onSaved: (application: Application) => void }) {
  return (
    <div class="stack-lg">
      <WebhooksSection application={props.application} onSaved={props.onSaved} />
      <TasksSection applicationId={props.application.id} />
    </div>
  );
}

function WebhooksSection(props: { application: Application; onSaved: (application: Application) => void }) {
  const webhook = createQuery(
    () => props.application.id,
    (id) => api.get<WebhookInfo>(`/applications/${id}/webhook`),
  );
  const toggleAutoDeploy = createAction(
    (enabled: boolean) =>
      api.patch<Application>(`/applications/${props.application.id}`, {
        name: props.application.name,
        description: props.application.description,
        config: { ...props.application.config, auto_deploy: enabled },
      }),
    { success: t("Preferência salva."), onSuccess: props.onSaved },
  );
  const rotate = createAction(() => api.post<WebhookInfo>(`/applications/${props.application.id}/webhook/rotate`), {
    success: t("Novo segredo gerado. Atualize-o no provedor Git."),
    onSuccess: (info) => webhook.set(info),
  });
  const isGit = () => props.application.config.build.pack !== "image";

  return (
    <Section
      title={t("Deploy automático")}
      description={t("Implante a cada push, ou dispare um deploy a partir do seu CI.")}
      actions={
        <Button small icon="refresh" pending={rotate.pending()} onClick={() => void rotate.run()}>{t("Gerar novo segredo")}
        </Button>
      }
    >
      <Show when={isGit()}>
        <Checkbox
          label={t("Implantar a cada push na branch “{branch}”", { branch: props.application.config.source.branch })}
          hint={t("Cadastre uma das URLs abaixo como webhook de push no seu repositório, usando o segredo.")}
          checked={props.application.config.auto_deploy}
          disabled={toggleAutoDeploy.pending()}
          onChange={(checked) => void toggleAutoDeploy.run(checked)}
        />
      </Show>
      <QueryView query={webhook}>
        {(info) => (
          <div class="stack">
            <CopyField label={t("Segredo")} value={info().secret} secret />
            <Show when={isGit()}>
              <CopyField label={t("GitHub (evento push, content type application/json)")} value={info().github_url} />
              <CopyField label={t("GitLab (Push events, Secret token)")} value={info().gitlab_url} />
              <CopyField label={t("Gitea / Forgejo")} value={info().gitea_url} />
            </Show>
            <CopyField
              label={t("Gatilho genérico (CI)")}
              value={`curl -X POST -H "X-Pier-Token: ${info().secret}" ${info().deploy_url}`}
              secret
              hint={t("Dispara um deploy imediatamente, independente da branch.")}
            />
          </div>
        )}
      </QueryView>
    </Section>
  );
}

function TasksSection(props: { applicationId: string }) {
  const tasks = createQuery(
    () => props.applicationId,
    (id) => api.get<ScheduledTask[]>(`/applications/${id}/tasks`),
  );
  const [editing, setEditing] = createSignal<ScheduledTask | "new" | null>(null);
  const [history, setHistory] = createSignal<ScheduledTask | null>(null);

  const run = createAction((id: string) => api.post<TaskExecution>(`/tasks/${id}/run`), {
    success: (execution) => (execution.status === "succeeded" ? t("Tarefa executada com sucesso.") : t("A tarefa terminou com erro.")),
  });
  const remove = createAction((id: string) => api.delete(`/tasks/${id}`), {
    success: t("Tarefa removida."),
    onSuccess: () => void tasks.refetch(),
  });

  return (
    <Section
      title={t("Tarefas agendadas")}
      description={t("Comandos executados periodicamente dentro do container da aplicação.")}
      actions={
        <Button small icon="plus" onClick={() => setEditing("new")}>{t("Nova tarefa")}
        </Button>
      }
    >
      <QueryView query={tasks}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={
              <EmptyState icon="clock" title={t("Nenhuma tarefa")}>{t("Use para rotinas como limpeza de cache, envio de relatórios ou filas.")}
              </EmptyState>
            }
          >
            <div class="list">
              <For each={list()}>
                {(task) => (
                  <div class="list-item">
                    <div class="grow stack-sm" style={{ gap: "0" }}>
                      <strong class="truncate">{task.name}</strong>
                      <span class="small muted mono truncate">
                        {task.cron} · {task.command}
                      </span>
                    </div>
                    <Show when={!task.enabled}>
                      <span class="tag">{t("pausada")}</span>
                    </Show>
                    <Button small icon="play" pending={run.pending()} onClick={() => void run.run(task.id)}>{t("Executar")}
                    </Button>
                    <Button small onClick={() => setHistory(task)}>{t("Histórico")}
                    </Button>
                    <Button small onClick={() => setEditing(task)}>{t("Editar")}
                    </Button>
                    <IconButton icon="trash" label={t("Remover {name}", { name: task.name })} onClick={() => void remove.run(task.id)} />
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>
      <Show when={editing()}>
        {(target) => (
          <TaskForm
            applicationId={props.applicationId}
            task={target() === "new" ? undefined : (target() as ScheduledTask)}
            onClose={() => setEditing(null)}
            onSaved={() => {
              setEditing(null);
              void tasks.refetch();
            }}
          />
        )}
      </Show>
      <Show when={history()}>{(task) => <TaskHistory task={task()} onClose={() => setHistory(null)} />}</Show>
    </Section>
  );
}

function TaskForm(props: { applicationId: string; task?: ScheduledTask; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = createSignal(props.task?.name ?? "");
  const [command, setCommand] = createSignal(props.task?.command ?? "");
  const [cron, setCron] = createSignal(props.task?.cron ?? "0 * * * *");
  const [enabled, setEnabled] = createSignal(props.task?.enabled ?? true);
  const save = createAction(
    () => {
      const body = { name: name(), command: command(), cron: cron(), enabled: enabled() };
      return props.task
        ? api.patch(`/tasks/${props.task.id}`, body)
        : api.post(`/applications/${props.applicationId}/tasks`, body);
    },
    { success: t("Tarefa salva."), onSuccess: props.onSaved },
  );

  return (
    <Modal title={props.task ? t("Editar tarefa") : t("Nova tarefa agendada")} onClose={props.onClose}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <TextInput label={t("Nome")} required value={name()} onValue={setName} error={save.errors().name} />
        <TextInput
          label={t("Comando")}
          required
          mono
          placeholder="php artisan schedule:run"
          value={command()}
          onValue={setCommand}
          error={save.errors().command}
        />
        <TextInput
          label={t("Agenda (cron)")}
          required
          mono
          hint={t("Cinco campos: minuto hora dia mês dia-da-semana. Ex.: */15 * * * * roda a cada 15 minutos.")}
          value={cron()}
          onValue={setCron}
          error={save.errors().cron}
        />
        <Checkbox label={t("Ativa")} checked={enabled()} onChange={setEnabled} />
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar tarefa")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function TaskHistory(props: { task: ScheduledTask; onClose: () => void }) {
  const executions = createQuery(
    () => props.task.id,
    (id) => api.get<TaskExecution[]>(`/tasks/${id}/executions`),
  );
  return (
    <Modal title={t("Histórico — {name}", { name: props.task.name })} onClose={props.onClose} wide>
      <QueryView query={executions}>
        {(list) => (
          <Show when={list().length > 0} fallback={<p class="muted">{t("A tarefa ainda não foi executada.")}</p>}>
            <div class="stack">
              <For each={list()}>
                {(execution) => (
                  <div class="stack-sm">
                    <div class="row-between">
                      <StatusBadge
                        status={
                          execution.status === "succeeded"
                            ? { label: t("Sucesso"), tone: "success" }
                            : execution.status === "failed"
                              ? { label: t("Falhou"), tone: "danger" }
                              : { label: t("Em andamento"), tone: "info", pulse: true }
                        }
                      />
                      <span class="small muted">{formatRelative(execution.started_at)}</span>
                    </div>
                    <Show when={execution.output}>
                      <pre class="log" style={{ height: "auto", "max-height": "200px", margin: "0" }}>
                        {execution.output}
                      </pre>
                    </Show>
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>
    </Modal>
  );
}
