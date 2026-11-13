/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { DOCS_PAGES, docsUrl } from "./docs.ts";

Deno.test("documentation links follow the interface language", () => {
  assertEquals(docsUrl("atualizar", "pt-BR"), "https://allanadegas.github.io/pier/docs/atualizar/");
  assertEquals(docsUrl("atualizar", "en"), "https://allanadegas.github.io/pier/en/docs/upgrading/");
  assertEquals(docsUrl("ia-mcp", "en"), "https://allanadegas.github.io/pier/en/docs/ai-mcp/");
  assertEquals(docsUrl("equipe", "en"), "https://allanadegas.github.io/pier/en/docs/team-and-access/");
});

Deno.test("every documentation page has a distinct English slug without accents", () => {
  const english = Object.values(DOCS_PAGES);
  assertEquals(new Set(english).size, english.length);
  assertEquals(english.filter((slug) => !/^[a-z0-9]+(-[a-z0-9]+)*$/.test(slug)), []);
  assertEquals(Object.keys(DOCS_PAGES).length, 24);
});
