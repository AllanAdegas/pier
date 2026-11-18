## Licença e acesso

O Pier tem **código-fonte disponível**, sob a licença **Apache-2.0 com a Commons Clause**. Na prática:

- **Pode, sem pedir nem pagar:** usar na sua empresa, inclusive para fins comerciais; implantar com ele os seus sistemas e os que você desenvolve para clientes, desde que eles não recebam acesso ao Pier; estudar, modificar, fazer fork e distribuir gratuitamente.
- **Precisa de licença comercial, com royalties:** vender o Pier ou algo cujo valor venha dele, mesmo com poucas alterações. Isso inclui revenda, versão com outra marca (*white label*), painel hospedado ou gerenciado pago e suporte ou consultoria paga do próprio Pier.

Não é "open source" na definição da OSI, porque a venda depende de licença comercial. Versões até a 0.9.0 continuam sob Apache-2.0 pura. Leia [LICENSE]({{repo}}/blob/main/LICENSE), [COMMERCIAL]({{repo}}/blob/main/COMMERCIAL.pt-BR.md), [NOTICE]({{repo}}/blob/main/NOTICE) e os avisos de dependências antes de redistribuir.

## Contribuir e reportar problemas

Consulte [CONTRIBUTING]({{repo}}/blob/main/CONTRIBUTING.md), [GOVERNANCE]({{repo}}/blob/main/GOVERNANCE.md) e o [código de conduta]({{repo}}/blob/main/CODE_OF_CONDUCT.md). Para vulnerabilidades, siga [SECURITY]({{repo}}/blob/main/SECURITY.md) e evite publicar segredos em issues.

A automação de testes, releases e atualizações reduz trabalho rotineiro. Ela não elimina revisão humana, responsabilidade por segurança ou decisão de aceitar mudanças. Verificações automáticas não garantem correção de um serviço em produção. Confirme permissões, proteção de branches e resultados de CI antes de confiar em fluxos automáticos.

## Beta e operação

A versão do código é **{{version}}**, lida diretamente do servidor ao gerar este site. O site não altera esse número para anunciar recursos. A linha PIER 0.6 continua beta: valide os fluxos necessários e teste recuperação em ambiente separado. Atualizações são aplicadas pelo operador; não há troca autônoma do próprio painel. Veja [atualizar](/docs/atualizar/).
