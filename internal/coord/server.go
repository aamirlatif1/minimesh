package coord

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/aamirlatif1/minimesh/proto"
)

// PublicKeyHeader is the gRPC metadata key a peer uses to identify its
// Signal stream. Not authenticated: anyone can claim any key. NetBird
// authenticates peers at the management service; see README.
const PublicKeyHeader = "x-public-key"

// Server implements the Coordinator gRPC service on top of a Registry.
type Server struct {
	pb.UnimplementedCoordinatorServer
	reg *Registry
	log *slog.Logger
}

func NewServer(reg *Registry, log *slog.Logger) *Server {
	return &Server{reg: reg, log: log}
}

func (s *Server) Register(_ context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	if req.PublicKey == "" {
		return nil, status.Error(codes.InvalidArgument, "public_key is required")
	}
	ip, err := s.reg.Register(req.PublicKey, req.Name)
	if err != nil {
		return nil, status.Error(codes.ResourceExhausted, err.Error())
	}
	s.log.Info("register", "name", req.Name, "key", short(req.PublicKey), "ip", ip)
	return &pb.RegisterResponse{OverlayIp: ip.String()}, nil
}

func (s *Server) Sync(req *pb.SyncRequest, stream pb.Coordinator_SyncServer) error {
	changed, unsubscribe, err := s.reg.Subscribe(req.PublicKey)
	if err != nil {
		return status.Error(codes.NotFound, err.Error())
	}
	defer func() {
		unsubscribe()
		s.log.Info("sync closed", "key", short(req.PublicKey))
	}()
	s.log.Info("sync opened", "key", short(req.PublicKey))

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-changed:
			if err := stream.Send(s.reg.PeerList(req.PublicKey)); err != nil {
				return err
			}
		}
	}
}

func (s *Server) Signal(stream pb.Coordinator_SignalServer) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	keys := md.Get(PublicKeyHeader)
	if len(keys) != 1 {
		return status.Errorf(codes.InvalidArgument, "missing %s header", PublicKeyHeader)
	}
	self := keys[0]

	inbox, closeMailbox, err := s.reg.OpenMailbox(self)
	if err != nil {
		return status.Error(codes.NotFound, err.Error())
	}
	defer closeMailbox()

	// Deliver our mailbox to the stream while the main loop reads from it.
	ctx := stream.Context()
	sendErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case env := <-inbox:
				if err := stream.Send(env); err != nil {
					sendErr <- err
					return
				}
			}
		}
	}()

	for {
		env, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
				return nil
			}
			return err
		}
		select {
		case err := <-sendErr:
			return err
		default:
		}
		env.From = self // never trust the sender's claim
		if err := s.reg.Deliver(env); err != nil {
			s.log.Warn("signal dropped", "from", short(self), "to", short(env.To), "err", err)
			continue
		}
		s.log.Debug("signal", "from", short(self), "to", short(env.To), "bytes", len(env.Payload))
	}
}

// short abbreviates a base64 public key for logs.
func short(key string) string {
	if len(key) > 8 {
		return key[:8]
	}
	return key
}
