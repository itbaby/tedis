package conn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/ssh"

	"tedis/internal/config"
)

// ServerInfo carries what the status bar shows about the server.
type ServerInfo struct {
	Version string // e.g. 7.2.4
	Mode    string // standalone | cluster
}

// Conn wraps a go-redis client plus connection-scoped state.
type Conn struct {
	P      *config.Profile
	Client redis.UniversalClient
	Info   ServerInfo

	ssh *ssh.Client
}

// Connect dials according to the profile: direct, TLS, SSH tunnel (standalone
// or cluster). Verifies with PING and loads server info.
func Connect(ctx context.Context, p *config.Profile) (*Conn, error) {
	if p == nil || p.Host == "" {
		return nil, errors.New("connection profile has no host")
	}
	c := &Conn{P: p}
	addr := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))

	var dialer func(ctx context.Context, network, addr string) (net.Conn, error)
	if p.SSH != nil && p.SSH.Host != "" {
		cl, err := sshClient(p)
		if err != nil {
			return nil, err
		}
		c.ssh = cl
		dialer = tunnelDialer(cl)
	}

	var tlsConfig *tls.Config
	if p.TLS.Enabled {
		tc := &tls.Config{
			ServerName:         p.Host,
			InsecureSkipVerify: p.TLS.Insecure, //nolint:gosec // explicit opt-in (ElastiCache self-managed CA)
			MinVersion:         tls.VersionTLS12,
		}
		if p.TLS.CACert != "" {
			pem, err := os.ReadFile(p.TLS.CACert)
			if err != nil {
				return nil, fmt.Errorf("tls: read ca cert: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("tls: no certificates found in ca cert file")
			}
			tc.RootCAs = pool
		}
		tlsConfig = tc
	}

	if p.Cluster {
		if p.DB != 0 {
			return nil, errors.New("cluster mode only exposes database 0")
		}
		c.Client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:          []string{addr},
			Username:       p.Username,
			Password:       p.Password,
			TLSConfig:      tlsConfig,
			Dialer:         dialer,
			RouteByLatency: true,
		})
	} else {
		c.Client = redis.NewClient(&redis.Options{
			Addr:      addr,
			Username:  p.Username,
			Password:  p.Password,
			DB:        p.DB,
			TLSConfig: tlsConfig,
			Dialer:    dialer,
		})
	}

	if err := c.Client.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}
	if err := c.loadInfo(ctx); err != nil {
		// info is cosmetic; keep the connection
		c.Info = ServerInfo{Version: "?"}
	}
	return c, nil
}

func (c *Conn) loadInfo(ctx context.Context) error {
	raw, err := c.Client.Info(ctx, "server").Result()
	if err != nil {
		return err
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r") // INFO responses use CRLF
		switch {
		case strings.HasPrefix(line, "redis_version:"):
			c.Info.Version = strings.TrimPrefix(line, "redis_version:")
		case strings.HasPrefix(line, "redis_mode:"):
			c.Info.Mode = strings.TrimPrefix(line, "redis_mode:")
		}
	}
	return nil
}

// Ping measures round-trip latency.
func (c *Conn) Ping(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	err := c.Client.Ping(ctx).Err()
	return time.Since(start), err
}

// DBSize returns the number of keys in the current database.
func (c *Conn) DBSize(ctx context.Context) (int64, error) {
	return c.Client.DBSize(ctx).Result()
}

// Close tears down the redis client and the SSH tunnel, if any.
func (c *Conn) Close() error {
	var errs []error
	if c.Client != nil {
		errs = append(errs, c.Client.Close())
	}
	if c.ssh != nil {
		errs = append(errs, c.ssh.Close())
	}
	return errors.Join(errs...)
}
