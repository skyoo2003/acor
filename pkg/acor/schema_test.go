// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"encoding/json"
	"testing"
)

func TestSchemaConstants(t *testing.T) {
	if SchemaV1 != 1 {
		t.Errorf("SchemaV1 = %d, want 1", SchemaV1)
	}
	if SchemaV2 != 2 {
		t.Errorf("SchemaV2 = %d, want 2", SchemaV2)
	}
}

func TestMigrationOptionsDefaults(t *testing.T) {
	opts := DefaultMigrationOptions()
	if opts.DryRun {
		t.Error("DryRun should be false by default")
	}
	if opts.KeepOldKeys {
		t.Error("KeepOldKeys should be false by default")
	}
	if opts.Progress != nil {
		t.Error("Progress should be nil by default")
	}
}

func TestMigrationResultJSON(t *testing.T) {
	result := &MigrationResult{
		Status:      "success",
		Collection:  "test",
		FromSchema:  SchemaV1,
		ToSchema:    SchemaV2,
		DryRun:      false,
		Keywords:    100,
		Prefixes:    300,
		OutputsKeys: 300,
		NodesKeys:   100,
		KeysBefore:  703,
		KeysAfter:   3,
		DurationMs:  500,
		RolledBack:  false,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed MigrationResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed.Status != result.Status {
		t.Errorf("Status = %s, want %s", parsed.Status, result.Status)
	}
}
