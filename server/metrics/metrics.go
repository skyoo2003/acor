// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"fmt"
	"reflect"
	"sync"

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
	VersionedBuilding        *prometheus.GaugeVec
	VersionedServingReady    *prometheus.GaugeVec
	VersionedRefreshFailures *prometheus.GaugeVec
	VersionedActiveLeases    *prometheus.GaugeVec
	versionedStatus          *versionedStatusCollector
	// GRPCServer holds the standard grpc_server_* Prometheus metrics. Install it
	// on a gRPC server via its UnaryServerInterceptor().
	GRPCServer *grpcprom.ServerMetrics
}

// VersionedStatusSource supplies the local V3 state used by the versioned
// Prometheus metrics. Status must not perform Redis I/O.
type VersionedStatusSource interface {
	Status() acor.VersionedStatus
}

type versionedStatusCollector struct {
	building        *prometheus.GaugeVec
	servingReady    *prometheus.GaugeVec
	refreshFailures *prometheus.GaugeVec
	activeLeases    *prometheus.GaugeVec

	mu      sync.RWMutex
	sources map[string]VersionedStatusSource
}

func NewRegistry(registerer prometheus.Registerer) *Registry {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	factory := promauto.With(registerer)
	namespace := "acor"

	grpcServer := grpcprom.NewServerMetrics(grpcprom.WithServerHandlingTimeHistogram())
	registerer.MustRegister(grpcServer)

	versioned := &versionedStatusCollector{sources: make(map[string]VersionedStatusSource)}
	versioned.building = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Namespace: namespace, Name: "versioned_building", Help: "Whether a V3 engine refresh is building"},
		[]string{"collection"},
	)
	versioned.servingReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Namespace: namespace, Name: "versioned_serving_ready", Help: "Whether a V3 engine is serving"},
		[]string{"collection"},
	)
	versioned.refreshFailures = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Namespace: namespace, Name: "versioned_refresh_failures", Help: "Cumulative V3 refresh failures observed by this instance"},
		[]string{"collection"},
	)
	versioned.activeLeases = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Namespace: namespace, Name: "versioned_active_leases", Help: "Number of V3 leases held by this instance"},
		[]string{"collection"},
	)
	registerer.MustRegister(versioned)

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
		VersionedBuilding:        versioned.building,
		VersionedServingReady:    versioned.servingReady,
		VersionedRefreshFailures: versioned.refreshFailures,
		VersionedActiveLeases:    versioned.activeLeases,
		versionedStatus:          versioned,
	}
}

// RegisterVersionedStatusSource binds one stable, operator-supplied collection
// label to a V3 status source. Re-registering the same source is safe; binding
// a different source to an existing label is rejected.
func (r *Registry) RegisterVersionedStatusSource(collection string, source VersionedStatusSource) error {
	if r == nil || r.versionedStatus == nil {
		return fmt.Errorf("metrics: versioned registry is required")
	}
	if !validCollectionLabel(collection) {
		return fmt.Errorf("metrics: invalid collection label %q", collection)
	}
	if source == nil || (reflect.ValueOf(source).Kind() == reflect.Ptr && reflect.ValueOf(source).IsNil()) {
		return fmt.Errorf("metrics: versioned status source is required")
	}

	c := r.versionedStatus
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.sources[collection]; ok {
		if sameStatusSource(existing, source) {
			return nil
		}
		return fmt.Errorf("metrics: collection label %q is already bound", collection)
	}
	c.sources[collection] = source
	return nil
}

func validCollectionLabel(label string) bool {
	if label == "" || len(label) > 64 {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == ':' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func sameStatusSource(a, b VersionedStatusSource) bool {
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	return ta == tb && ta.Comparable() && a == b
}

func (c *versionedStatusCollector) Describe(ch chan<- *prometheus.Desc) {
	c.building.Describe(ch)
	c.servingReady.Describe(ch)
	c.refreshFailures.Describe(ch)
	c.activeLeases.Describe(ch)
}

func (c *versionedStatusCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	sources := make(map[string]VersionedStatusSource, len(c.sources))
	for collection, source := range c.sources {
		sources[collection] = source
	}
	c.mu.RUnlock()

	for collection, source := range sources {
		status := source.Status()
		c.building.WithLabelValues(collection).Set(boolGauge(status.Building))
		c.servingReady.WithLabelValues(collection).Set(boolGauge(status.ServingVersion != "" && status.LastError == ""))
		c.refreshFailures.WithLabelValues(collection).Set(float64(status.RefreshFailures))
		c.activeLeases.WithLabelValues(collection).Set(float64(status.ActiveLeases))
	}
	c.building.Collect(ch)
	c.servingReady.Collect(ch)
	c.refreshFailures.Collect(ch)
	c.activeLeases.Collect(ch)
}

func boolGauge(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
