# Title Interpretation

YouTube titles are written by humans for algorithms, not as metadata. The
library's 341 tracks arrived with titles like:

```
【MV】Turing Love / チューリングラブ - Cecilia Immergreen & Gigi Murin (Cover)
「Cover」Dilemma / ジレンマ - Nanashi Mumei
【#RequiemforLove3D】 Bocca della Verità feat @MoriCalliope
【歌ってみた】メルティランドナイトメア【#07】
Queen - Bohemian Rhapsody (Official Video)
```

This document explains what yci does with those, what it deliberately does
**not** do, and why the boundary sits where it does.

- **Stripping (applied):** `internal/library/titlestrip.go`
- **Inference (frozen, hints only):** `internal/library/titleparse.go`
- **Corpus:** `internal/library/testdata/titles.txt` (341 real titles)
- **Schema:** migration `008_track_title_provenance.sql`
- **CLI:** `yci track retitle`

---

## The distinction the whole design rests on

Two operations look like one and are not:

| | Stripping | Inference |
|---|---|---|
| Question | which tokens are noise? | where does the song end? |
| Basis | a fixed vocabulary it recognizes | shape alone |
| Failure mode | does nothing | confidently wrong |
| Applied automatically | **yes** | **no** |

Stripping can only delete tokens it recognizes as decoration or a version
marker. It never splits, reorders, or infers, so its worst case is a title
left alone. Inference guesses where structure begins, and its worst case is a
title that is confidently, silently wrong.

That difference was not theoretical. An earlier version applied inference to
the stored title. Measured against twenty ordinary Western upload titles,
**nineteen were wrong and seventeen of those were reported at high
confidence** — the parser swapped artist and song on
`Queen - Bohemian Rhapsody (Official Video)`, because the corpus it was built
from contains only the opposite convention (`月に向かって撃て / 星街すいせい`).
A corpus containing only one convention can never falsify the assumption it
teaches.

So: **the title is stripped, and the guesses are stored beside it as
hints.** Nothing infers a title.

## What stripping does

Repeated passes (to a fixed point, bounded at four) remove:

- leading and trailing decoration — `【…】`, `『…』`, `「…」`, `[…]`, `(…)`
- version markers — `cover`, `official`, `original song`, `mv`, `live`,
  `lyric video`, `歌ってみた`, and bare trailing forms (`… COVER`)
- series and event tags — `【#07】`, `【#RequiemforLove3D】`
- surrounding whitespace, interpuncts and dangling separators

Passes repeat because peeling the outermost marker can expose another:
`Omajinai - お呪い / IRyS Cover 歌ってみた` hides `Cover` behind `歌ってみた`.
One pass would stop early and make stripping non-idempotent — a second run
would erode the title further, which is exactly the kind of slow,
unattributable damage this design is meant to make impossible.

Guarantees, pinned by tests over the full corpus:

- never returns an empty title for non-empty input
- never reorders a title (`TestStripTitleKeepsWesternOrder`)
- never strips a title a second time (`TestStripTitleIsSafe`)

Result on the real library: **333 of 341 titles change**, each losing only
recognized noise:

```
【MV】HAPPY HALLOWEEN - junky || HAKOS BAELZ COVER  →  HAPPY HALLOWEEN - junky || HAKOS BAELZ
Queen - Bohemian Rhapsody (Official Video)         →  Queen - Bohemian Rhapsody
```

## What is stored

| Store | Contents |
|---|---|
| `tracks.title` | the stripped title — the only display value |
| `tracks.raw_title` | the title exactly as the file's tags carried it |
| `tracks.performer` | inferred — **untrusted hint** |
| `tracks.version` | inferred — **untrusted hint** |
| `artist_id` | the link a human confirmed at import — the curated truth |

`performer` and `version` are not a second source of truth about
attribution, and nothing reconciles them with `artist_id`. They exist
because they were free to compute at import, and they are inert: no command
reads them, so a wrong hint is not yet a wrong answer.

`Confidence` is **retired as a gate**. It described parse hygiene — "no
decoration survived" — not correctness, and it endorsed the inversions
above. Nothing branches on it any more.

`raw_title` being NULL means the row predates this feature.

## What the user sees

```
$ yci track retitle --dry-run
TRACK ID   OLD TITLE                            NEW TITLE
5ead5f5e   少女A / 猫又おかゆ (cover)              少女A / 猫又おかゆ
f305de21   【MV】HERE COMES HOPE -IRyS【Orig…】   HERE COMES HOPE -IRyS
... and 331 more.

Dry run: 333 of 341 track(s) would be retitled, 341 row(s) would record provenance. No changes made.
```

Database only, idempotent, reversible from `raw_title`, no file operations.
A second run reports nothing to do.

New imports get it automatically, and the filename follows the stripped
title because it is derived at import. Since two different uploads can strip
to the same name, an occupied destination gets a ` (2)` suffix rather than
overwriting the file already there.

## What this does not do

- **It does not identify songs.** `Crazy:B「Honeycomb Summer」` and
  `Valkyrie「今宵月の館にて」` keep their series prefix: the song is a
  substring, and no rule can find it. This is a modeling gap, not a bug.
- **It does not know what a track is a cover of.** That relation is worth
  recording — it is what cover grouping needs — and it is not a title edit.
  It wants a work-identity relation, curated by a human.
- **It does not change what PowerAMP shows.** Players read the embedded tag
  and filename, not the database. The tag is still raw; rewriting Vorbis
  comments needs `ffmpeg -c copy` and its own journaling.
- **It does not rename existing files.** 341 renames would invalidate every
  Syncthing hash and re-transfer the whole library to the phone.
- **There is no `edit track`.** A mis-strip is correctable by fixing
  `raw_title` and re-running, but not from the CLI.

## Why inference is frozen rather than deleted

The rules are isolated, tested, and inert. Keeping them costs nothing and
preserves the measurement work: a future feature that needs attribution —
cover grouping, work identity — should first hand-label a sample of this
corpus and score the hints against it. If the accuracy is not good enough,
the hints stay hints, and the honest conclusion is that title-based rules
do not work for this library.

One promising idea is parked with it: at import yci already reads the file's
`TPE1` tag, which for Western uploads states the artist outright. Comparing
that against the parsed performer detects the inversion directly — a
cross-check against real data rather than another heuristic. It is useless
while nothing is inferred, and worth reviving only alongside measured work.

## Maintenance

Encounter a title that strips wrong: add a case to `TestStripTitle`,
preferring a real title, and extend `stripOnce`. Refresh the corpus with:

```sh
sqlite3 ~/.config/yci/data.db "SELECT title FROM tracks ORDER BY title;" \
  > internal/library/testdata/titles.txt
```

Stripping's vocabulary lives in Go beside the loop that reads it. That is
deliberate: one small rule set means each change is reviewable against a
corpus that catches regressions. If it ever grows enough to make editing
the cascade risky, extracting the *vocabulary* into an embedded data file is
the first move — not generalizing the mechanism, and not adding a plugin
registry.

## Path normalization

The filename is derived from the interpreted title, so it inherits whatever
normalization the source metadata used. YouTube titles arrive with combining
marks — `ネヴァモア` as ア plus a combining dakuten rather than as the
precomposed `ヴ` — while the filename on disk may hold the other form.

NFC and NFD are visually identical but different bytes, and ext4 compares
names byte for byte. A stored path that differs only in normalization is a
path that will never open.

**Imports cannot produce this.** `processSingleStagingFile` writes the row and
`MoveFile`s the file to that same path inside one transaction, and a rename
preserves bytes, so the stored path and the on-disk name agree by
construction. Nothing guards `resolveDestination` for this, because nothing
needs to.

It does arise for files placed by something else — hand-copied, or written by a
version that named files from `raw_title` before stripping existed. That is
how the one real case arose: the file carried `【MV】` and a `[cover]` suffix
the stored title had long since dropped.

Two things handle it:

- `internal/fs.ResolveExisting` tries a path's other normalization form, used
  by the duration probe so byte drift is reported as the file it is rather
  than as "not on disk".
- `track reconcile-paths [--dry-run]` rewrites stored paths, and only when the
  alternate form is provably present. A genuinely absent file is reported and
  left alone rather than guessed at.

**No migration repairs this.** Normalizing blindly to NFC would silently
orphan every track in a library that is genuinely NFD, and a
filesystem-probing migration is neither offline nor deterministic. Migrations
also run once, at an arbitrary moment, possibly before a library exists.
The M3U exporter writes stored paths verbatim, so reconcile before exporting:
making a pure renderer filesystem-aware is worse than an explicit step.
