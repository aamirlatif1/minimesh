// Package nat gathers connection candidates and punches holes through NATs.
//
// Everything runs on one UDP socket, bound to the port WireGuard will use
// later. That matters: the NAT mapping learnt via STUN and opened by the
// punch belongs to this source port, so WireGuard must send from it too.
package nat

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"

	"github.com/pion/stun/v3"
)

// Endpoint owns the UDP socket and demultiplexes what arrives on it:
// STUN responses go to the waiting STUN request, probes are answered or
// handed to the running punch for that peer.
type Endpoint struct {
	conn *net.UDPConn
	self PeerID
	log  *slog.Logger

	mu      sync.Mutex
	stunTx  map[[stun.TransactionIDSize]byte]chan netip.AddrPort
	punches map[PeerID]chan pong

	done chan struct{}
}

// PeerID is a raw 32-byte WireGuard public key.
type PeerID [32]byte

// Listen binds 0.0.0.0:port (IPv4 only, like the lab) and starts reading.
func Listen(port int, self PeerID, log *slog.Logger) (*Endpoint, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		return nil, fmt.Errorf("listen udp :%d: %w", port, err)
	}
	e := &Endpoint{
		conn:    conn,
		self:    self,
		log:     log,
		stunTx:  make(map[[stun.TransactionIDSize]byte]chan netip.AddrPort),
		punches: make(map[PeerID]chan pong),
		done:    make(chan struct{}),
	}
	go e.readLoop()
	return e, nil
}

// Port is the local UDP port.
func (e *Endpoint) Port() uint16 {
	return uint16(e.conn.LocalAddr().(*net.UDPAddr).Port)
}

// Close releases the socket so WireGuard can bind the same port. The NAT
// mapping lives in the router's conntrack table, not in the socket, so it
// survives until its timeout as long as someone keeps sending from the port.
func (e *Endpoint) Close() error {
	err := e.conn.Close()
	<-e.done
	return err
}

func (e *Endpoint) readLoop() {
	defer close(e.done)
	buf := make([]byte, 1500)
	for {
		n, src, err := e.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				e.log.Error("udp read", "err", err)
			}
			return
		}
		src = netip.AddrPortFrom(src.Addr().Unmap(), src.Port())
		b := buf[:n]

		switch {
		case stun.IsMessage(b):
			e.handleSTUN(b)
		case isProbe(b):
			e.handleProbe(b, src)
		default:
			e.log.Debug("ignoring unknown packet", "src", src, "bytes", n)
		}
	}
}

func (e *Endpoint) handleSTUN(b []byte) {
	m := &stun.Message{Raw: append([]byte(nil), b...)}
	if err := m.Decode(); err != nil || m.Type != stun.BindingSuccess {
		return
	}
	var xor stun.XORMappedAddress
	if err := xor.GetFrom(m); err != nil {
		return
	}
	ip, ok := netip.AddrFromSlice(xor.IP)
	if !ok {
		return
	}
	e.mu.Lock()
	ch := e.stunTx[m.TransactionID]
	e.mu.Unlock()
	if ch != nil {
		select {
		case ch <- netip.AddrPortFrom(ip.Unmap(), uint16(xor.Port)):
		default:
		}
	}
}

func (e *Endpoint) handleProbe(b []byte, src netip.AddrPort) {
	p, err := parseProbe(b)
	if err != nil {
		return
	}
	switch p.kind {
	case probePing:
		// Always answer, even if we are not punching toward this peer yet:
		// the pong both proves reachability and keeps our NAT mapping open.
		e.log.Debug("ping received", "from", p.from, "src", src)
		reply := probe{kind: probePong, from: e.self, txid: p.txid}
		if _, err := e.conn.WriteToUDPAddrPort(reply.marshal(), src); err != nil {
			e.log.Debug("pong send", "dst", src, "err", err)
		}
	case probePong:
		e.mu.Lock()
		ch := e.punches[p.from]
		e.mu.Unlock()
		if ch != nil {
			select {
			case ch <- pong{txid: p.txid, src: src}:
			default:
			}
		}
	}
}

func (e *Endpoint) send(b []byte, dst netip.AddrPort) error {
	_, err := e.conn.WriteToUDPAddrPort(b, dst)
	return err
}

func (id PeerID) String() string {
	return fmt.Sprintf("%x", id[:4])
}
