import { ConnectionChoice } from "./GitConnections";
import { createSignal, Show } from "solid-js";
import { Select } from "../../components/Form";
import { api } from "../../lib/api";
import { createQuery } from "../../lib/query";
import type { GitHubInstallation, GitHubRepository, GitProviderStatus } from "../../lib/types";
import { t } from "../../i18n";
import { githubCloneURL, pickStillApplies, type GitHubPick } from "./gitLinks";

/**
 * Picks a repository of a GitHub App installation. Renders nothing when no
 * App is configured. onPick receives the choice and the clone address.
 */
export function GitRepositoryPicker(props: { onPick: (pick: GitHubPick | null, cloneURL: string) => void }) {
  const status = createQuery(
    () => true,
    () => api.get<GitProviderStatus>("/git-provider"),
  );
  const [installation, setInstallation] = createSignal("");
  const installations = createQuery(
    () => status.data()?.configured === true,
    () => api.get<GitHubInstallation[]>("/git-provider/installations"),
  );
  const repositories = createQuery(
    () => installation(),
    (id) => api.get<GitHubRepository[]>(`/git-provider/installations/${id}/repositories`),
  );
  const [repository, setRepository] = createSignal("");
  const choose = (value: string) => {
    setRepository(value);
    if (!value) {
      props.onPick(null, "");
      return;
    }
    props.onPick({ installation_id: Number(installation()), repository: value }, githubCloneURL(value));
  };
  return (
    <>
    <ConnectionChoice onPick={props.onPick} />
    <Show when={status.data()?.configured}>
      <div class="grid-2">
        <Select
          label={t("Instalação do GitHub App")}
          placeholder={t("Digitar o endereço")}
          value={installation()}
          onValue={(value) => {
            setInstallation(value);
            choose("");
          }}
          options={(installations.data() ?? []).map((row) => ({ value: String(row.id), label: row.account }))}
        />
        <Select
          label={t("Repositório")}
          placeholder={t("Escolha")}
          disabled={!installation()}
          value={repository()}
          onValue={choose}
          options={(repositories.data() ?? []).map((row) => ({
            value: row.full_name,
            label: row.private ? `${row.full_name} (${t("privado")})` : row.full_name,
          }))}
        />
      </div>
    </Show>
    </>
  );
}

/** Links the created resource to the picked repository, if still the typed one. */
export async function linkPickedRepository(
  kind: "applications" | "stacks",
  id: string,
  pick: GitHubPick | null,
  repository: string,
): Promise<void> {
  if (pickStillApplies(pick, repository)) {
    await api.put(`/${kind}/${id}/${pick.connection_id ? "git-connection" : "git-provider"}`, pick);
  }
}
