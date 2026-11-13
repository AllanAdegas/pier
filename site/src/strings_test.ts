import { assert, assertEquals } from "@std/assert";
import { strings } from "./strings.ts";
import { pageById } from "./pages.ts";

/** "path:type" for every leaf, with array indexes, so both languages can be compared. */
function leaves(tree: unknown, path = ""): string[] {
  if (Array.isArray(tree)) return tree.flatMap((item, index) => leaves(item, `${path}[${index}]`));
  if (tree && typeof tree === "object") {
    return Object.entries(tree).flatMap(([key, value]) => leaves(value, path ? `${path}.${key}` : key));
  }
  return [`${path}:${typeof tree}`];
}

Deno.test("both strings objects have identical keys and value types", () => {
  assertEquals(leaves(strings.en), leaves(strings.pt));
});

Deno.test("no string is empty in either language", () => {
  for (const language of ["pt", "en"] as const) {
    const walk = (tree: unknown, path: string): void => {
      if (typeof tree === "string") assert(tree.trim() !== "", `${language}: ${path} is empty`);
      else if (typeof tree === "function") {
        assert(String(tree("x", "y")).trim() !== "", `${language}: ${path}`);
      } else if (tree && typeof tree === "object") {
        for (const [key, value] of Object.entries(tree)) walk(value, `${path}.${key}`);
      }
    };
    walk(strings[language], language);
  }
});

Deno.test("the home page makes the same claims in both languages: same cards, steps and links", () => {
  const pages = (language: "pt" | "en") => strings[language].home.features.map((feature) => feature.page);
  assertEquals(pages("en"), pages("pt"));
  for (const page of pages("pt")) pageById(page);
  assertEquals(strings.en.home.steps.length, strings.pt.home.steps.length);
  const tags = (html: string) => [...html.matchAll(/<\/?(\w+)/g)].map((match) => match[1]);
  const home = (language: "pt" | "en") => strings[language].home;
  assertEquals(tags(home("en").headlineHtml), tags(home("pt").headlineHtml));
  assertEquals(tags(home("en").installNoteHtml("/x/")), tags(home("pt").installNoteHtml("/x/")));
  assertEquals(tags(home("en").automationTextHtml), tags(home("pt").automationTextHtml));
  assertEquals(tags(home("en").licenseHtml("/x/")), tags(home("pt").licenseHtml("/x/")));
  assert(
    home("en").betaTextHtml("9.9.9").includes("9.9.9") && home("pt").betaTextHtml("9.9.9").includes("9.9.9"),
  );
  const commands = (example: string) => example.split("\n").map((line) => line.replace(/\s*#.*$/, ""));
  assertEquals(commands(home("en").automationExample), commands(home("pt").automationExample));
});
