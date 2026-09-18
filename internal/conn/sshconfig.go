package conn

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// sshBlock is one Host block from ssh_config.
type sshBlock struct {
	patterns                           []string
	user, hostName, port, identityFile string
}

// parseSSHConfig parses a minimal subset of ssh_config: Host blocks with
// User, HostName, Port, IdentityFile directives. OpenSSH semantics kept:
// first match wins per option, in file order; Host patterns support * and ?.
// Include is not followed (documented limitation).
func parseSSHConfig(path string) ([]sshBlock, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var blocks []sshBlock
	var cur *sshBlock
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "#"); i >= 0 { // trailing comment
			line = strings.TrimSpace(line[:i])
		}
		key, val := splitDirective(line)
		if key == "" {
			continue
		}
		switch key {
		case "host":
			blocks = append(blocks, sshBlock{patterns: strings.Fields(val)})
			cur = &blocks[len(blocks)-1]
		case "user", "hostname", "port", "identityfile":
			if cur == nil {
				continue
			}
			switch key {
			case "user":
				if cur.user == "" {
					cur.user = val
				}
			case "hostname":
				if cur.hostName == "" {
					cur.hostName = val
				}
			case "port":
				if cur.port == "" {
					cur.port = val
				}
			case "identityfile":
				if cur.identityFile == "" {
					cur.identityFile = val
				}
			}
		}
	}
	return blocks, sc.Err()
}

func splitDirective(line string) (key, val string) {
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return strings.ToLower(line), ""
	}
	return strings.ToLower(line[:i]), strings.TrimSpace(line[i+1:])
}

// lookupSSHConfig fills tunnel defaults from ~/.ssh/config. Fields already
// set by the profile take precedence; among config blocks, first match wins.
func lookupSSHConfig(alias string) (hostName, user string, port int, identityFile string) {
	hostName = alias
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	blocks, err := parseSSHConfig(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return
	}
	for _, b := range blocks {
		matched := false
		for _, pat := range b.patterns {
			if hostPatternMatch(pat, alias) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if hostName == alias && b.hostName != "" {
			hostName = b.hostName
		}
		if user == "" && b.user != "" {
			user = b.user
		}
		if port == 0 && b.port != "" {
			if n, err := strconv.Atoi(b.port); err == nil {
				port = n
			}
		}
		if identityFile == "" && b.identityFile != "" {
			identityFile = expandTilde(b.identityFile, home)
		}
	}
	return
}

func hostPatternMatch(pattern, host string) bool {
	if pattern == "*" || pattern == host {
		return true
	}
	if i := strings.IndexByte(pattern, '*'); i >= 0 {
		prefix, suffix := pattern[:i], pattern[i+1:]
		return strings.HasPrefix(host, prefix) && strings.HasSuffix(host, suffix)
	}
	if strings.ContainsRune(pattern, '?') {
		if len(host) != len(pattern) {
			return false
		}
		for k := range pattern {
			if pattern[k] != '?' && pattern[k] != host[k] {
				return false
			}
		}
		return true
	}
	return false
}

func expandTilde(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
