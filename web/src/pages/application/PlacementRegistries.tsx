import { createSignal, For, Show } from "solid-js";
import { Button, IconButton } from "../../components/Button";
import { EmptyState, QueryView } from "../../components/Feedback";
import { Select, TextInput } from "../../components/Form";
import { ConfirmDelete } from "../../components/Modal";
import { Section } from "../../components/Page";
import { t } from "../../i18n";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { REGISTRY_PRESETS, type Registry, type RegistryInput } from "./placementModel";

const EMPTY: RegistryInput = { name: "", host: "", username: "", password: "" };

/** Container registries shared by every application: the password is write-only. */
export function RegistriesSection() {
  const registries = createQuery(() => true, () => api.get<Registry[]>("/registries"));
  const [form, setForm] = createSignal<RegistryInput>({ ...EMPTY });
  const [removing, setRemoving] = createSignal<Registry>();
  const create = createAction(() => api.post<Registry>("/registries", form()), {
    success: t("Registro salvo. A senha fica cifrada e não é exibida de novo."),
    onSuccess: () => {
      setForm({ ...EMPTY });
      void registries.refetch();
    },
  });
  const verify = createAction((id: string) => api.post(`/registries/${id}/verify`), { success: t("O registro aceitou a credencial.") });
  const remove = createAction((id: string) => api.delete(`/registries/${id}`), {
    success: t("Registro removido."),
    onSuccess: () => {
      setRemoving(undefined);
      void registries.refetch();
    },
  });
  const field = (key: keyof RegistryInput) => (value: string) => setForm({ ...form(), [key]: value });

  return (
    <Section
      title={t("Registros de imagens")}
      description={t("Docker Hub, GHCR, GitLab ou qualquer registro OCI. Usados para baixar imagens privadas e distribuir builds entre servidores.")}
    >
      <QueryView query={registries}>
        {(list) => (
          <Show when={list().length > 0} fallback={<EmptyState icon="box" title={t("Nenhum registro")}>{t("Adicione um registro abaixo.")}</EmptyState>}>
            <ul class="stack-sm">
              <For each={list()}>
                {(registry) => (
                  <li class="row-between">
                    <span>
                      <strong>{registry.name}</strong> <span class="mono small muted">{registry.username}@{registry.host}</span>
                    </span>
                    <span class="row">
                      <Button small icon="check" pending={verify.pending()} onClick={() => void verify.run(registry.id)}>{t("Testar acesso")}
                      </Button>
                      <IconButton icon="trash" label={t("Remover registro")} onClick={() => setRemoving(registry)} />
                    </span>
                  </li>
                )}
              </For>
            </ul>
          </Show>
        )}
      </QueryView>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void create.run();
        }}
      >
        <Select
          label={t("Tipo de registro")}
          placeholder={t("Outro registro OCI")}
          value={REGISTRY_PRESETS.find((preset) => preset.host === form().host)?.host ?? ""}
          options={REGISTRY_PRESETS.map((preset) => ({ value: preset.host, label: preset.label }))}
          onValue={(host) => setForm({ ...form(), host, name: form().name || (REGISTRY_PRESETS.find((p) => p.host === host)?.label ?? "") })}
        />
        <TextInput label={t("Nome")} value={form().name} error={create.errors().name} onValue={field("name")} />
        <TextInput label={t("Host")} placeholder="registry.example.com" mono value={form().host} error={create.errors().host} onValue={field("host")} />
        <TextInput label={t("Usuário")} value={form().username} error={create.errors().username} autocomplete="off" onValue={field("username")} />
        <TextInput
          label={t("Senha ou token de acesso")}
          hint={t("Use um token com permissão de leitura e escrita de pacotes.")}
          type="password"
          autocomplete="new-password"
          value={form().password}
          error={create.errors().password}
          onValue={field("password")}
        />
        <div>
          <Button type="submit" icon="plus" pending={create.pending()}>{t("Adicionar registro")}
          </Button>
        </div>
      </form>
      <Show when={removing()}>
        {(registry) => (
          <ConfirmDelete
            title={t("Remover registro")}
            resourceName={registry().name}
            description={t("Aplicações que usam este registro precisam trocar de registro antes. Imagens já enviadas continuam no registro.")}
            pending={remove.pending()}
            onConfirm={() => void remove.run(registry().id)}
            onClose={() => setRemoving(undefined)}
          />
        )}
      </Show>
    </Section>
  );
}
