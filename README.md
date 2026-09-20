<div align="center">

# tedis

**The Redis GUI that lives in your terminal.**

Fast · keyboard-first · single binary · zero deps

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go)](https://go.dev)
[![Platforms](https://img.shields.io/badge/platform-linux%20%7C%20macOS-lightgrey)](https://github.com/itbaby/tedis/releases)
[![Release](https://img.shields.io/github/v/release/itbaby/tedis?color=brightgreen)](https://github.com/itbaby/tedis/releases)

</div>

---

Medis-quality browsing without the Electron bundle. `tedis` gives you a
three-pane Redis workbench — namespace tree, key list, live value inspector —
that starts in milliseconds, survives SSH hops, and happily handles millions
of keys. Every action is one keystroke away.

```
┌ local ────────────────┐┌ keys · users* · 2501 ─────────────────┐┌ users:1635 · string · json · 4 ┐
│*                  2.5k ││key              type   ttl    size    ││                                  │
│▸ users            2.5k ││users:1635  string    52m    48B       ││▾ profile {                       │
│▸ session           501 ││users:2995  …                          │││   name   "Ada Lovelace"         │
│                        ││                                       │││   roles  ["admin", "dev"]       │
│                        ││                                       │││ ▸ prefs  {"theme": "dark"}      │
└────────────────────────┘└───────────────────────────────────────┘││   logins 42                     │
                                                                    │▸ orders  [3 items]                │
local · db0 · Redis 8 · 4.6k keys · 182µs                           └──────────────────────────────────┘
c conn  : query  / filter  r rescan  i info  s settings  a alert:off  ? help  q quit
```

## Why tedis?

- **Blazing** — a static Go binary, no browser engine, no runtime. Sub-ms UI,
  182 µs round trips on localhost.
- **Scales to millions of keys** — background cursor `SCAN` (never `KEYS`),
  cancellable, windowed rendering; namespace tree with live counts.
- **Structured values, not blob dumps** — JSON / MessagePack / gzip / PHP
  serialize are auto-detected and rendered as an expandable tree with
  one-line inline previews (`["admin", "dev"]`), or flip to a jsoncrack-style
  node-graph with `g`.
- **Edit what you selected, not the whole payload** — open any row with `⏎`
  and tedis edits *that leaf*, merging it back into the document losslessly
  (key order preserved, re-encoded through the original codec, even renames).
- **Binary-safe** — protobuf, gob, images? A custom `encoder_*` script plugged
  into `~/.config/tedis/encoders/` decodes them in place — byte-compatible
  with Medis' encoder protocol, so existing scripts just work.
- **Works wherever your shell works** — TLS, SSH tunnels (agent / key /
  password / `~/.ssh/config`), Redis Cluster, multiple DBs (`0-9`).

## Features

| | |
|---|---|
| ⌨️ **Query console** `:` | Medis-grammar lexer (quotes, `\n`, multiline args), read/write coloring from the server's `COMMAND` table, `^R` runs the line or selection, persistent history |
| 🚦 **Alert mode** `a` | every write command demands an explicit confirmation — prod-safe by default |
| 🌲 **Key tree** | `:`-separated namespaces, configurable separator & fold level, fuzzy filtering (`/`, fzf-style ranking), glob when you type `*`/`?` |
| ✂️ **Row-level editing** | edit / add / delete items in hashes, lists, sets, zsets, streams and nested JSON — with rename and TTL-preserving writes |
| 🕸 **Graph view** `g` | visualize any document as connected node cards |
| 🗂 **12 panes in one screen** | three-pane layout, `Tab` / `⌘←` `⌘→` to jump, live meta (type, TTL, size, encoding) |
| 🌗 **Polish** | dark/light themes, en / zh-CN UI, connection profiles, delete-confirm toggle, INFO dashboard |

## Install

Download a prebuilt binary from the
[latest release](https://github.com/itbaby/tedis/releases) — `linux` and
`macOS`, `amd64` and `arm64`:

```sh
tar -xzf tedis_linux_amd64.tar.gz && sudo mv tedis /usr/local/bin/
```

Or build from source (requires Go 1.27+):

```sh
git clone https://github.com/itbaby/tedis && cd tedis && make build
```

## Quick start

```sh
tedis                                   # connection manager — press c
tedis -c local                          # connect a saved profile
tedis -u redis://127.0.0.1:6379/2       # or a URL (rediss:// implies TLS, ?cluster=true)
tedis -u redis://10.0.0.5:6379 --ssh ec2-user@jump -i ~/.ssh/prod.pem
```

Then: type `/` to filter, `⏎` to drill into a value, `e` to edit the selected
row, `:` for raw commands, `?` for help. That's the whole onboarding.

## Keybindings

| context | keys |
|---|---|
| global | `Tab` cycle panes · `⌘/⌥←→` jump panes · `/` filter · `:` query · `r` rescan · `i` info · `s` settings · `c` connections · `0-9` switch db · `a` alert mode · `?` help · `q` quit |
| key list | `⏎` open · `d` delete · `t` ttl · `m` rename · `y` copy |
| value pane | `⏎` fold branch / edit leaf · `e` edit row · `d` delete item · `n` new item · `v` codec · `g` graph · `y` copy · `,`/`.` page |
| query | `^R` run line/selection · `^A` alert · `^P`/`^N` history · `esc` back to browser |
| modals | `tab`/`⏎` next field · `⌃S` save · `esc` cancel |

## Custom encoders

Drop an executable `encoder_<Name>*` into `~/.config/tedis/encoders/`. It
receives Base64 on stdin with `decode`/`encode` as argv and replies Base64 on
stdout — byte-identical to the Medis protocol, so existing Medis encoder
scripts work as-is.

## Development

```sh
make build test    # unit + miniredis-backed integration; conn tests auto-skip without local redis
make seed          # load demo:* keys covering every type & codec
make smoke SMOKE_ARGS='-u redis://127.0.0.1:6379'   # tmux render check
```

## Credits

Heavily inspired by [Medis 2](https://docs.getmedis.com) — its feature set,
reimagined for people who live in a terminal.
