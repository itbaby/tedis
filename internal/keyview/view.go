// Package keyview loads and mutates values of every Redis key type. Pure
// data in/out so it is unit-testable without the UI (miniredis).
//
// Note on TTL: SET overwrites TTL. Callers that edit strings must re-apply
// EXPIRE afterwards (the app layer has the TTL from KeyMeta).
package keyview

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// PageLen is the item count per page for cursor/offset pagination.
const PageLen = 100

// ---- string --------------------------------------------------------------

// LoadString returns the raw value of a string key.
func LoadString(ctx context.Context, rdb redis.Cmdable, key string) (string, error) {
	return rdb.Get(ctx, key).Result()
}

// SaveString overwrites a string key.
func SaveString(ctx context.Context, rdb redis.Cmdable, key, val string) error {
	return rdb.Set(ctx, key, val, 0).Err()
}

// ---- hash ----------------------------------------------------------------

// HashItem is one field/value pair.
type HashItem struct {
	Field, Value string
}

// LoadHash returns one HSCAN page plus the next cursor (0 = end).
func LoadHash(ctx context.Context, rdb redis.Cmdable, key string, cursor uint64, count int) (uint64, []HashItem, error) {
	if count <= 0 {
		count = PageLen
	}
	keys, next, err := rdb.HScan(ctx, key, cursor, "", int64(count)).Result()
	if err != nil {
		return 0, nil, err
	}
	items := make([]HashItem, 0, len(keys)/2)
	for i := 0; i+1 < len(keys); i += 2 {
		items = append(items, HashItem{Field: keys[i], Value: keys[i+1]})
	}
	return next, items, nil
}

func SaveHashField(ctx context.Context, rdb redis.Cmdable, key, field, val string) error {
	return rdb.HSet(ctx, key, field, val).Err()
}

func DelHashField(ctx context.Context, rdb redis.Cmdable, key, field string) error {
	return rdb.HDel(ctx, key, field).Err()
}

// ---- set -----------------------------------------------------------------

// LoadSet returns one SSCAN page plus the next cursor.
func LoadSet(ctx context.Context, rdb redis.Cmdable, key string, cursor uint64, count int) (uint64, []string, error) {
	if count <= 0 {
		count = PageLen
	}
	k, n, e := rdb.SScan(ctx, key, cursor, "", int64(count)).Result()
	return n, k, e
}

func SaveSetMember(ctx context.Context, rdb redis.Cmdable, key, member string) error {
	return rdb.SAdd(ctx, key, member).Err()
}

func DelSetMember(ctx context.Context, rdb redis.Cmdable, key, member string) error {
	return rdb.SRem(ctx, key, member).Err()
}

// ---- sorted set ----------------------------------------------------------

// ZSetItem is one member/score pair.
type ZSetItem struct {
	Member string
	Score  float64
}

// LoadZSet returns one ZSCAN page plus the next cursor.
func LoadZSet(ctx context.Context, rdb redis.Cmdable, key string, cursor uint64, count int) (uint64, []ZSetItem, error) {
	if count <= 0 {
		count = PageLen
	}
	keys, next, err := rdb.ZScan(ctx, key, cursor, "", int64(count)).Result()
	if err != nil {
		return 0, nil, err
	}
	items := make([]ZSetItem, 0, len(keys)/2)
	for i := 0; i+1 < len(keys); i += 2 {
		score, _ := strconv.ParseFloat(keys[i+1], 64)
		items = append(items, ZSetItem{Member: keys[i], Score: score})
	}
	return next, items, nil
}

func SaveZSetMember(ctx context.Context, rdb redis.Cmdable, key, member string, score float64) error {
	return rdb.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
}

func DelZSetMember(ctx context.Context, rdb redis.Cmdable, key, member string) error {
	return rdb.ZRem(ctx, key, member).Err()
}

// ---- list ----------------------------------------------------------------

// ListItem is one index/value row.
type ListItem struct {
	Index int64
	Value string
}

// LoadList returns one LRANGE page and the total length.
func LoadList(ctx context.Context, rdb redis.Cmdable, key string, start int64, count int) ([]ListItem, int64, error) {
	if count <= 0 {
		count = PageLen
	}
	total, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		return nil, 0, err
	}
	stop := start + int64(count) - 1
	vals, err := rdb.LRange(ctx, key, start, stop).Result()
	if err != nil {
		return nil, 0, err
	}
	items := make([]ListItem, len(vals))
	for i, v := range vals {
		items[i] = ListItem{Index: start + int64(i), Value: v}
	}
	return items, total, nil
}

// SaveListItem overwrites the value at index (LSET).
func SaveListItem(ctx context.Context, rdb redis.Cmdable, key string, index int64, val string) error {
	return rdb.LSet(ctx, key, index, val).Err()
}

// AddListItem pushes a value; left=true → LPUSH, else RPUSH.
func AddListItem(ctx context.Context, rdb redis.Cmdable, key, val string, left bool) error {
	if left {
		return rdb.LPush(ctx, key, val).Err()
	}
	return rdb.RPush(ctx, key, val).Err()
}

// DelListItem removes the first occurrence of val (LREM 1).
func DelListItem(ctx context.Context, rdb redis.Cmdable, key, val string) error {
	return rdb.LRem(ctx, key, 1, val).Err()
}

// ---- stream --------------------------------------------------------------

// StreamEntry keeps field order as scanned.
type StreamEntry struct {
	ID     string
	Fields [][2]string
}

// LoadStream returns up to count entries strictly after from ("" = start).
func LoadStream(ctx context.Context, rdb redis.Cmdable, key, from string, count int64) ([]StreamEntry, error) {
	if count <= 0 {
		count = PageLen
	}
	start := from
	if start == "" {
		start = "-"
	}
	msgs, err := rdb.XRangeN(ctx, key, start, "+", count).Result()
	if err != nil {
		return nil, err
	}
	// inclusive fetch + drop the boundary entry: portable across servers
	// that lack the exclusive "(" range syntax
	if from != "" && len(msgs) > 0 && msgs[0].ID == from {
		msgs = msgs[1:]
	}
	items := make([]StreamEntry, 0, len(msgs))
	for _, m := range msgs {
		e := StreamEntry{ID: m.ID}
		for _, f := range sortedKeys(m.Values) {
			e.Fields = append(e.Fields, [2]string{f, toStr(m.Values[f])})
		}
		items = append(items, e)
	}
	return items, nil
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

func AddStreamEntry(ctx context.Context, rdb redis.Cmdable, key, field, val string) error {
	return rdb.XAdd(ctx, &redis.XAddArgs{Stream: key, Values: map[string]any{field: val}}).Err()
}

func DelStreamEntry(ctx context.Context, rdb redis.Cmdable, key, id string) error {
	return rdb.XDel(ctx, key, id).Err()
}
