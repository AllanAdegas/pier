// English catalog for stacks from Git (merged into ./en.ts).
export const englishGitstacks: Record<string, string> = {
  "Compose do Git": "Compose from Git",
  "Clona um repositório e sobe o docker-compose.yaml dele, com build no servidor e deploy a cada push.":
    "Clones a repository and runs its docker-compose.yaml, with builds on the server and deploys on every push.",
  "O repositório é clonado e construído neste servidor.": "The repository is cloned and built on this server.",
  "Caminho dentro do repositório (ou da pasta base).": "Path inside the repository (or the base folder).",
  "Pasta base e caminhos monitorados": "Base folder and watched paths",
  "Pasta base": "Base folder",
  "Opcional. Pasta do repositório onde está o arquivo compose.": "Optional. Repository folder that holds the compose file.",
  "Caminhos monitorados": "Watched paths",
  "Opcional, um por linha. O webhook de push só implanta se o push alterar algum destes caminhos.":
    "Optional, one per line. The push webhook only deploys when the push changes one of these paths.",
  "O Pier clona o repositório a cada deploy. Dados gravados dentro dele (bind mounts como ./data) são recriados; use volumes nomeados para o que precisa ficar.":
    "Pier clones the repository on every deploy. Data written inside it (bind mounts such as ./data) is recreated; use named volumes for what must stay.",
  "Origem Git": "Git source",
  "O compose vem do repositório e é clonado a cada deploy.": "The compose file comes from the repository and is cloned on every deploy.",
  "Último deploy": "Last deploy",
  "Ainda não implantada.": "Not deployed yet.",
  "O último deploy foi recusado: o compose do repositório pede poderes sobre o servidor que ainda não foram aceitos. Aceite-os na aba Git para implantar.":
    "The last deploy was refused: the repository's compose file asks for powers over the server that were not accepted yet. Accept them in the Git tab to deploy.",
  "Configurações salvas. Implante para aplicar.": "Settings saved. Deploy to apply.",
  "Deixe em branco para manter o endereço atual (ele pode conter um token).": "Leave blank to keep the current address (it may contain a token).",
  "Opcional. Com uma tag, ela é implantada no lugar da branch e os webhooks de push são ignorados.":
    "Optional. With a tag, it is deployed instead of the branch and push webhooks are ignored.",
  "Salvar configurações": "Save settings",
  "Riscos aceitos": "Accepted risks",
  "Poderes sobre o servidor que o compose deste repositório pode pedir. Um push que peça mais do que isto é recusado.":
    "Powers over the server this repository's compose file may ask for. A push that asks for more is refused.",
  "Nenhum risco aceito.": "No accepted risks.",
  "Aceitar os riscos pendentes": "Accept the pending risks",
  "Cadastre uma das URLs abaixo como webhook de push no seu repositório, usando o segredo. Salve as configurações para aplicar.":
    "Register one of the URLs below as a push webhook in your repository, using the secret. Save the settings to apply.",
  "Confiança do host Git redefinida.": "Git host trust reset.",
  "Redefinir a confiança do host SSH": "Reset SSH host trust",
  "Use somente depois de confirmar a nova chave do host com o administrador do repositório.":
    "Only use this after confirming the new host key with the repository administrator.",
  "O compose vem do repositório; edite-o lá e implante. Origem e webhook ficam na aba Git.":
    "The compose file comes from the repository; edit it there and deploy. Source and webhook are in the Git tab.",
  "Informe o nome do serviço, como está no compose do repositório.": "Enter the service name as it appears in the repository's compose file.",
  "Git": "Git",
};
