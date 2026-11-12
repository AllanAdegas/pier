import { useNavigate } from "@solidjs/router";
import { createSignal, For, Show } from "solid-js";
import { Button } from "../../components/Button";
import { QueryView } from "../../components/Feedback";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import type { Database, EngineInfo } from "../../lib/types";
import type { Placement } from "../NewResource";
import { t } from "../../i18n";
import { Select, TextInput } from "../../components/Form";

/** Provisions a database and starts it right away. */
export function DatabaseForm(props: { placement: Placement }) {
  const navigate = useNavigate();
  const engines = createQuery(
    () => true,
    () => api.get<EngineInfo[]>("/database-engines"),
  );
  const [engine, setEngine] = createSignal("postgresql");
  const [name, setName] = createSignal("");
  const [image, setImage] = createSignal("");
  const [serverId, setServerId] = createSignal(props.placement.servers[0]?.id ?? "");
  const current = () => engines.data()?.find((item) => item.engine === engine());

  const create = createAction(
    async () => {
      const database = await api.post<Database>("/databases", {
        environment_id: props.placement.environmentId,
        server_id: serverId(),
        name: name(),
        engine: engine(),
        image: image(),
      });
      // A failed start (e.g. image pull) should not lose the created record:
      // the user lands on the database page and can retry from there.
      await api.post(`/databases/${database.id}/actions/start`).catch(() => undefined);
      return database;
    },
    {
      success: t("Banco de dados criado."),
      onSuccess: (database) => navigate(`/databases/${database.id}`),
    },
  );

  return (
    <form
      class="card stack"
      onSubmit={(event) => {
        event.preventDefault();
        void create.run();
      }}
    >
      <QueryView query={engines}>
        {(list) => (
          <div class="stack-sm">
            <span class="field-label">{t("Engine")}</span>
            <div class="choice-grid">
              <For each={list()}>
                {(item) => (
                  <button
                    type="button"
                    class="choice"
                    aria-pressed={engine() === item.engine ? "true" : "false"}
                    onClick={() => {
                      setEngine(item.engine);
                      setImage("");
                    }}
                  >
                    <span class="choice-title">{item.label}</span>
                    <span class="choice-text">{item.description}</span>
                  </button>
                )}
              </For>
            </div>
          </div>
        )}
      </QueryView>

      <div class="grid-2">
        <TextInput
          label={t("Nome")}
          required
          placeholder={t("banco-principal")}
          value={name()}
          onValue={setName}
          error={create.errors().name}
        />
        <TextInput
          label={t("Imagem")}
          mono
          placeholder={current()?.default_image}
          hint={t("Deixe vazio para usar a versão padrão.")}
          value={image()}
          onValue={setImage}
          error={create.errors()["config.image"]}
        />
      </div>
      <Select
        label={t("Servidor")}
        value={serverId()}
        onValue={setServerId}
        options={props.placement.servers.map((server) => ({ value: server.id, label: server.name }))}
        error={create.errors().server_id}
      />
      <Show when={current() && !current()!.supports_backup}>
        <p class="muted small">{t("{engine} ainda não tem backup automático no Pier.", { engine: current()!.label })}</p>
      </Show>
      <p class="muted small">{t("Usuário e senha são gerados automaticamente e ficam visíveis na página do banco.")}</p>

      <div class="form-actions">
        <Button type="submit" variant="primary" icon="database" pending={create.pending()}>{t("Criar e iniciar")}
        </Button>
      </div>
    </form>
  );
}
