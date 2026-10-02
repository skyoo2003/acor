// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/skyoo2003/acor/pkg/acor"
	acorv1 "github.com/skyoo2003/acor/server/proto/acor/v1"
)

func newVersionedValidationCollection(t *testing.T) *acor.VersionedCollection {
	t.Helper()
	backend := miniredis.RunT(t)
	v, err := acor.OpenVersioned(context.Background(), &acor.VersionedOptions{
		Redis: acor.AhoCorasickArgs{Addr: backend.Addr(), Name: t.Name()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v
}

func TestVersionedHTTPRejectsMalformedInputs(t *testing.T) {
	handler := NewVersionedHTTPHandler(newVersionedValidationCollection(t))
	for _, tc := range []struct{ name, route, body string }{
		{"kind", "scan", `{"input":"text","kind":99}`},
		{"negative input", "scan", `{"input":"text","max_input_bytes":-1}`},
		{"negative matches", "scan", `{"input":"text","max_matches":-1}`},
		{"negative candidates", "scan", `{"input":"text","max_candidates":-1}`},
		{"negative output", "replace-text", `{"input":"text","replacement":"x","max_output_bytes":-1}`},
		{"negative rewrite matches", "mask", `{"input":"text","mask":"*","max_matches":-1}`},
		{"operation ID", "resolve-operation", `{"operation_id":"invalid"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/versioned/"+tc.route, strings.NewReader(tc.body))
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestVersionedGRPCRejectsMalformedInputs(t *testing.T) {
	adapter := &versionedGRPCServer{service: newVersionedValidationCollection(t)}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"kind", func() error {
			_, err := adapter.VersionedScan(ctx, &acorv1.VersionedScanRequest{Input: "text", Kind: 99})
			return err
		}},
		{"negative matches", func() error {
			_, err := adapter.VersionedScan(ctx, &acorv1.VersionedScanRequest{Input: "text", MaxMatches: -1})
			return err
		}},
		{"negative output", func() error {
			_, err := adapter.VersionedReplaceText(ctx, &acorv1.VersionedRewriteRequest{Input: "text", MaxOutputBytes: -1})
			return err
		}},
		{"operation ID", func() error {
			_, err := adapter.ResolveOperation(ctx, &acorv1.ResolveOperationRequest{OperationId: "invalid"})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); grpcstatus.Code(err) != codes.InvalidArgument {
				t.Fatalf("code=%s, want InvalidArgument; err=%v", grpcstatus.Code(err), err)
			}
		})
	}
}
