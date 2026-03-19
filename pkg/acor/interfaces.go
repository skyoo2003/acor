package acor

import "context"

type Z struct {
	Score  float64
	Member string
}

type KVStorage interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}) error
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	HSet(ctx context.Context, key string, values ...interface{}) error
	SAdd(ctx context.Context, key string, members ...interface{}) error
	SMembers(ctx context.Context, key string) ([]string, error)
	SRem(ctx context.Context, key string, members ...interface{}) error
	SCard(ctx context.Context, key string) (int64, error)
	SIsMember(ctx context.Context, key, member string) (bool, error)
	ZAdd(ctx context.Context, key string, members ...*Z) error
	ZRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	ZRank(ctx context.Context, key, member string) (int64, error)
	ZScore(ctx context.Context, key, member string) (float64, error)
	ZCard(ctx context.Context, key string) (int64, error)
	ZRem(ctx context.Context, key string, members ...interface{}) error
	Del(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, keys ...string) (int64, error)
	TxPipelined(ctx context.Context, fn func(Pipeliner) error) error
	Close() error
}

type Pipeliner interface {
	SAdd(ctx context.Context, key string, members ...interface{}) error
	HSet(ctx context.Context, key string, values ...interface{}) error
	ZAdd(ctx context.Context, key string, members ...*Z) error
	Del(ctx context.Context, keys ...string) error
}

type Matcher interface {
	Find(text string) ([]string, error)
	FindIndex(text string) (map[string][]int, error)
	FindMany(texts []string) (map[string][]string, error)
	FindParallel(text string, opts *ParallelOptions) ([]string, error)
}

type Indexer interface {
	Add(keyword string) (int, error)
	AddMany(keywords []string, opts *BatchOptions) (*BatchResult, error)
	Remove(keyword string) (int, error)
	RemoveMany(keywords []string) (*BatchResult, error)
}
