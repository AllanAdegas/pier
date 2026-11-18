O Pier é um painel que você instala no **seu** servidor para publicar projetos nele. Você aponta um repositório Git, uma imagem Docker ou uma pasta do seu computador. O Pier constrói, sobe, coloca domínio e HTTPS, faz backup dos bancos e avisa quando algo dá errado.

É uma alternativa self-hosted a serviços como Heroku, Render e Vercel: os projetos rodam numa VPS que você já paga, sem mensalidade por aplicação.

## Para quem é

- **Quem está começando** e quer colocar um site, uma API ou um bot no ar sem aprender Docker, proxy reverso e certificados.
- **Devs e equipes pequenas** que mantêm várias aplicações em uma ou algumas VPS e querem deploy sem downtime, rollback e backup sem montar tudo à mão.
- **Quem automatiza**: há API, CLI (`pierctl`), configuração no repositório (`pier.yaml`) e um servidor MCP para agentes de IA.

## O que dá para publicar

| Tipo | Exemplos | Página |
|---|---|---|
| Aplicação | API Node, Python, Go, PHP, Ruby, Java; site estático; SPA em React ou Vue | [Aplicações](/docs/aplicacoes/) |
| Banco de dados | PostgreSQL, MySQL, MariaDB, MongoDB, Redis, KeyDB, Dragonfly, ClickHouse | [Bancos de dados](/docs/bancos-de-dados/) |
| Stack | Mais de 350 serviços prontos (n8n, Plausible, Uptime Kuma, WordPress…) ou o seu `docker-compose.yaml` | [Stacks e catálogo](/docs/stacks/) |

## Como funciona

O Pier é um único programa, escrito em Go, que roda num container Docker. Ele guarda tudo num banco SQLite próprio e opera o Docker dos servidores:

- **No servidor onde está instalado**, pelo socket do Docker.
- **Em outros servidores**, por SSH. Não precisa instalar nada neles além do Docker.

Na frente das aplicações, cada servidor tem um proxy (Traefik) gerenciado pelo Pier. Ele recebe o tráfego nas portas 80 e 443, encaminha cada domínio para o container certo e emite os certificados do Let's Encrypt.

Um deploy segue sempre o mesmo caminho:

1. O Pier baixa o código ou a imagem.
2. Constrói a imagem, se for código.
3. Roda o comando de release, se houver. Exemplo: as migrations do banco.
4. Sobe a versão nova **ao lado** da atual e espera ela ficar pronta.
5. Só então muda o tráfego e para a versão anterior.

Falhas antes da troca preservam a versão anterior. Em múltiplos servidores, a atualização é sequencial e a compensação pode exigir intervenção; migrations e efeitos externos não são desfeitos pelo rollback.

## Organização

- **Projeto**: agrupa o que pertence a um mesmo produto. Exemplo: "Loja".
- **Ambiente**: divide um projeto em `production`, `staging` e outros. Um ambiente pode ser **protegido** (para apagar algo, é preciso digitar o nome dele) ou **congelado** (não aceita deploys).
- **Recurso**: uma aplicação, um banco ou uma stack, sempre dentro de um ambiente e rodando num servidor.

## O que ainda não faz

O Pier está em **beta** (versão {{version}}). Antes de decidir, saiba o que fica de fora desta versão:

- [Múltiplos destinos](/docs/multisservidor/) usam rollout sequencial. DNS/balanceador e dados compartilhados ficam por sua conta.
- [Previews por pull request](/docs/previews/) são opcionais e executam código do repositório; forks exigem cuidado especial.
- Há [equipe e papéis por projeto](/docs/equipe/); valide a integração de autorização desta versão antes de abrir acesso a uma equipe.
- Há [histórico de métricas](/docs/alertas/#historico-de-metricas); lacunas não significam uso zero.
- Há fluxos de instalação de Docker por SSH e provisionamento Hetzner/DigitalOcean, ainda sujeitos à validação do ambiente. Veja [operações](/docs/operacoes/).
- Os serviços do catálogo são validados automaticamente, mas só alguns foram implantados de verdade. Teste o que for usar.

Kubernetes existe como opção avançada (importar um cluster ou criar um K3s), mas nada no fluxo comum depende dele.

## Próximo passo

Siga a [instalação](/docs/instalacao/): são uns 10 minutos num servidor com Docker.

## Licença e responsabilidade

O código usa Apache-2.0 com a Commons Clause: uso livre, inclusive comercial dentro da empresa; vender o Pier exige licença comercial. Veja [licença e governança](/docs/comunidade/). Automação reduz manutenção, mas não dispensa revisão humana.

Os guias novos acompanham o desenvolvimento da linha 0.6. A versão publicada {{version}} pode não conter todos esses recursos até a release correspondente; confira as notas da tag escolhida.
