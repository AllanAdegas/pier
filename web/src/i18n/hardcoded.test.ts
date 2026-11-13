/// <reference lib="deno.ns" />
// Guard for the bilingual rule: Portuguese written in the interface code must
// go through t(), msg() or <Trans text>, so the English catalog sees it. The
// scan strips comments and translated calls, then reports what is left that
// reads as Portuguese, with file:line. Deliberate exceptions live in
// untranslated.ts, each with its reason.
import { assertEquals } from "jsr:@std/assert@1";
import { UNTRANSLATED } from "./untranslated.ts";

const SOURCE_ROOT = new URL("../", import.meta.url);

const accented = /[ãõçáéíóúâêôàÃÕÇÁÉÍÓÚÂÊÔÀ]/;
// Words that only Portuguese uses, checked in JSX text, where an English
// interface would not have them either.
const portugueseWord = /(?:^|\s)(em|para|com|sem|uma?|ao|aos|pelos?|pelas?|nenhuma?|ou|que|dos?|das?|criad[oa]s?|nunca)(?=\s|$)/i;

const translated = [
  /\b(?:t|msg)\(\s*"(?:\\.|[^"\\])*"/g,
  /<Trans\s+text="[^"]*"/g,
];

/** Keeps line breaks so positions still point at the original lines. */
const blank = (text: string) => text.replace(/[^\n]/g, " ");

/** The code with comments and translated texts blanked out. */
export function untranslatedCode(code: string): string {
  let rest = code.replace(/\/\*[\s\S]*?\*\//g, blank);
  for (const call of translated) rest = rest.replace(call, blank);
  return rest.replace(/(^|[^:"'`\\])\/\/.*$/gm, (whole, before: string) => before + blank(whole.slice(before.length)));
}

/** Lines of code that still read as Portuguese, as [line number, text]. */
export function portugueseLines(code: string): [number, string][] {
  const found: [number, string][] = [];
  untranslatedCode(code).split("\n").forEach((line, index) => {
    const jsxTexts = [...line.matchAll(/>([^<>{}]*)(?=[<{])/g)].map((match) => match[1]);
    if (accented.test(line) || jsxTexts.some((text) => portugueseWord.test(text))) found.push([index + 1, line.trim()]);
  });
  return found;
}

async function* walk(dir: URL, relative = ""): AsyncGenerator<[URL, string]> {
  for await (const entry of Deno.readDir(dir)) {
    const path = relative + entry.name;
    if (entry.isDirectory) {
      yield* walk(new URL(entry.name + "/", dir), path + "/");
    } else if (/\.tsx?$/.test(entry.name) && !entry.name.includes(".test.") && !/^i18n\/(en[^/]*|untranslated)\.ts$/.test(path)) {
      yield [new URL(entry.name, dir), path];
    }
  }
}

Deno.test("no Portuguese outside t(), msg() or <Trans text>", async () => {
  const allowed = new Set(UNTRANSLATED.map((entry) => `${entry.file}: ${entry.text}`));
  const found: string[] = [];
  for await (const [url, path] of walk(SOURCE_ROOT)) {
    for (const [line, text] of portugueseLines(await Deno.readTextFile(url))) {
      if (!allowed.has(`${path}: ${text}`)) found.push(`${path}:${line}: ${text}`);
    }
  }
  assertEquals(found, [], "wrap these texts in t() and add them to the English catalog, or justify them in untranslated.ts");
});

Deno.test("every allowed exception is still in the code", async () => {
  const stale = [];
  for (const entry of UNTRANSLATED) {
    const code = await Deno.readTextFile(new URL(entry.file, SOURCE_ROOT));
    if (!code.split("\n").some((line) => line.trim() === entry.text)) stale.push(entry.file + ": " + entry.text);
  }
  assertEquals(stale, []);
});

Deno.test("the guard sees attributes, JSX text and messages, and skips translated texts and comments", () => {
  const code = [
    `<Input placeholder="Nome da aplicação" />`,
    `<span>· criado em </span>`,
    `toast("Variável salva");`,
    `<p>{t("Implantação concluída")}</p>`,
    `// Comentário em português não é texto de tela`,
    `<Trans text="Abra o {link} para continuar" values={{}} />`,
    `const url = "https://example.com"; // é só um exemplo`,
    `<span>Docker · build</span>`,
  ].join("\n");
  assertEquals(portugueseLines(code).map(([line]) => line), [1, 2, 3]);
});
