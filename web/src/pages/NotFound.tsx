import { A } from "@solidjs/router";
import { EmptyState } from "../components/Feedback";
import { t } from "../i18n";

export function NotFoundPage() {
  return (
    <div class="page">
      <EmptyState
        icon="alert"
        title={t("Página não encontrada")}
        action={
          <A href="/" class="btn btn-primary">{t("Voltar ao painel")}
          </A>
        }
      >{t("O endereço não existe ou o recurso foi removido.")}
      </EmptyState>
    </div>
  );
}
