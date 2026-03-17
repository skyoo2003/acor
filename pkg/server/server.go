package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/skyoo2003/acor/pkg/acor"
	"github.com/skyoo2003/acor/pkg/health"
	"github.com/skyoo2003/acor/pkg/logging"
	"github.com/skyoo2003/acor/pkg/metrics"
	"github.com/skyoo2003/acor/pkg/tracing"
)

const (
	GRPCServiceName          = "acor.server.v1.Acor"
	GRPCMethodAdd            = "/" + GRPCServiceName + "/Add"
	GRPCMethodRemove         = "/" + GRPCServiceName + "/Remove"
	GRPCMethodFind           = "/" + GRPCServiceName + "/Find"
	GRPCMethodFindIndex      = "/" + GRPCServiceName + "/FindIndex"
	GRPCMethodSuggest        = "/" + GRPCServiceName + "/Suggest"
	GRPCMethodSuggestIndex   = "/" + GRPCServiceName + "/SuggestIndex"
	GRPCMethodInfo           = "/" + GRPCServiceName + "/Info"
	GRPCMethodFlush          = "/" + GRPCServiceName + "/Flush"
	defaultReadHeaderTimeout = 5 * time.Second
)

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

type GRPCService interface {
	Add(context.Context, *KeywordRequest) (*CountResponse, error)
	Remove(context.Context, *KeywordRequest) (*CountResponse, error)
	Find(context.Context, *InputRequest) (*MatchesResponse, error)
	FindIndex(context.Context, *InputRequest) (*MatchIndexesResponse, error)
	Suggest(context.Context, *InputRequest) (*MatchesResponse, error)
	SuggestIndex(context.Context, *InputRequest) (*MatchIndexesResponse, error)
	Info(context.Context, *EmptyRequest) (*InfoResponse, error)
	Flush(context.Context, *EmptyRequest) (*StatusResponse, error)
}

type API struct {
	service Service
}

type JSONCodec struct{}

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

type Observability struct {
	Metrics *metrics.Registry
	Logger  *logging.Logger
	Tracer  *tracing.Tracer
	Health  *health.HealthChecker
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

func NewGRPCServer(service Service, opts ...grpc.ServerOption) *grpc.Server {
	serverOpts := append([]grpc.ServerOption{grpc.ForceServerCodec(JSONCodec{})}, opts...)
	grpcServer := grpc.NewServer(serverOpts...)
	RegisterGRPCServer(grpcServer, NewAPI(service))
	return grpcServer
}

func NewGRPCServerWithObservability(service Service, obs *Observability, opts ...grpc.ServerOption) *grpc.Server {
	var serverOpts []grpc.ServerOption

	var unaryInterceptors []grpc.UnaryServerInterceptor

	if obs.Metrics != nil {
		unaryInterceptors = append(unaryInterceptors, metrics.GRPCUnaryInterceptor(obs.Metrics))
	}
	if obs.Logger != nil {
		unaryInterceptors = append(unaryInterceptors, logging.GRPCUnaryInterceptor(obs.Logger))
	}
	if obs.Tracer != nil {
		unaryInterceptors = append(unaryInterceptors, tracing.GRPCUnaryInterceptor(obs.Tracer))
	}

	if len(unaryInterceptors) > 0 {
		chained := grpc.ChainUnaryInterceptor(unaryInterceptors...)
		serverOpts = append(serverOpts, chained)
	}

	serverOpts = append(serverOpts, grpc.ForceServerCodec(JSONCodec{}))
	serverOpts = append(serverOpts, opts...)

	grpcServer := grpc.NewServer(serverOpts...)
	RegisterGRPCServer(grpcServer, NewAPI(service))

	if obs.Health != nil {
		grpc_health_v1.RegisterHealthServer(grpcServer, health.NewGRPCHealthServer(obs.Health))
	}

	return grpcServer
}

func RegisterGRPCServer(registrar grpc.ServiceRegistrar, service GRPCService) {
	registrar.RegisterService(&grpc.ServiceDesc{
		ServiceName: GRPCServiceName,
		HandlerType: (*GRPCService)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "Add", Handler: addHandler},
			{MethodName: "Remove", Handler: removeHandler},
			{MethodName: "Find", Handler: findHandler},
			{MethodName: "FindIndex", Handler: findIndexHandler},
			{MethodName: "Suggest", Handler: suggestHandler},
			{MethodName: "SuggestIndex", Handler: suggestIndexHandler},
			{MethodName: "Info", Handler: infoHandler},
			{MethodName: "Flush", Handler: flushHandler},
		},
	}, service)
}

func (JSONCodec) Name() string {
	return "acor-json"
}

func (JSONCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (JSONCodec) Unmarshal(data []byte, v interface{}) error {
	if len(data) == 0 {
		data = []byte("{}")
	}
	return json.Unmarshal(data, v)
}

func (api *API) Add(_ context.Context, req *KeywordRequest) (*CountResponse, error) {
	if req == nil {
		req = &KeywordRequest{}
	}
	count, err := api.service.Add(req.Keyword)
	if err != nil {
		return nil, grpcError(err)
	}
	return &CountResponse{Count: count}, nil
}

func (api *API) Remove(_ context.Context, req *KeywordRequest) (*CountResponse, error) {
	if req == nil {
		req = &KeywordRequest{}
	}
	count, err := api.service.Remove(req.Keyword)
	if err != nil {
		return nil, grpcError(err)
	}
	return &CountResponse{Count: count}, nil
}

func (api *API) Find(_ context.Context, req *InputRequest) (*MatchesResponse, error) {
	if req == nil {
		req = &InputRequest{}
	}
	matches, err := api.service.Find(req.Input)
	if err != nil {
		return nil, grpcError(err)
	}
	return &MatchesResponse{Matches: matches}, nil
}

func (api *API) FindIndex(_ context.Context, req *InputRequest) (*MatchIndexesResponse, error) {
	if req == nil {
		req = &InputRequest{}
	}
	matches, err := api.service.FindIndex(req.Input)
	if err != nil {
		return nil, grpcError(err)
	}
	return &MatchIndexesResponse{Matches: matches}, nil
}

func (api *API) Suggest(_ context.Context, req *InputRequest) (*MatchesResponse, error) {
	if req == nil {
		req = &InputRequest{}
	}
	matches, err := api.service.Suggest(req.Input)
	if err != nil {
		return nil, grpcError(err)
	}
	return &MatchesResponse{Matches: matches}, nil
}

func (api *API) SuggestIndex(_ context.Context, req *InputRequest) (*MatchIndexesResponse, error) {
	if req == nil {
		req = &InputRequest{}
	}
	matches, err := api.service.SuggestIndex(req.Input)
	if err != nil {
		return nil, grpcError(err)
	}
	return &MatchIndexesResponse{Matches: matches}, nil
}

func (api *API) Info(_ context.Context, _ *EmptyRequest) (*InfoResponse, error) {
	info, err := api.service.Info()
	if err != nil {
		return nil, grpcError(err)
	}
	return &InfoResponse{Keywords: info.Keywords, Nodes: info.Nodes}, nil
}

func (api *API) Flush(_ context.Context, _ *EmptyRequest) (*StatusResponse, error) {
	if err := api.service.Flush(); err != nil {
		return nil, grpcError(err)
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

func decodeKeywordRequest(w http.ResponseWriter, r *http.Request) (*KeywordRequest, bool) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return nil, false
	}
	defer closeReadCloser(r.Body)

	var req KeywordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, &ErrorResponse{Error: err.Error()})
		return nil, false
	}
	return &req, true
}

func decodeInputRequest(w http.ResponseWriter, r *http.Request) (*InputRequest, bool) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return nil, false
	}
	defer closeReadCloser(r.Body)

	var req InputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, &ErrorResponse{Error: err.Error()})
		return nil, false
	}
	return &req, true
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, &ErrorResponse{Error: "method not allowed"})
}

func writeServiceError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, &ErrorResponse{Error: status.Convert(err).Message()})
}

func writeJSON(w http.ResponseWriter, statusCode int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

func grpcError(err error) error {
	return status.Error(codes.Internal, err.Error())
}

func closeReadCloser(closer io.Closer) {
	_ = closer.Close()
}

func addHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(KeywordRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GRPCService).Add(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: GRPCMethodAdd}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(GRPCService).Add(ctx, req.(*KeywordRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func removeHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(KeywordRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GRPCService).Remove(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: GRPCMethodRemove}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(GRPCService).Remove(ctx, req.(*KeywordRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func findHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(InputRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GRPCService).Find(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: GRPCMethodFind}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(GRPCService).Find(ctx, req.(*InputRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func findIndexHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(InputRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GRPCService).FindIndex(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: GRPCMethodFindIndex}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(GRPCService).FindIndex(ctx, req.(*InputRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func suggestHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(InputRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GRPCService).Suggest(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: GRPCMethodSuggest}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(GRPCService).Suggest(ctx, req.(*InputRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func suggestIndexHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(InputRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GRPCService).SuggestIndex(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: GRPCMethodSuggestIndex}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(GRPCService).SuggestIndex(ctx, req.(*InputRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func infoHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(EmptyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GRPCService).Info(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: GRPCMethodInfo}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(GRPCService).Info(ctx, req.(*EmptyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func flushHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(EmptyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GRPCService).Flush(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: GRPCMethodFlush}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(GRPCService).Flush(ctx, req.(*EmptyRequest))
	}
	return interceptor(ctx, in, info, handler)
}
