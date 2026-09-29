// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/skyoo2003/acor/pkg/acor"
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

type versionedGRPCServer struct {
	acorv1.UnimplementedAcorServer
	service VersionedService
	metrics *metrics.Registry
}

// NewGRPCServer returns a *grpc.Server serving the acor.server.v1.Acor service
// defined in server/proto/acor/v1/acor.proto. Callers pass any grpc.ServerOption
// (TLS, interceptors, ...) and are responsible for Serve/Stop.
func NewGRPCServer(service Service, opts ...grpc.ServerOption) *grpc.Server {
	s := grpc.NewServer(opts...)
	acorv1.RegisterAcorServer(s, &grpcServer{service: service})
	return s
}

// NewVersionedGRPCServer returns a gRPC server exposing the V3 status, search,
// rewrite, and expected-version write RPCs. Legacy collection RPCs remain
// unimplemented because their mutation contract is incompatible with V3.
func NewVersionedGRPCServer(service VersionedService, opts ...grpc.ServerOption) *grpc.Server {
	return NewVersionedGRPCServerWithMetrics(service, nil, opts...)
}

// NewVersionedGRPCServerWithMetrics is NewVersionedGRPCServer and updates the
// supplied bounded V3 gauges whenever Status is called.
func NewVersionedGRPCServerWithMetrics(service VersionedService, registry *metrics.Registry, opts ...grpc.ServerOption) *grpc.Server {
	s := grpc.NewServer(opts...)
	acorv1.RegisterAcorServer(s, &versionedGRPCServer{service: service, metrics: registry})
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

func (s *versionedGRPCServer) Status(_ context.Context, _ *acorv1.EmptyRequest) (*acorv1.VersionedStatusResponse, error) {
	state := s.service.Status()
	if s.metrics != nil {
		s.metrics.UpdateVersionedStatus(&state)
	}
	return versionedStatusProto(&state), nil
}

func versionedGRPCError(err error) error {
	switch {
	case errors.Is(err, acor.ErrConcurrencyConflict):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, acor.ErrMaintenance), errors.Is(err, acor.ErrVersionedClosed):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, redis.Nil):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, errInvalidRewriteRequest), errors.Is(err, acor.ErrInvalidVersion), errors.Is(err, acor.ErrInputLimit),
		errors.Is(err, acor.ErrScanWorkLimit), errors.Is(err, acor.ErrMatchLimit),
		errors.Is(err, acor.ErrOutputLimit):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

func protoSourceMatches(matches []SourceMatchResponse) []*acorv1.SourceMatch {
	out := make([]*acorv1.SourceMatch, len(matches))
	for i, m := range matches {
		out[i] = &acorv1.SourceMatch{
			Keyword: m.Keyword, Text: m.Text, Start: int64(m.Start), End: int64(m.End),
			ByteStart: int64(m.ByteStart), ByteEnd: int64(m.ByteEnd),
		}
	}
	return out
}

func protoWrite(result *VersionedWriteResponse) *acorv1.VersionedWriteResponse {
	if result == nil {
		return nil
	}
	return &acorv1.VersionedWriteResponse{
		PreviousVersion: string(result.PreviousVersion), Version: string(result.Version),
		OperationId: result.OperationID, Added: int64(result.Added), Removed: int64(result.Removed), Outcome: result.Outcome,
	}
}

func (s *versionedGRPCServer) VersionedFind(ctx context.Context, req *acorv1.VersionedInputRequest) (*acorv1.MatchesResponse, error) {
	r, err := NewVersionedAPI(s.service).find(ctx, &VersionedInputRequest{Input: req.GetInput()})
	if err != nil {
		return nil, versionedGRPCError(err)
	}
	return &acorv1.MatchesResponse{Matches: r.Matches}, nil
}

func (s *versionedGRPCServer) VersionedScan(ctx context.Context, req *acorv1.VersionedScanRequest) (*acorv1.VersionedScanResponse, error) {
	request := &VersionedScanRequest{
		Input: req.GetInput(), MaxInputBytes: int(req.GetMaxInputBytes()), MaxMatches: int(req.GetMaxMatches()),
		MaxCandidates: int(req.GetMaxCandidates()), Kind: int(req.GetKind()), WholeWord: req.GetWholeWord(),
	}
	r, err := NewVersionedAPI(s.service).scan(ctx, request)
	if err != nil {
		return nil, versionedGRPCError(err)
	}
	return &acorv1.VersionedScanResponse{Matches: protoSourceMatches(r.Matches), Truncated: r.Truncated}, nil
}

func (s *versionedGRPCServer) versionedRewrite(ctx context.Context, req *acorv1.VersionedRewriteRequest, mask bool) (*acorv1.VersionedRewriteResponse, error) {
	request := &VersionedRewriteRequest{
		Input: req.GetInput(), Replacement: req.GetReplacement(), Mask: req.GetMask(),
		MaxInputBytes: int(req.GetMaxInputBytes()), MaxMatches: int(req.GetMaxMatches()),
		MaxCandidates: int(req.GetMaxCandidates()), MaxOutputBytes: int(req.GetMaxOutputBytes()), WholeWord: req.GetWholeWord(),
	}
	r, err := NewVersionedAPI(s.service).rewrite(ctx, request, mask)
	if err != nil {
		return nil, versionedGRPCError(err)
	}
	return &acorv1.VersionedRewriteResponse{Text: r.Text, Matches: protoSourceMatches(r.Matches)}, nil
}
func (s *versionedGRPCServer) VersionedMask(ctx context.Context, req *acorv1.VersionedRewriteRequest) (*acorv1.VersionedRewriteResponse, error) {
	return s.versionedRewrite(ctx, req, true)
}
func (s *versionedGRPCServer) VersionedReplaceText(ctx context.Context, req *acorv1.VersionedRewriteRequest) (*acorv1.VersionedRewriteResponse, error) {
	return s.versionedRewrite(ctx, req, false)
}

func (s *versionedGRPCServer) versionedWrite(ctx context.Context, req *acorv1.VersionedWriteRequest, operation string) (*acorv1.VersionedWriteResponse, error) {
	request := &VersionedWriteRequest{
		ExpectedVersion: acor.Version(req.GetExpectedVersion()), Keyword: req.GetKeyword(), Keywords: req.GetKeywords(),
	}
	r, err := NewVersionedAPI(s.service).write(ctx, request, operation)
	if errors.Is(err, acor.ErrCommitUnknown) {
		return protoWrite(r), nil
	}
	if err != nil {
		return nil, versionedGRPCError(err)
	}
	return protoWrite(r), nil
}
func (s *versionedGRPCServer) VersionedReplace(ctx context.Context, req *acorv1.VersionedWriteRequest) (*acorv1.VersionedWriteResponse, error) {
	return s.versionedWrite(ctx, req, "replace")
}
func (s *versionedGRPCServer) VersionedAdd(ctx context.Context, req *acorv1.VersionedWriteRequest) (*acorv1.VersionedWriteResponse, error) {
	return s.versionedWrite(ctx, req, "add")
}
func (s *versionedGRPCServer) VersionedRemove(ctx context.Context, req *acorv1.VersionedWriteRequest) (*acorv1.VersionedWriteResponse, error) {
	return s.versionedWrite(ctx, req, "remove")
}
func (s *versionedGRPCServer) VersionedAddMany(ctx context.Context, req *acorv1.VersionedWriteRequest) (*acorv1.VersionedWriteResponse, error) {
	return s.versionedWrite(ctx, req, "add-many")
}
func (s *versionedGRPCServer) VersionedRemoveMany(ctx context.Context, req *acorv1.VersionedWriteRequest) (*acorv1.VersionedWriteResponse, error) {
	return s.versionedWrite(ctx, req, "remove-many")
}

func (s *versionedGRPCServer) VersionedWait(ctx context.Context, req *acorv1.VersionedWaitRequest) (*acorv1.StatusResponse, error) {
	if err := s.service.WaitForVersion(ctx, acor.Version(req.GetVersion())); err != nil {
		return nil, versionedGRPCError(err)
	}
	return &acorv1.StatusResponse{Status: "ok"}, nil
}
func (s *versionedGRPCServer) ResolveOperation(ctx context.Context, req *acorv1.ResolveOperationRequest) (*acorv1.VersionedWriteResponse, error) {
	r, err := s.service.ResolveOperation(ctx, req.GetOperationId())
	if err != nil {
		return nil, versionedGRPCError(err)
	}
	return protoWrite(writeResponse(r)), nil
}

func versionedStatusProto(state *acor.VersionedStatus) *acorv1.VersionedStatusResponse {
	response := &acorv1.VersionedStatusResponse{
		Status:          "ok",
		ActiveVersion:   string(state.ActiveVersion),
		ServingVersion:  string(state.ServingVersion),
		Building:        state.Building,
		RefreshFailures: state.RefreshFailures,
		ActiveLeases:    int64(state.ActiveLeases),
	}
	if state.LastError != "" || state.ServingVersion == "" {
		response.Status = statusDegraded
	}
	if !state.LastRefreshSuccess.IsZero() {
		response.LastRefreshSuccess = state.LastRefreshSuccess.UTC().Format(time.RFC3339Nano)
	}
	if !state.LastRefreshFailure.IsZero() {
		response.LastRefreshFailure = state.LastRefreshFailure.UTC().Format(time.RFC3339Nano)
	}
	return response
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
