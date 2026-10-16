import { A } from "@solidjs/router";
import { EmptyState } from "../components/Feedback";

export function NotFoundPage() {
  return (
    <div class="page">
      <EmptyState
        icon="alert"
        title="Página não encontrada"
        action={
          <A href="/" class="btn btn-primary">
            Voltar ao painel
          </A>
        }
      >
        O endereço não existe ou o recurso foi removido.
      </EmptyState>
    </div>
  );
}
