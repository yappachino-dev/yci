# Playlists

Ordered track sequences persisted in the database and rendered to `.m3u`
files inside the library for external players (termusic on PC, Poweramp on
phone via Syncthing).

- **Status:** implemented — static playlists, seeded from a track, re-derivable
- **Core packages:** `internal/library/playlist.go`, `playlist_reseed.go`,
  `playlist_edit.go`, `playlist_render.go`, `internal/library/fragment.go`,
  `internal/videoid` (the shared YouTube ID rule)
- **Schema:** migrations `007_playlists.sql`, `010_playlist_seed_recipe.sql`,
  `011_playlist_seed_mode.sql`, `012_playlist_seed_excludes.sql`
- **CLI:** `internal/cli/playlist.go`, `playlist_seed.go`, `playlist_reseed.go`,
  `playlist_edit.go`

---

## Design rules

**The database is the only source of truth.** Exported `.m3u` files are
regenerable artifacts, rendered wholesale and idempotently by
`yci playlist export`. Hand edits to exported files are overwritten on the
next export and never flow back into the DB. This is the same
one-source-of-truth discipline that rejected a filename-template column.

**Files live inside the library:** `<library_dir>/playlists/<slug>.m3u`.
Syncthing then carries playlists to the phone alongside the music they
reference, with no extra sync configuration. Audit ignores them (it is
DB-driven: it stats expected track paths and never scans for extras).

**Paths are relative to the .m3u file itself** (`../Artists/...`), standard
m3u semantics. This is mandatory, not stylistic: Syncthing lands the library
at different absolute paths per device, so absolute entries would break on
one side by construction. Forward slashes everywhere — both consumers expect
URL-style separators and files cross OS boundaries.

**Membership never changes on its own.** A seeded playlist stores the recipe
that produced it — the seed track and the chosen title tokens — so membership
can be re-derived on demand with `playlist reseed`, which reports a diff
before writing. Importing a file cannot alter a playlist you play on another
device. This is the specific reason dynamic rules were rejected; see
"Why not dynamic playlists".

**Export is deliberately not journaled.** The fs journal exists to keep
DB-tracked files crash-consistent with metadata; an .m3u is a pure re-render
of DB state, so worst case after a crash is a stale file fixed by re-running
export.

## Schema notes

- `playlists.name` is `UNIQUE COLLATE NOCASE`: lookup and duplicate checks
  are case-insensitive (`yci playlist show gym` finds "Gym").
- `playlist_tracks` PK `(playlist_id, position)` + append-only positions:
  additions compute `MAX(position)+1` inside the INSERT statement; removals
  delete outright and leave gaps. Ordering tolerates gaps; contiguous
  renumbering belongs to a future reorder operation, not to removal.
- `UNIQUE (playlist_id, track_id)` rejects duplicates — removing "the third
  occurrence" is not a question v1 wants to answer.
- Both FKs cascade: deleting a playlist or track cleans membership rows.

## Seeded playlists

```
yci playlist create <name> --from-track <track-id>
```

Derives a fragment from the seed's cleaned title, shows which tracks it
matches, and materializes the accepted set atomically. The fragment is a
**token set with AND semantics**, not a joined substring, which is why
`["Mori", "Calliope"]` reaches the run-together `MoriCalliope` spelling.

Interactive sessions choose which title tokens to match on, with a live match
count per attempt. Non-interactive sessions must say what they mean:

| Flag | Behaviour |
|---|---|
| `--accept-all` | use the derived fragment |
| `--fragment "Mori Calliope"` | state the token set explicitly |
| `--match all\|any` | how the tokens combine (default `all`) |
| `--exclude <track-id>...` | leave specific matches out (prefix allowed) |
| *(neither)* | refuse: a non-TTY never guesses a selection |

Membership and the recipe explaining it are written in one transaction
(`CreateFromSeed`), so a playlist never exists holding rows that `reseed`
cannot reproduce.

### AND and ANY

Tokens combine as a set, not a joined substring. A token matches a title when
it equals one of the title's tokens, or when the whole selection concatenated
matches one of them — which is how `["Mori","Calliope"]` reaches a title
spelled `MoriCalliope` without falling back to a substring search that would
also let `"Mori"` reach `"Morisette"`. `--match` chooses how the tokens
combine:

- `all` (default) — every token must be present. This is what the
  derivation proposes, and it is what stops a 星街すいせい seed from also
  selecting every collab she appears in.
- `any` — at least one token. Deliberately wider, so it will pull in
  titles sharing only one word.

Measured on the real library with the fragment `"星街すいせい" "Calliope"`:
`all` selects 1 track, `any` selects 70. That gap is why `all` is the
default and why the UI says so when a mode is widened interactively.

An empty token set never matches under either mode: a playlist with no
criteria must not silently select the whole library.

Matching is deliberately **not** a substring search. That was the earlier
behaviour, and over the 341-title corpus it produced 18 matches the token-set
semantics should not have, across 22 of 682 seed/mode combinations. Every one
of those changes was a narrowing; none widened a selection.

The mode is stored per playlist (migration `011`), and defaults to `all` for
rows written before the column existed, so an old recipe does not silently
change meaning.

## Excluding a false positive

Token matching is lexical, not work identity, so a performer family also brings
in collabs, self-covers, and titles that merely share a word. Those are the
cases one review pass is meant to catch:

```
# non-interactively
yci playlist create Suisei --from-track <id> --fragment "星街すいせい" \
    --exclude <track-id> [...] --accept-all --yes

# interactively: every match starts selected, space deselects
yci playlist create Suisei --from-track <id>
```

Exclusions are stored in the recipe, not applied only at seed time. That is
the point: without them, the next `playlist reseed` would re-derive from the
tokens alone and put every deselected track straight back — which is the same
"silently mutates membership" failure the static-membership design exists to
prevent.

They stay adjustable afterwards, and the change is a merge rather than a
replace, so naming one track does not clear the others:

```
yci playlist edit Suisei --exclude <track-id>
yci playlist edit Suisei --include <track-id>
```

As with `--match`, editing the recipe does not alter membership by itself;
`playlist reseed` shows the resulting diff.

## Editing

```
yci playlist edit <name> --name "<new name>"
yci playlist edit <name> --match any
```

Renaming also moves the exported `.m3u`. Leaving it behind would put a
playlist on the phone under the old name that no future export could update
— exactly the drift the static design exists to prevent. When the file cannot
be followed (a name collision, a permissions failure) it is reported with its
path rather than left silently.

A case-only rename (`gym` -> `Gym`) is allowed: SQLite excludes the row being
updated from its own `UNIQUE COLLATE NOCASE` check, so it is a real change
rather than a self-conflict.

`--match` does **not** re-derive membership. It changes what the recipe
means; `playlist reseed` then shows the difference and applies it. Doing both
in one step would make a mode switch a silent rewrite.

## Re-deriving

```
yci playlist reseed <name>            # show the diff, ask before writing
yci playlist reseed <name> --accept   # non-interactive
```

Two distinct failures, deliberately given distinct messages:

- `NoRecipeError` — the playlist was hand-built, so there is nothing to
  re-derive.
- `MissingSeedError` — the recipe is intact but its seed track was deleted.
  `seed_track_id` is `ON DELETE SET NULL`, so the tokens survive the
  deletion; the surviving tokens are what marks the playlist as seeded.
  Reading the seed as the marker instead would tell a user who *did* seed a
  playlist that they never did.

## Why not dynamic playlists

A rule stored alongside membership was considered and rejected on two
grounds, the first measured:

**The need is largely already met.** 59 星街すいせい tracks span 4 artists,
and 56 already resolve to the single artist `Hoshimachi Suisei` — the import
path links channels to artists, so the cross-cutting case that motivates
cross-cutting lists is handled at import. The residual 3 tracks are a
five-way collab, an `ORIGINAL SONG` that arguably does not belong, and a
self-cover: judgment problems, not query problems.

**Rules break the `.m3u` contract.** A rule changes membership when someone
imports a file; the exported artifact does not; Syncthing ships the stale file
to a second device and nothing reports it. `playlist_tracks` has no
regeneration hook, and adding one makes export a side effect of import rather
than a command you run. A playlist you trust is the point; one that drifts
unnoticed is worse than none, because you stop checking it.

### Why not a tag system

A `tracks <-> tags <-> playlists` join was considered alongside rules and
dropped for a more basic reason than any of the above: a static playlist is
*mechanically the same thing* as a tag join table. If a user tags ten songs
`#creep` purely so a dynamic playlist can query `tag = 'creep'`, they are
doing by hand what `playlist add "Creep Covers" <id>...` already does
correctly.

It also makes intrinsic metadata (artist, folder, title) a second source of
truth, which then needs reconciling on every move and rename — and yci's
rename rules are deliberately conservative because Syncthing identifies files
by content hash. Manual tagging adds permanent administrative overhead to a
library whose whole import story is that nobody curates anything by hand.

**Known gap:** the seed wizard has no per-candidate exclusion, so removing a
false positive means accepting the set and running `playlist remove`, or
narrowing the token set.

## Slug collisions

Distinct names can sanitize to the same file name. Single-playlist export
just writes; export-all detects collisions and skips with a message naming
both playlists instead of silently overwriting.

## CLI surface

```
yci playlist create <name>                  # empty
yci playlist create <name> --from-track <id>
yci playlist create <name> --exclude <id>...   # seed without these candidates
yci playlist reseed <name> [--accept]
yci playlist edit <name> [--name <new>] [--match all|any]
yci playlist edit <name> [--exclude <id>...] [--include <id>...]
yci playlist list
yci playlist show <name>        # playback order, EXTINF-style durations
yci playlist add <name> <id>... # track IDs, prefix matching applies
yci playlist remove <name> <id>...
yci playlist delete <name>      # confirm-guarded; leaves exported files alone
yci playlist export [name]      # all playlists when name omitted
```

`--exclude` and `--include` merge into the recipe's exclusion set rather than
replacing it, so two edits in a row do not undo each other. They change the
recipe, not membership: a reseed re-applies them.

## What would reopen the dynamic question

Stated so the decision is falsifiable rather than closed:

- **Auto-join on import** — "anything matching this performer joins playlist X"
  is a genuinely different need from re-derivation, and the one place rules
  earn their keep.
- **Scale** — at tens of thousands of tracks and hundreds of performers, the
  diff-and-confirm loop stops scaling.
- **Shareable playlists** — a rule is something you can hand to someone; an
  explicit membership list is a snapshot of a moment.

## Superseded

Title cleanup at import (see `title-interpretation.md`) is done, and title
*structure* is deliberately never inferred. Alias unification was proposed via
MusicBrainz and withdrawn: it splits 星街すいせい and `Mori Calliope` into two
unlinked artist entities, so it cannot unify the pair that motivated it. A
user-curated local alias table is the right shape for that problem.

## Export format guarantees

`RenderM3U` flattens CR, LF and U+2028/U+2029 in the playlist name, in every
track title, and in every path. M3U is line-oriented, so a multi-line ID3
value would otherwise end the `#EXTINF` line early and leave the remainder of
the title in the file as what a player reads as a path — a phantom entry
pointing at a real absolute path. Tag values are the input here, and
multi-line ones occur in the wild.
