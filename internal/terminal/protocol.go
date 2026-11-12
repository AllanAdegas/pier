package terminal

import (
	"context"
	"encoding/json"

	"pier/internal/platform/executor"
)

// The wire protocol: binary frames carry terminal bytes both ways (keystrokes
// up, output down); text frames carry JSON control messages.

// Frame is one message of the transport.
type Frame struct {
	Binary bool
	Data   []byte
}

// Peer is the browser end of a terminal. Send may be called from several
// goroutines; Receive from one.
type Peer interface {
	Receive(ctx context.Context) (Frame, error)
	Send(ctx context.Context, frame Frame) error
}

// clientMessage is a control message from the browser.
type clientMessage struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

const clientResize = "resize"

// serverMessage is a control message to the browser.
type serverMessage struct {
	Type        string `json:"type"`
	Container   string `json:"container,omitempty"`
	Service     string `json:"service,omitempty"`
	Shell       string `json:"shell,omitempty"`
	IdleTimeout int    `json:"idle_timeout,omitempty"`
	MaxDuration int    `json:"max_duration,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Code        *int   `json:"code,omitempty"`
}

const (
	serverReady  = "ready"
	serverExit   = "exit"
	serverClosed = "closed"
)

func controlFrame(message serverMessage) Frame {
	data, _ := json.Marshal(message)
	return Frame{Data: data}
}

// parseResize reads a resize request; anything else is ignored.
func parseResize(data []byte) (executor.WindowSize, bool) {
	var message clientMessage
	if err := json.Unmarshal(data, &message); err != nil || message.Type != clientResize {
		return executor.WindowSize{}, false
	}
	if message.Cols == 0 || message.Rows == 0 {
		return executor.WindowSize{}, false
	}
	return clampSize(executor.WindowSize{Cols: message.Cols, Rows: message.Rows}), true
}
