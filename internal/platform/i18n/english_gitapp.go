package i18n

var englishGitapp = map[string]string{
	"O repositório da origem não corresponde à seleção do GitHub App. Selecione o repositório de novo.": "The source repository does not match the GitHub App selection. Select the repository again.",
	"Configuração do GitHub App inválida.":          "Invalid GitHub App configuration.",
	"GitHub indisponível. Tente novamente.":         "GitHub is unavailable. Try again.",
	"O GitHub recusou a operação (HTTP %d).":        "GitHub rejected the operation (HTTP %d).",
	"Limite do GitHub atingido. Aguarde um minuto.": "GitHub rate limit reached. Wait one minute.",
	"Assinatura do GitHub inválida.":                "Invalid GitHub signature.",
}

func init() { mergeEnglish(englishGitapp) }
