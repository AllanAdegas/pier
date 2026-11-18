import { createSignal, For, Show } from "solid-js";
import { Button } from "../components/Button";
import { Alert, EmptyState } from "../components/Feedback";
import { Select } from "../components/Form";
import { PageHeader, Section } from "../components/Page";
import { api, errorMessage } from "../lib/api";
import { createQuery } from "../lib/query";
import type { Server } from "../lib/types";
import { t } from "../i18n";
import { allowed } from "./access/permissions";
import { InfrastructureTabs } from "./Clusters";

type Cluster = { id: string; name: string; nodes: { server_id: string; server_name: string; role: string }[] };
type Service = { name: string; image: string; replicas: string };
type Node = { node_id: string; hostname: string; status: string; availability: string };
export function SwarmPage() {
  const clusters = createQuery(() => true, () => api.get<Cluster[]>("/swarm"));
  const servers = createQuery(() => allowed("write"), () => api.get<Server[]>("/servers"));
  const [selected, select] = createSignal("");
  const [manager, setManager] = createSignal("");
  const [worker, setWorker] = createSignal("");
  const [removingWorker, setRemovingWorker] = createSignal("");
  const current = () => clusters.data()?.find(cluster => cluster.id === selected());
  const options = () => (servers.data() ?? []).map(server => ({ value: server.id, label: server.name }));
  const services = createQuery(() => selected() || false, id => api.get<Service[]>(`/swarm/${id}/services`));
  const nodes = createQuery(() => selected() || false, id => api.get<Node[]>(`/swarm/${id}/nodes`));
  const [error, setError] = createSignal("");
  const [logs, setLogs] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  async function run(fn: () => Promise<unknown>) {
    setBusy(true); setError("");
    try { await fn(); await Promise.all([clusters.refetch(), services.refetch(), nodes.refetch()]); }
    catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  }
  function value(form: HTMLFormElement, name: string) { return String(new FormData(form).get(name) ?? ""); }
  return <div class="page">
    <PageHeader title="Docker Swarm" description={t("Crie um Swarm em servidores cadastrados e implante imagens acessíveis a todos os nós.")} />
    <InfrastructureTabs current="swarm" />
    <Show when={error() || clusters.error() || servers.error()}><Alert tone="danger">{error() || errorMessage(clusters.error() || servers.error())}</Alert></Show>
    <Show when={busy() || clusters.loading()}><p role="status">{t("Carregando")}</p></Show>
    <Show when={!clusters.loading() && !clusters.error()}>
      <Show when={clusters.data()?.length} fallback={<EmptyState icon="cluster" title={t("Nenhum Swarm")} />}>
        <Select label={t("Swarm")} value={selected()} placeholder={t("Selecione um Swarm")} disabled={busy()}
          options={(clusters.data() ?? []).map(cluster => ({ value: cluster.id, label: cluster.name }))}
          onValue={id => { select(id); setWorker(""); setRemovingWorker(""); setLogs(""); setError(""); }} />
      </Show>
    </Show>
    <Show when={allowed("write")}>
      <Section title={t("Criar Swarm")}>
        <form class="stack" onSubmit={e => {
          e.preventDefault();
          const f = e.currentTarget;
          void run(async () => {
            const cluster = await api.post<Cluster>("/swarm", { name: value(f, "name"), manager_server_id: manager(), advertise_addr: value(f, "address") });
            f.reset();
            setManager("");
            setLogs("");
            select(cluster.id);
          });
        }}>
          <div class="grid-2">
            <label class="field"><span class="field-label">{t("Nome")}</span><input class="input" name="name" required maxlength={60} /></label>
            <Select label={t("Servidor manager")} value={manager()} onValue={setManager} options={options()} placeholder={t("Escolha")} disabled={servers.loading()} />
          </div>
          <label class="field">
            <span class="field-label">{t("IP anunciado (opcional)")}</span>
            <input class="input" name="address" aria-describedby="swarm-manager-address-hint" />
            <span class="field-hint" id="swarm-manager-address-hint">{t("Em branco, o Pier usa o endereço do servidor cadastrado.")}</span>
          </label>
          <div class="row"><Button type="submit" variant="primary" disabled={!manager() || busy()}>{t("Criar Swarm")}</Button></div>
        </form>
      </Section>
    </Show>
    <Show when={selected()}>
      <Show when={services.error() || nodes.error()}><Alert tone="danger">{errorMessage(services.error() || nodes.error())}</Alert></Show>
      <Show when={!busy() && (services.loading() || nodes.loading())}><p role="status">{t("Carregando")}</p></Show>
      <Show when={allowed("write")}>
        <Section title={t("Adicionar worker")}>
          <form class="stack" onSubmit={e => {
            e.preventDefault();
            const f = e.currentTarget;
            void run(async () => {
              await api.post(`/swarm/${selected()}/nodes`, { server_id: worker(), role: "worker", advertise_addr: value(f, "address") });
              f.reset();
              setWorker("");
            });
          }}>
            <div class="grid-2">
              <Select label={t("Servidor worker")} value={worker()} onValue={setWorker}
                options={options().filter(option => !current()?.nodes?.some(node => node.server_id === option.value))}
                placeholder={t("Escolha")} disabled={servers.loading()} />
              <label class="field"><span class="field-label">{t("IP anunciado (opcional)")}</span><input class="input" name="address" /></label>
            </div>
            <div class="row"><Button type="submit" disabled={!worker() || busy()}>{t("Adicionar worker")}</Button></div>
          </form>
        </Section>
      </Show>
      <h2>{t("Nós Swarm")}</h2>
      <For each={nodes.data()}>{n => <p>{n.hostname} · {n.status} · {n.availability} <Show when={allowed("write")}><Button disabled={busy()} onClick={() => void run(() => api.post(`/swarm/${selected()}/nodes/${n.node_id}/availability`,{availability:n.availability === "drain" ? "active":"drain"}))}>{t("Alternar drenagem")}</Button></Show></p>}</For>
      <Show when={allowed("deploy")}>
      <form onSubmit={e => { e.preventDefault(); const f=e.currentTarget; void run(() => api.post(`/swarm/${selected()}/services`,{name:value(f,"name"),image:value(f,"image"),replicas:Number(value(f,"replicas"))})); }}>
        <label>{t("Nome")}<input name="name" required/></label><label>{t("Imagem")}<input name="image" required/></label>
        <label>{t("Réplicas")}<input name="replicas" type="number" min="0" max="100" value="1" required/></label>
        <button disabled={busy()}>{t("Implantar serviço")}</button>
      </form>
      </Show>
      <For each={services.data()}>{s => <section><h3>{s.name}</h3><p>{s.image} · {s.replicas}</p>
        <Show when={allowed("deploy")}>
        <form onSubmit={e => {e.preventDefault(); const f=e.currentTarget; void run(() => api.post(`/swarm/${selected()}/services/${s.name}`,{action:"scale",replicas:Number(value(f,"replicas"))}));}}><label>{t("Réplicas")}<input name="replicas" type="number" min="0" max="100" required/></label><button disabled={busy()}>{t("Escalar serviço")}</button></form>
        <button disabled={busy()} onClick={() => void run(() => api.post(`/swarm/${selected()}/services/${s.name}`,{action:"restart"}))}>{t("Reiniciar")}</button>
        <button disabled={busy()} onClick={() => void run(() => api.post(`/swarm/${selected()}/services/${s.name}`,{action:"rollback"}))}>{t("Rollback")}</button>
        <button disabled={busy()} onClick={() => {if(confirm(t("Remover este serviço e suas tarefas?"))) void run(() => api.post(`/swarm/${selected()}/services/${s.name}`,{action:"remove"}));}}>{t("Remover serviço")}</button>
        </Show>
        <Button disabled={busy()} onClick={() => void run(async () => { const v=await api.get<{logs:string}>(`/swarm/${selected()}/services/${s.name}/logs`); setLogs(v.logs); })}>{t("Logs")}</Button>
      </section>}</For>
      <Show when={allowed("write")}>
        <Section title={t("Remover worker")}>
          <form class="stack" onSubmit={e => {
            e.preventDefault();
            const f = e.currentTarget;
            void run(async () => {
              await api.post(`/swarm/${selected()}/nodes/${encodeURIComponent(removingWorker())}/remove`, { confirmation: value(f, "confirmation") });
              f.reset();
              setRemovingWorker("");
            });
          }}>
            <div class="grid-2">
              <Select label={t("Servidor worker")} value={removingWorker()} onValue={setRemovingWorker} placeholder={t("Escolha")}
                options={(current()?.nodes ?? []).filter(node => node.role === "worker").map(node => ({ value: node.server_id, label: node.server_name || node.server_id }))} />
              <label class="field"><span class="field-label">{t("Nome do Swarm para confirmar")}</span><input class="input" name="confirmation" required /></label>
            </div>
            <div class="row"><Button type="submit" variant="danger" disabled={!removingWorker() || busy()}>{t("Remover worker")}</Button></div>
          </form>
        </Section>
        <Section title={t("Excluir Swarm vazio")}>
          <form class="stack" onSubmit={e => { e.preventDefault(); const f = e.currentTarget; void run(async () => { await api.post(`/swarm/${selected()}/delete`, { confirmation: value(f, "confirmation") }); select(""); }); }}>
            <label class="field"><span class="field-label">{t("Nome do Swarm para confirmar")}</span><input class="input" name="confirmation" required /></label>
            <div class="row"><Button type="submit" variant="danger" disabled={busy()}>{t("Excluir Swarm vazio")}</Button></div>
          </form>
        </Section>
      </Show>
      <pre>{logs()}</pre>
    </Show>
  </div>;
}
