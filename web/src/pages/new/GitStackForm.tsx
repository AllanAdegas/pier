import { useNavigate } from "@solidjs/router";
import { createSignal, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert } from "../../components/Feedback";
import { Select, TextArea, TextInput } from "../../components/Form";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import type { PrivateKey } from "../../lib/types";
import { t } from "../../i18n";
import { parseLines, usesSsh } from "../stack/gitSource";
import type { Placement } from "../NewResource";
import { createStack } from "./stackCreation";
import { GitRepositoryPicker, linkPickedRepository } from "../settings/GitRepositoryPicker";
import type { GitHubPick } from "../settings/gitLinks";

/**
 * Creates a stack that runs the docker-compose file of a Git repository. The
 * repository is cloned on the server at every deployment; domains and
 * variables are set after the first one, when the services are known.
 */
export function GitStackForm(props: { placement: Placement }) {
  const navigate = useNavigate();
  const [serverId, setServerId] = createSignal(props.placement.servers[0]?.id ?? "");
  const [name, setName] = createSignal("");
  const [repository, setRepository] = createSignal("");
  const [branch, setBranch] = createSignal("main");
  const [composePath, setComposePath] = createSignal("docker-compose.yaml");
  const [baseDirectory, setBaseDirectory] = createSignal("");
  const [keyId, setKeyId] = createSignal("");
  const [watchPaths, setWatchPaths] = createSignal("");
  const [githubPick, setGithubPick] = createSignal<GitHubPick | null>(null);
  const keys = createQuery(
    () => true,
    () => api.get<PrivateKey[]>("/keys"),
  );

  const create = createAction(
    async (deploy: boolean) => {
      // Linked before the first deploy, so a private repository clones.
      const stack = await createStack(
        {
          environment_id: props.placement.environmentId,
          server_id: serverId(),
          name: name(),
          git: {
            repository: repository().trim(),
            branch: branch().trim(),
            compose_path: composePath().trim(),
            base_directory: baseDirectory().trim(),
            private_key_id: keyId(),
            watch_paths: parseLines(watchPaths()),
          },
        },
        false,
      );
      await linkPickedRepository("stacks", stack.id, githubPick(), repository());
      if (deploy) {
        await api.post(`/stacks/${stack.id}/actions/deploy`).catch(() => undefined);
      }
      return stack;
    },
    {
      success: t("Stack criada."),
      onSuccess: (stack) => navigate(`/stacks/${stack.id}?tab=overview`),
    },
  );
  const errors = () => create.errors();

  return (
    <form
      class="card stack"
      onSubmit={(event) => {
        event.preventDefault();
        void create.run(true);
      }}
    >
      <Select
        label={t("Servidor")}
        hint={t("O repositório é clonado e construído neste servidor.")}
        value={serverId()}
        onValue={setServerId}
        options={props.placement.servers.map((server) => ({ value: server.id, label: server.name }))}
        error={errors().server_id}
      />
      <TextInput label={t("Nome")} required value={name()} onValue={setName} error={errors().name} />
      <GitRepositoryPicker onPick={(pick, url) => { setGithubPick(pick); if (url) setRepository(url); }} />
      <TextInput
        label={t("Repositório")}
        required
        mono
        placeholder={t("https://github.com/empresa/app")}
        hint={t("URL https:// para repositórios públicos ou git@host:org/repo.git para privados.")}
        value={repository()}
        onValue={setRepository}
        error={errors()["git.repository"]}
      />
      <Show when={usesSsh(repository())}>
        <Select
          label={t("Chave de deploy")}
          hint={t("Cadastre a chave pública como deploy key no repositório. Gerencie chaves em Segurança.")}
          placeholder={t("Selecione uma chave")}
          value={keyId()}
          onValue={setKeyId}
          options={(keys.data() ?? []).map((key) => ({ value: key.id, label: key.name }))}
          error={errors()["git.private_key_id"]}
        />
      </Show>
      <div class="grid-2">
        <TextInput label="Branch" required mono value={branch()} onValue={setBranch} error={errors()["git.branch"]} />
        <TextInput
          label={t("Arquivo compose")}
          mono
          hint={t("Caminho dentro do repositório (ou da pasta base).")}
          value={composePath()}
          onValue={setComposePath}
          error={errors()["git.compose_path"]}
        />
      </div>
      <details>
        <summary class="small">{t("Pasta base e caminhos monitorados")}</summary>
        <div class="stack-sm" style={{ "margin-top": "var(--space-3)" }}>
          <TextInput
            label={t("Pasta base")}
            mono
            placeholder="deploy"
            hint={t("Opcional. Pasta do repositório onde está o arquivo compose.")}
            value={baseDirectory()}
            onValue={setBaseDirectory}
            error={errors()["git.base_directory"]}
          />
          <TextArea
            label={t("Caminhos monitorados")}
            mono
            rows={3}
            placeholder="deploy/**"
            hint={t("Opcional, um por linha. O webhook de push só implanta se o push alterar algum destes caminhos.")}
            value={watchPaths()}
            onValue={setWatchPaths}
            error={errors()["git.watch_paths"]}
          />
        </div>
      </details>
      <Alert tone="info">
        {t("O Pier clona o repositório a cada deploy. Dados gravados dentro dele (bind mounts como ./data) são recriados; use volumes nomeados para o que precisa ficar.")}
      </Alert>
      <div class="form-actions">
        <Button pending={create.pending()} onClick={() => void create.run(false)}>{t("Criar sem implantar")}
        </Button>
        <Button type="submit" variant="primary" icon="rocket" pending={create.pending()}>{t("Criar e implantar")}
        </Button>
      </div>
      <p class="small muted">{t("Domínios são configurados depois de criar, em “Compose e domínios”.")}</p>
    </form>
  );
}
