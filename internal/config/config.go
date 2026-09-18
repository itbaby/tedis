// Package config holds the persistent configuration model and its TOML
// representation (~/.config/tedis/config.toml).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// TLS settings for a connection.
type TLS struct {
	Enabled  bool   `toml:"enabled"`
	Insecure bool   `toml:"insecure,omitempty"` // skip certificate verification (e.g. ElastiCache)
	CACert   string `toml:"ca_cert,omitempty"`  // path to CA bundle
}

// SSH describes a jump host tunnel. Empty Host means no tunnel.
type SSH struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port,omitempty"` // default 22
	User     string `toml:"user,omitempty"`
	KeyPath  string `toml:"key_path,omitempty"`
	Password string `toml:"password,omitempty"`
}

// Rule is a content rule: when key name matches Pattern (glob) AND type
// equals Type ("" = any), use the given Viewer/Encoder ("" = auto).
// Applied to fields of container types as well. (Medis "Content Rules")
type Rule struct {
	Pattern string `toml:"pattern"`
	Type    string `toml:"type,omitempty"`
	Viewer  string `toml:"viewer,omitempty"`
	Encoder string `toml:"encoder,omitempty"`
}

// Profile is one saved connection.
type Profile struct {
	Name string `toml:"-"` // key in the profiles map

	Host     string `toml:"host"`
	Port     int    `toml:"port"` // default 6379
	Username string `toml:"username,omitempty"`
	Password string `toml:"password,omitempty"`
	DB       int    `toml:"db,omitempty"` // default database (Medis "Default Database")

	TLS     TLS  `toml:"tls,omitempty"`
	Cluster bool `toml:"cluster,omitempty"` // Redis Cluster mode
	SSH     *SSH `toml:"ssh,omitempty"`     // tunnel via jump host

	SkipDeleteConfirm bool `toml:"skip_delete_confirm,omitempty"` // Medis "Delete Confirmation"

	Separator    string `toml:"separator,omitempty"`      // key tree separator, default ":"
	MaxFoldLevel int    `toml:"max_fold_level,omitempty"` // Medis "fold level"
	ScanCount    int    `toml:"scan_count,omitempty"`     // SCAN COUNT batch size

	Rules []Rule `toml:"rules,omitempty"`
}

// Settings are app-wide (not per connection).
type Settings struct {
	Theme        string `toml:"theme,omitempty"`          // dark | light
	Language     string `toml:"language,omitempty"`       // en | zh-CN
	AlertDefault bool   `toml:"alert_default,omitempty"`  // Medis "Alert mode"
	ScanCount    int    `toml:"scan_count,omitempty"`     // default for profiles
	Separator    string `toml:"separator,omitempty"`      // default ":"
	MaxFoldLevel int    `toml:"max_fold_level,omitempty"` // default 1
}

// Config is the root document.
type Config struct {
	Settings Settings            `toml:"settings"`
	Profiles map[string]*Profile `toml:"profiles"`
}

// Default returns a config with sane defaults and no profiles.
func Default() *Config {
	return &Config{
		Settings: Settings{Theme: "dark", ScanCount: 500, Separator: ":", MaxFoldLevel: 1},
		Profiles: map[string]*Profile{},
	}
}

// Path returns the config file location.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tedis", "config.toml"), nil
}

// Load reads the config file; a missing file yields defaults (not an error).
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}
	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.normalize()
	return cfg, nil
}

// Save writes the config back to path (creating parent dirs).
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(cfg)
}

// normalize applies defaults that were omitted in the file.
func (c *Config) normalize() {
	if c.Settings.ScanCount <= 0 {
		c.Settings.ScanCount = 500
	}
	if c.Settings.Separator == "" {
		c.Settings.Separator = ":"
	}
	if c.Settings.MaxFoldLevel <= 0 {
		c.Settings.MaxFoldLevel = 1
	}
	for name, p := range c.Profiles {
		p.Name = name
		if p.Port <= 0 {
			p.Port = 6379
		}
		if p.Separator == "" {
			p.Separator = c.Settings.Separator
		}
		if p.MaxFoldLevel <= 0 {
			p.MaxFoldLevel = c.Settings.MaxFoldLevel
		}
		if p.ScanCount <= 0 {
			p.ScanCount = c.Settings.ScanCount
		}
		if p.SSH != nil && p.SSH.Port <= 0 {
			p.SSH.Port = 22
		}
	}
}

// UpsertProfile inserts or updates a profile by name.
func (c *Config) UpsertProfile(p *Profile) {
	name := p.Name
	c.Profiles[name] = p
}

// Get returns a profile by name.
func (c *Config) Get(name string) (*Profile, bool) {
	p, ok := c.Profiles[name]
	if ok {
		p.Name = name
	}
	return p, ok
}
