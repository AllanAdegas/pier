/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { english } from "./en.ts";
import { detectLanguage, interpolate, translate } from "./index.ts";

const SOURCE_ROOT = new URL("../", import.meta.url);

/** Every Portuguese text the interface asks to translate, by file. */
async function sourceTexts(): Promise<Map<string, string>> {
  const texts = new Map<string, string>();
  const call = /\b(?:t|msg)\(\s*"((?:\\.|[^"\\])*)"|<Trans\s+text="([^"]*)"/g;
  for await (const path of walk(SOURCE_ROOT)) {
    const code = await Deno.readTextFile(path);
    for (const match of code.matchAll(call)) {
      const text = match[1] !== undefined ? JSON.parse(`"${match[1]}"`) : match[2];
      texts.set(text, path.pathname.split("/src/")[1]);
    }
  }
  return texts;
}

async function* walk(dir: URL): AsyncGenerator<URL> {
  for await (const entry of Deno.readDir(dir)) {
    const url = new URL(entry.name + (entry.isDirectory ? "/" : ""), dir);
    if (entry.isDirectory) {
      if (entry.name !== "i18n") yield* walk(url);
    } else if (/\.tsx?$/.test(entry.name) && !entry.name.includes(".test.")) {
      yield url;
    }
  }
}

const placeholders = (text: string) => [...text.matchAll(/(?<!\$)\{(\w+)\}/g)].map((match) => match[1]).sort();

Deno.test("every interface text has an English translation", async () => {
  const missing = [...(await sourceTexts())].filter(([text]) => !(text in english)).map(([text, file]) => `${file}: ${text}`);
  assertEquals(missing, []);
});

Deno.test("the English catalog has no texts the interface stopped using", async () => {
  const used = await sourceTexts();
  assertEquals(Object.keys(english).filter((text) => !used.has(text)), []);
});

Deno.test("translations keep the placeholders of the source", () => {
  const broken = Object.entries(english).filter(([source, target]) => placeholders(source).join() !== placeholders(target).join());
  assertEquals(broken, []);
});

Deno.test("t() only receives literal texts, so the catalog check sees them all", async () => {
  const dynamic: string[] = [];
  for await (const path of walk(SOURCE_ROOT)) {
    const code = await Deno.readTextFile(path);
    for (const match of code.matchAll(/\b(?:t|msg)\(\s*([^\s"])/g)) dynamic.push(`${path.pathname.split("/src/")[1]}: ${match[0]}`);
  }
  assertEquals(dynamic, []);
});

Deno.test("the saved choice wins, then the first browser language Pier speaks", () => {
  assertEquals(detectLanguage("pt-BR", ["en-US"]), "pt-BR");
  assertEquals(detectLanguage(null, ["fr-FR", "pt-PT", "en"]), "pt-BR");
  assertEquals(detectLanguage(null, ["en-GB"]), "en");
  assertEquals(detectLanguage("klingon", ["de-DE"]), "en");
});

Deno.test("translation falls back to the source and fills placeholders", () => {
  assertEquals(translate("en", "{count} linha(s)", { count: 3 }), "3 line(s)");
  assertEquals(translate("pt-BR", "{count} linha(s)", { count: 3 }), "3 linha(s)");
  assertEquals(translate("en", "texto sem tradução"), "texto sem tradução");
  assertEquals(interpolate("{a} e {b}", { a: 1 }), "1 e {b}");
});
