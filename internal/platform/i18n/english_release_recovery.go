package i18n

func init() {
	mergeEnglish(map[string]string{
		"restaurar versão anterior: %w":                         "restore previous version: %w",
		"restaurar container %s: %w":                            "restore container %s: %w",
		"Não foi possível remover containers antigos de %s: %s": "Could not remove old containers on %s: %s",
	})
}
