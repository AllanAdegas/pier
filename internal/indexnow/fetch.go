package indexnow

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"pier/internal/platform/httpclient"
	"pier/internal/version"
)

// requestTimeout bounds every request of a submission.
const requestTimeout = 10 * time.Second

// errNotPublic is returned when a host resolves to an address that is not on
// the public internet: Pier never fetches its own network on behalf of a
// configured domain.
var errNotPublic = errors.New("address is not public")

// errTooLarge is returned when a document exceeds its read limit.
var errTooLarge = errors.New("document too large")

// sharedAddressSpace (100.64.0.0/10) is carrier-grade NAT, not the internet.
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// isPublic tells whether an address is reachable on the public internet.
func isPublic(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsGlobalUnicast() && !address.IsPrivate() && !address.IsLoopback() &&
		!address.IsLinkLocalUnicast() && !sharedAddressSpace.Contains(address)
}

// refuseNonPublic checks the address a connection is about to use, after DNS
// resolution, so a host that resolves (or rebinds) to a private address is
// refused at the socket.
func refuseNonPublic(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	parsed, err := netip.ParseAddr(host)
	if err != nil || !isPublic(parsed) {
		return fmt.Errorf("%w: %s", errNotPublic, host)
	}
	return nil
}

// NewHTTPClient creates the client of submissions: public addresses only,
// redirects within the same origin, bounded time.
func NewHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: requestTimeout, Control: refuseNonPublic}
	transport := &http.Transport{
		DialContext: dialer.DialContext, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: requestTimeout, ResponseHeaderTimeout: requestTimeout,
		Proxy: nil,
	}
	return &http.Client{Transport: transport, Timeout: requestTimeout, CheckRedirect: httpclient.SameOrigin}
}

// fetcher reads public documents of a site with a size limit.
type fetcher struct{ client *http.Client }

// get returns the body of a 200 response, at most limit bytes after
// decompression. Another status is an error that carries it.
func (f fetcher) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Pier/"+version.Number+" (IndexNow)")
	response, err := f.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, statusError{status: response.StatusCode}
	}
	body, err := readLimited(response.Body, limit)
	if err != nil {
		return nil, err
	}
	return gunzipIfNeeded(body, limit)
}

// statusError is an unexpected HTTP status.
type statusError struct{ status int }

func (e statusError) Error() string { return fmt.Sprintf("HTTP %d", e.status) }

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errTooLarge
	}
	return body, nil
}

// gunzipIfNeeded expands a gzip body (sitemap.xml.gz), with the same limit
// on the expanded size so a small file cannot unfold into gigabytes.
func gunzipIfNeeded(body []byte, limit int64) ([]byte, error) {
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		return body, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return readLimited(reader, limit)
}
