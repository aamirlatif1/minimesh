// Command peer is the minimesh agent (NetBird's client): it registers with
// the coordinator, gathers candidates via STUN, exchanges them with every
// other peer and hole-punches a direct UDP path.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/aamirlatif1/minimesh/internal/wg"
)

type config struct {
	name         string
	coordinator  string
	stunServer   string
	keyFile      string
	port         int
	punchEvery   time.Duration
	punchTimeout time.Duration
}

func main() {
	host, _ := os.Hostname()
	var cfg config
	flag.StringVar(&cfg.name, "name", host, "peer name shown to others")
	flag.StringVar(&cfg.coordinator, "coordinator", "203.0.113.1:7000", "coordinator gRPC address")
	flag.StringVar(&cfg.stunServer, "stun", "203.0.113.1:3478", "STUN server address")
	flag.StringVar(&cfg.keyFile, "key", "", "private key file, created if missing (default state/<name>.key)")
	flag.IntVar(&cfg.port, "port", 51820, "UDP port for probes and, later, WireGuard")
	flag.DurationVar(&cfg.punchEvery, "punch-interval", 200*time.Millisecond, "time between probe rounds")
	flag.DurationVar(&cfg.punchTimeout, "punch-timeout", 5*time.Second, "give up punching after this long")
	debug := flag.Bool("debug", false, "debug logging")
	flag.Parse()
	if cfg.keyFile == "" {
		cfg.keyFile = fmt.Sprintf("state/%s.key", cfg.name)
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})).With("peer", cfg.name)

	if err := run(cfg, log); err != nil {
		log.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run(cfg config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	key, err := wg.LoadOrCreateKey(cfg.keyFile)
	if err != nil {
		return fmt.Errorf("key: %w", err)
	}

	conn, err := grpc.NewClient(cfg.coordinator, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	a, err := newAgent(cfg, key, conn, log)
	if err != nil {
		return err
	}
	defer a.close()

	err = a.run(ctx)
	if ctx.Err() != nil {
		return nil // interrupted
	}
	return err
}
