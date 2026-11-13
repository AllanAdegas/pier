import { createMemo, createSignal, For, Show } from "solid-js";
import { Button } from "./Button";
import { Alert, QueryView, StatusBadge } from "./Feedback";
import { TextInput } from "./Form";
import { MetricsHistory } from "./charts/MetricsHistory";
import { containerStatus, formatBytes } from "../lib/format";
import { createMonitoringQuery } from "../lib/monitoring";
import type { ServerMetrics } from "../lib/types";
import { locale, t } from "../i18n";
import "../styles/monitoring.css";

const available = (value: number | null | undefined): value is number => typeof value === "number" && Number.isFinite(value);
const bytes = (value: number | null | undefined) => available(value) ? formatBytes(value) : t("Indisponível");

function Metric(props: { label: string; value: string; detail?: string; used?: number | null; total?: number | null }) {
  const percent = () => available(props.used) && available(props.total) && props.total > 0
    ? Math.max(0, Math.min(100, props.used / props.total * 100)) : undefined;
  return (
    <div class="monitor-metric stack-sm">
      <span class="small muted">{props.label}</span>
      <strong class="monitor-value">{props.value}</strong>
      <Show when={props.detail}><span class="small muted">{props.detail}</span></Show>
      <Show when={percent() !== undefined}>
        <div class="meter" role="meter" aria-label={props.label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent()}>
          <span style={{ width: `${percent()}%`, background: percent()! >= 90 ? "var(--danger)" : undefined }} />
        </div>
      </Show>
    </div>
  );
}

export function ServerMonitoring(props: { serverId: string }) {
  const metrics = createMonitoringQuery<ServerMetrics>(() => `/servers/${props.serverId}/metrics`);
  const [search, setSearch] = createSignal("");
  const containers = createMemo(() => (metrics.data()?.containers ?? []).filter((container) =>
    `${container.name} ${container.resource_type} ${container.state}`.toLowerCase().includes(search().toLowerCase()),
  ));
  const collected = () => Date.parse(metrics.data()?.collected_at ?? "");
  const stale = () => !!metrics.error() || !Number.isFinite(collected()) || metrics.now() - collected() > 45000;
  return (
    <div class="stack">
      <div class="row-between monitor-toolbar">
        <div class="row small muted">
          <Show when={metrics.data()}>
            <StatusBadge status={{ label: stale() ? t("Dados desatualizados") : t("Atualizado"), tone: stale() ? "warning" : "success" }} />
            <Show when={Number.isFinite(collected())} fallback={<span>{t("Horário de coleta indisponível")}</span>}>
              <span>{t("Coletado em")} <time dateTime={metrics.data()?.collected_at}>{new Date(collected()).toLocaleString(locale)}</time></span>
            </Show>
          </Show>
        </div>
        <Button small icon="refresh" pending={metrics.pending()} onClick={() => void metrics.refetch()}>{t("Atualizar")}</Button>
      </div>
      <Show when={metrics.error() && metrics.data()}>
        <Alert tone="warning">{t("Falha na atualização. A última leitura permanece visível.")}</Alert>
      </Show>
      <QueryView query={metrics}>
        {(data) => (
          <div class="stack">
            <Show when={data().host_error}><Alert tone="warning">{t("Métricas do host indisponíveis")}: {data().host_error}</Alert></Show>
            <Show when={data().disk_error}><Alert tone="warning">{t("Falha na leitura do disco")}: {data().disk_error}</Alert></Show>
            <div class="monitor-grid">
              <Metric label="CPU" value={available(data().cpu_percent) ? `${data().cpu_percent!.toLocaleString(locale, { maximumFractionDigits: 1 })}%` : t("Indisponível")} used={data().cpu_percent} total={100} detail={t("{count} CPUs", { count: data().cpus })} />
              <Metric label={t("RAM utilizada")} value={bytes(data().memory_used_bytes)} used={data().memory_used_bytes} total={data().memory_total_bytes}
                detail={t("Total: {total} · Disponível: {free}", { total: data().memory_total_bytes > 0 ? bytes(data().memory_total_bytes) : t("Indisponível"), free: bytes(data().memory_available_bytes) })} />
              <Metric label={t("Disco (diretório de dados)")} value={data().disk_error || data().disk_total_bytes <= 0 ? t("Indisponível") : bytes(data().disk_used_bytes)} used={data().disk_error ? null : data().disk_used_bytes} total={data().disk_total_bytes}
                detail={t("Total: {total} · Disponível: {free}", { total: data().disk_error || data().disk_total_bytes <= 0 ? t("Indisponível") : bytes(data().disk_total_bytes), free: data().disk_error ? t("Indisponível") : bytes(data().disk_available_bytes) })} />
              <Metric label={t("Swap utilizada")} value={bytes(data().swap_used_bytes)} used={data().swap_used_bytes} total={data().swap_total_bytes} detail={t("Total: {total}", { total: bytes(data().swap_total_bytes) })} />
              <Metric label={t("Tempo ativo")} value={available(data().uptime_seconds) ? t("{days}d {hours}h {minutes}min", { days: Math.floor(data().uptime_seconds! / 86400), hours: Math.floor(data().uptime_seconds! % 86400 / 3600), minutes: Math.floor(data().uptime_seconds! % 3600 / 60) }) : t("Indisponível")} />
              <Metric label={t("Carga média (1 / 5 / 15 min)")} value={data().load_average?.length === 3 ? data().load_average!.map((value) => value.toLocaleString(locale, { maximumFractionDigits: 2 })).join(" / ") : t("Indisponível")} />
            </div>
            <p class="small muted">{data().operating_system} · {data().architecture} · {t("Imagens")}: {data().images}</p>
            <Show when={data().host_error}><p class="small muted">{t("Sem acesso ao host, o total de RAM pode refletir a capacidade do Docker.")}</p></Show>
            <div class="monitor-counts">
              <strong>{t("Docker · todos os containers")}</strong>
              <span>{t("Total")}: {data().containers_total}</span>
              <span>{t("Em execução")}: {data().containers_running}</span>
              <span>{t("Parados")}: {data().containers_stopped}</span>
              <span>{t("Pausados")}: {data().containers_paused}</span>
            </div>
            <div class="stack-sm">
              <h3>{t("Containers gerenciados")}</h3>
              <p class="small muted">{t("Somente containers gerenciados pelo Pier. Containers parados não têm leitura de consumo.")}</p>
              <TextInput label={t("Buscar container")} value={search()} onValue={setSearch} />
              <Show when={containers().length > 0} fallback={<p class="muted">{t("Nenhum container encontrado.")}</p>}>
                <div class="table-wrap">
                  <table class="table">
                    <thead><tr><th>{t("Container")}</th><th>{t("Tipo")}</th><th>{t("Estado")}</th><th>CPU</th><th>RAM</th></tr></thead>
                    <tbody><For each={containers()}>{(container) => (
                      <tr>
                        <td class="mono">{container.name}</td><td>{container.resource_type}</td>
                        <td><StatusBadge status={containerStatus(container)} /></td>
                        <td>{container.cpu_percent?.trim() || "—"}</td>
                        <td>{container.memory_usage?.trim() || "—"}<Show when={container.memory_usage?.trim() && container.memory_percent?.trim()}> ({container.memory_percent})</Show></td>
                      </tr>
                    )}</For></tbody>
                  </table>
                </div>
              </Show>
            </div>
          </div>
        )}
      </QueryView>
      <MetricsHistory path={`/servers/${props.serverId}/metrics/history`} hostCpu />
    </div>
  );
}
