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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/skyoo2003/acor/pkg/acor"
	acorv1 "github.com/skyoo2003/acor/server/proto/acor/v1"
)

type versionedStatusSource struct{ status acor.VersionedStatus }

func (s *versionedStatusSource) Status() acor.VersionedStatus { return s.status }

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
	if status.Status != "degraded" {
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
