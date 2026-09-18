package keyview

import (
	"context"
	"math"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newClient(t *testing.T) redis.Cmdable {
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestStringRoundTrip(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	if err := SaveString(ctx, c, "s", "hello"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadString(ctx, c, "s")
	if err != nil || got != "hello" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestHashPageAndMutate(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		if err := SaveHashField(ctx, c, "h", field(i), "v"); err != nil {
			t.Fatal(err)
		}
	}
	_, items, err := LoadHash(ctx, c, "h", 0, 10)
	if err != nil || len(items) == 0 || len(items) > 30 {
		t.Fatalf("page1: %d items err %v", len(items), err)
	}
	if err := SaveHashField(ctx, c, "h", "x", "1"); err != nil {
		t.Fatal(err)
	}
	if err := DelHashField(ctx, c, "h", "x"); err != nil {
		t.Fatal(err)
	}
	n, _ := c.HLen(ctx, "h").Result()
	if n != 30 {
		t.Fatalf("hlen=%d", n)
	}
}

func TestSetAndZSet(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	for _, m := range []string{"a", "b", "c"} {
		if err := SaveSetMember(ctx, c, "set", m); err != nil {
			t.Fatal(err)
		}
	}
	_, members, err := LoadSet(ctx, c, "set", 0, 10)
	if err != nil || len(members) != 3 {
		t.Fatalf("members=%v err %v", members, err)
	}
	if err := DelSetMember(ctx, c, "set", "b"); err != nil {
		t.Fatal(err)
	}

	if err := SaveZSetMember(ctx, c, "z", "m1", 1.5); err != nil {
		t.Fatal(err)
	}
	if err := SaveZSetMember(ctx, c, "z", "m2", 3); err != nil {
		t.Fatal(err)
	}
	_, items, err := LoadZSet(ctx, c, "z", 0, 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v err %v", items, err)
	}
	if items[0].Member != "m1" || math.Abs(items[0].Score-1.5) > 1e-9 {
		t.Fatalf("first=%+v", items[0])
	}
	if err := DelZSetMember(ctx, c, "z", "m1"); err != nil {
		t.Fatal(err)
	}
	n, _ := c.ZCard(ctx, "z").Result()
	if n != 1 {
		t.Fatalf("zcard=%d", n)
	}
}

func TestListPageAndMutate(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := AddListItem(ctx, c, "l", string(rune('a'+i)), false); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := LoadList(ctx, c, "l", 1, 2)
	if err != nil || total != 5 || len(items) != 2 || items[0].Index != 1 || items[0].Value != "b" {
		t.Fatalf("items=%+v total=%d err %v", items, total, err)
	}
	if err := SaveListItem(ctx, c, "l", 1, "B"); err != nil {
		t.Fatal(err)
	}
	v, _ := c.LIndex(ctx, "l", 1).Result()
	if v != "B" {
		t.Fatalf("lindex=%q", v)
	}
	if err := DelListItem(ctx, c, "l", "B"); err != nil {
		t.Fatal(err)
	}
	n, _ := c.LLen(ctx, "l").Result()
	if n != 4 {
		t.Fatalf("llen=%d", n)
	}
}

func TestStreamPagingExclusive(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := AddStreamEntry(ctx, c, "st", "f", "v"); err != nil {
			t.Fatal(err)
		}
	}
	items, err := LoadStream(ctx, c, "st", "", 2)
	if err != nil || len(items) != 2 {
		t.Fatalf("page1 len=%d err %v", len(items), err)
	}
	next, err := LoadStream(ctx, c, "st", items[1].ID, 2)
	if err != nil || len(next) != 1 {
		t.Fatalf("page2 len=%d err %v", len(next), err)
	}
	if err := DelStreamEntry(ctx, c, "st", next[0].ID); err != nil {
		t.Fatal(err)
	}
}

func field(i int) string {
	return "f" + string(rune('a'+i/26)) + string(rune('a'+i%26))
}
