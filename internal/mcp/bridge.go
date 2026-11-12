package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"pier/internal/platform/httpclient"
)

// maxBridgeMessage bounds one JSON-RPC message read from stdin.
const maxBridgeMessage = 4 << 20

// Bridge connects an MCP client that only speaks stdio (one JSON-RPC message
// per line) to Pier's HTTP endpoint. The endpoint is stateless and answers
// each message with one JSON body, so forwarding is message by message.
type Bridge struct {
	// URL is the base address of Pier, e.g. http://127.0.0.1:8700.
	URL    string
	Token  string
	Client *http.Client
}

// Run forwards messages until in ends or ctx is cancelled.
func (b Bridge) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	if b.URL == "" || b.Token == "" {
		return errors.New("set PIER_URL (e.g. http://127.0.0.1:8700) and PIER_TOKEN (a Pier API token)")
	}
	client := b.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute, CheckRedirect: httpclient.SameOrigin}
	}
	endpoint := strings.TrimSuffix(b.URL, "/") + Path
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), maxBridgeMessage)
	for scanner.Scan() {
		message := bytes.TrimSpace(scanner.Bytes())
		if len(message) == 0 {
			continue
		}
		reply := b.forward(ctx, client, endpoint, message)
		if len(reply) == 0 {
			continue
		}
		if _, err := out.Write(append(bytes.TrimSpace(reply), '\n')); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// forward posts one message and returns what to write back: the response,
// nothing for a notification, or a JSON-RPC error when HTTP failed.
func (b Bridge) forward(ctx context.Context, client *http.Client, endpoint string, message []byte) []byte {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(message))
	if err != nil {
		return rpcError(message, err.Error())
	}
	request.Header.Set("Authorization", "Bearer "+b.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		return rpcError(message, "Pier is unreachable: "+err.Error())
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBridgeMessage))
	if err != nil {
		return rpcError(message, err.Error())
	}
	if response.StatusCode >= http.StatusBadRequest {
		return rpcError(message, fmt.Sprintf("Pier answered HTTP %d: %s", response.StatusCode,
			strings.TrimSpace(string(body))))
	}
	return body
}

// rpcError answers a request with a JSON-RPC error. Notifications (no id)
// get no answer, as the protocol demands.
func rpcError(message []byte, text string) []byte {
	var request struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(message, &request) != nil || len(request.ID) == 0 || string(request.ID) == "null" {
		return nil
	}
	reply, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": text},
	})
	return reply
}
