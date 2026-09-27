#!/usr/bin/env bash
# Build the NAT lab: one "internet" namespace with a bridge, two NAT routers
# on it, and one peer behind each router. Idempotent: tears down first.
#
# Usage: sudo lab/up.sh [cone|symmetric]   (mode applies to nat-b; nat-a is always cone)
set -euo pipefail
dir=$(dirname "$0")
source "$dir/common.sh"
require_root
require_cmds ip iptables sysctl

mode=${1:-cone}

"$dir/down.sh" >/dev/null

for ns in "${ALL_NS[@]}"; do
	ip netns add "$ns"
	nsx "$ns" ip link set lo up
	# No IPv6: keeps tcpdump output free of router solicitations and ND noise.
	nsx "$ns" sysctl -qw net.ipv6.conf.all.disable_ipv6=1 net.ipv6.conf.default.disable_ipv6=1
done

# --- internet: a bridge that every router's WAN side plugs into.
nsx "$NS_INTERNET" ip link add br0 type bridge
nsx "$NS_INTERNET" ip addr add "$INTERNET_IP/$PUBLIC_PREFIX" dev br0
nsx "$NS_INTERNET" ip addr add "$INTERNET_IP2/$PUBLIC_PREFIX" dev br0
nsx "$NS_INTERNET" ip link set br0 up

# --- routers: WAN veth into the bridge, LAN veth to their peer.
for r in "${NS_ROUTERS[@]}"; do
	ip link add wan netns "$r" type veth peer name "$r" netns "$NS_INTERNET"
	nsx "$NS_INTERNET" ip link set "$r" master br0 up

	nsx "$r" ip addr add "${ROUTER_WAN_IP[$r]}/$PUBLIC_PREFIX" dev wan
	nsx "$r" ip link set wan up
	nsx "$r" ip route add default via "$INTERNET_IP"
	nsx "$r" sysctl -qw net.ipv4.ip_forward=1

	# Drop unsolicited inbound, like a home router. Beyond realism this is
	# required for punching: without it, the peer's first probe arriving
	# before our own outbound packet gets a conntrack entry (to the router
	# itself), which then clashes with the outbound mapping, and MASQUERADE
	# silently picks another port. A DROP in filter runs before conntrack
	# confirms the entry, so nothing is left behind.
	nsx "$r" iptables -A INPUT -i wan -m conntrack --ctstate NEW -j DROP
	nsx "$r" iptables -A FORWARD -i wan -m conntrack --ctstate NEW -j DROP
done

# --- peers: private address, default route via their router.
for p in "${NS_PEERS[@]}"; do
	r=${PEER_ROUTER[$p]}
	ip link add eth0 netns "$p" type veth peer name lan netns "$r"

	nsx "$r" ip addr add "${ROUTER_LAN_IP[$r]}/24" dev lan
	nsx "$r" ip link set lan up

	nsx "$p" ip addr add "${PEER_IP[$p]}/24" dev eth0
	nsx "$p" ip link set eth0 up
	nsx "$p" ip route add default via "${ROUTER_LAN_IP[$r]}"
done

"$dir/nat-mode.sh" cone nat-a >/dev/null
"$dir/nat-mode.sh" "$mode" nat-b >/dev/null

echo "lab up: nat-a=cone nat-b=$mode"
echo "  peer-1 ${PEER_IP[peer-1]} -> nat-a ${ROUTER_WAN_IP[nat-a]}"
echo "  peer-2 ${PEER_IP[peer-2]} -> nat-b ${ROUTER_WAN_IP[nat-b]}"
echo "  internet $INTERNET_IP, $INTERNET_IP2"
