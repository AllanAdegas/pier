import { createMemo, createSignal, For, Show } from "solid-js";
import { Button, IconButton } from "../../components/Button";
import { Alert, EmptyState, QueryView, StatusBadge } from "../../components/Feedback";
import { LogStream } from "../../components/LogViewer";
import { ConfirmDelete, Modal } from "../../components/Modal";
import { api } from "../../lib/api";
import { formatRelative, kubeStatus } from "../../lib/format";
import { createAction, createQuery } from "../../lib/query";
import type { KubeResource, KubeRow } from "../../lib/types";
import { t } from "../../i18n";
import { LinesInput, NumberInput, Select, TextArea, TextInput } from "../../components/Form";

/** Menu groups, following the way Rancher's explorer organizes a cluster. */
const GROUPS: { label: string; kinds: string[] }[] = [
  { label: t("Workloads"), kinds: ["Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "Pod", "ReplicaSet"] },
  { label: t("Rede"), kinds: ["Service", "Ingress", "NetworkPolicy", "HorizontalPodAutoscaler"] },
  { label: t("Configuração e armazenamento"), kinds: ["ConfigMap", "Secret", "PersistentVolumeClaim", "PersistentVolume", "StorageClass"] },
  { label: t("Cluster"), kinds: ["Namespace", "Node", "Event"] },
];
const GROUPED = new Set(GROUPS.flatMap((group) => group.kinds));
const SCALABLE = new Set(["Deployment", "StatefulSet", "ReplicaSet"]);
const RESTARTABLE = new Set(["Deployment", "StatefulSet", "DaemonSet"]);

const key = (resource: KubeResource) => `${resource.group}/${resource.name}`;

type Dialog =
  | { kind: "workload" }
  | { kind: "manifest" | "logs" | "scale" | "delete"; row: KubeRow };

/**
 * One screen for every kind of object: the kinds come from the cluster's own
 * discovery, so custom resources are browsable like built-in ones.
 */
export function Explorer(props: { clusterId: string }) {
  const base = () => `/clusters/${props.clusterId}`;
  const resources = createQuery(
    () => props.clusterId,
    (id) => api.get<KubeResource[]>(`/clusters/${id}/api-resources`),
  );
  const [selectedKey, setSelectedKey] = createSignal("apps/deployments");
  const [namespace, setNamespace] = createSignal("");
  const [dialog, setDialog] = createSignal<Dialog | null>(null);

  const resource = createMemo(() => (resources.data() ?? []).find((item) => key(item) === selectedKey()));
  const options = createMemo(() => {
    const all = resources.data() ?? [];
    const byKind = new Map(all.filter((item) => GROUPED.has(item.kind)).map((item) => [item.kind, item]));
    const entries: { value: string; label: string }[] = [];
    for (const group of GROUPS) {
      for (const kind of group.kinds) {
        const found = byKind.get(kind);
        if (found) {
          entries.push({ value: key(found), label: `${group.label} › ${kind}` });
        }
      }
    }
    for (const item of all) {
      if (!GROUPED.has(item.kind)) {
        entries.push({ value: key(item), label: `${t("Outros")} › ${item.kind}${item.group ? ` (${item.group})` : ""}` });
      }
    }
    return entries;
  });
  const namespaces = createQuery(
    () => props.clusterId,
    (id) => api.get<KubeRow[]>(`/clusters/${id}/objects?resource=namespaces`),
  );
  const query = (extra: Record<string, string> = {}) => {
    const current = resource();
    const values = new URLSearchParams({ group: current?.group ?? "", resource: current?.name ?? "", ...extra });
    return values.toString();
  };
  const rows = createQuery(
    () => resource() && `${base()}/objects?${query(resource()!.namespaced && namespace() ? { namespace: namespace() } : {})}`,
    (path) => api.get<KubeRow[]>(path),
    { pollMs: 6000 },
  );

  const reference = (row: KubeRow) => ({
    group: resource()?.group ?? "", resource: resource()?.name ?? "", namespace: row.namespace, name: row.name,
  });
  const restart = createAction((row: KubeRow) => api.post(`${base()}/restart`, reference(row)), {
    success: t("Reinício gradual iniciado."),
    onSuccess: () => void rows.refetch(),
  });
  const remove = createAction(
    (row: KubeRow) => api.delete(`${base()}/object?${query({ namespace: row.namespace, name: row.name })}`),
    {
      success: t("Objeto removido."),
      onSuccess: () => {
        setDialog(null);
        void rows.refetch();
      },
    },
  );
  const selected = (kind: Dialog["kind"]) => {
    const current = dialog();
    return current?.kind === kind && "row" in current ? current.row : undefined;
  };
  const kind = () => resource()?.kind ?? "";

  return (
    <div class="stack">
      <QueryView query={resources}>
        {() => (
          <div class="row-between" style={{ "align-items": "flex-end" }}>
            <div class="row grow" style={{ "align-items": "flex-end" }}>
              <div style={{ "min-width": "280px" }}>
                <Select label={t("Tipo de recurso")} value={selectedKey()} onValue={setSelectedKey} options={options()} />
              </div>
              <Show when={resource()?.namespaced}>
                <div style={{ "min-width": "200px" }}>
                  <Select
                    label={t("Namespace")}
                    placeholder={t("Todos")}
                    value={namespace()}
                    onValue={setNamespace}
                    options={(namespaces.data() ?? []).map((item) => ({ value: item.name, label: item.name }))}
                  />
                </div>
              </Show>
            </div>
            <Button variant="primary" icon="plus" onClick={() => setDialog({ kind: "workload" })}>{t("Novo workload")}
            </Button>
          </div>
        )}
      </QueryView>

      <QueryView query={rows}>
        {(list) => (
          <Show
            when={list().length > 0}
            fallback={
              <EmptyState icon="cluster" title={t("Nenhum {name}", { name: kind() })}>
                {namespace()
                  ? t("Não há objetos deste tipo no namespace {namespace}.", { namespace: namespace() })
                  : t("Não há objetos deste tipo.")}
              </EmptyState>
            }
          >
            <div class="list">
              <For each={list()}>
                {(row) => (
                  <div class="list-item" style={{ "flex-wrap": "wrap" }}>
                    <div class="stack-xs" style={{ flex: "1 1 220px" }}>
                      <span class="mono truncate">
                        <strong>{row.name}</strong>
                      </span>
                      <span class="small muted truncate">
                        <Show when={row.namespace}>{row.namespace} · </Show>
                        {formatRelative(row.created)}
                        <Show when={row.info}> · {row.info}</Show>
                      </span>
                    </div>
                    <Show when={row.status}>
                      <StatusBadge status={kubeStatus(row)} />
                    </Show>
                    <div class="row" style={{ gap: "var(--space-1)" }}>
                      <Show when={kind() === "Pod"}>
                        <IconButton icon="lines" label={t("Logs de {name}", { name: row.name })} onClick={() => setDialog({ kind: "logs", row })} />
                      </Show>
                      <Show when={SCALABLE.has(kind())}>
                        <Button small onClick={() => setDialog({ kind: "scale", row })}>{t("Escalar")}
                        </Button>
                      </Show>
                      <Show when={RESTARTABLE.has(kind())}>
                        <IconButton icon="refresh" label={t("Reiniciar {name}", { name: row.name })} pending={restart.pending()} onClick={() => void restart.run(row)} />
                      </Show>
                      <IconButton icon="eye" label={t("Ver YAML de {name}", { name: row.name })} onClick={() => setDialog({ kind: "manifest", row })} />
                      <IconButton icon="trash" label={t("Remover {name}", { name: row.name })} onClick={() => setDialog({ kind: "delete", row })} />
                    </div>
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>

      <Show when={dialog()?.kind === "workload"}>
        <WorkloadForm
          base={base()}
          namespace={namespace()}
          onClose={() => setDialog(null)}
          onCreated={() => {
            setDialog(null);
            setSelectedKey("apps/deployments");
            void rows.refetch();
            void namespaces.refetch();
          }}
        />
      </Show>
      <Show when={selected("manifest")}>
        {(row) => (
          <ManifestModal
            base={base()}
            title={`${kind()} ${row().name}`}
            objectQuery={query({ namespace: row().namespace, name: row().name })}
            namespace={row().namespace}
            onClose={() => setDialog(null)}
            onApplied={() => void rows.refetch()}
          />
        )}
      </Show>
      <Show when={selected("logs")}>
        {(row) => (
          <Modal title={t("Logs — {name}", { name: row().name })} onClose={() => setDialog(null)} wide>
            <LogStream path={`${base()}/logs?${new URLSearchParams({ namespace: row().namespace, pod: row().name, tail: "300" })}`} />
          </Modal>
        )}
      </Show>
      <Show when={selected("scale")}>
        {(row) => (
          <ScaleModal
            base={base()}
            reference={reference(row())}
            onClose={() => setDialog(null)}
            onScaled={() => {
              setDialog(null);
              void rows.refetch();
            }}
          />
        )}
      </Show>
      <Show when={selected("delete")}>
        {(row) => (
          <ConfirmDelete
            title={t("Remover {name}", { name: kind() })}
            resourceName={row().name}
            description={t("O objeto é removido do cluster, junto com o que depende dele (os pods de um Deployment, por exemplo).")}
            pending={remove.pending()}
            onConfirm={() => void remove.run(row())}
            onClose={() => setDialog(null)}
          />
        )}
      </Show>
    </div>
  );
}

function ManifestModal(props: {
  base: string;
  title: string;
  objectQuery: string;
  namespace: string;
  onClose: () => void;
  onApplied: () => void;
}) {
  const manifest = createQuery(
    () => props.objectQuery,
    (query) => api.get<{ manifest: string }>(`${props.base}/object?${query}`),
  );
  const [edited, setEdited] = createSignal<string | null>(null);
  const apply = createAction(
    () => api.post(`${props.base}/apply`, { manifest: edited() ?? "", namespace: props.namespace }),
    {
      success: t("Alterações aplicadas."),
      onSuccess: () => {
        props.onApplied();
        props.onClose();
      },
    },
  );
  return (
    <Modal title={props.title} onClose={props.onClose} wide>
      <QueryView query={manifest}>
        {(data) => (
          <form
            class="stack"
            onSubmit={(event) => {
              event.preventDefault();
              void apply.run();
            }}
          >
            <TextArea
              label={t("Manifesto")}
              mono
              rows={20}
              hint={t("Edite e aplique para alterar o objeto. Campos de status são ignorados pelo cluster.")}
              value={edited() ?? data().manifest}
              onValue={setEdited}
            />
            <div class="form-actions">
              <Button onClick={props.onClose}>{t("Fechar")}</Button>
              <Button type="submit" variant="primary" pending={apply.pending()} disabled={edited() === null}>{t("Aplicar alterações")}
              </Button>
            </div>
          </form>
        )}
      </QueryView>
    </Modal>
  );
}

function ScaleModal(props: {
  base: string;
  reference: { group: string; resource: string; namespace: string; name: string };
  onClose: () => void;
  onScaled: () => void;
}) {
  const [replicas, setReplicas] = createSignal(1);
  const scale = createAction(() => api.post(`${props.base}/scale`, { ...props.reference, replicas: replicas() }), {
    success: t("Escala alterada."),
    onSuccess: props.onScaled,
  });
  return (
    <Modal title={t("Escalar {name}", { name: props.reference.name })} onClose={props.onClose}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void scale.run();
        }}
      >
        <NumberInput label={t("Réplicas")} hint={t("Zero para o workload sem removê-lo.")} min={0} max={1000} value={replicas()} onValue={setReplicas} />
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" pending={scale.pending()}>{t("Escalar")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function WorkloadForm(props: { base: string; namespace: string; onClose: () => void; onCreated: () => void }) {
  const [name, setName] = createSignal("");
  const [namespace, setNamespace] = createSignal(props.namespace || "default");
  const [image, setImage] = createSignal("");
  const [replicas, setReplicas] = createSignal(1);
  const [port, setPort] = createSignal(0);
  const [host, setHost] = createSignal("");
  const [env, setEnv] = createSignal<string[]>([]);
  const create = createAction(
    () => {
      const variables: Record<string, string> = {};
      for (const line of env()) {
        const separator = line.indexOf("=");
        if (line.trim() !== "" && separator > 0) {
          variables[line.slice(0, separator).trim()] = line.slice(separator + 1);
        }
      }
      return api.post(`${props.base}/workloads`, {
        name: name(), namespace: namespace(), image: image(), replicas: replicas(), port: port(), host: host(), env: variables,
      });
    },
    { success: t("Workload criado."), onSuccess: props.onCreated },
  );
  const errors = create.errors;
  return (
    <Modal title={t("Novo workload")} onClose={props.onClose} wide>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void create.run();
        }}
      >
        <Alert tone="info">{t("Gera um Deployment e, conforme o preenchimento, um Service e um Ingress. Para qualquer coisa além disso, use “Aplicar YAML”.")}
        </Alert>
        <div class="grid-2">
          <TextInput label={t("Nome")} required mono placeholder="api" value={name()} onValue={setName} error={errors().name} />
          <TextInput label={t("Namespace")} mono hint={t("Criado se não existir.")} value={namespace()} onValue={setNamespace} error={errors().namespace} />
        </div>
        <TextInput label={t("Imagem")} required mono placeholder={t("ghcr.io/empresa/api:1.4.2")} value={image()} onValue={setImage} error={errors().image} />
        <div class="grid-2">
          <NumberInput label={t("Réplicas")} min={1} max={100} value={replicas()} onValue={setReplicas} error={errors().replicas} />
          <NumberInput
            label={t("Porta do container")}
            placeholder={t("sem Service")}
            hint={t("Informe para criar um Service.")}
            min={0}
            max={65535}
            value={port()}
            onValue={setPort}
            error={errors().port}
          />
        </div>
        <TextInput
          label={t("Host público")}
          mono
          placeholder={t("api.exemplo.com")}
          hint={t("Opcional. Cria um Ingress para este hostname.")}
          value={host()}
          onValue={setHost}
          error={errors().host}
        />
        <LinesInput label={t("Variáveis de ambiente")} hint={t("CHAVE=valor, uma por linha.")} value={env()} onValue={setEnv} error={errors().env} />
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="primary" icon="rocket" pending={create.pending()}>{t("Criar workload")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
