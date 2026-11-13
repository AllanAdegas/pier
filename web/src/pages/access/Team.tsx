import { SSOSettings } from "./SSO";
import { InstanceRoles } from "./InstanceRoles";
import { createResource, createSignal, For, Show } from "solid-js";
import { useParams } from "@solidjs/router";
import { api, errorMessage } from "../../lib/api";
import { t } from "../../i18n";
import { grantableRoles, type Role } from "./can";
import { accessSummary, allowed, refreshAccess } from "./permissions";

type Member = { user_id: string; email: string; role: Role };
type Invite = { id: string; email: string; role: Role; expires_at: number; consumed_at: number | null; revoked_at: number | null };
export function TeamPage() {
  const params = useParams();
  const base = () => `/projects/${encodeURIComponent(params.id ?? "")}`;
  const [members, memberActions] = createResource(() => params.id, () => api.get<Member[]>(base() + "/members"));
  // Managing members needs admin or owner on the project; the API enforces it.
  const manages = () => allowed("write", params.id);
  const grantable = () => grantableRoles(accessSummary(), params.id ?? "");
  const [invites, inviteActions] = createResource(() => manages() && params.id, () => api.get<Invite[]>(base() + "/invites"));
  const [email, setEmail] = createSignal("");
  const [role, setRole] = createSignal<Role>("viewer");
  const [acceptToken, setAcceptToken] = createSignal("");
  const [token, setToken] = createSignal("");
  const [message, setMessage] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  async function run(action: () => Promise<unknown>) {
    setBusy(true); setMessage("");
    try { await action(); await Promise.all([memberActions.refetch(), inviteActions.refetch()]); }
    catch (error) { setMessage(errorMessage(error)); }
    finally { setBusy(false); }
  }
  const labels: Record<Role, string> = { owner: t("Proprietário"), admin: t("Administrador"), developer: t("Desenvolvedor"), viewer: t("Somente leitura") };
  return <main>
    <h1>{t("Equipe e permissões")}</h1>
    <p>{t("Permissões valem somente para este projeto. Convites expiram em 24 horas.")}</p>
    <Show when={message()}><p role="alert">{message()}</p></Show>
    <Show when={members.loading || invites.loading}><p aria-live="polite">{t("Carregando equipe…")}</p></Show>
    <Show when={members.error || invites.error}><p role="alert">{errorMessage(members.error || invites.error)}</p><button onClick={() => run(async () => {})}>{t("Tentar novamente")}</button></Show>
    <section><h2>{t("Membros do projeto")}</h2>
      <For each={members()} fallback={<p>{t("Nenhum membro explícito neste projeto.")}</p>}>{member => <div>
        <span>{member.email}</span>
        <Show when={manages() && grantable().includes(member.role)} fallback={<span>{labels[member.role]}</span>}>
          <select aria-label={t("Papel do membro")} value={member.role} disabled={busy()} onChange={e => { const value = e.currentTarget.value; void run(() => api.put(`${base()}/members/${encodeURIComponent(member.user_id)}`, { role: value })); }}>
            <For each={grantable()}>{r => <option value={r}>{labels[r]}</option>}</For>
          </select>
          <button disabled={busy()} onClick={() => run(() => api.put(`${base()}/members/${encodeURIComponent(member.user_id)}`, { role: "" }))}>{t("Remover acesso")}</button>
        </Show>
      </div>}</For>
    </section>
    <Show when={manages()}><section><h2>{t("Convidar para o projeto")}</h2>
      <form onSubmit={e => { e.preventDefault(); void run(async () => { const result = await api.post<{ token: string }>(base() + "/invites", { email: email(), role: role() }); setToken(result.token); setEmail(""); }); }}>
        <label>{t("Email do convidado")}<input type="email" required value={email()} onInput={e => setEmail(e.currentTarget.value)} /></label>
        <label>{t("Papel do convite")}<select value={role()} onChange={e => setRole(e.currentTarget.value as Role)}><For each={grantable().filter(r => r !== "owner")}>{r => <option value={r}>{labels[r]}</option>}</For></select></label>
        <button disabled={busy()} type="submit">{t("Criar convite")}</button>
      </form>
      <Show when={token()}><p>{t("Copie o token agora. Ele aparece somente nesta resposta.")}</p><output>{token()}</output><button onClick={() => setToken("")}>{t("Ocultar token")}</button></Show>
      <For each={invites()}>{invite => <div><span>{invite.email} · {labels[invite.role]} · {new Date(invite.expires_at * 1000).toLocaleString()}</span><button disabled={busy() || !!invite.revoked_at || !!invite.consumed_at} onClick={() => run(() => api.delete(`${base()}/invites/${encodeURIComponent(invite.id)}`))}>{t("Revogar convite")}</button></div>}</For>
    </section></Show>
    <section><h2>{t("Aceitar convite")}</h2><form onSubmit={e => { e.preventDefault(); void run(async () => { await api.post("/access/accept", { token: acceptToken() }); setAcceptToken(""); await refreshAccess(); }); }}>
      <label>{t("Token do convite")}<input type="password" required value={acceptToken()} onInput={e => setAcceptToken(e.currentTarget.value)} /></label>
      <button disabled={busy()} type="submit">{t("Aceitar convite")}</button>
    </form></section>
    <Show when={allowed("write", undefined, "owner")}>
      <InstanceRoles />
      <SSOSettings />
    </Show>
  </main>;
}
