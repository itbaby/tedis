package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.Settings.Separator != ":" || cfg.Settings.ScanCount != 500 || cfg.Settings.MaxFoldLevel != 1 {
		t.Fatalf("defaults not applied: %+v", cfg.Settings)
	}
	if len(cfg.Profiles) != 0 {
		t.Fatalf("expected no profiles, got %d", len(cfg.Profiles))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	cfg := Default()
	cfg.UpsertProfile(&Profile{
		Name:     "local",
		Host:     "127.0.0.1",
		Username: "default",
		Password: "secret",
		DB:       2,
		TLS:      TLS{Enabled: true, Insecure: true},
		SSH:      &SSH{Host: "jump.example.com", User: "ec2-user", KeyPath: "/tmp/k.pem"},
		Rules:    []Rule{{Pattern: "session:*", Encoder: "msgpack"}},
	})
	cfg.UpsertProfile(&Profile{Name: "prod", Host: "r.example.com", Port: 6380, Cluster: true})

	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	local, ok := got.Get("local")
	if !ok {
		t.Fatal("profile local lost")
	}
	if local.Host != "127.0.0.1" || local.Username != "default" || local.Password != "secret" || local.DB != 2 {
		t.Fatalf("basic fields lost: %+v", local)
	}
	if !local.TLS.Enabled || !local.TLS.Insecure {
		t.Fatalf("tls lost: %+v", local.TLS)
	}
	if local.SSH == nil || local.SSH.Host != "jump.example.com" || local.SSH.Port != 22 {
		t.Fatalf("ssh lost or port default not applied: %+v", local.SSH)
	}
	if len(local.Rules) != 1 || local.Rules[0].Pattern != "session:*" || local.Rules[0].Encoder != "msgpack" {
		t.Fatalf("rules lost: %+v", local.Rules)
	}
	// defaults normalized on load
	if local.Separator != ":" || local.ScanCount != 500 || local.MaxFoldLevel != 1 {
		t.Fatalf("profile defaults not normalized: sep=%q scan=%d fold=%d", local.Separator, local.ScanCount, local.MaxFoldLevel)
	}

	prod, _ := got.Get("prod")
	if !prod.Cluster || prod.Port != 6380 {
		t.Fatalf("cluster profile lost: %+v", prod)
	}
}

func TestPasswordNotInFileWhenEmpty(t *testing.T) {
	cfg := Default()
	cfg.UpsertProfile(&Profile{Name: "p", Host: "h"})
	path := filepath.Join(t.TempDir(), "c.toml")
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "password") {
		t.Fatalf("empty password should be omitted:\n%s", data)
	}
}
