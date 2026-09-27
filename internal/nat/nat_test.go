package nat

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func listen(t *testing.T, id byte) *Endpoint {
	t.Helper()
	e, err := Listen(0, PeerID{id}, discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func loopback(e *Endpoint) Candidate {
	return Candidate{Type: TypeHost, Addr: netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), e.Port())}
}

func TestProbeRoundTrip(t *testing.T) {
	p := probe{kind: probePong, from: PeerID{1, 2, 3}, txid: 42}
	got, err := parseProbe(p.marshal())
	if err != nil {
		t.Fatal(err)
	}
	if got != p {
		t.Fatalf("got %+v, want %+v", got, p)
	}
	if isProbe([]byte("mmp1 short")) {
		t.Fatal("short packet accepted as probe")
	}
}

func TestPunchLoopback(t *testing.T) {
	a, b := listen(t, 'a'), listen(t, 'b')
	ctx := context.Background()

	type result struct {
		res PunchResult
		err error
	}
	fromB := make(chan result, 1)
	go func() {
		res, err := b.Punch(ctx, a.self, []Candidate{loopback(a)}, 20*time.Millisecond, time.Second)
		fromB <- result{res, err}
	}()
	// An unreachable candidate first: the punch must still pick the working one.
	dead := Candidate{Type: TypeSrflx, Addr: netip.MustParseAddrPort("127.0.0.1:9")}
	res, err := a.Punch(ctx, b.self, []Candidate{dead, loopback(b)}, 20*time.Millisecond, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Remote != loopback(b) {
		t.Fatalf("winner %v, want %v", res.Remote, loopback(b))
	}
	if r := <-fromB; r.err != nil {
		t.Fatal(r.err)
	}
}

func TestPunchTimeout(t *testing.T) {
	a := listen(t, 'a')
	dead := Candidate{Type: TypeSrflx, Addr: netip.MustParseAddrPort("127.0.0.1:9")}
	_, err := a.Punch(context.Background(), PeerID{'x'}, []Candidate{dead}, 20*time.Millisecond, 200*time.Millisecond)
	if err != ErrPunchTimeout {
		t.Fatalf("got %v, want ErrPunchTimeout", err)
	}
}

// fakeSTUN answers Binding requests like cmd/stun does.
func fakeSTUN(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, src, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			req := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
			if req.Decode() != nil {
				continue
			}
			u := src.(*net.UDPAddr)
			resp, _ := stun.Build(stun.NewTransactionIDSetter(req.TransactionID), stun.BindingSuccess,
				&stun.XORMappedAddress{IP: u.IP, Port: u.Port}, stun.Fingerprint)
			conn.WriteTo(resp.Raw, src)
		}
	}()
	return conn.LocalAddr().String()
}

func TestSTUNReturnsMappedAddress(t *testing.T) {
	e := listen(t, 'a')
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got, err := e.STUN(ctx, fakeSTUN(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != loopback(e).Addr {
		t.Fatalf("got %v, want %v", got, loopback(e).Addr)
	}
}
