import { For, Match, Show, Switch, type Accessor, type JSX } from "solid-js";
import { errorMessage } from "../lib/api";
import type { StatusView, Tone } from "../lib/format";
import type { Query } from "../lib/query";
import { dismissToast, toasts } from "../lib/state";
import { Button, IconButton } from "./Button";
import { Icon, type IconName } from "./Icon";
import { t } from "../i18n";

/** Status as dot + text, so color is never the only carrier of meaning. */
export function StatusBadge(props: { status: StatusView }) {
  return (
    <span class="badge" data-tone={props.status.tone} data-pulse={props.status.pulse ? "true" : undefined}>
      {props.status.label}
    </span>
  );
}

export function Alert(props: { tone: Tone; children: JSX.Element }) {
  return (
    <div class="alert" data-tone={props.tone} role={props.tone === "danger" ? "alert" : "status"}>
      <Icon name={props.tone === "success" ? "check" : "alert"} />
      <div class="grow">{props.children}</div>
    </div>
  );
}

/** First-use state: says what belongs here and offers the action to start. */
export function EmptyState(props: { icon: IconName; title: string; children?: JSX.Element; action?: JSX.Element }) {
  return (
    <div class="empty">
      <span class="empty-icon">
        <Icon name={props.icon} size={36} />
      </span>
      <h3>{props.title}</h3>
      <Show when={props.children}>
        <p>{props.children}</p>
      </Show>
      {props.action}
    </div>
  );
}

export function Skeleton(props: { height?: number; lines?: number }) {
  return (
    <div class="stack-sm" aria-busy="true" aria-label={t("Carregando")}>
      <For each={Array.from({ length: props.lines ?? 1 })}>
        {() => <div class="skeleton" style={{ height: `${props.height ?? 56}px` }} />}
      </For>
    </div>
  );
}

/**
 * Renders the three states of a query: skeleton while the first load is
 * pending, a recoverable error, or the content. Stale data stays on screen
 * during background refetches.
 */
export function QueryView<T>(props: {
  query: Query<T>;
  children: (data: Accessor<T>) => JSX.Element;
  skeletonLines?: number;
}) {
  return (
    <Switch fallback={<Skeleton lines={props.skeletonLines ?? 3} />}>
      <Match when={props.query.data() !== undefined}>{props.children(() => props.query.data() as T)}</Match>
      <Match when={props.query.error()}>
        <Alert tone="danger">
          <div class="row-between">
            <span>{errorMessage(props.query.error())}</span>
            <Button small icon="refresh" onClick={() => void props.query.refetch()}>{t("Tentar novamente")}
            </Button>
          </div>
        </Alert>
      </Match>
    </Switch>
  );
}

export function Toasts() {
  return (
    <div class="toasts" aria-live="polite">
      <For each={toasts()}>
        {(toast) => (
          <div class="toast" data-tone={toast.tone} role={toast.tone === "danger" ? "alert" : "status"}>
            <span class="grow">{toast.message}</span>
            <IconButton icon="close" label={t("Dispensar")} onClick={() => dismissToast(toast.id)} />
          </div>
        )}
      </For>
    </div>
  );
}
