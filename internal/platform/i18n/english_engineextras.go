package i18n

var englishEngineExtras = map[string]string{
	"Informe um estágio válido de Dockerfile (até 128 letras, números, '.', '_' ou '-').": "Enter a valid Dockerfile stage (up to 128 letters, numbers, '.', '_' or '-').",
	"Política inválida: use no, always, unless-stopped ou on-failure.":                    "Invalid policy: use no, always, unless-stopped or on-failure.",
	"Use de 0 a 300 segundos para encerrar os processos.":                                 "Use 0 to 300 seconds for process shutdown.",
}

func init() { mergeEnglish(englishEngineExtras) }
