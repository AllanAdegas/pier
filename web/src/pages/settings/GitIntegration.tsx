import { createEffect, createSignal, Show } from "solid-js";
import { Button } from "../../components/Button";
import { Alert, QueryView } from "../../components/Feedback";
import { Checkbox, Select, TextInput } from "../../components/Form";
import { Section } from "../../components/Page";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import type { GitFeedbackProvider, GitFeedbackSettings, GitProviderLink } from "../../lib/types";
import { t } from "../../i18n";
import { GitRepositoryPicker } from "./GitRepositoryPicker";
import { feedbackInput, needsToken, suggestedProvider, type GitHubPick } from "./gitLinks";

type Kind = "applications" | "stacks";

/**
 * Git integration of one application or stack: the GitHub App link (private
 * clone and automatic webhooks) and the feedback on commits and pull requests.
 */
export function GitIntegration(props: { kind: Kind; id: string; repository: string }) {
  const base = () => `/${props.kind}/${props.id}/git-provider`;
  const link = createQuery(
    () => base(),
    (path) => api.get<GitProviderLink>(path),
  );
  const feedback = createQuery(
    () => props.kind === "applications" && base(),
    (path) => api.get<GitFeedbackSettings>(`${path}/feedback`),
  );
  return (
    <Section
      title={t("Integração Git")}
      description={t("Credenciais do GitHub App e status dos deploys nos commits e pull requests.")}
    >
      <QueryView query={link}>
        {(current) => (
          <GitHubLink
            base={base()}
            link={current()}
            onChanged={() => {
              void link.refetch();
              void feedback.refetch();
            }}
          />
        )}
      </QueryView>
      {/* Stacks have no deployment status reporting yet: only the link applies. */}
      <Show when={props.kind === "applications"}>
        <QueryView query={feedback}>
          {(current) => (
            <FeedbackForm
              base={base()}
              settings={current()}
              suggestion={suggestedProvider(props.repository, link.data()?.selected === true)}
              onSaved={(value) => feedback.set(value)}
            />
          )}
        </QueryView>
      </Show>
    </Section>
  );
}

function GitHubLink(props: { base: string; link: GitProviderLink; onChanged: () => void }) {
  const [pick, setPick] = createSignal<GitHubPick | null>(null);
  const save = createAction(() => api.put<GitProviderLink>(props.base, pick()), {
    success: t("Repositório vinculado ao GitHub App."),
    onSuccess: props.onChanged,
  });
  const remove = createAction(() => api.delete(props.base), {
    success: t("Vínculo removido. O próximo deploy usa a autenticação da origem."),
    onSuccess: props.onChanged,
  });
  return (
    <Show
      when={props.link.selected}
      fallback={
        <div class="stack">
          <GitRepositoryPicker onPick={(value) => setPick(value)} />
          <Show when={pick()}>
            <p class="muted small">{t("A origem precisa usar o mesmo endereço HTTPS do GitHub; senão o deploy é recusado.")}</p>
            <div class="form-actions">
              <Button pending={save.pending()} onClick={() => void save.run()}>{t("Vincular repositório")}
              </Button>
            </div>
          </Show>
        </div>
      }
    >
      <div class="row-between">
        <span>
          {t("Vinculado ao GitHub App:")} <span class="mono">{props.link.repository}</span>
        </span>
        <Button small variant="ghost" pending={remove.pending()} onClick={() => void remove.run()}>{t("Desvincular")}
        </Button>
      </div>
      <p class="muted small">{t("Push e pull requests chegam pelo webhook do app: não cadastre o webhook deste recurso no GitHub.")}</p>
    </Show>
  );
}

const PROVIDERS: { value: GitFeedbackProvider; label: string }[] = [
  { value: "none", label: "—" },
  { value: "github", label: "GitHub (GitHub App)" },
  { value: "gitlab", label: "GitLab" },
  { value: "gitea", label: "Gitea / Forgejo" },
  { value: "bitbucket", label: "Bitbucket Cloud" },
];

function FeedbackForm(props: {
  base: string;
  settings: GitFeedbackSettings;
  suggestion: GitFeedbackProvider;
  onSaved: (settings: GitFeedbackSettings) => void;
}) {
  const [draft, setDraft] = createSignal<GitFeedbackSettings>(props.settings);
  const [token, setToken] = createSignal("");
  createEffect(() => {
    if (!props.settings.saved && props.settings.provider === "none" && props.suggestion !== "none") {
      setDraft({ ...props.settings, provider: props.suggestion, commit_status: true, pull_request_comments: true });
    } else {
      setDraft(props.settings);
    }
  });
  const change = (patch: Partial<GitFeedbackSettings>) => setDraft({ ...draft(), ...patch });
  const save = createAction(() => api.put<GitFeedbackSettings>(`${props.base}/feedback`, feedbackInput(draft(), token())), {
    success: t("Preferências de retorno salvas."),
    onSuccess: (value) => {
      setToken("");
      props.onSaved(value);
    },
  });
  return (
    <form
      class="stack"
      onSubmit={(event) => {
        event.preventDefault();
        void save.run();
      }}
    >
      <Select
        label={t("Publicar no provedor")}
        value={draft().provider}
        onValue={(value) => change({ provider: value as GitFeedbackProvider })}
        options={PROVIDERS}
        error={save.errors().provider}
      />
      <Show when={draft().provider !== "none"}>
        <Checkbox
          label={t("Status do deploy no commit")}
          hint={t("Em andamento, concluído ou falhou, com link para o log.")}
          checked={draft().commit_status}
          onChange={(checked) => change({ commit_status: checked })}
        />
        <Checkbox
          label={t("Comentário com a URL do preview no pull request")}
          hint={t("Um único comentário, atualizado a cada deploy do preview.")}
          checked={draft().pull_request_comments}
          onChange={(checked) => change({ pull_request_comments: checked })}
        />
      </Show>
      <Show when={needsToken(draft().provider)}>
        <TextInput
          label={t("Token de acesso")}
          type="password"
          autocomplete="new-password"
          placeholder={props.settings.token_set ? t("Salvo; deixe vazio para manter") : ""}
          hint={tokenHint(draft().provider)}
          value={token()}
          onValue={setToken}
          error={save.errors().token}
        />
        <Alert tone="info">{t("O token só é enviado ao servidor do repositório atual. Se a origem mudar de host, salve-o de novo.")}</Alert>
      </Show>
      <div class="form-actions">
        <Button type="submit" pending={save.pending()}>{t("Salvar retorno")}
        </Button>
      </div>
    </form>
  );
}

function tokenHint(provider: GitFeedbackProvider): string {
  switch (provider) {
    case "gitlab":
      return t("Token pessoal, de grupo ou de projeto com o escopo api.");
    case "gitea":
      return t("Token com escrita em repositório e issues.");
    default:
      return t("Access token do repositório (Bearer) ou usuário:app password, com escrita em repositório e pull requests.");
  }
}
