import { Show } from "solid-js";
import { Alert } from "../../components/Feedback";
import { t } from "../../i18n";
import { api } from "../../lib/api";
import { createQuery } from "../../lib/query";
import type { RedeployStatus } from "../../lib/types";

/** Shared variables changed after the last deployment: nothing redeploys on its own. */
export function RedeployNotice(props: { applicationId: string }) {
  const status = createQuery(() => props.applicationId, (id) => api.get<RedeployStatus>(`/applications/${id}/redeploy-status`), { pollMs: 15000 });
  return (
    <Show when={status.data()?.required}>
      <Alert tone="warning">{t("Implante de novo para usar os valores atuais. {reason}", { reason: status.data()?.reason ?? "" })}</Alert>
    </Show>
  );
}
