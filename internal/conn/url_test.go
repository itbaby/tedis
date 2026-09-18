package conn

import "testing"

const defPort = 6379

func TestProfileFromURL(t *testing.T) {
	cases := []struct {
		raw       string
		host      string
		port      int
		user, pw  string
		db        int
		tls, insc bool
		cluster   bool
		wantErr   bool
	}{
		{raw: "redis://localhost:6379", host: "localhost", port: 6379},
		{raw: "redis://localhost", host: "localhost", port: defPort},
		{raw: "redis://u:p@h:6380/2", host: "h", port: 6380, user: "u", pw: "p", db: 2},
		{raw: "redis://:pw@h/db0", wantErr: true}, // db must be a plain int
		{raw: "redis://pw@h", host: "h", port: defPort, user: "pw"},
		{raw: "rediss://h", host: "h", port: defPort, tls: true},
		{raw: "redis://h?ssl=true", host: "h", port: defPort, tls: true},
		{raw: "redis://h?ssl=true&insecure=true", host: "h", port: defPort, tls: true, insc: true},
		{raw: "redis://h:99999", wantErr: true},
		{raw: "redis://h/9", host: "h", port: defPort, db: 9},
		{raw: "redis://h/cluster/x", wantErr: true},
		{raw: "redis://h?cluster=true", host: "h", port: defPort, cluster: true},
		{raw: "http://h", wantErr: true},
		{raw: "redis://", wantErr: true},
		{raw: "redis://h:0", wantErr: true},
	}
	for _, c := range cases {
		p, err := ProfileFromURL(c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %+v", c.raw, p)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.raw, err)
			continue
		}
		if p.Host != c.host || p.Port != c.port || p.Username != c.user || p.Password != c.pw ||
			p.DB != c.db || p.TLS.Enabled != c.tls || p.TLS.Insecure != c.insc || p.Cluster != c.cluster {
			t.Errorf("%s: got %+v", c.raw, p)
		}
	}
}

func TestProfileFromURLBadDBPath(t *testing.T) {
	if _, err := ProfileFromURL("redis://h/2/x"); err == nil {
		t.Fatal("expected error for multi-segment path")
	}
}
