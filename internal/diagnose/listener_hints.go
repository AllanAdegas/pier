package diagnose

import "pier/internal/platform/i18n"

const listenerDocURL = "https://docs.docker.com/engine/network/"

// loopbackFix shows how to bind to all interfaces in the stacks Pier deploys.
const loopbackFix = "A aplicação só aceita conexões de dentro do contêiner (127.0.0.1), " +
	"então o proxy não a alcança. " +
	"Faça-a escutar em 0.0.0.0 (todas as interfaces) ou defina a variável HOST=0.0.0.0. " +
	"Node: app.listen(PORT, '0.0.0.0'). " +
	"Python (Flask): app.run(host='0.0.0.0', port=PORT). " +
	"Go: http.ListenAndServe(':' + port, handler). " +
	"Ruby (Puma): bind 'tcp://0.0.0.0:3000'. " +
	"PHP: php -S 0.0.0.0:8000. " +
	"Java (Spring): server.address=0.0.0.0."

// ExplainListeners compares the port an application is configured with against
// the sockets it listens on inside the container. It returns at most one hint:
// a port mismatch takes precedence over a loopback-only binding. language is a
// BCP 47 tag; unknown tags fall back to Portuguese.
func ExplainListeners(configuredPort int, listeners []Listener, language string) []Hint {
	if len(listeners) == 0 {
		return nil
	}
	translator := resolveLanguage(language)
	if !listensOn(listeners, configuredPort) {
		return []Hint{portMismatchHint(translator, servicePort(listeners))}
	}
	if listensOnlyOnLoopback(listeners, configuredPort) {
		return []Hint{loopbackHint(translator)}
	}
	return nil
}

func resolveLanguage(tag string) i18n.Language {
	language, ok := i18n.Parse(tag)
	if !ok {
		return i18n.Portuguese
	}
	return language
}

func portMismatchHint(language i18n.Language, actualPort int) Hint {
	return Hint{
		Code:   "listener_port_mismatch",
		Title:  language.Text("Porta configurada diferente da porta da aplicação"),
		Fix:    language.Sprintf("a aplicação escuta na porta %d; ajuste a porta para %d", actualPort, actualPort),
		DocURL: listenerDocURL,
	}
}

func loopbackHint(language i18n.Language) Hint {
	return Hint{
		Code:   "listener_loopback_only",
		Title:  language.Text("A aplicação escuta apenas em localhost"),
		Fix:    language.Text(loopbackFix),
		DocURL: listenerDocURL,
	}
}

// servicePort is the port the application serves on: the first non-loopback
// socket when there is one, otherwise the first listener.
func servicePort(listeners []Listener) int {
	for _, listener := range listeners {
		if !listener.Loopback {
			return listener.Port
		}
	}
	return listeners[0].Port
}

func listensOn(listeners []Listener, port int) bool {
	return len(portListeners(listeners, port)) > 0
}

func listensOnlyOnLoopback(listeners []Listener, port int) bool {
	matching := portListeners(listeners, port)
	if len(matching) == 0 {
		return false
	}
	for _, listener := range matching {
		if !listener.Loopback {
			return false
		}
	}
	return true
}

func portListeners(listeners []Listener, port int) []Listener {
	var matching []Listener
	for _, listener := range listeners {
		if listener.Port == port {
			matching = append(matching, listener)
		}
	}
	return matching
}
