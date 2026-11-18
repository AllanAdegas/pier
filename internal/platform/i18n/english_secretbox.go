package i18n

func init() {
	mergeEnglish(map[string]string{
		"a chave secreta (PIER_SECRET_KEY ou secret.key) parece fraca: use um valor longo e aleatório, como `openssl rand -hex 32`; alterar a chave exige cadastrar novamente todos os segredos": "the secret key (PIER_SECRET_KEY or secret.key) looks weak: use a long random value, such as `openssl rand -hex 32`; changing it requires re-entering every stored secret",
		"secretbox: falha ao migrar segredos v1; dados preservados, nova tentativa na próxima inicialização":                                                                                     "secretbox: upgrade of v1 secrets failed; data left unchanged, retrying on next start",
		"secretbox: segredos v1 migrados para v2": "secretbox: v1 secrets upgraded to v2",
	})
}
