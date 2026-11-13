A API HTTP do Pier usa o prefixo `/api/v1`. O contrato versionado fica em [api/openapi.json]({{repo}}/blob/main/api/openapi.json). O campo `info.version` é a versão do contrato (`v1`), não a do servidor. Use o arquivo da **mesma tag** do servidor, não o `main` de outra versão.

## Autenticação e permissões

Integrações usam `Authorization: Bearer TOKEN`. O papel do usuário limita também a capacidade do token. Uma resposta 403 não deve ser contornada com IDs de outro projeto ou credenciais de outra conta.

As operações que exigem sessão do navegador, como segurança da conta e gerenciamento de identidades, não aceitam um token `write` como substituto. Mutações por cookie exigem o cabeçalho `X-Pier-Client`, além das proteções do próprio fluxo. Não copie cookies do navegador para scripts de CI.

## Respostas e operações longas

Confira método, status e corpo definidos para cada rota. Um `202` normalmente significa que o trabalho foi aceito, não que o deploy ou backup terminou. Consulte o estado ou acompanhe logs até a conclusão. Uma falha de release ou de prontidão deve ser tratada como falha da operação, mesmo quando uma versão anterior continua respondendo.

Erros têm código e mensagem; não use o texto traduzido como identificador permanente. Não registre corpos de autenticação, tokens, senhas ou arquivos de backup nos logs do seu cliente. O contrato pode precisar de campos específicos do Docker ou de provedores; confira as notas de cobertura da versão antes de gerar um SDK automaticamente.

Para scripts comuns, prefira a [pierctl](/docs/pierctl/), que já trata contexto, saída, confirmação e códigos de erro. Para agentes de IA, há [MCP](/docs/ia-mcp/), com um conjunto próprio de ferramentas, não um espelho irrestrito de toda a API.
