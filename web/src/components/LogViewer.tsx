import { createEffect, createMemo, createSignal, For, on, onCleanup, Show } from "solid-js";
import { openStream } from "../lib/api";
import type { Container } from "../lib/types";
import { Checkbox, Select } from "./Form";
import { EmptyState } from "./Feedback";

export interface LogLine {
  id: number;
  level: string;
  line: string;
}

const MAX_LINES = 5000;

/** Scrollable log that follows the tail unless the user turns that off. */
export function LogViewer(props: { lines: LogLine[]; emptyText?: string }) {
  const [follow, setFollow] = createSignal(true);
  let box: HTMLDivElement | undefined;

  createEffect(
    on(
      () => props.lines.length,
      () => {
        if (follow() && box) {
          box.scrollTop = box.scrollHeight;
        }
      },
    ),
  );

  return (
    <div class="stack-sm">
      <div class="row-between">
        <span class="muted small">{props.lines.length} linha(s)</span>
        <Checkbox label="Acompanhar o final" checked={follow()} onChange={setFollow} />
      </div>
      <div class="log" ref={box} role="log" aria-live="off" tabindex="0">
        <Show when={props.lines.length > 0} fallback={<span class="muted">{props.emptyText ?? "Sem linhas ainda."}</span>}>
          <For each={props.lines}>
            {(entry) => (
              <div class="log-line" data-level={entry.level}>
                {entry.line}
              </div>
            )}
          </For>
        </Show>
      </div>
    </div>
  );
}

/**
 * Buffers incoming lines and commits them in batches, so a chatty stream does
 * not re-render on every single line.
 */
export function createLineBuffer() {
  const [lines, setLines] = createSignal<LogLine[]>([]);
  let pending: LogLine[] = [];
  let timer: number | undefined;

  const flush = () => {
    timer = undefined;
    if (pending.length === 0) {
      return;
    }
    const batch = pending;
    pending = [];
    setLines((current) => {
      const merged = current.concat(batch);
      return merged.length > MAX_LINES ? merged.slice(merged.length - MAX_LINES) : merged;
    });
  };
  const push = (line: LogLine) => {
    pending.push(line);
    timer ??= window.setTimeout(flush, 120);
  };
  const reset = () => {
    pending = [];
    setLines([]);
  };
  onCleanup(() => window.clearTimeout(timer));
  return { lines, push, reset };
}

/**
 * Follows a Server-Sent Events log endpoint. The stream restarts only when
 * the path changes, so background refreshes of the page never reset it.
 */
export function LogStream(props: { path: string | undefined; emptyText?: string }) {
  const buffer = createLineBuffer();
  const path = createMemo(() => props.path);

  createEffect(
    on(path, (current) => {
      buffer.reset();
      if (!current) {
        return;
      }
      let sequence = 0;
      const close = openStream<{ line: string }>(current, "log", {
        onMessage: (payload) => buffer.push({ id: ++sequence, level: "output", line: payload.line }),
      });
      onCleanup(close);
    }),
  );

  return <LogViewer lines={buffer.lines()} emptyText={props.emptyText ?? "Aguardando saída…"} />;
}

/** Live runtime logs of the containers of a resource. */
export function ContainerLogs(props: { serverId: string; containers: Container[] }) {
  const [selected, setSelected] = createSignal("");
  const current = () => props.containers.find((container) => container.id === selected()) ?? props.containers[0];
  const path = () => (current() ? `/servers/${props.serverId}/containers/${current()!.id}/logs?tail=300` : undefined);

  return (
    <Show
      when={props.containers.length > 0}
      fallback={
        <EmptyState icon="terminal" title="Nenhum container">
          Os logs aparecem aqui assim que o recurso estiver em execução.
        </EmptyState>
      }
    >
      <div class="stack">
        <Show when={props.containers.length > 1}>
          <Select
            label="Container"
            value={current()?.id ?? ""}
            onValue={setSelected}
            options={props.containers.map((container) => ({
              value: container.id,
              label: `${container.name} — ${container.status}`,
            }))}
          />
        </Show>
        <LogStream path={path()} emptyText="Aguardando saída do container…" />
      </div>
    </Show>
  );
}
