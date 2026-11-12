Um papel define o que uma pessoa pode fazer em um projeto. Ele é separado da capacidade de um token de API: um token não deve ampliar o acesso do usuário.

## Papéis

| Papel | Uso esperado |
|---|---|
| Somente leitura (`viewer`) | Consultar recursos permitidos |
| Desenvolvedor (`developer`) | Consultar e implantar |
| Administrador (`admin`) | Configurar recursos e administrar convites |
| Proprietário (`owner`) | Administrar também os proprietários do projeto |

O proprietário da instância administra as configurações globais. Na migração, a conta mais antiga recebe esse papel; confira qual conta foi escolhida antes de liberar acesso. Outras contas existentes precisam de concessões explícitas. Não há transferência de proprietário da instância neste fluxo.

## Convidar alguém

Na tela **Equipe e permissões** do projeto (`/projects/ID/access`):

1. Informe o e-mail de uma conta já existente e escolha o menor papel necessário.
2. Clique em **Criar convite**. Copie o token agora: aparece uma vez. Envie por um canal privado.
3. A pessoa entra com a conta do mesmo e-mail e usa **Aceitar convite** com esse token.
4. Confira a lista de membros. Convites expiram em 24 horas, são de uso único e podem ser revogados.

Convites não cadastram contas e não substituem o papel de um membro existente. Só um proprietário pode conceder ou mudar `owner`. A proteção impede remover o último proprietário do projeto e alterar o próprio papel.

## SSO OpenID Connect

SSO é login por um provedor de identidade, não uma forma de criar contas automaticamente. Somente o proprietário da instância configura **SSO OpenID Connect**.

1. O operador deve configurar um issuer permitido e callback HTTPS exato no servidor. A disponibilidade depende dessa integração; não basta preencher a tela.
2. Cadastre no provedor o callback `/api/v1/access/oidc/callback` na origem HTTPS do painel.
3. Na tela, informe issuer, Client ID, Client secret e callback aprovados.
4. Autorize o par exato issuer + subject para o ID de uma conta existente. E-mail igual, mesmo verificado, não cria vínculo.
5. Use **Entrar com SSO**. Para retirar acesso, revogue a identidade e revise as sessões da conta.

SSO exige HTTPS, inclusive para o cookie de vinculação do navegador. Preserve uma conta local com [recuperação preparada](/docs/conta/) antes de testar ou desativar o provedor.

## Limite de integração nesta beta

As notas de implementação registram pendências de autorização transversal, filtros de listas e navegação. A presença da tela ou testes do serviço não prova isolamento completo de todos os endpoints. Antes de abrir a instância para usuários com menor confiança, valide na versão integrada leitura, deploy, escrita e acesso entre projetos, inclusive terminal, Docker, tokens e MCP. Não trate um projeto como fronteira de isolamento de host.
