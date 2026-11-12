import { createSignal, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert } from "../../components/Feedback";
import { CopyField, Section } from "../../components/Page";
import type { ApiToken } from "../../lib/types";
import { TokenForm } from "./TokenForm";
import { t } from "../../i18n";

const TOKEN_PLACEHOLDER = "<token>";

/** How to connect AI agents to this instance through the MCP endpoint. */
export function McpSection(props: { onTokenCreated: () => void }) {
  const [adding, setAdding] = createSignal(false);
  const [token, setToken] = createSignal<ApiToken | null>(null);
  const endpoint = () => `${window.location.origin}/mcp`;
  const secret = () => token()?.secret ?? TOKEN_PLACEHOLDER;

  const claudeCode = () => `claude mcp add --transport http pier ${endpoint()} --header "Authorization: Bearer ${secret()}"`;
  const httpConfig = () =>
    JSON.stringify({ mcpServers: { pier: { type: "http", url: endpoint(), headers: { Authorization: `Bearer ${secret()}` } } } });
  const stdioConfig = () =>
    JSON.stringify({
      mcpServers: { pier: { command: "pier", args: ["mcp"], env: { PIER_URL: window.location.origin, PIER_TOKEN: secret() } } },
    });

  return (
    <Section
      title={t("IA e agentes (MCP)")}
      description={t("Conecte Claude, Cursor ou outro agente pelo Model Context Protocol. O agente age com as permissões do token e não consegue remover nada.")}
      actions={
        <Button small icon="plus" onClick={() => setAdding(true)}>{t("Criar token para IA")}
        </Button>
      }
    >
      <div class="stack">
        <CopyField label={t("Endereço MCP")} value={endpoint()} hint={t("Aceita só token de API no cabeçalho Authorization, nunca a sessão do painel.")} />
        <Show
          when={token()}
          fallback={<p class="small muted">{t("Crie um token para a IA: os trechos abaixo passam a trazê-lo preenchido.")}</p>}
        >
          <Alert tone="warning">{t("Copie agora: o token aparece só desta vez.")}</Alert>
        </Show>
        <CopyField label="Claude Code" value={claudeCode()} secret={token() !== null} />
        <CopyField
          label={t("Clientes com MCP por HTTP (Cursor, Claude Desktop com conector, JSON genérico)")}
          value={httpConfig()}
          secret={token() !== null}
        />
        <CopyField
          label={t("Clientes só com stdio")}
          value={stdioConfig()}
          secret={token() !== null}
          hint={t("Usa o próprio binário do Pier como ponte: o comando pier precisa estar no PATH da máquina do agente.")}
        />
        <Alert tone="info">{t("Logs de deploy e de containers chegam ao agente e podem conter instruções de terceiros. Dê ao token só o que a IA precisa: Leitura para consultar, Deploy para implantar e reiniciar. Escrita raramente é necessária.")}
        </Alert>
      </div>
      <Show when={adding()}>
        <TokenForm
          initialName="agente-ia"
          initialAbilities={["read", "deploy"]}
          onClose={() => setAdding(false)}
          onSaved={(created) => {
            setAdding(false);
            setToken(created);
            props.onTokenCreated();
          }}
        />
      </Show>
    </Section>
  );
}
