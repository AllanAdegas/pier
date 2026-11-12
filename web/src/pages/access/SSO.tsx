import { createSignal, Show } from "solid-js";
import { api, errorMessage } from "../../lib/api";
import { t } from "../../i18n";
import { parseRoleMappings } from "./roleMapping";

export function SSOSettings() {
  const [issuer, setIssuer] = createSignal("");
  const [client, setClient] = createSignal("");
  const [secret, setSecret] = createSignal("");
  const [callback, setCallback] = createSignal("");
  const [subject, setSubject] = createSignal("");
  const [user, setUser] = createSignal("");
  const [roleClaim, setRoleClaim] = createSignal("groups");
  const [roleLines, setRoleLines] = createSignal("");
  const [defaultRole, setDefaultRole] = createSignal("");
  const [message, setMessage] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  async function run(action: () => Promise<unknown>) {
    setBusy(true); setMessage("");
    try { await action(); setMessage(t("Alteração salva.")); }
    catch (error) { setMessage(errorMessage(error)); }
    finally { setBusy(false); }
  }
  return <section><h2>{t("SSO OpenID Connect")}</h2>
    <p>{t("Somente o proprietário da instância configura SSO. Autorize cada identidade pelo subject; emails não criam vínculos.")}</p>
    <Show when={message()}><p role="status">{message()}</p></Show>
    <form onSubmit={e => { e.preventDefault(); void run(async () => {
      const parsed = parseRoleMappings(roleLines());
      if (parsed.invalid.length > 0) throw new Error(t("Linhas de mapeamento inválidas: {lines}", { lines: parsed.invalid.join(", ") }));
      await api.put("/access/oidc", { issuer: issuer(), client_id: client(), client_secret: secret(), callback: callback(),
        role_claim: roleClaim(), role_mappings: parsed.mappings, default_role: defaultRole() });
      setSecret("");
    }); }}>
      <label>{t("Issuer permitido")}<input required type="url" value={issuer()} onInput={e => setIssuer(e.currentTarget.value)} /></label>
      <label>{t("Client ID")}<input required value={client()} onInput={e => setClient(e.currentTarget.value)} /></label>
      <label>{t("Client secret")}<input type="password" autocomplete="new-password" value={secret()} onInput={e => setSecret(e.currentTarget.value)} /></label>
      <label>{t("Callback HTTPS")}<input required type="url" value={callback()} onInput={e => setCallback(e.currentTarget.value)} /></label>
      <p>{t("Papéis pelo SSO: uma linha grupo=papel (admin, developer ou viewer). O SSO nunca concede proprietário. Sem mapeamento, os papéis ficam só no Pier.")}</p>
      <label>{t("Claim dos grupos")}<input value={roleClaim()} onInput={e => setRoleClaim(e.currentTarget.value)} /></label>
      <label>{t("Mapeamento de papéis")}<textarea rows={3} value={roleLines()} onInput={e => setRoleLines(e.currentTarget.value)} /></label>
      <label>{t("Papel padrão do SSO")}<select value={defaultRole()} onChange={e => setDefaultRole(e.currentTarget.value)}>
        <option value="">{t("Nenhum")}</option>
        <option value="viewer">{t("Somente leitura")}</option>
        <option value="developer">{t("Desenvolvedor")}</option>
        <option value="admin">{t("Administrador")}</option>
      </select></label>
      <button disabled={busy()} type="submit">{t("Salvar SSO")}</button>
    </form>
    <form onSubmit={e => { e.preventDefault(); void run(() => api.post("/access/oidc/links", { issuer: issuer(), subject: subject(), user_id: user() })); }}>
      <label>{t("Subject autorizado")}<input required value={subject()} onInput={e => setSubject(e.currentTarget.value)} /></label>
      <label>{t("ID do usuário existente")}<input required value={user()} onInput={e => setUser(e.currentTarget.value)} /></label>
      <button disabled={busy()} type="submit">{t("Autorizar identidade SSO")}</button>
    </form>
    <button disabled={busy()} onClick={() => run(() => api.post("/access/oidc/unlink", { issuer: issuer(), subject: subject() }))}>{t("Revogar identidade SSO")}</button>
    <button disabled={busy()} onClick={() => run(() => api.delete("/access/oidc"))}>{t("Desativar SSO")}</button>
    <a href="/api/v1/access/oidc/start">{t("Entrar com SSO")}</a>
  </section>;
}
