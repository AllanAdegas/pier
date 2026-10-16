package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxToolOutput bounds what a tool hands back to the model: logs and lists
// can be large, and everything returned lands in the agent's context.
const maxToolOutput = 48 << 10

// apiRequest is one call to Pier's REST API made on behalf of a tool.
type apiRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   any
	// Present turns a successful response body into the tool's text; nil
	// returns the JSON as is.
	Present func(body []byte) (string, error)
}

// invoker runs API requests in process, through the same handler (routing,
// authentication, permissions, validation) that serves HTTP clients.
type invoker struct {
	api http.Handler
}

func (i invoker) call(ctx context.Context, authorization string, request apiRequest) (*sdk.CallToolResult, error) {
	httpRequest, err := i.newRequest(ctx, authorization, request)
	if err != nil {
		return nil, err
	}
	response := newRecorder()
	i.api.ServeHTTP(response, httpRequest)
	// Redirects only happen for paths the API does not serve as asked.
	if response.status >= http.StatusMultipleChoices {
		return errorResult(apiErrorMessage(response.status, response.body.Bytes())), nil
	}
	text := response.body.String()
	if request.Present != nil {
		if text, err = request.Present(response.body.Bytes()); err != nil {
			return nil, fmt.Errorf("format the response of %s: %w", request.Path, err)
		}
	}
	return textResult(truncate(text)), nil
}

func (i invoker) newRequest(ctx context.Context, authorization string, request apiRequest) (*http.Request, error) {
	target := request.Path
	if len(request.Query) > 0 {
		target += "?" + request.Query.Encode()
	}
	var body io.Reader = http.NoBody
	if request.Body != nil {
		encoded, err := json.Marshal(request.Body)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, request.Method, target, body)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", authorization)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.RemoteAddr = "127.0.0.1:0"
	return httpRequest, nil
}

// apiErrorMessage extracts the message of Pier's error body
// ({"error": {"code", "message"}}), falling back to the status.
func apiErrorMessage(status int, body []byte) string {
	var parsed struct {
		Error struct {
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) != nil || parsed.Error.Message == "" {
		return fmt.Sprintf("A API do Pier respondeu HTTP %d.", status)
	}
	message := parsed.Error.Message
	for field, problem := range parsed.Error.Fields {
		message += fmt.Sprintf(" [%s: %s]", field, problem)
	}
	return message
}

func textResult(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}
}

func errorResult(message string) *sdk.CallToolResult {
	result := textResult(message)
	result.IsError = true
	return result
}

func truncate(text string) string {
	if len(text) <= maxToolOutput {
		return text
	}
	// Cutting may split a UTF-8 sequence; drop the broken tail.
	cut := strings.ToValidUTF8(text[:maxToolOutput], "")
	return cut + fmt.Sprintf("\n… (truncated: %d of %d bytes shown)", len(cut), len(text))
}

// recorder is a minimal in-memory ResponseWriter.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newRecorder() *recorder { return &recorder{header: http.Header{}, status: http.StatusOK} }

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(p []byte) (int, error) { return r.body.Write(p) }
func (r *recorder) WriteHeader(status int)      { r.status = status }

// identifierPattern is the shape of Pier's resource IDs and template slugs.
// Anything else is refused before it can reach a path ("..", "/", "?").
var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func requireIdentifier(field, value string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("%s inválido: use o identificador mostrado pelas ferramentas de listagem", field)
	}
	return nil
}
