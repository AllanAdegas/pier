import { createSignal, For, Show } from "solid-js";
import { api } from "../../lib/api";
import { formatRelative } from "../../lib/format";
import { createAction, createQuery } from "../../lib/query";
import type { Server } from "../../lib/types";
import { t } from "../../i18n";
import { Button } from "../../components/Button";
import { Alert, StatusBadge } from "../../components/Feedback";
import { Modal } from "../../components/Modal";
import { Section } from "../../components/Page";
import { shortImage } from "./placementModel";
import { journalStatus, rolloutProgress, stepStatus, type RolloutJournal } from "./rolloutModel";

/** The last multi-server rollout: where the image is, server by server, and how to recover it. */
export function RolloutPanel(props: { applicationId: string }) {
  const journal = createQuery(
    () => props.applicationId,
    (id) => api.get<RolloutJournal | null>(`/applications/${id}/placement/rollout`),
    { pollMs: 4000 },
  );
  const servers = createQuery(() => true, () => api.get<Server[]>("/servers"));
  const serverName = (id: string) => servers.data()?.find((server) => server.id === id)?.name ?? id;
  const [confirmRollback, setConfirmRollback] = createSignal(false);
  const resume = createAction(() => api.post(`/applications/${props.applicationId}/placement/rollout/resume`, {}), {
    success: t("Rollout retomado."),
    onSuccess: () => void journal.refetch(),
  });
  const rollback = createAction(() => api.post(`/applications/${props.applicationId}/placement/rollout/rollback`, {}), {
    success: t("Reversão iniciada."),
    onSuccess: () => {
      setConfirmRollback(false);
      void journal.refetch();
    },
  });

  return (
    <Show when={journal.data()}>
      {(current) => {
        const progress = () => rolloutProgress(current());
        return (
          <Section
            title={t("Último rollout")}
            description={t("{released} de {total} servidores com a nova versão · iniciado {when}", {
              released: progress().released, total: progress().total, when: formatRelative(current().started_at),
            })}
            actions={<StatusBadge status={journalStatus(current().state)} />}
          >
            <p class="small muted">{t("Imagem")}: <span class="mono">{shortImage(current().image)}</span></p>
            <Show when={current().message}>
              <Alert tone={current().state === "completed" ? "info" : "warning"}><span>{current().message}</span></Alert>
            </Show>
            <ol class="list rollout-steps">
              <For each={progress().steps}>
                {(step) => (
                  <li class="list-item">
                    <span class="rollout-position" aria-hidden="true">{step.position}</span>
                    <div class="grow stack-xs">
                      <strong class="truncate">{serverName(step.server_id)}</strong>
                      <Show when={step.previous_image}>
                        <span class="small muted mono truncate">{t("antes")}: {shortImage(step.previous_image)}</span>
                      </Show>
                    </div>
                    <StatusBadge status={stepStatus(step.state)} />
                  </li>
                )}
              </For>
            </ol>
            <Show when={current().state === "interrupted"}>
              <p class="small muted">{t("Retome a imagem fixada ou restaure a versão anterior. Comandos de release podem exigir verificação manual.")}</p>
              <div class="form-actions">
                <Button disabled={resume.pending()} onClick={() => setConfirmRollback(true)}>{t("Reverter rollout")}</Button>
                <Button variant="primary" pending={resume.pending()} disabled={rollback.pending()} onClick={() => void resume.run()}>
                  {t("Retomar rollout")}
                </Button>
              </div>
            </Show>
            <Show when={confirmRollback()}>
              <Modal title={t("Reverter rollout")} onClose={() => setConfirmRollback(false)}>
                <div class="stack">
                  <p>{t("Os servidores que já receberam a nova versão voltam para a imagem anterior. Migrações de banco feitas pelo release não são desfeitas.")}</p>
                  <div class="form-actions">
                    <Button onClick={() => setConfirmRollback(false)}>{t("Cancelar")}</Button>
                    <Button variant="danger" pending={rollback.pending()} onClick={() => void rollback.run()}>{t("Reverter rollout")}</Button>
                  </div>
                </div>
              </Modal>
            </Show>
          </Section>
        );
      }}
    </Show>
  );
}
