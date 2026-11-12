/// <reference lib="deno.ns" />
import { assertEquals, assertThrows } from "jsr:@std/assert@1";
import { withSuggestedPort } from "./portSuggestion.ts";
import type { ApplicationConfig } from "../../lib/types.ts";
Deno.test("port correction preserves domains, publications and the rest of the current config", () => {
 const config = { network: { port: 3000, domains: ["https://api.example"], port_mappings: ["8000:8000"] }, release: { command: "migrate" } } as ApplicationConfig;
 const result = withSuggestedPort(config, 8080);
 assertEquals(result, { ...config, network: { ...config.network, port: 8080 } });
 assertEquals(config.network.port, 3000);
 for (const port of [0, 65536, 1.5, NaN]) assertThrows(() => withSuggestedPort(config, port), RangeError);
});
