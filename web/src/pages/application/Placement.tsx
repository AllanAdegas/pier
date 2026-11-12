import { For, createSignal } from "solid-js";
import { api } from "../../lib/api";
import { createQuery, createAction } from "../../lib/query";
import { QueryView } from "../../components/Feedback";
import { TextInput } from "../../components/Form";
import { Button } from "../../components/Button";
import { t } from "../../i18n";
type Placement = { build_server_id: string; registry_id: string; repository: string; destinations: string[] };
type Status = { server_id: string; image: string; status: string };
export function PlacementTab(props: { applicationId: string }) {
 const [credential, setCredential] = createSignal({id:"",host:"",username:"",password:""});
 const registrySave = createAction(async () => { await api.put("/registries", credential()); setCredential({...credential(),password:""}); }, { success: t("Configuração salva.") });
 const base = () => `/applications/${props.applicationId}/placement`;
 const settings = createQuery(base, b => api.get<Placement>(b));
 const status = createQuery(base, b => api.get<Status[]>(`${b}/status`), { pollMs: 5000 });
 const save = createAction(() => api.put(base(), settings.data()), { success: t("Configuração salva.") });
 const change = (key: keyof Placement, value: string | string[]) => settings.set({ ...settings.data()!, [key]: value });
 return <div class="stack"><p>{t("Cada servidor usa seu próprio proxy e TLS. Configure DNS ou balanceador externo para distribuir o tráfego.")}</p>
 <p>{t("Os próximos deploys usarão os destinos configurados. Exige proxy direto e portas sem mapeamento no host.")}</p>
 <div class="card stack">
 <TextInput label={t("ID do registro")} value={credential().id} onValue={v => setCredential({...credential(),id:v})} />
 <TextInput label={t("Host do registro")} value={credential().host} onValue={v => setCredential({...credential(),host:v})} />
 <TextInput label={t("Usuário do registro")} value={credential().username} onValue={v => setCredential({...credential(),username:v})} />
 <TextInput label={t("Senha do registro")} type="password" value={credential().password} onValue={v => setCredential({...credential(),password:v})} />
 <Button pending={registrySave.pending()} onClick={() => void registrySave.run()}>{t("Salvar registro")}</Button>
 </div><QueryView query={settings}>{p => <div class="card stack">
 <TextInput label={t("Servidor de build")} value={p().build_server_id} onValue={v => change("build_server_id", v)} />
 <TextInput label={t("Registro de imagens")} value={p().registry_id} onValue={v => change("registry_id", v)} />
 <TextInput label={t("Repositório de imagens")} value={p().repository} onValue={v => change("repository", v)} />
 <TextInput label={t("Destinos (IDs separados por vírgula)")} value={p().destinations.join(",")} onValue={v => change("destinations", v.split(",").map(x => x.trim()).filter(Boolean))} />
 <Button pending={save.pending()} onClick={() => void save.run()}>{t("Salvar")}</Button>
 </div>}</QueryView><QueryView query={status}>{rows => <For each={rows()}>{row => <div class="card">{row.server_id} · {row.status} · {row.image}</div>}</For>}</QueryView></div>;
}
