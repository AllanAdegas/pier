import { A } from "@solidjs/router";
import { createSignal, For, Show, type JSX } from "solid-js";
import { notify } from "../lib/state";
import { IconButton } from "./Button";
import { Icon } from "./Icon";

export interface Crumb {
  label: string;
  href?: string;
}

/** Title block of a page: where you are, what it is and the primary action. */
export function PageHeader(props: {
  title: JSX.Element;
  description?: JSX.Element;
  crumbs?: Crumb[];
  actions?: JSX.Element;
}) {
  return (
    <header class="stack-sm">
      <Show when={props.crumbs?.length}>
        <nav class="breadcrumbs" aria-label="Você está em">
          <For each={props.crumbs}>
            {(crumb, index) => (
              <>
                <Show when={index() > 0}>
                  <Icon name="chevron" size={14} />
                </Show>
                <Show when={crumb.href} fallback={<span>{crumb.label}</span>}>
                  <A href={crumb.href!}>{crumb.label}</A>
                </Show>
              </>
            )}
          </For>
        </nav>
      </Show>
      <div class="page-header">
        <div class="grow">
          <h1>{props.title}</h1>
          <Show when={props.description}>
            <p>{props.description}</p>
          </Show>
        </div>
        <Show when={props.actions}>
          <div class="row">{props.actions}</div>
        </Show>
      </div>
    </header>
  );
}

export interface TabItem {
  id: string;
  label: string;
}

/** Tab strip; the caller renders the panel for the selected id. */
export function Tabs(props: { tabs: TabItem[]; selected: string; onSelect: (id: string) => void }) {
  return (
    <div class="tabs" role="tablist">
      <For each={props.tabs}>
        {(tab) => (
          <button
            type="button"
            class="tab"
            role="tab"
            aria-selected={props.selected === tab.id ? "true" : "false"}
            onClick={() => props.onSelect(tab.id)}
          >
            {tab.label}
          </button>
        )}
      </For>
    </div>
  );
}

export function Section(props: { title: string; description?: string; actions?: JSX.Element; children: JSX.Element }) {
  return (
    <section class="card stack">
      <div class="row-between">
        <div class="grow">
          <h2>{props.title}</h2>
          <Show when={props.description}>
            <p class="muted small">{props.description}</p>
          </Show>
        </div>
        {props.actions}
      </div>
      {props.children}
    </section>
  );
}

/** Read-only value with a copy button; secrets start hidden. */
export function CopyField(props: { label: string; value: string; secret?: boolean; hint?: string }) {
  const [revealed, setRevealed] = createSignal(!props.secret);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(props.value);
      notify("Copiado para a área de transferência.");
    } catch {
      notify("Não foi possível copiar. Selecione o texto manualmente.", "danger");
    }
  };
  return (
    <div class="field">
      <span class="field-label">{props.label}</span>
      <div class="copy-field">
        <input
          class="input"
          readOnly
          aria-label={props.label}
          type={revealed() ? "text" : "password"}
          value={props.value}
          onFocus={(event) => event.currentTarget.select()}
        />
        <Show when={props.secret}>
          <IconButton
            icon={revealed() ? "eye-off" : "eye"}
            label={revealed() ? "Ocultar" : "Mostrar"}
            onClick={() => setRevealed(!revealed())}
          />
        </Show>
        <IconButton icon="copy" label="Copiar" onClick={() => void copy()} />
      </div>
      <Show when={props.hint}>
        <span class="field-hint">{props.hint}</span>
      </Show>
    </div>
  );
}
