package acor

import (
	"encoding/json"
)

// Schema version constants define the storage format used by the automaton.
const (
	// SchemaV1 represents the legacy V1 schema version.
	// V1 uses multiple Redis keys: one per prefix, suffix, output, and node.
	// Suitable for small collections but creates many keys.
	SchemaV1 = 1
	// SchemaV2 represents the current V2 schema version (default).
	// V2 consolidates data into 3 Redis keys using JSON and Lua scripts.
	// Recommended for most use cases due to better performance and fewer keys.
	SchemaV2 = 2
)

// MigrationOptions configures the V1 to V2 migration behavior.
// Use with MigrateV1ToV2 to upgrade legacy collections to the optimized schema.
type MigrationOptions struct {
	// DryRun if true, simulates the migration without making changes.
	// Useful for previewing what would be migrated.
	DryRun bool
	// KeepOldKeys if true, preserves V1 keys after migration.
	// Set to false (default) to delete V1 keys after successful migration.
	KeepOldKeys bool
	// Quiet if true, suppresses progress output.
	Quiet bool
	// Progress is an optional callback for migration progress updates.
	// Called with (done_steps, total_steps, message) for each migration phase.
	Progress func(done, total int, message string)
}

// DefaultMigrationOptions returns migration options with safe defaults:
// DryRun=false, KeepOldKeys=false, Quiet=false, Progress=nil.
func DefaultMigrationOptions() *MigrationOptions {
	return &MigrationOptions{}
}

// MigrationResult contains the results of a schema migration from V1 to V2.
// It provides detailed statistics about the migration process.
type MigrationResult struct {
	// Status indicates the migration outcome: "success", "error", or "dry-run".
	Status string `json:"status"`
	// Collection is the name of the migrated collection.
	Collection string `json:"collection"`
	// FromSchema is the source schema version (always 1).
	FromSchema int `json:"from_schema"`
	// ToSchema is the target schema version (always 2).
	ToSchema int `json:"to_schema"`
	// DryRun indicates whether this was a simulation.
	DryRun bool `json:"dry_run"`
	// Keywords is the number of keywords migrated.
	Keywords int `json:"keywords"`
	// Prefixes is the number of trie prefixes migrated.
	Prefixes int `json:"prefixes"`
	// OutputsKeys is the number of output state keys migrated.
	OutputsKeys int `json:"outputs_keys"`
	// NodesKeys is the number of node keys migrated.
	NodesKeys int `json:"nodes_keys"`
	// KeysBefore is the number of Redis keys before migration (V1 schema).
	KeysBefore int `json:"keys_before"`
	// KeysAfter is the number of Redis keys after migration (always 3 for V2).
	KeysAfter int `json:"keys_after"`
	// DurationMs is the migration duration in milliseconds.
	DurationMs int64 `json:"duration_ms"`
	// RolledBack indicates if the migration was rolled back due to error.
	RolledBack bool `json:"rolled_back"`
	// ErrorMessage contains the error message if Status is "error".
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
