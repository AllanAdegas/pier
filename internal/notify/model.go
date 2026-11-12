// Package notify is the supporting context that tells people what happened:
// domain events are fanned out to the channels subscribed to them.
package notify

import (
	"time"
)

// EventType names a fact other contexts publish.
type EventType string

const (
	DeploymentSucceeded EventType = "deployment.succeeded"
	DeploymentFailed    EventType = "deployment.failed"
	BackupSucceeded     EventType = "backup.succeeded"
	BackupFailed        EventType = "backup.failed"
	TaskFailed          EventType = "task.failed"
	ServerUnreachable   EventType = "server.unreachable"
	// Runtime alerts: what happens after a deployment succeeded.
	ContainerRestarting EventType = "container.restarting"
	ContainerOOMKilled  EventType = "container.oom_killed"
	ContainerUnhealthy  EventType = "container.unhealthy"
	ServerDiskHigh      EventType = "server.disk_high"
	ServerResourceHigh  EventType = "server.resource_high"
	CertificateExpiring EventType = "certificate.expiring"
	BackupMissed        EventType = "backup.missed"
	TaskMissed          EventType = "task.missed"
	RestoreTestFailed   EventType = "backup.restore_test_failed"
	UptimeDown          EventType = "uptime.down"
	// AlertResolved closes any of the runtime alerts above ("back to normal").
	AlertResolved EventType = "alert.resolved"
	// TestMessage is only sent by the "test channel" action.
	TestMessage EventType = "test"
)

// AllEvents lists the events a channel can subscribe to.
var AllEvents = []EventType{
	DeploymentSucceeded, DeploymentFailed, BackupSucceeded, BackupFailed, TaskFailed, ServerUnreachable,
	ContainerRestarting, ContainerOOMKilled, ContainerUnhealthy, ServerDiskHigh, ServerResourceHigh,
	CertificateExpiring, BackupMissed, TaskMissed, RestoreTestFailed, UptimeDown, AlertResolved,
}

// Event is an immutable fact that already happened.
type Event struct {
	Type       EventType `json:"type"`
	Title      string    `json:"title"`
	Message    string    `json:"message"`
	Resource   string    `json:"resource"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Failure reports whether the event describes something that went wrong.
func (e Event) Failure() bool {
	switch e.Type {
	case DeploymentFailed, BackupFailed, TaskFailed, ServerUnreachable,
		ContainerRestarting, ContainerOOMKilled, ContainerUnhealthy, ServerDiskHigh, ServerResourceHigh,
		CertificateExpiring, BackupMissed, TaskMissed, RestoreTestFailed, UptimeDown:
		return true
	}
	return false
}

// Kind identifies the transport of a channel.
type Kind string

const (
	KindDiscord  Kind = "discord"
	KindSlack    Kind = "slack"
	KindTelegram Kind = "telegram"
	KindWebhook  Kind = "webhook"
	KindEmail    Kind = "email"
)

// Channel is a destination subscribed to a set of events.
type Channel struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Kind      Kind              `json:"kind"`
	Config    map[string]string `json:"config"`
	Events    []EventType       `json:"events"`
	Enabled   bool              `json:"enabled"`
	CreatedAt time.Time         `json:"created_at"`
}

// Input carries the editable fields of a channel.
type Input struct {
	Name    string            `json:"name"`
	Kind    Kind              `json:"kind"`
	Config  map[string]string `json:"config"`
	Events  []EventType       `json:"events"`
	Enabled bool              `json:"enabled"`
}

// Publisher is what other contexts depend on to emit events.
type Publisher interface {
	Publish(event Event)
}
