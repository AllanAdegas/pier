package logdrain

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
	"pier/internal/platform/idgen"
	"pier/internal/platform/secretbox"
)

// Service manages the drains and owns the forwarder.
type Service struct {
	store     store
	box       *secretbox.Box
	directory directory
	forwarder *Forwarder
	client    *http.Client
	now       func() time.Time
}

// NewService creates the service; call Run to start forwarding.
func NewService(db *sql.DB, box *secretbox.Box, connector Connector, redactors RedactorSource, logger *slog.Logger) *Service {
	forwarder := &Forwarder{store: store{db: db}, box: box, directory: directory{db: db}, connector: connector, redactors: redactors,
		logger: logger, retries: defaultRetries, now: time.Now, shippers: map[string]*shipper{}, cursors: map[string]time.Time{}}
	return &Service{store: store{db: db}, box: box, directory: directory{db: db}, forwarder: forwarder, now: time.Now}
}

// SetHTTPClient replaces the client of the sinks (tests).
func (s *Service) SetHTTPClient(client *http.Client) {
	s.client = client
	s.forwarder.client = client
}

// Run forwards logs until ctx ends.
func (s *Service) Run(ctx context.Context) { s.forwarder.Run(ctx) }

// View is a drain with its delivery counters.
type View struct {
	Drain
	Status Status `json:"status"`
}

// List returns every drain with its counters.
func (s *Service) List(ctx context.Context) ([]View, error) {
	drains, err := s.store.list(ctx, false)
	if err != nil {
		return nil, err
	}
	views := make([]View, len(drains))
	for index, drain := range drains {
		views[index] = View{Drain: drain.Drain, Status: s.forwarder.Status(drain.ID)}
	}
	return views, nil
}

// Create stores a new drain.
func (s *Service) Create(ctx context.Context, input Input) (View, error) {
	now := s.now()
	return s.save(ctx, stored{Drain: Drain{ID: idgen.New(), CreatedAt: now}}, input)
}

// Update changes a drain; a nil token keeps the stored one.
func (s *Service) Update(ctx context.Context, id string, input Input) (View, error) {
	current, err := s.store.get(ctx, id)
	if err != nil {
		return View{}, err
	}
	return s.save(ctx, current, input)
}

func (s *Service) save(ctx context.Context, current stored, input Input) (View, error) {
	input, err := input.normalize(current.sealedToken != "")
	if err != nil {
		return View{}, err
	}
	if found, err := s.directory.exists(ctx, input.ScopeType, input.ScopeID); err != nil || !found {
		if err != nil {
			return View{}, err
		}
		return View{}, apperr.Invalid("O projeto ou recurso escolhido não existe.")
	}
	if input.Token != nil {
		current.sealedToken = ""
		if *input.Token != "" {
			if current.sealedToken, err = s.box.Seal(*input.Token); err != nil {
				return View{}, err
			}
		}
	}
	current.Name, current.Kind, current.URL, current.Labels = input.Name, input.Kind, input.URL, input.Labels
	current.ScopeType, current.ScopeID, current.Enabled = input.ScopeType, input.ScopeID, input.Enabled
	current.HasToken, current.UpdatedAt = current.sealedToken != "", s.now()
	if err := s.store.save(ctx, current); err != nil {
		return View{}, err
	}
	return View{Drain: current.Drain, Status: s.forwarder.Status(current.ID)}, nil
}

// Delete removes a drain after the operator typed its name.
func (s *Service) Delete(ctx context.Context, id, confirmation string) error {
	current, err := s.store.get(ctx, id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirmation) != current.Name {
		return apperr.Invalid("Digite o nome do encaminhamento para confirmar a remoção.")
	}
	return s.store.delete(ctx, id)
}

// Test sends one record to the sink right away and reports the answer.
func (s *Service) Test(ctx context.Context, id string) error {
	current, err := s.store.get(ctx, id)
	if err != nil {
		return err
	}
	token, err := s.forwarder.openToken(current)
	if err != nil {
		return err
	}
	labels := map[string]string{"pier_test": "true"}
	for name, value := range current.Labels {
		labels[name] = value
	}
	record := Record{Time: s.now().UTC(), Message: i18n.Instance().Text("Teste de encaminhamento de logs do Pier."), Labels: labels}
	if err := newSink(current.Drain, token, s.client).Send(ctx, []Record{record}); err != nil {
		return apperr.Unavailable("O destino recusou o envio: %s", err)
	}
	return nil
}
