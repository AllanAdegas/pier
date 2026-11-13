import { createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { locale, t } from "../../i18n";
import { buildGeometry, formatTime, nearestPoint, niceCeil, timeTicks, type PlotPoint } from "./chartPath";
import "../../styles/charts.css";

export interface ChartLine {
  label: string;
  /** CSS color, normally a design token such as var(--accent). */
  color: string;
  values: (number | null)[];
  dashed?: boolean;
  /** Fills the area under the line. */
  filled?: boolean;
}

interface LineChartProps {
  title: string;
  t: number[];
  lines: ChartLine[];
  stepSeconds: number;
  from: number;
  to: number;
  format: (value: number) => string;
  /** Fixed top of the scale; by default the next 1-2-5 step above the data. */
  yMax?: number;
}

const HEIGHT = 168;
const MARGIN = { left: 8, right: 8, top: 8, bottom: 22 };

/** Dependency-free SVG line/area chart. Hover, touch and arrow keys read exact values. */
export function LineChart(props: LineChartProps) {
  const [width, setWidth] = createSignal(600);
  const [cursor, setCursor] = createSignal<number>();
  let frame: HTMLDivElement | undefined;
  onMount(() => {
    if (!frame) return;
    setWidth(Math.max(240, frame.clientWidth - 32));
    const observer = new ResizeObserver(() => frame && setWidth(Math.max(240, frame.clientWidth - 32)));
    observer.observe(frame);
    onCleanup(() => observer.disconnect());
  });

  const area = () => ({ left: MARGIN.left, top: MARGIN.top, width: width() - MARGIN.left - MARGIN.right, height: HEIGHT - MARGIN.top - MARGIN.bottom });
  const yMax = () => props.yMax ?? niceCeil(Math.max(0, ...props.lines.flatMap((line) => line.values.filter((v): v is number => typeof v === "number"))));
  const geometries = createMemo(() => props.lines.map((line) => buildGeometry({
    t: props.t, values: line.values, stepSeconds: props.stepSeconds, from: props.from, to: props.to, yMax: yMax(), area: area(),
  })));
  const primary = () => geometries()[0]?.points ?? [];
  const selected = () => primary().find((point) => point.index === cursor()) ?? primary()[primary().length - 1];
  const span = () => props.to - props.from;
  const valuesAt = (point: PlotPoint) => props.lines.map((line) => ({ label: line.label, value: line.values[point.index] }));

  const select = (clientX: number, svg: SVGSVGElement) => {
    const box = svg.getBoundingClientRect();
    const point = nearestPoint(primary(), ((clientX - box.left) / box.width) * width());
    if (point) setCursor(point.index);
  };
  const move = (step: number) => {
    const points = primary();
    const at = points.findIndex((point) => point.index === selected()?.index);
    const next = points[Math.min(points.length - 1, Math.max(0, at + step))];
    if (next) setCursor(next.index);
  };
  const onKey = (event: KeyboardEvent) => {
    const steps: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, PageUp: -10, PageDown: 10 };
    if (event.key in steps) { event.preventDefault(); move(steps[event.key]); }
    else if (event.key === "Home") { event.preventDefault(); move(-primary().length); }
    else if (event.key === "End") { event.preventDefault(); move(primary().length); }
  };
  const summary = () => {
    const last = primary()[primary().length - 1];
    return last ? `${props.title}: ${t("último valor")} ${props.format(last.value)}` : `${props.title}: ${t("sem dados")}`;
  };

  return (
    <figure class="chart" ref={frame}>
      <figcaption class="chart-title">{props.title}</figcaption>
      <svg
        class="chart-svg" viewBox={`0 0 ${width()} ${HEIGHT}`} width={width()} height={HEIGHT} role="img" tabindex="0"
        aria-label={summary()}
        onPointerMove={(event) => select(event.clientX, event.currentTarget)}
        onPointerDown={(event) => select(event.clientX, event.currentTarget)}
        onPointerLeave={() => setCursor(undefined)}
        onKeyDown={onKey} onBlur={() => setCursor(undefined)}
      >
        <title>{props.title}</title>
        <For each={[0, 0.5, 1]}>{(fraction) => (
          <g>
            <line class="chart-grid" x1={area().left} x2={area().left + area().width} y1={area().top + area().height * (1 - fraction)} y2={area().top + area().height * (1 - fraction)} />
          </g>
        )}</For>
        <For each={timeTicks(props.from, props.to, width() < 420 ? 2 : 4)}>{(tick, index) => (
          <text class="chart-axis" y={HEIGHT - 6} x={area().left + area().width * (index() / (width() < 420 ? 1 : 3))}
            text-anchor={index() === 0 ? "start" : index() === (width() < 420 ? 1 : 3) ? "end" : "middle"}>
            {formatTime(tick, span(), locale)}
          </text>
        )}</For>
        <For each={props.lines}>{(line, index) => (
          <g style={{ "--series": line.color }}>
            <Show when={line.filled}><For each={geometries()[index()].areas}>{(d) => <path class="chart-area" d={d} />}</For></Show>
            <For each={geometries()[index()].lines}>{(d) => <path class={`chart-line ${line.dashed ? "chart-dashed" : ""}`} d={d} />}</For>
            <For each={geometries()[index()].dots}>{(dot) => <circle class="chart-dot" cx={dot.x} cy={dot.y} r="2.5" />}</For>
          </g>
        )}</For>
        <Show when={cursor() !== undefined && selected()}>{(point) => (
          <g>
            <line class="chart-cursor" x1={point().x} x2={point().x} y1={area().top} y2={area().top + area().height} />
            <For each={props.lines}>{(line, index) => {
              const hit = geometries()[index()].points.find((candidate) => candidate.index === point().index);
              return <Show when={hit}>{(h) => <circle class="chart-marker" style={{ "--series": line.color }} cx={h().x} cy={h().y} r="3.5" />}</Show>;
            }}</For>
          </g>
        )}</Show>
        <Show when={primary().length === 0}>
          <text class="chart-axis" x={width() / 2} y={HEIGHT / 2} text-anchor="middle">{t("Sem dados neste período")}</text>
        </Show>
      </svg>
      <div class="chart-readout" aria-live="polite">
        <Show when={selected()} fallback={<span class="muted">{t("Sem dados neste período")}</span>}>{(point) => (
          <>
            <time class="muted" dateTime={new Date(point().t * 1000).toISOString()}>{new Date(point().t * 1000).toLocaleString(locale)}</time>
            <For each={valuesAt(point())}>{(entry) => (
              <span class="chart-value"><i style={{ background: props.lines.find((l) => l.label === entry.label)?.color }} aria-hidden="true" />
                {entry.label}: <strong>{typeof entry.value === "number" ? props.format(entry.value) : "—"}</strong></span>
            )}</For>
          </>
        )}</Show>
      </div>
    </figure>
  );
}
