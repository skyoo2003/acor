// SPDX-License-Identifier: Apache-2.0

package acor

// Schema version constants define the storage format used by the automaton.
const (
	// SchemaV1 represents the legacy V1 schema version.
	// V1 spreads a collection over many Redis keys: three fixed ones holding the
	// keyword set, the prefix index, and the suffix index, plus one key per trie
	// state that carries output and one per keyword, so the key count grows with
	// the dictionary.
	//
	// V1 collections are read-only as of v1.5.0: Find, FindIndex, Suggest, Info, and
	// Flush work, MigrateV1ToV2 converts a collection in place, and Add and Remove
	// return ErrV1ReadOnly. Selecting V1 for a fresh collection therefore produces one
	// that can never gain a keyword — it exists so collections written by an earlier
	// release can still be read and migrated.
	//
	// It gains no features either: Preset engines and EnableCache both require V2.
	//
	// Deprecated: use SchemaV2 (the default) and migrate existing collections with
	// MigrateV1ToV2. The read path stays for the whole v1 line and is removed no
	// earlier than v2.
	SchemaV1 = 1
	// SchemaV2 represents the current V2 schema version (default).
	// V2 consolidates the whole collection into a fixed set of Redis keys, written
	// as JSON through Lua scripts, so the key count no longer grows with the
	// dictionary. Recommended for every use case.
	//
	// That set is at most three keys — {name}:trie, {name}:outputs and
	// {name}:nodes — but a collection rarely holds all three. A fresh one has only
	// :trie, and adding keywords brings up :outputs. Only MigrateV1ToV2 writes
	// :nodes; a collection built natively by Add never has it. Size a key-count
	// budget on three and expect to see fewer.
	SchemaV2 = 2
)

// MigrationOptions configures the V1 to V2 migration behavior.
// Use with MigrateV1ToV2 to upgrade legacy collections to the optimized schema.
type MigrationOptions struct {
	// DryRun if true, counts what would be migrated and returns before writing
	// anything. Useful for previewing the work.
	//
	// It is not entirely without effect on Redis: the migration lock is taken and
	// released around a dry run exactly as around a real one, so a dry run and a
	// real migration of the same collection still exclude each other with
	// ErrMigrationInProg.
	DryRun bool
	// KeepOldKeys if true, preserves V1 keys after migration.
	// Set to false (default) to delete V1 keys after successful migration.
	KeepOldKeys bool
	// Progress is an optional callback for migration progress updates.
	// Called with (done_steps, total_steps, message) as each migration phase
	// starts. total is always 5.
	//
	// A dry run stops after the four collection phases, so it reports 4/5 and
	// never calls back with done == total. Drive a progress bar off done/total
	// rather than waiting for a final call.
	Progress func(done, total int, message string)
}

// DefaultMigrationOptions returns migration options with safe defaults:
// DryRun=false, KeepOldKeys=false, Progress=nil.
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
	// KeysBefore estimates how many Redis keys the V1 collection occupied, as
	// Prefixes + Keywords + 2. It is not a count: the real total is the three fixed
	// V1 keys plus OutputsKeys plus NodesKeys, and only prefixes that carry output
	// and keywords that own nodes have a key at all. Expect it to read high on a
	// dictionary with many shared prefixes. Use it to convey the scale of the
	// reduction, not to reconcile against DBSIZE.
	KeysBefore int `json:"keys_before"`
	// KeysAfter is the size of the V2 key set, and is always 3 — a constant, not a
	// count of what the migration left behind. The migration writes :nodes and
	// :outputs only when there is something to put in them, and with
	// KeepOldKeys the V1 keys are still there too, so the collection can hold
	// either fewer keys than this or many more. See SchemaV2.
	KeysAfter int `json:"keys_after"`
	// DurationMs is the migration duration in milliseconds. Set on the dry-run and
	// success paths only: when Status is "error" it stays 0 rather than reporting
	// how long the attempt ran for.
	DurationMs int64 `json:"duration_ms"`
	// RolledBack is always false.
	//
	// Nothing sets it. A migration that fails partway cleans up its temporary keys
	// and leaves the V1 data in place — there is no committed state to undo, so no
	// rollback ever happens and the field has nothing to report. It is retained
	// because it is part of the frozen v1 surface and of the JSON shape. Treat
	// Status == "error" as the signal that a migration did not take effect.
	RolledBack bool `json:"rolled_back"`
	// ErrorMessage contains the error message if Status is "error".
	ErrorMessage string `json:"error,omitempty"`
}

// Stats returns the six size counters as a map, keyed by the same names their
// JSON tags use: keywords, prefixes, outputs_keys, nodes_keys, keys_before, and
// keys_after.
//
// It is a projection, not the whole result: the outcome fields — Status,
// ErrorMessage, DryRun, RolledBack, DurationMs, Collection, and the schema
// numbers — are not in it, so a caller cannot tell success from a dry run or a
// failure by reading the map. Read those off the MigrationResult, or marshal the
// result itself, which carries all thirteen.
func (r *MigrationResult) Stats() map[string]interface{} {
	return map[string]interface{}{
		fieldKeywords:  r.Keywords,
		fieldPrefixes:  r.Prefixes,
		"outputs_keys": r.OutputsKeys,
		"nodes_keys":   r.NodesKeys,
		"keys_before":  r.KeysBefore,
		"keys_after":   r.KeysAfter,
	}
}
