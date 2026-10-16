import { createEffect, createMemo, createSignal, For, on, Show } from "solid-js";
import { api } from "../lib/api";
import { createQuery } from "../lib/query";
import type { FolderListing } from "../lib/types";
import { Button } from "./Button";
import { Alert, QueryView } from "./Feedback";
import { Checkbox, TextInput } from "./Form";
import { Icon } from "./Icon";

/**
 * Browses the folders of a server to choose where a compose project lives.
 * It never shows file contents: only subfolders, the compose files of the
 * current folder and whether it has a .env.
 */
export function FolderPicker(props: { serverId: string; onChange: (listing: FolderListing | null) => void }) {
  // Empty path asks the server for the home of the user Pier connects as.
  const [path, setPath] = createSignal("");
  const [typed, setTyped] = createSignal("");
  const [showHidden, setShowHidden] = createSignal(false);
  // A string key: the query refetches when the value changes, not the object.
  const listing = createQuery(
    () => `/servers/${props.serverId}/files?path=${encodeURIComponent(path())}`,
    (url) => api.get<FolderListing>(url),
  );

  // Another server has another disk: start again from its home.
  createEffect(on(() => props.serverId, () => setPath(""), { defer: true }));
  createEffect(() => {
    const current = listing.data();
    if (current) {
      setTyped(current.path);
    }
    props.onChange(listing.error() ? null : (current ?? null));
  });

  const folders = createMemo(() =>
    (listing.data()?.directories ?? []).filter((name) => showHidden() || !name.startsWith(".")),
  );
  const open = (name: string) => {
    const base = listing.data()?.path ?? "";
    setPath(base === "/" ? `/${name}` : `${base}/${name}`);
  };

  return (
    <div class="stack">
      <form
        class="row"
        style={{ "align-items": "flex-end" }}
        onSubmit={(event) => {
          event.preventDefault();
          // The server validates the path; a bad one shows its error below.
          setPath(typed().trim());
        }}
      >
        <div class="grow">
          <TextInput label="Pasta no servidor" mono placeholder="/home/deploy/meu-projeto" value={typed()} onValue={setTyped} />
        </div>
        <Button type="submit">Abrir</Button>
      </form>

      <QueryView query={listing}>
        {(current) => (
          <div class="stack-sm">
            <div class="row-between">
              <Button small disabled={current().parent === ""} onClick={() => setPath(current().parent)}>
                ↑ Subir um nível
              </Button>
              <Checkbox label="Mostrar pastas ocultas" checked={showHidden()} onChange={setShowHidden} />
            </div>
            <Show
              when={folders().length > 0}
              fallback={<p class="muted small">Esta pasta não tem subpastas{showHidden() ? "" : " visíveis"}.</p>}
            >
              <div class="list folder-list">
                <For each={folders()}>
                  {(name) => (
                    <button type="button" class="list-item folder-item" onClick={() => open(name)}>
                      <Icon name="folder" size={16} />
                      <span class="mono truncate grow">{name}</span>
                    </button>
                  )}
                </For>
              </div>
            </Show>
            <Show when={current().truncated}>
              <Alert tone="warning">A pasta tem itens demais para listar todos; digite o caminho da subpasta.</Alert>
            </Show>
          </div>
        )}
      </QueryView>
    </div>
  );
}
