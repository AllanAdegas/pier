/**
 * Every text of the site that is not a Markdown body, once per language:
 * the page frame, the launch page, the 404 page and the labels that
 * assets/site.js reads from data attributes. Both objects must have the
 * same keys (strings_test.ts); the Strings interface enforces it at compile
 * time as well.
 *
 * Values ending in "Html" are trusted, authored HTML; the functions receive
 * values the caller has already escaped.
 */
import type { Language } from "./language.ts";

export interface Feature {
  title: string;
  text: string;
  /** Portuguese slug of the page the card links to. */
  page: string;
}

export interface Step {
  title: string;
  text: string;
}

export interface Strings {
  site: {
    homeTitle: string;
    description: string;
  };
  layout: {
    skipLink: string;
    menuOpen: string;
    menuClose: string;
    versionBadge: string;
    mainNav: string;
    navDocs: string;
    navInstall: string;
    themeToggle: string;
    /** This language's own name, shown on the switch that leads to it. */
    languageName: string;
    /** Short form of languageName, shown on narrow screens. */
    languageCode: string;
    /** Accessible name of the switch that leads to this language. */
    readInThisLanguage: string;
    footerStatus: (version: string) => string;
    footerNav: string;
    footerDocs: string;
    footerCode: string;
    footerReleases: string;
  };
  docs: {
    sidebar: string;
    onThisPage: string;
    pager: string;
    previous: string;
    next: string;
    translationInProgress: string;
  };
  code: {
    copy: string;
    copied: string;
  };
  notFound: {
    title: string;
    textHtml: (docsHref: string, homeHref: string) => string;
  };
  home: {
    eyebrow: (version: string) => string;
    headlineHtml: string;
    lead: string;
    installNow: string;
    readDocs: string;
    installTitle: string;
    installTerminal: string;
    installNoteHtml: (guideHref: string) => string;
    howItWorks: string;
    steps: Step[];
    included: string;
    features: Feature[];
    automationTitle: string;
    automationTextHtml: string;
    automationLink: string;
    automationExample: string;
    betaTitle: string;
    betaTextHtml: (version: string) => string;
    licenseHtml: (governanceHref: string) => string;
    notYetLink: string;
  };
}

const portuguese: Strings = {
  site: {
    homeTitle: "Pier — seus projetos no ar, no seu servidor",
    description:
      "Painel self-hosted para publicar aplicações, bancos e serviços no seu servidor, com domínio, HTTPS, backup e deploy com verificação de prontidão.",
  },
  layout: {
    skipLink: "Pular para o conteúdo",
    menuOpen: "Abrir o menu",
    menuClose: "Fechar o menu",
    versionBadge: "Versão atual (beta)",
    mainNav: "Principal",
    navDocs: "Documentação",
    navInstall: "Instalar",
    themeToggle: "Alternar tema claro ou escuro",
    languageName: "Português",
    languageCode: "PT",
    readInThisLanguage: "Ler esta página em português",
    footerStatus: (version) => `Pier ${version} · em beta · self-hosted`,
    footerNav: "Rodapé",
    footerDocs: "Documentação",
    footerCode: "Código",
    footerReleases: "Versões",
  },
  docs: {
    sidebar: "Documentação",
    onThisPage: "Nesta página",
    pager: "Navegação entre páginas",
    previous: "Anterior",
    next: "Próxima",
    translationInProgress:
      "Tradução em andamento: esta página aparece em inglês até a versão em português ficar pronta.",
  },
  code: {
    copy: "Copiar",
    copied: "Copiado",
  },
  notFound: {
    title: "Página não encontrada",
    textHtml: (docsHref, homeHref) =>
      `O endereço pode ter mudado. Comece pela <a href="${docsHref}">documentação</a> ou volte à <a href="${homeHref}">página inicial</a>.`,
  },
  home: {
    eyebrow: (version) => `Self-hosted · beta · v${version}`,
    headlineHtml: "Seus projetos no ar,<br>no seu próprio servidor.",
    lead:
      "O Pier é um painel para publicar aplicações, bancos de dados e serviços em uma VPS sua, com domínio, HTTPS, backup e deploy com troca após prontidão. Sem mensalidade por projeto e sem precisar saber Docker a fundo.",
    installNow: "Instalar agora",
    readDocs: "Ler a documentação",
    installTitle: "Instale a versão beta em um servidor Linux com Docker:",
    installTerminal: "terminal do servidor",
    installNoteHtml: (guideHref) =>
      `O instalador baixa a imagem pública do GHCR. Se o download falhar, use <code>PIER_BUILD=1 ./install.sh</code>. Ao terminar, o script mostra o endereço do painel e o código para criar sua conta. <a href="${guideHref}">Guia completo da instalação</a>.`,
    howItWorks: "Como funciona",
    steps: [
      { title: "Instale", text: "Um script sobe o Pier com Docker no seu servidor Linux." },
      {
        title: "Conecte",
        text: "Aponte um repositório Git, uma imagem Docker ou a pasta do seu computador.",
      },
      { title: "Publique", text: "Dê um domínio, clique em Implantar e acompanhe o log ao vivo." },
    ],
    included: "O que vem pronto",
    features: [
      {
        title: "Deploy com prontidão",
        text:
          "A versão nova sobe ao lado da atual e só assume quando está pronta. Se falhar, a anterior continua no ar.",
        page: "aplicacoes",
      },
      {
        title: "Domínio e HTTPS sozinhos",
        text: "Informe o domínio: o Pier confere o DNS, configura o proxy e emite o certificado.",
        page: "servidores-e-dominios",
      },
      {
        title: "Bancos com backup",
        text:
          "Bancos com senhas geradas, backup agendado e restauração. Volumes de aplicações e stacks têm backup próprio.",
        page: "bancos-de-dados",
      },
      {
        title: "Catálogo com 350+ serviços",
        text:
          "n8n, Plausible, Uptime Kuma, Supabase, WordPress… prontos para subir, ou cole seu próprio docker compose.",
        page: "stacks",
      },
      {
        title: "Avisa quando algo dá errado",
        text:
          "Container em loop, falta de memória, disco cheio, certificado vencendo, backup que não rodou ou site fora do ar.",
        page: "alertas",
      },
      {
        title: "Feito para automação",
        text:
          "CLI pierctl, pier.yaml no repositório, apply declarativo e um servidor MCP para agentes de IA.",
        page: "pierctl",
      },
    ],
    automationTitle: "Do seu jeito: painel, terminal ou IA",
    automationTextHtml:
      "Os fluxos de automação estão na CLI <code>pierctl</code>, pronta para scripts e pipelines de CI. Um agente de IA pode consultar logs e implantar pelo servidor MCP, com as permissões do token que você der a ele.",
    automationLink: "Conheça a pierctl →",
    automationExample: `pierctl login https://pier.exemplo.com --token-stdin
pierctl app deploy api --wait
pierctl up            # envia a pasta atual e implanta`,
    betaTitle: "Em beta, com transparência",
    betaTextHtml: (version) =>
      `A versão do código é ${version}. Estes guias acompanham o desenvolvimento da linha PIER 0.6; a tag publicada indicada acima pode ainda não conter os recursos novos. Confira as notas da release antes de instalar. As limitações de integração e validação aparecem em cada guia. Ainda é beta: teste o que for usar antes de colocar algo crítico, e faça o backup da instância logo no primeiro dia.`,
    licenseHtml: (governanceHref) =>
      `Licença Apache-2.0. A automação reduz manutenção rotineira; revisão humana, decisões de segurança e recuperação continuam necessárias. <a href="${governanceHref}">Licença e governança</a>.`,
    notYetLink: "O que o Pier ainda não faz →",
  },
};

const english: Strings = {
  site: {
    homeTitle: "Pier — your projects online, on your own server",
    description:
      "Self-hosted panel to publish applications, databases and services on your server, with a domain, HTTPS, backup and readiness-checked deploys.",
  },
  layout: {
    skipLink: "Skip to content",
    menuOpen: "Open the menu",
    menuClose: "Close the menu",
    versionBadge: "Current version (beta)",
    mainNav: "Main",
    navDocs: "Documentation",
    navInstall: "Install",
    themeToggle: "Toggle light or dark theme",
    languageName: "English",
    languageCode: "EN",
    readInThisLanguage: "Read this page in English",
    footerStatus: (version) => `Pier ${version} · in beta · self-hosted`,
    footerNav: "Footer",
    footerDocs: "Documentation",
    footerCode: "Code",
    footerReleases: "Releases",
  },
  docs: {
    sidebar: "Documentation",
    onThisPage: "On this page",
    pager: "Page navigation",
    previous: "Previous",
    next: "Next",
    translationInProgress:
      "Translation in progress: this page is shown in Portuguese until the English version is ready.",
  },
  code: {
    copy: "Copy",
    copied: "Copied",
  },
  notFound: {
    title: "Page not found",
    textHtml: (docsHref, homeHref) =>
      `The address may have changed. Start with the <a href="${docsHref}">documentation</a> or go back to the <a href="${homeHref}">home page</a>.`,
  },
  home: {
    eyebrow: (version) => `Self-hosted · beta · v${version}`,
    headlineHtml: "Your projects online,<br>on your own server.",
    lead:
      "Pier is a panel to publish applications, databases and services on a VPS of your own, with a domain, HTTPS, backup and deploys that switch over only once the new version is ready. No monthly fee per project and no need to know Docker in depth.",
    installNow: "Install now",
    readDocs: "Read the documentation",
    installTitle: "Install the beta on a Linux server with Docker:",
    installTerminal: "server terminal",
    installNoteHtml: (guideHref) =>
      `The installer downloads the public GHCR image. If the download fails, use <code>PIER_BUILD=1 ./install.sh</code>. When it finishes, the script shows the panel address and the code to create your account. <a href="${guideHref}">Full installation guide</a>.`,
    howItWorks: "How it works",
    steps: [
      { title: "Install", text: "A script starts Pier with Docker on your Linux server." },
      { title: "Connect", text: "Point to a Git repository, a Docker image or a folder on your computer." },
      { title: "Publish", text: "Set a domain, click Deploy and follow the log live." },
    ],
    included: "What comes ready",
    features: [
      {
        title: "Readiness-checked deploys",
        text:
          "The new version starts next to the current one and only takes over once it is ready. If it fails, the previous one stays online.",
        page: "aplicacoes",
      },
      {
        title: "Domain and HTTPS on their own",
        text: "Enter the domain: Pier checks the DNS, configures the proxy and issues the certificate.",
        page: "servidores-e-dominios",
      },
      {
        title: "Databases with backup",
        text:
          "Databases with generated passwords, scheduled backup and restore. Application and stack volumes have their own backup.",
        page: "bancos-de-dados",
      },
      {
        title: "Catalog with 350+ services",
        text:
          "n8n, Plausible, Uptime Kuma, Supabase, WordPress… ready to start, or paste your own docker compose.",
        page: "stacks",
      },
      {
        title: "Tells you when something goes wrong",
        text:
          "A container in a restart loop, low memory, a full disk, an expiring certificate, a backup that did not run or a site that is down.",
        page: "alertas",
      },
      {
        title: "Built for automation",
        text: "pierctl CLI, pier.yaml in the repository, declarative apply and an MCP server for AI agents.",
        page: "pierctl",
      },
    ],
    automationTitle: "Your way: panel, terminal or AI",
    automationTextHtml:
      "The automation flows are in the <code>pierctl</code> CLI, ready for scripts and CI pipelines. An AI agent can read logs and deploy through the MCP server, with the permissions of the token you give it.",
    automationLink: "Get to know pierctl →",
    automationExample: `pierctl login https://pier.exemplo.com --token-stdin
pierctl app deploy api --wait
pierctl up            # uploads the current folder and deploys`,
    betaTitle: "In beta, openly",
    betaTextHtml: (version) =>
      `The code version is ${version}. These guides follow the development of the PIER 0.6 line; the published tag shown above may not include the new features yet. Check the release notes before installing. The integration and validation limits are listed in each guide. It is still beta: test what you will use before putting anything critical on it, and back up the instance on the very first day.`,
    licenseHtml: (governanceHref) =>
      `Apache-2.0 license. Automation reduces routine maintenance; human review, security decisions and recovery are still needed. <a href="${governanceHref}">License and governance</a>.`,
    notYetLink: "What Pier does not do yet →",
  },
};

export const strings: Record<Language, Strings> = { pt: portuguese, en: english };
