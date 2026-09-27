// Command coordinator runs the registry, peer-list stream and signalling
// (NetBird's Management and Signal services, in one small gRPC server).
package main

import (
	"flag"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	"github.com/aamirlatif1/minimesh/internal/coord"
	pb "github.com/aamirlatif1/minimesh/proto"
)

func main() {
	listen := flag.String("listen", ":7000", "gRPC listen address")
	pool := flag.String("pool", "10.99.0.0/24", "overlay address pool")
	debug := flag.Bool("debug", false, "debug logging")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	prefix, err := netip.ParsePrefix(*pool)
	if err != nil {
		log.Error("bad -pool", "err", err)
		os.Exit(2)
	}

	lis, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
	srv := grpc.NewServer()
	pb.RegisterCoordinatorServer(srv, coord.NewServer(coord.NewRegistry(prefix), log))

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		srv.Stop()
	}()

	log.Info("coordinator listening", "addr", lis.Addr(), "pool", prefix)
	if err := srv.Serve(lis); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
