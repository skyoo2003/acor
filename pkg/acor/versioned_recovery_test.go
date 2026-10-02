// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestVersionedRefreshRecoversWithoutNewGeneration(t *testing.T) {
	ctx := context.Background()
	backend := miniredis.RunT(t)
	v := openV3Test(t, backend, "recovery")
	before := v.Status()
	backend.SetError("ERR temporary outage")
	if err := v.refresh(ctx); err == nil {
		t.Fatal("refresh succeeded during outage")
	}
	failed := v.Status()
	if failed.LastError == "" || failed.RefreshFailures == 0 || failed.LastRefreshFailure.IsZero() {
		t.Fatalf("missing failure diagnostics: %+v", failed)
	}
	backend.SetError("")
	if err := v.refresh(ctx); err != nil {
		t.Fatal(err)
	}
	recovered := v.Status()
	if recovered.LastError != "" || recovered.FailedShard != -1 {
		t.Fatalf("healthy unchanged generation retains error: %+v", recovered)
	}
	if recovered.ServingVersion != before.ServingVersion || recovered.CompletedBuilds != before.CompletedBuilds {
		t.Fatalf("recovery rebuilt or changed the serving generation: %+v", recovered)
	}
	if recovered.RefreshFailures < failed.RefreshFailures || recovered.LastRefreshFailure.Before(failed.LastRefreshFailure) {
		t.Fatalf("recovery discarded failure history: %+v", recovered)
	}
	if !recovered.LastRefreshSuccess.Equal(before.LastRefreshSuccess) {
		t.Fatalf("recovery changed the engine installation timestamp: %+v", recovered)
	}
}
