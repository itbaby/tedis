// Package cmdtable classifies Redis commands as read vs write. The table is
// loaded from the server's COMMAND output at connect time, so classification
// follows the server version instead of a hardcoded list (used for query
// highlighting and alert-mode confirmation, mirroring Medis).
package cmdtable

import (
	"context"
	"sort"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Class is the write classification of a command.
type Class int

const (
	Unknown Class = iota
	Readonly
	Write
)

func (c Class) String() string {
	switch c {
	case Readonly:
		return "readonly"
	case Write:
		return "write"
	}
	return "unknown"
}

// Table holds the loaded command metadata.
type Table struct {
	classes map[string]Class
	names   []string // sorted; drives completion
}

// ClassOf returns the class for a command name (case-insensitive).
func (t *Table) ClassOf(name string) Class {
	if t == nil {
		return Unknown
	}
	return t.classes[strings.ToLower(name)]
}

// Names returns the sorted command names (for completion).
func (t *Table) Names() []string {
	if t == nil {
		return nil
	}
	return t.names
}

// Source is anything that can run COMMAND (redis clients satisfy it).
type Source interface {
	Do(ctx context.Context, args ...interface{}) *redis.Cmd
}

// Load fetches COMMAND from the server. It never fails hard: on error an
// empty table is returned (classification degrades to Unknown).
func Load(ctx context.Context, src Source) *Table {
	t := &Table{classes: map[string]Class{}}
	v, err := src.Do(ctx, "COMMAND").Result()
	if err != nil {
		return t
	}
	ParseReply(v, t)
	return t
}

// ParseReply fills t from a COMMAND reply. Shape per RESP:
//
//  1. name   2) arity  3) flags [] 4) first-key 5) last-key 6) step
func ParseReply(v interface{}, t *Table) {
	entries, ok := v.([]interface{})
	if !ok {
		return
	}
	for _, e := range entries {
		fields, ok := e.([]interface{})
		if !ok || len(fields) < 3 {
			continue
		}
		name, ok := fields[0].(string)
		if !ok {
			if b, isBytes := fields[0].([]byte); isBytes {
				name = string(b)
			} else {
				continue
			}
		}
		cls := Unknown
		if flags, ok := fields[2].([]interface{}); ok {
			for _, f := range flags {
				switch flagToString(f) {
				case "write":
					cls = Write
				case "readonly":
					if cls == Unknown {
						cls = Readonly
					}
				}
			}
		}
		key := strings.ToLower(name)
		t.classes[key] = cls
		t.names = append(t.names, name)
	}
	sort.Strings(t.names)
}

func flagToString(f interface{}) string {
	switch x := f.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	}
	return ""
}
