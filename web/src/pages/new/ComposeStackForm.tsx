import { useNavigate } from "@solidjs/router";
import { createSignal } from "solid-js";
import { Button } from "../../components/Button";
import { Select, TextArea, TextInput } from "../../components/Form";
import { createAction } from "../../lib/query";
import type { Placement } from "../NewResource";
import { createStack } from "./stackCreation";

const COMPOSE_PLACEHOLDER = `services:
  web:
    image: nginx:alpine
    environment:
      SENHA: \${PIER_PASSWORD_WEB}
`;

/** Creates a stack from a pasted compose file and starts its first deployment. */
export function ComposeStackForm(props: { placement: Placement }) {
  const navigate = useNavigate();
  const [name, setName] = createSignal("");
  const [compose, setCompose] = createSignal("");
  const [serverId, setServerId] = createSignal(props.placement.servers[0]?.id ?? "");

  const create = createAction(
    () => createStack({ environment_id: props.placement.environmentId, server_id: serverId(), name: name(), compose: compose() }),
    { success: "Stack criada. Deploy iniciado.", onSuccess: (stack) => navigate(`/stacks/${stack.id}`) },
  );

  return (
    <form
      class="card stack"
      onSubmit={(event) => {
        event.preventDefault();
        void create.run();
      }}
    >
      <TextInput label="Nome" required value={name()} onValue={setName} error={create.errors().name} />
      <TextArea
        label="docker-compose.yaml"
        mono
        rows={16}
        placeholder={COMPOSE_PLACEHOLDER}
        hint="Variáveis ${PIER_PASSWORD_X}, ${PIER_USER_X}, ${PIER_SECRET_X}, ${PIER_HEX_X} e ${PIER_BASE64_X} são geradas e guardadas pelo Pier; ${PIER_URL_<SERVIÇO>} e ${PIER_HOST_<SERVIÇO>} recebem o endereço público de cada serviço com domínio. Domínios são configurados depois de criar."
        value={compose()}
        onValue={setCompose}
        error={create.errors().compose}
      />
      <Select
        label="Servidor"
        value={serverId()}
        onValue={setServerId}
        options={props.placement.servers.map((server) => ({ value: server.id, label: server.name }))}
        error={create.errors().server_id}
      />
      <div class="form-actions">
        <Button type="submit" variant="primary" icon="rocket" pending={create.pending()}>
          Criar e implantar
        </Button>
      </div>
    </form>
  );
}
