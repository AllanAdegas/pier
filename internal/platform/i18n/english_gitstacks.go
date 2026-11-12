package i18n

// englishGitstacks holds the Portuguese → English messages of stacks from Git.
var englishGitstacks = map[string]string{
	"Repositório: %s (%s)": "Repository: %s (%s)",
	"Commit %s — %s":       "Commit %s — %s",
	"Chaves de deploy não estão disponíveis nesta instância.":                        "Deploy keys are not available on this instance.",
	"Stacks do Git não estão disponíveis nesta instância.":                           "Stacks from Git are not available on this instance.",
	"O arquivo %s não existe no repositório (nesta branch ou tag).":                  "The file %s does not exist in the repository (on this branch or tag).",
	"O link simbólico %s aponta para fora do repositório ou não pode ser resolvido.": "The symbolic link %s points outside the repository or cannot be resolved.",
	"O arquivo %s aponta, por link simbólico, para fora do repositório.":             "The file %s points, through a symbolic link, outside the repository.",
	"O compose do repositório pede poderes sobre o servidor que ainda não foram aceitos: %s. Revise o arquivo e aceite esses riscos nas configurações da stack antes de implantar.": "The repository's compose file asks for powers over the server that were not accepted yet: %s. Review the file and accept these risks in the stack settings before deploying.",
	"Não foi possível apagar o repositório clonado: %s":                                                    "Could not delete the cloned repository: %s",
	"Não foi possível ler %s no repositório: %s.":                                                          "Could not read %s in the repository: %s.",
	"O arquivo %s não é um YAML válido: %s":                                                                "The file %s is not valid YAML: %s",
	"O compose inclui arquivos demais ou em profundidade demais.":                                          "The compose file includes too many files or nests them too deeply.",
	"O caminho %q (%s) sai do repositório ou depende de uma variável; use caminhos relativos dentro dele.": "The path %q (%s) leaves the repository or depends on a variable; use relative paths inside it.",
	"Nome de branch inválido.":                                                                             "Invalid branch name.",
	"Nome de tag inválido.":                                                                                "Invalid tag name.",
	"Use uma pasta relativa à raiz do repositório, sem '..'.":                                              "Use a folder relative to the repository root, without '..'.",
	"Use o caminho de um arquivo .yaml ou .yml dentro do repositório.":                                     "Use the path of a .yaml or .yml file inside the repository.",
	"Caminho de monitoramento inválido: use globs relativos ao repositório, ex.: apps/api/**.":             "Invalid watch path: use globs relative to the repository, e.g. apps/api/**.",
	"Risco desconhecido: %q.":                                                                              "Unknown risk: %q.",
	"Repositórios via SSH exigem uma chave de deploy.":                                                     "SSH repositories require a deploy key.",
	"Chave de deploy não encontrada.":                                                                      "Deploy key not found.",
	"Esta stack não vem de um repositório Git.":                                                            "This stack does not come from a Git repository.",
	"Não foi possível redefinir a confiança SSH do host Git: %s":                                           "Could not reset the trust of the Git SSH host: %s",
	"A stack ainda não foi implantada: o repositório é clonado no primeiro deploy.":                        "The stack has not been deployed yet: the repository is cloned on the first deploy.",
	"Escolha uma única origem: catálogo, compose, pasta ou repositório Git.":                               "Choose a single source: catalog, compose, folder or Git repository.",
	"Origem Git da stack":                                                                                  "Git source of the stack",
	"Deploy automático desativado para esta stack.":                                                        "Automatic deployment is off for this stack.",
	"Esta stack usa a tag fixa %s: webhooks de push são ignorados.":                                        "This stack uses the fixed tag %s: push webhooks are ignored.",
	"Branch removida: nada a fazer.":                                                                       "Branch removed: nothing to do.",
	"Use uma URL https:// ou git@host:org/repo.git.":                                                       "Use an https:// URL or git@host:org/repo.git.",
}

func init() { mergeEnglish(englishGitstacks) }
