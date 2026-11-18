/// <reference lib="deno.ns" />
import { assertAlmostEquals, assertEquals } from "jsr:@std/assert@1";
import { color, contrast, tokens, type RGB } from "./contrast.ts";

const css = Deno.readTextFileSync(new URL("./tokens.css", import.meta.url));
const lightStart = css.indexOf(':root[data-theme="light"]');
const dark = tokens(css.slice(0, lightStart));
const themes: Record<string, Record<string, string>> = { dark, light: { ...dark, ...tokens(css.slice(lightStart)) } };

const TEXT = 4.5; // WCAG 1.4.3, normal text
const UI = 3; // WCAG 1.4.11, control boundaries and focus indicators
const surfaces = ["bg", "surface", "surface-raised", "surface-sunken"];

/** Every pair the interface draws, with the minimum ratio it must meet. */
function pairs(theme: Record<string, string>): [string, RGB, RGB, number][] {
  const c = (name: string) => color(theme[name]);
  const out: [string, RGB, RGB, number][] = [];
  for (const fg of ["text", "text-muted", "accent", "success", "warning", "danger", "info"]) {
    for (const bg of surfaces) out.push([`${fg} on ${bg}`, c(fg), c(bg), TEXT]);
  }
  for (const bg of ["bg", "surface", "surface-raised"]) out.push([`text-faint on ${bg}`, c("text-faint"), c(bg), TEXT]);
  for (const tone of ["accent", "success", "warning", "danger", "info"]) {
    const soft = color(theme[`${tone}-soft`], c("surface"));
    out.push([`${tone} on ${tone}-soft`, c(tone), soft, TEXT], [`text on ${tone}-soft`, c("text"), soft, TEXT]);
  }
  out.push(["on-accent on accent", c("on-accent"), c("accent"), TEXT]);
  out.push(["on-accent on accent-hover", c("on-accent"), c("accent-hover"), TEXT]);
  out.push(["white on danger-solid", [255, 255, 255], c("danger-solid"), TEXT]);
  for (const bg of surfaces) out.push([`border-strong on ${bg}`, c("border-strong"), c(bg), UI]);
  for (const bg of surfaces) out.push([`accent (focus) on ${bg}`, c("accent"), c(bg), UI]);
  return out;
}

for (const [name, theme] of Object.entries(themes)) {
  Deno.test(`${name} theme: every token pair meets WCAG AA contrast`, () => {
    const failing = pairs(theme)
      .filter(([, fg, bg, minimum]) => contrast(fg, bg) < minimum)
      .map(([label, fg, bg, minimum]) => `${label}: ${contrast(fg, bg).toFixed(2)} < ${minimum}`);
    assertEquals(failing, []);
  });
}

Deno.test("contrast math matches the WCAG reference values", () => {
  assertAlmostEquals(contrast([0, 0, 0], [255, 255, 255]), 21, 0.001);
  assertAlmostEquals(contrast([118, 118, 118], [255, 255, 255]), 4.54, 0.01);
  assertEquals(color("rgba(255, 255, 255, 0.5)", [0, 0, 0]), [128, 128, 128]);
});
