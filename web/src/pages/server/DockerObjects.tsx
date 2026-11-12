import { createMemo, createSignal, For, Show } from "solid-js";
import { Button, IconButton } from "../../components/Button";
import { QueryView } from "../../components/Feedback";
import { ConfirmDelete, Modal } from "../../components/Modal";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import type { DockerImage, DockerNetwork, DockerVolume } from "../../lib/types";
import { Trans } from "../../i18n/Trans";
import { t } from "../../i18n";
import { TextInput } from "../../components/Form";

function reference(image: DockerImage): string {
  return image.repository === "<none>" || image.tag === "<none>" ? image.id : `${image.repository}:${image.tag}`;
}

export function Images(props: { base: string }) {
  const images = createQuery(
    () => props.base,
    (base) => api.get<DockerImage[]>(`${base}/images`),
  );
  const [filter, setFilter] = createSignal("");
  const [pulling, setPulling] = createSignal(false);
  const [image, setImage] = createSignal("");
  const visible = createMemo(() => {
    const term = filter().trim().toLowerCase();
    return (images.data() ?? []).filter((item) => term === "" || reference(item).toLowerCase().includes(term));
  });
  const pull = createAction(() => api.post<{ result: string }>(`${props.base}/images/pull`, { image: image() }), {
    success: (result) => result.result || t("Imagem baixada."),
    onSuccess: () => {
      setPulling(false);
      setImage("");
      void images.refetch();
    },
  });
  const [removing, setRemoving] = createSignal<DockerImage | null>(null);
  const remove = createAction((item: DockerImage) => api.delete(`${props.base}/images?ref=${encodeURIComponent(reference(item))}`), {
    success: t("Imagem removida."),
    onSuccess: () => {
      setRemoving(null);
      void images.refetch();
    },
  });
  const prune = createAction(() => api.post<{ report: string }>(`${props.base}/images/prune`), {
    success: (result) => result.report.split("\n").pop() || t("Limpeza concluída."),
    onSuccess: () => void images.refetch(),
  });

  return (
    <div class="stack">
      <div class="row-between">
        <div class="grow" style={{ "max-width": "360px" }}>
          <TextInput label={t("Filtrar")} type="search" placeholder={t("repositório ou tag")} value={filter()} onValue={setFilter} />
        </div>
        <div class="row">
          <Button icon="trash" pending={prune.pending()} onClick={() => void prune.run()}>{t("Remover imagens sem tag")}
          </Button>
          <Button variant="primary" icon="download" onClick={() => setPulling(true)}>{t("Baixar imagem")}
          </Button>
        </div>
      </div>
      <QueryView query={images}>
        {() => (
          <div class="table-wrap">
            <table class="table">
              <thead>
                <tr>
                  <th>{t("Imagem")}</th>
                  <th>{t("Tamanho")}</th>
                  <th>{t("Criada")}</th>
                  <th>
                    <span class="visually-hidden">{t("Ações")}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <For each={visible()}>
                  {(item) => (
                    <tr>
                      <td class="mono">{reference(item)}</td>
                      <td>{item.size}</td>
                      <td>{item.created}</td>
                      <td style={{ "text-align": "right" }}>
                        <IconButton icon="trash" label={t("Remover {name}", { name: reference(item) })} onClick={() => setRemoving(() => item)} />
                      </td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
          </div>
        )}
      </QueryView>
      <p class="muted small">{t("O Docker recusa a remoção de imagens em uso por algum container.")}</p>
      <Show when={removing()}>
        {(item) => (
          <Modal title={t("Remover imagem")} onClose={() => setRemoving(null)}>
            <div class="stack">
              <p>
                <Trans
                  text="A imagem {image} será apagada deste servidor. Uma imagem construída aqui não pode ser baixada de novo."
                  values={{ image: <span class="mono">{reference(item())}</span> }}
                />
              </p>
              <div class="form-actions">
                <Button onClick={() => setRemoving(null)}>{t("Cancelar")}</Button>
                <Button variant="danger" icon="trash" pending={remove.pending()} onClick={() => void remove.run(item())}>{t("Remover imagem")}
                </Button>
              </div>
            </div>
          </Modal>
        )}
      </Show>
      <Show when={pulling()}>
        <Modal title={t("Baixar imagem")} onClose={() => setPulling(false)}>
          <form
            class="stack"
            onSubmit={(event) => {
              event.preventDefault();
              void pull.run();
            }}
          >
            <TextInput label={t("Imagem")} required mono placeholder="postgres:17-alpine" value={image()} onValue={setImage} error={pull.errors().image} />
            <div class="form-actions">
              <Button onClick={() => setPulling(false)}>{t("Cancelar")}</Button>
              <Button type="submit" variant="primary" icon="download" pending={pull.pending()}>{t("Baixar")}
              </Button>
            </div>
          </form>
        </Modal>
      </Show>
    </div>
  );
}

/** Name + create + list + remove, shared by volumes and networks. */
function NamedObjects<T extends { name: string }>(props: {
  base: string;
  collection: "volumes" | "networks";
  labels: { create: string; created: string; removed: string; removeTitle: string; removeDescription: string };
  detail: (item: T) => string;
  locked?: (item: T) => boolean;
}) {
  const items = createQuery(
    () => `${props.base}/${props.collection}`,
    (path) => api.get<T[]>(path),
  );
  const [name, setName] = createSignal("");
  const [removing, setRemoving] = createSignal<T | null>(null);
  const create = createAction(() => api.post(`${props.base}/${props.collection}`, { name: name() }), {
    success: props.labels.created,
    onSuccess: () => {
      setName("");
      void items.refetch();
    },
  });
  const remove = createAction((item: T) => api.delete(`${props.base}/${props.collection}/${encodeURIComponent(item.name)}`), {
    success: props.labels.removed,
    onSuccess: () => {
      setRemoving(null);
      void items.refetch();
    },
  });

  return (
    <div class="stack">
      <form
        class="row"
        style={{ "align-items": "flex-end" }}
        onSubmit={(event) => {
          event.preventDefault();
          void create.run();
        }}
      >
        <div class="grow" style={{ "max-width": "360px" }}>
          <TextInput label={props.labels.create} mono value={name()} onValue={setName} error={create.errors().name} />
        </div>
        <Button type="submit" icon="plus" pending={create.pending()} disabled={!name().trim()}>{t("Criar")}
        </Button>
      </form>
      <QueryView query={items}>
        {(list) => (
          <div class="list">
            <For each={list()}>
              {(item) => (
                <div class="list-item">
                  <div class="grow stack-sm" style={{ gap: "0" }}>
                    <span class="mono truncate">{item.name}</span>
                    <span class="small muted">{props.detail(item)}</span>
                  </div>
                  <Show
                    when={!props.locked?.(item)}
                    fallback={<span class="tag">{t("reservada")}</span>}
                  >
                    <IconButton icon="trash" label={t("Remover {name}", { name: item.name })} onClick={() => setRemoving(() => item)} />
                  </Show>
                </div>
              )}
            </For>
          </div>
        )}
      </QueryView>
      <Show when={removing()}>
        {(item) => (
          <ConfirmDelete
            title={props.labels.removeTitle}
            resourceName={item().name}
            description={props.labels.removeDescription}
            pending={remove.pending()}
            onConfirm={() => void remove.run(item())}
            onClose={() => setRemoving(null)}
          />
        )}
      </Show>
    </div>
  );
}

export function Volumes(props: { base: string }) {
  return (
    <NamedObjects<DockerVolume>
      base={props.base}
      collection="volumes"
      labels={{
        create: t("Novo volume"),
        created: t("Volume criado."),
        removed: t("Volume removido."),
        removeTitle: t("Remover volume"),
        removeDescription: t(
          "Os dados do volume são apagados e não podem ser recuperados. O Docker recusa se algum container ainda o usa.",
        ),
      }}
      detail={(volume) => `driver ${volume.driver}`}
    />
  );
}

export function Networks(props: { base: string }) {
  return (
    <NamedObjects<DockerNetwork>
      base={props.base}
      collection="networks"
      labels={{
        create: t("Nova rede (bridge)"),
        created: t("Rede criada."),
        removed: t("Rede removida."),
        removeTitle: t("Remover rede"),
        removeDescription: t("A rede é removida. O Docker recusa se algum container ainda está conectado a ela."),
      }}
      detail={(network) => `${network.driver} · ${network.scope}`}
      locked={(network) => network.reserved}
    />
  );
}
