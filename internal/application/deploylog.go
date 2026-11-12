package application

import (
	"context"
	"regexp"
	"strings"
	"sync/atomic"

	"pier/internal/platform/i18n"
	"pier/internal/platform/pubsub"
)

const (
	maxLogLineLength = 4000
	minSecretLength  = 6
	// maxOutputLines caps what one deployment keeps of the output of its
	// commands. A build prints a few thousand lines; code under build that
	// prints forever must not fill the database of the panel.
	maxOutputLines = 20000
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
	OutputRedactor
	outputLines atomic.Int64
}

func (l *deployLog) redact(values ...string) { l.OutputRedactor.Redact(values...) }
func (l *deployLog) mask(text string) string { return l.OutputRedactor.Mask(text) }

// Info, Error and Success record a message, in the language of the instance.
func (l *deployLog) Info(format string, args ...any) {
	l.write(LogInfo, i18n.Instance().Sprintf(format, args...))
}

func (l *deployLog) Error(format string, args ...any) {
	l.write(LogError, i18n.Instance().Sprintf(format, args...))
}

func (l *deployLog) Success(format string, args ...any) {
	l.write(LogSuccess, i18n.Instance().Sprintf(format, args...))
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
	if level == LogOutput {
		switch count := l.outputLines.Add(1); {
		case count == maxOutputLines+1:
			level, line = LogError, i18n.Instance().Sprintf("Saída longa demais: as linhas depois de %d foram descartadas.", maxOutputLines)
		case count > maxOutputLines:
			return
		}
	}
	line = l.mask(line)
	if len(line) > maxLogLineLength {
		line = line[:maxLogLineLength] + i18n.Instance().Text("… (linha truncada)")
	}
	// Logs must survive cancellation of the deployment context.
	entry, err := l.store.appendLog(context.Background(), l.deploymentID, level, line)
	if err != nil {
		return
	}
	l.hub.Publish(l.deploymentID, entry)
}
