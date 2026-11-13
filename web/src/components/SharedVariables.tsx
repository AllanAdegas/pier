import { createSignal, For, Show } from "solid-js";
import { t } from "../i18n";
import { api } from "../lib/api";
import { createAction, createQuery } from "../lib/query";
import type { SharedScope, SharedVariable } from "../lib/types";
import { Button, IconButton } from "./Button";
import { Checkbox, TextInput } from "./Form";
import { Section } from "./Page";

/**
 * Shared variables of the project and of the selected environment.
 * Applications use them as ${{project.NAME}} / ${{environment.NAME}}.
 */
export function SharedVariables(props: { projectId: string; environmentId?: string }) {
  return (
    <>
      <SharedScopeSection scope="project" path={`/projects/${props.projectId}`} title={t("Variáveis compartilhadas do projeto")} />
      <Show when={props.environmentId}>
        {(id) => <SharedScopeSection scope="environment" path={`/environments/${id()}`} title={t("Variáveis compartilhadas do ambiente")} />}
      </Show>
    </>
  );
}

function SharedScopeSection(props: { scope: SharedScope; path: string; title: string }) {
  const variables = createQuery(() => props.path, (path) => api.get<SharedVariable[]>(`${path}/shared-variables`));
  const [key, setKey] = createSignal("");
  const [value, setValue] = createSignal("");
  const [secret, setSecret] = createSignal(true);
  const save = createAction(
    () => api.post<SharedVariable>(`${props.path}/shared-variables`, { key: key(), value: value(), secret: secret() }),
    {
      success: t("Variável salva. Aplicações que a usam precisam de um novo deploy."),
      onSuccess: () => {
        setKey("");
        setValue("");
        void variables.refetch();
      },
    },
  );
  const remove = createAction((name: string) => api.delete(`${props.path}/shared-variables/${encodeURIComponent(name)}`), {
    success: t("Variável removida."),
    onSuccess: () => void variables.refetch(),
  });
  return (
    <Section title={props.title} description={t("Use nas variáveis da aplicação como {reference}.", { reference: `\${{${props.scope}.NOME}}` })}>
      <For each={variables.data() ?? []}>
        {(variable) => (
          <div class="row-between">
            <code>{`\${{${props.scope}.${variable.key}}}`}</code>
            <span class="muted mono small grow">{variable.secret ? "••••••" : variable.value}</span>
            <IconButton icon="trash" label={t("Remover variável")} onClick={() => void remove.run(variable.key)} />
          </div>
        )}
      </For>
      <form class="stack" onSubmit={(event) => { event.preventDefault(); void save.run(); }}>
        <div class="grid-2">
          <TextInput label={t("Nome")} mono placeholder="API_URL" value={key()} onValue={setKey} error={save.errors().key} />
          <TextInput label={t("Valor")} mono type={secret() ? "password" : "text"} value={value()} onValue={setValue} error={save.errors().value}
            hint={t("Para trocar um segredo, salve o mesmo nome com o novo valor.")} />
        </div>
        <Checkbox label={t("Segredo (nunca é exibido de novo)")} checked={secret()} onChange={setSecret} />
        <div>
          <Button type="submit" small pending={save.pending()}>{t("Salvar variável")}</Button>
        </div>
      </form>
    </Section>
  );
}
