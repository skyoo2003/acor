package acor

import (
	"encoding/json"
)

const (
	SchemaV1 = 1
	SchemaV2 = 2
)

type MigrationOptions struct {
	BatchSize   int
	DryRun      bool
	KeepOldKeys bool
	Quiet       bool
	Progress    func(done, total int, message string)
}

func DefaultMigrationOptions() *MigrationOptions {
	return &MigrationOptions{
		BatchSize: 1000,
	}
}

type MigrationResult struct {
	Status       string `json:"status"`
	Collection   string `json:"collection"`
	FromSchema   int    `json:"from_schema"`
	ToSchema     int    `json:"to_schema"`
	DryRun       bool   `json:"dry_run"`
	Keywords     int    `json:"keywords"`
	Prefixes     int    `json:"prefixes"`
	OutputsKeys  int    `json:"outputs_keys"`
	NodesKeys    int    `json:"nodes_keys"`
	KeysBefore   int    `json:"keys_before"`
	KeysAfter    int    `json:"keys_after"`
	DurationMs   int64  `json:"duration_ms"`
	RolledBack   bool   `json:"rolled_back"`
	ErrorMessage string `json:"error,omitempty"`
}

func (r *MigrationResult) MarshalJSON() ([]byte, error) {
	type Alias MigrationResult
	return json.Marshal((*Alias)(r))
}

func (r *MigrationResult) UnmarshalJSON(data []byte) error {
	type Alias MigrationResult
	return json.Unmarshal(data, (*Alias)(r))
}

func (r *MigrationResult) Stats() map[string]interface{} {
	return map[string]interface{}{
		"keywords":     r.Keywords,
		"prefixes":     r.Prefixes,
		"outputs_keys": r.OutputsKeys,
		"nodes_keys":   r.NodesKeys,
		"keys_before":  r.KeysBefore,
		"keys_after":   r.KeysAfter,
	}
}
