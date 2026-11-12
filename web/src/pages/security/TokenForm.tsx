import { createSignal, For, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Modal } from "../../components/Modal";
import { api } from "../../lib/api";
import { createAction } from "../../lib/query";
import type { Ability, ApiToken } from "../../lib/types";
import { t } from "../../i18n";
import { Checkbox, TextInput } from "../../components/Form";

const ABILITIES: { value: Ability; label: string; hint: string }[] = [
  { value: "read", label: t("Leitura"), hint: t("Consultar recursos, deploys e logs. Não lê variáveis, credenciais nem backups.") },
  { value: "deploy", label: "Deploy", hint: t("Implantar, reiniciar, parar e disparar backups.") },
  { value: "write", label: t("Escrita"), hint: t("Tudo: criar, alterar e remover recursos, e ler segredos.") },
];

/** Creates an API token; the secret comes back once, in onSaved. */
export function TokenForm(props: {
  onClose: () => void;
  onSaved: (token: ApiToken) => void;
  initialName?: string;
  initialAbilities?: Ability[];
}) {
  const [name, setName] = createSignal(props.initialName ?? "");
  const [abilities, setAbilities] = createSignal<Ability[]>(props.initialAbilities ?? ["read"]);
  const toggle = (ability: Ability, enabled: boolean) =>
    setAbilities(enabled ? [...abilities(), ability] : abilities().filter((item) => item !== ability));
  const save = createAction(() => api.post<ApiToken>("/tokens", { name: name(), abilities: abilities() }), {
    onSuccess: props.onSaved,
  });
  return (
    <Modal title={t("Novo token de API")} onClose={props.onClose}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <TextInput label={t("Nome")} required placeholder="github-actions" value={name()} onValue={setName} error={save.errors().name} />
        <fieldset class="stack" style={{ border: "0", padding: "0", margin: "0" }}>
          <legend class="field-label" style={{ "margin-bottom": "var(--space-2)" }}>{t("Permissões")}
          </legend>
          <For each={ABILITIES}>
            {(ability) => (
              <Checkbox
                label={ability.label}
                hint={ability.hint}
                checked={abilities().includes(ability.value)}
                onChange={(checked) => toggle(ability.value, checked)}
              />
            )}
          </For>
          <Show when={save.errors().abilities}>
            <span class="field-error">{save.errors().abilities}</span>
          </Show>
        </fieldset>
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Criar token")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
