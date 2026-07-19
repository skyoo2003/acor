// SPDX-License-Identifier: Apache-2.0

package health

import (
	"context"
	"time"

	"google.golang.org/grpc"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// healthPollInterval is how often the gRPC serving status is re-derived from the
// checker. Kept short so grpc.health.v1 probes track live readiness, matching
// the per-request behaviour of the HTTP /readyz endpoint.
const healthPollInterval = 5 * time.Second

// RegisterGRPCHealthServer installs the standard grpc.health.v1 service and keeps
// its serving status in sync with the checker until ctx is cancelled. The overall
// server ("") and each named service track the checker's live result; a nil
// checker reports SERVING. On ctx cancellation the server is marked NOT_SERVING
// (graceful drain). The returned *grpchealth.Server is also handed back for
// callers that want direct control.
func RegisterGRPCHealthServer(ctx context.Context, registrar grpc.ServiceRegistrar, checker *HealthChecker, services ...string) *grpchealth.Server {
	hs := grpchealth.NewServer()
	names := append([]string{""}, services...)

	update := func() {
		status := healthpb.HealthCheckResponse_SERVING
		if checker != nil && checker.Check().Status != StatusHealthy {
			status = healthpb.HealthCheckResponse_NOT_SERVING
		}
		for _, name := range names {
			hs.SetServingStatus(name, status)
		}
	}
	update() // seed before the server starts accepting probes

	go func() {
		ticker := time.NewTicker(healthPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				hs.Shutdown()
				return
			case <-ticker.C:
				update()
			}
		}
	}()

	healthpb.RegisterHealthServer(registrar, hs)
	return hs
}
