package server

import (
	"strings"
	"testing"
)

// Output taken from a real server: a Caddy holds 80 and 443 through docker,
// sshd and systemd-resolved listen on their own ports.
const realDockerPS = "k0fliqj33fe1k3-n8n-1\tdocker.n8n.io/n8nio/n8n:latest\t5678/tcp\n" +
	"docker-caddy-1\tcaddy:2-alpine\t0.0.0.0:80->80/tcp, [::]:80->80/tcp, 0.0.0.0:443->443/tcp, [::]:443->443/tcp, 443/udp, 2019/tcp\n" +
	"pier-proxy\ttraefik:v3.6\t0.0.0.0:8880->80/tcp, [::]:8880->80/tcp, 0.0.0.0:8843->443/tcp\n"

const realSockets = `LISTEN 0 4096 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=900,fd=3))
LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:(("docker-proxy",pid=1500,fd=7))
LISTEN 0 4096 0.0.0.0:80 0.0.0.0:* users:(("docker-proxy",pid=1501,fd=7))
LISTEN 0 4096 0.0.0.0:8880 0.0.0.0:* users:(("docker-proxy",pid=1600,fd=7))
LISTEN 0 4096 127.0.0.1:9000 0.0.0.0:* users:(("php-fpm",pid=700,fd=8))
LISTEN 0 4096 [::]:8443 [::]:*`

func TestPortHoldersNameWhoHoldsThePort(t *testing.T) {
	holders := portHolders(realDockerPS, realSockets, []int{80, 443})
	if !strings.Contains(holders[80].String(), "docker-caddy-1 (caddy:2-alpine)") || !strings.Contains(holders[443].String(), "docker-caddy-1") {
		t.Errorf("the container holding 80/443 must be named: %v", holders)
	}
	// The proxy's own ports are not a conflict: restarting it must work.
	if holders := portHolders(realDockerPS, realSockets, []int{8880, 8843}); len(holders) != 0 {
		t.Errorf("the proxy does not conflict with itself: %v", holders)
	}
	// A program outside docker is found through ss, with or without its name.
	if holders := portHolders(realDockerPS, realSockets, []int{9000, 8443}); holders[9000].String() != "o processo php-fpm" || holders[8443].String() != "outro programa" {
		t.Errorf("non-docker holders: %v", holders)
	}
	// Free ports, and a machine without ss.
	if holders := portHolders(realDockerPS, "", []int{8080, 8444}); len(holders) != 0 {
		t.Errorf("free ports reported as taken: %v", holders)
	}
}
