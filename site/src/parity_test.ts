/**
 * The CI enforcement of the language rule for the documentation: every page
 * exists in Portuguese and in English with the same structure. Missing
 * English files fail here unless SITE_ALLOW_MISSING_EN=1 (local only; refused
 * in CI), in which case only the existing English files are compared.
 */
import { assert, assertEquals } from "@std/assert";
import { placeholders, readVersion } from "./config.ts";
import { allowMissingEnglish, contentDir, hasSource, markdownFiles, readSource } from "./content.ts";
import { documentShape, linkProblems, type ShapeIndex, structureProblems } from "./parity.ts";
import { allPages, contentFile, docPath } from "./pages.ts";
import { languages } from "./language.ts";

const values = placeholders(await readVersion());

async function shapeIndex(): Promise<ShapeIndex> {
  const index: ShapeIndex = new Map();
  for (const page of allPages) {
    const pt = documentShape(await readSource(page, "pt"), values);
    const en = await hasSource(page, "en") ? documentShape(await readSource(page, "en"), values) : undefined;
    index.set(page, { pt, en });
  }
  return index;
}

function report(problems: string[]): string {
  return `\n  ${problems.join("\n  ")}`;
}

Deno.test("every page of pages.ts has a Portuguese and an English file, and no file is left out", async () => {
  const expected = (language: "pt" | "en") =>
    allPages.map((page) => contentFile(page, language).replace(/^en\//, "")).sort();
  assertEquals(await markdownFiles(contentDir), expected("pt"), "content/docs/*.md and pages.ts differ");
  const english = await markdownFiles(new URL("en/", contentDir));
  const unknown = english.filter((name) => !expected("en").includes(name));
  assertEquals(unknown, [], "content/docs/en/ has files that pages.ts does not list");
  const missing = expected("en").filter((name) => !english.includes(name));
  if (!allowMissingEnglish()) assertEquals(missing, [], `missing English pages:${report(missing)}`);
  else if (missing.length > 0) {
    console.warn(`SITE_ALLOW_MISSING_EN=1: ${missing.length} English page(s) skipped`);
  }
});

Deno.test("slugs are unique per language and English slugs are plain", () => {
  for (const language of languages) {
    const slugs = allPages.map((page) => page.slug[language]);
    assertEquals(new Set(slugs).size, slugs.length, `duplicate ${language} slug`);
    const paths = allPages.map((page) => docPath(page, language));
    assertEquals(new Set(paths).size, paths.length);
  }
  for (const page of allPages) assert(/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(page.slug.en), page.slug.en);
});

Deno.test("titles, descriptions and section titles exist in both languages", async () => {
  const { sections } = await import("./pages.ts");
  for (const section of sections) for (const language of languages) assert(section.title[language].trim());
  for (const page of allPages) {
    for (const language of languages) {
      assert(page.title[language].trim(), `${page.slug.pt}: title ${language}`);
      assert(page.description[language].trim(), `${page.slug.pt}: description ${language}`);
    }
  }
});

Deno.test("Portuguese bodies have no # title and no front matter", async () => {
  const problems: string[] = [];
  for (const [page, shapes] of await shapeIndex()) {
    if (shapes.pt.headingDepths.includes(1)) problems.push(`${contentFile(page, "pt")}: # title in the body`);
    if (shapes.pt.frontMatter) problems.push(`${contentFile(page, "pt")}: front matter`);
  }
  assertEquals(problems, [], report(problems));
});

Deno.test("each English page keeps the headings, code blocks and placeholders of the Portuguese one", async () => {
  const problems: string[] = [];
  for (const [page, shapes] of await shapeIndex()) {
    if (!shapes.en) continue;
    for (const problem of structureProblems(shapes.pt, shapes.en)) {
      problems.push(`${contentFile(page, "en")}: ${problem}`);
    }
  }
  assertEquals(problems, [], report(problems));
});

Deno.test("each English page keeps the internal links of the Portuguese one, rewritten to English", async () => {
  const index = await shapeIndex();
  const problems: string[] = [];
  for (const page of allPages) {
    for (const problem of linkProblems(page, index)) problems.push(`${contentFile(page, "en")}: ${problem}`);
  }
  assertEquals(problems, [], report(problems));
});

Deno.test("the page shape ignores headings inside code and counts every placeholder", () => {
  const shape = documentShape(
    "Intro {{version}}\n\n## Um\n\n```yaml\n# not a heading {{tag}}\n```\n\n### Dois\n\n    indented\n\n" +
      "[a](/docs/x/#um) [b](#dois) [c](https://example.com/) [d](//cdn.example/)",
    values,
  );
  assertEquals(shape.headingDepths, [2, 3]);
  assertEquals(shape.headingIds, ["um", "dois"]);
  assertEquals(shape.codeLanguages, ["yaml", ""]);
  assertEquals(shape.placeholders, ["tag", "version"]);
  assertEquals(shape.links, ["/docs/x/#um", "#dois"]);
  assertEquals(shape.frontMatter, false);
  assert(documentShape("---\ntitle: x\n---\n", values).frontMatter);
});

Deno.test("structural differences are named", () => {
  const pt = documentShape("## A\n\n```sh\nls\n```\n{{tag}}", values);
  const en = documentShape("# Title\n\n## A\n\n### B\n\n```\nls\n```", values);
  const problems = structureProblems(pt, en);
  assertEquals(problems.length, 4);
  assert(problems[0].startsWith("headings differ"));
  assert(problems[1].startsWith("code blocks differ"));
  assert(problems[2].startsWith("placeholders differ"));
  assert(problems[3].includes("# title"));
  assertEquals(structureProblems(pt, documentShape("## B\n\n```sh\nls -la\n```\n{{tag}}", values)), []);
});

Deno.test("links are compared after rewriting page and anchor to English by heading position", () => {
  const [first, second, third] = allPages;
  const shape = (source: string) => documentShape(source, values);
  const index: ShapeIndex = new Map([
    [first, {
      pt: shape(
        `## Início\n\n[x](/docs/${second.slug.pt}/#segunda-secao) [y](#inicio) [z](/) [w](/docs/${third.slug.pt}/)`,
      ),
      en: shape(
        `## Start\n\n[x](/en/docs/${second.slug.en}/#second-section) [z](/en/) [extra](/en/docs/${second.slug.en}/)`,
      ),
    }],
    [second, { pt: shape("## Primeira\n\n## Segunda seção"), en: shape("## First\n\n## Second section") }],
    [third, { pt: shape("texto") }],
  ]);
  assertEquals(linkProblems(first, index), [
    "missing link #start (1×)",
    `unexpected link /en/docs/${second.slug.en}/ (1×)`,
  ]);
  assertEquals(linkProblems(third, index), []);
});
