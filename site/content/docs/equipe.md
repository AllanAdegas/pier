O acesso combina o papel da pessoa e a capacidade do token. Ter um token `write` não aumenta o papel do usuário. A interface esconde ações indisponíveis, e o backend deve recusar a mesma operação quando chamada diretamente.

## Papéis e escopos

| Papel | Operações permitidas pelo papel |
|---|---|
| Somente leitura (`viewer`) | Consultar recursos autorizados |
| Desenvolvedor (`developer`) | Consultar e implantar |
| Administrador (`admin`) | Configurar recursos e administrar acessos dentro do limite do próprio papel |
| Proprietário (`owner`) | Administrar também proprietários e operações reservadas |

Um papel de **projeto** vale somente naquele projeto. Um papel de **instância** vale para todos os projetos e para recursos globais, como servidores e configurações. **Somente projetos** significa que a conta não tem papel global.

Na atualização de uma instalação anterior à 0.6, **todas as contas existentes recebem `owner` da instância**, preservando o acesso administrativo que já tinham, inclusive por tokens. Revise e reduza esses papéis depois de atualizar; não presuma que a migração restringiu usuários antigos. Em uma instalação nova, a primeira conta é proprietária. A instância e cada projeto conservam pelo menos um proprietário; uma pessoa não altera o próprio papel por esse fluxo.

Na tela de equipe, proprietários da instância encontram **Papéis na instância**. Conceda acesso global só quando necessário. Um projeto não é isolamento do kernel: recursos que permitem executar código, Docker privilegiado ou terminal exigem confiança no operador.

## Convites do projeto

Na tela **Equipe e permissões** do projeto:

1. Informe o e-mail de uma conta existente e escolha o menor papel necessário.
2. Clique em **Criar convite** e copie o token, mostrado uma vez. Envie por canal privado.
3. A pessoa entra com a conta desse e-mail e usa **Aceitar convite**.
4. Confira membros e convites. Convites expiram em 24 horas, são de uso único e podem ser revogados.

O convite não cria uma conta e não substitui silenciosamente o papel de um membro existente. Revogar o convite impede seu uso; para retirar acesso já concedido, remova o membro ou o papel correspondente. Revise também os acessos globais.

## SSO OpenID Connect

SSO autentica contas previamente vinculadas. E-mail igual não cria vínculo e não há cadastro automático por SSO. Preserve uma conta local com [recuperação preparada](/docs/conta/) para não perder acesso ao desativar ou mudar o provedor.

O operador configura na instalação as variáveis `PIER_OIDC_ISSUERS` (issuers HTTPS exatos separados por espaço) e `PIER_OIDC_CALLBACK` (URL HTTPS exata de retorno). Repasse-as ao processo/container do Pier; não basta tê-las no shell se o Compose não as encaminhar. O callback termina em `/api/v1/access/oidc/callback`.

Em **SSO OpenID Connect**, o proprietário informa issuer permitido, Client ID, Client secret e callback aprovado. Autoriza o par exato issuer + subject para o ID de uma conta existente. O botão **Entrar com SSO** inicia o login. HTTPS é obrigatório, inclusive para o cookie que vincula o retorno ao navegador.

Opcionalmente, configure o claim de grupos e mapeamentos, uma linha `grupo=papel` (`admin`, `developer` ou `viewer`). Confira o papel padrão antes de salvar: um papel de instância amplia o acesso para todos os projetos. SSO nunca concede nem remove `owner`. Sem mapeamento, mantenha os papéis no Pier. Para retirar uma identidade, use **Revogar identidade SSO** e revise as sessões da conta.

## Verificar antes de abrir acesso

A integração da 0.6 está em validação de QA. Teste com contas diferentes: leitura, deploy, escrita, tentativa de acessar outro projeto, operações globais, terminal e MCP. Serviços e telas implementados não equivalem a uma auditoria externa ou a isolamento entre cargas não confiáveis.
