#!/usr/bin/env bash
# Switch a router between cone and symmetric NAT.
#
#   cone:      MASQUERADE keeps the source port when it is free, so every
#              destination sees the same public ip:port (endpoint-independent
#              mapping, RFC 4787 4.1). Hole punching works.
#   symmetric: MASQUERADE --random-fully picks a random port per connection
#              (5-tuple), so each destination sees a different public port
#              (endpoint-dependent mapping). The port STUN reports is useless
#              to the other peer, so hole punching fails.
#
# Filtering is conntrack in both modes: inbound is only allowed from an
# ip:port the inside host already sent to (address-and-port-dependent filtering).
#
# Usage: sudo lab/nat-mode.sh <cone|symmetric> [router]   (router defaults to nat-b)
set -euo pipefail
source "$(dirname "$0")/common.sh"
require_root

mode=${1:-}
router=${2:-nat-b}

case $mode in
cone) extra=() ;;
symmetric) extra=(--random-fully) ;;
*)
	echo "usage: $0 <cone|symmetric> [nat-a|nat-b]" >&2
	exit 2
	;;
esac

if ! ip netns list | grep -qw "^$router"; then
	echo "netns $router does not exist; run make lab first" >&2
	exit 1
fi

nsx "$router" iptables -t nat -F POSTROUTING
nsx "$router" iptables -t nat -A POSTROUTING -o wan -j MASQUERADE "${extra[@]}"

# Old mappings would otherwise survive the switch.
if command -v conntrack >/dev/null; then
	nsx "$router" conntrack -F 2>/dev/null || true
fi

echo "$router: $mode"
nsx "$router" iptables -t nat -S POSTROUTING | grep MASQUERADE
