// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/skyoo2003/acor/pkg/acor"
	servermetrics "github.com/skyoo2003/acor/server/metrics"
	acorv1 "github.com/skyoo2003/acor/server/proto/acor/v1"
)

type versionedStatusSource struct{ status acor.VersionedStatus }

func (s *versionedStatusSource) Status() acor.VersionedStatus { return s.status }
func (s *versionedStatusSource) Find(context.Context, string) ([]string, error) {
	return []string{"match"}, nil
}
func (s *versionedStatusSource) Scan(context.Context, string, *acor.ScanOptions) (*acor.ScanResult, error) {
	return &acor.ScanResult{Matches: []acor.SourceMatch{}}, nil
}
func (s *versionedStatusSource) MaskText(context.Context, string, rune, *acor.RewriteOptions) (*acor.RewriteResult, error) {
	return &acor.RewriteResult{}, nil
}
func (s *versionedStatusSource) ReplaceText(context.Context, string, string, *acor.RewriteOptions) (*acor.RewriteResult, error) {
	return &acor.RewriteResult{}, nil
}
func (s *versionedStatusSource) Replace(context.Context, acor.Version, []string) (*acor.WriteResult, error) {
	return &acor.WriteResult{}, nil
}
func (s *versionedStatusSource) Add(context.Context, acor.Version, string) (*acor.WriteResult, error) {
	return &acor.WriteResult{}, nil
}
func (s *versionedStatusSource) Remove(context.Context, acor.Version, string) (*acor.WriteResult, error) {
	return &acor.WriteResult{}, nil
}
func (s *versionedStatusSource) AddMany(context.Context, acor.Version, []string) (*acor.WriteResult, error) {
	return &acor.WriteResult{}, nil
}
func (s *versionedStatusSource) RemoveMany(context.Context, acor.Version, []string) (*acor.WriteResult, error) {
	return &acor.WriteResult{}, nil
}
func (s *versionedStatusSource) WaitForVersion(context.Context, acor.Version) error { return nil }
func (s *versionedStatusSource) ResolveOperation(context.Context, string) (*acor.WriteResult, error) {
	return &acor.WriteResult{}, nil
}

func TestVersionedHTTPHandlerStatusAndHealth(t *testing.T) {
	collection := versionedStatusSource{status: acor.VersionedStatus{
		ActiveVersion:      "active",
		ServingVersion:     "serving",
		LastRefreshSuccess: time.Now(),
	}}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/status", http.NoBody)
	rec := httptest.NewRecorder()
	NewVersionedHTTPHandler(&collection).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
	var status VersionedStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Status != "ok" || status.ServingVersion == "" || status.RefreshFailures != 0 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.LastRefreshSuccess == "" {
		t.Fatal("missing refresh success timestamp")
	}

	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	rec = httptest.NewRecorder()
	NewVersionedHTTPHandler(&collection).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health code = %d, want 200", rec.Code)
	}
}

func TestVersionedObservabilityConstructorsBindMetrics(t *testing.T) {
	promReg := prometheus.NewRegistry()
	registry := servermetrics.NewRegistry(promReg)
	collection := &versionedStatusSource{status: acor.VersionedStatus{ServingVersion: "serving", ActiveLeases: 4}}
	observability := &VersionedObservability{Metrics: registry, Collection: "moderation-prod"}

	handler, err := NewVersionedHTTPHandlerWithObservability(collection, observability)
	if err != nil {
		t.Fatal(err)
	}
	if handler == nil {
		t.Fatal("expected HTTP handler")
	}
	grpcServer, err := NewVersionedGRPCServerWithObservability(collection, observability)
	if err != nil {
		t.Fatal(err)
	}
	if grpcServer == nil {
		t.Fatal("expected gRPC server")
	}
	grpcServer.Stop()

	families, err := promReg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, family := range families {
		if family.GetName() != "acor_versioned_active_leases" {
			continue
		}
		for _, metric := range family.Metric {
			if metric.GetGauge().GetValue() != 4 {
				continue
			}
			for _, label := range metric.Label {
				if label.GetName() == "collection" && label.GetValue() == "moderation-prod" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("expected labeled V3 metric from automatic binding")
	}
}

func TestVersionedObservabilityRejectsInvalidConfig(t *testing.T) {
	collection := &versionedStatusSource{}
	registry := servermetrics.NewRegistry(prometheus.NewRegistry())
	if _, err := NewVersionedHTTPHandlerWithObservability(collection, &VersionedObservability{Metrics: registry, Collection: "invalid label"}); err == nil {
		t.Fatal("expected invalid label error")
	}
	if _, err := NewVersionedHTTPHandlerWithObservability(collection, &VersionedObservability{Collection: "valid"}); err == nil {
		t.Fatal("expected missing registry error")
	}
	if _, err := NewVersionedHTTPHandlerWithObservability(collection, nil); err == nil {
		t.Fatal("expected missing observability error")
	}
}

func TestVersionedHTTPHandlerOperations(t *testing.T) {
	collection := &versionedStatusSource{status: acor.VersionedStatus{ServingVersion: "serving"}}
	handler := NewVersionedHTTPHandler(collection)
	for _, path := range []string{"/v1/versioned/find", "/v1/versioned/scan", "/v1/versioned/add-many", "/v1/versioned/wait"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path,
			strings.NewReader(`{"input":"text","expected_version":"v","keywords":["word"],"version":"v"}`))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestVersionedRewriteOptionsAreCapped(t *testing.T) {
	opts := rewriteOptions(&VersionedRewriteRequest{
		MaxMatches: maxRewriteMatches + 1, MaxCandidates: maxRewriteCandidates + 1,
		MaxOutputBytes: maxRewriteOutputBytes + 1,
	})
	if opts.MaxMatches != maxRewriteMatches || opts.MaxCandidates != maxRewriteCandidates || opts.MaxOutputBytes != maxRewriteOutputBytes {
		t.Fatalf("rewrite options = %+v, want server caps", opts)
	}
}

func TestVersionedHTTPHandlerRejectsInvalidMask(t *testing.T) {
	handler := NewVersionedHTTPHandler(&versionedStatusSource{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/versioned/mask",
		strings.NewReader(`{"input":"text","mask":"ab"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestVersionedHTTPHandlerHidesErrorDetails(t *testing.T) {
	status := acor.VersionedStatus{LastError: "redis key should not be exposed"}
	rec := httptest.NewRecorder()
	writeVersionedStatus(rec, http.StatusServiceUnavailable, &status)
	if body := rec.Body.String(); body == "" || strings.Contains(body, "redis key") {
		t.Fatalf("status response exposed error details: %s", body)
	}
}

func TestVersionedHTTPHandlerDegradesWithoutServingEngine(t *testing.T) {
	collection := &versionedStatusSource{status: acor.VersionedStatus{LastError: "hidden"}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	rec := httptest.NewRecorder()
	NewVersionedHTTPHandler(collection).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health code = %d, want 503", rec.Code)
	}
	var status VersionedStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Status != statusDegraded {
		t.Fatalf("status = %q, want degraded", status.Status)
	}
}

func TestVersionedGRPCServerStatus(t *testing.T) {
	source := &versionedStatusSource{status: acor.VersionedStatus{
		ActiveVersion:      "active",
		ServingVersion:     "serving",
		LastRefreshSuccess: time.Now(),
	}}
	lis := bufconn.Listen(1 << 20)
	srv := NewVersionedGRPCServer(source)
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

	status, err := acorv1.NewAcorClient(conn).Status(context.Background(), &acorv1.EmptyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if status.GetStatus() != "ok" || status.GetServingVersion() != "serving" {
		t.Fatalf("unexpected gRPC status: %+v", status)
	}
}

func TestVersionedGRPCServerOperations(t *testing.T) {
	source := &versionedStatusSource{status: acor.VersionedStatus{ServingVersion: "serving"}}
	lis := bufconn.Listen(1 << 20)
	srv := NewVersionedGRPCServer(source)
	t.Cleanup(srv.Stop)
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := acorv1.NewAcorClient(conn)
	if _, callErr := client.VersionedFind(context.Background(), &acorv1.VersionedInputRequest{Input: "text"}); callErr != nil {
		t.Fatal(callErr)
	}
	if _, callErr := client.VersionedScan(context.Background(), &acorv1.VersionedScanRequest{Input: "text"}); callErr != nil {
		t.Fatal(callErr)
	}
	if _, callErr := client.VersionedAddMany(context.Background(),
		&acorv1.VersionedWriteRequest{ExpectedVersion: "v", Keywords: []string{"word"}}); callErr != nil {
		t.Fatal(callErr)
	}
	if _, callErr := client.VersionedWait(context.Background(), &acorv1.VersionedWaitRequest{Version: "v"}); callErr != nil {
		t.Fatal(callErr)
	}
	_, err = client.VersionedMask(context.Background(), &acorv1.VersionedRewriteRequest{Input: "text", Mask: "ab"})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("VersionedMask code = %s, want %s; err=%v", grpcstatus.Code(err), codes.InvalidArgument, err)
	}
}
