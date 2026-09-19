# tedis

A compact, keyboard-driven Redis TUI — feature-modeled after [Medis 2](https://docs.getmedis.com), in a single Go binary.

```
┌ local ────────────────┐┌ keys · users* · 2501 ───────────────┐┌ users:1635 · string ───┐
│*                  2.5k ││key               type    ttl    size ││type       string       │
│▸ users            2.5k ││users:1635   string         —     48B ││encoding  embstr        │
│▸ session            501 ││users:2995   …                        ││ttl        —            │
 ❯ query (phase 3)
local · db0 · Redis 8.0 · 4.6k keys · 182µs        c conn  : query  / filter  r rescan …
```

## Features

- **All key types**: string / hash / list / set / zset / stream — view, edit, add, delete; RedisJSON planned
- **Key tree**: `:`-namespace tree with configurable separator & fold level; counts update live
- **Millions of keys**: cursor `SCAN` in the background (never `KEYS`), cancellable, windowed list rendering
- **Command query** (`:`): Medis-grammar lexer (quotes, `\n`, multiline args), read/write coloring driven by the server's `COMMAND` table, `^R` runs line/selection, persistent history
- **Alert mode** (`a`/`^A`): write commands require explicit confirmation
- **Encoders**: JSON / MessagePack / gzip / PHP serialize auto-detection, `v` cycles codecs, custom `encoder_*` scripts using Medis' base64-stdio protocol, per-key content rules (glob + type)
- **Connections**: direct, TLS (CA / insecure), SSH tunnel (agent, key, password, `~/.ssh/config`), Redis Cluster
- Delete confirmation per profile, TTL edit, rename, `0-9` db switching, INFO page, dark/light themes, en/zh-CN UI

## Install

```sh
go install tedis@latest        # or: brew install (tap pending)
```

## Usage

```sh
tedis                                   # connection manager (c)
tedis -c local                          # saved profile
tedis -u redis://127.0.0.1:6379/2       # URL (rediss:// implies TLS, ?cluster=true)
tedis -u redis://10.0.0.5:6379 --ssh ec2-user@jump -i ~/.ssh/prod.pem
```

## Keys

| context | keys |
|---|---|
| global | `Tab` panes · `c` connections · `:` query · `/` filter (substring; `* ?` for glob) · `r` rescan · `i` info · `s` settings · `0-9` db · `a` alert · `?` help · `q` quit |
| key list | `⏎` open · `d` delete · `t` ttl · `m` rename |
| value pane | `e`/`⏎` edit · `d` delete item · `n` new · `v` codec · `,`/`.` page |
| query page | `^R` run line/selection · `^A` alert · `^P`/`^N` history · `Esc` close |
| modals | `y`/`n` confirm · `Ctrl+S` save editor · `Esc` cancel |

## Custom encoders

Drop executable `encoder_<Name>*` scripts into `~/.config/tedis/encoders/`. They receive Base64 on stdin with `decode`/`encode` as argv and reply Base64 on stdout — byte-identical to Medis, so existing encoder scripts work as-is.

## Development

```sh
make build test   # unit + miniredis; conn tests auto-skip without a local redis
make smoke SMOKE_ARGS='-u redis://127.0.0.1:6379'
```

Layout: `internal/conn` (dialing, TLS, SSH, cluster, exec) · `scanner` (SCAN + prefix tree) · `keyview` (per-type IO) · `cmdquery`/`cmdtable` (lexer + rw classification) · `encode` (codecs + rules) · `app` (tview UI).
