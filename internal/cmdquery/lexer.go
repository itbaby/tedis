// Package cmdquery implements the command-line query language. The grammar
// mirrors Medis' command query view exactly (docs.getmedis.com):
//
//   - arguments are bare words, "double quoted" or 'single quoted'
//   - inside double quotes: \n expands to newline, \" escapes a quote,
//     \\ escapes a backslash; single quotes have no escapes
//   - a quoted argument may span multiple lines (newlines inside quotes are
//     literal argument content, not command separators)
//   - unquoted newlines separate commands
//   - command names are case-insensitive (returned lowercased)
package cmdquery

import (
	"fmt"
	"strings"
)

// Token is one argument.
type Token struct {
	Text   string
	Quoted bool // was wrapped in quotes (drives highlighting)
}

// Command is one parsed command line.
type Command struct {
	Name string // lowercased
	Args []Token
	Line int // 1-based source line where the command starts
}

// Parse splits input into commands. It returns an error for unterminated
// quotes or a trailing backslash. Blank lines are skipped.
func Parse(input string) ([]Command, error) {
	var (
		cmds      []Command
		cur       *Command
		arg       strings.Builder
		inQuote   bool
		quoteCh   rune
		tokQuoted bool
		line      = 1
	)

	flushArg := func() {
		if arg.Len() == 0 && !tokQuoted {
			return
		}
		if cur == nil {
			cur = &Command{Line: line}
		}
		if cur.Name == "" {
			cur.Name = strings.ToLower(arg.String())
		} else {
			cur.Args = append(cur.Args, Token{Text: arg.String(), Quoted: tokQuoted})
		}
		arg.Reset()
		tokQuoted = false
	}
	flushCmd := func() {
		flushArg()
		if cur != nil {
			cmds = append(cmds, *cur)
			cur = nil
		}
	}

	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case inQuote:
			switch r {
			case quoteCh:
				inQuote = false
			case '\\':
				if quoteCh == '\'' {
					arg.WriteRune(r) // single quotes: no escapes
					break
				}
				if i+1 >= len(runes) {
					return nil, fmt.Errorf("line %d: trailing backslash", line)
				}
				i++
				switch runes[i] {
				case 'n':
					arg.WriteRune('\n')
				case '"', '\\':
					arg.WriteRune(runes[i])
				default:
					arg.WriteRune('\\')
					arg.WriteRune(runes[i])
				}
			case '\n':
				arg.WriteRune(r)
				line++
			default:
				arg.WriteRune(r)
			}
		case r == '"' || r == '\'':
			if arg.Len() == 0 && !tokQuoted {
				inQuote, quoteCh, tokQuoted = true, r, true
			} else {
				arg.WriteRune(r) // quote inside a bare word is literal
			}
		case r == ' ' || r == '\t':
			flushArg()
		case r == '\n':
			flushCmd()
			line++
		default:
			arg.WriteRune(r)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("line %d: unterminated quote (missing %c)", line, quoteCh)
	}
	flushCmd()
	return cmds, nil
}
