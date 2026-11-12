package application

import (
	"context"
	"sort"
	"time"
)

// DefaultPreviewJanitorInterval is how often stale previews are looked for.
const DefaultPreviewJanitorInterval = time.Hour

// RunJanitor sweeps previews every interval until ctx is cancelled.
func (p *PreviewService) RunJanitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.Sweep(ctx)
		}
	}
}

// Sweep destroys previews without activity for longer than their TTL and,
// when the limit was lowered, the least recently active beyond it. It
// returns how many were destroyed; failures are logged and retried later.
func (p *PreviewService) Sweep(ctx context.Context) int {
	previews, err := p.store.previews(ctx, "")
	if err != nil {
		p.logger.Error("list previews for the janitor", "error", err)
		return 0
	}
	byApplication := map[string][]Preview{}
	for _, preview := range previews {
		byApplication[preview.ApplicationID] = append(byApplication[preview.ApplicationID], preview)
	}
	destroyed := 0
	for applicationID, list := range byApplication {
		destroyed += p.sweepApplication(ctx, applicationID, list)
	}
	return destroyed
}

func (p *PreviewService) sweepApplication(ctx context.Context, applicationID string, previews []Preview) int {
	app, err := p.store.Get(ctx, applicationID)
	if err != nil {
		return 0
	}
	settings, err := p.store.previewSettings(ctx, applicationID)
	if err != nil {
		return 0
	}
	sort.SliceStable(previews, func(a, b int) bool { return previews[a].LastActivityAt.After(previews[b].LastActivityAt) })
	destroyed := 0
	for rank, preview := range previews {
		stale := p.now().Sub(preview.LastActivityAt) > settings.ttl()
		if (!stale && rank < settings.MaxPreviews) || preview.Status == PreviewDestroying {
			continue
		}
		if err := p.destroy(ctx, app, preview); err != nil {
			p.logger.Warn("janitor could not destroy preview", "application", applicationID, "pull_request", preview.Number, "error", err)
			continue
		}
		p.logger.Info("janitor destroyed preview", "application", applicationID, "pull_request", preview.Number, "stale", stale)
		destroyed++
	}
	return destroyed
}
