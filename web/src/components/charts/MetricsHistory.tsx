import { createSignal, For, Show } from "solid-js";
import { api } from "../../lib/api";
import { formatBytes } from "../../lib/format";
import { createQuery } from "../../lib/query";
import type { MetricsHistory as History, MetricsSeries } from "../../lib/types";
import { locale, t } from "../../i18n";
import { Button } from "../Button";
import { QueryView } from "../Feedback";
import { LineChart, type ChartLine } from "./LineChart";
import { maxValue } from "./chartPath";
import "../../styles/charts.css";

const RANGES = ["1h", "6h", "24h", "7d", "30d"] as const;
type RangeName = (typeof RANGES)[number];

const percent = (value: number) => `${value.toLocaleString(locale, { maximumFractionDigits: 1 })}%`;
const load = (value: number) => value.toLocaleString(locale, { maximumFractionDigits: 2 });

/** Average line, plus the dashed maximum line when the data has 15-minute resolution. */
function withMax(label: string, color: string, average?: (number | null)[], maximum?: (number | null)[]): ChartLine[] {
  const lines: ChartLine[] = [];
  if (average) lines.push({ label, color, values: average, filled: true });
  if (maximum) lines.push({ label: `${label} (${t("máx.")})`, color: "var(--warning)", values: maximum, dashed: true });
  return lines;
}

function charts(series: MetricsSeries, cpuCeiling?: number) {
  const memoryTotal = maxValue(series.mem_total);
  const diskTotal = maxValue(series.disk_total);
  return [
    { key: "cpu", title: "CPU", lines: withMax("CPU", "var(--accent)", series.cpu, series.cpu_max), format: percent, yMax: cpuCeiling as number | undefined },
    { key: "mem", title: t("Memória"), lines: withMax(t("Memória"), "var(--info)", series.mem_used, series.mem_used_max), format: formatBytes, yMax: memoryTotal || undefined },
    { key: "disk", title: t("Disco (diretório de dados)"), lines: withMax(t("Disco"), "var(--success)", series.disk_used, series.disk_used_max), format: formatBytes, yMax: diskTotal || undefined },
    { key: "load", title: t("Carga média (1 min)"), lines: withMax(t("Carga"), "var(--accent)", series.load, series.load_max), format: load, yMax: undefined },
  ].filter((chart) => chart.lines.length > 0);
}

/**
 * Charts of one server or one application, database or stack.
 * `path` is the history endpoint without the query string; a host's CPU is
 * capped at 100%, a container's can use several cores.
 */
export function MetricsHistory(props: { path: string; hostCpu?: boolean }) {
  const [range, setRange] = createSignal<RangeName>("1h");
  const history = createQuery(
    () => `${props.path}?range=${range()}`,
    (url) => api.get<History>(url),
    { pollMs: 60000 },
  );
  return (
    <div class="stack">
      <div class="row-between monitor-toolbar">
        <h3>{t("Histórico de uso")}</h3>
        <div class="chart-ranges" role="group" aria-label={t("Período")}>
          <For each={RANGES}>{(name) => (
            <Button small aria-pressed={range() === name} onClick={() => setRange(name)}>{name}</Button>
          )}</For>
        </div>
      </div>
      <QueryView query={history} skeletonLines={4}>
        {(data) => (
          <Show when={data().series.t.length > 0} fallback={<p class="small muted">{t("Ainda não há histórico. O Pier coleta uma amostra por minuto de servidores acessíveis.")}</p>}>
            <div class="chart-grid-layout">
              <For each={charts(data().series, props.hostCpu ? 100 : undefined)}>{(chart) => (
                <LineChart title={chart.title} t={data().series.t} lines={chart.lines} stepSeconds={data().step_seconds}
                  from={data().from} to={data().to} format={chart.format} yMax={chart.yMax} />
              )}</For>
            </div>
          </Show>
        )}
      </QueryView>
    </div>
  );
}
