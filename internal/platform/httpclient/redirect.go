// Package httpclient holds policies shared by the HTTP clients Pier uses to
// reach services configured by its operators.
package httpclient

import (
	"errors"
	"net/http"
)

const maxRedirects = 5

// SameOrigin follows redirects only within the origin of the first request
// and never from HTTPS to HTTP. A configured endpoint must not be able to
// bounce Pier — with the body of a notification or a credential — to some
// other host. Use it as http.Client.CheckRedirect.
func SameOrigin(request *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("too many redirects")
	}
	first := via[0].URL
	if request.URL.Host != first.Host || (first.Scheme == "https" && request.URL.Scheme != "https") {
		return errors.New("redirect to another origin refused: " + request.URL.Redacted())
	}
	return nil
}

// Never returns redirects to the caller as they are, for APIs that have no
// reason to redirect. Use it as http.Client.CheckRedirect.
func Never(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
