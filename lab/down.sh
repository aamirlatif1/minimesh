#!/usr/bin/env bash
# Remove every lab namespace. Deleting a namespace also deletes its veths,
# bridge, iptables rules and conntrack entries, so this is all the cleanup needed.
set -euo pipefail
source "$(dirname "$0")/common.sh"
require_root

for ns in "${ALL_NS[@]}"; do
	if ip netns list | grep -qw "^$ns"; then
		# Kill anything still running inside (peers, coordinator, tcpdump...).
		pids=$(ip netns pids "$ns" || true)
		[[ -n $pids ]] && kill $pids 2>/dev/null || true
		ip netns del "$ns"
		echo "deleted netns $ns"
	fi
done
