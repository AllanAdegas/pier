package registry

import (
	"context"

	"pier/internal/platform/executor"
)

// WithImageAuth runs fn with a docker command authenticated for the registry
// of image: a private client configuration holding only the credential
// stored for its host, or the plain docker command when there is none. It
// serves commands that forward the credential themselves, such as
// `docker stack deploy --with-registry-auth`. Lines are masked like Pull's.
func (s *Service) WithImageAuth(ctx context.Context, ex executor.Executor, image string, fn func(docker string, mask func(Output) Output) error) error {
	found, ok, err := s.credentialForHost(ctx, HostOf(image))
	if err != nil {
		return err
	}
	if !ok {
		return fn("docker", func(output Output) Output { return masked(output, "") })
	}
	return s.withConfig(ctx, ex, found, func(docker string) error {
		return fn(docker, func(output Output) Output { return masked(output, found.password) })
	})
}
