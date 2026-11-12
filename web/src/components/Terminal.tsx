import { createSignal, onCleanup, onMount, Show } from "solid-js";
import { Terminal as XTerminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { Button } from "./Button";
import { Modal } from "./Modal";
import { api } from "../lib/api";
import { t } from "../i18n";

/** The ticket endpoint resolves and authorizes the container server-side. */
export function TerminalButton(props: { endpoint: string; container?: string }) {
  const [open, setOpen] = createSignal(false);
  return <><Button small onClick={() => setOpen(true)}>{t("Terminal")}</Button>
    <Show when={open()}><Modal wide title={t("Terminal")} onClose={() => setOpen(false)}>
      <Terminal endpoint={props.endpoint} container={props.container} />
    </Modal></Show></>;
}

function Terminal(props: { endpoint: string; container?: string }) {
  let host!: HTMLDivElement;
  const [status, setStatus] = createSignal(t("Conectando terminal…"));
  const terminal = new XTerminal({ cursorBlink: true, convertEol: false, scrollback: 2000 });
  const fit = new FitAddon();
  terminal.loadAddon(fit);
  // Escape belongs to shell programs (vim, readline), not the surrounding modal.
  terminal.attachCustomKeyEventHandler((event) => { if (event.key === "Escape") event.stopPropagation(); return true; });
  let socket: WebSocket | undefined;
  let disposed = false;
  const observer = new ResizeObserver(() => { fit.fit(); resize(); });
  function resize() {
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "resize", cols: terminal.cols, rows: terminal.rows }));
  }
  onMount(() => {
    terminal.open(host);
    fit.fit();
    observer.observe(host);
    const input = terminal.onData((data) => {
      if (socket?.readyState !== WebSocket.OPEN) return;
      const bytes = new TextEncoder().encode(data);
      for (let offset = 0; offset < bytes.length; offset += 32768) socket.send(bytes.slice(offset, offset + 32768));
    });
    onCleanup(() => input.dispose());
    void api.post<{ ticket: string }>(props.endpoint, { container: props.container, shell: "auto" }).then((grant) => {
      if (disposed) return;
      const url = new URL("/api/v1/terminal", window.location.href);
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
      url.searchParams.set("ticket", grant.ticket);
      url.searchParams.set("cols", String(terminal.cols));
      url.searchParams.set("rows", String(terminal.rows));
      socket = new WebSocket(url);
      socket.binaryType = "arraybuffer";
      socket.onopen = () => { setStatus(t("Terminal conectado.")); terminal.focus(); resize(); };
      socket.onmessage = (event) => {
        if (event.data instanceof ArrayBuffer) terminal.write(new Uint8Array(event.data));
        else {
          try { const message = JSON.parse(event.data); if (message.type === "exit" || message.type === "closed") setStatus(t("Terminal encerrado.")); } catch { /* Ignore unknown control frames. */ }
        }
      };
      socket.onclose = () => setStatus(t("Terminal encerrado."));
      socket.onerror = () => setStatus(t("Não foi possível conectar o terminal."));
    }).catch((error: Error) => { if (!disposed) setStatus(error.message); });
  });
  onCleanup(() => { disposed = true; observer.disconnect(); socket?.close(); terminal.dispose(); });
  return <div class="stack"><p role="status">{status()}</p><div ref={host} style={{ height: "24rem", "min-width": "0", overflow: "hidden" }} /></div>;
}
