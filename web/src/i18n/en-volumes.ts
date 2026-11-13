export const englishVolumes: Record<string, string> = {
  "Backup de volumes iniciado.": "Volume backup started.",
  "Backup de volumes": "Volume backup",
  "Arquivos .tar.gz dos volumes e dos diretórios montados, guardados no diretório de dados do Pier.":
    "tar.gz archives of the volumes and mounted directories, kept in the Pier data directory.",
  "Backups de volumes": "Volume backups",
  "Nenhum backup de volumes": "No volume backups",
  "Restaurações de volumes": "Volume restores",
  "Nenhum volume em uso. Os volumes aparecem depois do primeiro deploy.": "No volume in use. Volumes show up after the first deploy.",
  "Incluído": "Included",
  "Fora do backup": "Not backed up",
  "Volume": "Volume",
  "Diretório do servidor": "Server directory",
  "Parar os containers durante o backup": "Stop the containers during the backup",
  "Dados consistentes, mas a aplicação fica fora do ar enquanto o arquivo é gerado.":
    "Consistent data, but the application is down while the archive is written.",
  "Com esta opção a aplicação fica indisponível durante cada backup, inclusive nos agendados.":
    "With this option the application is unavailable during every backup, including scheduled ones.",
  "Incluir diretórios fora dos dados do Pier": "Include directories outside the Pier data",
  "Por padrão, só entram volumes e diretórios dentro dos dados do Pier. Diretórios de sistema nunca entram.":
    "By default only volumes and directories inside the Pier data are included. System directories are never included.",
  "A restauração apaga o conteúdo atual dos volumes e o substitui pelo do backup. O recurso é parado durante a operação e iniciado ao final.":
    "The restore erases the current content of the volumes and replaces it with the backup. The resource is stopped during the operation and started at the end.",
  "Diretório fora dos dados do Pier: não entra no backup, a menos que você inclua os diretórios externos.":
    "Directory outside the Pier data: not backed up unless you include external directories.",
  "Caminho do sistema (socket, /etc, /proc…): nunca entra no backup.": "System path (socket, /etc, /proc…): never backed up.",
  "Nome de volume não suportado: não entra no backup.": "Unsupported volume name: not backed up.",
};
