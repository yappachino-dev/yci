# Configuration System

How `yci` resolves settings: layered sources, strict parsing, and the
`yci config` command group for inspecting and changing values.

- **Status:** implemented
- **Core package:** `internal/config` (`config.go`, `keys.go`)
- **CLI surface:** `internal/cli/config.go`, flags in `download.go` / `sync.go`
- **Related:** `internal/app/app.go` (consumes resolved `config.Config`)

---

## Layering and precedence

Four layers, later wins:

1. **Built-in defaults** — e.g. library at `$HOME/Music/Managed`, delay `5:30`
   (`config.DefaultDelay`).
2. **Config file** — TOML, discovered via `os.UserConfigDir()`:
   `$XDG_CONFIG_HOME/yci/config.toml`.
3. **Environment** — `YCI_LIBRARY_PATH`, `YCI_STAGING_PATH`,
   `YCI_DOWNLOAD_DELAY`, `YCI_DOWNLOAD_COOKIES`, plus
   `YCI_CONFIG_PATH` (see below).
4. **Flags** — only where a flag exists (`--delay`, `--cookies`). Registered
   with empty defaults so "unset" means "fall through to config".

Resolution is one-shot translation at boot (`config.Load`), not storage: the
resolved `Config` struct flows into `app.NewContainer`, which derives
`fs.AppPaths` from it. There is no global keeper; availability comes from DI.

## `YCI_CONFIG_PATH` relocates the whole profile

The config file *and* the database live in the same directory: `ConfigDir()`
is the parent of the resolved config file path. Pointing `YCI_CONFIG_PATH`
at `/other/profile.toml` therefore moves `data.db` too — useful for tests,
sandboxing, or a second profile.

Missing-file semantics differ by intent:

- Default location absent → fine, first run.
- Explicit `YCI_CONFIG_PATH` absent → loud error.

## Keys

| Key | Env var | Validation | Notes |
|---|---|---|---|
| `library_dir` | `YCI_LIBRARY_PATH` | absolute path | |
| `staging_dir` | `YCI_STAGING_PATH` | absolute path | |
| `download.delay` | `YCI_DOWNLOAD_DELAY` | `min:max`, non-negative ints | parsed by shared `config.ParseDelay` |
| `download.cookies_file` | `YCI_DOWNLOAD_COOKIES` | absolute path | existence checked at download time, not load time |

Validation lives with the key in a small registry (`keySpec`). Adding a key
means adding one registry entry — `yci config list`, shell completion, `get`,
and `set` all learn about it automatically. That derivation is deliberate:
listing and validation cannot drift from what the parser accepts.

## Strict parsing

Hand-edited files are decoded strictly: unknown keys are an error naming the
offending key (`MetaData.Undecoded()`), because a typo'd key otherwise
silently does nothing. This cuts both ways — it also means a config written
by a *newer* yci fails on an older binary with a clear message rather than
mysterious behavior.

## The `yci config` command group

- `yci config path` — print the resolved file location (creates nothing).
- `yci config list` — every key, its resolved value, and which layer supplied
  it (default/file/env). Derived from the registry.
- `yci config get <key>` — one key's resolved value + source.
- `yci config set <key> <value>` — validated write; creates the file (and
  directory) on first use.

The whole subtree skips database boot: it operates purely on settings.

Two documented limitations of `set`:

1. **Comments are dropped.** TOML marshaling cannot represent comments, so
   any rewrite loses them. The command warns on every invocation. (Line-level
   surgical editing was considered and rejected as fragile.)
2. **Env vars still win.** Setting `library_dir` while `YCI_LIBRARY_PATH` is
   exported updates the file but not the effective value. `config get` shows
   the source so this is diagnosable rather than mysterious.

Writes round-trip through a generic `map[string]any` — never through the
typed `File` struct — so keys this version doesn't model (e.g., written by a
newer yci) survive untouched.

## Deliberate non-features

- **No `config generate --preset minimal|full`.** Presets differing only in
  commented-out lines are fake choice; generated templates would be
  hand-maintained strings duplicating parser behavior (guaranteed drift);
  BurntSushi can't emit comments anyway. `config list` provides honest,
  derived discoverability instead.
- **No `retry_limit` config key yet.** The attempt cap mirrors a SQL
  predicate (`attempts < 3`) — changing it requires coordinated Go const and
  query changes, not a config entry.
- **No filename-template keys.** `tracks.relative_path` already persists the
  rendered outcome; a template column would be derivable state inviting
  drift. If per-library templates ever arrive, that's a `yci relayout`
  command (re-render paths from metadata + template via journaled moves),
  not a config surface.

## Numeric keys caveat

Precedence uses `cmp.Or(env, file, default)`, which treats the zero value as
unset. Fine for strings; a future numeric key must be modeled as a pointer
(or keep string-typed in TOML and parse at the edge) so `0` isn't mistaken
for "not configured".
