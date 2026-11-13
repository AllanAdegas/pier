/**
 * Translation parity between a Portuguese page and its English counterpart.
 * A translation keeps the structure of the source: the same headings (depth
 * and order), the same code blocks (count and language), the same
 * {{placeholders}} and the same internal links, each one rewritten to the
 * English page and to the English anchor of the same heading.
 */
import { Marked, type Tokens } from "marked";
import { fillPlaceholders } from "./config.ts";
import { renderMarkdown } from "./markdown.ts";
import { allPages, type DocPage, docPath } from "./pages.ts";
import { homePath } from "./language.ts";

export interface DocumentShape {
  /** Depth of every heading outside code, in order. */
  headingDepths: number[];
  /** Anchor of every heading, in the same order, as the site renders it. */
  headingIds: string[];
  /** Language label of every code block, in order ("" when it has none). */
  codeLanguages: string[];
  /** Placeholder names, sorted, one entry per occurrence. */
  placeholders: string[];
  /** Internal Markdown links ("/…" or "#…"), in order. */
  links: string[];
  frontMatter: boolean;
}

function isInternal(href: string): boolean {
  return (href.startsWith("/") && !href.startsWith("//")) || href.startsWith("#");
}

export function documentShape(source: string, values: Record<string, string>): DocumentShape {
  const marked = new Marked({ gfm: true });
  const codeLanguages: string[] = [];
  const links: string[] = [];
  marked.walkTokens(marked.lexer(source), (token) => {
    if (token.type === "code") codeLanguages.push(((token as Tokens.Code).lang ?? "").split(/\s/)[0]);
    if (token.type === "link" && isInternal((token as Tokens.Link).href)) {
      links.push((token as Tokens.Link).href);
    }
  });
  const { headings } = renderMarkdown(fillPlaceholders(source, values));
  return {
    headingDepths: headings.map((heading) => heading.depth),
    headingIds: headings.map((heading) => heading.id),
    codeLanguages,
    placeholders: [...source.matchAll(/\{\{(\w+)\}\}/g)].map((match) => match[1]).sort(),
    links,
    frontMatter: /^---\r?\n/.test(source),
  };
}

function sameList(a: readonly unknown[], b: readonly unknown[]): boolean {
  return a.length === b.length && a.every((value, index) => value === b[index]);
}

function describe(list: readonly unknown[]): string {
  return `[${list.join(", ")}]`;
}

/** Structural differences between a Portuguese page and its English translation. */
export function structureProblems(pt: DocumentShape, en: DocumentShape): string[] {
  const problems: string[] = [];
  if (!sameList(pt.headingDepths, en.headingDepths)) {
    problems.push(`headings differ: pt ${describe(pt.headingDepths)} en ${describe(en.headingDepths)}`);
  }
  if (!sameList(pt.codeLanguages, en.codeLanguages)) {
    problems.push(`code blocks differ: pt ${describe(pt.codeLanguages)} en ${describe(en.codeLanguages)}`);
  }
  if (!sameList(pt.placeholders, en.placeholders)) {
    problems.push(`placeholders differ: pt ${describe(pt.placeholders)} en ${describe(en.placeholders)}`);
  }
  if (en.headingDepths.includes(1)) problems.push("the body has a # title; the title comes from pages.ts");
  if (en.frontMatter) problems.push("front matter is not supported; the title comes from pages.ts");
  return problems;
}

/** The pages whose shapes are known in both languages, keyed by page. */
export type ShapeIndex = Map<DocPage, { pt: DocumentShape; en?: DocumentShape }>;

const translationUnknown = Symbol("translation unknown");

/**
 * The English link a Portuguese link becomes: the English page and the
 * anchor of the heading at the same position. translationUnknown when the
 * target's English file does not exist yet.
 */
function englishLink(href: string, from: DocPage, index: ShapeIndex): string | typeof translationUnknown {
  const match = href.match(/^(\/[^#?]*)?(?:#(.*))?$/);
  if (!match) return href;
  const [, path, fragment] = match;
  if (path === "/") return homePath("en") + (fragment ? `#${fragment}` : "");
  const target = path ? allPages.find((page) => docPath(page, "pt") === path) : from;
  if (!target) return href;
  const shapes = index.get(target);
  if (!shapes?.en) return translationUnknown;
  if (!fragment) return docPath(target, "en");
  const position = shapes.pt.headingIds.indexOf(fragment);
  const anchor = position < 0 ? `${fragment}?missing-in-pt` : shapes.en.headingIds[position];
  return `${path ? docPath(target, "en") : ""}#${anchor}`;
}

function linksToKnownPages(links: string[], index: ShapeIndex): string[] {
  return links.filter((href) => {
    const path = href.match(/^(\/[^#?]*)/)?.[1];
    const target = path && allPages.find((page) => docPath(page, "en") === path);
    return !target || index.get(target)?.en !== undefined;
  });
}

function countOf(list: string[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const item of list) counts.set(item, (counts.get(item) ?? 0) + 1);
  return counts;
}

/**
 * Internal links of the English page compared with the Portuguese links
 * rewritten to English (as a multiset: order may change with the sentence).
 * Links to pages whose English file does not exist yet are not compared.
 */
export function linkProblems(page: DocPage, index: ShapeIndex): string[] {
  const shapes = index.get(page);
  if (!shapes?.en) return [];
  const expected = shapes.pt.links.map((href) => englishLink(href, page, index))
    .filter((href): href is string => href !== translationUnknown);
  const actual = countOf(linksToKnownPages(shapes.en.links, index));
  const problems: string[] = [];
  for (const [href, count] of countOf(expected)) {
    const found = actual.get(href) ?? 0;
    if (found < count) problems.push(`missing link ${href} (${count - found}×)`);
    actual.set(href, found - count);
  }
  for (const [href, extra] of actual) if (extra > 0) problems.push(`unexpected link ${href} (${extra}×)`);
  return problems;
}
