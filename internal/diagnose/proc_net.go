package diagnose

import (
	"encoding/hex"
	"net"
	"strconv"
	"strings"
)

// listenState is the /proc/net/tcp state code of a socket in LISTEN.
const listenState = "0A"

// Listener is a socket in LISTEN state. Loopback is true when it accepts only
// connections from inside the container (127.0.0.0/8 or ::1).
type Listener struct {
	Address  string
	Port     int
	Loopback bool
}

// ParseProcNet reads the LISTEN rows of /proc/net/tcp or /proc/net/tcp6 content.
// Callers concatenate both tables. Malformed rows are skipped, so a truncated
// table still yields the rows it has.
func ParseProcNet(content string) []Listener {
	var listeners []Listener
	for _, row := range strings.Split(content, "\n") {
		if listener, ok := parseProcNetRow(row); ok {
			listeners = append(listeners, listener)
		}
	}
	return listeners
}

func parseProcNetRow(row string) (Listener, bool) {
	fields := strings.Fields(row)
	if len(fields) < 4 || fields[3] != listenState {
		return Listener{}, false
	}
	return parseLocalEndpoint(fields[1])
}

// parseLocalEndpoint reads the local_address column, "HEXADDRESS:HEXPORT".
func parseLocalEndpoint(endpoint string) (Listener, bool) {
	hexAddress, hexPort, found := strings.Cut(endpoint, ":")
	if !found {
		return Listener{}, false
	}
	ip, ok := decodeAddress(hexAddress)
	if !ok {
		return Listener{}, false
	}
	port, err := strconv.ParseUint(hexPort, 16, 16)
	if err != nil {
		return Listener{}, false
	}
	return Listener{Address: ip.String(), Port: int(port), Loopback: ip.IsLoopback()}, true
}

// decodeAddress reads an IPv4 (8 hex digits) or IPv6 (32 hex digits) address.
// The kernel prints each 32-bit word in host (little-endian) byte order.
func decodeAddress(hexAddress string) (net.IP, bool) {
	raw, err := hex.DecodeString(hexAddress)
	if err != nil || (len(raw) != net.IPv4len && len(raw) != net.IPv6len) {
		return nil, false
	}
	return reverseWords(raw), true
}

func reverseWords(raw []byte) net.IP {
	ip := make(net.IP, len(raw))
	for word := 0; word < len(raw); word += 4 {
		for offset := 0; offset < 4; offset++ {
			ip[word+offset] = raw[word+3-offset]
		}
	}
	return ip
}
