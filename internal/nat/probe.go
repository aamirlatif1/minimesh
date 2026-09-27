package nat

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Probe wire format (45 bytes):
//
//	magic "mmp1" | kind (1 = ping, 2 = pong) | sender public key (32) | txid (8)
//
// The pong echoes the ping's txid, so the sender knows which candidate
// answered. Probes are unauthenticated; a real implementation (ICE) signs
// them with credentials exchanged over signalling.
const (
	probeLen  = 4 + 1 + 32 + 8
	probePing = 1
	probePong = 2
)

var probeMagic = []byte("mmp1")

type probe struct {
	kind byte
	from PeerID
	txid uint64
}

func (p probe) marshal() []byte {
	b := make([]byte, 0, probeLen)
	b = append(b, probeMagic...)
	b = append(b, p.kind)
	b = append(b, p.from[:]...)
	return binary.BigEndian.AppendUint64(b, p.txid)
}

func isProbe(b []byte) bool {
	return len(b) == probeLen && bytes.HasPrefix(b, probeMagic)
}

func parseProbe(b []byte) (probe, error) {
	if !isProbe(b) {
		return probe{}, errors.New("not a probe")
	}
	p := probe{kind: b[4]}
	if p.kind != probePing && p.kind != probePong {
		return probe{}, errors.New("unknown probe kind")
	}
	copy(p.from[:], b[5:37])
	p.txid = binary.BigEndian.Uint64(b[37:])
	return p, nil
}
