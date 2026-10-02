# yci Style Guide

Production-grade conventions for this repository. Derived from how the project
is written, corrected toward what a senior Go engineer would enforce in review.
Rules here are binding for new code; known deviations are tracked in
`docs/technical-debt.md` with remediation plans.

Related reading: [`technical-debt.md`](technical-debt.md) (known flaws and
fixes), [`testing-go.md`](testing-go.md) (testing standards),
[`fs-journal.md`](fs-journal.md) (filesystem durability design).

---

## 1. Project layout & layering

```text
cmd/cli/            main.go only — composition root entrypoint
internal/
  appctx/           transaction propagation through context
  app/              DI container (NewContainer wires everything)
  audit/            library consistency checks
  cli/              cobra commands, one file per command group
  db/               transactor, migrations runner, filesystem action journal
  download/         queue-based yt-dlp orchestration
  fs/               physical filesystem primitives (mover, reader, paths)
  library/          core domain: folders, artists, channels, tracks, staging
  store/            sqlc-generated code — never edited by hand
db/
  migrations/       goose, sequential NN_name.sql, paired Up/Down
  queries/          sqlc query files, one per table/domain
docs/               design docs, style guide, debt register
```

Rules:

- **Dependencies point inward**: `cli → app → {domain, db, fs} → store`.
- Domain packages never import each other concretely; they declare minimal
  consumer-side interfaces (`download` consumes `library.TrackService` only
  through `TrackChecker`).
- **`main` contains no logic.** All wiring lives in `internal/app/app.go`.
- **Generated code is generated.** `internal/store/*` comes from
  `go tool sqlc generate`; hand edits are forbidden. Fix `.sql` sources and
  regenerate. Regeneration must produce zero diffs in CI (drift = broken build).

## 2. Design principles

### Make correct usage automatic, incorrect usage impossible

Abstractions must *enforce* invariants, not document them:

- The transactor refuses/joins nested transactions instead of failing at the
  SQLite driver layer.
- The journal executor treats replayed operations as success by construction —
  callers cannot forget idempotency.
- Constructors validate required collaborators; services never run with nil
  dependencies discovered at call time.

If a function can be called wrongly and fail three layers deep, move the check
to the boundary that accepts the input.

### One atomic boundary per operation, designed up front

Any operation spanning two consistency domains (DB + disk, DB + network) gets
its bridge designed before either side is written — journal/outbox for disk,
retry/idempotency keys for network. Physical side effects inside a transaction
are forbidden (see `docs/fs-journal.md`).

### Interfaces: accept them, return structs

- Define interfaces where consumed, at the smallest surface actually used.
- Return concrete types from constructors and repositories.
- An interface with exactly one implementation is allowed **only** when it
  marks a genuine seam (storage, clock, network). Two parallel interfaces
  served by one implementation is drift — merge them.
- Optional collaborators get no-op defaults defined next to the interface,
  kept in sync with it (compile-time assertion: `var _ EnqueueReporter =
  noOpReporter{}`).

### Dependency injection

- Constructor injection via config struct is justified when there are **3+
  collaborators or optional ones**; a single-dependency service takes the
  dependency directly (`NewChannelService(repo ChannelRepository)`).
- No package-level mutable state outside `cmd/`. The DI graph flows through
  constructor parameters and `context.Context`, never globals.

## 3. Types & naming

### Domain types over primitives

Identity and status values get named types with validation:

```go
type TrackID string

type FolderName string

func (n FolderName) Validate() error { ... }
```

**Uniformly applied** — including statuses (`QueueStatus`), external IDs, and
paths. If two states are compared by string literal, they are an enum waiting
to break. Enums are typed `iota` constants.

### Signatures

- More than ~3 related parameters become a params struct
  (`TrackCreateParams`); aggregated results become result structs
  (`EnqueueResult`).
- Receivers: `s` services, `r` repositories, `m` movers, `c` commands.
- No stutter: `library.Track`, field `channel.Name`.
- Constants live in grouped blocks; repeated literals become named constants
  with the flag/command wiring.

### Package hygiene

- Names must be unique across the import graph where practical — a type named
  `StagingConfig` meaning different things in two packages is a defect, not a
  namespacing feature. Prefix by role instead (`PathsProvider`,
  `LibraryConfig`).
- Avoid `Config.Config`, `Service.Service`-style fields; name fields after
  what they provide (`Paths`, `Tracks`).
- Watch aggregate size: when one package owns 5+ aggregates plus shared
  infrastructure, split by responsibility.

## 4. Error handling

- Wrap with `%w` and lowercase context at every layer that adds meaning:
  `fmt.Errorf("move staging file to library: %w", err)`.
  Pattern: `<action> <target> <qualifier>: <cause>`. Phrases are consistent
  across the codebase ("read staging directory", not sometimes-"staged").
- Sentinel errors (`ErrProcessAborted`) for control flow; checked via
  `errors.Is`. Typed errors carrying data get `errors.As`.
- `sql.ErrNoRows` never leaks upward, but its translation depends on whether
  absence is a legitimate answer. A *lookup* asked "does this exist?" returns
  `(zero, false, nil)` — see `FindArtistID`. A *fetch* asked for a specific
  entity returns that entity's not-found sentinel via `db.MapSQLError` — see
  `GetTrack`. Choosing wrongly either hides a real failure or turns a normal
  empty result into an error.
- Errors are handled once: logged **or** returned, never both, never `_ =`
  except best-effort cleanup with a comment explaining why failure is
  acceptable.
- Error text is user-facing in this CLI — write it for the person reading the
  terminal: `%q` around identifiers, no punctuation at the end.

## 5. Context, transactions & lifecycle

- `ctx context.Context` first parameter on every exported method that does I/O.
- Long loops poll `ctx.Err()` between iterations.
- Transactions: exactly one `WithinTransaction` per logical operation;
  re-entrant calls join the active transaction (transactor guarantees this).
- Post-commit work goes through explicit hooks (`SetPostCommit`), never
  goroutines spawned inside transactions.
- Process shutdown closes the DB (`PersistentPostRun`) and drains journals —
  cleanup paths are code-reviewed like happy paths because they run on every
  invocation.

## 6. SQL & migrations

- goose migrations: paired Up/Down, `StatementBegin`/`StatementEnd` around
  multi-statement bodies, sequential numbering. Down fully reverses Up.
- Migrations are **append-only**. Never edit one that has been applied: the
  applied version is not recoverable from the file, and recovery is by
  snapshot rather than by running Down. Add a new numbered migration.
- Integrity lives in schema: NOT NULL, CHECK enums, UNIQUE, FKs with explicit
  ON DELETE behavior. Application-level validation supplements, never
  replaces.
- sqlc queries: named parameters, one intent per query name, qualified columns
  whenever more than one table/CTE alias is in scope.
- Lint SQL with sqlfluff (config in `.sqlfluff`: uppercase keywords,
  4-space indent). The config exists; no gate runs it yet, so this is a
  convention rather than something CI will catch.

## 7. CLI layer

- Commands: package-level `cobra.Command` vars, one file per group, thin
  RunE bodies — parse flags, call service, print. Business logic lives in
  domain packages.
- Flag names as constants; **no shorthands at all**, even for flags used
  constantly.
- Interaction via charm stack (`huh`).
- Columnar output only through the shared `Table` in `internal/cli/table.go`,
  which pads from `runewidth.StringWidth`; all cell limits and truncation are
  in display cells. `text/tabwriter` is banned: it aligns on bytes, which
  misaligns CJK. Counting runes is equally wrong.
- `%q` for identifiers in messages.
- Version comes from build flags (`-ldflags "-X main.version=..."`), not a
  source constant.
- Dependencies reach commands through the container carried on
  `cmd.Context()` or command structs — not package variables.

## 7a. JSON output

`--json` is a published contract, not a dump of internal state.

**Why not reflect the domain structs.** Reflecting them would tie the
published shape to Go field names, so renaming a field would silently break
every script reading yci's output. The cost is that the DTOs can drift from
the domain types, which is what `TestJSONOutputSchemaIsStable` exists to
catch.

**Why the table output is unusable for scripts.** Tables are padded with
spaces, not tabs:

```text
$ yci track list | cut -f1
5ead5f5e   nekomata-okayu       bRp8fX44icE   少女A / 猫又おかゆ
```

`cut` sees no tabs and returns the whole row, which is not a usable ID. And
list output prints an 8-character prefix, so the full ID cannot be recovered
from it at all. Human output therefore uses short IDs while JSON uses full
ones; the two are different presentations of one row.

**No `omitempty`, ever.** A consumer reading `.artist_id` should get an empty
string, not a missing key. Absence and emptiness are different things, and a
schema that changes shape based on the data is not a schema.

**Empty lists are `[]`, never `null`.** A script doing `.tracks[]` should not
need to guard for either.

**stdout carries the requested data;** advisory output goes to stderr, so
`yci track list --json | jq` works with warnings present.

## 8. Testing (binding)

Full standard in [`testing-go.md`](testing-go.md). Non-negotiables:

- Every bug fix lands with a regression test reproducing the bug.
- Every behavior change lands with tests; pure refactors may rely on existing
  coverage — which means coverage must exist.
- Storage tests run against real SQLite and real temp directories; mocks are
  reserved for genuinely external boundaries (network APIs).
- `make test` passes before any commit reaches the default branch.

## 9. Tooling, formatting & CI

- Imports: stdlib → external → `github.com/yappachino-dev/yci/internal/...`,
  each group separated.
- Gated pipeline (Makefile targets, enforced in CI):

```text
make fmt        # gofmt/goimports — no diff leaves the tree
make lint       # golangci-lint run
make generate   # go tool sqlc generate — zero git diff afterwards
make test       # go vet ./... && go test ./... -race
make build      # CGO_ENABLED=0 binary with ldflags version
```

- `-race` on every test run; CGO stays off (pure-Go sqlite driver).
- Spelling counts in identifiers users see or maintainers grep:
  `committed`, not `commited`; `reporter`, not `repoter`.

## 10. Documentation

- Doc comments on all exported symbols; complete sentences; explain *why* in
  bodies, *what* in signatures.
- Feature design docs in `docs/<feature>.md` with header block (status,
  migration, core packages, related files), indexed in `docs/README.md`.
  Updated in the same change as the code.
- Decision matrices (crash windows, idempotency rules) as tables, not prose.
- Generated snapshots of the codebase (Repomix-style bundles) are build
  artifacts — never committed.

### Comment length: short and precise

A comment earns its place by saying something the code cannot. The longer it
gets, the more certainly it is one of three other things:

- **Design rationale** — belongs in `docs/<feature>.md`. Two copies of a
  rationale drift, and the code copy is the one that goes stale silently.
- **A record of the bug that was fixed** — belongs in
  `docs/technical-debt.md`. The commit message already says what changed and
  why; a comment restating it is noise that survives long after the change
  stops being interesting.
- **A narrative of the edit** — "this used to be broken" tells the next reader
  nothing they can act on. State the invariant that now holds.

Rules:

- **Three lines is the ceiling for a comment inside a function body.** One or
  two is the target.
- **Exported godoc may run longer**, because a reader of the symbol has
  nowhere else to learn the contract. It states the contract and the
  non-obvious invariant, not the history.
- **A comment that needs a second paragraph is a signal to extract the
  knowledge**, not to write a longer comment: either a named constant, a type,
  or a doc. If the answer is neither, the comment is probably unnecessary.
- **No comment restates what the next line does.** `// increment the counter`
  beside `i++` is pure cost.

Enforced by `make comment-check`, which is part of `make check` and part of
the pre-commit hook. The four longest blocks found when this rule was written
were written *in the sessions that wrote the rule* — the failure mode was not
ignoring the limit but treating a fix's reasoning as if it belonged next to
the fix.

## 11. Commit conventions

Conventional Commits, lowercase imperative, scope = package:

```text
feat(download): implement interactive playlist enqueue
refactor(cli): tidy up the module
fix(sql): add null check to folder children query
```

- Separate behavior changes (`feat:`/`fix:`) from structural ones
  (`refactor:`/`chore:`).
- Scopes: `(cli)`, `(db)`, `(download)`, `(library)`, `(sql)`, `(store)`,
  `(app)`. Omit scope only for cross-cutting changes.
- Debt retirement is first-class: `refactor:` commits referencing items in
  `docs/technical-debt.md` are expected and welcome.
