package i18n

func init() {
	mergeEnglish(map[string]string{
		"não foi possível registrar os containers antes da parada":            "could not record the containers before stopping",
		"Interrompido pela reinicialização do Pier. A recuperação falhou: %s": "Interrupted by Pier restarting. Recovery failed: %s",
		"não foi possível iniciar o banco novamente: %w":                      "could not start the database again: %w",
	})
}
