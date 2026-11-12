# Site do Pier

Site de lançamento com a documentação de uso. Páginas estáticas geradas por Deno: o conteúdo fica em `content/docs/*.md`, a ordem e os títulos em `src/pages.ts`, e a versão vem de `../internal/version/version.go`.

```sh
deno task serve     # gera dist/ e abre em http://localhost:8080
deno task test      # testes, incluindo links e âncoras internas
deno task build     # só gera dist/
```

Marcadores no Markdown: `{{version}}`, `{{tag}}`, `{{repo}}` e `{{cliRepo}}`. Um marcador desconhecido ou um link interno quebrado falham o build.

Imagem para publicar (a partir da raiz do repositório):

```sh
docker build -f site/Dockerfile -t pier-site .
docker run --rm -p 8080:8080 pier-site
```
