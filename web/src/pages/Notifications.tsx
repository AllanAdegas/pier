import { createSignal, For, Show } from "solid-js";
import { Button, IconButton } from "../components/Button";
import { EmptyState, QueryView } from "../components/Feedback";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/Page";
import { api } from "../lib/api";
import { createAction, createQuery } from "../lib/query";
import type { ChannelKind, NotificationChannel } from "../lib/types";
import { t } from "../i18n";
import { Checkbox, Select, TextInput } from "../components/Form";

interface ConfigField {
  key: string;
  label: string;
  hint?: string;
  secret?: boolean;
}

// What each kind of channel needs; mirrors Sender.RequiredConfig on the backend.
const KINDS: Record<ChannelKind, { label: string; fields: ConfigField[] }> = {
  discord: { label: "Discord", fields: [{ key: "webhook_url", label: t("URL do webhook"), secret: true }] },
  slack: { label: "Slack", fields: [{ key: "webhook_url", label: t("URL do incoming webhook"), secret: true }] },
  telegram: {
    label: "Telegram",
    fields: [
      { key: "bot_token", label: t("Token do bot"), secret: true },
      { key: "chat_id", label: t("Chat ID"), hint: t("ID do chat ou grupo que recebe as mensagens.") },
    ],
  },
  webhook: {
    label: t("Webhook genérico"),
    fields: [{ key: "url", label: "URL", hint: t("Recebe um POST com o evento em JSON.") }],
  },
  email: {
    label: t("E-mail (SMTP)"),
    fields: [
      { key: "host", label: t("Servidor SMTP") },
      { key: "port", label: t("Porta"), hint: t("Geralmente 587.") },
      { key: "username", label: t("Usuário"), hint: t("Opcional.") },
      { key: "password", label: t("Senha"), secret: true, hint: t("Opcional.") },
      { key: "from", label: t("Remetente") },
      { key: "to", label: t("Destinatários"), hint: t("Separe vários por vírgula.") },
    ],
  },
};

const EVENT_LABEL: Record<string, string> = {
  "container.restarting": t("Contêiner reiniciando em loop"),
  "container.oom_killed": t("Contêiner sem memória"),
  "container.unhealthy": t("Contêiner com saúde falhando"),
  "server.disk_high": t("Disco do servidor quase cheio"),
  "server.resource_high": t("Uso elevado de CPU ou memória"),
  "certificate.expiring": t("Certificado inválido ou próximo do vencimento"),
  "backup.missed": t("Backup não executado no horário"),
  "task.missed": t("Tarefa não executada no horário"),
  "backup.restore_test_failed": t("Teste de restauração falhou"),
  "uptime.down": t("URL fora do ar"),
  "alert.resolved": t("Alerta resolvido"),

  "deployment.succeeded": t("Deploy concluído"),
  "deployment.failed": t("Deploy falhou"),
  "backup.succeeded": t("Backup concluído"),
  "backup.failed": t("Backup falhou"),
  "task.failed": t("Tarefa agendada falhou"),
  "server.unreachable": t("Servidor inacessível"),
};

export function NotificationsPage() {
  const channels = createQuery(
    () => true,
    () => api.get<NotificationChannel[]>("/notifications"),
  );
  const [editing, setEditing] = createSignal<NotificationChannel | "new" | null>(null);
  const test = createAction((id: string) => api.post(`/notifications/${id}/test`), {
    success: t("Mensagem de teste enviada."),
  });
  const remove = createAction((id: string) => api.delete(`/notifications/${id}`), {
    success: t("Canal removido."),
    onSuccess: () => void channels.refetch(),
  });

  return (
    <div class="page">
      <PageHeader
        title={t("Notificações")}
        description={t("Saiba quando um deploy termina, um backup falha ou um servidor cai.")}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setEditing("new")}>{t("Novo canal")}
          </Button>
        }
      />
      <QueryView query={channels}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={
              <EmptyState
                icon="bell"
                title={t("Nenhum canal configurado")}
                action={
                  <Button variant="primary" icon="plus" onClick={() => setEditing("new")}>{t("Configurar o primeiro canal")}
                  </Button>
                }
              >{t("Conecte Discord, Slack, Telegram, e-mail ou um webhook e escolha quais eventos quer receber.")}
              </EmptyState>
            }
          >
            <div class="list">
              <For each={list()}>
                {(channel) => (
                  <div class="list-item">
                    <div class="grow stack-sm" style={{ gap: "0" }}>
                      <strong class="truncate">{channel.name}</strong>
                      <span class="small muted truncate">
                        {KINDS[channel.kind]?.label ?? channel.kind} · {channel.events.map((event) => EVENT_LABEL[event] ?? event).join(", ")}
                      </span>
                    </div>
                    <Show when={!channel.enabled}>
                      <span class="tag">{t("pausado")}</span>
                    </Show>
                    <Button small pending={test.pending()} onClick={() => void test.run(channel.id)}>{t("Testar")}
                    </Button>
                    <Button small onClick={() => setEditing(channel)}>{t("Editar")}
                    </Button>
                    <IconButton icon="trash" label={t("Remover {name}", { name: channel.name })} onClick={() => void remove.run(channel.id)} />
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>
      <Show when={editing()}>
        {(target) => (
          <ChannelForm
            channel={target() === "new" ? undefined : (target() as NotificationChannel)}
            onClose={() => setEditing(null)}
            onSaved={() => {
              setEditing(null);
              void channels.refetch();
            }}
          />
        )}
      </Show>
    </div>
  );
}

function ChannelForm(props: { channel?: NotificationChannel; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = createSignal(props.channel?.name ?? "");
  const [kind, setKind] = createSignal<ChannelKind>(props.channel?.kind ?? "discord");
  const [config, setConfig] = createSignal<Record<string, string>>({ ...(props.channel?.config ?? {}) });
  const [events, setEvents] = createSignal<string[]>(props.channel?.events ?? ["deployment.failed", "backup.failed", "server.unreachable"]);
  const [enabled, setEnabled] = createSignal(props.channel?.enabled ?? true);

  const toggleEvent = (event: string, checked: boolean) =>
    setEvents(checked ? [...events(), event] : events().filter((item) => item !== event));
  const save = createAction(
    () => {
      const body = { name: name(), kind: kind(), config: kind() === "email" ? { ...config(), require_tls: config().require_tls ?? "true" } : config(), events: events(), enabled: enabled() };
      return props.channel ? api.patch(`/notifications/${props.channel.id}`, body) : api.post("/notifications", body);
    },
    { success: t("Canal salvo."), onSuccess: props.onSaved },
  );

  return (
    <Modal title={props.channel ? t("Editar canal") : t("Novo canal de notificação")} onClose={props.onClose} wide>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <div class="grid-2">
          <TextInput label={t("Nome")} required placeholder={t("Alertas de produção")} value={name()} onValue={setName} error={save.errors().name} />
          <Select
            label={t("Tipo")}
            value={kind()}
            onValue={(value) => {
              setKind(value as ChannelKind);
              setConfig({});
            }}
            options={Object.entries(KINDS).map(([value, definition]) => ({ value, label: definition.label }))}
          />
        </div>
        <For each={KINDS[kind()].fields}>
          {(field) => (
            <TextInput
              label={field.label}
              hint={field.hint}
              type={field.secret ? "password" : "text"}
              autocomplete="off"
              mono={!field.secret}
              value={config()[field.key] ?? ""}
              onValue={(value) => setConfig({ ...config(), [field.key]: value })}
              error={save.errors()[`config.${field.key}`]}
            />
          )}
        </For>
        <fieldset class="stack" style={{ border: "0", padding: "0", margin: "0" }}>
          <legend class="field-label" style={{ "margin-bottom": "var(--space-2)" }}>{t("Eventos")}
          </legend>
          <div class="grid-2">
            <For each={Object.entries(EVENT_LABEL)}>
              {([event, label]) => (
                <Checkbox label={label} checked={events().includes(event)} onChange={(checked) => toggleEvent(event, checked)} />
              )}
            </For>
          </div>
          <Show when={save.errors().events}>
            <span class="field-error">{save.errors().events}</span>
          </Show>
        </fieldset>
        <Show when={kind() === "email"}><Checkbox label={t("Exigir TLS no SMTP")} checked={config().require_tls !== "false"} onChange={(checked) => setConfig({ ...config(), require_tls: String(checked) })} /></Show>
        <Checkbox label={t("Canal ativo")} checked={enabled()} onChange={setEnabled} />
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar canal")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
