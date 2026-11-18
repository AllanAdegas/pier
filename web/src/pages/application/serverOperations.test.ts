import { assertEquals } from "jsr:@std/assert@1";
import { serverOperationPath } from "./serverOperations.ts";
Deno.test("server operations stay application scoped and encode server selection", () => {
  assertEquals(serverOperationPath("app", "server&other=1", "terminal"), "/applications/app/terminal?server_id=server%26other%3D1");
});
