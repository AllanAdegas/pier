Use uma senha exclusiva e HTTPS no painel. Tokens de API não substituem uma sessão do navegador para gerenciar a segurança da própria conta.

## Ativar 2FA

1. Entre no painel e abra **Segurança**, na seção da conta.
2. Inicie a configuração de autenticação em dois fatores. Leia o QR com um aplicativo autenticador, ou copie a chave manualmente. O QR é gerado localmente.
3. Informe o código de seis dígitos para confirmar.
4. Guarde os dez códigos de recuperação fora do servidor, em local seguro. Eles aparecem somente ao gerar; cada código funciona uma vez.
5. No próximo login, informe senha e depois o código do autenticador ou um código de recuperação.

Se o código falhar, sincronize o relógio do celular e do servidor. Um código já consumido não pode ser repetido. Regenerar os códigos de recuperação substitui o conjunto anterior; guarde o novo. Desativar 2FA exige senha e segundo fator.

## Recuperar a senha

**Esqueci minha senha** só aparece quando a instância tem URL pública e canal de e-mail configurados. O link expira em 30 minutos e funciona uma vez. A resposta não confirma se o e-mail existe. Redefinir a senha encerra sessões, mas mantém o 2FA: guarde os códigos de recuperação.

Em **Segurança › Sua conta › Sessões da conta**, confira IP, navegador e última atividade. Revogue sessões desconhecidas ou use a opção de encerrar as outras sessões. Não há localização geográfica garantida pelo IP.

## Recuperação offline pelo operador

Sem e-mail ou sem segundo fator, o operador com acesso aos dados pode usar o binário `pier` compatível com a instalação, com a instância parada. Os comandos recusam execução com o banco em uso. `DIRETORIO` deve conter o banco existente; não é uma nova instalação.

```sh
pier admin reset-password --data-dir DIRETORIO --email EMAIL --password-file ARQUIVO
pier admin disable-2fa --data-dir DIRETORIO --email EMAIL
```

Crie o arquivo de senha com acesso restrito, sem senha no argumento ou em variável de ambiente, e remova-o após o uso. Execute apenas o comando necessário. Desativar 2FA também revoga sessões. Esse acesso offline tem poder administrativo; proteja o host e os backups.

Para login corporativo, veja [Equipe e SSO](/docs/equipe/).
