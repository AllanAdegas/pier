import { createSignal, Show } from "solid-js";
import { api, errorMessage } from "../../lib/api";
import { t } from "../../i18n";

export function SSOSettings() {
  const [issuer, setIssuer] = createSignal("");
  const [client, setClient] = createSignal("");
  const [secret, setSecret] = createSignal("");
  const [callback, setCallback] = createSignal("");
  const [subject, setSubject] = createSignal("");
  const [user, setUser] = createSignal("");
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
    <form onSubmit={e => { e.preventDefault(); void run(async () => { await api.put("/access/oidc", { issuer: issuer(), client_id: client(), client_secret: secret(), callback: callback() }); setSecret(""); }); }}>
      <label>{t("Issuer permitido")}<input required type="url" value={issuer()} onInput={e => setIssuer(e.currentTarget.value)} /></label>
      <label>{t("Client ID")}<input required value={client()} onInput={e => setClient(e.currentTarget.value)} /></label>
      <label>{t("Client secret")}<input type="password" autocomplete="new-password" value={secret()} onInput={e => setSecret(e.currentTarget.value)} /></label>
      <label>{t("Callback HTTPS")}<input required type="url" value={callback()} onInput={e => setCallback(e.currentTarget.value)} /></label>
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
