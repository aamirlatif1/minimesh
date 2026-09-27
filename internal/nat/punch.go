package nat

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"time"
)

// ErrPunchTimeout means no candidate answered in time: typically one side
// is behind a symmetric NAT, so a relay is needed.
var ErrPunchTimeout = errors.New("hole punch timed out")

type pong struct {
	txid uint64
	src  netip.AddrPort
}

// PunchResult says which remote candidate answered first.
type PunchResult struct {
	Remote Candidate      // the candidate our winning ping was sent to
	Src    netip.AddrPort // where the pong came from (equals Remote.Addr unless something odd rewrote it)
	RTT    time.Duration
}

// Punch sends a ping to every remote candidate each interval until one
// answers with a pong, or timeout passes.
//
// Both peers must run Punch at roughly the same time. Our first pings
// usually die at the remote NAT (it has no mapping for us yet), but they
// create the outbound mapping in our NAT. Once the remote side has done the
// same, pings get through both ways. That's the whole trick.
func (e *Endpoint) Punch(ctx context.Context, remote PeerID, cands []Candidate, interval, timeout time.Duration) (PunchResult, error) {
	if len(cands) == 0 {
		return PunchResult{}, errors.New("no remote candidates")
	}
	ch := make(chan pong, 8)
	e.mu.Lock()
	if _, busy := e.punches[remote]; busy {
		e.mu.Unlock()
		return PunchResult{}, fmt.Errorf("punch to %s already running", remote)
	}
	e.punches[remote] = ch
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.punches, remote)
		e.mu.Unlock()
	}()

	type sent struct {
		cand Candidate
		at   time.Time
	}
	inflight := make(map[uint64]sent)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		for _, c := range cands {
			txid := randomTxID()
			inflight[txid] = sent{cand: c, at: time.Now()}
			ping := probe{kind: probePing, from: e.self, txid: txid}
			if err := e.send(ping.marshal(), c.Addr); err != nil {
				// e.g. "network unreachable" for the other side's private host candidate.
				e.log.Debug("ping send", "dst", c, "err", err)
			}
		}
		select {
		case p := <-ch:
			s, ok := inflight[p.txid]
			if !ok {
				continue // pong for someone else's txid; ignore
			}
			return PunchResult{Remote: s.cand, Src: p.src, RTT: time.Since(s.at)}, nil
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return PunchResult{}, ErrPunchTimeout
			}
			return PunchResult{}, ctx.Err()
		case <-tick.C:
		}
	}
}

func randomTxID() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint64(b[:])
}
