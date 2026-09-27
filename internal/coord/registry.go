// Package coord is the coordinator: an in-memory peer registry that pushes
// peer lists to connected peers (NetBird's Management role) and forwards
// opaque envelopes between them (NetBird's Signal role).
package coord

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"sync"

	pb "github.com/aamirlatif1/minimesh/proto"
)

var (
	ErrUnknownPeer = errors.New("unknown peer")
	ErrPoolFull    = errors.New("overlay address pool exhausted")
)

type peer struct {
	key    string
	name   string
	ip     netip.Addr
	online int // number of open Sync streams; >0 means listed
}

// Registry holds registered peers and the channels used to notify Sync
// streams and deliver Signal envelopes. Safe for concurrent use.
type Registry struct {
	mu     sync.Mutex
	prefix netip.Prefix
	nextIP netip.Addr
	peers  map[string]*peer

	// syncSubs are woken (non-blocking, coalescing) when the peer list changes.
	syncSubs map[chan struct{}]string
	// mailboxes deliver envelopes to the Signal stream of a public key.
	mailboxes map[string]chan *pb.Envelope
}

// NewRegistry assigns overlay IPs from prefix, starting at its first host address.
func NewRegistry(prefix netip.Prefix) *Registry {
	return &Registry{
		prefix:    prefix.Masked(),
		nextIP:    prefix.Masked().Addr().Next(),
		peers:     make(map[string]*peer),
		syncSubs:  make(map[chan struct{}]string),
		mailboxes: make(map[string]chan *pb.Envelope),
	}
}

// Register adds a peer or returns its existing overlay IP.
func (r *Registry) Register(key, name string) (netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, ok := r.peers[key]; ok {
		p.name = name
		return p.ip, nil
	}
	ip := r.nextIP
	if !r.prefix.Contains(ip) || !r.prefix.Contains(ip.Next()) { // keep the broadcast address free
		return netip.Addr{}, ErrPoolFull
	}
	r.nextIP = ip.Next()
	r.peers[key] = &peer{key: key, name: name, ip: ip}
	return ip, nil
}

// Subscribe marks the peer online and returns a channel that receives a
// signal whenever the peer list changes (including once immediately).
// Call the returned func when the stream ends.
func (r *Registry) Subscribe(key string) (<-chan struct{}, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.peers[key]
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s", ErrUnknownPeer, key)
	}
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	r.syncSubs[ch] = key
	p.online++
	r.notifyLocked()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			delete(r.syncSubs, ch)
			p.online--
			r.notifyLocked()
		})
	}
	return ch, unsubscribe, nil
}

// notifyLocked wakes every Sync stream. The channel has capacity 1, so a
// slow stream coalesces many changes into one send of the latest list.
func (r *Registry) notifyLocked() {
	for ch := range r.syncSubs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// PeerList returns the online peers other than key, sorted by overlay IP.
func (r *Registry) PeerList(key string) *pb.PeerList {
	r.mu.Lock()
	defer r.mu.Unlock()

	list := &pb.PeerList{}
	for _, p := range r.peers {
		if p.key == key || p.online == 0 {
			continue
		}
		list.Peers = append(list.Peers, &pb.Peer{PublicKey: p.key, Name: p.name, OverlayIp: p.ip.String()})
	}
	sort.Slice(list.Peers, func(i, j int) bool {
		a, _ := netip.ParseAddr(list.Peers[i].OverlayIp)
		b, _ := netip.ParseAddr(list.Peers[j].OverlayIp)
		return a.Less(b)
	})
	return list
}

// OpenMailbox registers the Signal stream for key. A newer stream replaces
// an older one. Call the returned func when the stream ends.
func (r *Registry) OpenMailbox(key string) (<-chan *pb.Envelope, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.peers[key]; !ok {
		return nil, nil, fmt.Errorf("%w: %s", ErrUnknownPeer, key)
	}
	ch := make(chan *pb.Envelope, 32)
	r.mailboxes[key] = ch
	closeMailbox := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.mailboxes[key] == ch {
			delete(r.mailboxes, key)
		}
	}
	return ch, closeMailbox, nil
}

// Deliver puts env in the recipient's mailbox. It never blocks: if the
// recipient is offline or not draining, the envelope is dropped and an
// error returned. Peers are expected to retry.
func (r *Registry) Deliver(env *pb.Envelope) error {
	r.mu.Lock()
	ch, ok := r.mailboxes[env.To]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: no signal stream for %s", ErrUnknownPeer, env.To)
	}
	select {
	case ch <- env:
		return nil
	default:
		return fmt.Errorf("mailbox full for %s", env.To)
	}
}
