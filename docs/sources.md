# Watched YouTube Sources

A **source** is a YouTube playlist you follow. `source pull` polls every
source you have added and puts videos it has gained into the download queue.

## Not a playlist

The distinction is load-bearing, not vocabulary.

A **playlist** is local membership. The database is the source of truth, it
only changes when you change it, and the exported `.m3u` is an artifact
derived from it.

A **source** is a remote list that keeps changing on its own. Polling one
adds videos to the download queue and **never touches playlist membership**.

Merging the two would mean every poll rewrites membership without you asking,
which is the automatic-membership-change the design forbids. They are
separate tables (`playlist_sources` and `playlists`) and separate commands.

## Channel selection is stored by ID

`source add` fetches the playlist once and asks which channels to keep. The
selection is saved as `{id, name}` per channel, and a poll matches on the
**ID**.

YouTube reports both per entry: `channel` is a display name, `channel_id` is
the immutable `UC...`. yt-dlp populates `channel_id` for real playlist
listings and omits it for a channel's own uploads list, so the stored ID is
empty in that case and matching falls back to the name.

The picker starts with those channels already ticked, because a playlist you
watch is mostly artists you already have, and re-ticking them on every poll is
the manual work the command exists to remove. `--channels ID,NAME` adds
channels on top, for a channel with no artist yet that would otherwise have to
be hunted for in the list. `--no-preselect` starts from nothing and still
honours `--channels`.

An entry in `--channels` the playlist does not offer is **reported**, not
dropped: a typo there is indistinguishable from a working entry otherwise.

## Channels that deliver nothing

A selected channel with no linked artist receives downloads and then prompts
on import; if nothing is imported, it delivers nothing at all, silently.
`source list` counts them:

```
NAME   LINKED  CHANNELS                       URL
----   ------  -----------------------------  --------------------
mixed  2/3     Okayu Ch. 猫又おかゆ, ...       https://...

1 selected channel(s) are not linked to an artist. ...
```

This is **derived**, not recorded. "Selected but not linked" is computable
from the stored selection and the `channels` table at any moment, so it cannot
drift, and it clears itself the instant the channel is linked — verified by
linking one and watching the count drop from `1/3` to `2/3`.

A table of manually selected channels would need rows inserted at selection,
deleted on link, and a decision about what happens when a link is *removed*.
Deriving it answers the same question with none of that, and answers it
correctly when a link disappears rather than only when a table remembers.

The reason to key on the ID is rename. A performer who renames their channel
changes the display name only; a selection stored as a string would quietly
stop matching and the playlist would silently deliver nothing. Both halves are
stored because the name is still what a person reads.

## A deletion is a decision

The queue already skips videos that are in the library, staged, or queued.
What it cannot see is a track you **deleted**: once the row is gone, nothing
else remembers the video, and a playlist that still offers it would download
it again.

So deletion is journalled. `track_journal` is append-only, and `TrackService.Delete`
writes a `deleted` entry in the same transaction as the delete — a removal
recorded for a track that survived would suppress a download you never
cancelled.

A poll skips any video with a `deleted` entry and reports it as
`deleted from the library`. The test is *any* delete entry, not the most
recent one: a track removed again after being re-downloaded is one you have
now deleted twice, and honouring only the newest event would quietly undo that.

The journal only knows about the future. Tracks deleted before this table
existed leave no record, so they are downloaded once more and then recorded.

`imported` is recorded too. Nothing reads it yet: `tracks.source_id` already
answers whether a video is in the library. It is there so the table is a
history rather than a tombstone list.

## Pacing

`source pull` and `channel backfill` share one helper. A flat playlist listing
is one cheap request, so below **5** remaining sources no wait happens at all
— the sleep would cost more than the work. Above that, `download.delay` spaces
requests and honours cancellation.

## Commands

```
source add <url> [--name <name>]   # fetch, pick channels, store the selection
source list [--json]
source update <name>               # re-pick channels, starting from the current selection
source rename <current> <new>      # relabel only; url and selection are untouched
source remove <name>
source pull
```

`source add` and `source update` run an interactive picker, so they need a
terminal. `source pull` never asks anything: it replays the stored selection.

`source rename` is separate precisely because it needs neither a refetch nor a
picker. Relabelling is not a change to what is watched, and folding it into
`update` would cost a network round trip and a re-pick of the channels to
correct a typo. It is a label change: the url and the stored channel selection
are the same playlist either way, and nothing else in the database refers to a
source by name.

There is no `--dry-run` on `pull`. It only adds to the download queue, which
is inspectable with `download queue` and reversible from there. The dry-run
flags belong to the verbs that remove things.

## What a poll reports

One line per source, then a total. A source that fails keeps its own line:
one playlist going private is a thing to act on, not a number to average
away, so a failing poll does not abandon the rest of the run.

| Reported as | Meaning |
|---|---|
| `already exists in database` | already in the library |
| `already in download queue` | queued, not yet processed |
| `deleted from the library` | journalled as removed, deliberately not re-downloaded |
| `N new` | added to the queue by this poll |

"Not new" means *not in the library*, not *added since the last poll*. A track
you deleted is skipped, so a pull is a sync rather than a delta.