## Terminal do navegador

Use **Terminal** na aplicação, banco, container da stack ou explorador Docker do servidor. Para stacks, escolha o container desejado. A imagem precisa ter `/bin/sh`; a seleção automática prefere bash quando disponível.

É uma sessão interativa com os privilégios do usuário do container e acesso aos seus arquivos, rede e variáveis. Containers privilegiados ou com socket Docker podem dar controle do host. Use apenas com pessoas confiáveis. O terminal exige sessão do navegador com capacidade de escrita; tokens de API, mesmo de escrita, não abrem terminal.

Feche pelo botão do modal. Escape é enviado ao shell. Inatividade de 15 minutos ou duração de oito horas encerra a sessão; não há reconexão, compartilhamento ou gravação de comandos. A auditoria registra abertura e fechamento, sem transcrição: não use isso como registro completo do que foi executado. Revogar a sessão encerra o terminal na próxima revalidação, em até cerca de 30 segundos.

## Manutenção do servidor

Em **Operações do servidor**, marque **Pausar novos deploys neste servidor** e salve. Confira antes se há operações já em andamento. A manutenção não para containers existentes nem bloqueia leitura e restauração; não é desligamento da máquina. Ao terminar, desmarque e salve, então valide um deploy.

## Limpeza segura

1. Use **Simular limpeza segura** e leia o relatório.
2. Confira a idade mínima (24 a 8760 horas) e os objetos candidatos.
3. Digite o nome do servidor e use **Executar limpeza segura** somente após revisar.
4. Para automatizar, ative a agenda, defina cron e salve a política; acompanhe os resultados.

Só entram objetos descartáveis com `pier.managed=true`, `pier.cleanup=true` e `pier.type=scratch`, respeitando a idade. Containers precisam estar parados e imagens sem tag. Volumes e backups nunca entram. A simulação não reserva o estado do Docker: confira o resultado da execução. Não substitua esse fluxo por `docker system prune --volumes`; volumes podem conter os dados dos seus serviços.
