import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { createSignal, For, Match, Show, Switch } from "solid-js";
import { Button } from "../components/Button";
import { Alert, QueryView, StatusBadge } from "../components/Feedback";
import { ConfirmDelete } from "../components/Modal";
import { CopyField, PageHeader, Section, Tabs } from "../components/Page";
import { api } from "../lib/api";
import { clusterStatus, formatRelative, kubeStatus } from "../lib/format";
import { createAction, createQuery, type Query } from "../lib/query";
import type { Cluster, ClusterOverview, KubeApplied, OperationSnapshot } from "../lib/types";
import { Explorer } from "./cluster/Explorer";
import { Trans } from "../i18n/Trans";
import { t } from "../i18n";
import { TextArea, TextInput } from "../components/Form";

const TABS = [
  { id: "overview", label: t("Visão geral") },
  { id: "resources", label: t("Recursos") },
  { id: "apply", label: t("Aplicar YAML") },
  { id: "config", label: t("Configuração") },
];

export function ClusterPage() {
  const params = useParams<{ id: string }>();
  const [search, setSearch] = useSearchParams<{ tab?: string }>();
  const tab = () => search.tab ?? "overview";

  // Typed explicitly because the polling interval reads the query itself.
  const cluster: Query<Cluster> = createQuery(
    () => params.id,
    (id) => api.get<Cluster>(`/clusters/${id}`),
    { pollMs: (): number => (cluster.data()?.status === "provisioning" ? 2500 : 10000) },
  );
  const check = createAction(() => api.post<Cluster>(`/clusters/${params.id}/check`), {
    onSuccess: (updated) => cluster.set(updated),
    success: (updated) => (updated.status === "ready" ? t("Cluster respondeu.") : t("O cluster não respondeu. Veja o detalhe abaixo.")),
  });

  return (
    <div class="page">
      <QueryView query={cluster}>
        {(data) => (
          <>
            <PageHeader
              crumbs={[{ label: t("Clusters"), href: "/clusters" }, { label: data().name }]}
              title={
                <span class="row">
                  {data().name}
                  <StatusBadge status={clusterStatus(data().status)} />
                </span>
              }
              description={`${data().source === "k3s" ? t("K3s em container") : t("Importado")} · ${data().endpoint}${data().version ? ` · ${data().version}` : ""}`}
              actions={
                <Button icon="check" pending={check.pending()} disabled={data().status === "provisioning"} onClick={() => void check.run()}>{t("Verificar conexão")}
                </Button>
              }
            />
            <Show when={data().status === "unreachable" && data().status_message}>
              <Alert tone="danger">{data().status_message}</Alert>
            </Show>
            <Show
              when={data().status !== "provisioning"}
              fallback={<Provisioning clusterId={data().id} />}
            >
              <Tabs tabs={TABS} selected={tab()} onSelect={(id) => setSearch({ tab: id })} />
              <Switch>
                <Match when={tab() === "overview"}>
                  <OverviewTab cluster={data()} />
                </Match>
                <Match when={tab() === "resources"}>
                  <Explorer clusterId={data().id} />
                </Match>
                <Match when={tab() === "apply"}>
                  <ApplyTab clusterId={data().id} />
                </Match>
                <Match when={tab() === "config"}>
                  <ConfigTab cluster={data()} onSaved={(saved) => cluster.set(saved)} />
                </Match>
              </Switch>
            </Show>
          </>
        )}
      </QueryView>
    </div>
  );
}

function Provisioning(props: { clusterId: string }) {
  const operation = createQuery(
    () => props.clusterId,
    (id) => api.get<OperationSnapshot>(`/clusters/${id}/operation`),
    { pollMs: 2000 },
  );
  return (
    <Section title={t("Criando o cluster")} description={t("O K3s está iniciando no servidor. Esta tela se atualiza sozinha.")}>
      <pre class="log" style={{ height: "auto", "max-height": "360px", margin: "0" }}>
        {(operation.data()?.lines ?? ["Iniciando…"]).join("\n")}
      </pre>
    </Section>
  );
}

function OverviewTab(props: { cluster: Cluster }) {
  const overview = createQuery(
    () => props.cluster.status === "ready" && props.cluster.id,
    (id) => api.get<ClusterOverview>(`/clusters/${id}/overview`),
    { pollMs: 10000 },
  );
  return (
    <Show
      when={props.cluster.status === "ready"}
      fallback={<Alert tone="warning">{t("O cluster não está acessível. Verifique a conexão ou o kubeconfig em Configuração.")}</Alert>}
    >
      <QueryView query={overview}>
        {(data) => (
          <div class="stack-lg">
            <div class="grid-cards" style={{ "grid-template-columns": "repeat(auto-fill, minmax(160px, 1fr))" }}>
              <Stat label={t("Nós")} value={String(data().nodes.length)} />
              <Stat label={t("Namespaces")} value={String(data().namespaces)} />
              <Stat label={t("Deployments")} value={String(data().deployments)} />
              <Stat label={t("Pods prontos")} value={`${data().pods_running}/${data().pods}`} />
            </div>
            <Section title={t("Nós")}>
              <div class="list">
                <For each={data().nodes}>
                  {(node) => (
                    <div class="list-item">
                      <div class="grow stack-xs">
                        <span class="mono truncate">{node.name}</span>
                        <span class="small muted truncate">{node.info}</span>
                      </div>
                      <StatusBadge status={kubeStatus(node)} />
                    </div>
                  )}
                </For>
              </div>
            </Section>
            <Section title={t("Avisos recentes")} description={t("Eventos do tipo Warning em todos os namespaces.")}>
              <Show when={data().events.length > 0} fallback={<p class="muted">{t("Nenhum aviso recente.")}</p>}>
                <div class="list">
                  <For each={data().events}>
                    {(event) => (
                      <div class="list-item">
                        <div class="grow stack-xs">
                          <span class="small">{event.info}</span>
                          <span class="small muted">
                            {event.namespace} · {formatRelative(event.created)}
                          </span>
                        </div>
                      </div>
                    )}
                  </For>
                </div>
              </Show>
            </Section>
          </div>
        )}
      </QueryView>
    </Show>
  );
}

function Stat(props: { label: string; value: string }) {
  return (
    <div class="card stat">
      <span class="stat-value">{props.value}</span>
      <span class="stat-label">{props.label}</span>
    </div>
  );
}

const MANIFEST_PLACEHOLDER = `apiVersion: v1
kind: ConfigMap
metadata:
  name: example
data:
  KEY: value`;

function ApplyTab(props: { clusterId: string }) {
  const [manifest, setManifest] = createSignal("");
  const [namespace, setNamespace] = createSignal("default");
  const [applied, setApplied] = createSignal<KubeApplied[]>([]);
  const apply = createAction(
    () => api.post<{ applied: KubeApplied[] }>(`/clusters/${props.clusterId}/apply`, { manifest: manifest(), namespace: namespace() }),
    { success: t("Manifesto aplicado."), onSuccess: (result) => setApplied(result.applied) },
  );
  return (
    <form
      class="card stack"
      onSubmit={(event) => {
        event.preventDefault();
        setApplied([]);
        void apply.run();
      }}
    >
      <p class="muted small">
        <Trans
          text="Cria ou atualiza os objetos do manifesto, como {command}. Aceita vários documentos separados por {separator} e qualquer tipo que o cluster conheça."
          values={{ command: <code>kubectl apply --server-side</code>, separator: <code>---</code> }}
        />
      </p>
      <TextArea label={t("Manifesto YAML")} mono rows={18} placeholder={MANIFEST_PLACEHOLDER} value={manifest()} onValue={setManifest} />
      <TextInput
        label={t("Namespace padrão")}
        mono
        hint={t("Usado pelos objetos que não informam um namespace.")}
        value={namespace()}
        onValue={setNamespace}
      />
      <div class="form-actions">
        <Button type="submit" variant="primary" pending={apply.pending()} disabled={!manifest().trim()}>{t("Aplicar")}
        </Button>
      </div>
      <Show when={applied().length > 0}>
        <Alert tone="success">
          <strong>{t("{count} objeto(s) aplicados:", { count: applied().length })}</strong>
          <For each={applied()}>
            {(item) => (
              <div class="mono small">
                {item.kind} {item.namespace ? `${item.namespace}/` : ""}
                {item.name}
              </div>
            )}
          </For>
        </Alert>
      </Show>
    </form>
  );
}

function ConfigTab(props: { cluster: Cluster; onSaved: (cluster: Cluster) => void }) {
  const navigate = useNavigate();
  const [name, setName] = createSignal(props.cluster.name);
  const [description, setDescription] = createSignal(props.cluster.description);
  const [kubeconfig, setKubeconfig] = createSignal<string | null>(null);
  const [deleting, setDeleting] = createSignal(false);
  const isK3s = props.cluster.source === "k3s";

  const save = createAction(
    () => api.patch<Cluster>(`/clusters/${props.cluster.id}`, { name: name(), description: description() }),
    { success: t("Cluster salvo."), onSuccess: props.onSaved },
  );
  const reveal = createAction(() => api.get<{ kubeconfig: string }>(`/clusters/${props.cluster.id}/kubeconfig`), {
    onSuccess: (result) => setKubeconfig(result.kubeconfig),
  });
  const remove = createAction(
    (options: { force: boolean }) => api.delete(`/clusters/${props.cluster.id}?force=${options.force ? 1 : 0}`),
    { success: t("Cluster removido."), onSuccess: () => navigate("/clusters") },
  );

  return (
    <div class="stack-lg">
      <form
        class="card stack"
        onSubmit={(event) => {
          event.preventDefault();
          void save.run();
        }}
      >
        <h2>{t("Geral")}</h2>
        <TextInput label={t("Nome")} required value={name()} onValue={setName} error={save.errors().name} />
        <TextArea label={t("Descrição")} rows={2} value={description()} onValue={setDescription} />
        <Show when={isK3s}>
          <dl class="kv">
            <dt>{t("Imagem")}</dt>
            <dd class="mono">{props.cluster.k3s.image}</dd>
            <dt>{t("Porta da API")}</dt>
            <dd>{props.cluster.k3s.api_port}</dd>
            <dt>{t("Porta HTTP (Ingress)")}</dt>
            <dd>{props.cluster.k3s.http_port}</dd>
          </dl>
        </Show>
        <div class="form-actions">
          <Button type="submit" variant="primary" pending={save.pending()}>{t("Salvar")}
          </Button>
        </div>
      </form>

      <Section
        title="Kubeconfig"
        description={t("Para usar kubectl, Helm ou qualquer ferramenta diretamente no cluster. Dá acesso total: guarde como uma senha.")}
      >
        <Show
          when={kubeconfig() !== null}
          fallback={
            <div>
              <Button icon="eye" pending={reveal.pending()} onClick={() => void reveal.run()}>{t("Mostrar kubeconfig")}
              </Button>
            </div>
          }
        >
          <CopyField label={t("Conteúdo")} value={kubeconfig() ?? ""} secret />
          <pre class="log" style={{ height: "auto", "max-height": "320px", margin: "0" }}>
            {kubeconfig()}
          </pre>
        </Show>
      </Section>

      <Section
        title={t("Zona de perigo")}
        description={
          isK3s
            ? t("Destrói o container do K3s e o volume com todos os dados do cluster.")
            : t("Remove o cluster do Pier. Nada é alterado no cluster em si.")
        }
      >
        <div>
          <Button variant="danger" icon="trash" onClick={() => setDeleting(true)}>
            {isK3s ? t("Destruir cluster") : t("Remover do Pier")}
          </Button>
        </div>
      </Section>
      <Show when={deleting()}>
        <ConfirmDelete
          title={isK3s ? t("Destruir cluster") : t("Remover cluster")}
          resourceName={props.cluster.name}
          description={
            isK3s
              ? t("Todos os workloads e dados deste cluster serão apagados. Esta ação não pode ser desfeita.")
              : t("O Pier esquece este cluster e apaga o kubeconfig guardado. Os workloads continuam rodando nele.")
          }
          allowForce={isK3s}
          pending={remove.pending()}
          onConfirm={(options) => void remove.run(options)}
          onClose={() => setDeleting(false)}
        />
      </Show>
    </div>
  );
}
