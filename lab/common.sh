# Shared names and addresses for the NAT lab. Sourced by the other lab scripts.
#
#   peer-1 (192.168.1.2) --lan/eth0-- nat-a (192.168.1.1 | 203.0.113.10) --+
#                                                                         br0 in "internet" (203.0.113.1, .2)
#   peer-2 (192.168.2.2) --lan/eth0-- nat-b (192.168.2.1 | 203.0.113.20) --+

NS_INTERNET=internet
NS_ROUTERS=(nat-a nat-b)
NS_PEERS=(peer-1 peer-2)
ALL_NS=("$NS_INTERNET" "${NS_ROUTERS[@]}" "${NS_PEERS[@]}")

PUBLIC_PREFIX=24
INTERNET_IP=203.0.113.1  # coordinator, stun, relay live here
INTERNET_IP2=203.0.113.2 # second "server", used to show per-destination mapping

declare -A ROUTER_WAN_IP=([nat-a]=203.0.113.10 [nat-b]=203.0.113.20)
declare -A ROUTER_LAN_IP=([nat-a]=192.168.1.1 [nat-b]=192.168.2.1)
declare -A PEER_IP=([peer-1]=192.168.1.2 [peer-2]=192.168.2.2)
declare -A PEER_ROUTER=([peer-1]=nat-a [peer-2]=nat-b)

require_root() {
	if [[ $EUID -ne 0 ]]; then
		echo "$(basename "$0"): must run as root (try: sudo make lab)" >&2
		exit 1
	fi
}

require_cmds() {
	local missing=()
	for c in "$@"; do
		command -v "$c" >/dev/null || missing+=("$c")
	done
	if ((${#missing[@]})); then
		echo "missing commands: ${missing[*]}" >&2
		echo "install with: apt install -y iproute2 iptables conntrack tcpdump netcat-openbsd" >&2
		exit 1
	fi
}

# nsx <ns> <cmd...> runs a command inside a namespace.
nsx() {
	local ns=$1
	shift
	ip netns exec "$ns" "$@"
}
