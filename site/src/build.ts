/**
 * Builds the static site into dist/: the launch page, one page per entry of
 * pages.ts and the assets. Fails on a missing page, an unknown placeholder or
 * an internal link to a page that does not exist.
 */
import { fillPlaceholders, placeholders, readVersion } from "./config.ts";
import { renderMarkdown } from "./markdown.ts";
import { allPages, docPath } from "./pages.ts";
import { docBody, shell } from "./layout.ts";
import { homeBody } from "./home.ts";

const root = new URL("../", import.meta.url);
const contentDir = new URL("content/docs/", root);
const assetsDir = new URL("assets/", root);
const distDir = new URL("dist/", root);

export interface BuiltPage {
  path: string;
  html: string;
}

export async function buildPages(version: string): Promise<BuiltPage[]> {
  const values = placeholders(version);
  const description =
    "Painel self-hosted para publicar aplicações, bancos e serviços no seu servidor, com domínio, HTTPS, backup e deploy sem downtime.";
  const pages: BuiltPage[] = [{
    path: "/",
    html: shell({ title: "Pier — seus projetos no ar, no seu servidor", description, path: "/", version, body: homeBody(version) }),
  }];
  for (const page of allPages) {
    const source = await Deno.readTextFile(new URL(`${page.slug}.md`, contentDir));
    const { html, headings } = renderMarkdown(fillPlaceholders(source, values));
    pages.push({
      path: docPath(page),
      html: shell({ title: page.title, description: page.description, path: docPath(page), version, body: docBody(page, html, headings) }),
    });
  }
  pages.push({ path: "/404.html", html: shell({ title: "Página não encontrada", description, path: "/404", version, body: notFound() }) });
  return pages;
}

function notFound(): string {
  return `<main id="conteudo" class="home"><section class="section narrow"><h1>Página não encontrada</h1>
<p>O endereço pode ter mudado. Comece pela <a href="/docs/introducao/">documentação</a> ou volte à <a href="/">página inicial</a>.</p></section></main>`;
}

function anchorsOf(html: string): Set<string> {
  return new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((match) => match[1]));
}

/**
 * Internal links that point to no generated page or asset, or to an anchor
 * the target page does not have: "/docs/x/" and "/docs/x/#secao" and "#secao".
 */
export function brokenLinks(pages: BuiltPage[], assets: string[]): string[] {
  const anchors = new Map(pages.map((page) => [page.path, anchorsOf(page.html)]));
  const assetPaths = new Set(assets.map((name) => `/assets/${name}`));
  const broken: string[] = [];
  for (const page of pages) {
    for (const match of page.html.matchAll(/href="(\/[^"#?]*)?(?:#([^"]*))?"/g)) {
      const [, path, fragment] = match;
      const target = path ?? page.path;
      if (assetPaths.has(target) && !fragment) continue;
      const targetAnchors = anchors.get(target);
      if (!targetAnchors || (fragment && !targetAnchors.has(fragment))) {
        broken.push(`${page.path} → ${match[0].slice(6, -1)}`);
      }
    }
  }
  return broken;
}

async function listAssets(): Promise<string[]> {
  const names: string[] = [];
  for await (const entry of Deno.readDir(assetsDir)) if (entry.isFile) names.push(entry.name);
  return names;
}

function outputFile(path: string): URL {
  const relative = path.endsWith("/") ? `${path.slice(1)}index.html` : path.slice(1);
  return new URL(relative, distDir);
}

async function write(pages: BuiltPage[], assets: string[]) {
  await Deno.remove(distDir, { recursive: true }).catch((error) => {
    if (!(error instanceof Deno.errors.NotFound)) throw error;
  });
  for (const page of pages) {
    const file = outputFile(page.path);
    await Deno.mkdir(new URL("./", file), { recursive: true });
    await Deno.writeTextFile(file, page.html);
  }
  await Deno.mkdir(new URL("assets/", distDir), { recursive: true });
  for (const name of assets) await Deno.copyFile(new URL(name, assetsDir), new URL(`assets/${name}`, distDir));
}

if (import.meta.main) {
  const version = await readVersion();
  const pages = await buildPages(version);
  const assets = await listAssets();
  const broken = brokenLinks(pages, assets);
  if (broken.length > 0) {
    console.error(`links internos quebrados:\n  ${broken.join("\n  ")}`);
    Deno.exit(1);
  }
  await write(pages, assets);
  console.log(`site do Pier ${version}: ${pages.length} páginas em dist/`);
}
