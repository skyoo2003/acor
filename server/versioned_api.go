// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/redis/go-redis/v9"

	"github.com/skyoo2003/acor/pkg/acor"
)

// VersionedService is the V3 service boundary exposed by the HTTP and gRPC
// adapters. VersionedCollection implements it directly.
type VersionedService interface {
	VersionedStatusSource
	Find(context.Context, string) ([]string, error)
	Scan(context.Context, string, *acor.ScanOptions) (*acor.ScanResult, error)
	MaskText(context.Context, string, rune, *acor.RewriteOptions) (*acor.RewriteResult, error)
	ReplaceText(context.Context, string, string, *acor.RewriteOptions) (*acor.RewriteResult, error)
	Replace(context.Context, acor.Version, []string) (*acor.WriteResult, error)
	Add(context.Context, acor.Version, string) (*acor.WriteResult, error)
	Remove(context.Context, acor.Version, string) (*acor.WriteResult, error)
	AddMany(context.Context, acor.Version, []string) (*acor.WriteResult, error)
	RemoveMany(context.Context, acor.Version, []string) (*acor.WriteResult, error)
	WaitForVersion(context.Context, acor.Version) error
	ResolveOperation(context.Context, string) (*acor.WriteResult, error)
}

type VersionedInputRequest struct {
	Input string `json:"input"`
}

type VersionedScanRequest struct {
	Input         string `json:"input"`
	MaxInputBytes int    `json:"max_input_bytes,omitempty"`
	MaxMatches    int    `json:"max_matches,omitempty"`
	MaxCandidates int    `json:"max_candidates,omitempty"`
	Kind          int    `json:"kind,omitempty"`
	WholeWord     bool   `json:"whole_word,omitempty"`
}

type VersionedRewriteRequest struct {
	Input          string `json:"input"`
	Replacement    string `json:"replacement,omitempty"`
	Mask           string `json:"mask,omitempty"`
	MaxInputBytes  int    `json:"max_input_bytes,omitempty"`
	MaxMatches     int    `json:"max_matches,omitempty"`
	MaxCandidates  int    `json:"max_candidates,omitempty"`
	MaxOutputBytes int    `json:"max_output_bytes,omitempty"`
	WholeWord      bool   `json:"whole_word,omitempty"`
}

type VersionedWriteRequest struct {
	ExpectedVersion acor.Version `json:"expected_version"`
	Keyword         string       `json:"keyword,omitempty"`
	Keywords        []string     `json:"keywords,omitempty"`
}

type VersionedWaitRequest struct {
	Version acor.Version `json:"version"`
}

type ResolveOperationRequest struct {
	OperationID string `json:"operation_id"`
}

type SourceMatchResponse struct {
	Keyword   string `json:"keyword"`
	Text      string `json:"text"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	ByteStart int    `json:"byte_start"`
	ByteEnd   int    `json:"byte_end"`
}

type VersionedScanResponse struct {
	Matches   []SourceMatchResponse `json:"matches"`
	Truncated bool                  `json:"truncated"`
}

type VersionedRewriteResponse struct {
	Text    string                `json:"text"`
	Matches []SourceMatchResponse `json:"matches"`
}

const (
	maxRewriteMatches     = 1_000
	maxRewriteCandidates  = 100_000
	maxRewriteOutputBytes = 4 << 20
	operationIDLength     = 32
)

var errInvalidRewriteRequest = errors.New("mask must be exactly one Unicode rune")
var errInvalidVersionedRequest = errors.New("invalid versioned request")

type VersionedWriteResponse struct {
	PreviousVersion acor.Version `json:"previous_version"`
	Version         acor.Version `json:"version"`
	OperationID     string       `json:"operation_id"`
	Added           int          `json:"added"`
	Removed         int          `json:"removed"`
	Outcome         string       `json:"outcome,omitempty"`
}

// VersionedAPI adapts VersionedService to HTTP. It deliberately exposes only
// serializable matching options; custom WordRune callbacks remain a Go API.
type VersionedAPI struct{ service VersionedService }

func NewVersionedAPI(service VersionedService) *VersionedAPI { return &VersionedAPI{service: service} }

func scanOptions(req *VersionedScanRequest) *acor.ScanOptions {
	return &acor.ScanOptions{
		MaxInputBytes: req.MaxInputBytes, MaxMatches: req.MaxMatches,
		MaxCandidates: req.MaxCandidates, Kind: acor.MatchKind(req.Kind), WholeWord: req.WholeWord,
	}
}

func rewriteOptions(req *VersionedRewriteRequest) *acor.RewriteOptions {
	return &acor.RewriteOptions{
		MaxInputBytes: req.MaxInputBytes, MaxMatches: min(req.MaxMatches, maxRewriteMatches),
		MaxCandidates: min(req.MaxCandidates, maxRewriteCandidates), MaxOutputBytes: min(req.MaxOutputBytes, maxRewriteOutputBytes), WholeWord: req.WholeWord,
	}
}

func sourceMatches(matches []acor.SourceMatch) []SourceMatchResponse {
	out := make([]SourceMatchResponse, len(matches))
	for i, m := range matches {
		out[i] = SourceMatchResponse{Keyword: m.Keyword, Text: m.Text, Start: m.Start, End: m.End, ByteStart: m.ByteStart, ByteEnd: m.ByteEnd}
	}
	return out
}

func writeResponse(result *acor.WriteResult) *VersionedWriteResponse {
	if result == nil {
		return nil
	}
	return &VersionedWriteResponse{
		PreviousVersion: result.PreviousVersion, Version: result.Version, OperationID: result.OperationID,
		Added: result.Added, Removed: result.Removed,
	}
}

func (api *VersionedAPI) find(ctx context.Context, req *VersionedInputRequest) (*MatchesResponse, error) {
	matches, err := api.service.Find(ctx, req.Input)
	if err != nil {
		return nil, err
	}
	return &MatchesResponse{Matches: matches}, nil
}

func (api *VersionedAPI) scan(ctx context.Context, req *VersionedScanRequest) (*VersionedScanResponse, error) {
	if req.MaxInputBytes < 0 || req.MaxMatches < 0 || req.MaxCandidates < 0 {
		return nil, fmt.Errorf("%w: negative scan limit", errInvalidVersionedRequest)
	}
	if req.Kind != int(acor.MatchKindOverlapping) && req.Kind != int(acor.MatchKindLeftmostLongest) {
		return nil, fmt.Errorf("%w: invalid scan match kind", errInvalidVersionedRequest)
	}
	result, err := api.service.Scan(ctx, req.Input, scanOptions(req))
	if err != nil {
		return nil, err
	}
	return &VersionedScanResponse{Matches: sourceMatches(result.Matches), Truncated: result.Truncated}, nil
}

func (api *VersionedAPI) rewrite(ctx context.Context, req *VersionedRewriteRequest, mask bool) (*VersionedRewriteResponse, error) {
	if req.MaxInputBytes < 0 || req.MaxMatches < 0 || req.MaxCandidates < 0 || req.MaxOutputBytes < 0 {
		return nil, fmt.Errorf("%w: negative rewrite limit", errInvalidVersionedRequest)
	}
	var result *acor.RewriteResult
	var err error
	if mask {
		runes := []rune(req.Mask)
		if len(runes) != 1 {
			return nil, errInvalidRewriteRequest
		}
		result, err = api.service.MaskText(ctx, req.Input, runes[0], rewriteOptions(req))
	} else {
		result, err = api.service.ReplaceText(ctx, req.Input, req.Replacement, rewriteOptions(req))
	}
	if err != nil {
		return nil, err
	}
	return &VersionedRewriteResponse{Text: result.Text, Matches: sourceMatches(result.Matches)}, nil
}

func (api *VersionedAPI) write(ctx context.Context, req *VersionedWriteRequest, operation string) (*VersionedWriteResponse, error) {
	var result *acor.WriteResult
	var err error
	switch operation {
	case "replace":
		result, err = api.service.Replace(ctx, req.ExpectedVersion, req.Keywords)
	case "add":
		result, err = api.service.Add(ctx, req.ExpectedVersion, req.Keyword)
	case "remove":
		result, err = api.service.Remove(ctx, req.ExpectedVersion, req.Keyword)
	case "add-many":
		result, err = api.service.AddMany(ctx, req.ExpectedVersion, req.Keywords)
	case "remove-many":
		result, err = api.service.RemoveMany(ctx, req.ExpectedVersion, req.Keywords)
	}
	response := writeResponse(result)
	if errors.Is(err, acor.ErrCommitUnknown) && response != nil {
		response.Outcome = "unknown"
	}
	return response, err
}

func (api *VersionedAPI) handleFind(w http.ResponseWriter, r *http.Request) {
	var q VersionedInputRequest
	if decodeRequest(w, r, &q) {
		api.respond(w, func() (interface{}, error) { return api.find(r.Context(), &q) })
	}
}
func (api *VersionedAPI) handleScan(w http.ResponseWriter, r *http.Request) {
	var q VersionedScanRequest
	if decodeRequest(w, r, &q) {
		api.respond(w, func() (interface{}, error) { return api.scan(r.Context(), &q) })
	}
}
func (api *VersionedAPI) handleMask(w http.ResponseWriter, r *http.Request) {
	var q VersionedRewriteRequest
	if decodeRequest(w, r, &q) {
		api.respond(w, func() (interface{}, error) { return api.rewrite(r.Context(), &q, true) })
	}
}
func (api *VersionedAPI) handleReplaceText(w http.ResponseWriter, r *http.Request) {
	var q VersionedRewriteRequest
	if decodeRequest(w, r, &q) {
		api.respond(w, func() (interface{}, error) { return api.rewrite(r.Context(), &q, false) })
	}
}
func (api *VersionedAPI) handleWrite(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var q VersionedWriteRequest
		if decodeRequest(w, r, &q) {
			api.respond(w, func() (interface{}, error) { return api.write(r.Context(), &q, operation) })
		}
	}
}
func (api *VersionedAPI) handleWait(w http.ResponseWriter, r *http.Request) {
	var q VersionedWaitRequest
	if decodeRequest(w, r, &q) {
		api.respond(w, func() (interface{}, error) {
			return &StatusResponse{Status: "ok"}, api.service.WaitForVersion(r.Context(), q.Version)
		})
	}
}
func (api *VersionedAPI) handleResolve(w http.ResponseWriter, r *http.Request) {
	var q ResolveOperationRequest
	if decodeRequest(w, r, &q) {
		api.respond(w, func() (interface{}, error) {
			return api.resolve(r.Context(), q.OperationID)
		})
	}
}

func (api *VersionedAPI) resolve(ctx context.Context, id string) (*VersionedWriteResponse, error) {
	if len(id) != operationIDLength {
		return nil, fmt.Errorf("%w: invalid operation ID", errInvalidVersionedRequest)
	}
	result, err := api.service.ResolveOperation(ctx, id)
	return writeResponse(result), err
}

func (api *VersionedAPI) respond(w http.ResponseWriter, call func() (interface{}, error)) {
	response, err := call()
	if errors.Is(err, acor.ErrCommitUnknown) {
		writeJSON(w, http.StatusAccepted, response)
		return
	}
	if err != nil {
		writeVersionedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func writeVersionedError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, acor.ErrConcurrencyConflict):
		code = http.StatusConflict
	case errors.Is(err, acor.ErrMaintenance), errors.Is(err, acor.ErrVersionedClosed):
		code = http.StatusServiceUnavailable
	case errors.Is(err, context.DeadlineExceeded):
		code = http.StatusGatewayTimeout
	case errors.Is(err, context.Canceled):
		code = 499
	case errors.Is(err, redis.Nil):
		code = http.StatusNotFound
	case errors.Is(err, errInvalidVersionedRequest), errors.Is(err, errInvalidRewriteRequest),
		errors.Is(err, acor.ErrInvalidVersion), errors.Is(err, acor.ErrInputLimit),
		errors.Is(err, acor.ErrScanWorkLimit), errors.Is(err, acor.ErrMatchLimit),
		errors.Is(err, acor.ErrOutputLimit):
		code = http.StatusBadRequest
	}
	writeJSON(w, code, &ErrorResponse{Error: err.Error()})
}
