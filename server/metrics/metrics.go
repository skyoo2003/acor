// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/skyoo2003/acor/pkg/acor"
)

type Registry struct {
	HTTPRequestsTotal        *prometheus.CounterVec
	HTTPRequestDuration      *prometheus.HistogramVec
	RedisOperationsTotal     *prometheus.CounterVec
	RedisOperationDuration   *prometheus.HistogramVec
	KeywordsTotal            prometheus.Gauge
	TrieNodesTotal           prometheus.Gauge
	VersionedBuilding        prometheus.Gauge
	VersionedServingReady    prometheus.Gauge
	VersionedRefreshFailures prometheus.Gauge
	VersionedActiveLeases    prometheus.Gauge
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
		VersionedBuilding: factory.NewGauge(
			prometheus.GaugeOpts{Namespace: namespace, Name: "versioned_building", Help: "Whether a V3 engine refresh is building"},
		),
		VersionedServingReady: factory.NewGauge(
			prometheus.GaugeOpts{Namespace: namespace, Name: "versioned_serving_ready", Help: "Whether a V3 engine is serving"},
		),
		VersionedRefreshFailures: factory.NewGauge(
			prometheus.GaugeOpts{Namespace: namespace, Name: "versioned_refresh_failures", Help: "Cumulative V3 refresh failures observed by this instance"},
		),
		VersionedActiveLeases: factory.NewGauge(
			prometheus.GaugeOpts{Namespace: namespace, Name: "versioned_active_leases", Help: "Number of V3 leases held by this instance"},
		),
	}
}

// UpdateVersionedStatus records bounded V3 state without exposing version tokens
// or dictionary contents as metric labels.
func (r *Registry) UpdateVersionedStatus(status *acor.VersionedStatus) {
	if status == nil {
		return
	}
	r.VersionedBuilding.Set(boolGauge(status.Building))
	r.VersionedServingReady.Set(boolGauge(status.ServingVersion != "" && status.LastError == ""))
	r.VersionedRefreshFailures.Set(float64(status.RefreshFailures))
	r.VersionedActiveLeases.Set(float64(status.ActiveLeases))
}

func boolGauge(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
