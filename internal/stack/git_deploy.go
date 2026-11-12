package stack

import (
	"context"
	"fmt"
	"path"
	"strings"

	"pier/internal/application"
	"pier/internal/platform/apperr"
	"pier/internal/platform/oplog"
	"pier/internal/platform/shell"
)

// deployGit runs the compose file of a repository. The repository is cloned
// into a scratch directory and inspected there; only a checkout that passes
// replaces the one the running stack was started from. Then the pipeline is
// the one of a folder: validation by docker, Pier's override, build, up.
func (s *Service) deployGit(ctx context.Context, run *stackRun) error {
	git := run.stack.Git
	run.operation.Say("Repositório: %s (%s)", application.WithoutCredentials(git.Repository), git.Ref())
	s.withPendingCertificates(ctx, run)
	scratch := run.target.BuildDir("stack-" + run.stack.ID)
	discardDirectory(run, path.Dir(scratch), path.Base(scratch))
	defer discardDirectory(run, path.Dir(scratch), path.Base(scratch))
	sha, message, err := s.cloneRepository(ctx, run, scratch)
	if err != nil {
		return err
	}
	root := application.CheckoutPath(scratch)
	raw, err := s.inspectCheckout(ctx, run, root)
	if err != nil {
		return err
	}
	if err := installCheckout(ctx, run, root); err != nil {
		return err
	}
	run.operation.Say("Commit %s — %s", shortSHA(sha), message)
	if err := s.git.recordDeployment(ctx, run.stack.ID, git.Ref(), sha, message); err != nil {
		return err
	}
	doc, err := parseCompose(raw)
	if err != nil {
		return err
	}
	run.project = projectFor(ctx, run.ex, run.target, run.stack)
	run.lockBuild = lockBuild(run, doc)
	return s.runFolder(ctx, run, raw, doc)
}

// cloneRepository clones the configured ref into the scratch directory.
func (s *Service) cloneRepository(ctx context.Context, run *stackRun, scratch string) (sha, message string, err error) {
	git := run.stack.Git
	request := application.CloneRequest{
		Repository: git.Repository, Ref: git.Ref(), ServerID: run.stack.ServerID, BuildDir: scratch,
		TrustDir: path.Join(run.project.Dir, "git"),
	}
	if git.PrivateKeyID != "" {
		if s.keys == nil {
			return "", "", apperr.Unavailable("Chaves de deploy não estão disponíveis nesta instância.")
		}
		if request.PrivateKey, err = s.keys.Material(ctx, git.PrivateKeyID); err != nil {
			return "", "", err
		}
	}
	return application.CloneGitProvider(ctx, s.gitProvider, "stack", run.stack.ID, run.ex, request, &operationLog{operation: run.operation})
}

// inspectCheckout refuses a checkout that escapes itself, asks for powers the
// operator did not accept or points outside the repository. It returns the
// compose file.
func (s *Service) inspectCheckout(ctx context.Context, run *stackRun, root string) (string, error) {
	git := run.stack.Git
	file := git.ComposeFilePath()
	verdict, err := application.VerifyContained(ctx, run.ex, root, path.Join(root, file))
	if err != nil {
		return "", err
	}
	if err := containmentError(verdict, file); err != nil {
		return "", err
	}
	units, err := loadComposeTree(ctx, run.ex, root, file)
	if err != nil {
		return "", err
	}
	if err := checkContained(units); err != nil {
		return "", err
	}
	if err := s.checkRisks(ctx, run.stack, units); err != nil {
		return "", err
	}
	return readSource(ctx, run.ex, path.Join(root, file))
}

func containmentError(verdict application.Containment, file string) error {
	switch {
	case verdict.Missing != nil:
		return apperr.Invalid("O arquivo %s não existe no repositório (nesta branch ou tag).", file)
	case verdict.Escaping != "":
		return apperr.Invalid("O link simbólico %s aponta para fora do repositório ou não pode ser resolvido.", verdict.Escaping)
	case verdict.PathEscapes:
		return apperr.Invalid("O arquivo %s aponta, por link simbólico, para fora do repositório.", file)
	}
	return nil
}

// checkRisks refuses the deployment while the compose files ask for powers
// over the server that the operator has not accepted, and remembers what was asked.
func (s *Service) checkRisks(ctx context.Context, stack Stack, units []composeUnit) error {
	pending := stack.Git.pendingAmong(combinedDocument(units).risks())
	if err := s.git.recordPendingRisks(ctx, stack.ID, pending); err != nil {
		return err
	}
	if len(pending) > 0 {
		return apperr.Conflict("O compose do repositório pede poderes sobre o servidor que ainda não foram aceitos: %s. "+
			"Revise o arquivo e aceite esses riscos nas configurações da stack antes de implantar.", strings.Join(pending, ", "))
	}
	return nil
}

// installCheckout replaces the working tree of the stack by the inspected one.
func installCheckout(ctx context.Context, run *stackRun, root string) error {
	if err := removeCheckout(ctx, run); err != nil {
		return err
	}
	command := shell.Join("sh", "-c", `mkdir -p "$1" && mv "$2" "$1/$3"`, "sh", run.project.Dir, root, checkoutDirName)
	if _, err := run.ex.Run(ctx, command); err != nil {
		return fmt.Errorf("install checkout: %w", err)
	}
	return nil
}

// removeCheckout deletes the working tree of a Git stack. Files written by
// the clone container may belong to root, hence the container.
func removeCheckout(ctx context.Context, run *stackRun) error {
	if err := application.RemoveDirectory(ctx, run.ex, run.project.Dir, checkoutDirName); err != nil {
		return apperr.Unavailable("Não foi possível apagar o repositório clonado: %s", err)
	}
	return nil
}

func discardDirectory(run *stackRun, parent, name string) {
	_ = application.RemoveDirectory(context.WithoutCancel(context.Background()), run.ex, parent, name)
}

// lockBuild is the build gate of a Git stack: only when the compose file
// builds something does it wait for the server's build slot and check the disk.
func lockBuild(run *stackRun, doc document) func(context.Context) (func(), error) {
	if !buildsImages(doc) {
		return nil
	}
	return func(ctx context.Context) (func(), error) {
		release, err := application.AcquireBuild(ctx, run.stack.ServerID, func(note string) { run.operation.Append(note) })
		if err != nil {
			return nil, err
		}
		if err := application.CheckBuildDisk(ctx, run.ex); err != nil {
			release()
			return nil, err
		}
		return release, nil
	}
}

func buildsImages(doc document) bool {
	for _, service := range services(doc) {
		if _, builds := service["build"]; builds {
			return true
		}
	}
	return false
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// operationLog adapts the progress of an operation to the clone's log and
// keeps secrets out of what it shows.
type operationLog struct {
	operation *oplog.Operation
	secrets   []string
}

func (l *operationLog) Info(format string, args ...any) { l.operation.Say(format, args...) }
func (l *operationLog) Command(display string)          { l.operation.Append("$ " + display) }
func (l *operationLog) Output(line string)              { l.operation.Append(l.mask(line)) }
func (l *operationLog) Redact(values ...string) {
	for _, value := range values {
		if len(strings.TrimSpace(value)) >= 4 {
			l.secrets = append(l.secrets, value)
		}
	}
}

func (l *operationLog) mask(line string) string {
	for _, secret := range l.secrets {
		line = strings.ReplaceAll(line, secret, "••••••")
	}
	return line
}
