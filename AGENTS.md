# AGENTS.md — Session Context

Runtime notes for AI assistants working in `yci`. Read this, then
`docs/README.md` for the index.

**This file is for things an assistant cannot look up.** The rule for what
belongs here: durable knowledge goes in `docs/`, progress goes in git history
and `docs/technical-debt.md`, and only working state, owner preferences and
project-specific guardrails stay in here. If a section grows into a changelog,
it has stopped belonging here.

## Ground truth

| Question | Answer |
|---|---|
| Authoritative docs | `docs/` — index in `docs/README.md` |
| Known flaws | `docs/technical-debt.md` — read before proposing architecture |
| Standards | `docs/style-guide.md`, `docs/testing-go.md`, `docs/error-handling-go.md` |
| What changed | `git log --oneline` — not this file |

## Project orientation

`yci` is a CLI for a local, Syncthing-mirrored music library: download →
staging → journaled SQLite import. The owner studies Go by building it, and
self-studies from `docs/*.md` between sessions.

## Working agreements

- **Criticism**: honest and specific is wanted. Praise is useful only when
  precise. Report weak points in your own work rather than waiting to be
  asked.
- **Autonomy**: implement features end to end — spec, code, tests, docs,
  commits. Do not hand work back unprompted. Informal mid-implementation
  checks ("how am I doing?") are still answered as a quick diff read and a
  verdict. `docs/mentoring.md` describes an earlier review loop that is now
  dormant, kept as a record.
- **New debt** found in any form lands in `docs/technical-debt.md` as a
  numbered entry. Never chat-only.
- **Git**: Conventional Commits, subject line only, no body. One logical
  change per commit. Commit what the change touched — code, tests, generated
  `internal/store`, `AGENTS.md` — as one commit; never ship code without the
  regenerated store. `docs/` is gitignored: keep it current as part of the
  work, but it is not part of any commit until the owner decides otherwise.
- **Editing**: use the `edit` tool for targeted changes; scripted bulk edits
  only for genuinely mechanical multi-file rewrites. A scripted edit has
  already destroyed two register entries here — read back what you wrote.
- **Teaching material** goes in `docs/*.md`, not in code comments.

## Verification gates

Run before reporting a change complete:

```
make check          # gofmt, vet, build, test, sqlc drift
```

`make hooks` installs a pre-commit hook that runs the same gates with
`-race`; CI (`.github/workflows/ci.yml`) runs them on push and PR. A clean
`go test` does not prove the generated code matches `db/queries/**` — only the
sqlc drift check does.

After touching `db/queries/**`: `go tool sqlc generate`, then confirm the
diff in `internal/store/` contains only expected changes. Hand-editing
`internal/store/` is forbidden.

## Guardrails

These are decisions, not suggestions. Changing one is a deliberate act that
belongs in the register.

### Library and playlists

- Database membership is the single source of truth. No dynamic rules, and no
  automatic membership changes at import time.
- `.m3u` files under `<library_dir>/playlists/` are generated artifacts using
  relative paths. They follow renames where possible; the database does not
  depend on them.
- Exclusions belong in the seed recipe, not in a one-time UI action — a
  reseed would otherwise undo them.
- Fragment matching is lexical and static: token equality, with the whole
  selection also accepted concatenated. It cannot unify kanji and romanized
  aliases, and that limitation is accepted, not a bug to fix silently.

### Filesystem

- Never rename existing files. Syncthing identifies them by content hash.
- Never rewrite embedded audio tags. `retitle` changes the database only.
- Library directories `0755`; config directory `0700`; config files `0600`.
- `make sandbox` before any database or migration experiment.

### Database

- Migrations are append-only. Never edit an applied migration.
- Snapshots are written before pending migrations, atomically, and an existing
  recovery point is never overwritten or allowed to block a migration.
- `foreign_keys`, `busy_timeout` and `journal_mode` are per-connection DSN
  settings (`db.DSN`, `db.Open`). One-time pragmas on a `*sql.DB` are unsafe.

### CLI

- No flag shorthands.
- Output through `cmd.Print*` / `cmd.Printf` / `cmd.Println`, never
  `fmt.Print*`. JSON goes to stdout; advisory output goes to stderr.
- One shared `Table` dialect. Measure with `runewidth.StringWidth`; all cell
  limits and truncation are in display cells. `text/tabwriter` aligns on
  bytes and is banned; rune counts are wrong for CJK.
- Exact IDs beat longer prefixes; an ambiguous prefix lists candidates rather
  than guessing.
- Human output uses short IDs; JSON uses full IDs.

### JSON contract

An explicit, versioned contract, not a dump of internal structs:

- Hand-written DTOs. No reflection over domain structs.
- Stable snake_case field names. No `omitempty`. Empty lists emit `[]`.
- The schema is pinned by `TestJSONOutputSchemaIsStable`.

### Configuration

Precedence is **flag > env > file > built-in default**. Unknown TOML keys fail
loudly. Values are validated on write. `config set` preserves unknown keys and
loses comments.

### Errors

Shared `db.MapSQLError` with per-entity sentinels, mapped to one-line messages
at the CLI boundary. Wrap with `%w` and add context at each layer. See
`docs/error-handling-go.md`.

### Comments

`docs/style-guide.md` §10: three lines maximum inside a function body; design
rationale in `docs/<feature>.md`; history in `docs/technical-debt.md`. Run
`make comment-check` before committing and do not introduce new violations.
The command currently fails on pre-existing blocks; that is recorded in the
register, not a reason to stop.

### Testing

`docs/testing-go.md` is binding. Two rules earned the hard way:

- **Inject external tool execution.** `exec.CommandContext` called from a
  package-level function is why the download queue loop went untested — and
  untested code there was also wrong.
- **Mutation-check new regression tests.** Confirm the test fails when the fix
  is reverted. A test that passes against the bug is worse than no test.

## Environment quirks

- Exercise the real binary with `HOME=/tmp/sandbox
  XDG_CONFIG_HOME=/tmp/sandbox/.config` so migrations and journals never touch
  real user data.
- `go test ./...` does not modify the real database. Verified by checksum; if
  you add a test that reaches `~/.config/yci`, that is a bug.
