import { Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert, QueryView } from "../../components/Feedback";
import { api } from "../../lib/api";
import { createQuery } from "../../lib/query";
import type { StackSource } from "../../lib/types";

/**
 * Shows the compose file of a directory stack as it is on the server right
 * now. It is read-only here: the file is edited in the folder, where the
 * rest of the project lives.
 */
export function DirectorySource(props: { stackId: string }) {
  const source = createQuery(
    () => props.stackId,
    (id) => api.get<StackSource>(`/stacks/${id}/source`),
  );
  return (
    <QueryView query={source}>
      {(data) => (
        <div class="stack-sm">
          <div class="row-between">
            <span class="field-label">
              {data().compose_file} <span class="muted">— lido do disco agora</span>
            </span>
            <Button small icon="refresh" onClick={() => void source.refetch()}>
              Ler de novo
            </Button>
          </div>
          <pre class="log" style={{ height: "auto", "max-height": "420px", margin: "0" }}>
            {data().content}
          </pre>
          <Show
            when={data().env_file}
            fallback={<p class="small muted">A pasta não tem .env; as variáveis vêm só da aba Variáveis.</p>}
          >
            <Alert tone="info">
              A pasta tem um .env, que o compose usa direto do disco. Uma variável com o mesmo nome definida no Pier tem precedência.
            </Alert>
          </Show>
        </div>
      )}
    </QueryView>
  );
}
