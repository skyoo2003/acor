package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry(prometheus.NewRegistry())
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
	if reg.HTTPRequestsTotal == nil {
		t.Error("expected HTTPRequestsTotal to be initialized")
	}
	if reg.HTTPRequestDuration == nil {
		t.Error("expected HTTPRequestDuration to be initialized")
	}
	if reg.GRPCRequestsTotal == nil {
		t.Error("expected GRPCRequestsTotal to be initialized")
	}
	if reg.GRPCRequestDuration == nil {
		t.Error("expected GRPCRequestDuration to be initialized")
	}
	if reg.RedisOperationsTotal == nil {
		t.Error("expected RedisOperationsTotal to be initialized")
	}
	if reg.RedisOperationDuration == nil {
		t.Error("expected RedisOperationDuration to be initialized")
	}
	if reg.KeywordsTotal == nil {
		t.Error("expected KeywordsTotal to be initialized")
	}
	if reg.TrieNodesTotal == nil {
		t.Error("expected TrieNodesTotal to be initialized")
	}
}
