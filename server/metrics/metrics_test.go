// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/skyoo2003/acor/pkg/acor"
)

type versionedStatusSource struct {
	status acor.VersionedStatus
	calls  int
}

func (s *versionedStatusSource) Status() acor.VersionedStatus {
	s.calls++
	return s.status
}

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
	if reg.GRPCServer == nil {
		t.Error("expected GRPCServer to be initialized")
	}
	if reg.VersionedBuilding == nil || reg.VersionedServingReady == nil || reg.VersionedRefreshFailures == nil || reg.VersionedActiveLeases == nil {
		t.Error("expected V3 metrics to be initialized")
	}
}

func TestVersionedStatusCollectorRefreshesOnGather(t *testing.T) {
	promReg := prometheus.NewRegistry()
	reg := NewRegistry(promReg)
	source := &versionedStatusSource{status: acor.VersionedStatus{Building: true, ServingVersion: "v", RefreshFailures: 2, ActiveLeases: 3}}
	if err := reg.RegisterVersionedStatusSource("moderation-prod", source); err != nil {
		t.Fatal(err)
	}

	families, err := promReg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 {
		t.Fatalf("Status calls = %d, want 1", source.calls)
	}
	if got := gaugeValue(t, families, "acor_versioned_refresh_failures", "moderation-prod"); got != 2 {
		t.Fatalf("refresh failures = %v, want 2", got)
	}

	source.status = acor.VersionedStatus{ServingVersion: "next", ActiveLeases: 7}
	families, err = promReg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if source.calls != 2 {
		t.Fatalf("Status calls after second gather = %d, want 2", source.calls)
	}
	if got := gaugeValue(t, families, "acor_versioned_active_leases", "moderation-prod"); got != 7 {
		t.Fatalf("active leases = %v, want 7", got)
	}
}

func TestVersionedStatusCollectorSeparatesCollections(t *testing.T) {
	promReg := prometheus.NewRegistry()
	reg := NewRegistry(promReg)
	first := &versionedStatusSource{status: acor.VersionedStatus{ServingVersion: "v", ActiveLeases: 1}}
	second := &versionedStatusSource{status: acor.VersionedStatus{ServingVersion: "v", ActiveLeases: 2}}
	if err := reg.RegisterVersionedStatusSource("first", first); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterVersionedStatusSource("second", second); err != nil {
		t.Fatal(err)
	}
	families, err := promReg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if got := gaugeValue(t, families, "acor_versioned_active_leases", "first"); got != 1 {
		t.Fatalf("first active leases = %v, want 1", got)
	}
	if got := gaugeValue(t, families, "acor_versioned_active_leases", "second"); got != 2 {
		t.Fatalf("second active leases = %v, want 2", got)
	}
}

func TestRegisterVersionedStatusSourceValidatesBinding(t *testing.T) {
	reg := NewRegistry(prometheus.NewRegistry())
	source := &versionedStatusSource{}
	for _, label := range []string{"", "has space", "line\nbreak"} {
		if err := reg.RegisterVersionedStatusSource(label, source); err == nil {
			t.Fatalf("RegisterVersionedStatusSource(%q) succeeded", label)
		}
	}
	if err := reg.RegisterVersionedStatusSource("stable", source); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterVersionedStatusSource("stable", source); err != nil {
		t.Fatalf("same source registration: %v", err)
	}
	if err := reg.RegisterVersionedStatusSource("stable", &versionedStatusSource{}); err == nil {
		t.Fatal("different source registration succeeded")
	}
}

func gaugeValue(t *testing.T, families []*dto.MetricFamily, name, collection string) float64 {
	t.Helper()
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "collection" && label.GetValue() == collection {
					return metric.GetGauge().GetValue()
				}
			}
		}
	}
	t.Fatalf("metric %s{collection=%q} not found", name, collection)
	return 0
}
