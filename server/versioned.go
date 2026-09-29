// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"

	"github.com/skyoo2003/acor/pkg/acor"
	servermetrics "github.com/skyoo2003/acor/server/metrics"
)

// VersionedStatusResponse exposes V3 serving state without Redis keys,
// collection names, or the underlying error text.
type VersionedStatusResponse struct {
	Status             string       `json:"status"`
	ActiveVersion      acor.Version `json:"active_version"`
	ServingVersion     acor.Version `json:"serving_version"`
	Building           bool         `json:"building"`
	LastRefreshSuccess string       `json:"last_refresh_success,omitempty"`
	LastRefreshFailure string       `json:"last_refresh_failure,omitempty"`
	RefreshFailures    uint64       `json:"refresh_failures"`
	ActiveLeases       int          `json:"active_leases"`
}

// VersionedStatusSource is implemented by acor.VersionedCollection and makes
// the handler straightforward to test without a Redis server.
type VersionedStatusSource interface {
	Status() acor.VersionedStatus
}

// NewVersionedHTTPHandler returns the read-only V3 status and health surface.
// It is separate from NewHTTPHandler because V3's versioned write contract does
// not match the legacy Service interface.
func NewVersionedHTTPHandler(collection VersionedStatusSource, registries ...*servermetrics.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		status := collection.Status()
		if status.ServingVersion == "" || status.LastError != "" {
			writeVersionedStatus(w, http.StatusServiceUnavailable, &status, registries...)
			return
		}
		writeVersionedStatus(w, http.StatusOK, &status, registries...)
	})
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		status := collection.Status()
		writeVersionedStatus(w, http.StatusOK, &status, registries...)
	})
	return mux
}

func writeVersionedStatus(w http.ResponseWriter, code int, status *acor.VersionedStatus, registries ...*servermetrics.Registry) {
	for _, registry := range registries {
		if registry != nil {
			registry.UpdateVersionedStatus(status)
		}
	}
	response := VersionedStatusResponse{
		Status:          "ok",
		ActiveVersion:   status.ActiveVersion,
		ServingVersion:  status.ServingVersion,
		Building:        status.Building,
		RefreshFailures: status.RefreshFailures,
		ActiveLeases:    status.ActiveLeases,
	}
	if status.LastError != "" || status.ServingVersion == "" {
		response.Status = "degraded"
	}
	if !status.LastRefreshSuccess.IsZero() {
		response.LastRefreshSuccess = status.LastRefreshSuccess.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	}
	if !status.LastRefreshFailure.IsZero() {
		response.LastRefreshFailure = status.LastRefreshFailure.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(response)
}
