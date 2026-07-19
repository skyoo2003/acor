// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/skyoo2003/acor/pkg/acor"
)

const defaultReadHeaderTimeout = 5 * time.Second

type Service interface {
	Add(string) (int, error)
	Remove(string) (int, error)
	Find(string) ([]string, error)
	FindIndex(string) (map[string][]int, error)
	Suggest(string) ([]string, error)
	SuggestIndex(string) (map[string][]int, error)
	Flush() error
	Info() (*acor.AhoCorasickInfo, error)
}

type API struct {
	service Service
}

type KeywordRequest struct {
	Keyword string `json:"keyword"`
}

type InputRequest struct {
	Input string `json:"input"`
}

type EmptyRequest struct{}

type CountResponse struct {
	Count int `json:"count"`
}

type MatchesResponse struct {
	Matches []string `json:"matches"`
}

type MatchIndexesResponse struct {
	Matches map[string][]int `json:"matches"`
}

type InfoResponse struct {
	Keywords int `json:"keywords"`
	Nodes    int `json:"nodes"`
}

type StatusResponse struct {
	Status string `json:"status"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func NewAPI(service Service) *API {
	return &API{service: service}
}

func NewHTTPHandler(service Service) http.Handler {
	api := NewAPI(service)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", api.handleHealth)
	mux.HandleFunc("/v1/add", api.handleAdd)
	mux.HandleFunc("/v1/remove", api.handleRemove)
	mux.HandleFunc("/v1/find", api.handleFind)
	mux.HandleFunc("/v1/find-index", api.handleFindIndex)
	mux.HandleFunc("/v1/suggest", api.handleSuggest)
	mux.HandleFunc("/v1/suggest-index", api.handleSuggestIndex)
	mux.HandleFunc("/v1/info", api.handleInfo)
	mux.HandleFunc("/v1/flush", api.handleFlush)
	return mux
}

func NewHTTPServer(addr string, service Service) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewHTTPHandler(service),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
	}
}

func (api *API) Add(_ context.Context, req *KeywordRequest) (*CountResponse, error) {
	if req == nil {
		req = &KeywordRequest{}
	}
	count, err := api.service.Add(req.Keyword)
	if err != nil {
		return nil, err
	}
	return &CountResponse{Count: count}, nil
}

func (api *API) Remove(_ context.Context, req *KeywordRequest) (*CountResponse, error) {
	if req == nil {
		req = &KeywordRequest{}
	}
	count, err := api.service.Remove(req.Keyword)
	if err != nil {
		return nil, err
	}
	return &CountResponse{Count: count}, nil
}

func (api *API) Find(_ context.Context, req *InputRequest) (*MatchesResponse, error) {
	if req == nil {
		req = &InputRequest{}
	}
	matches, err := api.service.Find(req.Input)
	if err != nil {
		return nil, err
	}
	return &MatchesResponse{Matches: matches}, nil
}

func (api *API) FindIndex(_ context.Context, req *InputRequest) (*MatchIndexesResponse, error) {
	if req == nil {
		req = &InputRequest{}
	}
	matches, err := api.service.FindIndex(req.Input)
	if err != nil {
		return nil, err
	}
	return &MatchIndexesResponse{Matches: matches}, nil
}

func (api *API) Suggest(_ context.Context, req *InputRequest) (*MatchesResponse, error) {
	if req == nil {
		req = &InputRequest{}
	}
	matches, err := api.service.Suggest(req.Input)
	if err != nil {
		return nil, err
	}
	return &MatchesResponse{Matches: matches}, nil
}

func (api *API) SuggestIndex(_ context.Context, req *InputRequest) (*MatchIndexesResponse, error) {
	if req == nil {
		req = &InputRequest{}
	}
	matches, err := api.service.SuggestIndex(req.Input)
	if err != nil {
		return nil, err
	}
	return &MatchIndexesResponse{Matches: matches}, nil
}

func (api *API) Info(_ context.Context, _ *EmptyRequest) (*InfoResponse, error) {
	info, err := api.service.Info()
	if err != nil {
		return nil, err
	}
	return &InfoResponse{Keywords: info.Keywords, Nodes: info.Nodes}, nil
}

func (api *API) Flush(_ context.Context, _ *EmptyRequest) (*StatusResponse, error) {
	if err := api.service.Flush(); err != nil {
		return nil, err
	}
	return &StatusResponse{Status: "ok"}, nil
}

func (api *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, &StatusResponse{Status: "ok"})
}

func (api *API) handleAdd(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeKeywordRequest(w, r)
	if !ok {
		return
	}
	resp, err := api.Add(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (api *API) handleRemove(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeKeywordRequest(w, r)
	if !ok {
		return
	}
	resp, err := api.Remove(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (api *API) handleFind(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeInputRequest(w, r)
	if !ok {
		return
	}
	resp, err := api.Find(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (api *API) handleFindIndex(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeInputRequest(w, r)
	if !ok {
		return
	}
	resp, err := api.FindIndex(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (api *API) handleSuggest(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeInputRequest(w, r)
	if !ok {
		return
	}
	resp, err := api.Suggest(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (api *API) handleSuggestIndex(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeInputRequest(w, r)
	if !ok {
		return
	}
	resp, err := api.SuggestIndex(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (api *API) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	resp, err := api.Info(r.Context(), &EmptyRequest{})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (api *API) handleFlush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	resp, err := api.Flush(r.Context(), &EmptyRequest{})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

const maxRequestBodyBytes = 1 << 20 // 1MB

func decodeRequest(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	defer closeReadCloser(r.Body)

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeJSON(w, http.StatusRequestEntityTooLarge,
				&ErrorResponse{Error: fmt.Sprintf("request body must not be larger than %d bytes", mbe.Limit)})
		} else {
			writeJSON(w, http.StatusBadRequest, &ErrorResponse{Error: err.Error()})
		}
		return false
	}
	if err := dec.Decode(new(interface{})); err != io.EOF {
		writeJSON(w, http.StatusBadRequest,
			&ErrorResponse{Error: "request body must contain only a single JSON value"})
		return false
	}
	return true
}

func decodeKeywordRequest(w http.ResponseWriter, r *http.Request) (*KeywordRequest, bool) {
	var req KeywordRequest
	if !decodeRequest(w, r, &req) {
		return nil, false
	}
	return &req, true
}

func decodeInputRequest(w http.ResponseWriter, r *http.Request) (*InputRequest, bool) {
	var req InputRequest
	if !decodeRequest(w, r, &req) {
		return nil, false
	}
	return &req, true
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, &ErrorResponse{Error: "method not allowed"})
}

func writeServiceError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, &ErrorResponse{Error: err.Error()})
}

func writeJSON(w http.ResponseWriter, statusCode int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

func closeReadCloser(closer io.Closer) {
	_ = closer.Close()
}
