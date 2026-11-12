import { TextInput } from "../components/Form";
import { A } from "@solidjs/router";
import { createEffect, createSignal, For, Show } from "solid-js";
import { Button } from "../components/Button";
import { Alert, EmptyState, QueryView, StatusBadge } from "../components/Feedback";
import { PageHeader, Section } from "../components/Page";
import { ServerMonitoring } from "../components/ServerMonitoring";
import { api } from "../lib/api";
import { clusterStatus } from "../lib/format";
import { createMonitoringQuery } from "../lib/monitoring";
import { createAction, createQuery } from "../lib/query";
import type { Cluster, ClusterOverview, Server } from "../lib/types";
import { locale, t } from "../i18n";
import { InfrastructureTabs } from "./Clusters";

function ClusterMonitoring(props: { cluster: Cluster; server?: Server }) {
  const overview = createMonitoringQuery<ClusterOverview>(() => `/clusters/${props.cluster.id}/overview`);
  const stale = () => !!overview.error() || overview.now() - overview.receivedAt() > 45000;
  return (
    <Section title={props.cluster.name} actions={<A href={`/clusters/${props.cluster.id}`}>{t("Abrir cluster")}</A>}>
      <Show when={props.cluster.server_id}>
        <p class="small muted">{t("Servidor associado")}: <A href={`/servers/${props.cluster.server_id}`}>{props.server?.name || t("Abrir servidor")}</A></p>
      </Show>
      <div class="row-between monitor-toolbar">
        <div class="row">
          <StatusBadge status={clusterStatus(props.cluster.status)} />
          <Show when={overview.data()}>
            <StatusBadge status={{ label: stale() ? t("Dados desatualizados") : t("Atualizado"), tone: stale() ? "warning" : "success" }} />
            <span class="small muted">{t("Recebido em")} <time dateTime={new Date(overview.receivedAt()).toISOString()}>{new Date(overview.receivedAt()).toLocaleString(locale)}</time></span>
          </Show>
        </div>
        <Button small icon="refresh" pending={overview.pending()} onClick={() => void overview.refetch()}>{t("Atualizar")}</Button>
      </div>
      <Show when={overview.error() && overview.data()}><Alert tone="warning">{t("Falha na atualização. A última leitura permanece visível.")}</Alert></Show>
      <QueryView query={overview}>
        {(data) => (
          <div class="stack">
            <div class="monitor-grid">
              <div class="monitor-metric stack-sm"><span class="small muted">{t("Nós prontos / total")}</span><strong class="monitor-value">{data().nodes.filter((node) => node.health === "ok").length} / {data().nodes.length}</strong></div>
              <div class="monitor-metric stack-sm"><span class="small muted">{t("Pods saudáveis / total")}</span><strong class="monitor-value">{data().pods_running} / {data().pods}</strong></div>
              <div class="monitor-metric stack-sm"><span class="small muted">{t("Avisos recentes")}</span><strong class="monitor-value">{data().events.length}</strong></div>
            </div>
            <p class="small muted">{t("O resumo conta pods prontos e concluídos como saudáveis; não informa Ready separadamente.")}</p>
            <Show when={data().nodes.some((node) => node.health !== "ok") || data().pods_running < data().pods}>
              <Alert tone="warning">{t("Há nós não prontos ou pods não saudáveis. Abra o cluster para investigar.")}</Alert>
            </Show>
            <Show when={data().events.length > 0} fallback={<p class="small muted">{t("Nenhum aviso recente.")}</p>}>
              <ul class="stack-sm"><For each={data().events.slice(0, 5)}>{(event) => <li><strong>{event.namespace ? `${event.namespace}/` : ""}{event.name}</strong> · {event.info || event.status}</li>}</For></ul>
            </Show>
          </div>
        )}
      </QueryView>
    </Section>
  );
}

export function MonitoringPage() {
  const servers = createQuery(() => true, () => api.get<Server[]>("/servers"), { pollMs: 15000 });
  const clusters = createQuery(() => true, () => api.get<Cluster[]>("/clusters"), { pollMs: 15000 });
  return (
    <div class="page">
      <PageHeader title={t("Monitoramento")} description={t("Leituras reais por servidor, atualizadas a cada 15 segundos enquanto a página está visível.")} actions={<A href="/servers" class="btn">{t("Gerenciar servidores")}</A>} />
      <InfrastructureTabs current="monitoring" />
      <RuntimeMonitoring />
      <section class="stack-lg monitor-servers" aria-label={t("Servidores Docker")}>
        <div class="row-between"><h2>{t("Servidores Docker")}</h2><Button small icon="refresh" onClick={() => void servers.refetch()}>{t("Atualizar lista")}</Button></div>
        <Show when={servers.error() && servers.data()}><Alert tone="warning">{t("Não foi possível atualizar a lista de servidores.")}</Alert></Show>
        <QueryView query={servers}>
          {(list) => <Show when={list().length > 0} fallback={<EmptyState icon="server" title={t("Nenhum servidor")} action={<A href="/servers">{t("Adicionar servidor")}</A>} />}>
            <For each={list().map((server) => server.id)}>{(id) => {
              const server = () => list().find((item) => item.id === id)!;
              return <Section title={server().name} description={server().kind === "local" ? t("Máquina local") : `${server().user}@${server().host}`} actions={<A href={`/servers/${id}?tab=usage`}>{t("Abrir servidor")}</A>}><ServerMonitoring serverId={id} /></Section>;
            }}</For>
          </Show>}
        </QueryView>
      </section>
      <section class="stack-lg" aria-label={t("Clusters Kubernetes")}>
        <div class="row-between"><h2>{t("Clusters Kubernetes")}</h2><Button small icon="refresh" onClick={() => void clusters.refetch()}>{t("Atualizar lista")}</Button></div>
        <Show when={clusters.error() && clusters.data()}><Alert tone="warning">{t("Não foi possível atualizar a lista de clusters.")}</Alert></Show>
        <QueryView query={clusters}>
          {(list) => <Show when={list().length > 0} fallback={<EmptyState icon="cluster" title={t("Nenhum cluster")} action={<A href="/clusters">{t("Adicionar cluster")}</A>} />}><For each={list().map((cluster) => cluster.id)}>{(id) => {
            const cluster = () => list().find((item) => item.id === id)!;
            return <ClusterMonitoring cluster={cluster()} server={servers.data()?.find((server) => server.id === cluster().server_id)} />;
          }}</For></Show>}
        </QueryView>
      </section>
    </div>
  );
}

interface UptimeCheck {
  id: string; resource: string; url: string; expected_status: number;
  interval_seconds: number; enabled: boolean; last_checked: string;
}
interface UptimeResult { checked_at: string; status: number; healthy: boolean }

function UptimeHistory(props: { check: UptimeCheck }) {
  const history = createQuery(() => props.check.id, () => api.get<UptimeResult[]>(`/monitoring/uptime/${props.check.id}/history`), { pollMs: 60000 });
  return <Section title={props.check.url} description={props.check.resource}>
    <QueryView query={history}>{(rows) => <>
      <p>{rows().length ? (rows()[0].healthy ? t("Disponível") : t("Fora do ar")) : t("Aguardando primeira verificação")}</p>
      <details><summary>{t("Histórico dos últimos 7 dias")}</summary>
        <ul class="stack-sm"><For each={rows()}>{(row) => <li><time>{new Date(row.checked_at).toLocaleString(locale)}</time> · {row.status || "—"} · {row.healthy ? t("Disponível") : t("Fora do ar")}</li>}</For></ul>
      </details>
    </>}</QueryView>
  </Section>;
}

function AlertThresholds() {
  const options = createQuery(() => true, () => api.get<{ disk: number; memory: number; cpu: number; minutes: number }>("/monitoring/options"));
  const [disk, setDisk] = createSignal("90");
  const [memory, setMemory] = createSignal("90");
  const [cpu, setCPU] = createSignal("95");
  const [minutes, setMinutes] = createSignal("10");
  createEffect(() => { const value = options.data(); if (value) { setDisk(String(value.disk)); setMemory(String(value.memory)); setCPU(String(value.cpu)); setMinutes(String(value.minutes)); } });
  const save = createAction(() => api.patch("/monitoring/options", { disk: Number(disk()), memory: Number(memory()), cpu: Number(cpu()), minutes: Number(minutes()) }), { onSuccess: () => void options.refetch() });
  return <Section title={t("Limites de alertas do servidor")}>
    <QueryView query={options}>{() => <form class="stack" onSubmit={(event) => { event.preventDefault(); void save.run(); }}>
      <TextInput label={t("Disco (%)")} type="number" required value={disk()} onValue={setDisk} />
      <TextInput label={t("Memória (%)")} type="number" required value={memory()} onValue={setMemory} />
      <TextInput label={t("CPU (%)")} type="number" required value={cpu()} onValue={setCPU} />
      <TextInput label={t("Duração em minutos")} type="number" required value={minutes()} onValue={setMinutes} />
      <Button type="submit" pending={save.pending()}>{t("Salvar limites")}</Button>
    </form>}</QueryView>
  </Section>;
}

function RuntimeMonitoring() {
  const checks = createQuery(() => true, () => api.get<UptimeCheck[]>("/monitoring/uptime"), { pollMs: 60000 });
  const [resource, setResource] = createSignal("");
  const [url, setURL] = createSignal("");
  const [status, setStatus] = createSignal("200");
  const [interval, setInterval] = createSignal("60");
  const [silenceResource, setSilenceResource] = createSignal("");
  const [until, setUntil] = createSignal("");
  const save = createAction(() => api.post("/monitoring/uptime", { resource: resource(), url: url(), expected_status: Number(status()), interval_seconds: Number(interval()), enabled: true }), { onSuccess: () => void checks.refetch() });
  const silence = createAction(() => api.post("/monitoring/silences", { resource: silenceResource(), until_at: new Date(until()).toISOString() }));
  const remove = createAction((id: string) => api.delete(`/monitoring/uptime/${id}`), { onSuccess: () => void checks.refetch() });
  return <section class="stack-lg">
    <AlertThresholds />
    <Section title={t("Verificações de URL")} description={t("Verifique uma rota de aplicação ou stack a cada 60 segundos ou mais.")}>
      <form class="stack" onSubmit={(event) => { event.preventDefault(); void save.run(); }}>
        <TextInput label={t("ID do recurso")} required value={resource()} onValue={setResource} />
        <TextInput label="URL" type="url" required value={url()} onValue={setURL} />
        <TextInput label={t("Status HTTP esperado")} type="number" required value={status()} onValue={setStatus} />
        <TextInput label={t("Intervalo em segundos")} type="number" required value={interval()} onValue={setInterval} />
        <Button type="submit" pending={save.pending()}>{t("Adicionar verificação")}</Button>
      </form>
    </Section>
    <QueryView query={checks}>{(list) => <For each={list()}>{(check) => <div class="stack"><UptimeHistory check={check} /><Button pending={remove.pending()} onClick={() => void remove.run(check.id)}>{t("Remover verificação")}</Button></div>}</For>}</QueryView>
    <Section title={t("Silenciar durante manutenção")} description={t("Deixe o recurso vazio para silenciar todos os alertas até o horário informado.")}>
      <form class="stack" onSubmit={(event) => { event.preventDefault(); void silence.run(); }}>
        <TextInput label={t("ID do recurso")} value={silenceResource()} onValue={setSilenceResource} />
        <TextInput label={t("Silenciar até")} type="datetime-local" required value={until()} onValue={setUntil} />
        <Button type="submit" pending={silence.pending()}>{t("Silenciar alertas")}</Button>
      </form>
    </Section>
  </section>;
}
