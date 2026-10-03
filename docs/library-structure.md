# Artists and Folders

How performers and the folders their files live under relate, and the three
ways a new artist gets placed in the library.

| | |
|---|---|
| **Status** | implemented |
| **Introducing migration** | none; artist and folder existed from the initial schema |
| **Core packages** | `internal/library` (`ArtistService`, `FolderService`), `internal/cli` |
| **Related files** | `internal/library/artist.go`, `internal/library/folder.go`, `internal/cli/artist.go` |

## The shape

```sql
artists(id, display_name, folder_id)   -- folder_id NULL when orphaned
folders(id, display_name, parent_id)   -- self-referencing tree
```

Both `id` columns hold a **slug**, not a UUID. Track IDs are opaque UUIDv4s;
artists and folders are human-named things, and their identifiers are
`nameToSlug(display_name)`. That is why human output shows slugs for these two
nouns and UUID prefixes for tracks.

`ArtistName.ToSlugID()` and `FolderName.ToSlugID()` are separate methods that
both delegate to the same `nameToSlug`. **The invariant that an artist's leaf
folder ID equals the artist ID depends on them agreeing.** Changing one without
the other breaks the invariant silently, so it is pinned by
`TestLeafSlugMatchesArtistSlug`.

## The leaf folder convention

An artist's files live under a folder named after them, and that folder's slug
is the artist's slug. In the real library this holds for all 14 artists, each
with exactly one leaf folder.

The convention exists because an artist is not always one folder: an artist
with tracks in `JP` and `EN` occupies several leaves. The convention applies to
the *default* placement, not to every folder an artist may occupy.

`artists.folder_id` is a single column, so it records where the artist is
*based* — the folder whose slug matches. It is not an enumeration of the
artist's tracks.

## Placing a new artist

Three options, each meaning something different:

| Invocation | Effect |
|---|---|
| `artist create --name "X"` | Slug from the name. Links to an existing folder `x` if there is one, otherwise lands in root. |
| `artist create --name "X" --folder F` | Links to existing folder `F`. Fails if `F` does not exist. |
| `artist create --name "X" --parent P` | Creates folder `x` under `P` **and** links the artist to it, in one transaction. |

`--parent` and `--folder` are mutually exclusive. One *mints* a folder, the
other *links* one; silently picking a winner would invent library structure from
a typo.

`--parent` runs inside `WithinTransaction`. Both services carry a `Transactor`
and nested calls join the outer transaction, so a failed artist insert rolls the
new folder back. That is the reason this is a single call rather than a shell
function that runs `folder create` then `artist create` — the two-command form
can leave an empty leaf folder behind, which is the debris the single call
exists to prevent.

### Why `--parent` refuses to reuse an existing folder

If folder `x` already exists, `--parent` fails rather than linking to it. The
alternative is to ignore `--parent` and use the existing folder anyway, which
makes the flag's meaning depend on invisible state. Failing points at `--folder`,
which is the flag that means "this folder already exists".

The check itself is not a separate guard: the duplicate leaf insert fails on the
`UNIQUE` constraint and `db.MapSQLError` maps it to `ErrFolderAlreadyExists`,
which the CLI renders as *"a folder named after X already exists; pass --folder
to link it instead"*. The transaction discards the attempted insert either way.

## Deliberately not done

**No automatic leaf folder for the plain path.** `artist create --name "X"`
with no flags falls back to root rather than inventing a folder. The fallback is
the pre-existing behaviour and stays backward compatible; creating structure is
an explicit choice via `--parent`.

**No renaming of files on slug change.** `artist edit --update-id-slug` changes
identifiers, but no existing file is renamed — Syncthing identifies files by
content hash.