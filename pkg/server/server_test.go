package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/skyoo2003/acor/pkg/acor"
)

const (
	keywordHE   = "he"
	inputHEHE   = "hehe"
	statusOK    = "ok"
	testBufSize = 1024 * 1024
)

type fakeService struct {
	addCount       int
	removeCount    int
	findMatches    []string
	findIndexes    map[string][]int
	suggestMatches []string
	suggestIndexes map[string][]int
	info           *acor.AhoCorasickInfo
	addErr         error
	infoErr        error
	flushErr       error
	lastKeyword    string
	lastInput      string
	flushCalls     int
}

func (f *fakeService) Add(keyword string) (int, error) {
	f.lastKeyword = keyword
	if f.addErr != nil {
		return 0, f.addErr
	}
	return f.addCount, nil
}

func (f *fakeService) Remove(keyword string) (int, error) {
	f.lastKeyword = keyword
	return f.removeCount, nil
}

func (f *fakeService) Find(input string) ([]string, error) {
	f.lastInput = input
	return f.findMatches, nil
}

func (f *fakeService) FindIndex(input string) (map[string][]int, error) {
	f.lastInput = input
	return f.findIndexes, nil
}

func (f *fakeService) Suggest(input string) ([]string, error) {
	f.lastInput = input
	return f.suggestMatches, nil
}

func (f *fakeService) SuggestIndex(input string) (map[string][]int, error) {
	f.lastInput = input
	return f.suggestIndexes, nil
}

func (f *fakeService) Flush() error {
	f.flushCalls++
	return f.flushErr
}

func (f *fakeService) Info() (*acor.AhoCorasickInfo, error) {
	if f.infoErr != nil {
		return nil, f.infoErr
	}
	return f.info, nil
}

func TestHTTPHandlerHealth(t *testing.T) {
	server := httptest.NewServer(NewHTTPHandler(&fakeService{}))
	defer server.Close()

	var body StatusResponse
	doJSONRequest(t, http.MethodGet, server.URL+"/healthz", nil, &body)

	if body.Status != statusOK {
		t.Fatalf("expected health status %q, got %q", statusOK, body.Status)
	}
}

func TestHTTPHandlerAddAndFind(t *testing.T) {
	service := &fakeService{
		addCount:    1,
		findMatches: []string{keywordHE},
		findIndexes: map[string][]int{keywordHE: {0, 2}},
	}
	server := httptest.NewServer(NewHTTPHandler(service))
	defer server.Close()

	var addBody CountResponse
	doJSONRequest(t, http.MethodPost, server.URL+"/v1/add", KeywordRequest{Keyword: keywordHE}, &addBody)
	if addBody.Count != 1 {
		t.Fatalf("expected add count 1, got %d", addBody.Count)
	}
	if service.lastKeyword != keywordHE {
		t.Fatalf("expected add keyword %q, got %q", keywordHE, service.lastKeyword)
	}

	var findBody MatchesResponse
	doJSONRequest(t, http.MethodPost, server.URL+"/v1/find", InputRequest{Input: inputHEHE}, &findBody)
	if len(findBody.Matches) != 1 || findBody.Matches[0] != keywordHE {
		t.Fatalf("unexpected find matches %v", findBody.Matches)
	}
	if service.lastInput != inputHEHE {
		t.Fatalf("expected find input %q, got %q", inputHEHE, service.lastInput)
	}

	var indexBody MatchIndexesResponse
	doJSONRequest(t, http.MethodPost, server.URL+"/v1/find-index", InputRequest{Input: inputHEHE}, &indexBody)
	assertIndexes(t, indexBody.Matches[keywordHE], []int{0, 2})
}

func TestHTTPHandlerSuggestInfoAndFlush(t *testing.T) {
	service := &fakeService{
		suggestMatches: []string{keywordHE, "her"},
		suggestIndexes: map[string][]int{keywordHE: {0}, "her": {0}},
		info:           &acor.AhoCorasickInfo{Keywords: 2, Nodes: 3},
	}
	server := httptest.NewServer(NewHTTPHandler(service))
	defer server.Close()

	var suggestBody MatchesResponse
	doJSONRequest(t, http.MethodPost, server.URL+"/v1/suggest", InputRequest{Input: keywordHE}, &suggestBody)
	if len(suggestBody.Matches) != 2 {
		t.Fatalf("unexpected suggest matches %v", suggestBody.Matches)
	}

	var suggestIndexBody MatchIndexesResponse
	doJSONRequest(t, http.MethodPost, server.URL+"/v1/suggest-index", InputRequest{Input: keywordHE}, &suggestIndexBody)
	if len(suggestIndexBody.Matches) != 2 {
		t.Fatalf("unexpected suggest index matches %v", suggestIndexBody.Matches)
	}

	var infoBody InfoResponse
	doJSONRequest(t, http.MethodGet, server.URL+"/v1/info", nil, &infoBody)
	if infoBody.Keywords != 2 || infoBody.Nodes != 3 {
		t.Fatalf("unexpected info response %+v", infoBody)
	}

	var flushBody StatusResponse
	doJSONRequest(t, http.MethodPost, server.URL+"/v1/flush", EmptyRequest{}, &flushBody)
	if flushBody.Status != statusOK {
		t.Fatalf("expected flush status %q, got %q", statusOK, flushBody.Status)
	}
	if service.flushCalls != 1 {
		t.Fatalf("expected flush to be called once, got %d", service.flushCalls)
	}
}

func TestHTTPHandlerReturnsStructuredErrors(t *testing.T) {
	service := &fakeService{addErr: errors.New("add failed")}
	server := httptest.NewServer(NewHTTPHandler(service))
	defer server.Close()

	resp := doRawRequest(t, http.MethodGet, server.URL+"/v1/add", nil)
	defer closeReadCloser(resp.Body)
	var methodBody ErrorResponse
	decodeJSONResponse(t, resp, &methodBody)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", resp.StatusCode)
	}
	if methodBody.Error != "method not allowed" {
		t.Fatalf("unexpected error %q", methodBody.Error)
	}

	resp = doRawRequest(t, http.MethodPost, server.URL+"/v1/add", mustJSONReader(t, KeywordRequest{Keyword: keywordHE}))
	defer closeReadCloser(resp.Body)
	var serviceBody ErrorResponse
	decodeJSONResponse(t, resp, &serviceBody)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.StatusCode)
	}
	if serviceBody.Error != "add failed" {
		t.Fatalf("unexpected error %q", serviceBody.Error)
	}

	resp = doRawRequest(t, http.MethodPost, server.URL+"/v1/find", bytes.NewBufferString("{"))
	defer closeReadCloser(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestGRPCServerAddAndFind(t *testing.T) {
	service := &fakeService{
		addCount:    1,
		findMatches: []string{keywordHE},
		findIndexes: map[string][]int{keywordHE: {0, 2}},
	}
	lis := bufconn.Listen(testBufSize)
	grpcServer := NewGRPCServer(service)
	defer grpcServer.Stop()

	go func() {
		_ = grpcServer.Serve(lis)
	}()

	conn := dialBufConn(t, lis)
	defer closeClientConn(conn)

	ctx := context.Background()

	var addResp CountResponse
	if err := conn.Invoke(ctx, GRPCMethodAdd, &KeywordRequest{Keyword: keywordHE}, &addResp); err != nil {
		t.Fatal(err)
	}
	if addResp.Count != 1 {
		t.Fatalf("expected add count 1, got %d", addResp.Count)
	}

	var findResp MatchesResponse
	if err := conn.Invoke(ctx, GRPCMethodFind, &InputRequest{Input: inputHEHE}, &findResp); err != nil {
		t.Fatal(err)
	}
	if len(findResp.Matches) != 1 || findResp.Matches[0] != keywordHE {
		t.Fatalf("unexpected find matches %v", findResp.Matches)
	}

	var indexResp MatchIndexesResponse
	if err := conn.Invoke(ctx, GRPCMethodFindIndex, &InputRequest{Input: inputHEHE}, &indexResp); err != nil {
		t.Fatal(err)
	}
	assertIndexes(t, indexResp.Matches[keywordHE], []int{0, 2})

	var removeResp CountResponse
	if err := conn.Invoke(ctx, GRPCMethodRemove, &KeywordRequest{Keyword: keywordHE}, &removeResp); err != nil {
		t.Fatal(err)
	}
}

func TestGRPCServerInfoFlushAndErrors(t *testing.T) {
	service := &fakeService{
		addErr:         errors.New("add failed"),
		info:           &acor.AhoCorasickInfo{Keywords: 1, Nodes: 2},
		suggestMatches: []string{keywordHE, "her"},
		suggestIndexes: map[string][]int{keywordHE: {0}, "her": {0}},
	}
	lis := bufconn.Listen(testBufSize)
	grpcServer := NewGRPCServer(service)
	defer grpcServer.Stop()

	go func() {
		_ = grpcServer.Serve(lis)
	}()

	conn := dialBufConn(t, lis)
	defer closeClientConn(conn)

	ctx := context.Background()

	var infoResp InfoResponse
	if err := conn.Invoke(ctx, GRPCMethodInfo, &EmptyRequest{}, &infoResp); err != nil {
		t.Fatal(err)
	}
	if infoResp.Keywords != 1 || infoResp.Nodes != 2 {
		t.Fatalf("unexpected info response %+v", infoResp)
	}

	var suggestResp MatchesResponse
	if err := conn.Invoke(ctx, GRPCMethodSuggest, &InputRequest{Input: keywordHE}, &suggestResp); err != nil {
		t.Fatal(err)
	}
	if len(suggestResp.Matches) != 2 {
		t.Fatalf("unexpected suggest matches %v", suggestResp.Matches)
	}

	var suggestIndexResp MatchIndexesResponse
	if err := conn.Invoke(ctx, GRPCMethodSuggestIndex, &InputRequest{Input: keywordHE}, &suggestIndexResp); err != nil {
		t.Fatal(err)
	}
	if len(suggestIndexResp.Matches) != 2 {
		t.Fatalf("unexpected suggest index matches %v", suggestIndexResp.Matches)
	}

	var flushResp StatusResponse
	if err := conn.Invoke(ctx, GRPCMethodFlush, &EmptyRequest{}, &flushResp); err != nil {
		t.Fatal(err)
	}
	if flushResp.Status != statusOK {
		t.Fatalf("expected flush status %q, got %q", statusOK, flushResp.Status)
	}

	err := conn.Invoke(ctx, GRPCMethodAdd, &KeywordRequest{Keyword: keywordHE}, &CountResponse{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal gRPC error, got %v", err)
	}
}

func doJSONRequest(t *testing.T, method, url string, payload, target interface{}) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		body = mustJSONReader(t, payload)
	}

	resp := doRawRequest(t, method, url, body)
	defer closeReadCloser(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	decodeJSONResponse(t, resp, target)
}

func doRawRequest(t *testing.T, method, url string, body io.Reader) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeJSONResponse(t *testing.T, resp *http.Response, target interface{}) {
	t.Helper()

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func mustJSONReader(t *testing.T, payload interface{}) io.Reader {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(body)
}

func dialBufConn(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(JSONCodec{}), grpc.CallContentSubtype(JSONCodec{}.Name())),
	)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func assertIndexes(t *testing.T, actual, expected []int) {
	t.Helper()

	if len(actual) != len(expected) {
		t.Fatalf("expected indexes %v, got %v", expected, actual)
	}
	for idx, actualIndex := range actual {
		if actualIndex != expected[idx] {
			t.Fatalf("expected indexes %v, got %v", expected, actual)
		}
	}
}

func closeClientConn(conn *grpc.ClientConn) {
	_ = conn.Close()
}
