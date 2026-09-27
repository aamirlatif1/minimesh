#!/usr/bin/env bash
# Run coordinator + STUN on the "internet" and one agent per peer namespace,
# wait for the punch result, then show the logs and the NAT mappings.
#
# Usage: sudo lab/demo.sh [cone|symmetric]   (needs binaries in bin/, see make build)
# Exit status: 0 if the outcome matches the mode (cone: direct, symmetric: punch fails).
set -euo pipefail
dir=$(dirname "$0")
source "$dir/common.sh"
require_root
require_cmds ip conntrack

mode=${1:-cone}
bin=$dir/../bin
for b in coordinator stun peer; do
	[[ -x $bin/$b ]] || { echo "missing $bin/$b; run make build" >&2; exit 1; }
done

"$dir/up.sh" "$mode"

out=$dir/out/demo-$mode
rm -rf "$out"
mkdir -p "$out/state"

trap '"$dir/down.sh" >/dev/null' EXIT

nsx "$NS_INTERNET" "$bin/stun" -listen "$INTERNET_IP:3478" >"$out/stun.log" 2>&1 &
nsx "$NS_INTERNET" "$bin/coordinator" -listen "$INTERNET_IP:7000" >"$out/coordinator.log" 2>&1 &
sleep 0.5

for p in "${NS_PEERS[@]}"; do
	nsx "$p" "$bin/peer" -name "$p" -key "$out/state/$p.key" \
		-coordinator "$INTERNET_IP:7000" -stun "$INTERNET_IP:3478" >"$out/$p.log" 2>&1 &
done

# Wait for both peers to report a result (5 s punch timeout + setup).
for _ in $(seq 100); do
	done_count=$( (grep -l 'msg="punch \(ok\|failed\)"' "$out"/peer-*.log 2>/dev/null || true) | wc -l)
	((done_count == ${#NS_PEERS[@]})) && break
	sleep 0.1
done

for f in coordinator stun peer-1 peer-2; do
	echo "== $f"
	cat "$out/$f.log"
done

echo
for r in "${NS_ROUTERS[@]}"; do
	echo "== conntrack in $r (UDP mappings for port 51820)"
	nsx "$r" conntrack -L -p udp 2>/dev/null | grep 51820 || echo "(none)"
done

echo
result=0
for p in "${NS_PEERS[@]}"; do
	if [[ $mode == cone ]]; then
		grep -q 'msg="punch ok".*path=direct' "$out/$p.log" || { echo "FAIL: $p did not punch"; result=1; }
	else
		grep -q 'msg="punch failed"' "$out/$p.log" || { echo "FAIL: $p punched through symmetric NAT"; result=1; }
	fi
done
((result == 0)) && echo "PASS: $mode mode behaved as expected"
exit $result
