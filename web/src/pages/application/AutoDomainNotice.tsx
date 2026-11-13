import { Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert } from "../../components/Feedback";
import { t } from "../../i18n";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import type { Application, AutoDomain } from "../../lib/types";

/**
 * Marks the domain Pier generated. Removing it is remembered: it does not
 * come back on the next deployment.
 */
export function AutoDomainNotice(props: { applicationId: string; onDomains: (domains: string[]) => void }) {
  const domain = createQuery(() => props.applicationId, (id) => api.get<AutoDomain>(`/applications/${id}/auto-domain`));
  const remove = createAction(() => api.delete<Application>(`/applications/${props.applicationId}/auto-domain`), {
    success: t("Domínio automático removido. Implante para aplicar."),
    onSuccess: (app) => {
      props.onDomains(app.config.network.domains);
      void domain.refetch();
    },
  });
  return (
    <Show when={domain.data()?.automatic ? domain.data() : undefined}>
      {(current) => (
        <Alert tone="info">
          <div class="row-between">
            <span>{t("Domínio automático: {url}", { url: current().url })}</span>
            <Button small variant="ghost" icon="trash" pending={remove.pending()} onClick={() => void remove.run()}>{t("Remover")}
            </Button>
          </div>
        </Alert>
      )}
    </Show>
  );
}
