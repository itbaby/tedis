// tedis — a compact Redis TUI, feature-modeled after Medis 2.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"tedis/internal/app"
	"tedis/internal/config"
	"tedis/internal/conn"
)

func main() {
	var (
		profile = flag.String("c", "", "connect to a named profile from the config")
		urlArg  = flag.String("u", "", "connect via redis:// or rediss:// URL")
		sshArg  = flag.String("ssh", "", "SSH jump host user@host[:port] (with -u)")
		keyArg  = flag.String("i", "", "SSH identity file (with -ssh)")
	)
	flag.Parse()

	logger := newLogger()

	cfgPath := os.Getenv("TEDIS_CONFIG")
	if cfgPath == "" {
		var err error
		cfgPath, err = config.Path()
		if err != nil {
			fatal(err)
		}
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fatal(err)
	}

	// positional URL: tedis redis://localhost:6379/2
	if *urlArg == "" && flag.NArg() > 0 {
		*urlArg = flag.Args()[0]
	}

	var initial *config.Profile
	switch {
	case *profile != "":
		p, ok := cfg.Get(*profile)
		if !ok {
			fatal(fmt.Errorf("no profile %q in %s", *profile, cfgPath))
		}
		initial = p
	case *urlArg != "":
		p, err := conn.ProfileFromURL(*urlArg)
		if err != nil {
			fatal(err)
		}
		if *sshArg != "" {
			ssh, err := parseSSHArg(*sshArg)
			if err != nil {
				fatal(err)
			}
			if *keyArg != "" {
				ssh.KeyPath = *keyArg
			}
			p.SSH = ssh
		}
		initial = p
	}

	a := app.New(cfg, cfgPath, logger)
	if initial != nil {
		a.ConnectAsync(initial)
	}
	if err := a.Run(); err != nil {
		fatal(err)
	}
}

// parseSSHArg accepts user@host[:port] with every part but host optional.
func parseSSHArg(s string) (*config.SSH, error) {
	ssh := &config.SSH{}
	host := s
	if i := strings.Index(s, "@"); i >= 0 {
		ssh.User = s[:i]
		host = s[i+1:]
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		p, err := strconv.Atoi(host[i+1:])
		if err != nil || p <= 0 || p > 65535 {
			return nil, fmt.Errorf("bad ssh port in %q", s)
		}
		ssh.Port = p
		host = host[:i]
	}
	if host == "" {
		return nil, fmt.Errorf("bad ssh spec %q (want user@host[:port])", s)
	}
	ssh.Host = host
	return ssh, nil
}

func newLogger() *slog.Logger {
	home, err := os.UserHomeDir()
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	dir := filepath.Join(home, ".local", "share", "tedis")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	f, err := os.OpenFile(filepath.Join(dir, "tedis.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "tedis:", err)
	os.Exit(1)
}
