package nat

import (
	"context"
	"net"
	"net/netip"
)

// Candidate types, named as in ICE (RFC 8445 5.1.1).
const (
	TypeHost  = "host"  // an address on a local interface
	TypeSrflx = "srflx" // server-reflexive: our address as seen by STUN, i.e. the NAT's public side
)

// Candidate is one address a peer might be reachable at.
type Candidate struct {
	Type string
	Addr netip.AddrPort
}

func (c Candidate) String() string {
	return c.Type + ":" + c.Addr.String()
}

// Gather returns host candidates for every non-loopback IPv4 interface
// address, then the server-reflexive candidate from stunServer. A STUN
// failure is returned with the host candidates, so the caller can still
// try LAN-only connectivity.
func (e *Endpoint) Gather(ctx context.Context, stunServer string) ([]Candidate, error) {
	var cands []Candidate
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		pfx, err := netip.ParsePrefix(a.String())
		if err != nil {
			continue
		}
		ip := pfx.Addr()
		if !ip.Is4() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		cands = append(cands, Candidate{Type: TypeHost, Addr: netip.AddrPortFrom(ip, e.Port())})
	}

	srflx, err := e.STUN(ctx, stunServer)
	if err != nil {
		return cands, err
	}
	// Behind no NAT the reflexive address equals a host one; skip the duplicate.
	for _, c := range cands {
		if c.Addr == srflx {
			return cands, nil
		}
	}
	return append(cands, Candidate{Type: TypeSrflx, Addr: srflx}), nil
}
