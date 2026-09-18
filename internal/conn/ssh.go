package conn

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"tedis/internal/config"
)

// sshClient builds a client to the profile's jump host.
// Auth order: SSH agent (SSH_AUTH_SOCK — covers 1Password's agent), then
// identity file, then password. Empty fields are filled from ~/.ssh/config.
// Host keys are verified against ~/.ssh/known_hosts when that file exists.
func sshClient(p *config.Profile) (*ssh.Client, error) {
	s := p.SSH
	if s == nil || s.Host == "" {
		return nil, nil
	}
	host, user, port, keyPath := lookupSSHConfig(s.Host)
	if s.User != "" {
		user = s.User
	}
	if s.Port != 0 {
		port = s.Port
	}
	if s.KeyPath != "" {
		keyPath = s.KeyPath
	}
	if port == 0 {
		port = 22
	}

	// explicit key first (matches ssh -i semantics), then agent, then
	// password — servers count failed attempts against MaxAuthTries
	var methods []ssh.AuthMethod
	if keyPath != "" {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("ssh: read key %s: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("ssh: parse key %s: %w", keyPath, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if c, err := net.Dial("unix", sock); err == nil {
			ag := agent.NewClient(c)
			methods = append(methods, ssh.PublicKeysCallback(ag.Signers))
		}
	}
	if s.Password != "" {
		methods = append(methods, ssh.Password(s.Password))
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("ssh: no auth method available (no agent via SSH_AUTH_SOCK, no key file, no password)")
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback(),
	}
	return ssh.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), cfg)
}

// hostKeyCallback verifies against ~/.ssh/known_hosts when present; without
// the file we accept (matching Medis' behavior).
func hostKeyCallback() ssh.HostKeyCallback {
	return func(hostport string, remote net.Addr, key ssh.PublicKey) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		kh := filepath.Join(home, ".ssh", "known_hosts")
		if _, err := os.Stat(kh); err != nil {
			return nil
		}
		cb, err := knownhosts.New(kh)
		if err != nil {
			return nil
		}
		return cb(hostport, remote, key)
	}
}

// tunnelDialer routes every redis connection through the SSH client. Used as
// go-redis Options.Dialer, so both standalone and cluster (any node address)
// go through the jump host.
func tunnelDialer(cl *ssh.Client) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := cl.Dial(network, addr)
		if err != nil {
			return nil, err
		}
		return deadlineConn{c}, nil
	}
}

// deadlineConn hides the "deadline not supported" errors of x/crypto SSH
// channels: go-redis calls SetDeadline for context support and treats a
// failed call as a broken connection. Blocking reads are still unblocked by
// Close (which go-redis invokes on context cancellation).
type deadlineConn struct{ net.Conn }

func (deadlineConn) SetDeadline(time.Time) error      { return nil }
func (deadlineConn) SetReadDeadline(time.Time) error  { return nil }
func (deadlineConn) SetWriteDeadline(time.Time) error { return nil }
