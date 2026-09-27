# Measurements

## M1 — NAT lab: cone vs symmetric mapping

Each peer sends UDP from source port 51820 to two servers on the "internet"
(`203.0.113.1:9000` and `203.0.113.2:9000`). `tcpdump` on the internet bridge
shows the public `ip:port` each server sees. Reproduce with:

```sh
sudo make lab && sudo make prove             # nat-b cone
sudo make nat-symmetric && sudo make prove   # nat-b symmetric
```

nat-b in cone mode (`MASQUERADE`):

```text
  PEER    ROUTER MODE       SEEN BY 203.0.113.1      SEEN BY 203.0.113.2      MAPPING
  peer-1  nat-a  cone       203.0.113.10.51820       203.0.113.10.51820       endpoint-independent (cone)
  peer-2  nat-b  cone       203.0.113.20.51820       203.0.113.20.51820       endpoint-independent (cone)
```

nat-b in symmetric mode (`MASQUERADE --random-fully`):

```text
  PEER    ROUTER MODE       SEEN BY 203.0.113.1      SEEN BY 203.0.113.2      MAPPING
  peer-1  nat-a  cone       203.0.113.10.51820       203.0.113.10.51820       endpoint-independent (cone)
  peer-2  nat-b  symmetric  203.0.113.20.35334       203.0.113.20.55804       endpoint-dependent (symmetric)
```

Raw tcpdump for peer-2 in symmetric mode:

```text
23:17:05.905758 IP 203.0.113.20.35334 > 203.0.113.1.9000: UDP, length 18
23:17:05.909561 IP 203.0.113.20.55804 > 203.0.113.2.9000: UDP, length 18
```

**What this means for hole punching:** behind nat-b in symmetric mode, the
port a STUN server learns (`35334`) is not the port nat-b will use toward
peer-1 (some new random port). peer-1 punches toward the wrong port, and
nat-b's conntrack drops it. That's why a relay is unavoidable for this case.

Reachability in both modes: peers reach `203.0.113.1`; neither peer can reach
the other's private `192.168.x.2`, and neither can the internet.

> Captured in a privileged `ubuntu:24.04` Docker container on an arm64 Mac.
> Re-run in the Multipass VM and replace if the numbers differ.

## M3 — Hole punching: cone vs symmetric

`sudo make demo` runs STUN and the coordinator on the internet namespace and
one agent per peer namespace. Probes go every 200 ms for up to 5 s.

Cone (both NATs endpoint-independent), 6/6 runs direct:

```text
peer-1 msg="punch ok" remote=peer-2 path=direct candidate=srflx:203.0.113.20:51820 rtt=271µs took=202ms
peer-2 msg="punch ok" remote=peer-1 path=direct candidate=srflx:203.0.113.10:51820 rtt=89µs  took=0s
```

conntrack on nat-a after the punch: the mapping to peer-2 has been replied to
(no `[UNREPLIED]`), and the public port is still 51820:

```text
udp 17 29 src=192.168.1.2 dst=203.0.113.20 sport=51820 dport=51820 src=203.0.113.20 dst=203.0.113.10 sport=51820 dport=51820
```

Symmetric (`sudo make demo-symmetric`), 4/4 runs timed out. peer-1 aims at the
port nat-b showed the STUN server (`42857`), but nat-b uses a different random
port toward peer-1:

```text
peer-1 msg=punching remote=peer-2 candidates="[host:192.168.2.2:51820 srflx:203.0.113.20:42857]"
peer-1 msg="punch failed" remote=peer-2 path=none err="hole punch timed out" took=5.002s
```

### Finding: "cone" NAT on Linux isn't cone under collision

The first version of the lab did not drop unsolicited inbound traffic on the
routers, and cone mode failed most runs. peer-1's first probe reached nat-b
before peer-2 had sent anything; conntrack on nat-b recorded that inbound flow
`203.0.113.10:51820 -> 203.0.113.20:51820`. When peer-2 then sent outbound,
the tuple MASQUERADE wanted was taken, so it picked another port (`59986`):

```text
nat-b: src=192.168.2.2 dst=203.0.113.10 sport=51820 dport=51820 [UNREPLIED] src=203.0.113.10 dst=203.0.113.20 sport=51820 dport=59986
```

Fix (in `lab/up.sh`, and what real home routers do): `iptables -A INPUT -i wan
-m conntrack --ctstate NEW -j DROP`. The filter DROP happens before conntrack
confirms the entry, so the unsolicited probe leaves no state behind.

## M6 — Direct vs relay

| Metric | Direct | Relay |
| --- | --- | --- |
| Time from start to first ping reply | | |
| RTT (`ping -c 100`, avg / p99) | | |
| Throughput (`iperf3`, 10 s) | | |
