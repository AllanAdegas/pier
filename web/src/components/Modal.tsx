import { createSignal, createUniqueId, onCleanup, onMount, Show, type JSX } from "solid-js";
import { Portal } from "solid-js/web";
import { Button, IconButton } from "./Button";
import { Checkbox, TextInput } from "./Form";
import { t } from "../i18n";
import { api } from "../lib/api";
import { createQuery } from "../lib/query";
import type { Project } from "../lib/types";

/**
 * Dialog rendered above the page. It closes with the X button, the Escape
 * key or a click on the backdrop, and returns focus to where it was.
 */
export function Modal(props: { title: string; onClose: () => void; wide?: boolean; children: JSX.Element }) {
  const titleId = createUniqueId();
  let dialog: HTMLDivElement | undefined;
  const previouslyFocused = document.activeElement as HTMLElement | null;

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      props.onClose();
    }
  };
  onMount(() => {
    document.addEventListener("keydown", onKeyDown);
    const firstControl = dialog?.querySelector<HTMLElement>(".modal-body :is(input, select, textarea, button)");
    (firstControl ?? dialog)?.focus();
  });
  onCleanup(() => {
    document.removeEventListener("keydown", onKeyDown);
    previouslyFocused?.focus();
  });

  return (
    <Portal>
      <div
        class="modal-backdrop"
        onMouseDown={(event) => {
          if (event.target === event.currentTarget) {
            props.onClose();
          }
        }}
      >
        <div
          ref={dialog}
          class={`modal ${props.wide ? "modal-wide" : ""}`}
          role="dialog"
          aria-modal="true"
          aria-labelledby={titleId}
          tabindex="-1"
        >
          <div class="modal-header">
            <h2 id={titleId}>{props.title}</h2>
            <IconButton icon="close" label={t("Fechar")} onClick={props.onClose} />
          </div>
          <div class="modal-body">{props.children}</div>
        </div>
      </div>
    </Portal>
  );
}

export interface DeleteOptions {
  volumes: boolean;
  force: boolean;
  /** Name of a protected environment, typed by the operator. */
  confirmation: string;
}

/** Query string of a DELETE request built from the confirmation dialog. */
export function deleteQuery(options: DeleteOptions): string {
  const params = new URLSearchParams({ volumes: options.volumes ? "1" : "0", force: options.force ? "1" : "0" });
  if (options.confirmation) {
    params.set("confirmation", options.confirmation);
  }
  return params.toString();
}

/**
 * Confirmation for destructive, irreversible actions. Typing the resource
 * name prevents deleting the wrong thing by reflex.
 */
export function ConfirmDelete(props: {
  title: string;
  resourceName: string;
  description: JSX.Element;
  /** Shows the "also delete data" checkbox with this label. */
  volumesLabel?: string;
  /** Shows the "force" checkbox for when the server is unreachable. */
  allowForce?: boolean;
  /** Environment of the resource: a protected one also asks for its name. */
  environmentId?: string;
  pending: boolean;
  onConfirm: (options: DeleteOptions) => void;
  onClose: () => void;
}) {
  const [typed, setTyped] = createSignal("");
  const [typedEnvironment, setTypedEnvironment] = createSignal("");
  const [volumes, setVolumes] = createSignal(false);
  const [force, setForce] = createSignal(false);
  const projects = createQuery(() => props.environmentId, () => api.get<Project[]>("/projects"));
  const protectedEnvironment = () =>
    projects.data()?.flatMap((project) => project.environments).find((environment) =>
      environment.id === props.environmentId && environment.protected
    )?.name;
  const matches = () =>
    typed().trim() === props.resourceName &&
    (!protectedEnvironment() || typedEnvironment().trim() === protectedEnvironment());

  return (
    <Modal title={props.title} onClose={props.onClose}>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          if (matches()) {
            props.onConfirm({ volumes: volumes(), force: force(), confirmation: protectedEnvironment() ? typedEnvironment().trim() : "" });
          }
        }}
      >
        <p>{props.description}</p>
        <Show when={props.volumesLabel}>
          <Checkbox
            label={props.volumesLabel!}
            hint={t("Os dados não poderão ser recuperados.")}
            checked={volumes()}
            onChange={setVolumes}
          />
        </Show>
        <Show when={props.allowForce}>
          <Checkbox
            label={t("Forçar remoção")}
            hint={t("Apaga o registro mesmo que o servidor esteja inacessível. Containers podem continuar rodando lá.")}
            checked={force()}
            onChange={setForce}
          />
        </Show>
        <TextInput
          label={t("Digite \"{name}\" para confirmar", { name: props.resourceName })}
          value={typed()}
          onValue={setTyped}
          autocomplete="off"
        />
        <Show when={protectedEnvironment()}>
          {(environment) => (
            <TextInput
              label={t("O ambiente \"{name}\" é protegido. Digite o nome dele para confirmar", { name: environment() })}
              value={typedEnvironment()}
              onValue={setTypedEnvironment}
              autocomplete="off"
            />
          )}
        </Show>
        <div class="form-actions">
          <Button onClick={props.onClose}>{t("Cancelar")}</Button>
          <Button type="submit" variant="danger" icon="trash" disabled={!matches()} pending={props.pending}>{t("Remover definitivamente")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
