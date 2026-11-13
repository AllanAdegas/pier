/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { buildGeometry, formatTime, maxValue, nearestPoint, niceCeil, timeTicks } from "./chartPath.ts";

const area = { left: 0, top: 0, width: 100, height: 100 };
const base = { stepSeconds: 60, from: 0, to: 600, yMax: 100, area };

Deno.test("a continuous series is one line with an area closed on the baseline", () => {
  const g = buildGeometry({ ...base, t: [0, 60, 120], values: [0, 50, 100] });
  assertEquals(g.lines, ["M0,100L10,50L20,0"]);
  assertEquals(g.areas, ["M0,100L10,50L20,0L20,100L0,100Z"]);
  assertEquals(g.points.length, 3);
});

Deno.test("a hole longer than 2.5 steps splits the line instead of bridging it", () => {
  const g = buildGeometry({ ...base, t: [0, 60, 120, 420, 480], values: [10, 10, 10, 20, 20] });
  assertEquals(g.lines.length, 2);
  assertEquals(g.lines[0], "M0,90L10,90L20,90");
  assertEquals(g.lines[1], "M70,80L80,80");
});

Deno.test("a missing value breaks the line and a lone point becomes a dot", () => {
  const g = buildGeometry({ ...base, t: [0, 60, 120, 180, 240], values: [10, null, 30, 40, null] });
  assertEquals(g.lines.length, 1);
  assertEquals(g.dots.map((d) => d.t), [0]);
  assertEquals(g.points.map((p) => p.t), [0, 120, 180]);
});

Deno.test("a gap of exactly 2.5 steps is still connected, one more second is not", () => {
  assertEquals(buildGeometry({ ...base, t: [0, 150], values: [1, 1] }).lines.length, 1);
  assertEquals(buildGeometry({ ...base, t: [0, 151], values: [1, 1] }).lines.length, 0);
});

Deno.test("values outside the scale are clamped and non-finite values ignored", () => {
  const g = buildGeometry({ ...base, t: [0, 60, 120], values: [150, NaN, -5] });
  assertEquals(g.points.map((p) => p.y), [0, 100]);
  assertEquals(buildGeometry({ ...base, t: [], values: [] }), { lines: [], areas: [], dots: [], points: [] });
});

Deno.test("nice ceilings, maxima and nearest point", () => {
  assertEquals([niceCeil(0), niceCeil(0.3), niceCeil(7), niceCeil(130), niceCeil(2100)], [100, 0.5, 10, 200, 5000]);
  assertEquals(maxValue([1, null, 5], undefined, [3]), 5);
  const g = buildGeometry({ ...base, t: [0, 60, 120], values: [1, 2, 3] });
  assertEquals(nearestPoint(g.points, 11)?.t, 60);
  assertEquals(nearestPoint([], 5), undefined);
});

Deno.test("time ticks include both ends and labels adapt to the window", () => {
  assertEquals(timeTicks(0, 100, 3), [0, 50, 100]);
  assertEquals(timeTicks(5, 9, 1), [5]);
  const day = formatTime(1_700_000_000, 3600, "en-US");
  const week = formatTime(1_700_000_000, 7 * 86400, "en-US");
  assertEquals(day.includes("/"), false);
  assertEquals(week.includes("/"), true);
});
