import { useNavigate } from "@solidjs/router";
import { createEffect, createSignal, on, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert } from "../../components/Feedback";
import { FolderPicker } from "../../components/FolderPicker";
import { createAction } from "../../lib/query";
import type { FolderListing } from "../../lib/types";
import type { Placement } from "../NewResource";
import { createStack } from "./stackCreation";
import { Trans } from "../../i18n/Trans";
import { t } from "../../i18n";
import { Select, TextInput } from "../../components/Form";

/**
 * Creates a stack that runs the compose file of a folder of the server. The
 * folder stays the source: its .env, env files and secret files are read
 * where they are, and nothing is copied into Pier.
 */
export function DirectoryStackForm(props: { placement: Placement }) {
  const navigate = useNavigate();
  const [serverId, setServerId] = createSignal(props.placement.servers[0]?.id ?? "");
  const [folder, setFolder] = createSignal<FolderListing | null>(null);
  const [composeFile, setComposeFile] = createSignal("");
  const [name, setName] = createSignal("");
  const [projectName, setProjectName] = createSignal("");
  // The name follows the folder until the person types their own.
  const [nameEdited, setNameEdited] = createSignal(false);

  createEffect(
    on(folder, (current) => {
      setComposeFile(current?.compose_files[0] ?? "");
      if (!nameEdited() && current) {
        setName(current.path.split("/").filter(Boolean).pop() ?? "");
      }
    }),
  );

  const create = createAction(
    async (deploy: boolean) => {
      const stack = await createStack(
        {
          environment_id: props.placement.environmentId,
          server_id: serverId(),
          name: name(),
          directory: folder()?.path ?? "",
          compose_file: composeFile(),
          project_name: projectName().trim(),
        },
        deploy,
      );
      return { stack, deploy };
    },
    {
      success: t("Stack criada."),
      // Without a deploy the next step is setting domains and variables.
      onSuccess: (result) => navigate(`/stacks/${result.stack.id}?tab=${result.deploy ? "overview" : "compose"}`),
    },
  );
  const usable = () => (folder()?.compose_files.length ?? 0) > 0;

  return (
    <div class="stack">
      <div class="card stack">
        <Select
          label={t("Servidor")}
          hint={t("A pasta é procurada no disco deste servidor.")}
          value={serverId()}
          onValue={setServerId}
          options={props.placement.servers.map((server) => ({ value: server.id, label: server.name }))}
          error={create.errors().server_id}
        />
        <FolderPicker serverId={serverId()} onChange={setFolder} />
      </div>

      <Show when={folder()}>
        {(current) => (
          <Show
            when={usable()}
            fallback={
              <Alert tone="info">
                <Trans
                  text="Nenhum arquivo compose em {path}. Abra a pasta do projeto: ela precisa ter um compose.yaml ou docker-compose.yml."
                  values={{ path: <span class="mono">{current().path}</span> }}
                />
              </Alert>
            }
          >
            <form
              class="card stack"
              onSubmit={(event) => {
                event.preventDefault();
                void create.run(true);
              }}
            >
              <div>
                <h2>{t("Usar esta pasta")}</h2>
                <p class="mono small muted" style={{ "word-break": "break-all" }}>
                  {current().path}
                </p>
              </div>
              <Alert tone={current().env_file ? "success" : "info"}>
                {current().env_file
                  ? t("Esta pasta tem um .env: o compose usa as variáveis dele, direto do disco.")
                  : t("Esta pasta não tem .env. Variáveis podem ser definidas no Pier depois de criar.")}
              </Alert>
              <p class="small muted">
                <Trans
                  text="O Pier lê o compose desta pasta a cada deploy. Caminhos relativos, {envFile} e arquivos de {secrets} continuam sendo lidos daqui e não são copiados. O que o Pier acrescenta (labels e a rede do proxy) fica em um arquivo de override na pasta dele."
                  values={{ envFile: <span class="mono">env_file</span>, secrets: <span class="mono">secrets</span> }}
                />
              </p>
              <Show when={current().compose_files.length > 1}>
                <Select
                  label={t("Arquivo compose")}
                  value={composeFile()}
                  onValue={setComposeFile}
                  options={current().compose_files.map((file) => ({ value: file, label: file }))}
                  error={create.errors().compose_file}
                />
              </Show>
              <Show when={create.errors().directory || (current().compose_files.length <= 1 && create.errors().compose_file)}>
                {(message) => <Alert tone="danger">{message()}</Alert>}
              </Show>
              <TextInput
                label={t("Nome")}
                required
                value={name()}
                onValue={(value) => {
                  setNameEdited(true);
                  setName(value);
                }}
                error={create.errors().name}
              />
              <details>
                <summary class="small">{t("Já rodei este compose à mão nesta pasta")}</summary>
                <div class="stack-sm" style={{ "margin-top": "var(--space-3)" }}>
                  <TextInput
                    label={t("Nome do projeto compose")}
                    mono
                    placeholder={name().toLowerCase()}
                    hint={t("Opcional. Informe o nome do projeto existente (por padrão, o nome da pasta) para o Pier assumir os mesmos containers e volumes. Em branco, o Pier cria um projeto novo, com volumes novos.")}
                    value={projectName()}
                    onValue={setProjectName}
                    error={create.errors().project_name}
                  />
                </div>
              </details>
              <div class="form-actions">
                <Button pending={create.pending()} onClick={() => void create.run(false)}>{t("Criar sem implantar")}
                </Button>
                <Button type="submit" variant="primary" icon="rocket" pending={create.pending()}>{t("Criar e implantar")}
                </Button>
              </div>
              <p class="small muted">{t("Domínios são configurados depois de criar, em “Compose e domínios”.")}</p>
            </form>
          </Show>
        )}
      </Show>
    </div>
  );
}
