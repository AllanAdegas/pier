import { useNavigate } from "@solidjs/router";
import { createSignal, For, Show } from "solid-js";
import { createStore, unwrap } from "solid-js/store";
import { Button, IconButton } from "../../components/Button";
import { Checkbox, LinesInput, NumberInput, Select, TextArea, TextInput } from "../../components/Form";
import { ConfirmDelete } from "../../components/Modal";
import { Section } from "../../components/Page";
import { api } from "../../lib/api";
import { BUILD_PACK_LABEL } from "../../lib/format";
import { createAction, createQuery } from "../../lib/query";
import type { Application, ApplicationConfig, BuildPack, PrivateKey, Storage } from "../../lib/types";

const GIT_PACKS: BuildPack[] = ["dockerfile", "nixpacks", "static"];

/**
 * Full definition of the application. The form keeps its own copy of the
 * config so background refreshes of the page never overwrite what the user
 * is typing; changes apply on the next deployment.
 */
export function ConfigTab(props: { application: Application; onSaved: (application: Application) => void }) {
  const navigate = useNavigate();
  // Read once, on purpose: see the comment above.
  const initial = props.application;
  const [name, setName] = createSignal(initial.name);
  const [description, setDescription] = createSignal(initial.description);
  const [config, setConfig] = createStore<ApplicationConfig>(structuredClone(unwrap(initial.config)));
  const [deleting, setDeleting] = createSignal(false);
  const isImage = config.build.pack === "image";

  const keys = createQuery(
    () => !isImage,
    () => api.get<PrivateKey[]>("/keys"),
  );
  const save = createAction(
    () => api.patch<Application>(`/applications/${initial.id}`, { name: name(), description: description(), config }),
    { success: "Configuração salva. Implante para aplicar.", onSuccess: props.onSaved },
  );
  const remove = createAction(
    (options: { volumes: boolean; force: boolean }) =>
      api.delete(`/applications/${initial.id}?volumes=${options.volumes ? 1 : 0}&force=${options.force ? 1 : 0}`),
    { success: "Aplicação removida.", onSuccess: () => navigate("/projects") },
  );
  const errors = save.errors;

  const addStorage = () => setConfig("storages", (list) => [...list, { kind: "volume", source: "", target: "" }]);
  const removeStorage = (index: number) => setConfig("storages", (list) => list.filter((_, position) => position !== index));

  return (
    <form
      class="stack-lg"
      onSubmit={(event) => {
        event.preventDefault();
        void save.run();
      }}
    >
      <Section title="Geral">
        <TextInput label="Nome" required value={name()} onValue={setName} error={errors().name} />
        <TextArea label="Descrição" rows={2} value={description()} onValue={setDescription} />
      </Section>

      <Section title="Origem e build">
        <Show
          when={!isImage}
          fallback={
            <TextInput
              label="Imagem"
              mono
              required
              value={config.source.image}
              onValue={(value) => setConfig("source", "image", value)}
              error={errors()["source.image"]}
            />
          }
        >
          <TextInput
            label="Repositório"
            mono
            required
            value={config.source.repository}
            onValue={(value) => setConfig("source", "repository", value)}
            error={errors()["source.repository"]}
          />
          <div class="grid-3">
            <TextInput
              label="Branch"
              mono
              value={config.source.branch}
              onValue={(value) => setConfig("source", "branch", value)}
              error={errors()["source.branch"]}
            />
            <TextInput
              label="Commit fixo"
              mono
              placeholder="HEAD da branch"
              hint="Opcional: SHA para travar a versão."
              value={config.source.commit}
              onValue={(value) => setConfig("source", "commit", value)}
              error={errors()["source.commit"]}
            />
            <Select
              label="Chave de deploy"
              placeholder="Nenhuma (repositório público)"
              value={config.source.private_key_id}
              onValue={(value) => setConfig("source", "private_key_id", value)}
              options={(keys.data() ?? []).map((key) => ({ value: key.id, label: key.name }))}
              error={errors()["source.private_key_id"]}
            />
          </div>
          <div class="grid-3">
            <Select
              label="Build pack"
              value={config.build.pack}
              onValue={(value) => setConfig("build", "pack", value as BuildPack)}
              options={GIT_PACKS.map((value) => ({ value, label: BUILD_PACK_LABEL[value] }))}
            />
            <TextInput
              label="Diretório base"
              mono
              hint="Raiz do build dentro do repositório."
              value={config.build.base_directory}
              onValue={(value) => setConfig("build", "base_directory", value)}
              error={errors()["build.base_directory"]}
            />
            <Show when={config.build.pack === "dockerfile"}>
              <TextInput
                label="Dockerfile"
                mono
                value={config.build.dockerfile_path}
                onValue={(value) => setConfig("build", "dockerfile_path", value)}
                error={errors()["build.dockerfile_path"]}
              />
            </Show>
            <Show when={config.build.pack === "static"}>
              <TextInput
                label="Diretório publicado"
                mono
                hint="Pasta com o index.html."
                value={config.build.publish_directory}
                onValue={(value) => setConfig("build", "publish_directory", value)}
                error={errors()["build.publish_directory"]}
              />
            </Show>
          </div>
          <Show when={config.build.pack === "static"}>
            <Checkbox
              label="Single-page application"
              hint="Rotas desconhecidas devolvem index.html em vez de 404."
              checked={config.build.spa}
              onChange={(checked) => setConfig("build", "spa", checked)}
            />
          </Show>
          <Show when={config.build.pack === "nixpacks"}>
            <div class="grid-2">
              <TextInput
                label="Comando de instalação"
                mono
                placeholder="detectado automaticamente"
                value={config.build.install_command}
                onValue={(value) => setConfig("build", "install_command", value)}
              />
              <TextInput
                label="Comando de build"
                mono
                placeholder="detectado automaticamente"
                value={config.build.build_command}
                onValue={(value) => setConfig("build", "build_command", value)}
              />
            </div>
          </Show>
        </Show>
        <Show when={config.build.pack !== "static"}>
          <TextInput
            label="Comando de início"
            mono
            placeholder="padrão da imagem"
            hint="Opcional: substitui o CMD da imagem."
            value={config.build.start_command}
            onValue={(value) => setConfig("build", "start_command", value)}
          />
        </Show>
      </Section>

      <Section title="Rede">
        <LinesInput
          label="Domínios"
          hint="Um por linha. Use https:// para emitir certificado e redirecionar HTTP. Aceita caminho, ex.: https://site.com/api."
          placeholder="https://app.exemplo.com"
          value={config.network.domains}
          onValue={(value) => setConfig("network", "domains", value)}
          error={errors()["network.domains"]}
        />
        <div class="grid-2">
          <Show when={config.build.pack !== "static"}>
            <NumberInput
              label="Porta da aplicação"
              min={1}
              max={65535}
              value={config.network.port}
              onValue={(value) => setConfig("network", "port", value)}
              error={errors()["network.port"]}
            />
          </Show>
          <LinesInput
            label="Portas publicadas no host"
            hint="host:container, uma por linha. Desativa o rolling update (há indisponibilidade no deploy)."
            placeholder="8080:3000"
            value={config.network.port_mappings}
            onValue={(value) => setConfig("network", "port_mappings", value)}
            error={errors()["network.port_mappings"]}
          />
        </div>
      </Section>

      <Section
        title="Healthcheck"
        description="Com healthcheck, o tráfego só migra para a nova versão depois que ela responde; sem ele, basta o container ficar de pé."
      >
        <Checkbox
          label="Ativar healthcheck"
          hint="A imagem precisa ter curl ou wget, a menos que você informe um comando próprio."
          checked={config.health.enabled}
          onChange={(checked) => setConfig("health", "enabled", checked)}
        />
        <Show when={config.health.enabled}>
          <div class="grid-2">
            <TextInput
              label="Caminho HTTP"
              mono
              value={config.health.path}
              onValue={(value) => setConfig("health", "path", value)}
              error={errors()["health.path"]}
            />
            <TextInput
              label="Comando próprio"
              mono
              placeholder="usa o caminho HTTP"
              hint="Opcional: substitui a verificação HTTP."
              value={config.health.command}
              onValue={(value) => setConfig("health", "command", value)}
            />
          </div>
          <div class="grid-2">
            <NumberInput
              label="Intervalo (s)"
              min={1}
              value={config.health.interval_seconds}
              onValue={(value) => setConfig("health", "interval_seconds", value)}
            />
            <NumberInput
              label="Timeout (s)"
              min={1}
              value={config.health.timeout_seconds}
              onValue={(value) => setConfig("health", "timeout_seconds", value)}
            />
            <NumberInput
              label="Tentativas"
              min={1}
              value={config.health.retries}
              onValue={(value) => setConfig("health", "retries", value)}
              error={errors()["health.retries"]}
            />
            <NumberInput
              label="Período inicial (s)"
              hint="Tempo de carência antes de contar falhas."
              min={0}
              value={config.health.start_period_seconds}
              onValue={(value) => setConfig("health", "start_period_seconds", value)}
              error={errors()["health.start_period_seconds"]}
            />
          </div>
        </Show>
      </Section>

      <Section
        title="Armazenamento persistente"
        description="Volumes sobrevivem aos deploys. Sem eles, tudo que o container grava é perdido a cada versão."
        actions={
          <Button small icon="plus" onClick={addStorage}>
            Adicionar
          </Button>
        }
      >
        <Show when={config.storages.length > 0} fallback={<p class="muted">Nenhum volume configurado.</p>}>
          <For each={config.storages}>
            {(storage, index) => (
              <div class="stack-sm">
                <div class="grid-3">
                  <Select
                    label="Tipo"
                    value={storage.kind}
                    onValue={(value) => setConfig("storages", index(), "kind", value as Storage["kind"])}
                    options={[
                      { value: "volume", label: "Volume Docker" },
                      { value: "bind", label: "Diretório do host" },
                    ]}
                  />
                  <TextInput
                    label={storage.kind === "volume" ? "Nome do volume" : "Caminho no host"}
                    mono
                    placeholder={storage.kind === "volume" ? "dados" : "/srv/app/dados"}
                    value={storage.source}
                    onValue={(value) => setConfig("storages", index(), "source", value)}
                  />
                  <TextInput
                    label="Caminho no container"
                    mono
                    placeholder="/app/data"
                    value={storage.target}
                    onValue={(value) => setConfig("storages", index(), "target", value)}
                  />
                </div>
                <div class="row-between">
                  <span class="field-error">{errors()[`storages.${index()}`]}</span>
                  <IconButton icon="trash" label="Remover volume" onClick={() => removeStorage(index())} />
                </div>
              </div>
            )}
          </For>
        </Show>
      </Section>

      <Section title="Limites e comandos de deploy">
        <div class="grid-2">
          <TextInput
            label="CPUs"
            mono
            placeholder="sem limite"
            hint="Ex.: 0.5 ou 2."
            value={config.limits.cpus}
            onValue={(value) => setConfig("limits", "cpus", value)}
            error={errors()["limits.cpus"]}
          />
          <TextInput
            label="Memória"
            mono
            placeholder="sem limite"
            hint="Ex.: 512m ou 2g."
            value={config.limits.memory}
            onValue={(value) => setConfig("limits", "memory", value)}
            error={errors()["limits.memory"]}
          />
        </div>
        <div class="grid-2">
          <TextInput
            label="Comando pré-deploy"
            mono
            placeholder="php artisan down"
            hint="Roda no container da versão atual, antes de subir a nova."
            value={config.hooks.pre_deploy}
            onValue={(value) => setConfig("hooks", "pre_deploy", value)}
          />
          <TextInput
            label="Comando pós-deploy"
            mono
            placeholder="npm run migrate"
            hint="Roda no container novo, depois que ele está pronto."
            value={config.hooks.post_deploy}
            onValue={(value) => setConfig("hooks", "post_deploy", value)}
          />
        </div>
      </Section>

      <div class="form-actions">
        <Button type="submit" variant="primary" pending={save.pending()}>
          Salvar configuração
        </Button>
      </div>

      <Section title="Zona de perigo" description="Remove a aplicação, seus containers, imagens e histórico de deploys.">
        <div>
          <Button variant="danger" icon="trash" onClick={() => setDeleting(true)}>
            Remover aplicação
          </Button>
        </div>
      </Section>
      <Show when={deleting()}>
        <ConfirmDelete
          title="Remover aplicação"
          resourceName={initial.name}
          description="A aplicação sai do ar imediatamente. Esta ação não pode ser desfeita."
          volumesLabel="Apagar também os volumes Docker"
          allowForce
          pending={remove.pending()}
          onConfirm={(options) => void remove.run(options)}
          onClose={() => setDeleting(false)}
        />
      </Show>
    </form>
  );
}
