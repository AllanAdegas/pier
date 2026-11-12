package i18n

var englishAlerts = map[string]string{
	"O alerta %s voltou ao normal.": "Alert %s is back to normal.",
	"Alerta resolvido":              "Alert resolved",
	"Alerta de execução":            "Runtime alert",
	"Informe limites percentuais maiores que 0 e até 100, e duração entre 1 e 10080 minutos.":                     "Enter percentage thresholds above 0 and up to 100, and a duration between 1 and 10080 minutes.",
	"Informe URL http(s), recurso, status esperado e intervalo entre 60 e 86400 segundos.":                        "Enter an http(s) URL, resource, expected status and interval between 60 and 86400 seconds.",
	"O contêiner reiniciou ao menos 3 vezes em 10 minutos. Confira os logs e o comando de início.":                "The container restarted at least 3 times in 10 minutes. Check the logs and start command.",
	"O contêiner ficou sem memória. Aumente o limite de memória ou a capacidade do servidor.":                     "The container ran out of memory. Increase its memory limit or server capacity.",
	"A saúde do contêiner falhou. Confira a porta, a rota de saúde e os logs.":                                    "Container health failed. Check the port, health endpoint and logs.",
	"Uso de %s acima de %.0f%%. Libere espaço ou aumente a capacidade do servidor.":                               "%s usage above %.0f%%. Free space or increase server capacity.",
	"A URL não respondeu com o status esperado. Confira o proxy, o DNS e os logs da aplicação.":                   "The URL did not return the expected status. Check the proxy, DNS and application logs.",
	"O certificado HTTPS é inválido ou vence em menos de 14 dias. Confira o DNS, a renovação e os logs do proxy.": "The HTTPS certificate is invalid or expires in less than 14 days. Check DNS, renewal and proxy logs.",
	"A tarefa não concluiu no horário esperado. Confira o agendador e o histórico de execuções.":                  "The task did not complete on schedule. Check the scheduler and execution history.",
	"O backup não concluiu no horário esperado. Confira o agendador, o espaço livre e o histórico de backups.":    "The backup did not complete on schedule. Check the scheduler, free space and backup history.",
}

func init() { mergeEnglish(englishAlerts) }
