package stack

import (
	"context"
	"fmt"
	"path"
	"strings"

	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
)

// createHostFile walks directories before creating a new entry with noclobber.
// Existing regular files belong to the operator and keep their content and mode.
func createHostFile(ctx context.Context, ex executor.Executor, root string, file hostFile) error {
	clean := path.Clean(file.Path)
	if !path.IsAbs(root) || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || clean == "." {
		return fmt.Errorf("invalid stack file path")
	}
	target := path.Join(root, clean)
	script := `set -eu
cd /
`
	current := ""
	for _, part := range strings.Split(strings.TrimPrefix(path.Dir(target), "/"), "/") {
		if part == "" {
			continue
		}
		current += "/" + part
		q := shell.Quote(part)
		script += "test ! -L " + q + "\nif test ! -e " + q + "; then mkdir -m 700 -- " + q + "; fi\ncd -P -- " + q + "\ntest \"$(pwd -P)\" = " + shell.Quote(current) + "\n"
	}
	q := shell.Quote(path.Base(target))
	script += "test ! -L " + q + "\nif test -e " + q + "; then test -f " + q + "; exit; fi\n"
	script += fmt.Sprintf("umask 077\nset -C\nexec 3> %s\nchmod %03o /dev/fd/3\ncat >&3\n", q, file.Mode.Perm())
	_, err := executor.RunInput(ctx, ex, "sh -c "+shell.Quote(script), file.Content)
	return err
}
