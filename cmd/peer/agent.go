package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"github.com/aamirlatif1/minimesh/internal/coord"
	"github.com/aamirlatif1/minimesh/internal/nat"
	pb "github.com/aamirlatif1/minimesh/proto"
)

// candidateWait is how long to wait for a peer's candidates before asking again.
const candidateWait = 2 * time.Second

type agent struct {
	cfg    config
	log    *slog.Logger
	pub    wgtypes.Key
	client pb.CoordinatorClient
	ep     *nat.Endpoint

	cands []nat.Candidate // ours, gathered once at start

	sendMu sync.Mutex // grpc streams allow one concurrent Send
	signal pb.Coordinator_SignalClient

	mu      sync.Mutex
	remotes map[string]*remote         // online peers, by base64 public key
	early   map[string][]nat.Candidate // candidates that arrived before the peer list did
}

// remote is our view of one other peer.
type remote struct {
	info   *pb.Peer
	id     nat.PeerID
	cands  chan []nat.Candidate // capacity 1: latest candidates win
	cancel context.CancelFunc
}

func newAgent(cfg config, key wgtypes.Key, conn *grpc.ClientConn, log *slog.Logger) (*agent, error) {
	pub := key.PublicKey()
	ep, err := nat.Listen(cfg.port, nat.PeerID(pub), log)
	if err != nil {
		return nil, err
	}
	return &agent{
		cfg:     cfg,
		log:     log,
		pub:     pub,
		client:  pb.NewCoordinatorClient(conn),
		ep:      ep,
		remotes: make(map[string]*remote),
		early:   make(map[string][]nat.Candidate),
	}, nil
}

func (a *agent) close() {
	a.mu.Lock()
	for _, r := range a.remotes {
		r.cancel()
	}
	a.mu.Unlock()
	_ = a.ep.Close()
}

func (a *agent) run(ctx context.Context) error {
	resp, err := a.client.Register(ctx, &pb.RegisterRequest{PublicKey: a.pub.String(), Name: a.cfg.name})
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	a.log.Info("registered", "key", a.pub.String()[:8], "overlay_ip", resp.OverlayIp)

	gctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	a.cands, err = a.ep.Gather(gctx, a.cfg.stunServer)
	cancel()
	if err != nil {
		a.log.Warn("stun failed, only host candidates", "err", err)
	}
	a.log.Info("candidates gathered", "candidates", fmt.Sprint(a.cands))

	// Open Signal before Sync: by the time anyone sees us in a peer list,
	// our mailbox should exist, so their first envelope isn't dropped.
	sctx := metadata.AppendToOutgoingContext(ctx, coord.PublicKeyHeader, a.pub.String())
	a.signal, err = a.client.Signal(sctx)
	if err != nil {
		return fmt.Errorf("signal: %w", err)
	}
	signalErr := make(chan error, 1)
	go func() { signalErr <- a.recvSignal(ctx) }()

	syncStream, err := a.client.Sync(ctx, &pb.SyncRequest{PublicKey: a.pub.String()})
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	syncErr := make(chan error, 1)
	go func() {
		for {
			list, err := syncStream.Recv()
			if err != nil {
				syncErr <- fmt.Errorf("sync: %w", err)
				return
			}
			a.onPeerList(ctx, list)
		}
	}()

	select {
	case err := <-signalErr:
		return err
	case err := <-syncErr:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// onPeerList starts a connect loop for new peers and stops it for gone ones.
func (a *agent) onPeerList(ctx context.Context, list *pb.PeerList) {
	a.mu.Lock()
	defer a.mu.Unlock()

	seen := make(map[string]bool)
	for _, p := range list.Peers {
		seen[p.PublicKey] = true
		if _, ok := a.remotes[p.PublicKey]; ok {
			continue
		}
		k, err := wgtypes.ParseKey(p.PublicKey)
		if err != nil {
			a.log.Warn("bad peer key", "peer", p.Name, "err", err)
			continue
		}
		rctx, cancel := context.WithCancel(ctx)
		r := &remote{info: p, id: nat.PeerID(k), cands: make(chan []nat.Candidate, 1), cancel: cancel}
		if c, ok := a.early[p.PublicKey]; ok {
			r.cands <- c
			delete(a.early, p.PublicKey)
		}
		a.remotes[p.PublicKey] = r
		a.log.Info("peer joined", "remote", p.Name, "overlay_ip", p.OverlayIp)
		go a.connect(rctx, r)
	}
	for k, r := range a.remotes {
		if !seen[k] {
			r.cancel()
			delete(a.remotes, k)
			a.log.Info("peer left", "remote", r.info.Name)
		}
	}
}

// connect exchanges candidates with r and punches.
func (a *agent) connect(ctx context.Context, r *remote) {
	var theirs []nat.Candidate
	for theirs == nil {
		if err := a.sendCandidates(r.info.PublicKey, true); err != nil {
			a.log.Warn("send candidates", "remote", r.info.Name, "err", err)
		}
		select {
		case theirs = <-r.cands:
		case <-time.After(candidateWait):
		case <-ctx.Done():
			return
		}
	}
	a.log.Info("punching", "remote", r.info.Name, "candidates", fmt.Sprint(theirs))

	start := time.Now()
	res, err := a.ep.Punch(ctx, r.id, theirs, a.cfg.punchEvery, a.cfg.punchTimeout)
	switch {
	case err == nil:
		a.log.Info("punch ok", "remote", r.info.Name, "path", "direct",
			"candidate", res.Remote.String(), "rtt", res.RTT, "took", time.Since(start).Round(time.Millisecond))
	case errors.Is(err, nat.ErrPunchTimeout):
		// M5 falls back to the relay here.
		a.log.Warn("punch failed", "remote", r.info.Name, "path", "none",
			"err", err, "took", time.Since(start).Round(time.Millisecond))
	case ctx.Err() != nil:
	default:
		a.log.Error("punch", "remote", r.info.Name, "err", err)
	}
}

func (a *agent) sendCandidates(to string, replyRequested bool) error {
	msg := &pb.Candidates{ReplyRequested: replyRequested}
	for _, c := range a.cands {
		msg.Candidates = append(msg.Candidates, &pb.Candidate{Type: c.Type, Addr: c.Addr.String()})
	}
	payload, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	return a.signal.Send(&pb.Envelope{To: to, Payload: payload})
}

func (a *agent) recvSignal(ctx context.Context) error {
	for {
		env, err := a.signal.Recv()
		if err != nil {
			return fmt.Errorf("signal: %w", err)
		}
		var msg pb.Candidates
		if err := proto.Unmarshal(env.Payload, &msg); err != nil {
			a.log.Warn("bad signal payload", "from", env.From[:8], "err", err)
			continue
		}
		var cands []nat.Candidate
		for _, c := range msg.Candidates {
			addr, err := netip.ParseAddrPort(c.Addr)
			if err != nil {
				continue
			}
			cands = append(cands, nat.Candidate{Type: c.Type, Addr: addr})
		}
		if len(cands) == 0 {
			continue
		}

		a.mu.Lock()
		if r, ok := a.remotes[env.From]; ok {
			select { // replace whatever is unread with the latest
			case <-r.cands:
			default:
			}
			r.cands <- cands
		} else {
			a.early[env.From] = cands
		}
		a.mu.Unlock()

		if msg.ReplyRequested {
			if err := a.sendCandidates(env.From, false); err != nil {
				a.log.Warn("reply candidates", "err", err)
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}
