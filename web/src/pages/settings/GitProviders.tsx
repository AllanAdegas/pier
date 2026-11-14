import { GitConnections } from "./GitConnections";
import { createSignal, For, onMount, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert, EmptyState, QueryView } from "../../components/Feedback";
import { TextArea, TextInput } from "../../components/Form";
import { CopyField, PageHeader, Section } from "../../components/Page";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import type { GitHubInstallation, GitHubManifestStart, GitProviderStatus } from "../../lib/types";
import { t } from "../../i18n";
import { manifestCallback } from "./gitLinks";

/** Route of the GitHub App callback: GitHub sends the browser back here. */
export function GitProvidersPage() {
  return (
    <div class="page">
      <PageHeader title={t("GitHub App")} description={t("Repositórios privados, webhooks automáticos e status nos pull requests.")} />
      <GitProviders />
      <GitConnections />
    </div>
  );
}

/** Settings section of the instance's GitHub App. */
export function GitProviders() {
  const status = createQuery(
    () => true,
    () => api.get<GitProviderStatus>("/git-provider"),
  );
  const complete = createAction(
    (callback: { code: string; state: string }) => api.post<GitProviderStatus>("/git-provider/manifest/complete", callback),
    { success: t("GitHub App criado. Agora instale-o nas contas e repositórios que o Pier deve acessar."), onSuccess: (value) => status.set(value) },
  );
  onMount(() => {
    const callback = manifestCallback(window.location.search);
    if (callback) {
      // The one-time code must not stay in the address bar or the history.
      window.history.replaceState(null, "", window.location.pathname);
      void complete.run(callback);
    }
  });
  return (
    <Section
      title={t("GitHub App")}
      description={t("Clona repositórios privados com tokens de curta duração, recebe push e pull requests sem webhook por recurso e publica o status dos deploys.")}
    >
      <QueryView query={status}>
        {(current) => (
          <Show when={current().configured} fallback={<CreateApp onSaved={(value) => status.set(value)} />}>
            <ConfiguredApp status={current()} onSaved={(value) => status.set(value)} />
          </Show>
        )}
      </QueryView>
    </Section>
  );
}

function CreateApp(props: { onSaved: (status: GitProviderStatus) => void }) {
  const [organization, setOrganization] = createSignal("");
  const start = createAction(() => api.post<GitHubManifestStart>("/git-provider/manifest", { organization: organization().trim() }), {
    onSuccess: submitManifest,
  });
  return (
    <div class="stack">
      <p class="muted small">
        {t("O Pier cria o app no GitHub com as permissões mínimas (conteúdo: leitura; status de commit e pull requests: escrita) e o webhook já apontando para este painel. Nenhum segredo é copiado à mão.")}
      </p>
      <form
        class="stack"
        onSubmit={(event) => {
          event.preventDefault();
          void start.run();
        }}
      >
        <TextInput
          label={t("Organização do GitHub (opcional)")}
          hint={t("Vazio, o app pertence à sua conta pessoal.")}
          mono
          value={organization()}
          onValue={setOrganization}
          error={start.errors().organization}
        />
        <div class="form-actions">
          <Button type="submit" variant="primary" pending={start.pending()}>{t("Criar GitHub App no GitHub")}
          </Button>
        </div>
      </form>
      <details>
        <summary>{t("Já tenho um GitHub App")}</summary>
        <ManualCredentials onSaved={props.onSaved} />
      </details>
    </div>
  );
}

/** GitHub only accepts the manifest as a form POST from the browser. */
function submitManifest(start: GitHubManifestStart) {
  const form = document.createElement("form");
  form.method = "post";
  form.action = start.action_url;
  const field = document.createElement("input");
  field.type = "hidden";
  field.name = "manifest";
  field.value = start.manifest;
  form.appendChild(field);
  document.body.appendChild(form);
  form.submit();
}

function ManualCredentials(props: { onSaved: (status: GitProviderStatus) => void }) {
  const [appId, setAppId] = createSignal("");
  const [slug, setSlug] = createSignal("");
  const [key, setKey] = createSignal("");
  const [secret, setSecret] = createSignal("");
  const save = createAction(
    () =>
      api.put<GitProviderStatus>("/git-provider", {
        app_id: Number(appId()),
        slug: slug().trim(),
        private_key: key(),
        webhook_secret: secret(),
      }),
    {
      success: t("GitHub App salvo."),
      onSuccess: (status) => {
        setKey("");
        setSecret("");
        props.onSaved(status);
      },
    },
  );
  return (
    <form
      class="stack"
      onSubmit={(event) => {
        event.preventDefault();
        void save.run();
      }}
    >
      <div class="grid-2">
        <TextInput label={t("App ID")} required inputmode="numeric" mono value={appId()} onValue={setAppId} />
        <TextInput
          label={t("Nome curto do app (slug)")}
          hint={t("Opcional; habilita o link de instalação.")}
          mono
          value={slug()}
          onValue={setSlug}
        />
      </div>
      <TextArea label={t("Chave privada PEM")} mono rows={6} value={key()} onValue={setKey} />
      <TextInput
        label={t("Segredo do webhook (mínimo 32 caracteres)")}
        type="password"
        autocomplete="new-password"
        value={secret()}
        onValue={setSecret}
      />
      <div class="form-actions">
        <Button type="submit" pending={save.pending()}>{t("Salvar credenciais")}
        </Button>
      </div>
    </form>
  );
}

function ConfiguredApp(props: { status: GitProviderStatus; onSaved: (status: GitProviderStatus) => void }) {
  const installations = createQuery(
    () => true,
    () => api.get<GitHubInstallation[]>("/git-provider/installations"),
  );
  return (
    <div class="stack">
      <dl class="grid-2">
        <div>
          <dt class="field-label">{t("App ID")}</dt>
          <dd class="mono">{props.status.app_id}</dd>
        </div>
        <Show when={props.status.slug}>
          <div>
            <dt class="field-label">{t("Nome curto do app (slug)")}</dt>
            <dd class="mono">{props.status.slug}</dd>
          </div>
        </Show>
      </dl>
      <CopyField
        label={t("URL do webhook do app")}
        value={props.status.webhook_url}
        hint={t("Recebe push e pull requests de todos os repositórios vinculados. Não cadastre também o webhook da aplicação.")}
      />
      <div class="row-between">
        <h3>{t("Instalações")}</h3>
        <Show when={props.status.install_url}>
          {(url) => (
            <a class="btn btn-sm" href={url()} target="_blank" rel="noopener noreferrer">{t("Instalar em uma conta")}
            </a>
          )}
        </Show>
      </div>
      <QueryView query={installations}>
        {(rows) => (
          <Show
            when={rows().length > 0}
            fallback={
              <EmptyState icon="key" title={t("Nenhuma instalação")}>{t("Instale o app no GitHub e escolha os repositórios que o Pier pode ler.")}
              </EmptyState>
            }
          >
            <div class="list">
              <For each={rows()}>
                {(installation) => (
                  <div class="list-item">
                    <span class="mono">{installation.account}</span>
                    <span class="muted small">
                      {installation.repository_selection === "all" ? t("Todos os repositórios") : t("Repositórios selecionados")}
                    </span>
                    <Show when={installation.suspended}>
                      <Alert tone="warning">{t("Suspensa: o Pier não consegue clonar nem publicar status.")}</Alert>
                    </Show>
                  </div>
                )}
              </For>
            </div>
          </Show>
        )}
      </QueryView>
      <details>
        <summary>{t("Substituir as credenciais do app")}</summary>
        <ManualCredentials onSaved={props.onSaved} />
      </details>
    </div>
  );
}
