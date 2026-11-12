import { For, Show } from "solid-js";
import { A } from "@solidjs/router";
import { api } from "../../lib/api";
import { createQuery, createAction } from "../../lib/query";
import { Button } from "../../components/Button";
import { Checkbox, TextInput } from "../../components/Form";
import { EnvEditor } from "../../components/EnvEditor";
import { QueryView } from "../../components/Feedback";
import { t } from "../../i18n";

type Settings = { enabled: boolean; domain_template: string; max_previews: number; allow_forks: boolean; ttl_days: number };
type Preview = { id: string; number: number; branch: string; commit_sha: string; url: string; status: string; last_deployment_id: string; updated_at: string };
export function PreviewsTab(props: { applicationId: string }) {
 const base = () => `/applications/${props.applicationId}/previews`;
 const settings = createQuery(base, b => api.get<Settings>(`${b}/settings`));
 const previews = createQuery(base, b => api.get<Preview[]>(b), { pollMs: 5000 });
 const save = createAction(() => api.patch<Settings>(`${base()}/settings`, settings.data()), { success: t("Configuração salva.") });
 const change = <K extends keyof Settings>(key: K, value: Settings[K]) => settings.set({ ...settings.data()!, [key]: value });
 const operate = createAction(async (input: { id: string; remove: boolean }) => {
  if (input.remove) await api.delete(`${base()}/${input.id}`); else await api.post(`${base()}/${input.id}/deploy`);
 }, { success: t("Operação concluída.") });
 return <div class="stack">
 <QueryView query={settings}>{s => <div class="card stack">
 <Checkbox label={t("Ativar previews de pull requests")} checked={s().enabled} onChange={v => change("enabled", v)} />
 <TextInput label={t("Modelo de domínio")} value={s().domain_template} placeholder="pr-{number}.preview.example.com" onValue={v => change("domain_template", v)} />
 <TextInput label={t("Máximo de previews")} type="number" min="1" max="20" value={s().max_previews} onValue={v => change("max_previews", Number(v))} />
 <TextInput label={t("Dias sem atividade até remover")} type="number" min="1" max="90" value={s().ttl_days} onValue={v => change("ttl_days", Number(v))} />
 <Checkbox label={t("Permitir pull requests de forks")} checked={s().allow_forks} onChange={v => change("allow_forks", v)} />
 <p>{t("Forks executam código de terceiros no servidor. Ative apenas se confiar nos autores. As variáveis exclusivas dos previews também ficam disponíveis para esse código.")}</p>
 <Button pending={save.pending()} onClick={() => void save.run()}>{t("Salvar")}</Button>
 </div>}</QueryView>
 <EnvEditor basePath={base()} allowBuildTime note={t("Variáveis exclusivas dos previews substituem as da aplicação. Forks recebem somente estas variáveis.")} />
 <QueryView query={previews}>{rows => <div class="stack"><For each={rows()}>{p => <div class="card stack">
 <strong>#{p.number} · {p.branch}</strong><span>{p.status} · {p.commit_sha.slice(0, 8)} · {p.updated_at}</span>
 <a href={p.url} target="_blank" rel="noreferrer">{p.url}</a>
 <Show when={p.last_deployment_id}><A href={`/deployments/${p.last_deployment_id}`}>{t("Ver deploy")}</A></Show>
 <div class="row"><Button pending={operate.pending()} onClick={() => void operate.run({id:p.id,remove:false})}>{t("Reimplantar preview")}</Button>
 <Button pending={operate.pending()} onClick={() => void operate.run({id:p.id,remove:true})}>{t("Remover preview")}</Button></div>
 </div>}</For></div>}</QueryView>
 </div>;
}
