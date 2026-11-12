/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { certificateHosts } from "./domainCheck.ts";

Deno.test("only hosts that will request a certificate are checked, once each", () => {
  assertEquals(
    certificateHosts(["https://App.example.com/api", "app.example.com", "http://plain.example.com", "", " www.example.com ", "https://"]),
    ["app.example.com", "www.example.com"],
  );
});
