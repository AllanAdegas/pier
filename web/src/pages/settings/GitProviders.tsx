import { createSignal, For } from "solid-js";
import { api, errorMessage } from "../../lib/api";
import { t } from "../../i18n";

interface Installation { id: number; account: { login: string } }
interface Repository { id: number; full_name: string }
export function GitProviders() {
 const [appID, setAppID] = createSignal("");
 const [key, setKey] = createSignal("");
 const [secret, setSecret] = createSignal("");
 const [message, setMessage] = createSignal("");
 const [installations, setInstallations] = createSignal<Installation[]>([]);
 const [installation, setInstallation] = createSignal("");
 const [repos, setRepos] = createSignal<Repository[]>([]);
 const [repo, setRepo] = createSignal("");
 const [kind, setKind] = createSignal("application");
 const [resource, setResource] = createSignal("");
 const [busy, setBusy] = createSignal(false);
 async function run(action: () => Promise<unknown>) { setBusy(true); setMessage(""); try { await action(); } catch (err) { setMessage(errorMessage(err)); } finally { setBusy(false); } }
 return <section class="page">
  <h1>{t("GitHub App")}</h1>
  <p>{t("Configure seu GitHub App. Conteúdo: leitura; status de commit e pull requests: escrita. Instale apenas nos repositórios necessários.")}</p>
  <p>{t("Webhook: /webhooks/git-provider/github. Assine eventos de instalação e repositórios da instalação.")}</p>
  <form onSubmit={e => {e.preventDefault(); void run(async () => {await api.put("/git-provider", {app_id: Number(appID()), private_key: key(), webhook_secret: secret()}); setKey(""); setSecret(""); setMessage(t("GitHub App salvo."));});}}>
   <label>{t("App ID")}<input required type="number" min="1" value={appID()} onInput={e=>setAppID(e.currentTarget.value)} /></label>
   <label>{t("Chave privada PEM")}<textarea required autocomplete="off" value={key()} onInput={e=>setKey(e.currentTarget.value)} /></label>
   <label>{t("Segredo do webhook (mínimo 32 caracteres)")}<input required type="password" autocomplete="new-password" minLength={32} value={secret()} onInput={e=>setSecret(e.currentTarget.value)} /></label>
   <button disabled={busy()}>{t("Salvar")}</button>
  </form>
  <button disabled={busy()} onClick={()=>void run(async()=>setInstallations(await api.get<Installation[]>("/git-provider/installations")))}>{t("Listar instalações")}</button>
  <select aria-label={t("Instalação GitHub")} value={installation()} onChange={e=>{setInstallation(e.currentTarget.value);setRepos([]);setRepo("");void run(async()=>setRepos(await api.get<Repository[]>(`/git-provider/installations/${installation()}/repositories`)));}}>
   <option value="">—</option><For each={installations()}>{i=><option value={i.id}>{i.account.login}</option>}</For>
  </select>
  <select aria-label={t("Repositório GitHub")} value={repo()} onChange={e=>setRepo(e.currentTarget.value)}><option value="">—</option><For each={repos()}>{r=><option value={r.full_name}>{r.full_name}</option>}</For></select>
  <select aria-label={t("Tipo de recurso")} value={kind()} onChange={e=>setKind(e.currentTarget.value)}><option value="application">Application</option><option value="stack">Stack</option></select>
  <p>{t("Configure a origem do recurso com a mesma URL HTTPS do GitHub selecionado. A seleção fornece a credencial no deploy e nos previews.")}</p>
  <label>{t("ID do recurso")}<input value={resource()} onInput={e=>setResource(e.currentTarget.value)} /></label>
  <button disabled={busy() || !repo() || !resource()} onClick={()=>void run(async()=>{await api.put(`/git-provider/${kind()}/${encodeURIComponent(resource())}`,{installation_id:Number(installation()),repository:repo()});setMessage(t("Repositório selecionado."));})}>{t("Selecionar repositório")}</button>
  <button disabled={busy() || !resource()} onClick={()=>void run(async()=>{await api.delete(`/git-provider/${kind()}/${encodeURIComponent(resource())}`);setMessage(t("Seleção removida. O próximo deploy usará a autenticação da origem."));})}>{t("Remover seleção")}</button>
  <p role="status">{message()}</p>
 </section>;
}
