package diagnose

import (
	"strings"
	"testing"

	"pier/internal/platform/i18n"
)

func TestExplainListeners(t *testing.T) {
	tests := []struct {
		name      string
		port      int
		listeners []Listener
		language  string
		wantCode  string
		wantInFix string
	}{
		{
			name:      "no listeners means the process is not up yet",
			port:      8080,
			listeners: nil,
			language:  "pt-BR",
		},
		{
			name:      "app listens on all interfaces at the configured port",
			port:      3000,
			listeners: []Listener{{Address: "0.0.0.0", Port: 3000}},
			language:  "pt-BR",
		},
		{
			name:      "app listens on another port",
			port:      8080,
			listeners: []Listener{{Address: "0.0.0.0", Port: 3000}},
			language:  "pt-BR",
			wantCode:  "listener_port_mismatch",
			wantInFix: "a aplicação escuta na porta 3000; ajuste a porta para 3000",
		},
		{
			name: "port mismatch prefers the non-loopback listener",
			port: 8080,
			listeners: []Listener{
				{Address: "127.0.0.1", Port: 9229, Loopback: true},
				{Address: "0.0.0.0", Port: 3000},
			},
			language:  "pt-BR",
			wantCode:  "listener_port_mismatch",
			wantInFix: "porta 3000",
		},
		{
			name:      "configured port bound only to loopback",
			port:      8080,
			listeners: []Listener{{Address: "127.0.0.1", Port: 8080, Loopback: true}},
			language:  "pt-BR",
			wantCode:  "listener_loopback_only",
			wantInFix: "HOST=0.0.0.0",
		},
		{
			name: "configured port reachable even if another socket is loopback-only",
			port: 8080,
			listeners: []Listener{
				{Address: "127.0.0.1", Port: 9229, Loopback: true},
				{Address: "::", Port: 8080},
			},
			language: "pt-BR",
		},
		{
			name:      "loopback hint in English",
			port:      8080,
			listeners: []Listener{{Address: "::1", Port: 8080, Loopback: true}},
			language:  "en",
			wantCode:  "listener_loopback_only",
			wantInFix: "HOST=0.0.0.0",
		},
		{
			name:      "port mismatch in English",
			port:      8080,
			listeners: []Listener{{Address: "0.0.0.0", Port: 3000}},
			language:  "en",
			wantCode:  "listener_port_mismatch",
			wantInFix: "the application listens on port 3000; set the port to 3000",
		},
		{
			name:      "unknown language falls back to Portuguese",
			port:      8080,
			listeners: []Listener{{Address: "0.0.0.0", Port: 3000}},
			language:  "fr",
			wantCode:  "listener_port_mismatch",
			wantInFix: "a aplicação escuta na porta 3000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hints := ExplainListeners(tt.port, tt.listeners, tt.language)
			if tt.wantCode == "" {
				if len(hints) != 0 {
					t.Fatalf("got hints %+v, want none", hints)
				}
				return
			}
			if len(hints) != 1 || hints[0].Code != tt.wantCode {
				t.Fatalf("got %+v, want one hint with code %q", hints, tt.wantCode)
			}
			if !strings.Contains(hints[0].Fix, tt.wantInFix) {
				t.Errorf("fix %q does not contain %q", hints[0].Fix, tt.wantInFix)
			}
			if hints[0].Title == "" || hints[0].DocURL == "" {
				t.Errorf("hint must carry title and doc URL: %+v", hints[0])
			}
		})
	}
}

func TestLoopbackHintShowsBindingExamplesPerStack(t *testing.T) {
	hints := ExplainListeners(8080, []Listener{{Address: "127.0.0.1", Port: 8080, Loopback: true}}, "pt-BR")
	if len(hints) != 1 {
		t.Fatalf("got %d hints, want 1", len(hints))
	}
	for _, stack := range []string{"Node", "Python", "Go", "Ruby", "PHP", "Java"} {
		if !strings.Contains(hints[0].Fix, stack) {
			t.Errorf("loopback fix has no %s example", stack)
		}
	}
}

func TestLoopbackHintIsTranslatedToEnglish(t *testing.T) {
	hints := ExplainListeners(8080, []Listener{{Address: "127.0.0.1", Port: 8080, Loopback: true}}, "en")
	if len(hints) != 1 {
		t.Fatalf("got %d hints, want 1", len(hints))
	}
	if hints[0].Title != "The application only listens on localhost" {
		t.Errorf("title = %q", hints[0].Title)
	}
	if strings.Contains(hints[0].Fix, "aplicação") {
		t.Errorf("fix still in Portuguese: %q", hints[0].Fix)
	}
}

func TestResolveLanguageAcceptsRegionalTags(t *testing.T) {
	if got := resolveLanguage("en-US"); got != i18n.English {
		t.Errorf("resolveLanguage(en-US) = %q, want English", got)
	}
	if got := resolveLanguage(""); got != i18n.Portuguese {
		t.Errorf("resolveLanguage(\"\") = %q, want Portuguese", got)
	}
}

func TestServicePortPrefersNonLoopback(t *testing.T) {
	tests := []struct {
		name      string
		listeners []Listener
		want      int
	}{
		{name: "single listener", listeners: []Listener{{Port: 80}}, want: 80},
		{name: "non-loopback wins", listeners: []Listener{{Port: 1, Loopback: true}, {Port: 2}}, want: 2},
		{
			name:      "all loopback falls back to first",
			listeners: []Listener{{Port: 1, Loopback: true}, {Port: 2, Loopback: true}},
			want:      1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := servicePort(tt.listeners); got != tt.want {
				t.Errorf("servicePort = %d, want %d", got, tt.want)
			}
		})
	}
}
