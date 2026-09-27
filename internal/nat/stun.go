package nat

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/pion/stun/v3"
)

// STUN asks server (host:port) what public address our socket appears as:
// the server-reflexive candidate. Retries every 500 ms until ctx ends.
func (e *Endpoint) STUN(ctx context.Context, server string) (netip.AddrPort, error) {
	raddr, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return netip.AddrPort{}, err
	}
	dst := raddr.AddrPort()

	req, err := stun.Build(stun.TransactionID, stun.BindingRequest, stun.Fingerprint)
	if err != nil {
		return netip.AddrPort{}, err
	}
	ch := make(chan netip.AddrPort, 1)
	e.mu.Lock()
	e.stunTx[req.TransactionID] = ch
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.stunTx, req.TransactionID)
		e.mu.Unlock()
	}()

	retry := time.NewTicker(500 * time.Millisecond)
	defer retry.Stop()
	for {
		if err := e.send(req.Raw, dst); err != nil {
			return netip.AddrPort{}, fmt.Errorf("stun send: %w", err)
		}
		select {
		case addr := <-ch:
			return addr, nil
		case <-ctx.Done():
			return netip.AddrPort{}, fmt.Errorf("stun %s: %w", server, ctx.Err())
		case <-retry.C:
		}
	}
}
