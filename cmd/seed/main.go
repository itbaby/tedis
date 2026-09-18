// tedis-seed populates a redis with demo data that exercises every viewer
// and codec: JSON / MessagePack / gzip / PHP-serialize strings and container
// fields, binary blobs, TTL countdowns, and one key per container type.
//
// Usage: go run ./cmd/seed [-addr 127.0.0.1:6379] [-flush demo:*]
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vmihailenco/msgpack/v5"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:6379", "redis address")
	flag.Parse()

	var ctx = context.Background()

	c := redis.NewClient(&redis.Options{Addr: *addr})
	defer c.Close()
	if err := c.Ping(ctx).Err(); err != nil {
		log.Fatalf("connect %s: %v", *addr, err)
	}

	user := map[string]any{
		"id": 1001, "name": "Ada Lovelace", "active": true,
		"roles": []string{"admin", "dev"},
		"meta":  map[string]any{"logins": 42, "last": "2026-09-19T01:02:03Z"},
	}
	event := map[string]any{"type": "login", "ok": true, "ms": 12}

	set := func(k string, v any, ttl time.Duration) {
		var b []byte
		switch x := v.(type) {
		case []byte:
			b = x
		case string:
			b = []byte(x)
		default:
			b = must(json.Marshal(x))
		}
		if err := c.Set(ctx, k, b, ttl).Err(); err != nil {
			log.Fatalf("set %s: %v", k, err)
		}
	}

	// strings: one per codec
	set("demo:str:json", user, 0)
	set("demo:str:json-compact", `{"compact":true,"nested":{"a":[1,2,3],"b":null}}`, 0)
	set("demo:str:msgpack", must(msgpack.Marshal(user)), 0)
	set("demo:str:msgpack-event", must(msgpack.Marshal(event)), 0)
	set("demo:str:gzip", gz(must(json.Marshal(user))), 0)
	set("demo:str:php", `a:3:{s:2:"id";i:1001;s:4:"name";s:12:"Ada Lovelace";s:5:"roles";a:2:{i:0;s:5:"admin";i:1;s:3:"dev";}}`, 0)
	set("demo:str:bin", []byte{0x00, 0x01, 0xfe, 0xff, 'b', 'i', 'n', 0x80, 0x7f}, 0)
	set("demo:str:plain", "just plain utf8 text ✓ — no codec needed", 0)
	set("demo:str:expiring", "watch my ttl turn red", 90*time.Second)

	// hash with per-field codecs (field-value pipeline demo)
	c.Del(ctx, "demo:hash:profiles")
	c.HSet(ctx, "demo:hash:profiles", map[string]any{
		"json":    must(json.Marshal(user)),
		"msgpack": must(msgpack.Marshal(event)),
		"plain":   "text field",
		"binary":  []byte{0x81, 0xa1, 'x', 0x01}, // msgpack {"x":1}
	})

	// list with mixed json/plain items
	c.Del(ctx, "demo:list:events")
	for i := 0; i < 6; i++ {
		e := map[string]any{"seq": i, "type": "tick", "at": time.Now().Unix()}
		c.RPush(ctx, "demo:list:events", must(json.Marshal(e)))
	}
	c.RPush(ctx, "demo:list:events", "plain tail item")

	// set / zset / stream
	c.Del(ctx, "demo:set:tags", "demo:zset:scores", "demo:stream:log")
	c.SAdd(ctx, "demo:set:tags", "alpha", "beta", "gamma", "delta")
	for _, z := range []redis.Z{{Score: 1.5, Member: "p50"}, {Score: 12.25, Member: "p90"}, {Score: 99, Member: "p99"}} {
		c.ZAdd(ctx, "demo:zset:scores", z)
	}
	for i := 0; i < 5; i++ {
		c.XAdd(ctx, &redis.XAddArgs{Stream: "demo:stream:log",
			Values: map[string]any{"lvl": "info", "msg": fmt.Sprintf("entry %d", i)}})
	}

	n, _ := c.DBSize(ctx).Result()
	fmt.Printf("seeded demo:* into %s (dbsize now %d)\n", *addr, n)
}

func gz(b []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(b)
	w.Close()
	return buf.Bytes()
}

func must(b []byte, err error) []byte {
	if err != nil {
		log.Fatal(err)
	}
	return b
}
