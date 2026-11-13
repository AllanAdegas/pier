import { assert, assertEquals, assertStringIncludes, assertThrows } from "@std/assert";
import { fillPlaceholders, parseVersion, placeholders, readVersion } from "./config.ts";
import { renderMarkdown, slugify, stripTags } from "./markdown.ts";
import { allPages, neighbours } from "./pages.ts";
import { brokenLinks, buildPages, withBasePath } from "./build.ts";

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

Deno.test("code is escaped and gets a copy button; external links open apart", () => {
  const { html } = renderMarkdown(
    "```sh\necho '<b>'\n```\n\n[GitHub](https://github.com) e [docs](/docs/x/)",
  );
  assertStringIncludes(html, "echo &#39;&lt;b&gt;&#39;");
  assertStringIncludes(html, 'class="copy"');
  assertStringIncludes(html, 'href="https://github.com" target="_blank" rel="noopener noreferrer"');
  assert(!html.includes('href="/docs/x/" target'));
});

Deno.test("every page has neighbours in reading order", () => {
  assertEquals(neighbours(allPages[0].slug).previous, undefined);
  assertEquals(neighbours(allPages[0].slug).next, allPages[1]);
  assertThrows(() => neighbours("nao-existe"));
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

Deno.test("the real site builds with the current version and no broken links", async () => {
  const version = await readVersion();
  const pages = await buildPages(version);
  assertEquals(pages.length, allPages.length + 2);
  assertEquals(brokenLinks(pages, ["favicon.svg", "style.css", "site.js", "theme.js"]), []);
  const home = pages.find((page) => page.path === "/")!;
  assertStringIncludes(home.html, `git clone --branch v${version} --depth 1`);
  for (const page of pages) assert(!/\{\{\w+\}\}/.test(page.html), `${page.path} tem marcador sem valor`);
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

Deno.test("every generated local URL supports project hosting", async () => {
  for (const page of await buildPages(await readVersion())) {
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
