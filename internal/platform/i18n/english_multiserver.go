package i18n

var englishMultiserver = map[string]string{
	"Remova os releases antes de alterar os destinos.":                                   "Remove releases before changing destinations.",
	"Rollout incompleto: reconcilie os destinos.":                                        "Incomplete rollout: reconcile destinations.",
	"Versão ativada; limpeza dos destinos incompleta.":                                   "Version activated; destination cleanup incomplete.",
	"Destino sem containers.":                                                            "Destination has no containers.",
	"Operação parcial nos destinos.":                                                     "Partial operation across destinations.",
	"Rollout multisservidor incompleto. Reconcilie os destinos antes de iniciar o Pier.": "Incomplete multi-server rollout. Reconcile destinations before starting Pier.",
	"Placement exige proxy direto e portas sem mapeamento no host.":                      "Placement requires a direct proxy and no host port mappings.",
	"Registro inválido.":                                                                 "Invalid registry.",
	"Registro indisponível.":                                                             "Registry unavailable.",
	"Credencial indisponível.":                                                           "Credential unavailable.",
	"Falha ao preparar autenticação.":                                                    "Could not prepare authentication.",
	"Falha ao remover autenticação temporária.":                                          "Could not remove temporary authentication.",
	"Operação no registro falhou.":                                                       "Registry operation failed.",
	"Repositório inválido.":                                                              "Invalid repository.",
	"Use uma imagem por digest.":                                                         "Use an image by digest.",
	"Proteção de ambiente indisponível.":                                                 "Environment protection unavailable.",
	"Configuração de destinos inválida.":                                                 "Invalid destination configuration.",
	"Servidor de destino inválido.":                                                      "Invalid destination server.",
	"Integração de release multisservidor indisponível.":                                 "Multi-server release integration unavailable.",
	"Falha ao validar arquitetura.":                                                      "Could not validate architecture.",
	"Arquiteturas incompatíveis.":                                                        "Incompatible architectures.",
	"Rollback parcial: verifique os destinos.":                                           "Partial rollback: check destinations.",
	"Deploy falhou; versão anterior preservada.":                                         "Deployment failed; previous version preserved.",
}

func init() { mergeEnglish(englishMultiserver) }
