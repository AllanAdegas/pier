package i18n

// englishVolumes holds the volume backups area (and key-value database backups).
var englishVolumes = map[string]string{
	"Não foi possível reiniciar o recurso após a operação: %w":                    "Could not restart the resource after the operation: %w",
	"Esta engine só pode ser restaurada no próprio banco.":                        "This engine can only be restored to the same database.",
	"Parando o banco para substituir os dados. As conexões serão interrompidas.":  "Stopping the database to replace its data. Connections will be interrupted.",
	"Não foi possível iniciar o banco novamente: %w (restauração: %v).":           "Could not start the database again: %w (restore: %v).",
	"o arquivamento dos volumes falhou: %w":                                       "archiving the volumes failed: %w",
	"o arquivo gerado está corrompido: %w":                                        "the generated archive is corrupted: %w",
	"o arquivamento gerou um arquivo vazio":                                       "the archive step produced an empty file",
	"não foi possível parar a aplicação: %w":                                      "could not stop the application: %w",
	"Nenhum volume para salvar. Faça um deploy ou inclua os diretórios externos.": "No volume to back up. Deploy first or include the external directories.",
	"Backup de volumes falhou: %s":                                                "Volume backup failed: %s",
	"Backup de volumes concluído: %s":                                             "Volume backup completed: %s",
	"Aguarde a operação de volumes terminar.":                                     "Wait for the volume operation to finish.",
	"Aguarde a operação da stack terminar.":                                       "Wait for the stack operation to finish.",
	"Aguarde o deploy da aplicação terminar.":                                     "Wait for the application deployment to finish.",
	"não foi possível parar o recurso: %w":                                        "could not stop the resource: %w",
	"Restaurando %s.":                                                             "Restoring %s.",
	"A restauração de %s falhou: %w":                                              "Restoring %s failed: %w",
	"Restauração concluída.":                                                      "Restore finished.",
	"o volume %s não é mais usado por este recurso; a restauração foi cancelada":  "the volume %s is no longer used by this resource; the restore was cancelled",
	"Digite o nome do recurso para confirmar a restauração.":                      "Type the resource name to confirm the restore.",
	"Parando o recurso para restaurar os volumes.":                                "Stopping the resource to restore the volumes.",
	"Iniciando o recurso novamente.":                                              "Starting the resource again.",
	"O backup de volumes de %s não concluiu no horário esperado. Confira o agendador, o espaço livre e o histórico de backups.": "The volume backup of %s did not finish at the expected time. Check the scheduler, free space and backup history.",
	"Não foi possível listar os volumes: %s": "Could not list the volumes: %s",
}

func init() { mergeEnglish(englishVolumes) }
