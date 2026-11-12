package i18n

// englishPlatform holds the platform area's Portuguese → English messages.
var englishPlatform = map[string]string{
	"PIER_DOMAIN inválido: %w":                                            "invalid PIER_DOMAIN: %w",
	"PIER_DOMAIN deve conter apenas o domínio":                            "PIER_DOMAIN must contain only the hostname",
	"servidor local não encontrado para PIER_DOMAIN":                      "local server not found for PIER_DOMAIN",
	"Diretório de dados do Pier parado":                                   "Data directory of the stopped Pier instance",
	"Cursor de auditoria inválido.":                                       "Invalid audit cursor.",
	"Use um limite entre 1 e 200 registros.":                              "Use a limit between 1 and 200 entries.",
	"Informe datas no formato RFC3339.":                                   "Enter dates in RFC3339 format.",
	"Informe um e-mail com até 320 caracteres.":                           "Enter an email address with up to 320 characters.",
	"Aplicação ou stack":                                                  "Application or stack",
	"A URL deve corresponder a uma rota publicada da aplicação ou stack.": "The URL must match a published route of the application or stack.",
	"Informe um término válido para a manutenção, em até 30 dias.":        "Enter a valid maintenance end time within 30 days.",
	"Recurso": "Resource",
	"Informe uma justificativa de 5 a 1000 caracteres para descongelar o ambiente.":        "Enter a reason of 5 to 1000 characters to unfreeze the environment.",
	"Formato de backup inválido ou versão não suportada.":                                  "Invalid backup format or unsupported version.",
	"Frase-senha incorreta ou backup corrompido.":                                          "Incorrect passphrase or corrupted backup.",
	"Conteúdo inválido no backup da instância.":                                            "Invalid contents in the instance backup.",
	"O banco do backup falhou na verificação de integridade.":                              "The backup database failed the integrity check.",
	"O diretório de dados está em uso. Pare o Pier antes de restaurar.":                    "The data directory is in use. Stop Pier before restoring.",
	"Use uma frase-senha de 16 a 1024 caracteres para o backup.":                           "Use a backup passphrase of 16 to 1024 characters.",
	"Informe uma pasta absoluta exclusiva para os backups da instância.":                   "Enter a dedicated absolute directory for instance backups.",
	"Informe uma agenda cron válida e retenção entre 1 e 365 backups.":                     "Enter a valid cron schedule and retention between 1 and 365 backups.",
	"Defina a frase-senha antes de ativar o backup automático.":                            "Set a passphrase before enabling automatic backups.",
	"Erro interno ao executar a ferramenta. Consulte os logs do Pier.":                     "Internal error executing the tool. Check the Pier logs.",
	"O ambiente %q está congelado. Descongele-o com uma justificativa antes de implantar.": "Environment %q is frozen. Unfreeze it with a reason before deploying.",
	"Digite o nome do ambiente %q para confirmar a remoção.":                               "Type the environment name %q to confirm deletion.",
}

func init() { mergeEnglish(englishPlatform) }
