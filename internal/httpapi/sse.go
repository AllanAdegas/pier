package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const sseHeartbeat = 20 * time.Second

// eventStream writes Server-Sent Events to one client.
type eventStream struct {
	writer     http.ResponseWriter
	controller *http.ResponseController
}

func newEventStream(w http.ResponseWriter) *eventStream {
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	// Tell reverse proxies (nginx, Traefik) not to buffer the stream.
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	stream := &eventStream{writer: w, controller: http.NewResponseController(w)}
	_ = stream.controller.Flush()
	return stream
}

// send emits one event; payload is JSON-encoded.
func (s *eventStream) send(event string, id int64, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if id > 0 {
		if _, err := fmt.Fprintf(s.writer, "id: %d\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(s.writer, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	return s.controller.Flush()
}

// heartbeat keeps idle connections open through proxies.
func (s *eventStream) heartbeat() error {
	if _, err := fmt.Fprint(s.writer, ": ping\n\n"); err != nil {
		return err
	}
	return s.controller.Flush()
}
