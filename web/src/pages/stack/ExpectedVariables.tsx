import { createMemo, For, Show } from "solid-js";
import { Alert, StatusBadge } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { api } from "../../lib/api";
import type { StatusView } from "../../lib/format";
import { createQuery } from "../../lib/query";
import type { StackVariable } from "../../lib/types";
import { Trans } from "../../i18n/Trans";
import { msg, t } from "../../i18n";

function statusOf(variable: StackVariable): StatusView {
  if (variable.source === "folder") {
    return { label: t("No .env da pasta"), tone: "success" };
  }
  if (variable.set) {
    return { label: t("Definida"), tone: "success" };
  }
  if (variable.required) {
    return { label: t("Obrigatória — falta definir"), tone: "danger" };
  }
  if (variable.kind === "address") {
    return { label: t("Sem domínio"), tone: "warning" };
  }
  return variable.default !== "" ? { label: t("Usa o padrão"), tone: "neutral" } : { label: t("Sem valor"), tone: "neutral" };
}

/**
 * Lists the variables the compose file interpolates and whether each will
 * have a value, so a missing one is seen before a deployment fails on it.
 * Names and defaults only: stored values stay in the editor below.
 */
export function ExpectedVariables(props: { stackId: string }) {
  const variables = createQuery(
    () => props.stackId,
    (id) => api.get<StackVariable[]>(`/stacks/${id}/variables`),
    { pollMs: 8000 },
  );
  // Generated ones are always set and only add noise here.
  const relevant = createMemo(() => (variables.data() ?? []).filter((variable) => variable.kind !== "generated"));
  const missing = createMemo(() => relevant().filter((variable) => variable.required && !variable.set));

  return (
    <Show when={relevant().length > 0}>
      <Section title={t("Esperadas pelo compose")} description={t("O que o arquivo compose referencia e de onde vem cada valor.")}>
        <Show when={missing().length > 0}>
          <Alert tone="danger">
            <Trans
              text={missing().length === 1 ? msg("O compose não sobe sem esta variável: {names}. Defina abaixo e implante.") : msg("O compose não sobe sem estas variáveis: {names}. Defina abaixo e implante.")}
              values={{ names: <span class="mono">{missing().map((variable) => variable.name).join(", ")}</span> }}
            />
          </Alert>
        </Show>
        <div class="list">
          <For each={relevant()}>
            {(variable) => (
              <div class="list-item">
                <div class="stack-xs" style={{ flex: "1 1 220px" }}>
                  <span class="mono truncate">{variable.name}</span>
                  <span class="small muted truncate">
                    {variable.kind === "address"
                      ? t("Endereço público: vem do domínio do serviço")
                      : variable.default !== ""
                        ? t("Padrão: {name}", { name: variable.default })
                        : t("Sem padrão")}
                  </span>
                </div>
                <StatusBadge status={statusOf(variable)} />
              </div>
            )}
          </For>
        </div>
      </Section>
    </Show>
  );
}
