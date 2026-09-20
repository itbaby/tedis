package conn

import (
	"context"
	"testing"

	"tedis/internal/cmdquery"
)

func TestExecDirect(t *testing.T) {
	p := testProfile(t)
	ctx := context.Background()
	c, err := Connect(ctx, p)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	c.Client.Del(ctx, "tedis:exec:n")
	c.Client.Set(ctx, "tedis:exec:str", "hello", 0)
	c.Client.Del(ctx, "tedis:exec:list")
	c.Client.RPush(ctx, "tedis:exec:list", "a", "b")

	r := c.Exec(ctx, mustCmd(t, `get tedis:exec:str`))
	if r.Text != "hello" || r.Err != nil {
		t.Fatalf("get: %+v", r)
	}
	r = c.Exec(ctx, mustCmd(t, `incr tedis:exec:n`))
	if r.Text != "1" {
		t.Fatalf("incr: %+v", r)
	}
	r = c.Exec(ctx, mustCmd(t, `lrange tedis:exec:list 0 -1`))
	if r.Text != "(2 items)\n1) a\n2) b" {
		t.Fatalf("lrange: %q", r.Text)
	}
	r = c.Exec(ctx, mustCmd(t, `get tedis:missing:key`))
	if r.Text != "(nil)" {
		t.Fatalf("nil: %q", r.Text)
	}
	r = c.Exec(ctx, mustCmd(t, `get`)) // wrong arity
	if r.Err == nil {
		t.Fatalf("error: want err, got %+v", r)
	}
}

// TestRenderReplyKinds covers the RESP reply classification without a server.
func TestRenderReplyKinds(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		kind string
		text string
	}{
		{"nil", nil, "nil", "(nil)"},
		{"bulk", "hello", "bulk", "hello"},
		{"bytes", []byte("hi"), "bulk", "hi"},
		{"int", int64(7), "int", "7"},
		{"array", []interface{}{"a", int64(2)}, "array", "(2 items)\n1) a\n2) 2"},
		{"status", "OK", "bulk", "OK"},
	}
	for _, tc := range cases {
		k, tx := renderReply(tc.in)
		if k != tc.kind || tx != tc.text {
			t.Errorf("%s: got (%q,%q), want (%q,%q)", tc.name, k, tx, tc.kind, tc.text)
		}
	}
}

func TestCmdTableLoad(t *testing.T) {
	p := testProfile(t)
	ctx := context.Background()
	c, err := Connect(ctx, p)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()
	tbl := c.LoadCmdTable(ctx)
	if tbl.ClassOf("get") != 1 { // readonly
		t.Fatalf("get class=%d", tbl.ClassOf("get"))
	}
	if tbl.ClassOf("set") != 2 { // write
		t.Fatalf("set class=%d", tbl.ClassOf("set"))
	}
	if len(tbl.Names()) < 100 {
		t.Fatalf("command table suspiciously small: %d", len(tbl.Names()))
	}
}

func mustCmd(t *testing.T, line string) cmdquery.Command {
	t.Helper()
	cmds, err := cmdquery.Parse(line)
	if err != nil || len(cmds) != 1 {
		t.Fatalf("parse %q: %v (%d cmds)", line, err, len(cmds))
	}
	return cmds[0]
}
