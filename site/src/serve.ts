/**
 * Serves dist/ with security headers and the custom 404 page. Used locally
 * (deno task serve) and by the container image.
 */
import { serveDir } from "@std/http/file-server";

const root = new URL("../dist/", import.meta.url).pathname;
const port = Number(Deno.env.get("PORT") ?? "8080");

const securityHeaders: Record<string, string> = {
  "Content-Security-Policy":
    "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'",
  "X-Content-Type-Options": "nosniff",
  "Referrer-Policy": "strict-origin-when-cross-origin",
  "Permissions-Policy": "camera=(), microphone=(), geolocation=()",
};

const notFoundPage = await Deno.readTextFile(`${root}404.html`);

function withHeaders(response: Response, cache: string): Response {
  const headers = new Headers(response.headers);
  for (const [name, value] of Object.entries(securityHeaders)) headers.set(name, value);
  headers.set("Cache-Control", cache);
  return new Response(response.body, { status: response.status, headers });
}

export async function handle(request: Request): Promise<Response> {
  const response = await serveDir(request, { fsRoot: root, quiet: true, showDirListing: false });
  if (response.status === 404) {
    return withHeaders(
      new Response(notFoundPage, { status: 404, headers: { "Content-Type": "text/html; charset=utf-8" } }),
      "no-cache",
    );
  }
  const path = new URL(request.url).pathname;
  return withHeaders(response, path.startsWith("/assets/") ? "public, max-age=3600" : "no-cache");
}

if (import.meta.main) {
  Deno.serve({ port, hostname: "0.0.0.0" }, handle);
}
