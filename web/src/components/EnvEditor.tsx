import { createSignal, For, Show } from "solid-js";
import { api } from "../lib/api";
import { createAction, createQuery } from "../lib/query";
import type { Application, Database, EnvVariable } from "../lib/types";
import { Button, IconButton } from "./Button";
import { EmptyState, QueryView } from "./Feedback";
import { Checkbox, Select, TextArea, TextInput } from "./Form";
import { Modal } from "./Modal";
import { Section } from "./Page";
import { t } from "../i18n";

/**
 * Environment variables of a resource. `basePath` is the API prefix of the
 * owner (e.g. "/applications/abc"); the same editor serves apps and stacks.
 */
export function EnvEditor(props: { basePath: string; allowBuildTime?: boolean; note: string }) {
  const variables = createQuery(
    () => props.basePath,
    (base) => api.get<EnvVariable[]>(`${base}/envs`),
  );
  const application = createQuery(
    () => props.basePath.startsWith("/applications/") && props.basePath,
    (base) => api.get<Application>(base.replace(/\/previews$/, "")),
  );
  const databases = createQuery(
    () => application.data()?.environment_id,
    (environment) => api.get<Database[]>(`/databases?environment_id=${encodeURIComponent(environment)}`),
  );
  const [database, setDatabase] = createSignal("");
  const [secretBuild, setSecretBuild] = createSignal(false);
  const [key, setKey] = createSignal("");
  const [value, setValue] = createSignal("");
  const [buildTime, setBuildTime] = createSignal(false);
  const [revealed, setRevealed] = createSignal<Record<string, boolean>>({});
  const [bulkOpen, setBulkOpen] = createSignal(false);

  const add = createAction(
    () => api.post(`${props.basePath}/envs`, { key: key(), value: value(), build_time: buildTime(), secret_build: secretBuild() }),
    {
      success: t("Variável salva."),
      onSuccess: () => {
        setKey("");
        setValue("");
        setBuildTime(false);
        setSecretBuild(false);
        void variables.refetch();
      },
    },
  );
  const remove = createAction((id: string) => api.delete(`${props.basePath}/envs/${id}`), {
    success: t("Variável removida."),
    onSuccess: () => void variables.refetch(),
  });

  return (
    <Section
      title={t("Variáveis de ambiente")}
      description={props.note}
      actions={
        <Button small onClick={() => setBulkOpen(true)}>{t("Editar como .env")}
        </Button>
      }
    >
      <QueryView query={variables}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={
              <EmptyState icon="key" title={t("Nenhuma variável")}>{t("Adicione segredos e configurações abaixo. Os valores ficam criptografados no banco do Pier.")}
              </EmptyState>
            }
          >
            <div class="list">
              <For each={list()}>
                {(variable) => (
                  <div class="list-item">
                    <div class="grow stack-sm" style={{ gap: "2px" }}>
                      <span class="mono truncate">
                        <strong>{variable.key}</strong>
                      </span>
                      <span class="mono muted truncate">
                        {revealed()[variable.id] ? variable.value || "(vazio)" : "••••••••"}
                      </span>
                    </div>
                    <Show when={variable.secret_build}>
                      <span class="tag">{t("segredo de build")}</span>
                    </Show>
                    <Show when={variable.build_time && !variable.secret_build}>
                      <span class="tag">build</span>
                    </Show>
                    <IconButton
                      icon={revealed()[variable.id] ? "eye-off" : "eye"}
                      label={revealed()[variable.id] ? t("Ocultar {name}", { name: variable.key }) : t("Mostrar {name}", { name: variable.key })}
                      onClick={() => setRevealed({ ...revealed(), [variable.id]: !revealed()[variable.id] })}
                    />
                    <IconButton icon="trash" label={t("Remover {name}", { name: variable.key })} onClick={() => void remove.run(variable.id)} />
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>

      <Show when={application.data()}>
        <div class="grid-2">
          <Select
            label={t("Conectar a um banco")}
            placeholder={t("Selecione um banco deste ambiente")}
            value={database()}
            onValue={(id) => {
              setDatabase(id);
              if (id) {
                setKey("DATABASE_URL");
                setValue("${{" + id + ".url}}");
                setBuildTime(false);
                setSecretBuild(false);
              }
            }}
            options={(databases.data() ?? []).map((item) => ({ value: item.id, label: item.name }))}
            hint={t("A conexão atual do banco será resolvida no próximo deploy.")}
          />
        </div>
      </Show>

      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void add.run();
        }}
      >
        <div class="grid-2">
          <TextInput
            label={t("Nome")}
            mono
            placeholder="DATABASE_URL"
            value={key()}
            onValue={setKey}
            error={add.errors().key}
            autocomplete="off"
          />
          <TextInput label={t("Valor")} mono value={value()} onValue={setValue} autocomplete="off" />
        </div>
        <Show when={props.allowBuildTime}>
          <Checkbox
            label={t("Disponível durante o build")}
            hint={t("Passada como --build-arg ao docker build. Evite para segredos: build args ficam no histórico da imagem.")}
            checked={buildTime()}
            onChange={(checked) => { setBuildTime(checked); if (checked) setSecretBuild(false); }}
          />
          <Checkbox
            label={t("Segredo de build")}
            hint={t("Disponível apenas por mount do BuildKit durante o build; não vira build arg nem fica no histórico da imagem.")}
            checked={secretBuild()}
            onChange={(checked) => { setSecretBuild(checked); if (checked) setBuildTime(false); }}
          />
        </Show>
        <div class="form-actions">
          <Button type="submit" icon="plus" pending={add.pending()} disabled={!key().trim()}>{t("Adicionar variável")}
          </Button>
        </div>
      </form>

      <Show when={bulkOpen()}>
        <BulkEnvModal
          basePath={props.basePath}
          current={variables.data() ?? []}
          onClose={() => setBulkOpen(false)}
          onSaved={() => {
            setBulkOpen(false);
            void variables.refetch();
          }}
        />
      </Show>
    </Section>
  );
}

function toDotenv(variables: EnvVariable[]): string {
  return variables.map((variable) => `${variable.key}=${variable.value}`).join("\n");
}

function BulkEnvModal(props: { basePath: string; current: EnvVariable[]; onClose: () => void; onSaved: () => void }) {
  const [text, setText] = createSignal(toDotenv(props.current));
  // The backend parses the .env text, the single implementation of that format.
  const save = createAction(() => api.put(`${props.basePath}/envs`, { dotenv: text() }), {
    success: t("Variáveis substituídas."),
    onSuccess: props.onSaved,
  });

  return (
    <Modal title={t("Editar como .env")} onClose={props.onClose} wide>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <TextArea
          label={t("Conteúdo")}
          hint={t("Uma variável por linha, no formato CHAVE=valor. Salvar substitui todas as variáveis atuais.")}
          mono
          rows={14}
          value={text()}
          onValue={setText}
        />
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Substituir variáveis")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
