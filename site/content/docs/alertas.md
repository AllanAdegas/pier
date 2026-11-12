O Pier acompanha o que acontece **depois** do deploy e avisa pelos canais que você escolher. A ideia é você saber do problema antes do seu usuário.

## Canais de notificação

Em **Notificações**, cadastre um ou mais canais:

| Canal | O que informar |
|---|---|
| Discord | URL do webhook do canal |
| Slack | URL do *incoming webhook* |
| Telegram | Token do bot e id do chat |
| E-mail (SMTP) | Servidor, porta, usuário, senha e destinatários. TLS pode ser obrigatório |
| Webhook | Qualquer URL que receba um POST em JSON |

Em cada canal, escolha os eventos que ele recebe e use **Testar** para conferir.

## O que gera alerta

| Situação | Quando dispara |
|---|---|
| Deploy falhou ou concluiu | A cada deploy (você escolhe quais) |
| Container reiniciando em loop | 3 reinícios ou mais em 10 minutos |
| Container morto por falta de memória | Quando o Docker registra `OOMKilled` |
| Health check falhando | Quando o container fica `unhealthy` depois do deploy |
| Disco, memória ou CPU alto | Acima de 90%, 90% e 95% durante 10 minutos (editável) |
| Certificado com problema | Inválido, autoassinado ou vencendo em menos de 14 dias |
| Backup ou tarefa que não rodou | 10 minutos depois do horário previsto sem execução bem-sucedida |
| Teste de restauração falhou | Quando o teste semanal de um banco falha |
| Site fora do ar | Quando uma checagem de uptime falha |
| Servidor inacessível | Quando o Pier perde o contato com um servidor |

"Não rodou" é diferente de "falhou". O Pier avisa também quando um backup agendado simplesmente não aconteceu.

## Sem inundar a caixa de entrada

- **Sem repetição:** enquanto um alerta continua ativo, a mensagem só se repete como lembrete a cada 6 horas.
- **Recuperação:** quando o problema some, chega uma única mensagem avisando que normalizou.
- **Silêncio:** durante uma manutenção, silencie tudo ou um recurso específico, por até 30 dias. Ao fim do silêncio, o Pier reavalia a situação.
- O estado dos alertas sobrevive a um reinício do Pier: você não recebe tudo de novo.

## Checagem de uptime

Em **Infraestrutura › Monitoramento**, crie uma checagem para uma aplicação ou stack:

- **URL:** um dos domínios publicados.
- **Status esperado:** padrão 200.
- **Intervalo:** de 1 minuto a 1 dia.

Cada resultado fica num histórico de 7 dias. A checagem parte do próprio servidor do Pier. Para saber se o servidor inteiro caiu, use também um monitor externo.

## Monitoramento ao vivo

Em **Infraestrutura › Monitoramento**, cada servidor mostra:

- CPU;
- memória usada e disponível;
- disco do diretório de dados e swap;
- tempo ligado, carga média e containers.

As leituras atualizam a cada 15 segundos com a página aberta. São fotos do momento, não um histórico. Os limites dos alertas de capacidade são editados na mesma tela.
