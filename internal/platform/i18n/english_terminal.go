package i18n

var englishTerminal = map[string]string{
	"Serviço":                               "Service",
	"O terminal não está disponível.":       "The terminal is unavailable.",
	"Origem da conexão não permitida.":      "Connection origin is not allowed.",
	"Shell inválido. Use auto, sh ou bash.": "Invalid shell. Use auto, sh or bash.",
	"Tipo de terminal desconhecido.":        "Unknown terminal type.",
	"Muitos terminais sendo abertos ao mesmo tempo. Tente de novo em alguns segundos.":    "Too many terminals opening at once. Try again in a few seconds.",
	"Terminal expirado ou inválido. Abra o terminal de novo.":                             "Expired or invalid terminal. Open the terminal again.",
	"O servidor já tem o máximo de terminais abertos. Feche um deles e tente de novo.":    "The server has reached its terminal limit. Close one and try again.",
	"Não foi possível abrir o terminal: %s":                                               "Could not open terminal: %s",
	"Você já tem %d terminais abertos. Feche um deles e tente de novo.":                   "You already have %d terminals open. Close one and try again.",
	"Este servidor já tem %d terminais abertos. Feche um deles e tente de novo.":          "This server already has %d terminals open. Close one and try again.",
	"Escolha o serviço da stack.":                                                         "Choose the stack service.",
	"Nenhum container deste recurso foi encontrado no servidor. Faça um deploy primeiro.": "No container for this resource was found on the server. Deploy it first.",
	"O container não está em execução. Inicie o recurso e tente de novo.":                 "The container is not running. Start the resource and try again.",
}

func init() { mergeEnglish(englishTerminal) }
