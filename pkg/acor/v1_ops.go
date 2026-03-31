package acor

import "context"

func (ac *AhoCorasick) addV1(ctx context.Context, keyword string) (int, error) {
	return ac.ops.add(ctx, keyword)
}

func (ac *AhoCorasick) removeV1(ctx context.Context, keyword string) (int, error) {
	return ac.ops.remove(ctx, keyword)
}

func (ac *AhoCorasick) findV1(ctx context.Context, text string) ([]string, error) {
	return ac.ops.find(ctx, text)
}

func (ac *AhoCorasick) findIndexV1(ctx context.Context, text string) (map[string][]int, error) {
	return ac.ops.findIndex(ctx, text)
}

func (ac *AhoCorasick) flushV1(ctx context.Context) error {
	return ac.ops.flush(ctx)
}

func (ac *AhoCorasick) infoV1(ctx context.Context) (*AhoCorasickInfo, error) {
	return ac.ops.info(ctx)
}

func (ac *AhoCorasick) suggestV1(ctx context.Context, input string) ([]string, error) {
	return ac.ops.suggest(ctx, input)
}

func (ac *AhoCorasick) suggestIndexV1(ctx context.Context, input string) (map[string][]int, error) {
	return ac.ops.suggestIndex(ctx, input)
}
