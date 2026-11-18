import { A, useNavigate } from "@solidjs/router";
import { createSignal, For, Show } from "solid-js";
import { Button } from "../components/Button";
import { Alert, EmptyState, QueryView, StatusBadge } from "../components/Feedback";
import { Icon } from "../components/Icon";
import { Modal } from "../components/Modal";
import { PageHeader, Tabs } from "../components/Page";
import { api } from "../lib/api";
import { clusterStatus } from "../lib/format";
import { createAction, createQuery } from "../lib/query";
import type { Cluster, Server } from "../lib/types";
import { t } from "../i18n";
import { NumberInput, Select, TextArea, TextInput } from "../components/Form";

/** Servers and clusters are the two halves of "where things run". */
export function InfrastructureTabs(props: { current: "servers" | "clusters" | "swarm" | "monitoring" }) {
  const navigate = useNavigate();
  return (
    <Tabs
      tabs={[
        { id: "servers", label: t("Servidores Docker") },
        { id: "clusters", label: t("Clusters Kubernetes") },
        { id: "swarm", label: "Docker Swarm" },
        { id: "monitoring", label: t("Monitoramento") },
      ]}
      selected={props.current}
      onSelect={(id) => navigate(`/${id}`)}
    />
  );
}

export function ClustersPage() {
  const navigate = useNavigate();
  const clusters = createQuery(
    () => true,
    () => api.get<Cluster[]>("/clusters"),
    { pollMs: 8000 },
  );
  const [adding, setAdding] = createSignal(false);

  return (
    <div class="page">
      <PageHeader
        title={t("Infraestrutura")}
        description={t("Kubernetes é opcional: use quando um servidor Docker não for suficiente.")}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setAdding(true)}>{t("Adicionar cluster")}
          </Button>
        }
      />
      <InfrastructureTabs current="clusters" />
      <QueryView query={clusters}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={
              <EmptyState
                icon="cluster"
                title={t("Nenhum cluster")}
                action={
                  <Button variant="primary" icon="plus" onClick={() => setAdding(true)}>{t("Adicionar cluster")}
                  </Button>
                }
              >{t("Importe um cluster existente pelo kubeconfig ou crie um K3s de nó único em um dos seus servidores. Tudo o mais no Pier funciona sem Kubernetes.")}
              </EmptyState>
            }
          >
            <div class="list">
              <For each={list()}>
                {(cluster) => (
                  <A href={`/clusters/${cluster.id}`} class="list-item">
                    <span class="muted">
                      <Icon name="cluster" size={20} />
                    </span>
                    <div class="grow stack-sm" style={{ gap: "0" }}>
                      <strong class="truncate">{cluster.name}</strong>
                      <span class="small muted truncate">
                        {cluster.source === "k3s" ? t("K3s em container") : t("Importado")} · {cluster.endpoint}
                        <Show when={cluster.version}> · {cluster.version}</Show>
                        <Show when={cluster.nodes > 0}> · {t("{count} nó(s)", { count: cluster.nodes })}</Show>
                      </span>
                    </div>
                    <StatusBadge status={clusterStatus(cluster.status)} />
                    <span class="muted">
                      <Icon name="chevron" size={16} />
                    </span>
                  </A>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>
      <Show when={adding()}>
        <AddClusterModal onClose={() => setAdding(false)} onCreated={(cluster) => navigate(`/clusters/${cluster.id}`)} />
      </Show>
    </div>
  );
}

function AddClusterModal(props: { onClose: () => void; onCreated: (cluster: Cluster) => void }) {
  const [mode, setMode] = createSignal<"import" | "k3s">("import");
  return (
    <Modal title={t("Adicionar cluster")} onClose={props.onClose} wide>
      <div class="stack">
        <div class="choice-grid">
          <button type="button" class="choice" aria-pressed={mode() === "import" ? "true" : "false"} onClick={() => setMode("import")}>
            <span class="choice-title">{t("Importar cluster existente")}</span>
            <span class="choice-text">{t("Qualquer Kubernetes, pelo kubeconfig. O Pier não instala nada nele.")}</span>
          </button>
          <button type="button" class="choice" aria-pressed={mode() === "k3s" ? "true" : "false"} onClick={() => setMode("k3s")}>
            <span class="choice-title">{t("Criar K3s em um servidor")}</span>
            <span class="choice-text">{t("Cluster de nó único rodando como container. Pronto em menos de um minuto.")}</span>
          </button>
        </div>
        <Show when={mode() === "import"} fallback={<K3sForm onClose={props.onClose} onCreated={props.onCreated} />}>
          <ImportForm onClose={props.onClose} onCreated={props.onCreated} />
        </Show>
      </div>
    </Modal>
  );
}

function ImportForm(props: { onClose: () => void; onCreated: (cluster: Cluster) => void }) {
  const [name, setName] = createSignal("");
  const [kubeconfig, setKubeconfig] = createSignal("");
  const [context, setContext] = createSignal("");
  const save = createAction(
    () => api.post<Cluster>("/clusters", { name: name(), kubeconfig: kubeconfig(), context: context() }),
    { success: t("Cluster importado."), onSuccess: props.onCreated },
  );
  return (
    <form
      class="stack"
      onSubmit={(event) => {
        event.preventDefault();
        void save.run();
      }}
    >
      <TextInput label={t("Nome")} required placeholder={t("producao")} value={name()} onValue={setName} error={save.errors().name} />
      <TextArea
        label="Kubeconfig"
        mono
        rows={10}
        placeholder="apiVersion: v1&#10;kind: Config&#10;…"
        hint={t("Precisa trazer as credenciais embutidas (certificado ou token). Gere com: kubectl config view --flatten --minify")}
        value={kubeconfig()}
        onValue={setKubeconfig}
        error={save.errors().kubeconfig}
      />
      <TextInput
        label={t("Contexto")}
        mono
        placeholder={t("contexto atual do arquivo")}
        hint={t("Opcional. Só as credenciais do contexto escolhido são guardadas.")}
        value={context()}
        onValue={setContext}
      />
      <Alert tone="info">{t("Kubeconfigs de GKE, EKS e AKS autenticam por um comando local e não funcionam aqui. Nesses casos, crie uma ServiceAccount com token no cluster e monte um kubeconfig com ele.")}
      </Alert>
      <div class="form-actions">
        <Button onClick={props.onClose}>{t("Cancelar")}</Button>
        <Button type="submit" variant="primary" pending={save.pending()}>{t("Importar cluster")}
        </Button>
      </div>
    </form>
  );
}

function K3sForm(props: { onClose: () => void; onCreated: (cluster: Cluster) => void }) {
  const servers = createQuery(
    () => true,
    () => api.get<Server[]>("/servers"),
  );
  const [name, setName] = createSignal("");
  const [serverId, setServerId] = createSignal("");
  const [apiPort, setApiPort] = createSignal(6443);
  const [httpPort, setHttpPort] = createSignal(8080);
  const chosenServer = () => serverId() || servers.data()?.[0]?.id || "";
  const save = createAction(
    () => api.post<Cluster>("/clusters/k3s", { name: name(), server_id: chosenServer(), api_port: apiPort(), http_port: httpPort() }),
    { success: t("Criação do cluster iniciada."), onSuccess: props.onCreated },
  );
  return (
    <form
      class="stack"
      onSubmit={(event) => {
        event.preventDefault();
        void save.run();
      }}
    >
      <TextInput label={t("Nome")} required placeholder="k3s-dev" value={name()} onValue={setName} error={save.errors().name} />
      <Select
        label={t("Servidor")}
        value={chosenServer()}
        onValue={setServerId}
        options={(servers.data() ?? []).map((server) => ({ value: server.id, label: server.name }))}
        error={save.errors().server_id}
      />
      <div class="grid-2">
        <NumberInput
          label={t("Porta da API")}
          hint={t("Onde a API do Kubernetes fica acessível no servidor.")}
          min={1024}
          max={65535}
          value={apiPort()}
          onValue={setApiPort}
          error={save.errors().api_port}
        />
        <NumberInput
          label={t("Porta HTTP")}
          hint={t("Onde os Ingress do cluster respondem.")}
          min={1024}
          max={65535}
          value={httpPort()}
          onValue={setHttpPort}
          error={save.errors().http_port}
        />
      </div>
      <Alert tone="warning">{t("O nó roda como container privilegiado e as duas portas ficam abertas em todas as interfaces do servidor. Indicado para desenvolvimento e cargas pequenas; para produção com alta disponibilidade, importe um cluster de verdade.")}
      </Alert>
      <div class="form-actions">
        <Button onClick={props.onClose}>{t("Cancelar")}</Button>
        <Button type="submit" variant="primary" pending={save.pending()}>{t("Criar cluster")}
        </Button>
      </div>
    </form>
  );
}
