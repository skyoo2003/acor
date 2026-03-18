package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Registry struct {
	HTTPRequestsTotal      *prometheus.CounterVec
	HTTPRequestDuration    *prometheus.HistogramVec
	GRPCRequestsTotal      *prometheus.CounterVec
	GRPCRequestDuration    *prometheus.HistogramVec
	RedisOperationsTotal   *prometheus.CounterVec
	RedisOperationDuration *prometheus.HistogramVec
	KeywordsTotal          prometheus.Gauge
	TrieNodesTotal         prometheus.Gauge
}

func NewRegistry(registerer prometheus.Registerer) *Registry {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	factory := promauto.With(registerer)
	namespace := "acor"

	return &Registry{
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
		GRPCRequestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "grpc_requests_total",
				Help:      "Total number of gRPC requests",
			},
			[]string{"method", "status"},
		),
		GRPCRequestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "grpc_request_duration_seconds",
				Help:      "gRPC request latency in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method"},
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
