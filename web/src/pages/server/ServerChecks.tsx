import { For, Show } from "solid-js";
import { StatusBadge } from "../../components/Feedback";
import type { ServerCheck } from "../../lib/types";
import { t } from "../../i18n";

export function ServerChecks(props: { checks: ServerCheck[] }) {
  return (
    <Show when={props.checks.length > 0}>
      <section class="card stack" aria-label={t("Validação do servidor")}>
        <h2>{t("Validação do servidor")}</h2>
        <For each={props.checks}>
          {(check) => (
            <div class="stack-sm">
              <div class="row">
                <StatusBadge status={{
                  label: check.status === "pass" ? t("OK") : check.status === "error" ? t("Corrigir") : check.status === "warning" ? t("Atenção") : t("Informação"),
                  tone: check.status === "pass" ? "success" : check.status === "error" ? "danger" : check.status === "warning" ? "warning" : "neutral",
                }} />
                <strong>{check.message}</strong>
              </div>
              <Show when={check.status !== "pass"}>
                <p class="small muted">{check.fix}</p>
              </Show>
            </div>
          )}
        </For>
      </section>
    </Show>
  );
}
