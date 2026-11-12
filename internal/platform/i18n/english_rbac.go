package i18n

var englishRbac = map[string]string{
	"SSO não autorizado.":                  "SSO access denied.",
	"Acesso não autorizado.":               "Access denied.",
	"Mantenha pelo menos um proprietário.": "Keep at least one owner.",
	"Convite inválido.":                    "Invalid invitation.",
}

func init() { mergeEnglish(englishRbac) }
