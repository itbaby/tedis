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
	if r.Kind != "bulk" || r.Text != "hello" || r.Err != nil {
		t.Fatalf("get: %+v", r)
	}
	r = c.Exec(ctx, mustCmd(t, `incr tedis:exec:n`))
	if r.Kind != "int" || r.Text != "1" {
		t.Fatalf("incr: %+v", r)
	}
	r = c.Exec(ctx, mustCmd(t, `lrange tedis:exec:list 0 -1`))
	if r.Kind != "array" || r.Text != "(2 items)\n1) a\n2) b" {
		t.Fatalf("lrange: %q", r.Text)
	}
	r = c.Exec(ctx, mustCmd(t, `get tedis:missing:key`))
	if r.Kind != "nil" {
		t.Fatalf("nil: %+v", r)
	}
	r = c.Exec(ctx, mustCmd(t, `get`)) // wrong arity
	if r.Kind != "error" {
		t.Fatalf("error: %+v", r)
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
