// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/skyoo2003/acor/server/health"
	"github.com/skyoo2003/acor/server/logging"
	"github.com/skyoo2003/acor/server/metrics"
	acorv1 "github.com/skyoo2003/acor/server/proto/acor/v1"
	"github.com/skyoo2003/acor/server/tracing"
)

// Observability bundles the optional observability backends wired into a gRPC
// server. Any field may be nil to skip that pillar.
type Observability struct {
	Metrics *metrics.Registry
	Logger  *logging.Logger
	Tracer  *tracing.Tracer
	Health  *health.HealthChecker
}

// grpcServer adapts the Service interface to the generated protobuf AcorServer.
type grpcServer struct {
	acorv1.UnimplementedAcorServer
	service Service
}

// NewGRPCServer returns a *grpc.Server serving the acor.server.v1.Acor service
// defined in server/proto/acor/v1/acor.proto. Callers pass any grpc.ServerOption
// (TLS, interceptors, ...) and are responsible for Serve/Stop.
func NewGRPCServer(service Service, opts ...grpc.ServerOption) *grpc.Server {
	s := grpc.NewServer(opts...)
	acorv1.RegisterAcorServer(s, &grpcServer{service: service})
	return s
}

// NewGRPCServerWithObservability is NewGRPCServer plus standard-library
// observability: OpenTelemetry tracing (otelgrpc stats handler), Prometheus
// metrics (grpc_server_*), zerolog request logging, and the grpc.health.v1
// health service. Each pillar is wired only when its Observability field is set.
//
// ctx bounds the background health-status poller: cancel it (e.g. on server
// shutdown) to stop the goroutine and mark the server NOT_SERVING.
func NewGRPCServerWithObservability(ctx context.Context, service Service, obs *Observability, opts ...grpc.ServerOption) *grpc.Server {
	var serverOpts []grpc.ServerOption
	var unary []grpc.UnaryServerInterceptor

	if obs != nil {
		if obs.Tracer != nil {
			serverOpts = append(serverOpts, grpc.StatsHandler(tracing.GRPCStatsHandler(obs.Tracer)))
		}
		if obs.Metrics != nil {
			unary = append(unary, obs.Metrics.GRPCServer.UnaryServerInterceptor())
		}
		if obs.Logger != nil {
			unary = append(unary, logging.GRPCUnaryInterceptor(obs.Logger))
		}
	}
	if len(unary) > 0 {
		serverOpts = append(serverOpts, grpc.ChainUnaryInterceptor(unary...))
	}
	serverOpts = append(serverOpts, opts...)

	s := grpc.NewServer(serverOpts...)
	acorv1.RegisterAcorServer(s, &grpcServer{service: service})

	if obs != nil {
		if obs.Metrics != nil {
			obs.Metrics.GRPCServer.InitializeMetrics(s)
		}
		if obs.Health != nil {
			health.RegisterGRPCHealthServer(ctx, s, obs.Health, acorv1.Acor_ServiceDesc.ServiceName)
		}
	}
	return s
}

func (s *grpcServer) Add(_ context.Context, req *acorv1.KeywordRequest) (*acorv1.CountResponse, error) {
	count, err := s.service.Add(req.GetKeyword())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &acorv1.CountResponse{Count: int64(count)}, nil
}

func (s *grpcServer) Remove(_ context.Context, req *acorv1.KeywordRequest) (*acorv1.CountResponse, error) {
	count, err := s.service.Remove(req.GetKeyword())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &acorv1.CountResponse{Count: int64(count)}, nil
}

func (s *grpcServer) Find(_ context.Context, req *acorv1.InputRequest) (*acorv1.MatchesResponse, error) {
	matches, err := s.service.Find(req.GetInput())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &acorv1.MatchesResponse{Matches: matches}, nil
}

func (s *grpcServer) FindIndex(_ context.Context, req *acorv1.InputRequest) (*acorv1.MatchIndexesResponse, error) {
	matches, err := s.service.FindIndex(req.GetInput())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &acorv1.MatchIndexesResponse{Matches: toPositions(matches)}, nil
}

func (s *grpcServer) Suggest(_ context.Context, req *acorv1.InputRequest) (*acorv1.MatchesResponse, error) {
	matches, err := s.service.Suggest(req.GetInput())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &acorv1.MatchesResponse{Matches: matches}, nil
}

func (s *grpcServer) SuggestIndex(_ context.Context, req *acorv1.InputRequest) (*acorv1.MatchIndexesResponse, error) {
	matches, err := s.service.SuggestIndex(req.GetInput())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &acorv1.MatchIndexesResponse{Matches: toPositions(matches)}, nil
}

func (s *grpcServer) Info(_ context.Context, _ *acorv1.EmptyRequest) (*acorv1.InfoResponse, error) {
	info, err := s.service.Info()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &acorv1.InfoResponse{Keywords: int64(info.Keywords), Nodes: int64(info.Nodes)}, nil
}

func (s *grpcServer) Flush(_ context.Context, _ *acorv1.EmptyRequest) (*acorv1.StatusResponse, error) {
	if err := s.service.Flush(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &acorv1.StatusResponse{Status: "ok"}, nil
}

// toPositions converts native match-index offsets to their protobuf wrapper.
func toPositions(m map[string][]int) map[string]*acorv1.Positions {
	if m == nil {
		return nil
	}
	out := make(map[string]*acorv1.Positions, len(m))
	for kw, offsets := range m {
		ps := make([]int64, len(offsets))
		for i, off := range offsets {
			ps[i] = int64(off)
		}
		out[kw] = &acorv1.Positions{Positions: ps}
	}
	return out
}
