import { useNavigate } from "@solidjs/router";
import { createSignal, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert } from "../../components/Feedback";
import { NumberInput, Select, TextInput } from "../../components/Form";
import { api } from "../../lib/api";
import { BUILD_PACK_LABEL } from "../../lib/format";
import { createAction, createQuery } from "../../lib/query";
import type { Application, ApplicationConfig, BuildPack, Deployment, PrivateKey } from "../../lib/types";
import type { Placement } from "../NewResource";

const GIT_PACKS: BuildPack[] = ["dockerfile", "nixpacks", "static"];

const PACK_HINT: Record<string, string> = {
  dockerfile: "Usa o Dockerfile do repositório.",
  nixpacks: "Detecta a linguagem e gera a imagem sozinho. Exige o Nixpacks instalado no servidor.",
  static: "Serve os arquivos do repositório com nginx (porta 80).",
};

/**
 * Creates an application and immediately starts its first deployment, so the
 * flow ends on the live log instead of an idle settings page.
 */
export function ApplicationForm(props: { placement: Placement; source: "git" | "image" }) {
  const navigate = useNavigate();
  const [name, setName] = createSignal("");
  const [repository, setRepository] = createSignal("");
  const [branch, setBranch] = createSignal("main");
  const [pack, setPack] = createSignal<BuildPack>(props.source === "image" ? "image" : "dockerfile");
  const [image, setImage] = createSignal("");
  const [port, setPort] = createSignal(3000);
  const [domain, setDomain] = createSignal("");
  const [serverId, setServerId] = createSignal(props.placement.servers[0]?.id ?? "");
  const [keyId, setKeyId] = createSignal("");

  const keys = createQuery(
    () => props.source === "git",
    () => api.get<PrivateKey[]>("/keys"),
  );
  const usesSsh = () => repository().trim() !== "" && !repository().trim().startsWith("http");

  const create = createAction(
    async () => {
      const config = await api.get<ApplicationConfig>(`/applications/defaults?pack=${pack()}`);
      config.source = { ...config.source, repository: repository(), branch: branch(), private_key_id: keyId(), image: image() };
      config.network = { ...config.network, port: pack() === "static" ? 80 : port(), domains: domain().trim() ? [domain()] : [] };
      const application = await api.post<Application>("/applications", {
        environment_id: props.placement.environmentId,
        server_id: serverId(),
        name: name(),
        config,
      });
      return api.post<Deployment>(`/applications/${application.id}/deploy`);
    },
    {
      success: "Aplicação criada. Deploy iniciado.",
      onSuccess: (deployment) => navigate(`/deployments/${deployment.id}`),
    },
  );
  const errors = create.errors;

  return (
    <form
      class="card stack"
      onSubmit={(event) => {
        event.preventDefault();
        void create.run();
      }}
    >
      <TextInput label="Nome" required placeholder="api" value={name()} onValue={setName} error={errors().name} />

      <Show
        when={props.source === "git"}
        fallback={
          <TextInput
            label="Imagem"
            required
            mono
            placeholder="ghcr.io/empresa/app:1.4.2"
            hint="Qualquer registry público. Fixe a tag para deploys reproduzíveis."
            value={image()}
            onValue={setImage}
            error={errors()["source.image"]}
          />
        }
      >
        <TextInput
          label="Repositório"
          required
          mono
          placeholder="https://github.com/empresa/app"
          hint="URL https:// para repositórios públicos ou git@host:org/repo.git para privados."
          value={repository()}
          onValue={setRepository}
          error={errors()["source.repository"]}
        />
        <div class="grid-2">
          <TextInput label="Branch" required mono value={branch()} onValue={setBranch} error={errors()["source.branch"]} />
          <Select
            label="Como construir"
            hint={PACK_HINT[pack()]}
            value={pack()}
            onValue={(value) => setPack(value as BuildPack)}
            options={GIT_PACKS.map((value) => ({ value, label: BUILD_PACK_LABEL[value] }))}
          />
        </div>
        <Show when={usesSsh()}>
          <Select
            label="Chave de deploy"
            hint="Cadastre a chave pública como deploy key no repositório. Gerencie chaves em Segurança."
            placeholder="Selecione uma chave"
            value={keyId()}
            onValue={setKeyId}
            options={(keys.data() ?? []).map((key) => ({ value: key.id, label: key.name }))}
            error={errors()["source.private_key_id"]}
          />
        </Show>
      </Show>

      <div class="grid-2">
        <Show when={pack() !== "static"}>
          <NumberInput
            label="Porta da aplicação"
            hint="Porta em que o processo escuta dentro do container."
            min={1}
            max={65535}
            value={port()}
            onValue={setPort}
            error={errors()["network.port"]}
          />
        </Show>
        <TextInput
          label="Domínio"
          mono
          placeholder="https://app.exemplo.com"
          hint="Opcional. Com https://, o certificado é emitido automaticamente."
          value={domain()}
          onValue={setDomain}
          error={errors()["network.domains"]}
        />
      </div>

      <Select
        label="Servidor"
        value={serverId()}
        onValue={setServerId}
        options={props.placement.servers.map((server) => ({ value: server.id, label: server.name }))}
        error={errors().server_id}
      />

      <Show when={domain().trim() === ""}>
        <Alert tone="info">Sem domínio, a aplicação só fica acessível para outros recursos na rede interna do servidor.</Alert>
      </Show>

      <div class="form-actions">
        <Button type="submit" variant="primary" icon="rocket" pending={create.pending()}>
          Criar e implantar
        </Button>
      </div>
    </form>
  );
}
