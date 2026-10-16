package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

const deliveryTimeout = 15 * time.Second

// Service manages channels and delivers events to them.
type Service struct {
	db      *sql.DB
	box     *secretbox.Box
	senders map[Kind]Sender
	logger  *slog.Logger
}

// NewService creates the service.
func NewService(db *sql.DB, box *secretbox.Box, logger *slog.Logger) *Service {
	client := &http.Client{Timeout: deliveryTimeout}
	return &Service{db: db, box: box, senders: defaultSenders(client), logger: logger}
}

// Publish delivers the event to every subscribed channel in the background.
// Delivery failures are logged and never propagate to the publisher.
func (s *Service) Publish(event Event) {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*deliveryTimeout)
		defer cancel()
		channels, err := s.List(ctx)
		if err != nil {
			s.logger.Error("list notification channels", "error", err)
			return
		}
		for _, channel := range channels {
			if !channel.Enabled || !slices.Contains(channel.Events, event.Type) {
				continue
			}
			if err := s.deliver(ctx, channel, event); err != nil {
				s.logger.Warn("deliver notification", "channel", channel.Name, "event", event.Type, "error", err)
			}
		}
	}()
}

// Test sends a sample message through one channel and reports the outcome.
func (s *Service) Test(ctx context.Context, id string) error {
	channel, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	event := Event{
		Type: TestMessage, Title: "Pier — teste de notificação",
		Message:    "Se você está lendo isto, o canal \"" + channel.Name + "\" está funcionando.",
		OccurredAt: time.Now(),
	}
	if err := s.deliver(ctx, channel, event); err != nil {
		return apperr.Unavailable("O envio falhou: %s", err)
	}
	return nil
}

func (s *Service) deliver(ctx context.Context, channel Channel, event Event) error {
	sender, ok := s.senders[channel.Kind]
	if !ok {
		return errors.New("unknown channel kind " + string(channel.Kind))
	}
	ctx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	return sender.Send(ctx, channel.Config, event)
}

// List returns every channel.
func (s *Service) List(ctx context.Context) ([]Channel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, kind, config, events, enabled, created_at FROM notification_channels ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	channels := []Channel{}
	for rows.Next() {
		channel, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	return channels, rows.Err()
}

// Get returns one channel.
func (s *Service) Get(ctx context.Context, id string) (Channel, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, kind, config, events, enabled, created_at FROM notification_channels WHERE id = ?`, id)
	channel, err := s.scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Channel{}, apperr.NotFound("Canal de notificação")
	}
	return channel, err
}

// Create registers a channel.
func (s *Service) Create(ctx context.Context, input Input) (Channel, error) {
	channel := Channel{ID: idgen.New(), CreatedAt: time.Now()}
	if err := s.apply(&channel, input); err != nil {
		return Channel{}, err
	}
	sealed, events, err := s.encode(channel)
	if err != nil {
		return Channel{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO notification_channels
		(id, name, kind, config, events, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		channel.ID, channel.Name, channel.Kind, sealed, events, channel.Enabled, sqlite.FormatTime(channel.CreatedAt))
	return channel, err
}

// Update changes a channel.
func (s *Service) Update(ctx context.Context, id string, input Input) (Channel, error) {
	channel, err := s.Get(ctx, id)
	if err != nil {
		return Channel{}, err
	}
	if err := s.apply(&channel, input); err != nil {
		return Channel{}, err
	}
	sealed, events, err := s.encode(channel)
	if err != nil {
		return Channel{}, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE notification_channels
		SET name = ?, kind = ?, config = ?, events = ?, enabled = ? WHERE id = ?`,
		channel.Name, channel.Kind, sealed, events, channel.Enabled, id)
	return channel, err
}

// Delete removes a channel.
func (s *Service) Delete(ctx context.Context, id string) error {
	err := sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?`, id))
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound("Canal de notificação")
	}
	return err
}

func (s *Service) apply(channel *Channel, input Input) error {
	fields := apperr.Fields{}
	channel.Name = strings.TrimSpace(input.Name)
	channel.Kind = input.Kind
	channel.Enabled = input.Enabled
	channel.Events = input.Events
	channel.Config = map[string]string{}
	if channel.Name == "" {
		fields.Add("name", "Informe o nome do canal.")
	}
	sender, known := s.senders[input.Kind]
	if !known {
		fields.Add("kind", "Tipo de canal desconhecido.")
		return fields.Err()
	}
	for key, value := range input.Config {
		channel.Config[key] = strings.TrimSpace(value)
	}
	for _, key := range sender.RequiredConfig() {
		if channel.Config[key] == "" {
			fields.Add("config."+key, "Campo obrigatório.")
		}
	}
	for _, key := range []string{"webhook_url", "url"} {
		if value := channel.Config[key]; value != "" && !validHTTPURL(value) {
			fields.Add("config."+key, "Informe uma URL http(s) válida.")
		}
	}
	if len(channel.Events) == 0 {
		fields.Add("events", "Selecione ao menos um evento.")
	}
	for _, event := range channel.Events {
		if !slices.Contains(AllEvents, event) {
			fields.Add("events", "Evento desconhecido: %s.", event)
		}
	}
	return fields.Err()
}

func (s *Service) encode(channel Channel) (sealedConfig, events string, err error) {
	config, err := json.Marshal(channel.Config)
	if err != nil {
		return "", "", err
	}
	sealedConfig, err = s.box.Seal(string(config))
	if err != nil {
		return "", "", err
	}
	names := make([]string, len(channel.Events))
	for i, event := range channel.Events {
		names[i] = string(event)
	}
	return sealedConfig, strings.Join(names, ","), nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Service) scan(row rowScanner) (Channel, error) {
	var channel Channel
	var sealed, events, createdAt string
	if err := row.Scan(&channel.ID, &channel.Name, &channel.Kind, &sealed, &events, &channel.Enabled, &createdAt); err != nil {
		return Channel{}, err
	}
	config, err := s.box.Open(sealed)
	if err != nil {
		return Channel{}, err
	}
	if err := json.Unmarshal([]byte(config), &channel.Config); err != nil {
		return Channel{}, err
	}
	channel.Events = []EventType{}
	for _, name := range strings.Split(events, ",") {
		if name != "" {
			channel.Events = append(channel.Events, EventType(name))
		}
	}
	channel.CreatedAt = sqlite.ParseTime(createdAt)
	return channel, nil
}

func validHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
