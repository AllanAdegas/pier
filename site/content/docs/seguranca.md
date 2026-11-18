O Pier controla servidores inteiros. Vale a pena entender o que cada acesso permite e fechar o que não precisa ficar aberto.

## O que o Pier pode fazer

O Pier usa o socket do Docker do servidor local e acessa os outros servidores por SSH. Na prática, **quem administra o Pier administra os servidores**. Por isso:

- mantenha o painel atrás de HTTPS e não abra a porta 8700;
- use senhas fortes, com 10 caracteres ou mais;
- dê acesso só a quem já teria acesso root aos servidores.

O acesso é controlado por [papéis de projeto e de instância](/docs/equipe/). A capacidade do token é limitada também pelo papel atual do usuário. Contas com operações privilegiadas sobre Docker e servidores precisam continuar sendo confiáveis.

## Tokens de API

Em **Segurança › Chaves e tokens**, seção **Tokens de API**, crie tokens para scripts, CI e IA. Cada token tem uma permissão:

| Permissão | Pode |
|---|---|
| `read` | Consultar recursos, estados e logs. Não vê segredos |
| `deploy` | Tudo do `read`, mais implantar, reiniciar e operar |
| `write` | Configurar dentro do escopo permitido pelo papel. Operações de Docker/host autorizadas podem equivaler a root |

Dê a cada uso o mínimo necessário. Um pipeline de CI que só implanta precisa de `deploy`, não de `write`. O valor do token aparece uma única vez, ao ser criado.

## Ambientes protegidos e congelados

Em **Projetos**, cada ambiente tem duas proteções:

- **Proteger remoções:** para apagar uma aplicação, um banco, uma stack, o ambiente ou o projeto, é preciso digitar o **nome do ambiente**. Evita apagar produção achando que era staging.
- **Congelar deploys:** nenhum deploy entra no ambiente, por nenhum caminho: painel, webhook, CLI, MCP, tag ou promoção. Use em janelas em que nada pode mudar.

Para descongelar, é preciso escrever uma justificativa, que fica registrada na auditoria.

Na CLI, uma remoção em ambiente protegido exige `--confirm-environment <nome>`.

## Auditoria

Em **Segurança › Auditoria** fica o registro de quem fez o quê: logins, deploys, mudanças de configuração, remoções e ações por token ou por IA. Os dados sensíveis aparecem mascarados. O registro guarda 90 dias.

## Segredos

- Senhas, chaves SSH, tokens e variáveis ficam **cifrados** no banco do Pier.
- A chave fica em `secret.key`, no diretório de dados, ou em `PIER_SECRET_KEY`. **Faça backup dela:** o [backup da instância](/docs/backup/) já a inclui.
- Os valores das variáveis aparecem mascarados nos logs de deploy.
- Chaves privadas nunca são devolvidas pela API.

## Boas práticas

- [ ] Painel em HTTPS, com a URL pública em **Configurações**.
- [ ] Porta 8700 fechada no firewall.
- [ ] Backup da instância agendado e copiado para fora do servidor.
- [ ] Tokens com a menor permissão possível, um por uso.
- [ ] Produção com **Proteger remoções** ligado.
- [ ] Bancos sem porta pública, salvo necessidade real.
- [ ] Serviços do catálogo que pedem poderes especiais revisados antes de criar.
- [ ] Pier e Docker atualizados. Veja [Atualizar o Pier](/docs/atualizar/).

## Reportar uma vulnerabilidade

Encontrou uma falha de segurança? Não abra uma issue pública. Escreva para o mantenedor pelo GitHub do projeto, em [{{repo}}]({{repo}}), descrevendo o problema e como reproduzir.
