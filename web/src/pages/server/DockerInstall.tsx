import { createEffect, createSignal, For, Show } from "solid-js";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import type { DockerInstallMethod, OperationSnapshot, Server } from "../../lib/types";
import { Alert } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { Button } from "../../components/Button";
import { Select } from "../../components/Form";
import { ConfirmTyped } from "./ConfirmTyped";
import { t } from "../../i18n";

/**
 * Offered on an SSH server whose validation found no usable Docker: installs
 * Docker Engine after the operator types the server name, and follows the
 * output until the versions are verified.
 */
export function DockerInstall(props: { server: Server; onInstalled: () => void }) {
  const dockerMissing = () => (props.server.checks ?? []).some((check) => check.code === "docker" && check.status === "error");
  const [confirming, setConfirming] = createSignal(false);
  const [method, setMethod] = createSignal<DockerInstallMethod>("convenience");
  const [following, setFollowing] = createSignal(false);
  const path = () => `/servers/${props.server.id}/docker-install`;
  const status = createQuery(
    () => props.server.kind === "ssh" && (dockerMissing() || following()) && props.server.id,
    () => api.get<OperationSnapshot>(path()),
    { pollMs: () => (following() ? 2000 : 0) },
  );
  const start = createAction((confirmation: string) => api.post<OperationSnapshot>(path(), { method: method(), confirmation }), {
    onSuccess: (snapshot) => {
      status.set(snapshot);
      setFollowing(true);
      setConfirming(false);
    },
  });
  createEffect(() => {
    const snapshot = status.data();
    if (following() && snapshot && !snapshot.running && snapshot.finished_at) {
      setFollowing(false);
      if (!snapshot.error) props.onInstalled();
    }
  });

  return (
    <Show when={props.server.kind === "ssh" && (dockerMissing() || following() || status.data()?.lines.length)}>
      <Section
        title={t("Instalar o Docker")}
        description={t("O servidor não tem Docker 24 ou superior. O Pier pode instalar o Docker Engine por SSH, como root ou com sudo sem senha.")}
      >
        <Select
          label={t("Método")}
          value={method()}
          onValue={(value) => setMethod(value as DockerInstallMethod)}
          options={[
            { value: "convenience", label: t("Script oficial (get.docker.com)") },
            { value: "packages", label: t("Pacotes da distribuição (Ubuntu, Debian, Fedora)") },
          ]}
          hint={t("O script oficial instala a versão atual do Docker com os plugins compose e buildx. Pacotes da distribuição podem vir sem o compose 2.20.")}
        />
        <div>
          <Button variant="primary" icon="download" pending={start.pending() || status.data()?.running} onClick={() => setConfirming(true)}>
            {t("Instalar o Docker")}
          </Button>
        </div>
        <Show when={status.data()}>
          {(snapshot) => (
            <Show when={snapshot().lines.length > 0}>
              <Show when={snapshot().error}>
                <Alert tone="danger">{snapshot().error}</Alert>
              </Show>
              <pre class="log" style={{ height: "auto", "max-height": "320px", margin: "0" }}>
                <For each={snapshot().lines}>{(line) => <div>{line}</div>}</For>
              </pre>
            </Show>
          )}
        </Show>
      </Section>
      <Show when={confirming()}>
        <ConfirmTyped
          title={t("Instalar o Docker")}
          name={props.server.name}
          description={t("Comandos de instalação serão executados como root neste servidor. Pacotes do sistema podem ser atualizados.")}
          action={t("Instalar agora")}
          pending={start.pending()}
          onConfirm={(typed) => void start.run(typed)}
          onClose={() => setConfirming(false)}
        />
      </Show>
    </Show>
  );
}
