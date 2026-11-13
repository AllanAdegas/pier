/**
 * The documentation, in reading order (the same in both languages). Each
 * entry maps to content/docs/<pt-slug>.md, published at /docs/<pt-slug>/,
 * and to content/docs/en/<en-slug>.md, published at /en/docs/<en-slug>/.
 * The Portuguese slug is the page's stable identity.
 */
import type { Language, Localized } from "./language.ts";

export interface DocPage {
  slug: Localized;
  title: Localized;
  description: Localized;
}

export interface DocSection {
  title: Localized;
  pages: DocPage[];
}

export const sections: DocSection[] = [
  {
    title: { pt: "Comece aqui", en: "Start here" },
    pages: [
      {
        slug: { pt: "introducao", en: "introduction" },
        title: { pt: "O que é o Pier", en: "What Pier is" },
        description: {
          pt: "O que o Pier faz, para quem é e como funciona.",
          en: "What Pier does, who it is for and how it works.",
        },
      },
      {
        slug: { pt: "instalacao", en: "installation" },
        title: { pt: "Instalação", en: "Installation" },
        description: {
          pt: "Do servidor vazio ao painel no ar, passo a passo.",
          en: "From an empty server to a running panel, step by step.",
        },
      },
      {
        slug: { pt: "primeiros-passos", en: "getting-started" },
        title: { pt: "Primeiros passos", en: "Getting started" },
        description: {
          pt: "Sua primeira aplicação no ar, com domínio e HTTPS.",
          en: "Your first application online, with a domain and HTTPS.",
        },
      },
    ],
  },
  {
    title: { pt: "Usar", en: "Use" },
    pages: [
      {
        slug: { pt: "previews", en: "previews" },
        title: { pt: "Previews de PR", en: "PR previews" },
        description: {
          pt: "Revisar mudanças com ambientes temporários e controlar forks.",
          en: "Review changes in temporary environments and control forks.",
        },
      },
      {
        slug: { pt: "git-integracoes", en: "git-integrations" },
        title: { pt: "Git e GitHub App", en: "Git and GitHub App" },
        description: {
          pt: "Compose do Git, credenciais e retorno dos deploys ao provedor.",
          en: "Compose from Git, credentials and deploy status sent back to the provider.",
        },
      },
      {
        slug: { pt: "aplicacoes", en: "applications" },
        title: { pt: "Aplicações", en: "Applications" },
        description: {
          pt: "Deploy do código ou de uma imagem, sem derrubar o que está no ar.",
          en: "Deploy from code or from an image without taking down what is running.",
        },
      },
      {
        slug: { pt: "build-e-configuracao", en: "build-and-configuration" },
        title: { pt: "Build e configuração", en: "Build and configuration" },
        description: {
          pt: "Railpack, estágio de Dockerfile, variáveis e opções de execução.",
          en: "Railpack, Dockerfile stage, variables and runtime options.",
        },
      },
      {
        slug: { pt: "multisservidor", en: "multi-server" },
        title: { pt: "Registry e múltiplos servidores", en: "Registry and multiple servers" },
        description: {
          pt: "Build remoto e rollout sequencial com imagem por digest.",
          en: "Remote build and sequential rollout with the image pinned by digest.",
        },
      },
      {
        slug: { pt: "bancos-de-dados", en: "databases" },
        title: { pt: "Bancos de dados", en: "Databases" },
        description: {
          pt: "PostgreSQL, MySQL, Redis e outros, com backup e restauração.",
          en: "PostgreSQL, MySQL, Redis and others, with backup and restore.",
        },
      },
      {
        slug: { pt: "stacks", en: "stacks" },
        title: { pt: "Stacks e catálogo", en: "Stacks and catalog" },
        description: {
          pt: "Mais de 350 serviços prontos ou o seu próprio docker compose.",
          en: "More than 350 ready-made services, or your own docker compose.",
        },
      },
      {
        slug: { pt: "servidores-e-dominios", en: "servers-and-domains" },
        title: { pt: "Servidores e domínios", en: "Servers and domains" },
        description: {
          pt: "Outras máquinas por SSH, DNS, proxy e certificados.",
          en: "Other machines over SSH, DNS, proxy and certificates.",
        },
      },
      {
        slug: { pt: "alertas", en: "alerts" },
        title: { pt: "Alertas e monitoramento", en: "Alerts and monitoring" },
        description: {
          pt: "Saiba quando algo cai, enche ou vence antes do seu usuário.",
          en: "Know when something goes down, fills up or expires before your users do.",
        },
      },
    ],
  },
  {
    title: { pt: "Operar", en: "Operate" },
    pages: [
      {
        slug: { pt: "backup", en: "backups" },
        title: { pt: "Backup e restauração", en: "Backup and restore" },
        description: {
          pt: "Proteja a instância, bancos e volumes de aplicações.",
          en: "Protect the instance, databases and application volumes.",
        },
      },
      {
        slug: { pt: "conta", en: "account-security" },
        title: { pt: "Conta, 2FA e recuperação", en: "Account, 2FA and recovery" },
        description: {
          pt: "Proteja o login e prepare a recuperação de acesso.",
          en: "Protect your sign-in and prepare access recovery.",
        },
      },
      {
        slug: { pt: "equipe", en: "team-and-access" },
        title: { pt: "Equipe e SSO", en: "Team and SSO" },
        description: {
          pt: "Papéis, convites e identidade corporativa.",
          en: "Roles, invitations and corporate identity.",
        },
      },
      {
        slug: { pt: "operacoes", en: "server-operations" },
        title: { pt: "Terminal e manutenção", en: "Terminal and maintenance" },
        description: {
          pt: "Acesso privilegiado e limpeza controlada do servidor.",
          en: "Privileged access and controlled server cleanup.",
        },
      },
      {
        slug: { pt: "comunidade", en: "community" },
        title: { pt: "Licença e governança", en: "License and governance" },
        description: {
          pt: "Apache-2.0, contribuição e responsabilidade humana.",
          en: "Apache-2.0, contributing and human responsibility.",
        },
      },
      {
        slug: { pt: "seguranca", en: "security" },
        title: { pt: "Segurança e permissões", en: "Security and permissions" },
        description: {
          pt: "Tokens, ambientes protegidos, auditoria e boas práticas.",
          en: "Tokens, protected environments, audit log and good practices.",
        },
      },
      {
        slug: { pt: "atualizar", en: "upgrading" },
        title: { pt: "Atualizar o Pier", en: "Upgrading Pier" },
        description: {
          pt: "Trocar de versão sem perder dados.",
          en: "Change versions without losing data.",
        },
      },
      {
        slug: { pt: "problemas-comuns", en: "troubleshooting" },
        title: { pt: "Problemas comuns", en: "Common problems" },
        description: {
          pt: "Os erros mais frequentes e como resolver cada um.",
          en: "The most frequent errors and how to fix each one.",
        },
      },
    ],
  },
  {
    title: { pt: "Automatizar", en: "Automate" },
    pages: [
      {
        slug: { pt: "pierctl", en: "pierctl" },
        title: { pt: "CLI pierctl", en: "pierctl CLI" },
        description: {
          pt: "Comandos para scripts e CI, com limites das operações que exigem sessão.",
          en: "Commands for scripts and CI, with the limits of operations that need a session.",
        },
      },
      {
        slug: { pt: "pier-yaml", en: "pier-yaml" },
        title: { pt: "pier.yaml e apply", en: "pier.yaml and apply" },
        description: {
          pt: "Configuração no repositório e infraestrutura declarativa.",
          en: "Configuration in the repository and declarative infrastructure.",
        },
      },
      {
        slug: { pt: "api", en: "api" },
        title: { pt: "API HTTP e OpenAPI", en: "HTTP API and OpenAPI" },
        description: {
          pt: "Contrato da API, autenticação e operações assíncronas.",
          en: "API contract, authentication and asynchronous operations.",
        },
      },
      {
        slug: { pt: "ia-mcp", en: "ai-mcp" },
        title: { pt: "IA e agentes (MCP)", en: "AI and agents (MCP)" },
        description: {
          pt: "Deixe um agente de IA consultar e implantar com segurança.",
          en: "Let an AI agent query and deploy safely.",
        },
      },
    ],
  },
];

export const allPages: DocPage[] = sections.flatMap((section) => section.pages);

/** "/docs/<pt-slug>/" or "/en/docs/<en-slug>/". */
export function docPath(page: DocPage, language: Language): string {
  return language === "pt" ? `/docs/${page.slug.pt}/` : `/en/docs/${page.slug.en}/`;
}

/** The source file, relative to content/docs/. */
export function contentFile(page: DocPage, language: Language): string {
  return language === "pt" ? `${page.slug.pt}.md` : `en/${page.slug.en}.md`;
}

/** The page whose Portuguese slug is given; the Portuguese slug is the stable id. */
export function pageById(ptSlug: string): DocPage {
  const page = allPages.find((candidate) => candidate.slug.pt === ptSlug);
  if (!page) throw new Error(`página desconhecida: ${ptSlug}`);
  return page;
}

/** The pages before and after, for the links at the bottom of a page. */
export function neighbours(page: DocPage): { previous?: DocPage; next?: DocPage } {
  const index = allPages.indexOf(page);
  if (index < 0) throw new Error(`página desconhecida: ${page.slug.pt}`);
  return { previous: allPages[index - 1], next: allPages[index + 1] };
}
