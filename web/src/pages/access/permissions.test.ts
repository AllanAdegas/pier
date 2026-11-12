/// <reference lib="deno.ns" />
import { assertEquals } from "jsr:@std/assert@1";
import { accessSummary, refreshAccess, resetAccess } from "./permissions";

Deno.test("permission reset rejects stale requests from the previous session", async () => {
 const originalFetch = globalThis.fetch;
 let completeOld!: (r:Response)=>void;
 try {
  resetAccess();
  globalThis.fetch=()=>new Promise<Response>((resolve)=>{completeOld=resolve;});
  const old=refreshAccess();
  resetAccess();
  globalThis.fetch=()=>Promise.resolve(Response.json({instance_role:"viewer",projects:[]}));
  await refreshAccess();
  completeOld(Response.json({instance_role:"owner",projects:[]}));
  await old;
  assertEquals(accessSummary()?.instance_role,"viewer");
  resetAccess();
 } finally {globalThis.fetch=originalFetch;}
});
