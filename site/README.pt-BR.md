[English](README.md)

# Site do Pier

Página de lançamento e documentação de uso, em português (pt-BR) e inglês. Páginas estáticas geradas por
Deno: o conteúdo fica em `content/docs/`, a ordem, os títulos e as descrições em `src/pages.ts`, os demais
textos em `src/strings.ts`, e a versão vem de `../internal/version/version.go`.

```sh
deno task serve     # gera dist/ e abre em http://localhost:8080
deno task test      # testes, incluindo links, âncoras e os pares de idioma
deno task build     # só gera dist/
```

## A regra dos idiomas

Toda página existe nos dois idiomas, na mesma mudança. A CI (`deno task test`) e o build falham quando falta
uma página em inglês ou quando ela não corresponde à fonte em português.

| | Português (fonte) | Inglês |
| --- | --- | --- |
| Início | `/` | `/en/` |
| Documentação | `/docs/<slug-pt>/` a partir de `content/docs/<slug-pt>.md` | `/en/docs/<slug-en>/` a partir de `content/docs/en/<slug-en>.md` |
| Títulos, descrições, nomes das seções | `src/pages.ts` (`pt`) | `src/pages.ts` (`en`) |
| Topo, rodapé, início, 404, rótulos de botões | `src/strings.ts` (`pt`) | `src/strings.ts` (`en`) |

Cada página aponta para a mesma página no outro idioma e declara as alternativas `hreflang` (`x-default` é o
inglês). A página 404 é uma só, com os dois idiomas.

O que os testes conferem em cada par:

- os dois arquivos existem, e não há arquivo que `pages.ts` não liste;
- os mesmos títulos (`##`, `###`, … na mesma ordem), os mesmos blocos de código (quantidade e linguagem) e os
  mesmos `{{marcadores}}`;
- nenhum título `#` nem front matter no corpo: título e descrição vêm de `pages.ts`;
- os mesmos links internos, reescritos para o inglês: `/docs/backup/#redis-keydb-e-dragonfly` vira
  `/en/docs/backups/#redis-keydb-and-dragonfly`, a âncora do título na mesma posição;
- nenhum link sai do idioma da página (só o seletor de idioma);
- todo link interno e toda âncora existem nos dois idiomas; nenhum marcador fica sem valor;
- `strings.ts` tem as mesmas chaves nos dois idiomas.

### Para criar ou mudar uma página

1. Escreva ou edite `content/docs/<slug-pt>.md` (português).
2. Escreva ou edite `content/docs/en/<slug-en>.md` com a mesma estrutura. Traduza o texto e os comentários
   dentro dos blocos de código; mantenha comandos, marcadores e código iguais. Aponte para páginas em inglês
   (`/en/docs/<slug-en>/#ancora-em-ingles`); a âncora é o título em minúsculas, sem acentos, com hífens
   (`## 5. Put the panel on a domain with HTTPS` → `#5-put-the-panel-on-a-domain-with-https`).
3. Para uma página nova, acrescente uma entrada em `src/pages.ts` com `slug`, `title` e `description` em `pt`
   e `en`, no mesmo lugar da ordem de leitura.
4. Rode `deno task test`.

### Enquanto uma tradução está em andamento

`SITE_ALLOW_MISSING_EN=1 deno task build` (ou `deno task build --allow-missing-en`) gera a página em inglês
que falta a partir do texto em português, com um aviso "Translation in progress". Serve só para o
desenvolvimento local: o build e os testes recusam a opção quando a variável `CI` existe, e a CI nunca a
usa. `SITE_ALLOW_MISSING_EN=1 deno task test` roda as verificações de paridade nos arquivos em inglês que já
existem.

## Markdown

Marcadores: `{{version}}`, `{{tag}}`, `{{repo}}` e `{{cliRepo}}`. Um marcador desconhecido ou um link interno
quebrado falham o build.

## Hospedagem

`SITE_BASE_PATH=/pier/ deno task build` ajusta todos os links locais para um site servido abaixo de um
prefixo (nos dois idiomas). Imagem para publicar (a partir da raiz do repositório):

```sh
docker build -f site/Dockerfile -t pier-site .
docker run --rm -p 8080:8080 pier-site
```
