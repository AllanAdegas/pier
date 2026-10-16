package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"pier/internal/platform/pubsub"
)

const (
	maxLogLineLength = 4000
	minSecretLength  = 6
)

// ansiPattern matches terminal color/cursor sequences emitted by build tools.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// LogHub broadcasts deployment log lines to live viewers.
type LogHub = pubsub.Hub[LogEntry]

// deployLog writes the log of one deployment: every line is redacted,
// persisted and then broadcast.
type deployLog struct {
	store        *Store
	hub          *LogHub
	deploymentID string
	secrets      []string
}

// redact registers values that must never appear in the log.
func (l *deployLog) redact(values ...string) {
	for _, value := range values {
		if len(value) >= minSecretLength {
			l.secrets = append(l.secrets, value)
		}
	}
}

func (l *deployLog) Info(format string, args ...any) { l.write(LogInfo, fmt.Sprintf(format, args...)) }

func (l *deployLog) Error(format string, args ...any) {
	l.write(LogError, fmt.Sprintf(format, args...))
}

func (l *deployLog) Success(format string, args ...any) {
	l.write(LogSuccess, fmt.Sprintf(format, args...))
}

// Command records a command about to run, in a form safe to display.
func (l *deployLog) Command(display string) { l.write(LogCommand, display) }

// Output records one line produced by a command; it is an executor.LineSink.
func (l *deployLog) Output(line string) { l.write(LogOutput, line) }

func (l *deployLog) write(level LogLevel, line string) {
	line = strings.TrimRight(ansiPattern.ReplaceAllString(line, ""), " \t")
	if line == "" && level == LogOutput {
		return
	}
	for _, secret := range l.secrets {
		line = strings.ReplaceAll(line, secret, "••••••")
	}
	if len(line) > maxLogLineLength {
		line = line[:maxLogLineLength] + "… (linha truncada)"
	}
	// Logs must survive cancellation of the deployment context.
	entry, err := l.store.appendLog(context.Background(), l.deploymentID, level, line)
	if err != nil {
		return
	}
	l.hub.Publish(l.deploymentID, entry)
}
