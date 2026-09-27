package coord

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/aamirlatif1/minimesh/proto"
)

func TestRegisterAssignsStableIPs(t *testing.T) {
	r := NewRegistry(netip.MustParsePrefix("10.99.0.0/24"))

	a, err := r.Register("A", "a")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := r.Register("B", "b")
	again, _ := r.Register("A", "a-renamed")

	if a.String() != "10.99.0.1" || b.String() != "10.99.0.2" {
		t.Fatalf("got %s, %s; want 10.99.0.1, 10.99.0.2", a, b)
	}
	if again != a {
		t.Fatalf("re-register got %s, want %s", again, a)
	}
}

func TestRegisterPoolExhausted(t *testing.T) {
	r := NewRegistry(netip.MustParsePrefix("10.99.0.0/30")) // hosts .1 and .2, .3 is broadcast
	for _, k := range []string{"A", "B"} {
		if _, err := r.Register(k, k); err != nil {
			t.Fatalf("register %s: %v", k, err)
		}
	}
	if _, err := r.Register("C", "c"); err == nil {
		t.Fatal("expected pool exhausted")
	}
}

// startServer runs the gRPC service over an in-memory listener.
func startServer(t *testing.T) pb.CoordinatorClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	reg := NewRegistry(netip.MustParsePrefix("10.99.0.0/24"))
	pb.RegisterCoordinatorServer(srv, NewServer(reg, slog.New(slog.NewTextHandler(io.Discard, nil))))
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return pb.NewCoordinatorClient(conn)
}

func register(t *testing.T, c pb.CoordinatorClient, key string) {
	t.Helper()
	if _, err := c.Register(context.Background(), &pb.RegisterRequest{PublicKey: key, Name: key}); err != nil {
		t.Fatal(err)
	}
}

// nextList returns the next PeerList as a slice of names.
func nextList(t *testing.T, s pb.Coordinator_SyncClient) []string {
	t.Helper()
	list, err := s.Recv()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range list.Peers {
		names = append(names, p.Name)
	}
	return names
}

// waitList reads lists until one equals want. Updates coalesce, so
// intermediate lists may or may not be seen.
func waitList(t *testing.T, s pb.Coordinator_SyncClient, want ...string) {
	t.Helper()
	for i := 0; i < 5; i++ {
		got := nextList(t, s)
		if equal(got, want) {
			return
		}
	}
	t.Fatalf("never saw peer list %v", want)
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSyncFanOutAndDisconnect(t *testing.T) {
	c := startServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	register(t, c, "A")
	register(t, c, "B")

	syncA, err := c.Sync(ctx, &pb.SyncRequest{PublicKey: "A"})
	if err != nil {
		t.Fatal(err)
	}
	waitList(t, syncA) // alone: empty list

	ctxB, cancelB := context.WithCancel(ctx)
	syncB, err := c.Sync(ctxB, &pb.SyncRequest{PublicKey: "B"})
	if err != nil {
		t.Fatal(err)
	}
	waitList(t, syncB, "A")
	waitList(t, syncA, "B") // A is told B joined

	cancelB()
	waitList(t, syncA) // and that B left
}

func TestSyncUnknownPeer(t *testing.T) {
	c := startServer(t)
	s, err := c.Sync(context.Background(), &pb.SyncRequest{PublicKey: "nobody"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected error for unregistered peer")
	}
}

func TestSignalForwardsAndStampsSender(t *testing.T) {
	c := startServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	register(t, c, "A")
	register(t, c, "B")

	sigA, err := c.Signal(metadata.AppendToOutgoingContext(ctx, PublicKeyHeader, "A"))
	if err != nil {
		t.Fatal(err)
	}
	sigB, err := c.Signal(metadata.AppendToOutgoingContext(ctx, PublicKeyHeader, "B"))
	if err != nil {
		t.Fatal(err)
	}

	// B's mailbox is opened by the server asynchronously; resend until it arrives.
	got := make(chan *pb.Envelope, 1)
	go func() {
		env, err := sigB.Recv()
		if err == nil {
			got <- env
		}
	}()
	for {
		if err := sigA.Send(&pb.Envelope{From: "spoofed", To: "B", Payload: []byte("hi")}); err != nil {
			t.Fatal(err)
		}
		select {
		case env := <-got:
			if env.From != "A" || string(env.Payload) != "hi" {
				t.Fatalf("got from=%q payload=%q", env.From, env.Payload)
			}
			return
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("envelope never delivered")
		}
	}
}
