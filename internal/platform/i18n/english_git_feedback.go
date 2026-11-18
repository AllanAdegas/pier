package i18n

func init() {
	mergeEnglish(map[string]string{
		"Envio":                                        "Report",
		"Envio ao provedor Git inválido.":              "Invalid Git provider report.",
		"O provedor Git não respondeu a tempo.":        "The Git provider did not respond in time.",
		"Falha inesperada ao enviar ao provedor Git.":  "Unexpected failure sending to the Git provider.",
		"Commit recebido; aguardando o build no Pier.": "Commit received; waiting for the build in Pier.",
		"Deploy cancelado ou interrompido.":            "Deployment cancelled or interrupted.",
		"[Detalhes](%s)":                               "[Details](%s)",
	})
}
