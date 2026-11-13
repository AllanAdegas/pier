import { useNavigate } from "@solidjs/router";
import { createSignal, For, Show } from "solid-js";
import { createStore, unwrap } from "solid-js/store";
import { Button, IconButton } from "../../components/Button";
import { VolumeBackups } from "../../components/VolumeBackups";
import { ConfirmDelete, deleteQuery, type DeleteOptions } from "../../components/Modal";
import { Section } from "../../components/Page";
import { api } from "../../lib/api";
import { BUILD_PACK_LABEL } from "../../lib/format";
import { createAction, createQuery } from "../../lib/query";
import type { Application, ApplicationConfig, BuildPack, PrivateKey, Storage } from "../../lib/types";
import { t } from "../../i18n";
import { Checkbox, LinesInput, NumberInput, Select, TextArea, TextInput } from "../../components/Form";
import { createDomainDnsCheck } from "./DomainDnsStatus";
import { EngineExtras } from "./EngineExtras";
import { ProcessesEditor } from "./ProcessesEditor";
import { AutoDomainNotice } from "./AutoDomainNotice";
import { DockerOptionsEditor } from "./DockerOptionsEditor";
import { RedeployNotice } from "./RedeployNotice";
import { CloneResource } from "../../components/CloneResource";

const GIT_PACKS: BuildPack[] = ["dockerfile", "railpack", "nixpacks", "static"];

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
    { success: t("Configuração salva. Implante para aplicar."), onSuccess: props.onSaved },
  );
  const remove = createAction(
    (options: DeleteOptions) =>
      api.delete(`/applications/${initial.id}?${deleteQuery(options)}`),
    { success: t("Aplicação removida."), onSuccess: () => navigate("/projects") },
  );
  const resetGitHostKey = createAction(
    () => api.post(`/applications/${initial.id}/git-host-key/reset`),
    { success: t("Confiança SSH do Git redefinida. A nova chave será registrada no próximo deploy.") },
  );
  const errors = save.errors;
  const dns = createDomainDnsCheck(() => initial.server_id);

  const addStorage = () => setConfig("storages", (list) => [...list, { kind: "volume", source: "", target: "" }]);
  const removeStorage = (index: number) => setConfig("storages", (list) => list.filter((_, position) => position !== index));

  return (
    <div class="stack-lg">
    <RedeployNotice applicationId={initial.id} />
    <form
      class="stack-lg"
      onSubmit={(event) => {
        event.preventDefault();
        void save.run();
      }}
    >
      <Section title={t("Geral")}>
        <TextInput label={t("Nome")} required value={name()} onValue={setName} error={errors().name} />
        <TextArea label={t("Descrição")} rows={2} value={description()} onValue={setDescription} />
      </Section>

      <Section title={t("Origem e build")}>
        <Show
          when={!isImage}
          fallback={
            <TextInput
              label={t("Imagem")}
              mono
              required
              value={config.source.image}
              onValue={(value) => setConfig("source", "image", value)}
              error={errors()["source.image"]}
            />
          }
        >
          <TextInput
            label={t("Repositório")}
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
              label={t("Commit fixo")}
              mono
              placeholder={t("HEAD da branch")}
              hint={t("Opcional: SHA para travar a versão.")}
              value={config.source.commit}
              onValue={(value) => setConfig("source", "commit", value)}
              error={errors()["source.commit"]}
            />
            <Select
              label={t("Chave de deploy")}
              placeholder={t("Nenhuma (repositório público)")}
              value={config.source.private_key_id}
              onValue={(value) => setConfig("source", "private_key_id", value)}
              options={(keys.data() ?? []).map((key) => ({ value: key.id, label: key.name }))}
              error={errors()["source.private_key_id"]}
            />
          </div>
          <div class="grid-2">
            <TextInput
              label={t("Tag fixa")}
              mono
              placeholder="v1.2.3"
              hint={t("Opcional: implanta esta tag em vez da branch.")}
              value={config.source.tag ?? ""}
              onValue={(value) => setConfig("source", "tag", value)}
              error={errors()["source.tag"]}
            />
            <TextInput
              label={t("Padrão de tags do deploy automático")}
              mono
              placeholder="v*"
              hint={t("Opcional: o webhook implanta só tags que casam com o padrão e ignora pushes em branches.")}
              value={config.source.tag_pattern ?? ""}
              onValue={(value) => setConfig("source", "tag_pattern", value)}
              error={errors()["source.tag_pattern"]}
            />
          </div>
          <LinesInput label={t("Caminhos monitorados")} hint={t("Um glob por linha, ex.: apps/api/**. Vazio implanta qualquer push na branch.")} value={config.source.watch_paths ?? []} onValue={(value) => setConfig("source", "watch_paths", value)} error={errors()["source.watch_paths"]} />
          <div class="stack-sm">
            <span class="muted">{t("Se o host Git mudou de chave, confirme a nova chave com o administrador do repositório antes de redefinir a confiança.")}</span>
            <Button small onClick={() => void resetGitHostKey.run()} pending={resetGitHostKey.pending()}>
              {t("Redefinir confiança SSH do Git")}
            </Button>
          </div>
          <div class="grid-3">
            <Select
              label={t("Build pack")}
              value={config.build.pack}
              onValue={(value) => {
                setConfig("build", "pack", value as BuildPack);
                if (value !== "dockerfile") setConfig("build", "target", "");
              }}
              options={GIT_PACKS.map((value) => ({ value, label: BUILD_PACK_LABEL[value] }))}
            />
            <TextInput
              label={t("Diretório base")}
              mono
              hint={t("Raiz do build dentro do repositório.")}
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
                label={t("Diretório publicado")}
                mono
                hint={t("Pasta com o index.html.")}
                value={config.build.publish_directory}
                onValue={(value) => setConfig("build", "publish_directory", value)}
                error={errors()["build.publish_directory"]}
              />
            </Show>
          </div>
          <Show when={config.build.pack === "static"}>
            <Checkbox
              label={t("Single-page application")}
              hint={t("Rotas desconhecidas devolvem index.html em vez de 404.")}
              checked={config.build.spa}
              onChange={(checked) => setConfig("build", "spa", checked)}
            />
          </Show>
          <Show when={config.build.pack === "nixpacks" || config.build.pack === "railpack"}>
            <div class="grid-2">
              <TextInput
                label={t("Comando de instalação")}
                mono
                placeholder={t("detectado automaticamente")}
                value={config.build.install_command}
                onValue={(value) => setConfig("build", "install_command", value)}
              />
              <TextInput
                label={t("Comando de build")}
                mono
                placeholder={t("detectado automaticamente")}
                value={config.build.build_command}
                onValue={(value) => setConfig("build", "build_command", value)}
              />
            </div>
          </Show>
        </Show>
        <Show when={config.build.pack !== "static"}>
          <TextInput
            label={t("Comando de início")}
            mono
            placeholder={t("padrão da imagem")}
            hint={t("Opcional: substitui o CMD da imagem.")}
            value={config.build.start_command}
            onValue={(value) => setConfig("build", "start_command", value)}
          />
        </Show>
      </Section>

      <EngineExtras config={config} errors={errors()}
        onTarget={(target) => setConfig("build", "target", target)}
        onRuntime={(runtime) => setConfig("runtime", runtime)} />

      <Section title={t("Rede")}>
        <div onFocusOut={() => void dns.check(config.network.domains)}>
          <LinesInput
            label={t("Domínios")}
            hint={t("Um por linha. Use https:// para emitir certificado e redirecionar HTTP. Aceita caminho, ex.: https://site.com/api.")}
            placeholder={t("https://app.exemplo.com")}
            value={config.network.domains}
            onValue={(value) => setConfig("network", "domains", value)}
            error={errors()["network.domains"]}
          />
        </div>
        <dns.View />
        <AutoDomainNotice applicationId={initial.id} onDomains={(domains) => setConfig("network", "domains", domains)} />
        <div class="grid-2">
          <Show when={config.build.pack !== "static"}>
            <NumberInput
              label={t("Porta da aplicação")}
              min={1}
              max={65535}
              value={config.network.port}
              onValue={(value) => setConfig("network", "port", value)}
              error={errors()["network.port"]}
            />
          </Show>
          <LinesInput
            label={t("Portas publicadas no host")}
            hint={t("host:container, uma por linha. Desativa o rolling update (há indisponibilidade no deploy).")}
            placeholder="8080:3000"
            value={config.network.port_mappings}
            onValue={(value) => setConfig("network", "port_mappings", value)}
            error={errors()["network.port_mappings"]}
          />
        </div>
      </Section>

      <Section
        title="Healthcheck"
        description={t("Com healthcheck, o tráfego só migra para a nova versão depois que ela responde; sem ele, basta o container ficar de pé.")}
      >
        <Checkbox
          label={t("Ativar healthcheck")}
          hint={t("A imagem precisa ter curl ou wget, a menos que você informe um comando próprio.")}
          checked={config.health.enabled}
          onChange={(checked) => setConfig("health", "enabled", checked)}
        />
        <Show when={config.health.enabled}>
          <div class="grid-2">
            <TextInput
              label={t("Caminho HTTP")}
              mono
              value={config.health.path}
              onValue={(value) => setConfig("health", "path", value)}
              error={errors()["health.path"]}
            />
            <TextInput
              label={t("Comando próprio")}
              mono
              placeholder={t("usa o caminho HTTP")}
              hint={t("Opcional: substitui a verificação HTTP.")}
              value={config.health.command}
              onValue={(value) => setConfig("health", "command", value)}
            />
          </div>
          <div class="grid-2">
            <NumberInput
              label={t("Intervalo (s)")}
              min={1}
              value={config.health.interval_seconds}
              onValue={(value) => setConfig("health", "interval_seconds", value)}
            />
            <NumberInput
              label={t("Timeout (s)")}
              min={1}
              value={config.health.timeout_seconds}
              onValue={(value) => setConfig("health", "timeout_seconds", value)}
            />
            <NumberInput
              label={t("Tentativas")}
              min={1}
              value={config.health.retries}
              onValue={(value) => setConfig("health", "retries", value)}
              error={errors()["health.retries"]}
            />
            <NumberInput
              label={t("Período inicial (s)")}
              hint={t("Tempo de carência antes de contar falhas.")}
              min={0}
              value={config.health.start_period_seconds}
              onValue={(value) => setConfig("health", "start_period_seconds", value)}
              error={errors()["health.start_period_seconds"]}
            />
          </div>
        </Show>
      </Section>

      <Section
        title={t("Armazenamento persistente")}
        description={t("Volumes sobrevivem aos deploys. Sem eles, tudo que o container grava é perdido a cada versão.")}
        actions={
          <Button small icon="plus" onClick={addStorage}>{t("Adicionar")}
          </Button>
        }
      >
        <Show when={config.storages.length > 0} fallback={<p class="muted">{t("Nenhum volume configurado.")}</p>}>
          <For each={config.storages}>
            {(storage, index) => (
              <div class="stack-sm">
                <div class="grid-3">
                  <Select
                    label={t("Tipo")}
                    value={storage.kind}
                    onValue={(value) => setConfig("storages", index(), "kind", value as Storage["kind"])}
                    options={[
                      { value: "volume", label: t("Volume Docker") },
                      { value: "bind", label: t("Diretório do host") },
                    ]}
                  />
                  <TextInput
                    label={storage.kind === "volume" ? t("Nome do volume") : t("Caminho no host")}
                    mono
                    placeholder={storage.kind === "volume" ? t("dados") : t("/srv/app/dados")}
                    value={storage.source}
                    onValue={(value) => setConfig("storages", index(), "source", value)}
                  />
                  <TextInput
                    label={t("Caminho no container")}
                    mono
                    placeholder="/app/data"
                    value={storage.target}
                    onValue={(value) => setConfig("storages", index(), "target", value)}
                  />
                </div>
                <div class="row-between">
                  <span class="field-error">{errors()[`storages.${index()}`]}</span>
                  <IconButton icon="trash" label={t("Remover volume")} onClick={() => removeStorage(index())} />
                </div>
              </div>
            )}
          </For>
        </Show>
      </Section>

      <ProcessesEditor
        processes={config.processes}
        onChange={(processes) => setConfig("processes", processes)}
        error={errors().processes}
      />
      <DockerOptionsEditor options={config.docker_options} errors={errors()} onChange={(options) => setConfig("docker_options", options)} />

      <Section title={t("Limites e comandos de deploy")}>
        <div class="grid-2">
          <TextInput
            label={t("CPUs")}
            mono
            placeholder={t("sem limite")}
            hint={t("Ex.: 0.5 ou 2.")}
            value={config.limits.cpus}
            onValue={(value) => setConfig("limits", "cpus", value)}
            error={errors()["limits.cpus"]}
          />
          <TextInput
            label={t("Memória")}
            mono
            placeholder={t("sem limite")}
            hint={t("Ex.: 512m ou 2g.")}
            value={config.limits.memory}
            onValue={(value) => setConfig("limits", "memory", value)}
            error={errors()["limits.memory"]}
          />
        </div>
        <TextInput label={t("Comando de release")} mono placeholder="npm run migrate" hint={t("Roda na nova imagem antes de subir a versão, inclusive no primeiro deploy. Se falhar, mantém a versão anterior.")} value={config.release?.command ?? ""} onValue={(value) => setConfig("release", { command: value })} />
        <div class="grid-2">
          <TextInput
            label={`${t("Comando pré-deploy")}: ${t("rodar no contêiner atual")}`}
            mono
            placeholder="php artisan down"
            hint={t("Roda no container da versão atual, antes de subir a nova.")}
            value={config.hooks.pre_deploy}
            onValue={(value) => setConfig("hooks", "pre_deploy", value)}
          />
          <TextInput
            label={t("Comando pós-deploy")}
            mono
            placeholder="npm run migrate"
            hint={t("Roda no container novo, depois que ele está pronto.")}
            value={config.hooks.post_deploy}
            onValue={(value) => setConfig("hooks", "post_deploy", value)}
          />
        </div>
      </Section>

      <div class="form-actions">
        <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar configuração")}
        </Button>
      </div>

      <Section title={t("Zona de perigo")} description={t("Remove a aplicação, seus containers, imagens e histórico de deploys.")}>
        <div>
          <Button variant="danger" icon="trash" onClick={() => setDeleting(true)}>{t("Remover aplicação")}
          </Button>
        </div>
      </Section>
      <Show when={deleting()}>
        <ConfirmDelete
          title={t("Remover aplicação")}
          resourceName={initial.name}
          environmentId={initial.environment_id}
          description={t("A aplicação sai do ar imediatamente. Esta ação não pode ser desfeita.")}
          volumesLabel={t("Apagar também os volumes Docker")}
          allowForce
          pending={remove.pending()}
          onConfirm={(options) => void remove.run(options)}
          onClose={() => setDeleting(false)}
        />
      </Show>
    </form>
    <VolumeBackups kind="applications" id={initial.id} name={initial.name} />
    <CloneResource kind="applications" id={initial.id} name={initial.name} environmentId={initial.environment_id} />
    </div>
  );
}
