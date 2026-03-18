package acor

import (
	"encoding/json"
)

const (
	// SchemaV1 represents the legacy V1 schema version.
	SchemaV1 = 1
	// SchemaV2 represents the current V2 schema version.
	SchemaV2 = 2
)

// MigrationOptions configures the V1 to V2 migration behavior.
type MigrationOptions struct {
	DryRun      bool
	KeepOldKeys bool
	Quiet       bool
	Progress    func(done, total int, message string)
}

// DefaultMigrationOptions returns default migration options.
func DefaultMigrationOptions() *MigrationOptions {
	return &MigrationOptions{}
}

// MigrationResult contains the results of a schema migration.
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

// MarshalJSON implements json.Marshaler for MigrationResult.
func (r *MigrationResult) MarshalJSON() ([]byte, error) {
	type Alias MigrationResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler for MigrationResult.
func (r *MigrationResult) UnmarshalJSON(data []byte) error {
	type Alias MigrationResult
	return json.Unmarshal(data, (*Alias)(r))
}

// Stats returns migration statistics as a map.
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
