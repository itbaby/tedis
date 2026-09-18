package conn

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"tedis/internal/config"
)

// testProfile returns a profile for a local redis (docker), skipping the test
// when nothing is reachable.
func testProfile(t *testing.T) *config.Profile {
	t.Helper()
	addr := os.Getenv("TEDIS_TEST_REDIS")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	host, ps, err := net.SplitHostPort(addr)
	if err != nil {
		t.Skipf("bad TEDIS_TEST_REDIS %q: %v", addr, err)
	}
	port, err := strconv.Atoi(ps)
	if err != nil || port <= 0 {
		t.Skipf("bad port in TEDIS_TEST_REDIS %q", addr)
	}
	p := &config.Profile{Host: host, Port: port}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	cl, err := Connect(ctx, p)
	if err != nil {
		t.Skipf("local redis not reachable (%v)", err)
	}
	_ = cl.Close()
	return p
}

func TestConnectDirectInfoAndDBSize(t *testing.T) {
	p := testProfile(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Connect(ctx, p)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	if c.Info.Version == "" || strings.ContainsAny(c.Info.Version, "\r\n") {
		t.Fatalf("bad version %q", c.Info.Version)
	}
	if _, err := c.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if _, err := c.DBSize(ctx); err != nil {
		t.Fatalf("dbsize: %v", err)
	}
}

func TestConnectBadPort(t *testing.T) {
	p := &config.Profile{Host: "127.0.0.1", Port: 1} // nothing listens here
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := Connect(ctx, p); err == nil {
		t.Fatal("expected error for closed port")
	}
}

func TestConnectMissingHost(t *testing.T) {
	if _, err := Connect(context.Background(), &config.Profile{}); err == nil {
		t.Fatal("expected error for empty host")
	}
}

// TestConnectClusterDBRejected verifies the cluster/db guard without needing
// a live cluster.
func TestConnectClusterDBRejected(t *testing.T) {
	p := &config.Profile{Host: "127.0.0.1", Port: 6379, Cluster: true, DB: 2}
	if _, err := Connect(context.Background(), p); err == nil ||
		!strings.Contains(fmt.Sprint(err), "database 0") {
		t.Fatalf("expected db-0 guard error, got %v", err)
	}
}
