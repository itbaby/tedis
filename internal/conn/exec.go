package conn

import (
	"context"
	"fmt"
	"strings"

	"tedis/internal/cmdquery"
	"tedis/internal/cmdtable"
)

// ExecResult is one rendered command reply.
type ExecResult struct {
	Cmd  cmdquery.Command
	Kind string // status | int | bulk | array | nil | error
	Text string
	Err  error
}

// Exec runs one parsed command and renders the reply.
func (c *Conn) Exec(ctx context.Context, cmd cmdquery.Command) ExecResult {
	full := make([]interface{}, 0, len(cmd.Args)+1)
	full = append(full, cmd.Name)
	for _, a := range cmd.Args {
		full = append(full, a.Text)
	}
	v, err := c.Client.Do(ctx, full...).Result()
	if err != nil && err.Error() != "redis: nil" {
		return ExecResult{Cmd: cmd, Kind: "error", Text: err.Error(), Err: err}
	}
	kind, text := renderReply(v)
	return ExecResult{Cmd: cmd, Kind: kind, Text: text}
}

// renderReply converts a RESP reply to display text.
func renderReply(v interface{}) (kind, text string) {
	switch x := v.(type) {
	case nil:
		return "nil", "(nil)"
	case string:
		return "bulk", x
	case []byte:
		return "bulk", string(x)
	case int64:
		return "int", fmt.Sprintf("%d", x)
	case []interface{}:
		var b strings.Builder
		fmt.Fprintf(&b, "(%d items)", len(x))
		for i, e := range x {
			k, t := renderReply(e)
			fmt.Fprintf(&b, "\n%d) ", i+1)
			if k == "bulk" && strings.Contains(t, "\n") {
				b.WriteString(indentLines(t))
			} else {
				b.WriteString(t)
			}
		}
		return "array", b.String()
	case fmt.Stringer:
		return "status", x.String()
	}
	return "status", fmt.Sprintf("%v", v)
}

func indentLines(s string) string {
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = "    " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// LoadCmdTable refreshes the read/write command table from the server.
func (c *Conn) LoadCmdTable(ctx context.Context) *cmdtable.Table {
	t := cmdtable.Load(ctx, c.Client)
	return t
}
