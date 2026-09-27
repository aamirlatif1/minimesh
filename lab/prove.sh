#!/usr/bin/env bash
# Prove the lab behaves like the NATs it claims to be:
#   1. peers reach the internet but not each other's private address;
#   2. each peer sends UDP from one fixed source port to two different servers,
#      and tcpdump on the internet bridge shows which public port each server saw.
#      Same port for both servers = endpoint-independent mapping (cone).
#      Different ports              = endpoint-dependent mapping (symmetric).
#
# Usage: sudo lab/prove.sh   (raw tcpdump output is saved to lab/out/)
set -euo pipefail
dir=$(dirname "$0")
source "$dir/common.sh"
require_root
require_cmds ip tcpdump nc ping

SRC_PORT=51820 # the port WireGuard will use later
DST_PORT=9000
out="$dir/out"
mkdir -p "$out"

fail=0
check() { # check <description> <expect: ok|fail> <cmd...>
	local desc=$1 expect=$2
	shift 2
	if "$@" >/dev/null 2>&1; then got=ok; else got=fail; fi
	if [[ $got == "$expect" ]]; then
		printf '  PASS  %s\n' "$desc"
	else
		printf '  FAIL  %s (expected %s, got %s)\n' "$desc" "$expect" "$got"
		fail=1
	fi
}

echo "== reachability"
check "peer-1 -> internet $INTERNET_IP" ok nsx peer-1 ping -c1 -W1 "$INTERNET_IP"
check "peer-2 -> internet $INTERNET_IP" ok nsx peer-2 ping -c1 -W1 "$INTERNET_IP"
check "peer-1 cannot reach peer-2 private ${PEER_IP[peer-2]}" fail nsx peer-1 ping -c1 -W1 "${PEER_IP[peer-2]}"
check "peer-2 cannot reach peer-1 private ${PEER_IP[peer-1]}" fail nsx peer-2 ping -c1 -W1 "${PEER_IP[peer-1]}"
check "internet cannot reach peer-1 private ${PEER_IP[peer-1]}" fail nsx "$NS_INTERNET" ping -c1 -W1 "${PEER_IP[peer-1]}"

echo
echo "== NAT mapping (src port $SRC_PORT -> $INTERNET_IP:$DST_PORT and $INTERNET_IP2:$DST_PORT)"
printf '  %-7s %-6s %-10s %-24s %-24s %s\n' PEER ROUTER MODE "SEEN BY $INTERNET_IP" "SEEN BY $INTERNET_IP2" MAPPING

for p in "${NS_PEERS[@]}"; do
	r=${PEER_ROUTER[$p]}
	mode=cone
	nsx "$r" iptables -t nat -S POSTROUTING | grep -q -- --random-fully && mode=symmetric
	nsx "$r" conntrack -F 2>/dev/null || true

	pcap="$out/nat-$p-$mode.txt"
	nsx "$NS_INTERNET" tcpdump -i br0 -n -l -c 2 "udp and dst port $DST_PORT" >"$pcap" 2>/dev/null &
	tcpdump_pid=$!
	sleep 1 # let tcpdump attach before sending

	for dst in "$INTERNET_IP" "$INTERNET_IP2"; do
		echo "probe from $p" | nsx "$p" nc -u -w1 -p "$SRC_PORT" "$dst" "$DST_PORT" || true
	done

	for _ in $(seq 20); do kill -0 "$tcpdump_pid" 2>/dev/null || break; sleep 0.1; done
	kill "$tcpdump_pid" 2>/dev/null || true
	wait "$tcpdump_pid" 2>/dev/null || true

	# Line looks like: 12:00:00.000 IP 203.0.113.10.51820 > 203.0.113.1.9000: UDP, length 16
	seen1=$(awk -v d="$INTERNET_IP.$DST_PORT:" '$5 == d {print $3}' "$pcap")
	seen2=$(awk -v d="$INTERNET_IP2.$DST_PORT:" '$5 == d {print $3}' "$pcap")
	port1=${seen1##*.}
	port2=${seen2##*.}

	if [[ -z $port1 || -z $port2 ]]; then
		verdict="no capture (see $pcap)"
		fail=1
	elif [[ $port1 == "$port2" ]]; then
		verdict="endpoint-independent (cone)"
	else
		verdict="endpoint-dependent (symmetric)"
	fi
	[[ $verdict == *"($mode)" ]] || fail=1

	printf '  %-7s %-6s %-10s %-24s %-24s %s\n' "$p" "$r" "$mode" "${seen1:--}" "${seen2:--}" "$verdict"
done

echo
echo "raw tcpdump output: $out/"
exit $fail
