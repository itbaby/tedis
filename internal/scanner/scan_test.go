package scanner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) redis.UniversalClient {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestScanCollectsAllKeys(t *testing.T) {
	c := newTestRedis(t)
	ctx := context.Background()
	want := map[string]bool{}
	for i := 0; i < 100; i++ {
		k := fmt.Sprintf("users:%03d", i)
		if err := c.Set(ctx, k, "v", 0).Err(); err != nil {
			t.Fatal(err)
		}
		want[k] = true
	}
	got := map[string]bool{}
	err := Scan(ctx, c, "", 10, func(keys []string) bool {
		for _, k := range keys {
			got[k] = true
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("scanned %d, want %d", len(got), len(want))
	}
	for k := range want {
		if !got[k] {
			t.Fatalf("missing %s", k)
		}
	}
}

func TestScanMatch(t *testing.T) {
	c := newTestRedis(t)
	ctx := context.Background()
	for _, k := range []string{"users:1", "users:2", "session:1"} {
		c.Set(ctx, k, "v", 0)
	}
	var got []string
	if err := Scan(ctx, c, "users:*", 10, func(keys []string) bool {
		got = append(got, keys...)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("match returned %v", got)
	}
}

func TestScanEarlyStop(t *testing.T) {
	c := newTestRedis(t)
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		c.Set(ctx, fmt.Sprintf("k%02d", i), "v", 0)
	}
	n := 0
	if err := Scan(ctx, c, "", 5, func(keys []string) bool {
		n++
		return false // stop after first batch
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("fn called %d times, want 1", n)
	}
}

func TestScanCanceledContext(t *testing.T) {
	c := newTestRedis(t)
	ctx := context.Background()
	for i := 0; i < 500; i++ {
		c.Set(ctx, fmt.Sprintf("k%03d", i), "v", 0)
	}
	sctx, cancel := context.WithCancel(ctx)
	cancel() // abort before the first round trip
	start := time.Now()
	err := Scan(sctx, c, "", 1, func(keys []string) bool { return true })
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if time.Since(start) > time.Second {
		t.Fatal("canceled scan should return immediately")
	}
}
