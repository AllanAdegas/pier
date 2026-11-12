package terminal

import "pier/internal/platform/shell"

// shellScripts are the only programs a terminal starts. Nothing the browser
// sends is ever placed in them: the choice is a key into this table.
var shellScripts = map[string]string{
	ShellAuto: "if command -v bash >/dev/null 2>&1; then exec bash; fi; exec sh",
	ShellBash: "exec bash",
	ShellSh:   "exec sh",
}

// execCommand is the `docker exec` that attaches the shell to a terminal.
// containerID was read from docker by resolve, and is quoted regardless.
func execCommand(containerID, choice string) string {
	return shell.Join("docker", "exec", "--interactive", "--tty", "--env", "TERM=xterm-256color",
		containerID, "/bin/sh", "-c", shellScripts[choice])
}
