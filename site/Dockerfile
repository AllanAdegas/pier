# Launch site of Pier: static pages built by Deno and served by the same
# small Deno server used locally (security headers, custom 404).
#
# Build from the repository root, because the site reads the release number
# from internal/version/version.go:
#   docker build -f site/Dockerfile -t pier-site .
#   docker run --rm -p 8080:8080 pier-site
ARG DENO_VERSION=2.9.5

FROM denoland/deno:${DENO_VERSION} AS build
WORKDIR /src/site
COPY site/deno.json site/deno.lock ./
COPY site/src/ src/
RUN deno cache --frozen src/build.ts src/serve.ts
COPY internal/version/version.go /src/internal/version/version.go
COPY site/content/ content/
COPY site/assets/ assets/
RUN deno task build

FROM denoland/deno:${DENO_VERSION}
WORKDIR /srv/site
COPY --from=build /deno-dir/ /deno-dir/
COPY site/deno.json site/deno.lock ./
COPY site/src/serve.ts src/serve.ts
COPY --from=build /src/site/dist/ dist/
USER deno
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
    CMD deno eval --no-check "const r = await fetch('http://127.0.0.1:8080/'); if (!r.ok) Deno.exit(1)" || exit 1
CMD ["deno", "run", "--cached-only", "--allow-net", "--allow-read=dist", "--allow-env=PORT", "src/serve.ts"]
