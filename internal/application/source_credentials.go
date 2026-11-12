package application

import (
	"context"
	"sort"
	"strings"

	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// dockerEnvFile uses Docker's literal env-file format (not Compose's dotenv).
func dockerEnvFile(values map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var content strings.Builder
	for _, key := range keys {
		if strings.ContainsAny(values[key], "\r\n\x00") {
			return nil, i18n.Errorf("o valor de %s não pode conter quebras de linha no arquivo de ambiente do clone", key)
		}
		content.WriteString(key + "=" + values[key] + "\n")
	}
	return []byte(content.String()), nil
}

func removeBuildCredentials(ctx context.Context, ex executor.Executor, paths ...string) error {
	command := shell.Join(append([]string{"rm", "-f"}, paths...)...)
	for _, filename := range paths {
		command += " && test ! -e " + shell.Quote(filename) + " && test ! -L " + shell.Quote(filename)
	}
	if _, err := ex.Run(ctx, command); err != nil {
		return i18n.Errorf("não foi possível remover as credenciais temporárias do build; o deploy foi interrompido: %w", err)
	}
	return nil
}
