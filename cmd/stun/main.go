// Command stun is a minimal STUN server (RFC 8489): it answers Binding
// requests with the requester's address as seen from here, in an
// XOR-MAPPED-ADDRESS attribute. No auth, no TCP, no other methods.
package main

import (
	"flag"
	"log/slog"
	"net"
	"os"

	"github.com/pion/stun/v3"
)

func main() {
	listen := flag.String("listen", ":3478", "UDP address to listen on")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	conn, err := net.ListenPacket("udp4", *listen)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
	log.Info("stun listening", "addr", conn.LocalAddr())

	buf := make([]byte, 1500)
	for {
		n, src, err := conn.ReadFrom(buf)
		if err != nil {
			log.Error("read", "err", err)
			os.Exit(1)
		}
		req := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
		if err := req.Decode(); err != nil || req.Type != stun.BindingRequest {
			continue
		}
		udp := src.(*net.UDPAddr)
		resp, err := stun.Build(
			stun.NewTransactionIDSetter(req.TransactionID),
			stun.BindingSuccess,
			&stun.XORMappedAddress{IP: udp.IP, Port: udp.Port},
			stun.Fingerprint,
		)
		if err != nil {
			log.Error("build response", "err", err)
			continue
		}
		if _, err := conn.WriteTo(resp.Raw, src); err != nil {
			log.Warn("write", "dst", src, "err", err)
			continue
		}
		log.Info("binding", "mapped", udp)
	}
}
