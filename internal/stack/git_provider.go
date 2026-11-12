package stack

import "pier/internal/application"

// SetGitProvider resolves installation credentials for selected Git stacks.
func (s *Service) SetGitProvider(provider application.GitCredentials) { s.gitProvider = provider }
