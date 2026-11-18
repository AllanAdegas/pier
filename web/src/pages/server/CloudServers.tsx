import { createSignal, For, Show } from "solid-js";
import { A } from "@solidjs/router";
import { createStore } from "solid-js/store";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { formatRelative } from "../../lib/format";
import type { CloudCatalog, CloudCredential, CloudMachine, CloudProvider } from "../../lib/types";
import { Alert, QueryView, StatusBadge } from "../../components/Feedback";
import { PageHeader, Section } from "../../components/Page";
import { Button } from "../../components/Button";
import { Checkbox, Select, TextInput } from "../../components/Form";
import { ConfirmTyped } from "./ConfirmTyped";
import { machineBusy, machineStatus } from "./serverExtras";
import { t } from "../../i18n";

const PROVIDERS: Record<CloudProvider, string> = { hetzner: "Hetzner Cloud", digitalocean: "DigitalOcean" };

/** Infrastructure › Cloud: accounts, new machines and their provisioning. */
export function CloudServersPage() {
  const credentials = createQuery(() => true, () => api.get<CloudCredential[]>("/cloud/credentials"));
  const [busy, setBusy] = createSignal(false);
  const machines = createQuery(() => true, async () => {
    const list = await api.get<CloudMachine[]>("/cloud/machines");
    setBusy(list.some((machine) => machineBusy(machine.status)));
    return list;
  }, { pollMs: () => (busy() ? 4000 : 0) });
  return (
    <div class="page">
      <PageHeader
        crumbs={[{ label: t("Servidores"), href: "/servers" }, { label: t("Nuvem") }]}
        title={t("Servidores na nuvem")}
        description={t("Crie servidores na Hetzner Cloud ou na DigitalOcean com a chave SSH do Pier e o Docker instalados. A criação gera cobrança no provedor.")}
      />
      <Accounts credentials={credentials.data() ?? []} onChanged={() => void credentials.refetch()} />
      <Show when={(credentials.data() ?? []).length > 0}>
        <NewMachine credentials={credentials.data()!} onCreated={() => void machines.refetch()} />
      </Show>
      <Section title={t("Servidores criados pelo Pier")}>
        <QueryView query={machines}>
          {(list) => (
            <Show when={list().length > 0} fallback={<p class="muted small">{t("Nenhum servidor criado na nuvem.")}</p>}>
              <div class="list">
                <For each={list()}>{(machine) => <MachineRow machine={machine} onChanged={() => void machines.refetch()} />}</For>
              </div>
            </Show>
          )}
        </QueryView>
      </Section>
    </div>
  );
}

function Accounts(props: { credentials: CloudCredential[]; onChanged: () => void }) {
  const [input, setInput] = createStore({ provider: "hetzner" as CloudProvider, name: "", token: "" });
  const add = createAction(() => api.post<CloudCredential>("/cloud/credentials", input), {
    success: t("Conta conectada. O token foi verificado e guardado cifrado."),
    onSuccess: () => {
      setInput({ name: "", token: "" });
      props.onChanged();
    },
  });
  const remove = createAction((id: string) => api.delete(`/cloud/credentials/${id}`), { success: t("Conta removida do Pier."), onSuccess: props.onChanged });
  return (
    <Section
      title={t("Contas de provedor")}
      description={t("Use um token de API com leitura e escrita. O token fica cifrado no Pier e nunca é exibido de novo.")}
    >
      <For each={props.credentials}>
        {(credential) => (
          <div class="row-between">
            <span>
              <strong>{credential.name}</strong> <span class="muted small">{PROVIDERS[credential.provider]}</span>
            </span>
            <Button small variant="ghost" icon="trash" pending={remove.pending()} onClick={() => void remove.run(credential.id)}>
              {t("Remover conta")}
            </Button>
          </div>
        )}
      </For>
      <form
        class="grid-3"
        onSubmit={(event) => {
          event.preventDefault();
          void add.run();
        }}
      >
        <Select
          label={t("Provedor")}
          value={input.provider}
          onValue={(value) => setInput("provider", value as CloudProvider)}
          options={[{ value: "hetzner", label: PROVIDERS.hetzner }, { value: "digitalocean", label: PROVIDERS.digitalocean }]}
          error={add.errors().provider}
        />
        <TextInput label={t("Nome")} placeholder={t("minha-conta")} value={input.name} onValue={(value) => setInput("name", value)} error={add.errors().name} />
        <TextInput label={t("Token de API")} type="password" autocomplete="off" value={input.token} onValue={(value) => setInput("token", value)} error={add.errors().token} />
        <div>
          <Button type="submit" pending={add.pending()}>{t("Conectar conta")}</Button>
        </div>
      </form>
    </Section>
  );
}

function NewMachine(props: { credentials: CloudCredential[]; onCreated: () => void }) {
  const [input, setInput] = createStore({ credential_id: props.credentials[0].id, name: "", region: "", size: "", image: "", install_docker: true });
  const catalog = createQuery(() => input.credential_id, (id) => api.get<CloudCatalog>(`/cloud/credentials/${id}/catalog`));
  const create = createAction(() => api.post<CloudMachine>("/cloud/machines", input), {
    success: t("Criação iniciada. Acompanhe o andamento abaixo."),
    onSuccess: () => {
      setInput("name", "");
      props.onCreated();
    },
  });
  const options = (list: { id: string; name: string; detail: string }[]) =>
    list.map((option) => ({ value: option.id, label: [option.name || option.id, option.detail].filter(Boolean).join(" · ") }));
  return (
    <Section title={t("Criar servidor")} description={t("O Pier espera o SSH responder, cadastra o servidor e instala o Docker. Uma falha não apaga nada: destrua o servidor abaixo se não for usá-lo.")}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void create.run();
        }}
      >
        <div class="grid-2">
          <Select
            label={t("Conta")}
            value={input.credential_id}
            onValue={(value) => setInput({ credential_id: value, region: "", size: "", image: "" })}
            options={props.credentials.map((credential) => ({ value: credential.id, label: `${credential.name} (${PROVIDERS[credential.provider]})` }))}
          />
          <TextInput
            label={t("Nome do servidor")}
            placeholder={t("producao-1")}
            mono
            value={input.name}
            onValue={(value) => setInput("name", value)}
            hint={t("Letras minúsculas, números e hífens. Vira o nome no provedor e no Pier.")}
            error={create.errors().name}
          />
        </div>
        <QueryView query={catalog}>
          {(choices) => (
            <div class="grid-3">
              <Select label={t("Região")} value={input.region} placeholder={t("Escolha")} onValue={(value) => setInput("region", value)} options={options(choices().regions)} error={create.errors().region} />
              <Select label={t("Tamanho")} value={input.size} placeholder={t("Escolha")} onValue={(value) => setInput("size", value)} options={options(choices().sizes)} error={create.errors().size} />
              <Select label={t("Imagem")} value={input.image} placeholder={t("Escolha")} onValue={(value) => setInput("image", value)} options={options(choices().images)} error={create.errors().image} hint={t("Só sistemas em que o Pier sabe instalar o Docker.")} />
            </div>
          )}
        </QueryView>
        <Checkbox label={t("Instalar o Docker pelo script oficial")} checked={input.install_docker} onChange={(value) => setInput("install_docker", value)} />
        <div class="form-actions">
          <Button type="submit" variant="primary" icon="plus" pending={create.pending()}>{t("Criar servidor")}</Button>
        </div>
      </form>
    </Section>
  );
}

function MachineRow(props: { machine: CloudMachine; onChanged: () => void }) {
  const [open, setOpen] = createSignal(machineBusy(props.machine.status));
  const [destroying, setDestroying] = createSignal(false);
  const detail = createQuery(() => open() && props.machine.id, (id) => api.get<CloudMachine>(`/cloud/machines/${id}`), {
    pollMs: () => (machineBusy(props.machine.status) ? 3000 : 0),
  });
  const destroy = createAction(
    (confirmation: string) => api.delete(`/cloud/machines/${props.machine.id}?${new URLSearchParams({ confirmation })}`),
    { success: t("Servidor destruído no provedor e removido do Pier."), onSuccess: () => { setDestroying(false); props.onChanged(); } },
  );
  return (
    <div class="list-item stack-sm" style={{ "align-items": "stretch" }}>
      <div class="row-between">
        <div class="stack-xs">
          <strong>{props.machine.name}</strong>
          <span class="small muted">
            {PROVIDERS[props.machine.provider]} · {props.machine.region} · {props.machine.size} · {props.machine.image}
            <Show when={props.machine.ipv4}> · <span class="mono">{props.machine.ipv4}</span></Show>
            {" · "}{formatRelative(props.machine.created_at)}
          </span>
        </div>
        <div class="row">
          <StatusBadge status={machineStatus(props.machine.status)} />
          <Show when={props.machine.server_id}>
            <A href={`/servers/${props.machine.server_id}`} class="btn btn-sm">{t("Abrir servidor")}</A>
          </Show>
          <Button small variant="ghost" onClick={() => setOpen(!open())}>{open() ? t("Ocultar andamento") : t("Ver andamento")}</Button>
          <Button small variant="danger" icon="trash" disabled={machineBusy(props.machine.status)} onClick={() => setDestroying(true)}>{t("Destruir")}</Button>
        </div>
      </div>
      <Show when={props.machine.error}>
        <Alert tone="danger">{props.machine.error}</Alert>
      </Show>
      <Show when={open()}>
        <Show when={detail.data()?.operation?.lines.length} fallback={<p class="small muted">{t("Sem andamento registrado desde que o Pier iniciou.")}</p>}>
          <pre class="log" style={{ height: "auto", "max-height": "280px", margin: "0" }}>
            <For each={detail.data()?.operation?.lines ?? []}>{(line) => <div>{line}</div>}</For>
          </pre>
        </Show>
      </Show>
      <Show when={destroying()}>
        <ConfirmTyped
          title={t("Destruir servidor")}
          name={props.machine.name}
          description={t("O servidor será apagado no provedor, com o disco e tudo o que ele guarda, e removido do Pier. Não há como desfazer. Servidores que ainda hospedam recursos são recusados.")}
          action={t("Destruir definitivamente")}
          danger
          pending={destroy.pending()}
          onConfirm={(typed) => void destroy.run(typed)}
          onClose={() => setDestroying(false)}
        />
      </Show>
    </div>
  );
}

