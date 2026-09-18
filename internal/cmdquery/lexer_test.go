package cmdquery

import (
	"reflect"
	"testing"
)

func texts(toks []Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Text
	}
	return out
}

// The doc examples (docs.getmedis.com/reference/command-query.md) plus edge
// cases from the documented grammar.
func TestParseDocExamples(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		names []string
		args  [][]string
	}{
		{
			name:  "quoted with spaces",
			in:    `set keyname "content that contains a space"`,
			names: []string{"set"},
			args:  [][]string{{"keyname", "content that contains a space"}},
		},
		{
			name:  "single quoted",
			in:    `set keyname 'content that contains a space'`,
			names: []string{"set"},
			args:  [][]string{{"keyname", "content that contains a space"}},
		},
		{
			name:  "escaped quotes inside double quotes",
			in:    `set keyname "content that contains both spaces \"and\" quotes"`,
			names: []string{"set"},
			args:  [][]string{{"keyname", `content that contains both spaces "and" quotes`}},
		},
		{
			name:  "escaped newline",
			in:    `set keyname "content that contains a \n newline"`,
			names: []string{"set"},
			args:  [][]string{{"keyname", "content that contains a \n newline"}},
		},
		{
			name:  "escaped backslash",
			in:    `set keyname "content that contains a \\"`,
			names: []string{"set"},
			args:  [][]string{{"keyname", `content that contains a \`}},
		},
		{
			name:  "multiline eval",
			in:    "eval \"\nlocal a = 1\nlocal b = 2\nreturn a + b\n\" 0",
			names: []string{"eval"},
			args:  [][]string{{"\nlocal a = 1\nlocal b = 2\nreturn a + b\n", "0"}},
		},
		{
			name:  "multiple commands separated by newline",
			in:    "get a\nset b 1\n",
			names: []string{"get", "set"},
			args:  [][]string{{"a"}, {"b", "1"}},
		},
		{
			name:  "case insensitive name",
			in:    "GeT a",
			names: []string{"get"},
			args:  [][]string{{"a"}},
		},
		{
			name:  "tabs separate args",
			in:    "set\ta\tb",
			names: []string{"set"},
			args:  [][]string{{"a", "b"}},
		},
		{
			name:  "empty quoted arg kept",
			in:    `set k ""`,
			names: []string{"set"},
			args:  [][]string{{"k", ""}},
		},
		{
			name:  "quote inside bare word is literal",
			in:    `set k it's`,
			names: []string{"set"},
			args:  [][]string{{"k", "it's"}},
		},
		{
			name:  "blank lines skipped",
			in:    "\n\nget a\n\n\nget b\n",
			names: []string{"get", "get"},
			args:  [][]string{{"a"}, {"b"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmds, err := Parse(c.in)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(cmds) != len(c.names) {
				t.Fatalf("got %d cmds %+v, want %d", len(cmds), cmds, len(c.names))
			}
			for i, cmd := range cmds {
				if cmd.Name != c.names[i] {
					t.Errorf("cmd %d name=%q want %q", i, cmd.Name, c.names[i])
				}
				if !reflect.DeepEqual(texts(cmd.Args), c.args[i]) {
					t.Errorf("cmd %d args=%q want %q", i, texts(cmd.Args), c.args[i])
				}
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse(`set k "open`); err == nil {
		t.Error("unterminated double quote should error")
	}
	if _, err := Parse(`set k 'open`); err == nil {
		t.Error("unterminated single quote should error")
	}
	if _, err := Parse(`set k "trailing \`); err == nil {
		t.Error("trailing backslash should error")
	}
}

func TestParseQuotedFlagsAndLines(t *testing.T) {
	cmds, err := Parse(`SET k "v"` + "\n" + `GET k`)
	if err != nil {
		t.Fatal(err)
	}
	if !cmds[0].Args[1].Quoted {
		t.Error("quoted arg should carry Quoted=true")
	}
	if cmds[0].Line != 1 || cmds[1].Line != 2 {
		t.Errorf("lines: %d %d", cmds[0].Line, cmds[1].Line)
	}
	// command starting on a later line after blanks
	cmds, _ = Parse("\n\n\nget x")
	if cmds[0].Line != 4 {
		t.Errorf("start line = %d, want 4", cmds[0].Line)
	}
}
