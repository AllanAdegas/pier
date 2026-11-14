package i18n

func init() {
	mergeEnglish(map[string]string{
		"Não foi possível localizar o container da tarefa Swarm.":                                         "Could not locate the Swarm task container.",
		"Diretório temporário Swarm inválido.":                                                            "Invalid Swarm temporary directory.",
		"Rollout Swarm interrompido: %s":                                                                  "Swarm rollout interrupted: %s",
		"Tarefa Swarm falhou: %s":                                                                         "Swarm task failed: %s",
		"Não foi possível identificar a versão do serviço Swarm.":                                         "Could not identify the Swarm service version.",
		"Mantenha os mesmos processos e réplicas no alvo Swarm.":                                          "Keep the same processes and replicas on the Swarm target.",
		"Use um novo deploy para atualizar a aplicação Swarm.":                                            "Use a new deployment to update the Swarm application.",
		"Remova a versão implantada antes de mudar o alvo Swarm.":                                         "Remove the deployed release before changing the Swarm target.",
		"Escolha Swarm ou destinos individuais.":                                                          "Choose Swarm or individual destinations.",
		"Swarm não aceita volumes, portas publicadas, hooks, release ou opções Docker extras neste alvo.": "Swarm does not accept volumes, published ports, hooks, release commands or extra Docker options on this target.",
		"Remova os outros nós antes de excluir o Swarm.":                                                  "Remove other nodes before deleting the Swarm.",
		"Swarm":                                                      "Swarm",
		"Use no máximo %d caracteres.":                               "Use at most %d characters.",
		"Selecione o servidor manager.":                              "Select the manager server.",
		"Use um endereço IP, como 10.0.0.5.":                         "Use an IP address, such as 10.0.0.5.",
		"Escolha manager ou worker.":                                 "Choose manager or worker.",
		"Nenhum manager Swarm está disponível.":                      "No Swarm manager is available.",
		"O servidor já pertence a um Swarm.":                         "The server already belongs to a Swarm.",
		"O Swarm atingiu o limite de nós.":                           "The Swarm reached its node limit.",
		"O nó não confirmou a entrada no Swarm.":                     "The node did not confirm joining the Swarm.",
		"Digite o nome do Swarm para confirmar.":                     "Type the Swarm name to confirm.",
		"Não remova managers pelo Pier; preserve o quorum do Swarm.": "Do not remove managers through Pier; preserve Swarm quorum.",
		"Remova os workers antes de excluir o Swarm.":                "Remove workers before deleting the Swarm.",
		"Remova os serviços antes de excluir o Swarm.":               "Remove services before deleting the Swarm.",
		"Informe serviço, imagem e réplicas válidos (0 a 100).":      "Enter a valid service, image and replica count (0 to 100).",
		"Ação Swarm inválida.":                                       "Invalid Swarm action.",
	})
}
