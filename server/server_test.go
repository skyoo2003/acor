// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/skyoo2003/acor/pkg/acor"
)

const (
	keywordHE = "he"
	inputHEHE = "hehe"
	statusOK  = "ok"
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

	tests := []struct {
		name string
		call func(*API) error
	}{
		{"add", func(a *API) error {
			_, err := a.Add(context.Background(), &KeywordRequest{Keyword: keywordHE})
			return err
		}},
		{"remove", func(a *API) error {
			_, err := a.Remove(context.Background(), &KeywordRequest{Keyword: keywordHE})
			return err
		}},
		{"find", func(a *API) error {
			_, err := a.Find(context.Background(), &InputRequest{Input: inputHEHE})
			return err
		}},
		{"findIndex", func(a *API) error {
			_, err := a.FindIndex(context.Background(), &InputRequest{Input: inputHEHE})
			return err
		}},
		{"suggest", func(a *API) error {
			_, err := a.Suggest(context.Background(), &InputRequest{Input: keywordHE})
			return err
		}},
		{"suggestIndex", func(a *API) error {
			_, err := a.SuggestIndex(context.Background(), &InputRequest{Input: keywordHE})
			return err
		}},
		{"info", func(a *API) error { _, err := a.Info(context.Background(), &EmptyRequest{}); return err }},
		{"flush", func(a *API) error { _, err := a.Flush(context.Background(), &EmptyRequest{}); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := NewAPI(&fakeService{
				addErr:          svcErr,
				removeErr:       svcErr,
				findErr:         svcErr,
				findIndexErr:    svcErr,
				suggestErr:      svcErr,
				suggestIndexErr: svcErr,
				infoErr:         svcErr,
				flushErr:        svcErr,
			})
			if err := tt.call(api); !errors.Is(err, svcErr) {
				t.Fatalf("expected service error, got %v", err)
			}
		})
	}
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
