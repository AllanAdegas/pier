// WCAG 2.1 contrast helpers for the design tokens (tokens.css).

export type RGB = [number, number, number];

/** Reads "--name: value;" declarations of one CSS block. */
export function tokens(block: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const match of block.matchAll(/--([\w-]+):\s*([^;]+);/g)) {
    out[match[1]] = match[2].trim();
  }
  return out;
}

/** A #rrggbb or rgba() token drawn over an opaque background. */
export function color(value: string, background: RGB = [0, 0, 0]): RGB {
  const hex = /^#([0-9a-f]{6})$/i.exec(value);
  if (hex) {
    const n = Number.parseInt(hex[1], 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  }
  const rgba = /^rgba\((\d+),\s*(\d+),\s*(\d+),\s*([\d.]+)\)$/.exec(value);
  if (!rgba) {
    throw new Error(`not a color: ${value}`);
  }
  const alpha = Number(rgba[4]);
  return [1, 2, 3].map((i, k) => Math.round(alpha * Number(rgba[i]) + (1 - alpha) * background[k])) as RGB;
}

function luminance(rgb: RGB): number {
  const [r, g, b] = rgb.map((channel) => {
    const value = channel / 255;
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** Contrast ratio between two opaque colors, from 1 to 21. */
export function contrast(a: RGB, b: RGB): number {
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (light + 0.05) / (dark + 0.05);
}
