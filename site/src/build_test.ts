import { assert, assertEquals, assertRejects, assertStringIncludes, assertThrows } from "@std/assert";
import { fillPlaceholders, parseVersion, placeholders, readVersion } from "./config.ts";
import { renderMarkdown, slugify, stripTags } from "./markdown.ts";
import { allPages, type DocPage, docPath, neighbours } from "./pages.ts";
import { brokenLinks, buildPages, type BuiltPage, crossLanguageLinks, withBasePath } from "./build.ts";
import { allowMissingEnglish, MissingTranslationError } from "./content.ts";
import { homePath, languages } from "./language.ts";
import { strings } from "./strings.ts";

const assets = ["favicon.svg", "style.css", "site.js", "theme.js"];

let cachedSite: Promise<BuiltPage[]> | undefined;

/** The real site, built once per test run; missing English pages fall back only with SITE_ALLOW_MISSING_EN=1. */
function realSite(): Promise<BuiltPage[]> {
  cachedSite ??= readVersion().then((version) =>
    buildPages(version, { allowMissingEnglish: allowMissingEnglish() })
  );
  return cachedSite;
}

Deno.test("the version comes from the server's version.go", () => {
  assertEquals(parseVersion('const (\n\tNumber = "1.2.3"\n\tAPI = "v1"\n)'), "1.2.3");
  assertThrows(() => parseVersion("package version"));
});

Deno.test("an unknown placeholder fails the build instead of printing it", () => {
  const values = placeholders("1.2.3");
  assertEquals(fillPlaceholders("git clone --branch {{tag}}", values), "git clone --branch v1.2.3");
  assertEquals(
    fillPlaceholders("${{principal.url}} ${{ secrets.X }}", values),
    "${{principal.url}} ${{ secrets.X }}",
  );
  assertThrows(() => fillPlaceholders("{{versao}}", values));
});

Deno.test("headings get accent-free anchors, unique per page", () => {
  assertEquals(slugify("Configurações do `.env`"), "configuracoes-do-env");
  const { html, headings } = renderMarkdown("## Instalação\n\n## Instalação\n\n### Passo 1");
  assertStringIncludes(html, 'id="instalacao"');
  assertStringIncludes(html, 'id="instalacao-2"');
  assertEquals(headings.map((heading) => heading.depth), [2, 2, 3]);
});

Deno.test("code is escaped and gets a copy button in the page's language; external links open apart", () => {
  const { html } = renderMarkdown(
    "```sh\necho '<b>'\n```\n\n[GitHub](https://github.com) e [docs](/docs/x/)",
  );
  assertStringIncludes(html, "echo &#39;&lt;b&gt;&#39;");
  assertStringIncludes(html, 'class="copy"');
  assertStringIncludes(html, 'data-copied-label="Copiado"');
  assertStringIncludes(html, 'href="https://github.com" target="_blank" rel="noopener noreferrer"');
  assert(!html.includes('href="/docs/x/" target'));
  const english = renderMarkdown("```sh\nls\n```", strings.en.code).html;
  assertStringIncludes(english, 'aria-label="Copy" data-copied-label="Copied">Copy</button>');
});

Deno.test("every page has neighbours in reading order", () => {
  assertEquals(neighbours(allPages[0]).previous, undefined);
  assertEquals(neighbours(allPages[0]).next, allPages[1]);
  const unknown: DocPage = {
    slug: { pt: "x", en: "x" },
    title: { pt: "", en: "" },
    description: { pt: "", en: "" },
  };
  assertThrows(() => neighbours(unknown));
});

Deno.test("broken internal links and anchors are reported", () => {
  const pages = [
    {
      path: "/",
      html:
        '<a href="/docs/a/#ok"></a><a href="/docs/a/#falta"></a><a href="/nada/"></a><a href="#topo"></a>',
    },
    { path: "/docs/a/", html: '<h2 id="ok"></h2><img src="/assets/x.svg"><a href="/assets/x.svg"></a>' },
  ];
  assertEquals(brokenLinks(pages, ["x.svg"]), ["/ → /docs/a/#falta", "/ → /nada/", "/ → #topo"]);
});

Deno.test("anchors of a page still waiting for its translation are not checked, the page itself is", () => {
  const pages: BuiltPage[] = [
    { path: "/en/", language: "en", html: '<a href="/en/docs/a/#english"></a><a href="/en/docs/b/"></a>' },
    { path: "/en/docs/a/", language: "en", translationPending: true, html: '<h2 id="portugues"></h2>' },
  ];
  assertEquals(brokenLinks(pages, []), ["/en/ → /en/docs/b/"]);
});

Deno.test("links that leave the page's language are reported, except the hreflang switch", () => {
  const pages: BuiltPage[] = [
    {
      path: "/en/docs/a/",
      language: "en",
      html:
        '<a href="/docs/a/" hreflang="pt-BR">PT</a><a href="/docs/b/#x">b</a><a href="/assets/s.css"></a>' +
        '<a href="/en/docs/b/">b</a><a href="#y"></a><a href="https://example.com/"></a>',
    },
    { path: "/docs/a/", language: "pt", html: '<a href="/en/">en</a><a href="/">início</a>' },
    { path: "/en/docs/c/", language: "en", translationPending: true, html: '<a href="/docs/a/"></a>' },
    { path: "/404.html", html: '<a href="/docs/a/"></a><a href="/en/"></a>' },
  ];
  assertEquals(crossLanguageLinks(pages), ["/en/docs/a/ → /docs/b/", "/docs/a/ → /en/"]);
});

Deno.test("a missing English page fails the build and names every missing file", async () => {
  const empty = new URL("../content/docs/does-not-exist/", import.meta.url);
  const error = await assertRejects(
    () => buildPages("1.2.3", { contentDir: empty }),
    MissingTranslationError,
  );
  assertEquals(error.files.length, allPages.length);
  assertStringIncludes(error.message, "content/docs/en/introduction.md");
  assertStringIncludes(error.message, "SITE_ALLOW_MISSING_EN=1");
});

Deno.test("SITE_ALLOW_MISSING_EN is refused in CI", () => {
  const saved = { allow: Deno.env.get("SITE_ALLOW_MISSING_EN"), ci: Deno.env.get("CI") };
  const restore = (name: string, value?: string) =>
    value === undefined ? Deno.env.delete(name) : Deno.env.set(name, value);
  try {
    Deno.env.delete("SITE_ALLOW_MISSING_EN");
    Deno.env.set("CI", "true");
    assertEquals(allowMissingEnglish(), false);
    assertThrows(() => allowMissingEnglish(["--allow-missing-en"]));
    Deno.env.set("SITE_ALLOW_MISSING_EN", "1");
    assertThrows(() => allowMissingEnglish());
    Deno.env.delete("CI");
    assertEquals(allowMissingEnglish(), true);
  } finally {
    restore("SITE_ALLOW_MISSING_EN", saved.allow);
    restore("CI", saved.ci);
  }
});

Deno.test("the real site builds both languages with the current version and no broken links", async () => {
  const version = await readVersion();
  const pages = await realSite();
  assertEquals(pages.length, 2 * allPages.length + 3);
  assertEquals(brokenLinks(pages, assets), []);
  assertEquals(crossLanguageLinks(pages), []);
  for (const language of languages) {
    const home = pages.find((page) => page.path === homePath(language));
    assert(home, `missing home ${homePath(language)}`);
    assertStringIncludes(home.html, `git clone --branch v${version} --depth 1`);
    for (const page of allPages) assert(pages.some((built) => built.path === docPath(page, language)));
  }
  for (const page of pages) {
    assert(!/\{\{\w+\}\}/.test(page.html), `${page.path} has an unresolved placeholder`);
  }
});

function linkTag(html: string, className: string): string | undefined {
  return html.match(new RegExp(`<a class="${className}"[^>]*>`))?.[0];
}

Deno.test("every page declares its language and links to the same page in the other one", async () => {
  const pages = await realSite();
  const paths = new Set(pages.map((page) => page.path));
  for (const page of pages.filter((each) => each.language)) {
    const expectedLang = page.language === "pt" ? "pt-BR" : "en";
    assertStringIncludes(page.html, `<html lang="${expectedLang}">`);
    assertStringIncludes(
      page.html,
      `<meta property="og:locale" content="${page.language === "pt" ? "pt_BR" : "en_US"}">`,
    );
    const alternates = [...page.html.matchAll(/<link rel="alternate" hreflang="([^"]+)" href="([^"]+)">/g)]
      .map(([, hreflang, href]) => ({ hreflang, href }));
    assertEquals(alternates.map((link) => link.hreflang), ["pt-BR", "en", "x-default"], page.path);
    for (const link of alternates) assert(paths.has(link.href), `${page.path}: hreflang ${link.href}`);
    assertEquals(alternates[2].href, alternates[1].href, "x-default is the English page");
    assertEquals(alternates.find((link) => link.hreflang === expectedLang)?.href, page.path);
    const toggle = linkTag(page.html, "language-switch");
    assert(toggle, `${page.path} has no language switch`);
    const target = toggle.match(/href="([^"]+)"/)![1];
    assert(paths.has(target) && target !== page.path, `${page.path}: switch → ${target}`);
    assertEquals(
      target,
      alternates.find((link) => link.hreflang !== expectedLang && link.hreflang !== "x-default")?.href,
    );
  }
});

Deno.test("the 404 page offers both languages", async () => {
  const notFound = (await realSite()).find((page) => page.path === "/404.html")!;
  for (const language of languages) {
    assertStringIncludes(
      notFound.html,
      `<div lang="${language === "pt" ? "pt-BR" : "en"}"><h1>${strings[language].notFound.title}</h1>`,
    );
    assertStringIncludes(notFound.html, `href="${homePath(language)}"`);
    assertStringIncludes(notFound.html, `href="${docPath(allPages[0], language)}"`);
  }
});

/** Every plain string of a strings subtree. */
function textsOf(tree: unknown): string[] {
  if (typeof tree === "string") return [tree];
  if (Array.isArray(tree)) return tree.flatMap(textsOf);
  if (tree && typeof tree === "object") return Object.values(tree).flatMap(textsOf);
  return [];
}

Deno.test("the frame of English pages has no Portuguese label, and the reverse", async () => {
  const switchTexts = (language: "pt" | "en") => [
    strings[language].layout.languageName,
    strings[language].layout.languageCode,
    strings[language].layout.readInThisLanguage,
  ];
  for (const page of (await realSite()).filter((each) => each.language && !each.translationPending)) {
    const other = page.language === "pt" ? "en" : "pt";
    const frame = page.html.replace(/<article>[\s\S]*<\/article>/, "");
    const foreign = textsOf([strings[other].layout, strings[other].docs, strings[other].code])
      .filter((text) =>
        !switchTexts(other).includes(text) && !textsOf(strings[page.language!]).includes(text)
      );
    for (const text of foreign) {
      assert(!frame.includes(`>${text}<`) && !frame.includes(`"${text}"`), `${page.path}: "${text}"`);
    }
  }
});

Deno.test("project prefix rebases local links and assets only", () => {
  const html =
    '<a href="/"></a><a href="/docs/a/#b"></a><script src="/assets/x.js"></script><a href="https://example.com/"></a><a href="//example.com/"></a><a href="#b"></a>';
  const rebased = withBasePath(html, "/pier/");
  assertStringIncludes(rebased, 'href="/pier/"');
  assertStringIncludes(rebased, 'href="/pier/docs/a/#b"');
  assertStringIncludes(rebased, 'src="/pier/assets/x.js"');
  assertStringIncludes(rebased, 'href="https://example.com/"');
  assertStringIncludes(rebased, 'href="//example.com/"');
  assertStringIncludes(rebased, 'href="#b"');
  assertEquals(withBasePath(html), html);
  for (const invalid of ["https://example.com/", "//example.com/", "/../", '/x"/']) {
    assertThrows(() => withBasePath(html, invalid));
  }
});

Deno.test("every generated local URL supports project hosting, in both languages", async () => {
  for (const page of await realSite()) {
    for (const match of withBasePath(page.html, "/pier/").matchAll(/\b(?:href|src)="(\/(?!\/)[^"]*)"/g)) {
      assert(match[1].startsWith("/pier/"), `${page.path}: ${match[1]}`);
    }
  }
});

Deno.test("heading text for the table of contents never keeps markup", () => {
  assertEquals(stripTags("a <b>b</b> c"), "a b c");
  assertEquals(stripTags("<scr<script>ipt>x"), "x");
  assertEquals(slugify("<script>Olá</script>"), "scriptola-script");
});
