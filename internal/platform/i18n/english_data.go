package i18n

var englishData = map[string]string{
	"Não foi possível reconectar o banco: %w (restauração: %v).":                                   "Could not reconnect the database: %w (restore: %v).",
	"A restauração falhou: %w":                                                                     "Restore failed: %w",
	"A verificação da restauração falhou: %w":                                                      "Restore verification failed: %w",
	"A limpeza da operação interrompida falhou: %s":                                                "Interrupted operation cleanup failed: %s",
	"Backup local concluído; a cópia S3 falhou: %s":                                                "Local backup completed; the S3 copy failed: %s",
	"Aguarde a operação do banco terminar.":                                                        "Wait for the database operation to finish.",
	"Esta engine não suporta teste de restauração.":                                                "This engine does not support restore testing.",
	"Configure o destino S3 antes de enviar backups.":                                              "Configure the S3 destination before uploading backups.",
	"O arquivo de backup não está disponível no servidor nem no S3.":                               "The backup file is unavailable on the server and in S3.",
	"Faça um backup antes de testar a restauração.":                                                "Create a backup before testing restore.",
	"Teste de restauração falhou: %s":                                                              "Restore test failed: %s",
	"Esta engine não suporta restauração.":                                                         "This engine does not support restore.",
	"Inicie o banco antes de restaurar.":                                                           "Start the database before restoring.",
	"Restaurando backup. Interrompa as escritas das aplicações dependentes durante a restauração.": "Restoring backup. Pause writes from dependent applications during restore.",
	"Restauração verificada.":                                                                      "Restore verified.",
	"Selecione um backup concluído para restaurar.":                                                "Select a completed backup to restore.",
	"Digite o nome do banco para confirmar a restauração.":                                         "Type the database name to confirm restore.",
	"Informe um endpoint S3 HTTPS válido.":                                                         "Enter a valid HTTPS S3 endpoint.",
	"Informe bucket e credenciais S3.":                                                             "Enter an S3 bucket and credentials.",
	"A configuração S3 não pode conter quebras de linha.":                                          "S3 settings cannot contain line breaks.",
}

func init() { mergeEnglish(englishData) }
