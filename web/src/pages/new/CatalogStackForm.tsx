import { useNavigate } from "@solidjs/router";
import { createMemo, createSignal, For, Show } from "solid-js";
import { createStore } from "solid-js/store";
import { Button } from "../../components/Button";
import { Alert, EmptyState, QueryView } from "../../components/Feedback";
import { Icon } from "../../components/Icon";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import type { ComposeVariable, StackTemplate, StackTemplateRoute } from "../../lib/types";
import type { Placement } from "../NewResource";
import { createStack } from "./stackCreation";
import { createDomainDnsCheck } from "../application/DomainDnsStatus";
import { t } from "../../i18n";
import { Select, TextInput } from "../../components/Form";

const ALL = "";
/** Rendering every card at once is slow to scan; more load on demand. */
const PAGE_SIZE = 60;

/** Creates a stack from the catalog and starts its first deployment. */
/** What each risk a template declares means for the server. */
export const RISK_LABELS: Record<string, string> = {
  "docker-socket": t("Controla o Docker do servidor, o que equivale a acesso root à máquina."),
  privileged: t("Roda em modo privilegiado, com acesso aos dispositivos do servidor."),
  "host-network": t("Usa a rede do servidor diretamente: as portas abrem sem passar pelo proxy."),
  "host-ports": t("Publica portas diretamente no servidor, fora do proxy. Confira o firewall."),
  "capabilities": t("Recebe permissões ou dispositivos que podem afetar o servidor."),
  "unconfined": t("Desativa o isolamento de segurança ou compartilha namespaces do servidor."),
  "host-path": t("Monta caminhos do servidor que podem conter dados de outras stacks."),
  "first-visitor-setup": t("O primeiro visitante pode criar o administrador. Faça a configuração em uma rede restrita."),
  "no-auth": t("Este serviço não tem autenticação configurada. Restrinja o acesso antes de publicar."),
  "host-pid": t("Enxerga e pode sinalizar os processos do servidor."),
};

export function CatalogStackForm(props: { placement: Placement }) {
  const templates = createQuery(
    () => true,
    () => api.get<StackTemplate[]>("/stack-templates"),
  );
  const [search, setSearch] = createSignal("");
  const [category, setCategory] = createSignal(ALL);
  const [shown, setShown] = createSignal(PAGE_SIZE);
  const [chosen, setChosen] = createSignal<string | null>(null);

  const categories = createMemo(() => {
    const counts = new Map<string, number>();
    for (const item of templates.data() ?? []) {
      counts.set(item.category, (counts.get(item.category) ?? 0) + 1);
    }
    return [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0], "pt-BR"));
  });
  const filtered = createMemo(() => {
    const term = search().trim().toLowerCase();
    return (templates.data() ?? []).filter(
      (item) =>
        (category() === ALL || item.category === category()) &&
        (term === "" || `${item.name} ${item.slug} ${item.category} ${item.description} ${item.tags.join(" ")}`.toLowerCase().includes(term)),
    );
  });
  const narrow = (apply: () => void) => {
    apply();
    setShown(PAGE_SIZE);
  };

  return (
    <div class="stack">
      <Show
        when={chosen()}
        fallback={
          <QueryView query={templates}>
            {(all) => (
              <>
                <div class="catalog-filters">
                  <TextInput
                    label={t("Buscar no catálogo")}
                    type="search"
                    placeholder={t("grafana, cms, whatsapp, postgres…")}
                    value={search()}
                    onValue={(value) => narrow(() => setSearch(value))}
                  />
                  <Select
                    label={t("Categoria")}
                    value={category()}
                    onValue={(value) => narrow(() => setCategory(value))}
                    options={[
                      { value: ALL, label: t("Todas ({count})", { count: all().length }) },
                      ...categories().map(([name, count]) => ({ value: name, label: `${name} (${count})` })),
                    ]}
                  />
                </div>
                <p class="small muted" aria-live="polite">
                  {filtered().length === all().length
                    ? t("{count} serviços no catálogo", { count: all().length })
                    : t("{shown} de {count} serviços", { shown: filtered().length, count: all().length })}
                </p>
                <Show
                  when={filtered().length > 0}
                  fallback={
                    <EmptyState icon="layers" title={t("Nada encontrado")}>{t("Nenhum serviço do catálogo corresponde à busca. Você também pode usar um compose próprio.")}
                    </EmptyState>
                  }
                >
                  <div class="choice-grid">
                    <For each={filtered().slice(0, shown())}>
                      {(item) => (
                        <button type="button" class="choice" onClick={() => setChosen(item.slug)}>
                          <span class="row-between" style={{ "align-items": "flex-start" }}>
                            <span class="choice-title">{item.name}</span>
                            <span class="tag">{item.category}</span>
                          </span>
                          <span class="choice-text">{item.description}</span>
                        </button>
                      )}
                    </For>
                  </div>
                  <Show when={filtered().length > shown()}>
                    <div class="form-actions" style={{ "justify-content": "center" }}>
                      <Button onClick={() => setShown(shown() + PAGE_SIZE)}>
                        {t("Mostrar mais ({count} restantes)", { count: filtered().length - shown() })}
                      </Button>
                    </div>
                  </Show>
                </Show>
              </>
            )}
          </QueryView>
        }
      >
        {(slug) => <TemplateForm slug={slug()} placement={props.placement} onBack={() => setChosen(null)} />}
      </Show>
    </div>
  );
}

/** One domain field: the route it publishes and the routes that share it. */
interface DomainField {
  route: StackTemplateRoute;
  /** Routes that answer under a path of the same domain. */
  nested: StackTemplateRoute[];
}

/**
 * Groups the routes of a template into the domains a person has to type:
 * a route with a path prefix lives under the domain of the first route.
 */
function domainFields(routes: StackTemplateRoute[]): DomainField[] {
  const own = routes.filter((route) => route.path === "");
  const nested = routes.filter((route) => route.path !== "");
  if (own.length === 0) {
    return nested.length > 0 ? [{ route: { ...nested[0]!, path: "" }, nested }] : [];
  }
  return own.map((route, index) => ({ route, nested: index === 0 ? nested : [] }));
}

function TemplateForm(props: { slug: string; placement: Placement; onBack: () => void }) {
  const navigate = useNavigate();
  const template = createQuery(
    () => props.slug,
    (slug) => api.get<StackTemplate>(`/stack-templates/${slug}`),
  );
  const [name, setName] = createSignal(props.slug);
  const [serverId, setServerId] = createSignal(props.placement.servers[0]?.id ?? "");
  const [domains, setDomains] = createStore<Record<string, string>>({});
  const [risksAccepted, setRisksAccepted] = createSignal(false);
  const [values, setValues] = createStore<Record<string, string>>({});

  const dns = createDomainDnsCheck(serverId);
  const checkDomains = () => void dns.check(Object.values(domains));

  const fields = createMemo(() => domainFields(template.data()?.routes ?? []));
  const variables = () => template.data()?.variables ?? [];
  const required = createMemo(() => variables().filter((variable) => variable.required));
  const optional = createMemo(() => variables().filter((variable) => !variable.required));

  const create = createAction(
    () => {
      if ((template.data()?.risks.length ?? 0) > 0 && !risksAccepted()) return Promise.reject(new Error(t("Confirme os riscos antes de criar a stack.")));
      const routes = fields().flatMap((field) => {
        const domain = (domains[field.route.service] ?? "").trim().replace(/\/+$/, "");
        if (domain === "") {
          return [];
        }
        return [
          { service: field.route.service, port: field.route.port, domain },
          ...field.nested.map((route) => ({ service: route.service, port: route.port, domain: domain + route.path })),
        ];
      });
      const env = Object.fromEntries(Object.entries(values).filter(([, value]) => value.trim() !== ""));
      return createStack({
        environment_id: props.placement.environmentId,
        server_id: serverId(),
        name: name(),
        template: props.slug,
        routes,
        env,
      });
    },
    { success: t("Stack criada. Deploy iniciado."), onSuccess: (stack) => navigate(`/stacks/${stack.id}`) },
  );
  const routeError = () => Object.entries(create.errors()).find(([field]) => field.startsWith("routes."))?.[1];

  return (
    <QueryView query={template}>
      {(data) => (
        <form
          class="card stack"
          onSubmit={(event) => {
            event.preventDefault();
            void create.run();
          }}
        >
          <div class="row-between" style={{ "align-items": "flex-start" }}>
            <div class="grow stack-sm">
              <h2>{data().name}</h2>
              <p class="muted small">{data().description}</p>
              <Show when={data().website}>
                <a class="small" href={data().website} target="_blank" rel="noreferrer">{t("Documentação")} <Icon name="external" size={12} />
                </a>
              </Show>
            </div>
            <Button small onClick={props.onBack}>{t("Escolher outro")}
            </Button>
          </div>

          <TextInput label={t("Nome")} required value={name()} onValue={setName} error={create.errors().name} />

          <div class="stack" onFocusOut={checkDomains}>
          <For each={fields()}>
            {(field) => (
              <TextInput
                label={fields().length > 1 ? t("Domínio de {name}", { name: field.route.service }) : t("Domínio")}
                mono
                placeholder={t("https://servico.exemplo.com")}
                hint={t(
                  "Publica “{service}” (porta {port}) pelo proxy. Defina já na criação se o serviço usa o próprio endereço (links, webhooks, login).",
                  { service: field.route.service, port: field.route.port },
                )}
                value={domains[field.route.service] ?? ""}
                onValue={(value) => setDomains(field.route.service, value)}
              />
            )}
          </For>
          </div>
          <dns.View />
          <Show when={routeError()}>{(message) => <Alert tone="danger">{message()}</Alert>}</Show>

          <Show when={required().length > 0}>
            <fieldset class="stack">
              <legend class="section-title">{t("Este serviço precisa de")}</legend>
              <For each={required()}>{(variable) => <VariableInput variable={variable} values={values} onValue={setValues} required />}</For>
            </fieldset>
          </Show>
          <Show when={optional().length > 0}>
            <details>
              <summary class="small">{t("Variáveis opcionais ({count})", { count: optional().length })}</summary>
              <div class="stack" style={{ "margin-top": "var(--space-3)" }}>
                <p class="small muted">{t("Em branco, vale o padrão do template. Todas podem ser alteradas depois, na aba Variáveis.")}</p>
                <For each={optional()}>{(variable) => <VariableInput variable={variable} values={values} onValue={setValues} />}</For>
              </div>
            </details>
          </Show>
          <Show when={create.errors().env}>{(message) => <Alert tone="danger">{message()}</Alert>}</Show>

          <Select
            label={t("Servidor")}
            value={serverId()}
            onValue={(value) => {
              setServerId(value);
              checkDomains();
            }}
            options={props.placement.servers.map((server) => ({ value: server.id, label: server.name }))}
            error={create.errors().server_id}
          />

          <Show when={data().risks.length > 0}>
            <Alert tone="warning">
              <div class="stack-sm">
                <strong>{t("Este serviço pede acesso além dos próprios containers:")}</strong>
                <ul style={{ margin: "0", "padding-left": "1.2em" }}>
                  <For each={data().risks}>{(risk) => <li>{RISK_LABELS[risk] ?? risk}</li>}</For>
                </ul>
                <span class="small">{t("Implante só se confia no projeto e na imagem.")}</span>
              </div>
            </Alert>
          </Show>

          <Show when={!data().verified}>
            <p class="small muted">{t("Este serviço ainda não foi testado num deploy real. Senhas e chaves são geradas pelo Pier; a imagem e a configuração seguem a documentação de cada projeto. Confira o resultado antes de usar em produção.")}
            </p>
          </Show>

          <Show when={(data().host_ports?.length ?? 0) > 0}>
            <p class="small mono">{t("Portas no servidor")}: {data().host_ports?.join(", ")}</p>
          </Show>
          <Show when={data().risks.length > 0}>
            <label class="row"><input type="checkbox" required checked={risksAccepted()} onChange={(e) => setRisksAccepted(e.currentTarget.checked)} />{t("Entendo os riscos e quero criar esta stack.")}</label>
          </Show>
          <div class="form-actions">
            <Button type="submit" variant="primary" icon="rocket" pending={create.pending()} disabled={data().risks.length > 0 && !risksAccepted()}>{t("Criar e implantar")}
            </Button>
          </div>
        </form>
      )}
    </QueryView>
  );
}

function VariableInput(props: {
  variable: ComposeVariable;
  values: Record<string, string>;
  onValue: (name: string, value: string) => void;
  required?: boolean;
}) {
  return (
    <TextInput
      label={props.variable.name}
      mono
      required={props.required}
      placeholder={props.variable.default}
      value={props.values[props.variable.name] ?? ""}
      onValue={(value) => props.onValue(props.variable.name, value)}
    />
  );
}
