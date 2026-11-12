package i18n

// englishStack holds the stack area's Portuguese → English messages.
var englishStack = map[string]string{
	"AVISO: o domínio %s ainda não aponta para este servidor: publicado só em HTTP, sem pedir certificado. %s Depois que o DNS propagar, faça um novo deploy: o certificado HTTPS será emitido nele.": "WARNING: the domain %s does not point to this server yet: published over HTTP only, no certificate requested. %s Once DNS has propagated, deploy again: the HTTPS certificate will be issued then.",
	"Não foi possível verificar as etiquetas da imagem %s do serviço %q: %s":                                                                                                        "Could not verify the labels of image %s of service %q: %s",
	"A imagem %s do serviço %q retornou etiquetas inválidas; a operação foi interrompida.":                                                                                          "Image %s of service %q returned invalid labels; the operation was stopped.",
	"O serviço %q usa a imagem %s, que traz etiquetas reservadas (%s). Use uma imagem sem etiquetas traefik.* e pier.*: elas podem alterar rotas e recursos gerenciados pelo Pier.": "Service %q uses image %s, which carries reserved labels (%s). Use an image without traefik.* and pier.* labels: they can change routes and resources managed by Pier.",
	"O serviço %q declara etiquetas reservadas no compose (%s). Remova as etiquetas traefik.* e pier.*: elas podem alterar rotas e recursos gerenciados pelo Pier.":                 "Service %q declares reserved labels in the compose file (%s). Remove the traefik.* and pier.* labels: they can change routes and resources managed by Pier.",
	"Não foi possível ler a configuração resolvida da stack para verificar as etiquetas: %s":                                                                                        "Could not read the stack's resolved configuration to verify its labels: %s",
	"A configuração resolvida da stack é inválida; a operação foi interrompida.":                                                                                                    "The stack's resolved configuration is invalid; the operation was stopped.",
}

func init() { mergeEnglish(englishStack) }
