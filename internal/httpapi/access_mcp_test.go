package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	rbac "pier/internal/access"
	"pier/internal/mcp"
)

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(request)
}

// TestMCPToolsRespectProjectRoles: tools call the REST routes in process
// with the agent's token, so the same role checks and list filtering apply.
func TestMCPToolsRespectProjectRoles(t *testing.T) {
	world := newAccessWorld(t)
	developer, token := testIdentity(t, world.a, "agent")
	grantMember(t, world.a, developer.ID, "pa", rbac.Developer)
	server := httptest.NewServer(world.a.Handler())
	t.Cleanup(server.Close)
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint: server.URL + mcp.Path, HTTPClient: &http.Client{Transport: bearerTransport{token}}, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	call := func(name string, arguments map[string]any) (string, bool) {
		result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var text strings.Builder
		for _, content := range result.Content {
			if piece, ok := content.(*sdk.TextContent); ok {
				text.WriteString(piece.Text)
			}
		}
		return text.String(), result.IsError
	}
	if text, failed := call("list_projects", nil); failed || !strings.Contains(text, `"pa"`) || strings.Contains(text, `"pb"`) {
		t.Fatalf("list_projects not filtered: %q %v", text, failed)
	}
	if text, failed := call("get_application", map[string]any{"id": "appa"}); failed {
		t.Fatalf("own application: %q", text)
	}
	for name, arguments := range map[string]map[string]any{
		"get_application":    {"id": "appb"},
		"deploy_application": {"id": "appb"},
		"list_servers":       nil,
		"get_dashboard":      nil,
	} {
		if text, failed := call(name, arguments); !failed || !(strings.Contains(text, "Access denied") || strings.Contains(text, "Acesso não autorizado")) {
			t.Errorf("%s %v allowed: %q", name, arguments, text)
		}
	}
}
