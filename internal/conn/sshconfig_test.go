package conn

import (
	"os"
	"path/filepath"
	"testing"
)

func findBlock(blocks []sshBlock, pattern string) *sshBlock {
	for i := range blocks {
		for _, p := range blocks[i].patterns {
			if p == pattern {
				return &blocks[i]
			}
		}
	}
	return nil
}

func TestParseSSHConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `
# global comment
Host jump prod-jump *.example.com
  User ec2-user        # inline comment
  IdentityFile ~/.ssh/prod.pem
  Port 2222

Host *
  User default-user
  AddKeysToAgent yes
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	blocks, err := parseSSHConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d: %+v", len(blocks), blocks)
	}
	jump := findBlock(blocks, "jump")
	if jump == nil || jump.user != "ec2-user" || jump.port != "2222" || jump.hostName != "" {
		t.Fatalf("jump block: %+v", jump)
	}
	if jump.identityFile != "~/.ssh/prod.pem" {
		t.Fatalf("identity file: %q", jump.identityFile)
	}
	if len(jump.patterns) != 3 { // jump, prod-jump, *.example.com
		t.Fatalf("patterns: %v", jump.patterns)
	}
	star := findBlock(blocks, "*")
	if star == nil || star.user != "default-user" {
		t.Fatalf("* block: %+v", star)
	}
}

func TestLookupSSHConfigFirstMatchWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ssh", "config")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("Host db\n  HostName 10.0.0.5\n  User admin\n  Port 2222\n  IdentityFile ~/keys/db.pem\n\nHost *\n  User fallback\n  Port 9999\n"), 0o600)

	t.Setenv("HOME", dir)
	hostName, user, port, key := lookupSSHConfig("db")
	if hostName != "10.0.0.5" || user != "admin" || port != 2222 || key != filepath.Join(dir, "keys", "db.pem") {
		t.Fatalf("got host=%q user=%q port=%d key=%q", hostName, user, port, key)
	}

	// unknown alias: only Host * matches → fallback user, its port applies
	hostName, user, port, key = lookupSSHConfig("other")
	if hostName != "other" || user != "fallback" || port != 9999 || key != "" {
		t.Fatalf("fallback: host=%q user=%q port=%d key=%q", hostName, user, port, key)
	}
}

func TestHostPatternMatch(t *testing.T) {
	cases := []struct {
		pat, host string
		want      bool
	}{
		{"*", "anything", true},
		{"jump", "jump", true},
		{"jump", "jumpy", false},
		{"*.example.com", "a.example.com", true},
		{"*.example.com", "example.org", false},
		{"db?", "db1", true},
		{"db?", "db12", false},
		{"?ump", "jump", true},
	}
	for _, c := range cases {
		if got := hostPatternMatch(c.pat, c.host); got != c.want {
			t.Errorf("%q vs %q: got %v", c.pat, c.host, got)
		}
	}
}
