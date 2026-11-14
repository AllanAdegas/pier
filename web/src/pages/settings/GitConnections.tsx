import type { GitHubPick } from "./gitLinks";
import { createSignal, For, Show } from "solid-js";
import { Button } from "../../components/Button";
import { QueryView } from "../../components/Feedback";
import { Select, TextInput } from "../../components/Form";
import { Section } from "../../components/Page";
import { api } from "../../lib/api";
import { createAction, createQuery } from "../../lib/query";
import { t } from "../../i18n";
import { allowed } from "../access/permissions";

type Connection = { id: string; name: string; provider: string };
type Repository = { path: string; clone_url: string; default_branch: string };
type Page<T> = { items: T[]; has_more: boolean };

export function GitConnections() {
 const rows = createQuery(() => true, () => api.get<Connection[]>("/git-connections"));
 const [provider, setProvider] = createSignal("gitlab");
 const [name, setName] = createSignal("");
 const [base, setBase] = createSignal("");
 const [workspace, setWorkspace] = createSignal("");
 const [token, setToken] = createSignal("");
 const save = createAction(() => api.post("/git-connections", {provider:provider(),name:name(),base_url:base(),workspace:workspace(),token:token()}), {onSuccess: () => {setToken(""); void rows.refetch();}});
 const rotate = createAction((id:string) => api.put(`/git-connections/${id}`, {token:token()}), {onSuccess: () => {setToken("");void rows.refetch();}});
 const remove = createAction((id:string) => api.delete(`/git-connections/${id}`), {onSuccess: () => void rows.refetch()});
 return <Section title={t("Conexões Git")}>
 <QueryView query={rows}>{data => <For each={data()}>{row => <div class="row-between"><span>{row.name} ({row.provider})</span><Show when={allowed("write")}><Button disabled={!token()} pending={rotate.pending()} onClick={() => void rotate.run(row.id)}>{t("Substituir token")}</Button><Button pending={remove.pending()} onClick={() => void remove.run(row.id)}>{t("Remover conexão")}</Button></Show></div>}</For>}</QueryView>
 <Show when={allowed("write")}><div class="stack">
 <Select label={t("Provedor")} value={provider()} onValue={value => {setProvider(value);setBase("");setWorkspace("");}} options={[{value:"gitlab",label:"GitLab"},{value:"gitea",label:"Gitea / Forgejo"},{value:"bitbucket",label:"Bitbucket Cloud"}]} />
 <TextInput label={t("Nome da conexão")} value={name()} onValue={setName} />
 <TextInput label={t("Endereço do provedor")} value={base()} onValue={setBase} />
 <Show when={provider()==="bitbucket"}><TextInput label="Workspace" value={workspace()} onValue={setWorkspace} /></Show>
 <TextInput label={t("Token de acesso")} type="password" value={token()} onValue={setToken} autocomplete="new-password" />
 <p>{t("Use api no GitLab; leitura de usuário e escrita de repositório no Gitea; leitura de repositório e escrita de webhook no Bitbucket. O token fica criptografado no servidor.")}</p>
 <Button pending={save.pending()} onClick={() => void save.run()}>{t("Salvar conexão")}</Button>
 </div></Show></Section>;
}

export function ConnectionRepositoryPicker(props: {kind: "applications" | "stacks"; id:string}) {
 const rows = createQuery(() => allowed("write"), () => api.get<Connection[]>("/git-connections"));
 const [connection,setConnection]=createSignal("");
 const [query,setQuery]=createSignal("");
 const [page,setPage]=createSignal(1);
 const [repository,setRepository]=createSignal("");
 const repositories=createQuery(() => connection() && `${connection()}?q=${encodeURIComponent(query())}&page=${page()}`, key => {const [id,q]=key.split("?");return api.get<Page<Repository>>(`/git-connections/${id}/repositories?${q}`);});
 const [branchPage,setBranchPage]=createSignal(1);
 const branches=createQuery(() => connection() && repository() && `${connection()}?repository=${encodeURIComponent(repository())}&page=${branchPage()}`, key => {const [id,q]=key.split("?");return api.get<Page<string>>(`/git-connections/${id}/branches?${q}`);});
 const base=()=>`/${props.kind}/${props.id}/git-connection`;
 const link=createQuery(base,path=>api.get<{linked:boolean;repository:string;webhook:string}>(path));
 const save=createAction(()=>api.put(base(),{connection_id:connection(),repository:repository()}),{onSuccess:()=>void link.refetch()});
 const remove=createAction(()=>api.delete(base()),{onSuccess:()=>void link.refetch()});
 return <Section title={t("Conexões Git")}>
 <QueryView query={link}>{data=><Show when={data().linked}><p>{data().repository}: {webhookLabel(data().webhook)}</p></Show>}</QueryView>
 <Show when={allowed("write")}><div class="stack">
 <Select label={t("Conexão Git")} value={connection()} onValue={v=>{setConnection(v);setRepository("");setPage(1);}} options={(rows.data()??[]).map(row=>({value:row.id,label:row.name}))} />
 <TextInput label={t("Buscar repositório")} value={query()} onValue={v=>{setQuery(v);setPage(1);setRepository("");}} />
 <Show when={connection()}><QueryView query={repositories}>{data=><><Select label={t("Repositório")} value={repository()} onValue={v=>{setRepository(v);setBranchPage(1);}} options={data().items.map(row=>({value:row.path,label:row.path}))} /><p class="mono">{data().items.find(row=>row.path===repository())?.clone_url}</p><Button disabled={page()===1} onClick={()=>{setPage(page()-1);setRepository("");}}>{t("Anterior")}</Button><Button disabled={!data().has_more} onClick={()=>{setPage(page()+1);setRepository("");}}>{t("Próxima")}</Button></>}</QueryView></Show>
 <Show when={repository()}><QueryView query={branches}>{data=><><p>{t("Branches disponíveis")}: {data().items.join(", ")}</p><Button disabled={branchPage()===1} onClick={()=>setBranchPage(branchPage()-1)}>{t("Anterior")}</Button><Button disabled={!data().has_more} onClick={()=>setBranchPage(branchPage()+1)}>{t("Próxima")}</Button></>}</QueryView></Show>
 <p>{t("A origem deve usar o endereço de clone escolhido. Sem permissão para webhook, configure-o manualmente na aba de webhook.")}</p>
 <Button disabled={!repository()} pending={save.pending()} onClick={()=>void save.run()}>{t("Vincular repositório")}</Button>
 <Button pending={remove.pending()} onClick={()=>void remove.run()}>{t("Desvincular")}</Button>
 </div></Show></Section>;
}

function webhookLabel(state: string): string {
 switch(state) {
 case "active": return t("Webhook ativo");
 case "manual": return t("Configure o webhook manualmente");
 case "failed": return t("Falha no webhook; vincule novamente para tentar");
 default: return t("Webhook pendente");
 }
}

export function ConnectionChoice(props: {onPick: (pick: GitHubPick | null, cloneURL: string) => void}) {
 const rows=createQuery(()=>allowed("write"),()=>api.get<Connection[]>("/git-connections"));
 const [connection,setConnection]=createSignal("");
 const [query,setQuery]=createSignal("");
 const [page,setPage]=createSignal(1);
 const [selected,setSelected]=createSignal("");
 const repositories=createQuery(()=>connection() && `${connection()}?q=${encodeURIComponent(query())}&page=${page()}`,key=>{const [id,q]=key.split("?");return api.get<Page<Repository>>(`/git-connections/${id}/repositories?${q}`);});
 const clear=()=>{setSelected("");props.onPick(null,"");};
 return <Show when={(rows.data()?.length??0)>0}><div class="stack">
 <Select label={t("Conexão Git")} value={connection()} onValue={value=>{setConnection(value);setPage(1);clear();}} options={(rows.data()??[]).map(row=>({value:row.id,label:row.name}))} />
 <Show when={connection()}>
 <TextInput label={t("Buscar repositório")} value={query()} onValue={value=>{setQuery(value);setPage(1);clear();}} />
 <QueryView query={repositories}>{data=><>
 <Select label={t("Repositório")} value={selected()} onValue={value=>{setSelected(value);const row=data().items.find(row=>row.path===value);props.onPick(row?{installation_id:0,connection_id:connection(),repository:row.path,clone_url:row.clone_url}:null,row?.clone_url??"");}} options={data().items.map(row=>({value:row.path,label:row.path}))} />
 <Button disabled={page()===1} onClick={()=>{setPage(page()-1);clear();}}>{t("Anterior")}</Button>
 <Button disabled={!data().has_more} onClick={()=>{setPage(page()+1);clear();}}>{t("Próxima")}</Button>
 </>}</QueryView></Show>
 </div></Show>;
}
