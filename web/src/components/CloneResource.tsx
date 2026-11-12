import { useNavigate } from "@solidjs/router";
import { createSignal, Show } from "solid-js";
import { t } from "../i18n";
import { api } from "../lib/api";
import { createAction, createQuery } from "../lib/query";
import type { CloneOptions, Project } from "../lib/types";
import { Button } from "./Button";
import { Alert } from "./Feedback";
import { Checkbox, Select, TextInput } from "./Form";
import { Modal } from "./Modal";
import { Section } from "./Page";

type CloneKind = "applications" | "databases" | "stacks";

/**
 * Copies the definition of a resource (never its data, image or history)
 * into an environment. Domains stay off by default to avoid two resources
 * answering on the same address; secrets only cross projects when checked.
 */
export function CloneResource(props: { kind: CloneKind; id: string; name: string; environmentId: string }) {
  const [open, setOpen] = createSignal(false);
  return (
    <Section title={t("Clonar")} description={t("Cria uma cópia da configuração em outro ambiente ou projeto, sem dados nem histórico.")}>
      <div>
        <Button icon="plus" onClick={() => setOpen(true)}>{t("Clonar recurso")}
        </Button>
      </div>
      <Show when={open()}>
        <CloneDialog {...props} onClose={() => setOpen(false)} />
      </Show>
    </Section>
  );
}

function CloneDialog(props: { kind: CloneKind; id: string; name: string; environmentId: string; onClose: () => void }) {
  const navigate = useNavigate();
  const projects = createQuery(() => true, () => api.get<Project[]>("/projects"));
  const [options, setOptions] = createSignal<CloneOptions>({
    target_environment_id: props.environmentId, name: "", copy_variables: true, copy_domains: false, copy_secrets: false,
  });
  const update = (patch: Partial<CloneOptions>) => setOptions({ ...options(), ...patch });
  const projectOf = (environmentId: string) => projects.data()?.find((project) => project.environments.some((item) => item.id === environmentId))?.id;
  const crossProject = () => projectOf(options().target_environment_id) !== projectOf(props.environmentId);
  const environments = () =>
    (projects.data() ?? []).flatMap((project) => project.environments.map((item) => ({ value: item.id, label: `${project.name} / ${item.name}` })));
  const clone = createAction(() => api.post<{ id: string }>(`/${props.kind}/${props.id}/clone`, options()), {
    success: t("Recurso clonado."),
    onSuccess: (created) => navigate(`/${props.kind}/${created.id}`),
  });
  return (
    <Modal title={t("Clonar {name}", { name: props.name })} onClose={props.onClose}>
      <form class="stack" onSubmit={(event) => { event.preventDefault(); void clone.run(); }}>
        <Select label={t("Ambiente de destino")} value={options().target_environment_id} options={environments()}
          onValue={(target_environment_id) => update({ target_environment_id })} error={clone.errors().target_environment_id} />
        <TextInput label={t("Nome")} placeholder={props.name} value={options().name} onValue={(name) => update({ name })} />
        <Checkbox label={t("Copiar variáveis")} checked={options().copy_variables} onChange={(copy_variables) => update({ copy_variables })} />
        <Checkbox label={t("Copiar domínios")} hint={t("Desligado por padrão: dois recursos no mesmo domínio entram em conflito.")}
          checked={options().copy_domains} onChange={(copy_domains) => update({ copy_domains })} />
        <Show when={crossProject()}>
          <Checkbox label={t("Copiar também os valores secretos para o outro projeto")}
            hint={t("Sem esta opção, as variáveis chegam vazias (exceto referências) e tokens saem da URL do repositório.")}
            checked={options().copy_secrets} onChange={(copy_secrets) => update({ copy_secrets })} />
          <Show when={options().copy_secrets}>
            <Alert tone="warning">{t("Quem tiver acesso ao outro projeto verá esses segredos.")}</Alert>
          </Show>
        </Show>
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" pending={clone.pending()}>{t("Clonar")}</Button>
        </div>
      </form>
    </Modal>
  );
}
