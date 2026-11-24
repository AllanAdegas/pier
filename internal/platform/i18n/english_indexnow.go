package i18n

// Search engine notification (internal/indexnow).
func init() {
	mergeEnglish(map[string]string{
		"AVISO: %s":           "WARNING: %s",
		"AVISO: IndexNow: %s": "WARNING: IndexNow: %s",
		"AVISO: IndexNow: o proxy não foi atualizado: %s":                                         "WARNING: IndexNow: the proxy was not updated: %s",
		"IndexNow: nenhuma página mudou desde o último envio.":                                    "IndexNow: no page changed since the last submission.",
		"%s (só as primeiras %d páginas)":                                                         "%s (only the first %d pages)",
		"IndexNow: o envio não chegou ao IndexNow: %s":                                            "IndexNow: the submission did not reach IndexNow: %s",
		"IndexNow: %d URL(s) de %s enviadas; aceito (%d).":                                        "IndexNow: %d URL(s) from %s submitted; accepted (%d).",
		"IndexNow: envio recusado (400): formato inválido.":                                       "IndexNow: submission refused (400): invalid format.",
		"IndexNow: chave recusada (403): o buscador não conseguiu ler o arquivo da chave.":        "IndexNow: key refused (403): the search engine could not read the key file.",
		"IndexNow: URLs recusadas (422): não pertencem ao domínio ou à chave.":                    "IndexNow: URLs refused (422): they do not belong to the domain or to the key.",
		"IndexNow: envios demais (429): o próximo deploy tenta de novo.":                          "IndexNow: too many submissions (429): the next deployment tries again.",
		"IndexNow: resposta inesperada (%d).":                                                     "IndexNow: unexpected response (%d).",
		"IndexNow: %s não resolve para um endereço público; nada foi enviado.":                    "IndexNow: %s does not resolve to a public address; nothing was submitted.",
		"IndexNow: %s não respondeu com a chave (%s). Confira o DNS e o proxy; nada foi enviado.": "IndexNow: %s did not answer with the key (%s). Check the DNS and the proxy; nothing was submitted.",
		"IndexNow: nenhum sitemap encontrado em https://%s (robots.txt e /sitemap.xml).":          "IndexNow: no sitemap found at https://%s (robots.txt and /sitemap.xml).",
		"IndexNow: sitemap acima do limite de leitura: %s":                                        "IndexNow: sitemap over the read limit: %s",
		"IndexNow: não foi possível ler o sitemap: %s":                                            "IndexNow: could not read the sitemap: %s",
		"Domínio": "Domain",
		"Chave":   "Key",
		"Outro recurso já avisa os buscadores sobre %s.":                                                         "Another resource already notifies search engines about %s.",
		"Disponível só para recursos publicados pelo proxy do Pier, fora do Swarm.":                              "Available only for resources published by Pier's proxy, outside Swarm.",
		"Configure a URL pública do Pier em Configurações para avisar buscadores a partir de outros servidores.": "Set Pier's public URL in Settings to notify search engines from other servers.",
		"Use um domínio https:// sem caminho deste recurso.":                                                     "Use an https:// domain of this resource, without a path.",
	})
}
