#!/bin/sh
# Debian userland against the Ubuntu host daemon, not a Debian engine.
set -eu
apt-get update -qq
apt-get install -y -qq ca-certificates curl git sqlite3 tar
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
echo "deb [signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian bookworm stable" > /etc/apt/sources.list.d/docker.list
apt-get update -qq
apt-get install -y -qq docker-ce-cli docker-compose-plugin docker-buildx-plugin
test "$(readlink -f /bin/sh)" = /usr/bin/dash
# shellcheck disable=SC1091
. /etc/os-release
test "$VERSION_ID" = 12
test "$(dpkg --print-architecture)" = amd64
sh -n install.sh
sh tools/installtest/clean-install.sh
sh tools/installtest/upgrade.sh
