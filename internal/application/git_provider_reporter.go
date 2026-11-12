package application

import (
	"context"
	"pier/internal/gitprovider"
	"sync"
	"time"
)

// GitAppReporter implements PreviewReporter with bounded asynchronous work.
// The coordinator wires it with PreviewService.SetReporter.
type GitAppReporter struct {
	Provider *gitprovider.Service
	slots    chan struct{}
	mu       sync.Mutex
	last     <-chan struct{}
}

func NewGitAppReporter(provider *gitprovider.Service) *GitAppReporter {
	return &GitAppReporter{Provider: provider, slots: make(chan struct{}, 4)}
}
func (r *GitAppReporter) ReportPreview(ctx context.Context, report PreviewReport) {
	if report.Preview.Provider != ProviderGitHub {
		return
	}
	select {
	case r.slots <- struct{}{}:
	default:
		return
	}
	r.mu.Lock()
	previous := r.last
	done := make(chan struct{})
	r.last = done
	r.mu.Unlock()
	go func() {
		defer close(done)
		defer func() { <-r.slots }()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		if previous != nil {
			select {
			case <-previous:
			case <-ctx.Done():
				return
			}
		}
		selection, err := r.Provider.Selection(ctx, "application", report.Application.ID)
		if err != nil {
			return
		}
		state := "pending"
		switch report.Event {
		case PreviewEventReady:
			state = "success"
		case PreviewEventFailed:
			state = "failure"
		case PreviewEventDestroyed:
			state = "error"
		}
		// Only public preview metadata is sent; deployment errors may contain secrets.
		_ = r.Provider.Report(ctx, selection, report.Preview.CommitSHA, report.Preview.Number, state, "Pier preview: "+string(report.Event)+"\n"+report.Preview.URL)
	}()
}
