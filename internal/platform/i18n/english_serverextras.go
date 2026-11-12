package i18n

var englishServerExtras = map[string]string{
	"Docker Buildx disponível": "Docker Buildx available",
	"Instale o plugin Docker Buildx para construir aplicações neste servidor.": "Install the Docker Buildx plugin to build applications on this server.",
	"A limpeza exige idade mínima entre 24 e 8760 horas.":                      "Cleanup requires a minimum age between 24 and 8760 hours.",
	"Servidor em manutenção. Desative a manutenção para implantar.":            "Server under maintenance. Disable maintenance to deploy.",
	"A limpeza deste servidor já está em andamento.":                           "Cleanup is already running on this server.",
	"Digite o nome do servidor para confirmar a limpeza.":                      "Type the server name to confirm cleanup.",
}

func init() { mergeEnglish(englishServerExtras) }
