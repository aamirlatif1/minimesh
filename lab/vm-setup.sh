#!/usr/bin/env bash
# Install lab dependencies inside the Ubuntu 24.04 VM. Run once as root.
set -euo pipefail

apt-get update
apt-get install -y wireguard-tools iproute2 iptables conntrack tcpdump \
	netcat-openbsd iperf3 make docker.io docker-compose-v2
snap list go >/dev/null 2>&1 || snap install go --classic
usermod -aG docker "${SUDO_USER:-ubuntu}"

modprobe wireguard
lsmod | grep -q '^wireguard' && echo "wireguard kernel module: ok"
echo "done; log out and back in for the docker group to apply"
