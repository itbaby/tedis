// Package conn builds and manages Redis connections (direct, TLS, SSH
// tunnel, cluster) on top of go-redis.
package conn

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"tedis/internal/config"
)

// ProfileFromURL parses a connection URL into a Profile.
// Supported forms (equivalent of Medis' medis:// / mediss:// schemes):
//
//	redis://[username[:password]@]host[:port][/db][?ssl=true&insecure=true&cluster=true]
//	rediss://...  (implies TLS)
func ProfileFromURL(raw string) (*config.Profile, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "redis", "rediss":
	default:
		return nil, fmt.Errorf("unsupported scheme %q (want redis:// or rediss://)", u.Scheme)
	}

	p := &config.Profile{Host: u.Hostname()}
	if p.Host == "" {
		return nil, fmt.Errorf("missing host in %q", raw)
	}
	if s := u.Port(); s != "" {
		port, err := strconv.Atoi(s)
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("bad port %q", s)
		}
		p.Port = port
	}
	if p.Port == 0 {
		p.Port = 6379
	}
	if u.User != nil {
		p.Username = u.User.Username()
		p.Password, _ = u.User.Password()
	}
	if s := u.Path; len(s) > 1 { // "/2"
		db, err := strconv.Atoi(s[1:])
		if err != nil || db < 0 {
			return nil, fmt.Errorf("bad database %q (want /<int>)", s[1:])
		}
		p.DB = db
	}
	q := u.Query()
	if u.Scheme == "rediss" || q.Get("ssl") == "true" {
		p.TLS.Enabled = true
	}
	if q.Get("insecure") == "true" {
		p.TLS.Enabled = true
		p.TLS.Insecure = true
	}
	if q.Get("cluster") == "true" {
		p.Cluster = true
		p.DB = 0 // cluster only exposes db 0
	}
	p.Name = net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	return p, nil
}
