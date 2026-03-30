package acor

import "context"

// operations defines the core Aho-Corasick methods shared by V1 and V2
// implementations. This unexported interface is the Strategy pattern
// foundation: AhoCorasick dispatches through this interface instead of
// explicit if/else version checks.
//
// All methods accept a context.Context for cancellation and timeout propagation.
//
// Note: debug() is intentionally excluded — it is observability-only, not a
// core operation.
type operations interface {
	add(ctx context.Context, keyword string) (int, error)
	remove(ctx context.Context, keyword string) (int, error)
	find(ctx context.Context, text string) ([]string, error)
	findIndex(ctx context.Context, text string) (map[string][]int, error)
	suggest(ctx context.Context, input string) ([]string, error)
	suggestIndex(ctx context.Context, input string) (map[string][]int, error)
	flush(ctx context.Context) error
	info(ctx context.Context) (*AhoCorasickInfo, error)
}
