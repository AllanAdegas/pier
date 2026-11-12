import { createSignal, For, Show } from "solid-js";
import { Button } from "../../components/Button";
import { TextInput, Select } from "../../components/Form";
import { QueryView } from "../../components/Feedback";
import { Section } from "../../components/Page";
import { api } from "../../lib/api";
import { createQuery } from "../../lib/query";
import { formatDateTime } from "../../lib/format";
import { t } from "../../i18n";
interface AuditEntry {id:number;occurred_at:string;user_id:string;user_name:string;token_id:string;token_name:string;ip:string;method:string;route:string;resource_id:string;status:number;summary:string}
export function AuditSection() {
 const [user,setUser]=createSignal("");const [resource,setResource]=createSignal("");const [method,setMethod]=createSignal("");const [since,setSince]=createSignal("");
 const [filter,setFilter]=createSignal("");const [before,setBefore]=createSignal(0);
 const entries=createQuery(()=>filter()+`&before=${before() || ""}`,q=>api.get<AuditEntry[]>(`/audit?limit=50${q}`));
 function apply(){const q=new URLSearchParams();if(user())q.set("user_id",user());if(resource())q.set("resource_id",resource());if(method())q.set("method",method());if(since())q.set("since",new Date(since()).toISOString());setBefore(0);setFilter("&"+q.toString());}
 return <Section title={t("Auditoria")} description={t("Alterações e leituras sensíveis dos últimos 90 dias. Segredos são ocultados.")}>
 <form class="stack" onSubmit={(e)=>{e.preventDefault();apply();}}>
 <div class="grid-2"><TextInput label={t("ID do usuário")} value={user()} onValue={setUser}/><TextInput label={t("ID do recurso")} value={resource()} onValue={setResource}/>
 <Select label={t("Método HTTP")} value={method()} onValue={setMethod} options={[{value:"",label:t("Todos")},...['GET','POST','PATCH','PUT','DELETE'].map(v=>({value:v,label:v}))]}/>
 <TextInput label={t("A partir de")} type="datetime-local" value={since()} onValue={setSince}/></div>
 <div class="form-actions"><Button type="submit">{t("Filtrar")}</Button></div>
 </form>
 <QueryView query={entries}>{list=><div class="stack"><Show when={list().length>0} fallback={<p class="muted">{t("Nenhum registro de auditoria encontrado.")}</p>}>
 <For each={list()}>{entry=><details class="card"><summary><span class="mono">{entry.status} {entry.method} {entry.route}</span><br/>{formatDateTime(entry.occurred_at)} · {entry.user_name || "—"}{entry.token_name?` · ${entry.token_name}`:""} · {entry.ip}</summary>
 <p class="small mono">{entry.resource_id}</p><pre style={{"white-space":"pre-wrap","overflow-wrap":"anywhere"}}>{entry.summary}</pre></details>}</For>
 </Show><div class="row"><Button small onClick={()=>setBefore(0)}>{t("Mais recentes")}</Button><Show when={list().length===50}><Button small onClick={()=>setBefore(list()[list().length-1].id)}>{t("Mais antigos")}</Button></Show></div></div>}</QueryView>
 </Section>;
}
