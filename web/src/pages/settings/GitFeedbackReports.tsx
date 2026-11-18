import { For, Show } from "solid-js";
import { api } from "../../lib/api";
import { formatRelative } from "../../lib/format";
import { createAction, createQuery } from "../../lib/query";
import { t } from "../../i18n";
import { Button } from "../../components/Button";
import { QueryView, StatusBadge } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { reportStatus, reportSubject, type FailedReport } from "./gitFeedbackModel";

/**
 * Statuses and comments Pier could not deliver to the Git provider: the ones
 * still being retried and the ones it gave up on, with a manual resend.
 */
export function GitFeedbackReports(props: { kind: "applications" | "stacks"; id: string }) {
  const base = () => `/${props.kind}/${props.id}/git-provider/reports`;
  const reports = createQuery(() => base(), (path) => api.get<FailedReport[]>(path), { pollMs: 15000 });
  const resend = createAction((id: number) => api.post(`${base()}/${id}/resend`, {}), {
    success: t("Reenvio agendado."),
    onSuccess: () => void reports.refetch(),
  });

  return (
    <Section
      title={t("Envios ao provedor Git")}
      description={t("Status e comentários que ainda não chegaram ao provedor. Entregues não aparecem aqui; falhas somem após 30 dias.")}
    >
      <QueryView query={reports}>
        {(list) => (
          <Show when={list().length > 0} fallback={<p class="small muted">{t("Nada pendente: todos os envios foram entregues.")}</p>}>
            <div class="list">
              <For each={list()}>
                {(report) => (
                  <div class="list-item">
                    <div class="grow stack-xs">
                      <strong class="truncate">{reportSubject(report)}</strong>
                      <span class="small muted">
                        {report.last_error || t("Sem detalhe do erro.")}
                        <Show when={report.state !== "dead" && report.next_attempt_at}>
                          {" · "}{t("próxima tentativa {when}", { when: formatRelative(report.next_attempt_at) })}
                        </Show>
                      </span>
                    </div>
                    <StatusBadge status={reportStatus(report)} />
                    <Button small pending={resend.pending()} onClick={() => void resend.run(report.id)}>{t("Reenviar agora")}</Button>
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>
    </Section>
  );
}
