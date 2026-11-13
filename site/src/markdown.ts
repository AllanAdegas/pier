/**
 * Markdown to HTML for the documentation: headings get stable anchors (and
 * feed the "nesta página" index), code blocks get a copy button and external
 * links open in a new tab.
 */
import { Marked, type Tokens } from "marked";

export interface Heading {
  id: string;
  text: string;
  depth: number;
}

export interface RenderedPage {
  html: string;
  headings: Heading[];
}

export function escapeHtml(text: string): string {
  return text
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

/** Plain text of rendered inline HTML: tags removed until none is left, stray angle brackets dropped. */
export function stripTags(html: string): string {
  let text = html;
  for (let previous = ""; previous !== text;) {
    previous = text;
    text = text.replace(/<[^<>]*>/g, "");
  }
  return text.replace(/[<>]/g, "");
}

/** "Domínio e HTTPS" → "dominio-e-https". */
export function slugify(text: string): string {
  return text
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[<>]/g, "")
    .replace(/&[a-z#0-9]+;/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function uniqueId(base: string, used: Map<string, number>): string {
  const count = used.get(base) ?? 0;
  used.set(base, count + 1);
  return count === 0 ? base : `${base}-${count + 1}`;
}

function isExternal(href: string): boolean {
  return /^https?:\/\//.test(href);
}

export function renderMarkdown(source: string): RenderedPage {
  const headings: Heading[] = [];
  const usedIds = new Map<string, number>();
  const marked = new Marked({ gfm: true });

  marked.use({
    renderer: {
      heading(this: { parser: { parseInline(tokens: Tokens.Generic[]): string } }, token: Tokens.Heading) {
        const inner = this.parser.parseInline(token.tokens);
        const id = uniqueId(slugify(token.text) || "secao", usedIds);
        if (token.depth === 2 || token.depth === 3) {
          headings.push({ id, text: stripTags(inner), depth: token.depth });
        }
        return `<h${token.depth} id="${id}"><a class="anchor" href="#${id}" aria-hidden="true" tabindex="-1">#</a>${inner}</h${token.depth}>\n`;
      },
      code(token: Tokens.Code) {
        const language = (token.lang ?? "").split(/\s/)[0];
        const label = language ? `<span class="code-lang">${escapeHtml(language)}</span>` : "";
        return `<div class="code-block">${label}<button class="copy" type="button" aria-label="Copiar">Copiar</button>` +
          `<pre><code${language ? ` class="language-${escapeHtml(language)}"` : ""}>${escapeHtml(token.text)}</code></pre></div>\n`;
      },
      link(this: { parser: { parseInline(tokens: Tokens.Generic[]): string } }, token: Tokens.Link) {
        const inner = this.parser.parseInline(token.tokens);
        const title = token.title ? ` title="${escapeHtml(token.title)}"` : "";
        const external = isExternal(token.href) ? ' target="_blank" rel="noopener noreferrer"' : "";
        return `<a href="${escapeHtml(token.href)}"${title}${external}>${inner}</a>`;
      },
      table(this: { parser: { parseInline(tokens: Tokens.Generic[]): string } }, token: Tokens.Table) {
        const cell = (c: Tokens.TableCell, tag: "th" | "td") => `<${tag}>${this.parser.parseInline(c.tokens)}</${tag}>`;
        const head = token.header.map((c) => cell(c, "th")).join("");
        const body = token.rows.map((row) => `<tr>${row.map((c) => cell(c, "td")).join("")}</tr>`).join("\n");
        return `<div class="table-wrap"><table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table></div>\n`;
      },
    },
  });

  const html = marked.parse(source, { async: false }) as string;
  return { html, headings };
}
