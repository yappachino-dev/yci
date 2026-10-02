# Error Handling Standard

How this codebase handles and words errors, grounded in its real messages
(quoted verbatim). Rules here are binding for new code; §7 is the review
checklist. [`style-guide.md`](style-guide.md) §4 codifies the layering;
[`technical-debt.md`](technical-debt.md) tracks known violations.

This document describes project conventions. It does not teach Go's error
machinery; where a rule needs justification, the reason is given rather than
a general principle.

---

## 1. The rule

Every error crossing a layer boundary is one of four things, and each call
site must pick deliberately:

1. **Handled** — recovered, retried, substituted, or translated into a domain
   outcome. `sql.ErrNoRows` is the case to get right: a lookup returns
   `(zero, false, nil)`, a fetch returns a not-found sentinel (§3).
2. **Contextualized and passed up** — `fmt.Errorf("move staging file to
   library: %w", err)`.
3. **Passed up unchanged** — the caller above already has enough meaning.
4. **Deliberately swallowed** — best-effort cleanup only, always with a
   comment justifying why failure is acceptable.

Exactly one layer *handles*; the others *transport*. Doing both
(log-and-return) duplicates the report; doing neither is a silent drop — which
is how `Enqueue` once discarded unrecognized URLs, leaving a bad playlist to
enqueue nothing for months with no output to explain it.

The chain that reaches the user must read like one sentence written by
several people, each adding exactly one clause:

```text
create artist "radiohead": folder fallback for artist "radiohead": sql: no rows in result set
└── what I was doing        └── how I tried to do it                └── ground truth
```

## 2. Wrapping mechanics

```go
if err != nil {
    return fmt.Errorf("resolve path %q: %w", id, err)
}
```

- **Always `%w`** when the cause matters to anyone up-chain (almost always).
  `%v` severs the chain: `errors.Is`/`errors.As` stop working. Use `%v` only
  when deliberately summarizing a secondary error, e.g. reporting a rollback
  failure *alongside* the original: `"rollback transaction (original error:
  %v): %w"` — here `err` is the payload, `rbErr` is metadata.
- Wrap at the level that adds meaning. A clause that answers no new question
  is noise.
- Leaf errors need no wrapping: `fmt.Errorf("channel name cannot be empty")`
  is complete on its own.

### Inspecting wrapped chains

```go
var notFound *NotFoundError
switch {
case errors.Is(err, ErrProcessAborted):   // sentinel identity
case errors.As(err, &notFound):           // typed payload
}
```

Never match on strings. `strings.Contains(err.Error(), "no rows")` breaks the
moment any layer rephrases.

## 3. Sentinels & typed errors

Two tools for "what kind of failure is this?" decisions:

**Sentinel** — one identity, no data:

```go
var ErrNotFound = errors.New("not found")
```

**Typed** — identity plus structured fields:

```go
type ConflictError struct{ Entity, ID string }
func (e *ConflictError) Error() string {
    return fmt.Sprintf("%s %q already exists", e.Entity, e.ID)
}
```

Each domain package carries its own sentinel set (`ErrArtistNotFound`,
`ErrFolderNotFound`, `ErrTrackNotFound`, …), translated from storage failures
by `db.MapSQLError` inside the SQLite repositories. Driver errors no longer
reach user-facing output (§5).

Sentinels live in the file of the entity that returns them, not in a central
`errors.go` — `ErrArtistNotFound` sits in `library/artist.go`. The pattern when
adding an entity:

```go
// library/folder.go
var (
    ErrFolderNotFound = errors.New("folder not found")
)
```

Repositories translate at the boundary:

```go
func (r *SQLiteArtistRepository) ByID(ctx context.Context, id ArtistID) (Artist, error) {
    row, err := r.db(ctx).GetArtist(ctx, string(id))
    if errors.Is(err, sql.ErrNoRows) {
        return Artist{}, ErrArtistNotFound
    }
    if err != nil {
        return Artist{}, fmt.Errorf("get artist %q: %w", id, err)
    }
    ...
}
```

Callers then branch on meaning instead of driver trivia, and users never see
the literal text `sql: no rows in result set`.

## 4. Message craft

Format: `<action> <target> <qualifier>: <cause>`, all lowercase, no trailing
period, `%q` around identifiers.

| Quality | Weak | Strong |
|---|---|---|
| Verb names the operation | `failed tracking artist folder target` | `resolve artist target folder path` |
| Says what failed, not that failure happened | `error: --name requires --id` | `--update-id-slug requires --name to be provided` |
| Identifies the subject | `execute batch tracking update for entry %q` | `update track path %q` |
| One clause per layer | `could not find artist with ID "x": artist by id "x": …` | `artist by id "x": artist not found` |

Rules distilled from the codebase's best messages:

- Prefer present-tense verbs over "failed to…" — the chain already implies
  failure; spend characters on *what*. ("delete folder", not "failed to
  delete folder".)
- No `error:` prefixes — cobra prints `Error:` for you.
- Punctuation lives inside sentences, not after them: `"no URLs provided:
  pass links as arguments or use --file flag"`, not mid-message periods.
- One vocabulary: the directory is `staging` everywhere — not "staged dir"
  in one file and "staging directory" in another. Reuse an existing noun
  before inventing one.
- Copy-paste is where vocabulary goes to die: `folder.go` says
  `"all artists: %w"` inside `AllIDs` for folders; `root.go` says
  "initialize application container statements" about a container that holds
  no statements. When cloning a block, re-read the words.

## 5. Layer responsibilities

| Layer | Adds | Never adds |
|---|---|---|
| Repository | low-level detail (`get artist %q`) | business intent |
| Service | intent (`update artist %q`) + translates driver/sentinel boundaries | duplicate IDs already named below |
| CLI | nothing (usually) — the chain is already user-readable | re-wrapped restatements of the service clause |

The double-wrap trap, as it read before sentinels existed:

```text
could not find artist with ID "x": artist by id "x": sql: no rows in result set
```

Three clauses, two of them identical. With sentinels (§3) the same failure
becomes

```text
create channel: link lookup: artist by id "x": artist not found
```

or, better, the CLI handles `ErrArtistNotFound` and prints one friendly line.

Rule of thumb before returning a wrapped error: read the full chain you are
about to produce. If any fact appears twice, delete your clause.

## 6. Anti-pattern catalog

| Anti-pattern | Seen at | Why it hurts | Instead |
|---|---|---|---|
| Silent drop | `Enqueue` switch, unknown URL type | work vanishes without trace; sync enqueued nothing for months | count it, report it, fail it |
| Log/print **and** return | `ProcessStaging`: `fmt.Printf("Error processing…")` then continue | acceptable loop pattern, but never combine with also returning the same err upward | choose per-error: handle OR return |
| Discarding the cause | — (none; `deps.go` wrapped `exec.LookPath` and now returns `ErrDependencyMissing`) | loses "is it installed vs PATH issue" distinction | wrap with `%w` or branch on `errors.Is` |
| String matching | — (none yet — keep it that way) | brittle to rewording | `errors.Is`/`As` + sentinels |
| Driver types above the repo | `sql.ErrNoRows` reaching CLI output | couples UX to storage engine | translate at repository boundary |
| Panic across layers | — (none) | crashes instead of reporting | reserve panics for programmer bugs (impossible states), never data/input conditions |

## 7. Review checklist

Before committing error-touching code, verify:

- [ ] Every `fmt.Errorf` that transports uses `%w`.
- [ ] Every layer adds exactly one new fact to the chain.
- [ ] Full chain reads as one coherent sentence; no fact twice.
- [ ] Storage/driver error types do not escape their package.
- [ ] Common misses have sentinels; callers use `errors.Is`.
- [ ] Messages: lowercase, no trailing period, `%q` identifiers, no
      `error:` prefix, consistent nouns with the rest of the codebase.
- [ ] Nothing silently dropped: unknown cases report/count/fail.
- [ ] Tests assert behavior via `errors.Is`, not message substrings.
