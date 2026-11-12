package i18n

// englishServer holds the server area's Portuguese → English messages.
var englishServer = map[string]string{
	"Docker 24 ou superior": "Docker 24 or newer",
	"Instale ou atualize o Docker Engine para a versão 24 ou superior.":                                                                        "Install or update Docker Engine to version 24 or newer.",
	"Docker Compose 2.20 ou superior":                                                                                                          "Docker Compose 2.20 or newer",
	"Instale ou atualize o plugin Docker Compose para a versão 2.20 ou superior.":                                                              "Install or update the Docker Compose plugin to version 2.20 or newer.",
	"Pelo menos 10 GB livres no disco do Docker":                                                                                               "At least 10 GB free on the Docker disk",
	"Libere espaço no diretório raiz do Docker, removendo imagens e cache sem uso, ou amplie o disco.":                                         "Free space in the Docker root directory by removing unused images and cache, or expand the disk.",
	"Portas 80 e 443 livres ou usadas pelo proxy configurado":                                                                                  "Ports 80 and 443 free or used by the configured proxy",
	"Libere as portas 80 e 443 ou configure o Caddy existente como proxy na frente. Confira também as regras do firewall do provedor.":         "Free ports 80 and 443 or configure the existing Caddy as the edge proxy. Also check your provider's firewall rules.",
	"Rotação de logs do Docker":                                                                                                                "Docker log rotation",
	"O Pier já configura rotação por container. Para os demais, configure log-driver local ou max-size e max-file em /etc/docker/daemon.json.": "Pier already configures rotation per container. For other containers, configure the local log driver or max-size and max-file in /etc/docker/daemon.json.",
	"Relógio sincronizado com o Pier (até 30 segundos)":                                                                                        "Clock synchronized with Pier (within 30 seconds)",
	"Ative a sincronização NTP no servidor, por exemplo: timedatectl set-ntp true.":                                                            "Enable NTP synchronization on the server, for example: timedatectl set-ntp true.",
	"Não foi possível descobrir o IP público do servidor: %s":                                                                                  "Could not discover the server's public IP: %s",
	"O servidor não informou um IP público válido.":                                                                                            "The server did not report a valid public IP.",
	"Informe um domínio válido, sem protocolo, porta ou caminho.":                                                                              "Enter a valid domain without a protocol, port or path.",
	"Não foi possível resolver o endereço do servidor: %s":                                                                                     "Could not resolve the server address: %s",
	"Configure os registros DNS abaixo e remova os registros A/AAAA que apontam para outro servidor:\n%s":                                      "Configure the DNS records below and remove A/AAAA records pointing to another server:\n%s",
	"Não foi possível consultar o DNS do domínio: %s":                                                                                          "Could not query the domain's DNS: %s",
}

func init() { mergeEnglish(englishServer) }
