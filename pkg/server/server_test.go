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
	"github.com/skyoo2003/acor/pkg/health"
)

const (
	keywordHE   = "he"
	inputHEHE   = "hehe"
	statusOK    = "ok"
	testBufSize = 1024 * 1024
)

type fakeService struct {
	addCount        int
	removeCount     int
	findMatches     []string
	findIndexes     map[string][]int
	suggestMatches  []string
	suggestIndexes  map[string][]int
	info            *acor.AhoCorasickInfo
	addErr          error
	removeErr       error
	findErr         error
	findIndexErr    error
	suggestErr      error
	suggestIndexErr error
	infoErr         error
	flushErr        error
	lastKeyword     string
	lastInput       string
	flushCalls      int
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
	if f.removeErr != nil {
		return 0, f.removeErr
	}
	return f.removeCount, nil
}

func (f *fakeService) Find(input string) ([]string, error) {
	f.lastInput = input
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.findMatches, nil
}

func (f *fakeService) FindIndex(input string) (map[string][]int, error) {
	f.lastInput = input
	if f.findIndexErr != nil {
		return nil, f.findIndexErr
	}
	return f.findIndexes, nil
}

func (f *fakeService) Suggest(input string) ([]string, error) {
	f.lastInput = input
	if f.suggestErr != nil {
		return nil, f.suggestErr
	}
	return f.suggestMatches, nil
}

func (f *fakeService) SuggestIndex(input string) (map[string][]int, error) {
	f.lastInput = input
	if f.suggestIndexErr != nil {
		return nil, f.suggestIndexErr
	}
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

func TestNewHTTPServer(t *testing.T) {
	service := &fakeService{}
	srv := NewHTTPServer("127.0.0.1:0", service)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.Addr != "127.0.0.1:0" {
		t.Errorf("Addr = %q, want %q", srv.Addr, "127.0.0.1:0")
	}
}

func TestHTTPHandlerRemove(t *testing.T) {
	service := &fakeService{removeCount: 1}
	server := httptest.NewServer(NewHTTPHandler(service))
	defer server.Close()

	var body CountResponse
	doJSONRequest(t, http.MethodPost, server.URL+"/v1/remove", KeywordRequest{Keyword: keywordHE}, &body)
	if body.Count != 1 {
		t.Fatalf("expected remove count 1, got %d", body.Count)
	}
	if service.lastKeyword != keywordHE {
		t.Fatalf("expected remove keyword %q, got %q", keywordHE, service.lastKeyword)
	}
}

func TestHTTPHandlerRemoveWrongMethod(t *testing.T) {
	service := &fakeService{}
	server := httptest.NewServer(NewHTTPHandler(service))
	defer server.Close()

	resp := doRawRequest(t, http.MethodGet, server.URL+"/v1/remove", nil)
	defer closeReadCloser(resp.Body)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestGRPCServerWithObservability(t *testing.T) {
	service := &fakeService{
		addCount:    1,
		findMatches: []string{keywordHE},
		info:        &acor.AhoCorasickInfo{Keywords: 1, Nodes: 2},
	}

	obs := &Observability{
		Health: health.NewChecker(),
	}

	lis := bufconn.Listen(testBufSize)
	grpcServer := NewGRPCServerWithObservability(service, obs)
	defer grpcServer.Stop()

	if _, ok := grpcServer.GetServiceInfo()["grpc.health.v1.Health"]; !ok {
		t.Fatal("expected gRPC health service to be registered")
	}

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

	var infoResp InfoResponse
	if err := conn.Invoke(ctx, GRPCMethodInfo, &EmptyRequest{}, &infoResp); err != nil {
		t.Fatal(err)
	}
	if infoResp.Keywords != 1 || infoResp.Nodes != 2 {
		t.Fatalf("unexpected info response %+v", infoResp)
	}
}

func TestGRPCServerWithNilObservability(t *testing.T) {
	service := &fakeService{
		addCount:    1,
		findMatches: []string{keywordHE},
	}

	lis := bufconn.Listen(testBufSize)
	grpcServer := NewGRPCServerWithObservability(service, nil)
	defer grpcServer.Stop()

	if _, ok := grpcServer.GetServiceInfo()["grpc.health.v1.Health"]; ok {
		t.Fatal("did not expect gRPC health service to be registered with nil observability")
	}

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
}

func TestHTTPHandlerHealthWrongMethod(t *testing.T) {
	service := &fakeService{}
	server := httptest.NewServer(NewHTTPHandler(service))
	defer server.Close()

	resp := doRawRequest(t, http.MethodPost, server.URL+"/healthz", nil)
	defer closeReadCloser(resp.Body)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestHTTPHandlerInfoWrongMethod(t *testing.T) {
	service := &fakeService{}
	server := httptest.NewServer(NewHTTPHandler(service))
	defer server.Close()

	resp := doRawRequest(t, http.MethodPost, server.URL+"/v1/info", nil)
	defer closeReadCloser(resp.Body)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestHTTPHandlerFlushWrongMethod(t *testing.T) {
	service := &fakeService{}
	server := httptest.NewServer(NewHTTPHandler(service))
	defer server.Close()

	resp := doRawRequest(t, http.MethodGet, server.URL+"/v1/flush", nil)
	defer closeReadCloser(resp.Body)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestHTTPHandlerFlushError(t *testing.T) {
	service := &fakeService{flushErr: errors.New("flush failed")}
	server := httptest.NewServer(NewHTTPHandler(service))
	defer server.Close()

	resp := doRawRequest(t, http.MethodPost, server.URL+"/v1/flush", mustJSONReader(t, EmptyRequest{}))
	defer closeReadCloser(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.StatusCode)
	}
}

func TestHTTPHandlerInfoError(t *testing.T) {
	service := &fakeService{infoErr: errors.New("info failed")}
	server := httptest.NewServer(NewHTTPHandler(service))
	defer server.Close()

	resp := doRawRequest(t, http.MethodGet, server.URL+"/v1/info", nil)
	defer closeReadCloser(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.StatusCode)
	}
}

func TestHandlersWithInterceptor(t *testing.T) {
	api := NewAPI(&fakeService{
		addCount:       1,
		removeCount:    2,
		findMatches:    []string{keywordHE},
		findIndexes:    map[string][]int{keywordHE: {0}},
		suggestMatches: []string{keywordHE},
		suggestIndexes: map[string][]int{keywordHE: {0}},
		info:           &acor.AhoCorasickInfo{Keywords: 3, Nodes: 4},
	})

	var capturedMethod string
	interceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		capturedMethod = info.FullMethod
		return handler(ctx, req)
	}

	t.Run("add", func(t *testing.T) {
		capturedMethod = ""
		resp, err := addHandler(api, context.Background(), func(v interface{}) error {
			v.(*KeywordRequest).Keyword = keywordHE
			return nil
		}, interceptor)
		if err != nil {
			t.Fatal(err)
		}
		if resp.(*CountResponse).Count != 1 {
			t.Fatalf("expected count 1, got %d", resp.(*CountResponse).Count)
		}
		if capturedMethod != GRPCMethodAdd {
			t.Fatalf("expected method %q, got %q", GRPCMethodAdd, capturedMethod)
		}
	})

	t.Run("remove", func(t *testing.T) {
		capturedMethod = ""
		resp, err := removeHandler(api, context.Background(), func(v interface{}) error {
			v.(*KeywordRequest).Keyword = keywordHE
			return nil
		}, interceptor)
		if err != nil {
			t.Fatal(err)
		}
		if resp.(*CountResponse).Count != 2 {
			t.Fatalf("expected count 2, got %d", resp.(*CountResponse).Count)
		}
		if capturedMethod != GRPCMethodRemove {
			t.Fatalf("expected method %q, got %q", GRPCMethodRemove, capturedMethod)
		}
	})

	t.Run("find", func(t *testing.T) {
		capturedMethod = ""
		resp, err := findHandler(api, context.Background(), func(v interface{}) error {
			v.(*InputRequest).Input = inputHEHE
			return nil
		}, interceptor)
		if err != nil {
			t.Fatal(err)
		}
		matches := resp.(*MatchesResponse).Matches
		if len(matches) != 1 || matches[0] != keywordHE {
			t.Fatalf("unexpected matches %v", matches)
		}
		if capturedMethod != GRPCMethodFind {
			t.Fatalf("expected method %q, got %q", GRPCMethodFind, capturedMethod)
		}
	})

	t.Run("findIndex", func(t *testing.T) {
		capturedMethod = ""
		resp, err := findIndexHandler(api, context.Background(), func(v interface{}) error {
			v.(*InputRequest).Input = inputHEHE
			return nil
		}, interceptor)
		if err != nil {
			t.Fatal(err)
		}
		matches := resp.(*MatchIndexesResponse).Matches
		if len(matches) != 1 {
			t.Fatalf("unexpected matches %v", matches)
		}
		if capturedMethod != GRPCMethodFindIndex {
			t.Fatalf("expected method %q, got %q", GRPCMethodFindIndex, capturedMethod)
		}
	})

	t.Run("suggest", func(t *testing.T) {
		capturedMethod = ""
		resp, err := suggestHandler(api, context.Background(), func(v interface{}) error {
			v.(*InputRequest).Input = keywordHE
			return nil
		}, interceptor)
		if err != nil {
			t.Fatal(err)
		}
		matches := resp.(*MatchesResponse).Matches
		if len(matches) != 1 || matches[0] != keywordHE {
			t.Fatalf("unexpected matches %v", matches)
		}
		if capturedMethod != GRPCMethodSuggest {
			t.Fatalf("expected method %q, got %q", GRPCMethodSuggest, capturedMethod)
		}
	})

	t.Run("suggestIndex", func(t *testing.T) {
		capturedMethod = ""
		resp, err := suggestIndexHandler(api, context.Background(), func(v interface{}) error {
			v.(*InputRequest).Input = keywordHE
			return nil
		}, interceptor)
		if err != nil {
			t.Fatal(err)
		}
		matches := resp.(*MatchIndexesResponse).Matches
		if len(matches) != 1 {
			t.Fatalf("unexpected matches %v", matches)
		}
		if capturedMethod != GRPCMethodSuggestIndex {
			t.Fatalf("expected method %q, got %q", GRPCMethodSuggestIndex, capturedMethod)
		}
	})

	t.Run("info", func(t *testing.T) {
		capturedMethod = ""
		resp, err := infoHandler(api, context.Background(), func(v interface{}) error {
			return nil
		}, interceptor)
		if err != nil {
			t.Fatal(err)
		}
		info := resp.(*InfoResponse)
		if info.Keywords != 3 || info.Nodes != 4 {
			t.Fatalf("unexpected info %+v", info)
		}
		if capturedMethod != GRPCMethodInfo {
			t.Fatalf("expected method %q, got %q", GRPCMethodInfo, capturedMethod)
		}
	})

	t.Run("flush", func(t *testing.T) {
		capturedMethod = ""
		resp, err := flushHandler(api, context.Background(), func(v interface{}) error {
			return nil
		}, interceptor)
		if err != nil {
			t.Fatal(err)
		}
		if resp.(*StatusResponse).Status != statusOK {
			t.Fatalf("expected status %q, got %q", statusOK, resp.(*StatusResponse).Status)
		}
		if capturedMethod != GRPCMethodFlush {
			t.Fatalf("expected method %q, got %q", GRPCMethodFlush, capturedMethod)
		}
	})
}

func TestHandlersDecoderError(t *testing.T) {
	api := NewAPI(&fakeService{})

	decoderErr := errors.New("decode failed")
	badDecoder := func(interface{}) error { return decoderErr }

	tests := []struct {
		name    string
		handler grpc.MethodHandler
	}{
		{"add", addHandler},
		{"remove", removeHandler},
		{"find", findHandler},
		{"findIndex", findIndexHandler},
		{"suggest", suggestHandler},
		{"suggestIndex", suggestIndexHandler},
		{"info", infoHandler},
		{"flush", flushHandler},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.handler(api, context.Background(), badDecoder, nil)
			if err != decoderErr {
				t.Fatalf("expected decoder error, got %v", err)
			}
		})
	}
}

func TestHandlersInterceptorError(t *testing.T) {
	api := NewAPI(&fakeService{})

	interceptorErr := errors.New("interceptor failed")
	errInterceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return nil, interceptorErr
	}

	tests := []struct {
		name    string
		handler grpc.MethodHandler
		decoder func(interface{}) error
	}{
		{"add", addHandler, func(v interface{}) error { return nil }},
		{"remove", removeHandler, func(v interface{}) error { return nil }},
		{"find", findHandler, func(v interface{}) error { return nil }},
		{"findIndex", findIndexHandler, func(v interface{}) error { return nil }},
		{"suggest", suggestHandler, func(v interface{}) error { return nil }},
		{"suggestIndex", suggestIndexHandler, func(v interface{}) error { return nil }},
		{"info", infoHandler, func(v interface{}) error { return nil }},
		{"flush", flushHandler, func(v interface{}) error { return nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.handler(api, context.Background(), tt.decoder, errInterceptor)
			if err != interceptorErr {
				t.Fatalf("expected interceptor error, got %v", err)
			}
		})
	}
}

func TestAPINilRequests(t *testing.T) {
	api := NewAPI(&fakeService{
		addCount:    0,
		removeCount: 0,
	})

	t.Run("add_nil", func(t *testing.T) {
		resp, err := api.Add(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Count != 0 {
			t.Fatalf("expected 0, got %d", resp.Count)
		}
	})

	t.Run("remove_nil", func(t *testing.T) {
		resp, err := api.Remove(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Count != 0 {
			t.Fatalf("expected 0, got %d", resp.Count)
		}
	})

	t.Run("find_nil", func(t *testing.T) {
		resp, err := api.Find(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Matches != nil {
			t.Fatalf("expected nil matches, got %v", resp.Matches)
		}
	})

	t.Run("findIndex_nil", func(t *testing.T) {
		resp, err := api.FindIndex(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Matches != nil {
			t.Fatalf("expected nil matches, got %v", resp.Matches)
		}
	})

	t.Run("suggest_nil", func(t *testing.T) {
		resp, err := api.Suggest(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Matches != nil {
			t.Fatalf("expected nil matches, got %v", resp.Matches)
		}
	})

	t.Run("suggestIndex_nil", func(t *testing.T) {
		resp, err := api.SuggestIndex(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Matches != nil {
			t.Fatalf("expected nil matches, got %v", resp.Matches)
		}
	})
}

func TestAPIServiceErrors(t *testing.T) {
	svcErr := errors.New("service error")

	t.Run("remove_error", func(t *testing.T) {
		api := NewAPI(&fakeService{removeErr: svcErr})
		_, err := api.Remove(context.Background(), &KeywordRequest{Keyword: keywordHE})
		if status.Code(err) != codes.Internal {
			t.Fatalf("expected Internal code, got %v", err)
		}
	})

	t.Run("find_error", func(t *testing.T) {
		api := NewAPI(&fakeService{findErr: svcErr})
		_, err := api.Find(context.Background(), &InputRequest{Input: inputHEHE})
		if status.Code(err) != codes.Internal {
			t.Fatalf("expected Internal code, got %v", err)
		}
	})

	t.Run("findIndex_error", func(t *testing.T) {
		api := NewAPI(&fakeService{findIndexErr: svcErr})
		_, err := api.FindIndex(context.Background(), &InputRequest{Input: inputHEHE})
		if status.Code(err) != codes.Internal {
			t.Fatalf("expected Internal code, got %v", err)
		}
	})

	t.Run("suggest_error", func(t *testing.T) {
		api := NewAPI(&fakeService{suggestErr: svcErr})
		_, err := api.Suggest(context.Background(), &InputRequest{Input: keywordHE})
		if status.Code(err) != codes.Internal {
			t.Fatalf("expected Internal code, got %v", err)
		}
	})

	t.Run("suggestIndex_error", func(t *testing.T) {
		api := NewAPI(&fakeService{suggestIndexErr: svcErr})
		_, err := api.SuggestIndex(context.Background(), &InputRequest{Input: keywordHE})
		if status.Code(err) != codes.Internal {
			t.Fatalf("expected Internal code, got %v", err)
		}
	})

	t.Run("add_error", func(t *testing.T) {
		api := NewAPI(&fakeService{addErr: svcErr})
		_, err := api.Add(context.Background(), &KeywordRequest{Keyword: keywordHE})
		if status.Code(err) != codes.Internal {
			t.Fatalf("expected Internal code, got %v", err)
		}
	})

	t.Run("info_error", func(t *testing.T) {
		api := NewAPI(&fakeService{infoErr: svcErr})
		_, err := api.Info(context.Background(), &EmptyRequest{})
		if status.Code(err) != codes.Internal {
			t.Fatalf("expected Internal code, got %v", err)
		}
	})

	t.Run("flush_error", func(t *testing.T) {
		api := NewAPI(&fakeService{flushErr: svcErr})
		_, err := api.Flush(context.Background(), &EmptyRequest{})
		if status.Code(err) != codes.Internal {
			t.Fatalf("expected Internal code, got %v", err)
		}
	})
}

func TestJSONCodec(t *testing.T) {
	codec := JSONCodec{}

	t.Run("name", func(t *testing.T) {
		if codec.Name() != "acor-json" {
			t.Fatalf("expected name %q, got %q", "acor-json", codec.Name())
		}
	})

	t.Run("marshal", func(t *testing.T) {
		data, err := codec.Marshal(&CountResponse{Count: 42})
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != `{"count":42}` {
			t.Fatalf("unexpected marshal output: %s", data)
		}
	})

	t.Run("unmarshal_empty", func(t *testing.T) {
		var resp CountResponse
		if err := codec.Unmarshal([]byte{}, &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Count != 0 {
			t.Fatalf("expected 0, got %d", resp.Count)
		}
	})

	t.Run("unmarshal_nil_data", func(t *testing.T) {
		var resp CountResponse
		if err := codec.Unmarshal(nil, &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Count != 0 {
			t.Fatalf("expected 0, got %d", resp.Count)
		}
	})

	t.Run("unmarshal_with_data", func(t *testing.T) {
		var resp CountResponse
		if err := codec.Unmarshal([]byte(`{"count":7}`), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Count != 7 {
			t.Fatalf("expected 7, got %d", resp.Count)
		}
	})
}

func TestCloseReadCloser(t *testing.T) {
	called := false
	rc := &testCloser{closeFn: func() error { called = true; return nil }}
	closeReadCloser(rc)
	if !called {
		t.Fatal("expected Close to be called")
	}
}

type testCloser struct {
	closeFn func() error
}

func (t *testCloser) Close() error { return t.closeFn() }

func TestHTTPHandlerFindErrors(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		method  string
		service *fakeService
	}{
		{"remove_error", "/v1/remove", http.MethodPost, &fakeService{removeErr: errors.New("fail")}},
		{"find_error", "/v1/find", http.MethodPost, &fakeService{findErr: errors.New("fail")}},
		{"findIndex_error", "/v1/find-index", http.MethodPost, &fakeService{findIndexErr: errors.New("fail")}},
		{"suggest_error", "/v1/suggest", http.MethodPost, &fakeService{suggestErr: errors.New("fail")}},
		{"suggestIndex_error", "/v1/suggest-index", http.MethodPost, &fakeService{suggestIndexErr: errors.New("fail")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(NewHTTPHandler(tt.service))
			defer server.Close()

			var body io.Reader
			if tt.path == "/v1/remove" {
				body = mustJSONReader(t, KeywordRequest{Keyword: keywordHE})
			} else {
				body = mustJSONReader(t, InputRequest{Input: inputHEHE})
			}

			resp := doRawRequest(t, tt.method, server.URL+tt.path, body)
			defer closeReadCloser(resp.Body)
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("expected status 500, got %d", resp.StatusCode)
			}
		})
	}
}

func TestHTTPHandlerWrongMethods(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		method string
	}{
		{"find_wrong_method", "/v1/find", http.MethodGet},
		{"findIndex_wrong_method", "/v1/find-index", http.MethodGet},
		{"suggest_wrong_method", "/v1/suggest", http.MethodGet},
		{"suggestIndex_wrong_method", "/v1/suggest-index", http.MethodGet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(NewHTTPHandler(&fakeService{}))
			defer server.Close()

			resp := doRawRequest(t, tt.method, server.URL+tt.path, nil)
			defer closeReadCloser(resp.Body)
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("expected status 405, got %d", resp.StatusCode)
			}
		})
	}
}
