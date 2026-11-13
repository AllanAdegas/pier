// Links from the panel to the Pier website documentation. The Portuguese
// pages live at /docs/<pt-slug>/ and their English mirror at
// /en/docs/<en-slug>/; every link goes through docsUrl so it follows the
// interface language and the slug map stays in one place.

import { language, type Language } from "../i18n";

export const DOCS_SITE = "https://allanadegas.github.io/pier/";

/** Portuguese page slug → English page slug, as published by the website. */
export const DOCS_PAGES = {
  "introducao": "introduction",
  "instalacao": "installation",
  "primeiros-passos": "getting-started",
  "previews": "previews",
  "git-integracoes": "git-integrations",
  "aplicacoes": "applications",
  "build-e-configuracao": "build-and-configuration",
  "bancos-de-dados": "databases",
  "stacks": "stacks",
  "servidores-e-dominios": "servers-and-domains",
  "multisservidor": "multi-server",
  "alertas": "alerts",
  "backup": "backups",
  "operacoes": "server-operations",
  "equipe": "team-and-access",
  "conta": "account-security",
  "seguranca": "security",
  "atualizar": "upgrading",
  "pier-yaml": "pier-yaml",
  "pierctl": "pierctl",
  "ia-mcp": "ai-mcp",
  "api": "api",
  "comunidade": "community",
  "problemas-comuns": "troubleshooting",
} as const;

export type DocsPage = keyof typeof DOCS_PAGES;

/** Address of a documentation page in the given language (the interface's by default). */
export function docsUrl(page: DocsPage, target: Language = language): string {
  return target === "en" ? `${DOCS_SITE}en/docs/${DOCS_PAGES[page]}/` : `${DOCS_SITE}docs/${page}/`;
}
