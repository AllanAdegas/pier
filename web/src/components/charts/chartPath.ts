// Pure geometry for the SVG charts: no DOM, so it is unit-tested.

export interface PlotArea {
  left: number;
  top: number;
  width: number;
  height: number;
}

export interface PlotPoint {
  index: number;
  x: number;
  y: number;
  t: number;
  value: number;
}

export interface Geometry {
  /** SVG path data of each continuous run of two or more points. */
  lines: string[];
  /** Closed paths under each line, down to the baseline. */
  areas: string[];
  /** Runs of one point, drawn as dots because a line needs two. */
  dots: PlotPoint[];
  /** Every plotted point, in time order. */
  points: PlotPoint[];
}

export interface GeometryInput {
  t: number[];
  values: (number | null)[];
  /** Expected distance between two points; a longer distance is a gap. */
  stepSeconds: number;
  from: number;
  to: number;
  yMax: number;
  area: PlotArea;
}

const GAP_FACTOR = 2.5;

const round = (value: number) => Math.round(value * 10) / 10;
const isNumber = (value: number | null | undefined): value is number => typeof value === "number" && Number.isFinite(value);

/** Smallest 1, 2, 5 × 10^n that is at least `value`; 100 for non-positive input. */
export function niceCeil(value: number): number {
  if (!isNumber(value) || value <= 0) return 100;
  const magnitude = 10 ** Math.floor(Math.log10(value));
  for (const factor of [1, 2, 5, 10]) {
    if (factor * magnitude >= value) return factor * magnitude;
  }
  return 10 * magnitude;
}

/** Largest finite value of a column, or 0. */
export function maxValue(...columns: ((number | null)[] | undefined)[]): number {
  let max = 0;
  for (const column of columns) {
    for (const value of column ?? []) if (isNumber(value) && value > max) max = value;
  }
  return max;
}

/**
 * Splits a series into continuous runs: a missing value or a hole longer
 * than 2.5 steps ends the run, so the line is never drawn across a gap.
 */
export function buildGeometry(input: GeometryInput): Geometry {
  const { t, values, stepSeconds, from, to, yMax, area } = input;
  const span = Math.max(1, to - from);
  const scaleX = (time: number) => area.left + Math.min(1, Math.max(0, (time - from) / span)) * area.width;
  const scaleY = (value: number) => area.top + area.height - Math.min(1, Math.max(0, value / (yMax > 0 ? yMax : 1))) * area.height;
  const points: PlotPoint[] = [];
  const runs: PlotPoint[][] = [];
  let previous: PlotPoint | undefined;
  for (let index = 0; index < t.length; index++) {
    const value = values[index];
    if (!isNumber(value) || !Number.isFinite(t[index])) {
      previous = undefined;
      continue;
    }
    const point = { index, x: round(scaleX(t[index])), y: round(scaleY(value)), t: t[index], value };
    points.push(point);
    if (!previous || t[index] - previous.t > stepSeconds * GAP_FACTOR) runs.push([]);
    runs[runs.length - 1].push(point);
    previous = point;
  }
  const baseline = round(area.top + area.height);
  const lines: string[] = [];
  const areas: string[] = [];
  const dots: PlotPoint[] = [];
  for (const run of runs) {
    if (run.length === 1) {
      dots.push(run[0]);
      continue;
    }
    const line = run.map((point, position) => `${position === 0 ? "M" : "L"}${point.x},${point.y}`).join("");
    lines.push(line);
    areas.push(`${line}L${run[run.length - 1].x},${baseline}L${run[0].x},${baseline}Z`);
  }
  return { lines, areas, dots, points };
}

/** The plotted point closest to an x coordinate, or undefined when there is none. */
export function nearestPoint(points: PlotPoint[], x: number): PlotPoint | undefined {
  let best: PlotPoint | undefined;
  for (const point of points) {
    if (!best || Math.abs(point.x - x) < Math.abs(best.x - x)) best = point;
  }
  return best;
}

/** Evenly spaced timestamps for the x axis labels, including both ends. */
export function timeTicks(from: number, to: number, count: number): number[] {
  if (count < 2) return [from];
  return Array.from({ length: count }, (_, index) => Math.round(from + ((to - from) * index) / (count - 1)));
}

/** Time of day for windows up to a day, short date and time beyond. */
export function formatTime(seconds: number, spanSeconds: number, locale: string): string {
  const date = new Date(seconds * 1000);
  const options: Intl.DateTimeFormatOptions = spanSeconds <= 86400
    ? { hour: "2-digit", minute: "2-digit" }
    : { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" };
  return new Intl.DateTimeFormat(locale, options).format(date);
}
