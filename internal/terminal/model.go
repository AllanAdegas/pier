// Package terminal opens interactive shells inside containers for people in
// the panel. It owns who may open one (tickets bound to a browser session),
// how many may be open (per user and per server) and how long one lives
// (idle timeout and maximum duration). The transport is abstracted as a Peer
// so the HTTP layer only adapts its WebSocket.
package terminal

import (
	"time"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
)

// Kind is what a terminal is opened on.
type Kind string

const (
	KindApplication Kind = docker.TypeApplication
	KindDatabase    Kind = docker.TypeDatabase
	KindStack       Kind = docker.TypeStack
	// KindContainer is any container of a server, managed by Pier or not.
	KindContainer Kind = "container"
)

// Shell choices; ShellAuto prefers bash and falls back to sh.
const (
	ShellAuto = "auto"
	ShellSh   = "sh"
	ShellBash = "bash"
)

// Limits bound what terminals may cost.
type Limits struct {
	// IdleTimeout closes a terminal nobody typed into for that long.
	IdleTimeout time.Duration
	// MaxDuration closes any terminal after that long, active or not.
	MaxDuration time.Duration
	PerUser     int
	PerServer   int
	TicketTTL   time.Duration
}

// DefaultLimits are the limits of the panel.
func DefaultLimits() Limits {
	return Limits{IdleTimeout: 15 * time.Minute, MaxDuration: 8 * time.Hour, PerUser: 3, PerServer: 4, TicketTTL: 30 * time.Second}
}

// MaxFrameBytes bounds one message from the browser (keystrokes or a pasted
// chunk). The web client splits larger pastes.
const MaxFrameBytes = 64 << 10

// Window size bounds; larger values are clamped.
const (
	maxCols = 500
	maxRows = 200
)

// Owner identifies who a ticket was issued to: the user and the browser
// session, so a ticket leaked to another session is useless.
type Owner struct {
	UserID    string
	SessionID string
}

// Request asks for a terminal. ServerID and ResourceID come from the route,
// never from the body; Container and Service only narrow the choice inside
// the resource.
type Request struct {
	Kind       Kind
	ServerID   string
	ResourceID string
	Service    string
	Container  string
	Shell      string
}

func (r *Request) validate() error {
	if r.Shell == "" {
		r.Shell = ShellAuto
	}
	if r.Shell != ShellAuto && r.Shell != ShellSh && r.Shell != ShellBash {
		return apperr.Invalid("Shell inválido. Use auto, sh ou bash.")
	}
	switch r.Kind {
	case KindApplication, KindDatabase, KindStack, KindContainer:
	default:
		return apperr.Invalid("Tipo de terminal desconhecido.")
	}
	if r.ServerID == "" || r.ResourceID == "" {
		return apperr.NotFound("Recurso")
	}
	for _, name := range []string{r.Service, r.Container} {
		if name != "" && !docker.ValidObjectName(name) {
			return apperr.NotFound("Container")
		}
	}
	if r.Kind == KindContainer && !docker.ValidObjectName(r.ResourceID) {
		return apperr.NotFound("Container")
	}
	return nil
}

// Container is the container a terminal runs in.
type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Service string `json:"service,omitempty"`
}

// Grant is what a ticket allows: one terminal in one container.
type Grant struct {
	Kind       Kind      `json:"kind"`
	ServerID   string    `json:"server_id"`
	ResourceID string    `json:"resource_id"`
	Container  Container `json:"container"`
	Shell      string    `json:"shell"`
}

// Ticket is handed to the browser, which opens the WebSocket with it.
type Ticket struct {
	Ticket    string    `json:"ticket"`
	ExpiresIn int       `json:"expires_in"`
	Container Container `json:"container"`
	Shell     string    `json:"shell"`
}

// End reasons reported to the browser and recorded in the audit log.
const (
	ReasonExit        = "exit"
	ReasonClient      = "client_closed"
	ReasonIdle        = "idle"
	ReasonMaxDuration = "max_duration"
	ReasonShutdown    = "server_closed"
	ReasonFailed      = "failed"
)

// Report describes a finished terminal, for the audit log. It never holds
// what was typed or printed.
type Report struct {
	Grant    Grant
	Started  time.Time
	Duration time.Duration
	Reason   string
	// ExitCode is set when the shell exited by itself.
	ExitCode *int
}
