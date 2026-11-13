/**
 * Reads the Markdown sources. Every page exists twice: content/docs/<pt-slug>.md
 * and content/docs/en/<en-slug>.md. A missing English file fails the build;
 * SITE_ALLOW_MISSING_EN=1 (or --allow-missing-en) lets local development
 * render those pages from the Portuguese text instead, and is refused in CI.
 */
import { allPages, contentFile, type DocPage } from "./pages.ts";
import type { Language } from "./language.ts";

export const contentDir = new URL("../content/docs/", import.meta.url);

export const allowMissingEnglishVariable = "SITE_ALLOW_MISSING_EN";
export const allowMissingEnglishFlag = "--allow-missing-en";

export class MissingTranslationError extends Error {
  constructor(readonly files: string[]) {
    super(
      `${files.length} English page(s) missing:\n  ${
        files.map((file) => `content/docs/${file}`).join("\n  ")
      }\n` +
        "Every page in src/pages.ts needs its English file (same ## and ### headings, code blocks and " +
        `placeholders as the Portuguese one). For local development only: ${allowMissingEnglishVariable}=1 ` +
        `or deno task build ${allowMissingEnglishFlag}.`,
    );
    this.name = "MissingTranslationError";
  }
}

export class AllowMissingRefused extends Error {}

export async function exists(url: URL): Promise<boolean> {
  try {
    return (await Deno.stat(url)).isFile;
  } catch (error) {
    if (error instanceof Deno.errors.NotFound) return false;
    throw error;
  }
}

/** Whether the page's source file exists in that language. */
export function hasSource(page: DocPage, language: Language, dir = contentDir): Promise<boolean> {
  return exists(new URL(contentFile(page, language), dir));
}

export function readSource(page: DocPage, language: Language, dir = contentDir): Promise<string> {
  return Deno.readTextFile(new URL(contentFile(page, language), dir));
}

/** English source files (relative to content/docs/) that pages.ts expects but do not exist. */
export async function missingEnglishFiles(dir = contentDir): Promise<string[]> {
  const missing: string[] = [];
  for (const page of allPages) {
    if (!(await hasSource(page, "en", dir))) missing.push(contentFile(page, "en"));
  }
  return missing;
}

/** Markdown files present in a folder of content/docs/ (non-recursive, dotfiles ignored). */
export async function markdownFiles(dir: URL): Promise<string[]> {
  const names: string[] = [];
  try {
    for await (const entry of Deno.readDir(dir)) {
      if (entry.isFile && entry.name.endsWith(".md") && !entry.name.startsWith(".")) names.push(entry.name);
    }
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) throw error;
  }
  return names.sort();
}

function readVariable(name: string): string {
  try {
    return Deno.env.get(name) ?? "";
  } catch {
    return "";
  }
}

/**
 * Whether missing English pages may fall back to Portuguese. Only for local
 * development: refused when the CI variable is set, so CI never relies on it.
 */
export function allowMissingEnglish(args: readonly string[] = []): boolean {
  const allowed = readVariable(allowMissingEnglishVariable) === "1" || args.includes(allowMissingEnglishFlag);
  const ci = readVariable("CI");
  if (allowed && ci !== "" && ci !== "false") {
    throw new AllowMissingRefused(
      `${allowMissingEnglishVariable} is for local development only and is refused in CI.`,
    );
  }
  return allowed;
}
