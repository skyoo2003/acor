// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Registry struct {
	HTTPRequestsTotal      *prometheus.CounterVec
	HTTPRequestDuration    *prometheus.HistogramVec
	RedisOperationsTotal   *prometheus.CounterVec
	RedisOperationDuration *prometheus.HistogramVec
	KeywordsTotal          prometheus.Gauge
	TrieNodesTotal         prometheus.Gauge
	// GRPCServer holds the standard grpc_server_* Prometheus metrics. Install it
	// on a gRPC server via its UnaryServerInterceptor().
	GRPCServer *grpcprom.ServerMetrics
}

func NewRegistry(registerer prometheus.Registerer) *Registry {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	factory := promauto.With(registerer)
	namespace := "acor"

	grpcServer := grpcprom.NewServerMetrics(grpcprom.WithServerHandlingTimeHistogram())
	registerer.MustRegister(grpcServer)

	return &Registry{
		GRPCServer: grpcServer,
		HTTPRequestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		HTTPRequestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request latency in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
		RedisOperationsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "redis_operations_total",
				Help:      "Total number of Redis operations",
			},
			[]string{"operation", "status"},
		),
		RedisOperationDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "redis_operation_duration_seconds",
				Help:      "Redis operation latency in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"operation"},
		),
		KeywordsTotal: factory.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "keywords_total",
				Help:      "Number of registered keywords",
			},
		),
		TrieNodesTotal: factory.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "trie_nodes_total",
				Help:      "Number of trie nodes",
			},
		),
	}
}
