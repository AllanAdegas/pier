/**
 * The documentation, in reading order. Each entry maps to
 * content/docs/<slug>.md and is published at /docs/<slug>/.
 */
export interface DocPage {
  slug: string;
  title: string;
  description: string;
}

export interface DocSection {
  title: string;
  pages: DocPage[];
}

export const sections: DocSection[] = [
  {
    title: "Comece aqui",
    pages: [
      {
        slug: "introducao",
        title: "O que é o Pier",
        description: "O que o Pier faz, para quem é e como funciona.",
      },
      {
        slug: "instalacao",
        title: "Instalação",
        description: "Do servidor vazio ao painel no ar, passo a passo.",
      },
      {
        slug: "primeiros-passos",
        title: "Primeiros passos",
        description: "Sua primeira aplicação no ar, com domínio e HTTPS.",
      },
    ],
  },
  {
    title: "Usar",
    pages: [
      {
        slug: "previews",
        title: "Previews de PR",
        description: "Revisar mudanças com ambientes temporários e controlar forks.",
      },
      {
        slug: "git-integracoes",
        title: "Git e GitHub App",
        description: "Compose do Git, credenciais e retorno dos deploys ao provedor.",
      },
      {
        slug: "aplicacoes",
        title: "Aplicações",
        description: "Deploy do código ou de uma imagem, sem derrubar o que está no ar.",
      },
      {
        slug: "build-e-configuracao",
        title: "Build e configuração",
        description: "Railpack, estágio de Dockerfile, variáveis e opções de execução.",
      },
      {
        slug: "multisservidor",
        title: "Registry e múltiplos servidores",
        description: "Build remoto e rollout sequencial com imagem por digest.",
      },
      {
        slug: "bancos-de-dados",
        title: "Bancos de dados",
        description: "PostgreSQL, MySQL, Redis e outros, com backup e restauração.",
      },
      {
        slug: "stacks",
        title: "Stacks e catálogo",
        description: "Mais de 350 serviços prontos ou o seu próprio docker compose.",
      },
      {
        slug: "servidores-e-dominios",
        title: "Servidores e domínios",
        description: "Outras máquinas por SSH, DNS, proxy e certificados.",
      },
      {
        slug: "alertas",
        title: "Alertas e monitoramento",
        description: "Saiba quando algo cai, enche ou vence antes do seu usuário.",
      },
    ],
  },
  {
    title: "Operar",
    pages: [
      {
        slug: "backup",
        title: "Backup e restauração",
        description: "Proteja a instância, bancos e volumes de aplicações.",
      },
      {
        slug: "conta",
        title: "Conta, 2FA e recuperação",
        description: "Proteja o login e prepare a recuperação de acesso.",
      },
      { slug: "equipe", title: "Equipe e SSO", description: "Papéis, convites e identidade corporativa." },
      {
        slug: "operacoes",
        title: "Terminal e manutenção",
        description: "Acesso privilegiado e limpeza controlada do servidor.",
      },
      {
        slug: "comunidade",
        title: "Licença e governança",
        description: "Apache-2.0, contribuição e responsabilidade humana.",
      },
      {
        slug: "seguranca",
        title: "Segurança e permissões",
        description: "Tokens, ambientes protegidos, auditoria e boas práticas.",
      },
      { slug: "atualizar", title: "Atualizar o Pier", description: "Trocar de versão sem perder dados." },
      {
        slug: "problemas-comuns",
        title: "Problemas comuns",
        description: "Os erros mais frequentes e como resolver cada um.",
      },
    ],
  },
  {
    title: "Automatizar",
    pages: [
      {
        slug: "pierctl",
        title: "CLI pierctl",
        description: "Comandos para scripts e CI, com limites das operações que exigem sessão.",
      },
      {
        slug: "pier-yaml",
        title: "pier.yaml e apply",
        description: "Configuração no repositório e infraestrutura declarativa.",
      },
      {
        slug: "api",
        title: "API HTTP e OpenAPI",
        description: "Contrato da API, autenticação e operações assíncronas.",
      },
      {
        slug: "ia-mcp",
        title: "IA e agentes (MCP)",
        description: "Deixe um agente de IA consultar e implantar com segurança.",
      },
    ],
  },
];

export const allPages: DocPage[] = sections.flatMap((section) => section.pages);

export function docPath(page: DocPage): string {
  return `/docs/${page.slug}/`;
}

/** The pages before and after, for the links at the bottom of a page. */
export function neighbours(slug: string): { previous?: DocPage; next?: DocPage } {
  const index = allPages.findIndex((page) => page.slug === slug);
  if (index < 0) throw new Error(`página desconhecida: ${slug}`);
  return { previous: allPages[index - 1], next: allPages[index + 1] };
}
