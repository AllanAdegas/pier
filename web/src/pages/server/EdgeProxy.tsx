import { createSignal, For, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { api } from "../../lib/api";
import { createAction } from "../../lib/query";
import type { EdgeProxy, EdgeSync, Server } from "../../lib/types";
import { Trans } from "../../i18n/Trans";
import { t } from "../../i18n";
import { Select, TextInput } from "../../components/Form";

/**
 * Settings of the Caddy that owns ports 80/443 in front of Pier's proxy.
 * Rendered only when the server runs a Caddy or one is already configured:
 * everywhere else the setting is noise.
 */
export function EdgeFields(props: {
  candidates: string[];
  edge: EdgeProxy;
  onChange: (field: keyof EdgeProxy, value: string) => void;
  errors: Record<string, string>;
}) {
  const containers = () => {
    const configured = props.edge.container.trim();
    return configured && !props.candidates.includes(configured) ? [...props.candidates, configured] : props.candidates;
  };
  return (
    <Show when={containers().length > 0}>
      <EdgeFieldset containers={containers()} edge={props.edge} onChange={props.onChange} errors={props.errors} />
    </Show>
  );
}

function EdgeFieldset(props: {
  containers: string[];
  edge: EdgeProxy;
  onChange: (field: keyof EdgeProxy, value: string) => void;
  errors: Record<string, string>;
}) {
  return (
    <fieldset class="stack" style={{ border: "0", padding: "0", margin: "0" }}>
      <legend class="field-label" style={{ "margin-bottom": "var(--space-2)" }}>{t("Caddy na frente")}
      </legend>
      <p class="small muted" style={{ margin: "0" }}>{t("Este servidor tem um Caddy. Se ele ocupa as portas 80 e 443, escolha-o: o proxy do Pier fica em outras portas e o Pier mantém um bloco no Caddyfile com os domínios deste servidor; o Caddy emite os certificados e encaminha para o Pier.")}
      </p>
      <div class="grid-2">
        <Select
          label={t("Container do Caddy")}
          placeholder={t("Nenhum (o Pier atende direto)")}
          options={props.containers.map((name) => ({
            value: name,
            label: name,
          }))}
          value={props.edge.container}
          onValue={(value) => props.onChange("container", value)}
          error={props.errors["proxy.edge.container"]}
        />
        <Show when={props.edge.container.trim()}>
          <TextInput
            label={t("Caddyfile dentro do container")}
            hint={t("Precisa vir de um arquivo do servidor (montagem).")}
            placeholder="/etc/caddy/Caddyfile"
            value={props.edge.caddyfile}
            onValue={(value) => props.onChange("caddyfile", value)}
            error={props.errors["proxy.edge.caddyfile"]}
          />
        </Show>
      </div>
    </fieldset>
  );
}

/** Shows and refreshes the Pier block of the edge Caddyfile on demand. */
export function EdgeSyncCard(props: { server: Server }) {
  const [result, setResult] = createSignal<EdgeSync | null>(null);
  const sync = createAction(() => api.post<EdgeSync>(`/servers/${props.server.id}/proxy/edge/sync`), {
    success: (outcome) => (outcome.changed ? t("Caddyfile atualizado e Caddy recarregado.") : t("O Caddyfile já estava em dia.")),
    onSuccess: setResult,
  });

  return (
    <Section
      title={t("Caddy na frente")}
      description={t(
        "O Pier atualiza o bloco dele no Caddyfile de {container} a cada deploy e remoção. Sincronize se o arquivo foi alterado por fora.",
        { container: props.server.proxy.edge.container },
      )}
      actions={
        <Button icon="refresh" pending={sync.pending()} onClick={() => void sync.run()}>{t("Sincronizar agora")}
        </Button>
      }
    >
      <Show when={result()}>
        {(outcome) => (
          <div class="stack-sm">
            <Show
              when={outcome().addresses.length > 0}
              fallback={<Alert tone="info">{t("Nenhum domínio publicado neste servidor: o bloco do Pier foi removido.")}</Alert>}
            >
              <span class="small muted">
                <Trans
                  text="Encaminhados para {upstream} em {caddyfile}:"
                  values={{ upstream: <code>{outcome().upstream}</code>, caddyfile: <code>{outcome().caddyfile}</code> }}
                />
              </span>
              <ul class="small" style={{ margin: "0" }}>
                <For each={outcome().addresses}>
                  {(address) => (
                    <li>
                      <code>{address}</code>
                    </li>
                  )}
                </For>
              </ul>
            </Show>
          </div>
        )}
      </Show>
    </Section>
  );
}
