package encode

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ExternalDir returns the custom encoder directory (Medis-compatible).
func ExternalDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tedis", "encoders"), nil
}

// External is a user-supplied encoder script (encoder_<name> executable).
// Protocol (identical to Medis): the script receives Base64 of the content
// on stdin and argv[1] = "decode" | "encode", and replies with Base64 on
// stdout. A non-zero exit status surfaces as an error.
type External struct {
	Path string
}

func (e *External) Name() string {
	base := strings.TrimPrefix(filepath.Base(e.Path), "encoder_")
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func (e *External) run(op string, payload []byte) ([]byte, error) {
	cmd := exec.Command(e.Path, op)
	cmd.Stdin = bytes.NewReader([]byte(base64.StdEncoding.EncodeToString(payload)))
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s %s: %s", e.Name(), op, msg)
	}
	b64 := strings.TrimSpace(out.String())
	return base64.StdEncoding.DecodeString(b64)
}

func (e *External) Decode(raw []byte) (string, error) {
	out, err := e.run("decode", raw)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (e *External) Encode(text string) ([]byte, error) {
	return e.run("encode", []byte(text))
}

// externalCache memoizes ScanExternal: ByName/CycleNames run while rendering
// values, and the encoders directory rarely changes while the app runs.
var (
	externalMu    sync.Mutex
	externalCache []*External
	externalValid bool
)

// ScanExternal lists available encoder_* executables, sorted by name.
func ScanExternal() []*External {
	externalMu.Lock()
	defer externalMu.Unlock()
	if externalValid {
		return externalCache
	}
	var out []*External
	dir, err := ExternalDir()
	if err == nil {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, en := range entries {
				name := en.Name()
				if !strings.HasPrefix(name, "encoder_") || en.IsDir() {
					continue
				}
				p := filepath.Join(dir, name)
				if info, err := os.Stat(p); err == nil && info.Mode()&0o111 != 0 {
					out = append(out, &External{Path: p})
				}
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
		}
	}
	externalCache, externalValid = out, true
	return out
}

// ClearExternalCache forces the next ScanExternal to re-read the directory.
func ClearExternalCache() {
	externalMu.Lock()
	externalValid = false
	externalMu.Unlock()
}
