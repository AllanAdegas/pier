import { createResource, createSignal, For, Show } from "solid-js";
import { api, errorMessage } from "../lib/api";
import { t } from "../i18n";

type Cluster = { id: string; name: string; nodes: { server_id: string; server_name: string; role: string }[] };
type Service = { name: string; image: string; replicas: string };
type Node = { node_id: string; hostname: string; status: string; availability: string };
export function SwarmPage() {
  const [clusters, { refetch }] = createResource(() => api.get<Cluster[]>("/swarm"));
  const [selected, select] = createSignal("");
  const [services, { refetch: refreshServices }] = createResource(selected, id => api.get<Service[]>(`/swarm/${id}/services`));
  const [nodes, { refetch: refreshNodes }] = createResource(selected, id => api.get<Node[]>(`/swarm/${id}/nodes`));
  const [error, setError] = createSignal("");
  const [logs, setLogs] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  async function run(fn: () => Promise<unknown>) {
    setBusy(true); setError("");
    try { await fn(); await refetch(); if (selected()) { await refreshServices(); await refreshNodes(); } }
    catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  }
  function value(form: HTMLFormElement, name: string) { return String(new FormData(form).get(name) ?? ""); }
  return <main>
    <h1>Docker Swarm</h1>
    <p>{t("Crie um Swarm em servidores cadastrados e implante imagens acessíveis a todos os nós.")}</p>
    <Show when={error() || clusters.error}><p role="alert">{error() || errorMessage(clusters.error)}</p></Show>
    <form onSubmit={e => { e.preventDefault(); const f = e.currentTarget; void run(() => api.post("/swarm", { name: value(f,"name"), manager_server_id: value(f,"manager"), advertise_addr: value(f,"address") })); }}>
      <label>{t("Nome")}<input name="name" required maxlength={60}/></label>
      <label>{t("ID do servidor manager")}<input name="manager" required/></label>
      <label>{t("IP anunciado")}<input name="address" required/></label>
      <button disabled={busy()}>{t("Criar Swarm")}</button>
    </form>
    <label>{t("Swarm")}<select value={selected()} onChange={e => { select(e.currentTarget.value); setLogs(""); }}><option value="">{t("Selecione um Swarm")}</option><For each={clusters()}>{c => <option value={c.id}>{c.name}</option>}</For></select></label>
    <Show when={selected()}>
      <Show when={services.error || nodes.error}><p role="alert">{errorMessage(services.error || nodes.error)}</p></Show>
      <form onSubmit={e => { e.preventDefault(); const f=e.currentTarget; void run(() => api.post(`/swarm/${selected()}/nodes`,{server_id:value(f,"server"),role:"worker",advertise_addr:value(f,"address")})); }}>
        <label>{t("ID do servidor worker")}<input name="server" required/></label>
        <label>{t("IP anunciado")}<input name="address"/></label>
        <button disabled={busy()}>{t("Adicionar worker")}</button>
      </form>
      <h2>{t("Nós Swarm")}</h2>
      <For each={nodes()}>{n => <p>{n.hostname} · {n.status} · {n.availability} <button disabled={busy()} onClick={() => void run(() => api.post(`/swarm/${selected()}/nodes/${n.node_id}/availability`,{availability:n.availability === "drain" ? "active":"drain"}))}>{t("Alternar drenagem")}</button></p>}</For>
      <form onSubmit={e => { e.preventDefault(); const f=e.currentTarget; void run(() => api.post(`/swarm/${selected()}/services`,{name:value(f,"name"),image:value(f,"image"),replicas:Number(value(f,"replicas"))})); }}>
        <label>{t("Nome")}<input name="name" required/></label><label>{t("Imagem")}<input name="image" required/></label>
        <label>{t("Réplicas")}<input name="replicas" type="number" min="0" max="100" value="1" required/></label>
        <button disabled={busy()}>{t("Implantar serviço")}</button>
      </form>
      <For each={services()}>{s => <section><h3>{s.name}</h3><p>{s.image} · {s.replicas}</p>
        <form onSubmit={e => {e.preventDefault(); const f=e.currentTarget; void run(() => api.post(`/swarm/${selected()}/services/${s.name}`,{action:"scale",replicas:Number(value(f,"replicas"))}));}}><label>{t("Réplicas")}<input name="replicas" type="number" min="0" max="100" required/></label><button disabled={busy()}>{t("Escalar serviço")}</button></form>
        <button disabled={busy()} onClick={() => void run(() => api.post(`/swarm/${selected()}/services/${s.name}`,{action:"restart"}))}>{t("Reiniciar")}</button>
        <button disabled={busy()} onClick={() => void run(() => api.post(`/swarm/${selected()}/services/${s.name}`,{action:"rollback"}))}>{t("Rollback")}</button>
        <button disabled={busy()} onClick={() => void run(async () => { const v=await api.get<{logs:string}>(`/swarm/${selected()}/services/${s.name}/logs`); setLogs(v.logs); })}>{t("Logs")}</button>
        <button disabled={busy()} onClick={() => {if(confirm(t("Remover este serviço e suas tarefas?"))) void run(() => api.post(`/swarm/${selected()}/services/${s.name}`,{action:"remove"}));}}>{t("Remover serviço")}</button>
      </section>}</For>
      <form onSubmit={e => {e.preventDefault(); const f=e.currentTarget; void run(() => api.post(`/swarm/${selected()}/nodes/${encodeURIComponent(value(f,"server"))}/remove`,{confirmation:value(f,"confirmation")}));}}>
        <label>{t("ID do servidor worker")}<input name="server" required/></label><label>{t("Nome do Swarm para confirmar")}<input name="confirmation" required/></label><button disabled={busy()}>{t("Remover worker")}</button>
      </form>
      <form onSubmit={e => {e.preventDefault(); const f=e.currentTarget; void run(async () => {await api.post(`/swarm/${selected()}/delete`,{confirmation:value(f,"confirmation")});select("");});}}><label>{t("Nome do Swarm para confirmar")}<input name="confirmation" required/></label><button disabled={busy()}>{t("Excluir Swarm vazio")}</button></form>
      <pre>{logs()}</pre>
    </Show>
  </main>;
}
