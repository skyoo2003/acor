// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	"github.com/skyoo2003/acor/server/health"
	"github.com/skyoo2003/acor/server/logging"
	"github.com/skyoo2003/acor/server/metrics"
	acorv1 "github.com/skyoo2003/acor/server/proto/acor/v1"
	"github.com/skyoo2003/acor/server/tracing"
)

func TestGRPCServerWithObservability(t *testing.T) {
	promReg := prometheus.NewRegistry()
	tracer, err := tracing.NewTracer(&tracing.Config{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	obs := &Observability{
		Metrics: metrics.NewRegistry(promReg),
		Logger:  logging.NewLogger(io.Discard, "info"),
		Tracer:  tracer,
		Health:  health.NewChecker(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	lis := bufconn.Listen(1 << 20)
	srv := NewGRPCServerWithObservability(ctx, &fakeService{addCount: 1}, obs)
	t.Cleanup(srv.Stop)

	if _, ok := srv.GetServiceInfo()["grpc.health.v1.Health"]; !ok {
		t.Fatal("expected grpc.health.v1.Health service to be registered")
	}

	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, addErr := acorv1.NewAcorClient(conn).Add(ctx, &acorv1.KeywordRequest{Keyword: keywordHE}); addErr != nil {
		t.Fatal(addErr)
	}

	hResp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if hResp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health status = %v, want SERVING", hResp.GetStatus())
	}

	families, err := promReg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !hasMetricFamily(families, "grpc_server_handled_total") {
		t.Fatal("expected grpc_server_* metrics to be recorded by the interceptor")
	}
}

type stubChecker struct{ status string }

func (s stubChecker) Check() health.CheckResult { return health.CheckResult{Status: s.status} }

// TestGRPCServerHealthReflectsChecker guards against the health status being
// hardcoded/frozen: an unhealthy checker must surface as NOT_SERVING over the
// standard grpc.health.v1 endpoint.
func TestGRPCServerHealthReflectsChecker(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("dep", stubChecker{status: health.StatusUnhealthy})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	lis := bufconn.Listen(1 << 20)
	srv := NewGRPCServerWithObservability(ctx, &fakeService{}, &Observability{Health: checker})
	t.Cleanup(srv.Stop)
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	resp, err := healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("health status = %v, want NOT_SERVING (must reflect the unhealthy checker)", resp.GetStatus())
	}
}

func TestGRPCServerWithNilObservability(t *testing.T) {
	srv := NewGRPCServerWithObservability(context.Background(), &fakeService{}, nil)
	t.Cleanup(srv.Stop)

	if _, ok := srv.GetServiceInfo()["grpc.health.v1.Health"]; ok {
		t.Fatal("did not expect health service to be registered with nil observability")
	}
}

func hasMetricFamily(families []*dto.MetricFamily, name string) bool {
	for _, f := range families {
		if f.GetName() == name {
			return true
		}
	}
	return false
}
