import { createSignal, For, Show } from "solid-js";
import { useParams } from "@solidjs/router";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { t } from "../../i18n";
import { formatDateTime } from "../../lib/format";
import type { Project } from "../../lib/types";
import { Button } from "../../components/Button";
import { EmptyState, QueryView } from "../../components/Feedback";
import { Select, TextInput } from "../../components/Form";
import { CopyField, PageHeader, Section } from "../../components/Page";
import { grantableRoles, type Role } from "./can";
import { accessSummary, allowed } from "./permissions";
import { roleLabels } from "./roleLabels";

type Member = { user_id: string; email: string; role: Role };
type Invite = { id: string; email: string; role: Role; expires_at: number; consumed_at: number | null; revoked_at: number | null };

/** Who can act on one project, and invitations to it. */
export function TeamPage() {
  const params = useParams();
  const base = () => `/projects/${encodeURIComponent(params.id ?? "")}`;
  const project = createQuery(() => params.id, () => api.get<Project>(base()));
  const members = createQuery(() => params.id, () => api.get<Member[]>(base() + "/members"));
  // Managing members needs admin or owner on the project; the API enforces it.
  const manages = () => allowed("write", params.id);
  const grantable = () => grantableRoles(accessSummary(), params.id ?? "");
  const options = (roles: Role[]) => roles.map((role) => ({ value: role, label: roleLabels()[role] }));
  const changeRole = createAction((member: Member, role: string) =>
    api.put(`${base()}/members/${encodeURIComponent(member.user_id)}`, { role }), {
    success: t("Alteração salva."),
    onSuccess: () => void members.refetch(),
  });

  return (
    <div class="page">
      <PageHeader
        crumbs={[
          { label: t("Projetos"), href: "/projects" },
          { label: project.data()?.name ?? "…", href: base() },
          { label: t("Equipe") },
        ]}
        title={t("Equipe e permissões")}
        description={t("Permissões valem somente para este projeto. Convites expiram em 24 horas.")}
      />
      <Section title={t("Membros do projeto")}>
        <QueryView query={members}>
          {(list) => (
            <Show
              when={list().length > 0}
              fallback={<EmptyState icon="shield" title={t("Nenhum membro explícito neste projeto.")}>
                {t("Papéis na instância continuam valendo aqui. Convide alguém para dar acesso só a este projeto.")}
              </EmptyState>}
            >
              <div class="list">
                <For each={list()}>
                  {(member) => (
                    <div class="list-item">
                      <span class="grow truncate">{member.email}</span>
                      <Show when={manages() && grantable().includes(member.role)} fallback={<span class="tag">{roleLabels()[member.role]}</span>}>
                        <div class="inline-select">
                          <Select
                            label={t("Papel do membro")}
                            value={member.role}
                            disabled={changeRole.pending()}
                            options={options(grantable())}
                            onValue={(role) => void changeRole.run(member, role)}
                          />
                        </div>
                        <Button small variant="ghost" pending={changeRole.pending()} onClick={() => void changeRole.run(member, "")}>
                          {t("Remover acesso")}
                        </Button>
                      </Show>
                    </div>
                  )}
                </For>
              </div>
            </Show>
          )}
        </QueryView>
      </Section>
      <Show when={manages()}>
        <InviteSection base={base()} roles={options(grantable().filter((role) => role !== "owner"))} />
      </Show>
    </div>
  );
}

function InviteSection(props: { base: string; roles: { value: string; label: string }[] }) {
  const invites = createQuery(() => props.base, () => api.get<Invite[]>(props.base + "/invites"));
  const [email, setEmail] = createSignal("");
  const [role, setRole] = createSignal("viewer");
  const [token, setToken] = createSignal("");
  const create = createAction(() => api.post<{ token: string }>(props.base + "/invites", { email: email(), role: role() }), {
    onSuccess: (result) => {
      setToken(result.token);
      setEmail("");
      void invites.refetch();
    },
  });
  const revoke = createAction((id: string) => api.delete(`${props.base}/invites/${encodeURIComponent(id)}`), {
    success: t("Convite revogado."),
    onSuccess: () => void invites.refetch(),
  });
  const pending = () => (invites.data() ?? []).filter((invite) => !invite.revoked_at && !invite.consumed_at);

  return (
    <Section title={t("Convidar para o projeto")} description={t("A pessoa aceita o convite em Segurança, com o token gerado aqui.")}>
      <form class="stack" onSubmit={(event) => { event.preventDefault(); void create.run(); }}>
        <div class="grid-2">
          <TextInput label={t("Email do convidado")} type="email" required value={email()} onValue={setEmail}
            error={create.errors().email} />
          <Select label={t("Papel do convite")} value={role()} onValue={setRole} options={props.roles} />
        </div>
        <div class="form-actions">
          <Button type="submit" variant="primary" icon="plus" pending={create.pending()}>{t("Criar convite")}</Button>
        </div>
      </form>
      <Show when={token()}>
        <div class="stack-sm">
          <CopyField label={t("Token do convite")} value={token()} secret hint={t("Copie o token agora. Ele aparece somente nesta resposta.")} />
          <div><Button small variant="ghost" onClick={() => setToken("")}>{t("Ocultar token")}</Button></div>
        </div>
      </Show>
      <Show when={pending().length > 0}>
        <h3 class="section-title">{t("Convites pendentes")}</h3>
        <div class="list">
          <For each={pending()}>
            {(invite) => (
              <div class="list-item">
                <div class="grow stack-xs">
                  <strong class="truncate">{invite.email}</strong>
                  <span class="small muted">
                    {roleLabelOf(invite.role)} · {t("expira em {date}", { date: formatDateTime(new Date(invite.expires_at * 1000).toISOString()) })}
                  </span>
                </div>
                <Button small variant="ghost" pending={revoke.pending()} onClick={() => void revoke.run(invite.id)}>
                  {t("Revogar convite")}
                </Button>
              </div>
            )}
          </For>
        </div>
      </Show>
    </Section>
  );
}

function roleLabelOf(role: Role): string {
  return roleLabels()[role];
}
