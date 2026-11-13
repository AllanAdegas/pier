package stack

import (
	"context"
	"path"
	"slices"
	"strings"

	"pier/internal/application"
	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/shell"
)

// KeyVault hands out the deploy keys of private repositories.
type KeyVault interface {
	Material(ctx context.Context, id string) (string, error)
}

// SetGitSupport enables stacks from Git: box seals the repository address and
// the webhook secret, keys supplies the deploy keys.
func (s *Service) SetGitSupport(box *secretbox.Box, keys KeyVault) {
	s.git = gitStore{db: s.db, box: box}
	s.keys = keys
}

// knownRisks are the risks a Git stack may accept (see document.risks).
var knownRisks = []string{RiskDockerSocket, RiskPrivileged, RiskHostNetwork, RiskHostPID,
	"host-ports", "capabilities", "unconfined", "host-path"}

// attachGit completes a stack loaded from the database with its Git source.
func (s *Service) attachGit(ctx context.Context, stack *Stack) error {
	source, err := s.git.load(ctx, stack.ID)
	stack.Git = source
	return err
}

// newGitSource builds the source of a stack being created from the input,
// applying defaults and reporting every invalid field.
func (s *Service) newGitSource(ctx context.Context, input GitInput, fields apperr.Fields) GitSource {
	source := GitSource{AutoDeploy: input.AutoDeploy == nil || *input.AutoDeploy}
	s.applyGitInput(ctx, &source, input, fields)
	return source
}

// applyGitInput copies the fields of the input over a source and validates the result.
func (s *Service) applyGitInput(ctx context.Context, source *GitSource, input GitInput, fields apperr.Fields) {
	if repository := strings.TrimSpace(input.Repository); repository != "" {
		source.Repository = repository
	}
	source.Branch = orDefault(strings.TrimSpace(input.Branch), "main")
	source.Tag = strings.TrimSpace(input.Tag)
	source.BaseDirectory = cleanBase(input.BaseDirectory)
	source.ComposePath = orDefault(strings.TrimSpace(input.ComposePath), DefaultComposePath)
	source.PrivateKeyID = strings.TrimSpace(input.PrivateKeyID)
	source.WatchPaths = compactStrings(input.WatchPaths)
	source.AcceptedRisks = compactStrings(input.AcceptedRisks)
	if input.AutoDeploy != nil {
		source.AutoDeploy = *input.AutoDeploy
	}
	s.validateGit(ctx, *source, fields)
}

func (s *Service) validateGit(ctx context.Context, source GitSource, fields apperr.Fields) {
	if !application.ValidRepository(source.Repository) {
		fields.Add("git.repository", "Use uma URL https:// ou git@host:org/repo.git.")
	}
	if !application.ValidRef(source.Branch) {
		fields.Add("git.branch", "Nome de branch inválido.")
	}
	if source.Tag != "" && !application.ValidRef(source.Tag) {
		fields.Add("git.tag", "Nome de tag inválido.")
	}
	if !application.ValidRelativePath(source.BaseDirectory) || !composeDirPattern(source.BaseDirectory) {
		fields.Add("git.base_directory", "Use uma pasta relativa à raiz do repositório, sem '..'.")
	}
	if !composeFilePattern.MatchString(source.ComposeFilePath()) || strings.Contains(source.ComposeFilePath(), "..") {
		fields.Add("git.compose_path", "Use o caminho de um arquivo .yaml ou .yml dentro do repositório.")
	}
	for _, pattern := range source.WatchPaths {
		if !application.ValidWatchPattern(pattern) {
			fields.Add("git.watch_paths", "Caminho de monitoramento inválido: use globs relativos ao repositório, ex.: apps/api/**.")
		}
	}
	for _, risk := range source.AcceptedRisks {
		if !slices.Contains(knownRisks, risk) {
			fields.Add("git.accepted_risks", "Risco desconhecido: %q.", risk)
		}
	}
	s.validateGitCredentials(ctx, source, fields)
}

func (s *Service) validateGitCredentials(ctx context.Context, source GitSource, fields apperr.Fields) {
	if application.UsesSSH(source.Repository) && source.PrivateKeyID == "" {
		fields.Add("git.private_key_id", "Repositórios via SSH exigem uma chave de deploy.")
		return
	}
	if source.PrivateKeyID == "" {
		return
	}
	if s.keys == nil {
		fields.Add("git.private_key_id", "Chaves de deploy não estão disponíveis nesta instância.")
	} else if _, err := s.keys.Material(ctx, source.PrivateKeyID); err != nil {
		fields.Add("git.private_key_id", "Chave de deploy não encontrada.")
	}
}

func composeDirPattern(base string) bool {
	return base == "" || composeFilePattern.MatchString(base+"/x.yaml")
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func compactStrings(values []string) []string {
	out := []string{}
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// placeGitCheckout points the stack at the checkout kept on its server.
func (s *Service) placeGitCheckout(ctx context.Context, stack *Stack) error {
	_, target, err := s.connector.Executor(ctx, stack.ServerID)
	if err != nil {
		return err
	}
	stack.Directory = path.Join(target.ResourceDir(docker.TypeStack, stack.ID), checkoutDirName)
	stack.ComposeFile = stack.Git.ComposeFilePath()
	return nil
}

// gitSource finds the compose file of a Git stack. Before the first
// deployment there is no checkout: the file is then unresolved, which is not
// an error, and only what can be checked without it is validated.
func (s *Service) gitSource(ctx context.Context, stack *Stack) (raw string, resolved bool, err error) {
	ex, _, err := s.connector.Executor(ctx, stack.ServerID)
	if err != nil {
		return "", false, err
	}
	raw, err = readSource(ctx, ex, path.Join(stack.Directory, stack.ComposeFile))
	return raw, err == nil, nil
}

// WebhookSecret returns the secret that authenticates the push webhooks of a Git stack.
func (s *Service) WebhookSecret(ctx context.Context, id string) (string, error) {
	if err := s.git.available(); err != nil {
		return "", err
	}
	return s.git.webhookSecret(ctx, id)
}

// RotateWebhookSecret replaces the webhook secret, invalidating the old one.
func (s *Service) RotateWebhookSecret(ctx context.Context, id string) (string, error) {
	if err := s.git.available(); err != nil {
		return "", err
	}
	secret := idgen.Token(24)
	return secret, s.git.setWebhookSecret(ctx, id, secret)
}

// ResetGitHostKey forgets the SSH host keys pinned for the repository of a
// Git stack. Only call it after the operator checked the new fingerprint.
func (s *Service) ResetGitHostKey(ctx context.Context, id string) error {
	stack, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !stack.FromGit() {
		return apperr.Invalid("Esta stack não vem de um repositório Git.")
	}
	ex, target, err := s.connector.Executor(ctx, stack.ServerID)
	if err != nil {
		return err
	}
	release, err := application.AcquireBuild(ctx, stack.ServerID, nil)
	if err != nil {
		return err
	}
	defer release()
	known := path.Join(target.ResourceDir(docker.TypeStack, id), "git", "known_hosts")
	if _, err := ex.Run(ctx, shell.Join("rm", "-f", known)); err != nil {
		return apperr.Unavailable("Não foi possível redefinir a confiança SSH do host Git: %s", err)
	}
	return nil
}

// errNotCloned says a Git stack has no checkout yet: nothing was deployed.
var errNotCloned = apperr.Unavailable("A stack ainda não foi implantada: o repositório é clonado no primeiro deploy.")

// resolveGitSource is resolveSource for a stack created from a repository.
func (s *Service) resolveGitSource(ctx context.Context, stack *Stack, fields apperr.Fields) (string, bool, error) {
	if stack.Template != "" || stack.Compose != "" || stack.Directory != "" || stack.ComposeFile != "" {
		fields.Add("git.repository", "Escolha uma única origem: catálogo, compose, pasta ou repositório Git.")
		return "", false, nil
	}
	if err := s.placeGitCheckout(ctx, stack); err != nil {
		return "", false, err
	}
	return s.gitSource(ctx, stack)
}

// updateGitSource applies a change of the repository settings and finds the compose file again.
func (s *Service) updateGitSource(ctx context.Context, stack *Stack, input *GitInput, fields apperr.Fields) (string, bool, error) {
	if input != nil {
		updated := *stack.Git
		s.applyGitInput(ctx, &updated, *input, fields)
		stack.Git = &updated
	}
	if err := s.placeGitCheckout(ctx, stack); err != nil {
		return "", false, err
	}
	return s.gitSource(ctx, stack)
}

// saveNewGitSource stores the source of a stack just created; a failure
// takes the stack back, so nothing is left half registered.
func (s *Service) saveNewGitSource(ctx context.Context, stack Stack) error {
	if !stack.FromGit() {
		return nil
	}
	if err := s.git.insert(ctx, stack.ID, *stack.Git, idgen.Token(24)); err != nil {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM stacks WHERE id = ?`, stack.ID)
		return err
	}
	return nil
}

func (s *Service) saveGitSource(ctx context.Context, stack Stack) error {
	if !stack.FromGit() {
		return nil
	}
	return s.git.update(ctx, stack.ID, *stack.Git)
}
