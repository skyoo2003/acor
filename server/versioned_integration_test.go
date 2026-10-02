// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/skyoo2003/acor/pkg/acor"
	acorv1 "github.com/skyoo2003/acor/server/proto/acor/v1"
)

func newVersionedTransportIntegration(t *testing.T) (ctx context.Context, writer, delayed *acor.VersionedCollection) {
	t.Helper()
	addr := os.Getenv("ACOR_INTEGRATION_ADDR")
	if addr == "" {
		t.Skip("ACOR_INTEGRATION_ADDR not set; skipping real-server integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("v3-transport-integration-%x", nonce)
	cleanup := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() {
		defer cleanup.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		root := fmt.Sprintf("{acor-v3-%x", sha256.Sum256([]byte(name)))
		for _, pattern := range []string{root + "}:*", root + "-shard-*}:*"} {
			var keys []string
			iter := cleanup.Scan(cleanupCtx, 0, pattern, 100).Iterator()
			for iter.Next(cleanupCtx) {
				keys = append(keys, iter.Val())
			}
			if err := iter.Err(); err != nil {
				t.Error(err)
				continue
			}
			if len(keys) != 0 {
				if err := cleanup.Del(cleanupCtx, keys...).Err(); err != nil {
					t.Error(err)
				}
			}
		}
	})
	open := func(debounce time.Duration) *acor.VersionedCollection {
		v, err := acor.OpenVersioned(ctx, &acor.VersionedOptions{
			Redis: acor.AhoCorasickArgs{Addr: addr, Name: name}, ShardCount: 4,
			ShardConcurrency: 2, PollInterval: time.Hour, RefreshDebounce: debounce,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := v.Close(); err != nil {
				t.Error(err)
			}
		})
		return v
	}
	return ctx, open(time.Millisecond), open(time.Hour)
}

func versionedIntegrationGRPC(t *testing.T, ctx context.Context, v *acor.VersionedCollection) acorv1.AcorClient {
	t.Helper()
	var config net.ListenConfig
	lis, err := config.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewVersionedGRPCServer(v)
	t.Cleanup(srv.Stop)
	go func() {
		if serveErr := srv.Serve(lis); serveErr != nil {
			t.Error(serveErr)
		}
	}()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return acorv1.NewAcorClient(conn)
}

func versionedIntegrationPOST(t *testing.T, ctx context.Context, srv *httptest.Server, path string, body any, wantCode int, out any) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantCode {
		t.Fatalf("%s status = %d, want %d; body = %s", path, response.StatusCode, wantCode, data)
	}
	if out != nil {
		if err = json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
}

// Exercise registered network routes with real Lua commits and local serving barriers.
func TestIntegrationVersionedHTTPAndGRPC(t *testing.T) {
	ctx, collection, delayed := newVersionedTransportIntegration(t)
	httpServer := httptest.NewServer(NewVersionedHTTPHandler(collection))
	t.Cleanup(httpServer.Close)
	client := versionedIntegrationGRPC(t, ctx, collection)
	delayedClient := versionedIntegrationGRPC(t, ctx, delayed)
	base := collection.Status().ServingVersion
	// Warm the connection before testing the barrier's deadline.
	if _, err := delayedClient.VersionedWait(ctx, &acorv1.VersionedWaitRequest{Version: string(base)}); err != nil {
		t.Fatal(err)
	}
	var written VersionedWriteResponse
	versionedIntegrationPOST(t, ctx, httpServer, "/v1/versioned/add-many",
		VersionedWriteRequest{ExpectedVersion: base, Keywords: []string{"한국어"}}, http.StatusOK, &written)
	if written.Added != 1 || written.Version == base || written.OperationID == "" {
		t.Fatalf("HTTP write receipt: %+v", written)
	}
	versionedIntegrationPOST(t, ctx, httpServer, "/v1/versioned/wait",
		VersionedWaitRequest{Version: written.Version}, http.StatusOK, nil)
	var found MatchesResponse
	versionedIntegrationPOST(t, ctx, httpServer, "/v1/versioned/find",
		VersionedInputRequest{Input: "한국어 문장"}, http.StatusOK, &found)
	if !slices.Equal(found.Matches, []string{"한국어"}) {
		t.Fatalf("HTTP search after barrier: %q", found.Matches)
	}
	r, err := client.VersionedAdd(ctx, &acorv1.VersionedWriteRequest{ExpectedVersion: string(written.Version), Keyword: "한국"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.VersionedWait(ctx, &acorv1.VersionedWaitRequest{Version: r.GetVersion()}); err != nil {
		t.Fatal(err)
	}
	matches, err := client.VersionedFind(ctx, &acorv1.VersionedInputRequest{Input: "한국어 문장"})
	if err != nil {
		t.Fatal(err)
	}
	got := matches.GetMatches()
	slices.Sort(got)
	if !slices.Equal(got, []string{"한국", "한국어"}) {
		t.Fatalf("gRPC search after barrier: %q", got)
	}
	t.Run("stale expected version", func(t *testing.T) {
		versionedIntegrationPOST(t, ctx, httpServer, "/v1/versioned/add",
			VersionedWriteRequest{ExpectedVersion: base, Keyword: "conflict"}, http.StatusConflict, nil)
		_, callErr := client.VersionedAdd(ctx, &acorv1.VersionedWriteRequest{ExpectedVersion: string(base), Keyword: "conflict"})
		if grpcstatus.Code(callErr) != codes.Aborted {
			t.Fatalf("conflict code = %s, err = %v", grpcstatus.Code(callErr), callErr)
		}
	})
	t.Run("unserved committed version deadline", func(t *testing.T) {
		waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, callErr := delayedClient.VersionedWait(waitCtx, &acorv1.VersionedWaitRequest{Version: r.GetVersion()})
		if grpcstatus.Code(callErr) != codes.DeadlineExceeded {
			t.Fatalf("barrier deadline code = %s, err = %v", grpcstatus.Code(callErr), callErr)
		}
		if delayed.Status().ServingVersion != base {
			t.Fatalf("delayed reader unexpectedly served the target: %+v", delayed.Status())
		}
	})
	t.Run("malformed network gRPC requests", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			call func() error
		}{
			{"scan kind", func() error {
				_, callErr := client.VersionedScan(ctx, &acorv1.VersionedScanRequest{Input: "한국어", Kind: 99})
				return callErr
			}},
			{"negative match limit", func() error {
				_, callErr := client.VersionedScan(ctx, &acorv1.VersionedScanRequest{Input: "한국어", MaxMatches: -1})
				return callErr
			}},
			{"operation ID", func() error {
				_, callErr := client.ResolveOperation(ctx, &acorv1.ResolveOperationRequest{OperationId: "invalid"})
				return callErr
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if callErr := tc.call(); grpcstatus.Code(callErr) != codes.InvalidArgument {
					t.Fatalf("malformed request code = %s, err = %v", grpcstatus.Code(callErr), callErr)
				}
			})
		}
	})
}
