/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { formatBytes } from "./format.ts";

Deno.test("sizes use the decimal separator of the language", () => {
  assertEquals(formatBytes(1536, "en-US"), "1.5 KB");
  assertEquals(formatBytes(1536, "pt-BR"), "1,5 KB");
  assertEquals(formatBytes(20 * 1024 ** 3, "pt-BR"), "20 GB");
  assertEquals(formatBytes(1023, "en-US"), "1023 B");
  assertEquals(formatBytes(0, "pt-BR"), "0 B");
});
