package diagnose

import "regexp"

// failureRules is ordered from the most specific evidence to the most generic.
// Each code appears once; the tests enforce it.
var failureRules = []rule{
	{
		hint: Hint{
			Code:  "out_of_memory",
			Title: "Falta de memória",
			Fix: "O processo foi encerrado por exceder a memória disponível (código 137). Aumente o limite de memória do " +
				"contêiner ou reduza o consumo da aplicação, por exemplo com NODE_OPTIONS=--max-old-space-size.",
			DocURL: "https://docs.docker.com/engine/containers/resource_constraints/",
		},
		exitCode: 137,
		pattern:  regexp.MustCompile(`(?i)oomkilled|out of memory|cannot allocate memory`),
	},
	{
		hint: Hint{
			Code:  "dockerfile_missing",
			Title: "Dockerfile não encontrado",
			Fix: "Confirme o caminho do Dockerfile nas configurações da aplicação e que o arquivo está commitado no " +
				"repositório, dentro do diretório de build.",
			DocURL: "https://docs.docker.com/reference/dockerfile/",
		},
		pattern: regexp.MustCompile(`(?i)failed to read dockerfile|dockerfile: no such file|` +
			`evaluate symlinks in dockerfile path`),
	},
	{
		hint: Hint{
			Code:  "start_script_missing",
			Title: "Comando de start não encontrado",
			Fix: "Defina o script 'start' no package.json (por exemplo 'start': 'node server.js') ou informe o comando de " +
				"start nas configurações da aplicação.",
			DocURL: "https://docs.npmjs.com/cli/v10/using-npm/scripts",
		},
		pattern: regexp.MustCompile(`(?i)missing script: "?start|command "start" not found|no start command could be found`),
	},
	{
		hint: Hint{
			Code:  "disk_full",
			Title: "Não há espaço em disco",
			Fix: "Não há espaço livre no diretório do Docker. Libere espaço em Servidores (imagens sem uso, contêineres " +
				"parados e caches de build) e tente novamente.",
			DocURL: "https://docs.docker.com/engine/manage-resources/pruning/",
		},
		pattern: regexp.MustCompile(`(?i)no space left on device|enospc`),
	},
	{
		hint: Hint{
			Code:  "git_access_denied",
			Title: "Sem acesso ao repositório Git",
			Fix: "Confirme que o repositório existe e que a chave de deploy ou o token da aplicação tem permissão de leitura " +
				"nele.",
			DocURL: "https://docs.github.com/en/authentication/connecting-to-github-with-ssh",
		},
		pattern: regexp.MustCompile(`(?i)permission denied \(publickey\)|repository not found|could not read username|` +
			`authentication failed for`),
	},
	{
		hint: Hint{
			Code:  "docker_hub_rate_limit",
			Title: "O limite de downloads do Docker Hub foi atingido",
			Fix: "Aguarde a renovação do limite ou autentique o servidor no Docker Hub (docker login) " +
				"para usar a cota da conta.",
			DocURL: "https://docs.docker.com/docker-hub/usage/",
		},
		pattern: regexp.MustCompile(`(?i)toomanyrequests|pull rate limit`),
	},
	{
		hint: Hint{
			Code:  "dns_resolution_failed",
			Title: "Não foi possível resolver o nome do host",
			Fix: "O build não conseguiu resolver um nome de host. Verifique a configuração de DNS do servidor e a " +
				"conectividade de saída da rede do Docker.",
			DocURL: "https://docs.docker.com/engine/daemon/",
		},
		pattern: regexp.MustCompile(`(?i)temporary failure in name resolution|could not resolve host|no such host|` +
			`\beai_again\b|\benotfound\b`),
	},
	{
		hint: Hint{
			Code:  "architecture_mismatch",
			Title: "Arquitetura da imagem não é a do servidor",
			Fix: "A imagem foi compilada para outra arquitetura, por exemplo arm64 em um servidor amd64. Gere a imagem para a " +
				"arquitetura do servidor com docker buildx build --platform linux/amd64.",
			DocURL: "https://docs.docker.com/build/building/multi-platform/",
		},
		pattern: regexp.MustCompile(`(?i)exec format error|no matching manifest|requested image's platform`),
	},
	{
		hint: Hint{
			Code:  "address_in_use",
			Title: "Porta já está em uso",
			Fix: "A aplicação tentou usar uma porta que outro processo já ocupa. Faça-a escutar somente na porta configurada, " +
				"sem fixar outra porta.",
			DocURL: "https://docs.docker.com/engine/network/",
		},
		pattern: regexp.MustCompile(`(?i)address already in use|eaddrinuse`),
	},
	{
		hint: Hint{
			Code:  "healthcheck_failed",
			Title: "Healthcheck não passou",
			Fix: "A aplicação não respondeu ao healthcheck dentro do prazo. Confirme que ela escuta na porta configurada e " +
				"responde no caminho do healthcheck, ou aumente o tempo de inicialização se ela demora para subir.",
			DocURL: "https://docs.docker.com/reference/dockerfile/#healthcheck",
		},
		pattern: regexp.MustCompile(`(?i)n.o ficou pronto dentro|did not become ready in time|` +
			`healthcheck do novo container falhou|healthcheck failed`),
	},
	{
		hint: Hint{
			Code:  "container_crash_loop",
			Title: "Contêiner encerra logo após iniciar",
			Fix: "O processo principal termina ao iniciar. Veja os logs do contêiner: a causa costuma ser comando de start " +
				"incorreto, variável de ambiente ausente ou erro de configuração.",
			DocURL: "https://docs.docker.com/engine/containers/run/",
		},
		pattern: regexp.MustCompile(`(?i)o container encerrou logo ap.s iniciar|the container exited right after starting|` +
			`o container est. reiniciando em loop|restart loop`),
	},
	{
		hint: Hint{
			Code:  "module_missing",
			Title: "Dependência não instalada",
			Fix: "Uma biblioteca importada não está na imagem. Confirme que a instalação de dependências (pip install -r " +
				"requirements.txt, npm ci etc.) roda no build e que o arquivo de lock está no repositório.",
			DocURL: "https://docs.npmjs.com/cli/v10/commands/npm-ci",
		},
		pattern: regexp.MustCompile(`ModuleNotFoundError|No module named|Cannot find module|MODULE_NOT_FOUND`),
	},
	{
		hint: Hint{
			Code:  "volume_permission_denied",
			Title: "Permissão negada em volume ou arquivo",
			Fix: "O usuário do contêiner não pode acessar o caminho. Ajuste o dono do diretório no Dockerfile (chown) ou as " +
				"permissões do volume.",
			DocURL: "https://docs.docker.com/engine/storage/volumes/",
		},
		pattern: regexp.MustCompile(`EACCES|PermissionError: \[Errno 13\]|: permission denied`),
	},
}
