import { createSignal, For, Show } from "solid-js";
import { createStore } from "solid-js/store";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { formatRelative } from "../../lib/format";
import type { LogDrain, LogDrainInput, LogDrainKind, LogDrainScope, Project, Resources } from "../../lib/types";
import { Alert, QueryView, StatusBadge } from "../../components/Feedback";
import { PageHeader, Section } from "../../components/Page";
import { Button } from "../../components/Button";
import { Checkbox, Select, TextArea, TextInput } from "../../components/Form";
import { Modal } from "../../components/Modal";
import { ConfirmTyped } from "./ConfirmTyped";
import { drainHealth, formatLabels, parseLabels } from "./serverExtras";
import { t } from "../../i18n";

/** Infrastructure › Log forwarding: drains of application and stack logs. */
export function LogDrainsPage() {
  const drains = createQuery(() => true, () => api.get<LogDrain[]>("/log-drains"), { pollMs: 10000 });
  const [editing, setEditing] = createSignal<LogDrain | "new">();
  return (
    <div class="page">
      <PageHeader
        crumbs={[{ label: t("Servidores"), href: "/servers" }, { label: t("Encaminhamento de logs") }]}
        title={t("Encaminhamento de logs")}
        description={t("Envia os logs de aplicações e stacks para um endpoint HTTP ou para o Loki. Segredos das variáveis são mascarados antes do envio. Uma falha no destino nunca afeta deploys.")}
        actions={<Button variant="primary" icon="plus" onClick={() => setEditing("new")}>{t("Novo encaminhamento")}</Button>}
      />
      <QueryView query={drains}>
        {(list) => (
          <Show when={list().length > 0} fallback={<p class="muted">{t("Nenhum encaminhamento. Crie um para enviar logs a um sistema externo.")}</p>}>
            <div class="stack">
              <For each={list()}>{(drain) => <DrainCard drain={drain} onEdit={() => setEditing(drain)} onChanged={() => void drains.refetch()} />}</For>
            </div>
          </Show>
        )}
      </QueryView>
      <Show when={editing()}>
        {(current) => (
          <DrainForm
            drain={current() === "new" ? undefined : (current() as LogDrain)}
            onClose={() => setEditing(undefined)}
            onSaved={() => {
              setEditing(undefined);
              void drains.refetch();
            }}
          />
        )}
      </Show>
    </div>
  );
}

function DrainCard(props: { drain: LogDrain; onEdit: () => void; onChanged: () => void }) {
  const [removing, setRemoving] = createSignal(false);
  const test = createAction(() => api.post(`/log-drains/${props.drain.id}/test`), { success: t("O destino aceitou o registro de teste.") });
  const remove = createAction(
    (confirmation: string) => api.delete(`/log-drains/${props.drain.id}?${new URLSearchParams({ confirmation })}`),
    { success: t("Encaminhamento removido."), onSuccess: () => { setRemoving(false); props.onChanged(); } },
  );
  const status = () => props.drain.status;
  return (
    <Section
      title={props.drain.name}
      description={`${props.drain.kind === "loki" ? "Loki" : "HTTP JSON"} · ${props.drain.url}`}
      actions={<StatusBadge status={drainHealth(props.drain)} />}
    >
      <dl class="grid-3 small">
        <div><dt class="muted">{t("Enviados")}</dt><dd>{status().sent}</dd></div>
        <div><dt class="muted">{t("Descartados")}</dt><dd>{status().dropped}</dd></div>
        <div><dt class="muted">{t("Na fila")}</dt><dd>{status().buffered} / {status().capacity}</dd></div>
      </dl>
      <p class="small muted">
        {t("Contadores desde o último início do Pier. Com o destino lento ou fora do ar, os registros mais antigos da fila são descartados.")}
      </p>
      <Show when={status().last_error}>
        <Alert tone="danger">{t("Última falha {when}: {error}", { when: formatRelative(status().last_error_at), error: status().last_error })}</Alert>
      </Show>
      <div class="row">
        <Button small onClick={props.onEdit}>{t("Editar")}</Button>
        <Button small variant="ghost" pending={test.pending()} onClick={() => void test.run()}>{t("Enviar teste")}</Button>
        <Button small variant="danger" icon="trash" onClick={() => setRemoving(true)}>{t("Remover")}</Button>
      </div>
      <Show when={removing()}>
        <ConfirmTyped
          title={t("Remover encaminhamento")}
          name={props.drain.name}
          description={t("Os logs deixam de ser enviados para este destino. O que já foi enviado continua lá.")}
          action={t("Remover definitivamente")}
          danger
          pending={remove.pending()}
          onConfirm={(typed) => void remove.run(typed)}
          onClose={() => setRemoving(false)}
        />
      </Show>
    </Section>
  );
}

function DrainForm(props: { drain?: LogDrain; onClose: () => void; onSaved: () => void }) {
  const [input, setInput] = createStore<LogDrainInput>({
    name: props.drain?.name ?? "", kind: props.drain?.kind ?? "loki", url: props.drain?.url ?? "",
    labels: props.drain?.labels ?? {}, scope_type: props.drain?.scope_type ?? "project", scope_id: props.drain?.scope_id ?? "",
    enabled: props.drain?.enabled ?? true,
  });
  const [labelsText, setLabelsText] = createSignal(formatLabels(input.labels));
  const [token, setToken] = createSignal("");
  const [clearToken, setClearToken] = createSignal(false);
  const projects = createQuery(() => true, () => api.get<Project[]>("/projects"));
  const resources = createQuery(() => true, () => api.get<Resources>("/resources"));
  const parsed = () => parseLabels(labelsText());
  const body = (): LogDrainInput => {
    const value: LogDrainInput = { ...input, labels: parsed().labels };
    if (clearToken()) value.token = "";
    else if (token()) value.token = token();
    return value;
  };
  const save = createAction(
    () => (props.drain ? api.put(`/log-drains/${props.drain.id}`, body()) : api.post("/log-drains", body())),
    { success: t("Encaminhamento salvo."), onSuccess: props.onSaved },
  );
  const scopeOptions = () => {
    if (input.scope_type === "project") return (projects.data() ?? []).map((project) => ({ value: project.id, label: project.name }));
    const list = input.scope_type === "application" ? resources.data()?.applications ?? [] : resources.data()?.stacks ?? [];
    return list.map((resource) => ({ value: resource.id, label: resource.name }));
  };
  return (
    <Modal title={props.drain ? t("Editar encaminhamento") : t("Novo encaminhamento")} onClose={props.onClose} wide>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          if (parsed().invalid.length === 0) void save.run();
        }}
      >
        <div class="grid-2">
          <TextInput label={t("Nome")} value={input.name} onValue={(value) => setInput("name", value)} error={save.errors().name} />
          <Select
            label={t("Destino")}
            value={input.kind}
            onValue={(value) => setInput("kind", value as LogDrainKind)}
            options={[{ value: "loki", label: t("Loki (API de push)") }, { value: "http", label: t("HTTP JSON (POST de uma lista de registros)") }]}
          />
        </div>
        <TextInput
          label={t("URL")}
          mono
          placeholder={input.kind === "loki" ? "https://loki.exemplo.com" : "https://logs.exemplo.com/ingest"}
          value={input.url}
          onValue={(value) => setInput("url", value)}
          hint={input.kind === "loki" ? t("Sem caminho, o Pier usa /loki/api/v1/push.") : t("Recebe um POST com uma lista JSON de {timestamp, message, labels}.")}
          error={save.errors().url}
        />
        <TextInput
          label={t("Token (opcional)")}
          type="password"
          autocomplete="off"
          value={token()}
          onValue={setToken}
          placeholder={props.drain?.has_token ? t("Mantém o token atual") : ""}
          hint={t("Enviado como Bearer; no Loki, \"usuário:senha\" vira autenticação básica. Exige HTTPS.")}
          error={save.errors().token}
        />
        <Show when={props.drain?.has_token}>
          <Checkbox label={t("Remover o token guardado")} checked={clearToken()} onChange={setClearToken} />
        </Show>
        <div class="grid-2">
          <Select
            label={t("Encaminhar logs de")}
            value={input.scope_type}
            onValue={(value) => setInput({ scope_type: value as LogDrainScope, scope_id: "" })}
            options={[{ value: "project", label: t("Um projeto inteiro") }, { value: "application", label: t("Uma aplicação") }, { value: "stack", label: t("Uma stack") }]}
          />
          <Select label={t("Qual")} value={input.scope_id} placeholder={t("Escolha")} onValue={(value) => setInput("scope_id", value)} options={scopeOptions()} error={save.errors().scope_id} />
        </div>
        <TextArea
          label={t("Rótulos")}
          mono
          rows={3}
          placeholder={"env=prod\nteam=web"}
          value={labelsText()}
          onValue={setLabelsText}
          hint={t("Um por linha, nome=valor. O Pier também envia pier_server, pier_resource_type, pier_resource_id e pier_container.")}
          error={parsed().invalid.length > 0 ? t("Linhas sem nome=valor: {lines}", { lines: parsed().invalid.join(", ") }) : save.errors().labels}
        />
        <Checkbox label={t("Ativo")} checked={input.enabled} onChange={(value) => setInput("enabled", value)} />
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar encaminhamento")}</Button>
        </div>
      </form>
    </Modal>
  );
}
