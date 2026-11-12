/**
 * The launch page: what Pier is, how to install the stable release and where
 * to go next. Copy is pt-BR, written for someone who has never used it.
 */
import { escapeHtml } from "./markdown.ts";
import { repository } from "./config.ts";

interface Feature {
  title: string;
  text: string;
  href: string;
}

const features: Feature[] = [
  {
    title: "Deploy sem derrubar nada",
    text: "A versão nova sobe ao lado da atual e só assume quando está pronta. Se falhar, a anterior continua no ar.",
    href: "/docs/aplicacoes/",
  },
  {
    title: "Domínio e HTTPS sozinhos",
    text: "Informe o domínio: o Pier confere o DNS, configura o proxy e emite o certificado.",
    href: "/docs/servidores-e-dominios/",
  },
  {
    title: "Bancos com backup",
    text: "PostgreSQL, MySQL, MariaDB, MongoDB, Redis e mais, com senhas geradas, backup agendado e restauração testada.",
    href: "/docs/bancos-de-dados/",
  },
  {
    title: "Catálogo com 350+ serviços",
    text: "n8n, Plausible, Uptime Kuma, Supabase, WordPress… prontos para subir, ou cole seu próprio docker compose.",
    href: "/docs/stacks/",
  },
  {
    title: "Avisa quando algo dá errado",
    text: "Container em loop, falta de memória, disco cheio, certificado vencendo, backup que não rodou ou site fora do ar.",
    href: "/docs/alertas/",
  },
  {
    title: "Feito para automação",
    text: "CLI pierctl, pier.yaml no repositório, apply declarativo e um servidor MCP para agentes de IA.",
    href: "/docs/pierctl/",
  },
];

const steps = [
  { title: "Instale", text: "Um script sobe o Pier com Docker no seu servidor Linux." },
  { title: "Conecte", text: "Aponte um repositório Git, uma imagem Docker ou a pasta do seu computador." },
  { title: "Publique", text: "Dê um domínio, clique em Implantar e acompanhe o log ao vivo." },
];

function installBlock(tag: string): string {
  const command = `git clone --branch ${tag} --depth 1 ${repository}.git
cd pier
./install.sh`;
  return `<div class="code-block install">
  <span class="code-lang">terminal do servidor</span>
  <button class="copy" type="button" aria-label="Copiar">Copiar</button>
  <pre><code>${escapeHtml(command)}</code></pre>
</div>`;
}

function featureCards(): string {
  return features.map((feature) =>
    `<a class="card" href="${feature.href}"><h3>${escapeHtml(feature.title)}</h3><p>${escapeHtml(feature.text)}</p></a>`
  ).join("\n");
}

function stepList(): string {
  return steps.map((step, index) =>
    `<li><span class="step-number">${index + 1}</span><div><h3>${escapeHtml(step.title)}</h3><p>${
      escapeHtml(step.text)
    }</p></div></li>`
  ).join("\n");
}

export function homeBody(version: string): string {
  const tag = `v${version}`;
  return `<main id="conteudo" class="home">
<section class="hero">
  <div class="hero-text">
    <p class="eyebrow">Self-hosted · beta · v${escapeHtml(version)}</p>
    <h1>Seus projetos no ar,<br>no seu próprio servidor.</h1>
    <p class="hero-lead">O Pier é um painel para publicar aplicações, bancos de dados e serviços em uma VPS sua, com domínio, HTTPS, backup e deploy sem downtime. Sem mensalidade por projeto e sem precisar saber Docker a fundo.</p>
    <div class="hero-actions">
      <a class="button primary" href="/docs/instalacao/">Instalar agora</a>
      <a class="button" href="/docs/introducao/">Ler a documentação</a>
    </div>
  </div>
  <div class="hero-install">
    <p class="install-title">Instale a versão estável em um servidor Linux com Docker:</p>
    ${installBlock(tag)}
    <p class="install-note">Ao terminar, o script mostra o endereço do painel e o código para criar sua conta. <a href="/docs/instalacao/">Guia completo da instalação</a>.</p>
  </div>
</section>

<section class="section">
  <h2>Como funciona</h2>
  <ol class="steps">${stepList()}</ol>
</section>

<section class="section">
  <h2>O que vem pronto</h2>
  <div class="cards">${featureCards()}</div>
</section>

<section class="section split">
  <div>
    <h2>Do seu jeito: painel, terminal ou IA</h2>
    <p>Tudo o que o painel faz também está na CLI <code>pierctl</code>, pronta para scripts e pipelines de CI. Um agente de IA pode consultar logs e implantar pelo servidor MCP, com as permissões do token que você der a ele.</p>
    <p><a href="/docs/pierctl/">Conheça a pierctl →</a></p>
  </div>
  <div class="code-block">
    <span class="code-lang">sh</span>
    <button class="copy" type="button" aria-label="Copiar">Copiar</button>
    <pre><code>pierctl login https://pier.exemplo.com --token-stdin
pierctl app deploy api --wait
pierctl up            # envia a pasta atual e implanta</code></pre>
  </div>
</section>

<section class="section beta">
  <h2>Em beta, com transparência</h2>
  <p>A versão ${escapeHtml(version)} roda em produção desde novembro de 2026 e passou por testes de instalação, backup e restauração, deploy e rollback, segredos de build, promoção e alertas. Ainda é beta: teste o que for usar antes de colocar algo crítico, e faça o backup da instância logo no primeiro dia.</p>
  <p><a href="/docs/introducao/#o-que-ainda-nao-faz">O que o Pier ainda não faz →</a></p>
</section>
</main>`;
}
