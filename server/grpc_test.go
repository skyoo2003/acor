// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/skyoo2003/acor/pkg/acor"
	acorv1 "github.com/skyoo2003/acor/server/proto/acor/v1"
)

func newGRPCTestClient(t *testing.T, service Service) acorv1.AcorClient {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := NewGRPCServer(service)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return acorv1.NewAcorClient(conn)
}

func TestGRPCServerAddFindRemove(t *testing.T) {
	client := newGRPCTestClient(t, &fakeService{
		addCount:    1,
		removeCount: 1,
		findMatches: []string{keywordHE},
		findIndexes: map[string][]int{keywordHE: {0, 2}},
	})
	ctx := context.Background()

	addResp, err := client.Add(ctx, &acorv1.KeywordRequest{Keyword: keywordHE})
	if err != nil {
		t.Fatal(err)
	}
	if addResp.GetCount() != 1 {
		t.Fatalf("add count = %d, want 1", addResp.GetCount())
	}

	findResp, err := client.Find(ctx, &acorv1.InputRequest{Input: inputHEHE})
	if err != nil {
		t.Fatal(err)
	}
	if len(findResp.GetMatches()) != 1 || findResp.GetMatches()[0] != keywordHE {
		t.Fatalf("find matches = %v", findResp.GetMatches())
	}

	idxResp, err := client.FindIndex(ctx, &acorv1.InputRequest{Input: inputHEHE})
	if err != nil {
		t.Fatal(err)
	}
	pos := idxResp.GetMatches()[keywordHE].GetPositions()
	if len(pos) != 2 || pos[0] != 0 || pos[1] != 2 {
		t.Fatalf("positions = %v, want [0 2]", pos)
	}

	if _, err := client.Remove(ctx, &acorv1.KeywordRequest{Keyword: keywordHE}); err != nil {
		t.Fatal(err)
	}
}

func TestGRPCServerSuggestInfoFlush(t *testing.T) {
	client := newGRPCTestClient(t, &fakeService{
		suggestMatches: []string{keywordHE, "her"},
		suggestIndexes: map[string][]int{keywordHE: {0}},
		info:           &acor.AhoCorasickInfo{Keywords: 2, Nodes: 3},
	})
	ctx := context.Background()

	sResp, err := client.Suggest(ctx, &acorv1.InputRequest{Input: keywordHE})
	if err != nil {
		t.Fatal(err)
	}
	if len(sResp.GetMatches()) != 2 {
		t.Fatalf("suggest matches = %v", sResp.GetMatches())
	}

	siResp, err := client.SuggestIndex(ctx, &acorv1.InputRequest{Input: keywordHE})
	if err != nil {
		t.Fatal(err)
	}
	if len(siResp.GetMatches()) != 1 {
		t.Fatalf("suggestIndex matches = %v", siResp.GetMatches())
	}

	iResp, err := client.Info(ctx, &acorv1.EmptyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if iResp.GetKeywords() != 2 || iResp.GetNodes() != 3 {
		t.Fatalf("info = %+v", iResp)
	}

	fResp, err := client.Flush(ctx, &acorv1.EmptyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if fResp.GetStatus() != statusOK {
		t.Fatalf("flush status = %q", fResp.GetStatus())
	}
}

func TestGRPCServerPropagatesErrors(t *testing.T) {
	client := newGRPCTestClient(t, &fakeService{addErr: errors.New("add failed")})

	_, err := client.Add(context.Background(), &acorv1.KeywordRequest{Keyword: keywordHE})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal code, got %v", err)
	}
}
