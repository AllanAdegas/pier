import { createSignal, Show } from "solid-js";
import { t } from "../i18n";
import { api } from "../lib/api";
import { docsUrl } from "../lib/docs";
import { createQuery } from "../lib/query";
import type { UpdateStatus } from "../lib/types";
import { Button } from "./Button";
import { Alert } from "./Feedback";
import { readDismissed, safeReleaseUrl, saveDismissed, shouldShowBanner, upgradeCommands } from "./updateRules";

/** Announces a newer Pier release with the exact commands to upgrade; dismissible per version. */
export function UpdateBanner() {
  const status = createQuery(() => true, () => api.get<UpdateStatus>("/updates"));
  const [dismissed, setDismissed] = createSignal(readDismissed(localStorage));

  const dismiss = (version: string) => {
    saveDismissed(localStorage, version);
    setDismissed(version);
  };

  return (
    <Show when={shouldShowBanner(status.data(), dismissed()) ? status.data() : undefined}>
      {(current) => (
        <Alert tone="info">
          <p>
            <strong>{t("Pier {latest} disponível", { latest: current().latest ?? "" })}</strong>{" "}
            {t("Você usa a versão {current}.", { current: current().current })}{" "}
            <Show when={safeReleaseUrl(current().url)}>
              {(url) => <a href={url()} target="_blank" rel="noopener noreferrer">{t("Ver novidades")}</a>}
            </Show>
          </p>
          <details>
            <summary>{t("Como atualizar")}</summary>
            <p class="small">{t("Na pasta onde o Pier foi instalado, rode (os dados são mantidos):")}</p>
            <pre class="log" style={{ height: "auto", margin: "0" }}>{upgradeCommands(current().latest ?? "")}</pre>
            <p class="small">
              <a href={docsUrl("atualizar")} target="_blank" rel="noopener noreferrer">{t("Guia de atualização")}</a>
            </p>
            <Show when={current().notes}>
              <p class="small muted" style={{ "white-space": "pre-wrap" }}>{current().notes}</p>
            </Show>
          </details>
          <Button variant="ghost" small onClick={() => dismiss(current().latest ?? "")}>{t("Dispensar")}</Button>
        </Alert>
      )}
    </Show>
  );
}
