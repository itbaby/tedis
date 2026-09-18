// Package scanner provides the non-blocking key discovery engine: a pure
// SCAN iterator (cursor-based, cancellable, batched) and an incremental
// prefix tree that mirrors Redis' ":" namespace convention (Medis key tree).
package scanner

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// DefaultCount is the SCAN COUNT hint used when none is configured.
const DefaultCount = 500

// Scan iterates keys via SCAN (never KEYS), calling fn once per batch.
// Returning false from fn stops iteration. Cancel ctx to abort a scan in
// flight; the redis call then fails with context.Canceled.
func Scan(ctx context.Context, client redis.UniversalClient, match string, count int, fn func(keys []string) bool) error {
	if count <= 0 {
		count = DefaultCount
	}
	if match == "" {
		match = "*"
	}
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, match, int64(count)).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 && !fn(keys) {
			return nil
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}
