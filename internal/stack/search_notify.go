package stack

import "context"

// SearchNotifier tells search engines about the pages of a stack after a
// successful deployment. It reports through report and never fails it.
type SearchNotifier interface {
	Deployed(ctx context.Context, stackID string, report func(string))
}

// SetSearchNotifier installs the notifier; nil notifies nobody.
func (s *Service) SetSearchNotifier(notifier SearchNotifier) { s.search = notifier }

// notifySearchEngines runs after a deployment of any origin succeeded; stop
// and restart never reach it.
func (s *Service) notifySearchEngines(ctx context.Context, run *stackRun) {
	if s.search != nil {
		s.search.Deployed(ctx, run.stack.ID, run.operation.Append)
	}
}
