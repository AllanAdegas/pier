import { createSignal, For, Show } from "solid-js";
import { api } from "../lib/api";
import { createAction, createQuery } from "../lib/query";
import type { EnvVariable } from "../lib/types";
import { Button, IconButton } from "./Button";
import { EmptyState, QueryView } from "./Feedback";
import { Checkbox, TextArea, TextInput } from "./Form";
import { Modal } from "./Modal";
import { Section } from "./Page";

/**
 * Environment variables of a resource. `basePath` is the API prefix of the
 * owner (e.g. "/applications/abc"); the same editor serves apps and stacks.
 */
export function EnvEditor(props: { basePath: string; allowBuildTime?: boolean; note: string }) {
  const variables = createQuery(
    () => props.basePath,
    (base) => api.get<EnvVariable[]>(`${base}/envs`),
  );
  const [key, setKey] = createSignal("");
  const [value, setValue] = createSignal("");
  const [buildTime, setBuildTime] = createSignal(false);
  const [revealed, setRevealed] = createSignal<Record<string, boolean>>({});
  const [bulkOpen, setBulkOpen] = createSignal(false);

  const add = createAction(
    () => api.post(`${props.basePath}/envs`, { key: key(), value: value(), build_time: buildTime() }),
    {
      success: "Variável salva.",
      onSuccess: () => {
        setKey("");
        setValue("");
        setBuildTime(false);
        void variables.refetch();
      },
    },
  );
  const remove = createAction((id: string) => api.delete(`${props.basePath}/envs/${id}`), {
    success: "Variável removida.",
    onSuccess: () => void variables.refetch(),
  });

  return (
    <Section
      title="Variáveis de ambiente"
      description={props.note}
      actions={
        <Button small onClick={() => setBulkOpen(true)}>
          Editar como .env
        </Button>
      }
    >
      <QueryView query={variables}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={
              <EmptyState icon="key" title="Nenhuma variável">
                Adicione segredos e configurações abaixo. Os valores ficam criptografados no banco do Pier.
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
                    <Show when={variable.build_time}>
                      <span class="tag">build</span>
                    </Show>
                    <IconButton
                      icon={revealed()[variable.id] ? "eye-off" : "eye"}
                      label={revealed()[variable.id] ? `Ocultar ${variable.key}` : `Mostrar ${variable.key}`}
                      onClick={() => setRevealed({ ...revealed(), [variable.id]: !revealed()[variable.id] })}
                    />
                    <IconButton icon="trash" label={`Remover ${variable.key}`} onClick={() => void remove.run(variable.id)} />
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>

      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void add.run();
        }}
      >
        <div class="grid-2">
          <TextInput
            label="Nome"
            mono
            placeholder="DATABASE_URL"
            value={key()}
            onValue={setKey}
            error={add.errors().key}
            autocomplete="off"
          />
          <TextInput label="Valor" mono value={value()} onValue={setValue} autocomplete="off" />
        </div>
        <Show when={props.allowBuildTime}>
          <Checkbox
            label="Disponível durante o build"
            hint="Passada como --build-arg ao docker build. Evite para segredos: build args ficam no histórico da imagem."
            checked={buildTime()}
            onChange={setBuildTime}
          />
        </Show>
        <div class="form-actions">
          <Button type="submit" icon="plus" pending={add.pending()} disabled={!key().trim()}>
            Adicionar variável
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
    success: "Variáveis substituídas.",
    onSuccess: props.onSaved,
  });

  return (
    <Modal title="Editar como .env" onClose={props.onClose} wide>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <TextArea
          label="Conteúdo"
          hint="Uma variável por linha, no formato CHAVE=valor. Salvar substitui todas as variáveis atuais."
          mono
          rows={14}
          value={text()}
          onValue={setText}
        />
        <div class="form-actions">
          <Button onClick={props.onClose}>Cancelar</Button>
          <Button type="submit" variant="primary" pending={save.pending()}>
            Substituir variáveis
          </Button>
        </div>
      </form>
    </Modal>
  );
}
