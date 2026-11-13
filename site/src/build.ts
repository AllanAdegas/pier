/**
 * Builds the static site into dist/: the launch page and one page per entry
 * of pages.ts in each language (Portuguese at /, English at /en/), the single
 * 404 page and the assets. Fails on a missing English page, an unknown
 * placeholder, an internal link to a page or anchor that does not exist, or a
 * link that leaves the page's language.
 */
import { fillPlaceholders, placeholders, readVersion } from "./config.ts";
import { renderMarkdown } from "./markdown.ts";
import { allPages, contentFile, type DocPage, docPath } from "./pages.ts";
import { docBody, shell } from "./layout.ts";
import { homeBody } from "./home.ts";
import { notFoundBody, notFoundLanguage, notFoundTitle } from "./not_found.ts";
import { homePath, type Language, languages, type Localized } from "./language.ts";
import { strings } from "./strings.ts";
import {
  allowMissingEnglish,
  AllowMissingRefused,
  contentDir as defaultContentDir,
  missingEnglishFiles,
  MissingTranslationError,
  readSource,
} from "./content.ts";

const root = new URL("../", import.meta.url);
const assetsDir = new URL("assets/", root);
const distDir = new URL("dist/", root);

export interface BuiltPage {
  path: string;
  html: string;
  /** Absent on the 404 page, which offers both languages. */
  language?: Language;
  /** An English page rendered from the Portuguese text (local development only). */
  translationPending?: boolean;
}

export interface BuildOptions {
  /** Render missing English pages from Portuguese instead of failing. Local development only. */
  allowMissingEnglish?: boolean;
  contentDir?: URL;
}

interface BuildContext {
  version: string;
  values: Record<string, string>;
  dir: URL;
  missingEnglish: Set<string>;
}

function homeAlternates(): Localized {
  return { pt: homePath("pt"), en: homePath("en") };
}

function docAlternates(page: DocPage): Localized {
  return { pt: docPath(page, "pt"), en: docPath(page, "en") };
}

function homePage(language: Language, context: BuildContext): BuiltPage {
  const text = strings[language].site;
  return {
    path: homePath(language),
    language,
    html: shell({
      language,
      title: text.homeTitle,
      description: text.description,
      path: homePath(language),
      version: context.version,
      body: homeBody(context.version, language),
      alternates: homeAlternates(),
    }),
  };
}

async function docPage(page: DocPage, language: Language, context: BuildContext): Promise<BuiltPage> {
  const translationPending = language === "en" && context.missingEnglish.has(contentFile(page, "en"));
  const source = await readSource(page, translationPending ? "pt" : language, context.dir);
  const { html, headings } = renderMarkdown(fillPlaceholders(source, context.values), strings[language].code);
  return {
    path: docPath(page, language),
    language,
    translationPending,
    html: shell({
      language,
      title: page.title[language],
      description: page.description[language],
      path: docPath(page, language),
      version: context.version,
      body: docBody(page, language, { html, headings, translationPending }),
      alternates: docAlternates(page),
    }),
  };
}

function notFoundPage(version: string): BuiltPage {
  return {
    path: "/404.html",
    html: shell({
      language: notFoundLanguage,
      title: notFoundTitle(),
      description: strings[notFoundLanguage].site.description,
      path: "/404",
      version,
      body: notFoundBody(),
    }),
  };
}

export async function buildPages(version: string, options: BuildOptions = {}): Promise<BuiltPage[]> {
  const dir = options.contentDir ?? defaultContentDir;
  const missing = await missingEnglishFiles(dir);
  if (missing.length > 0 && !options.allowMissingEnglish) throw new MissingTranslationError(missing);
  const context: BuildContext = {
    version,
    values: placeholders(version),
    dir,
    missingEnglish: new Set(missing),
  };
  const pages: BuiltPage[] = [];
  for (const language of languages) {
    pages.push(homePage(language, context));
    for (const page of allPages) pages.push(await docPage(page, language, context));
  }
  pages.push(notFoundPage(version));
  return pages;
}

function anchorsOf(html: string): Set<string> {
  return new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((match) => match[1]));
}

/**
 * Internal links that point to no generated page or asset, or to an anchor
 * the target page does not have: "/docs/x/", "/en/docs/x/#section" and
 * "#section". Anchors of a page still waiting for its translation are not
 * checked, because it shows the Portuguese headings.
 */
export function brokenLinks(pages: BuiltPage[], assets: string[]): string[] {
  const anchors = new Map(pages.map((page) => [page.path, anchorsOf(page.html)]));
  const pending = new Set(pages.filter((page) => page.translationPending).map((page) => page.path));
  const assetPaths = new Set(assets.map((name) => `/assets/${name}`));
  const broken: string[] = [];
  for (const page of pages) {
    for (const match of page.html.matchAll(/href="(\/[^"#?]*)?(?:#([^"]*))?"/g)) {
      const [, path, fragment] = match;
      const target = path ?? page.path;
      if (assetPaths.has(target) && !fragment) continue;
      const targetAnchors = anchors.get(target);
      const anchorMissing = fragment && !targetAnchors?.has(fragment) && !pending.has(target);
      if (!targetAnchors || anchorMissing) broken.push(`${page.path} → ${match[0].slice(6, -1)}`);
    }
  }
  return broken;
}

function languageOfPath(path: string): Language {
  return path === "/en/" || path.startsWith("/en/") ? "en" : "pt";
}

/**
 * Links from a page to a page in the other language. Only the language
 * switch may do that, and it is marked with hreflang. Pages waiting for
 * their translation show Portuguese text and are not checked.
 */
export function crossLanguageLinks(pages: BuiltPage[]): string[] {
  const found: string[] = [];
  for (const page of pages) {
    if (!page.language || page.translationPending) continue;
    for (const [tag] of page.html.matchAll(/<a\s[^>]*>/g)) {
      const href = tag.match(/\shref="(\/(?!\/|assets\/)[^"#?]*)/)?.[1];
      if (!href || /\shreflang="/.test(tag)) continue;
      if (languageOfPath(href) !== page.language) found.push(`${page.path} → ${href}`);
    }
  }
  return found;
}

async function listAssets(): Promise<string[]> {
  const names: string[] = [];
  for await (const entry of Deno.readDir(assetsDir)) if (entry.isFile) names.push(entry.name);
  return names;
}

/** Rebase local document and asset URLs for a project hosted below a prefix. */
export function withBasePath(html: string, basePath = "/"): string {
  if (!/^\/(?:[A-Za-z0-9_-]+\/)*$/.test(basePath)) {
    throw new Error("SITE_BASE_PATH must be a root-relative directory ending in /.");
  }
  if (basePath === "/") return html;
  return html.replace(
    /\b(href|src)="\/(?!\/)([^"]*)"/g,
    (_, attribute, target) => `${attribute}="${basePath}${target}"`,
  );
}

function outputFile(path: string): URL {
  const relative = path.endsWith("/") ? `${path.slice(1)}index.html` : path.slice(1);
  return new URL(relative, distDir);
}

async function write(pages: BuiltPage[], assets: string[], basePath: string) {
  await Deno.remove(distDir, { recursive: true }).catch((error) => {
    if (!(error instanceof Deno.errors.NotFound)) throw error;
  });
  for (const page of pages) {
    const file = outputFile(page.path);
    await Deno.mkdir(new URL("./", file), { recursive: true });
    await Deno.writeTextFile(file, withBasePath(page.html, basePath));
  }
  await Deno.mkdir(new URL("assets/", distDir), { recursive: true });
  for (const name of assets) {
    await Deno.copyFile(new URL(name, assetsDir), new URL(`assets/${name}`, distDir));
  }
}

function failWith(title: string, problems: string[]): void {
  if (problems.length > 0) throw new BuildRefused(`${title}:\n  ${problems.join("\n  ")}`);
}

/** A problem the author must fix: printed without a stack trace. */
class BuildRefused extends Error {}

async function main() {
  const version = await readVersion();
  const pages = await buildPages(version, { allowMissingEnglish: allowMissingEnglish(Deno.args) });
  const pending = pages.filter((page) => page.translationPending).map((page) => page.path);
  if (pending.length > 0) {
    console.warn(
      `translation in progress, shown in Portuguese (local development only):\n  ${pending.join("\n  ")}`,
    );
  }
  const assets = await listAssets();
  failWith("broken internal links", brokenLinks(pages, assets));
  failWith("links that leave the page's language", crossLanguageLinks(pages));
  await write(pages, assets, Deno.env.get("SITE_BASE_PATH") || "/");
  console.log(`Pier site ${version}: ${pages.length} pages in dist/`);
}

if (import.meta.main) {
  await main().catch((error) => {
    if (
      !(error instanceof MissingTranslationError || error instanceof AllowMissingRefused ||
        error instanceof BuildRefused)
    ) throw error;
    console.error(error.message);
    Deno.exit(1);
  });
}
