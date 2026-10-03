# Channels

Status: implemented (schema 14)

A *channel link* records that a library artist publishes on a particular
YouTube channel. It exists so import can decide which folder an incoming file
belongs to without asking.

## Why the name is not the identity

The first implementation identified a link by the channel's **display name**,
because that is what the user typed when linking:

```
channel create --artist irys --name "IRyS Ch. hololive-EN"
```

That is a label, not an identifier. A performer can rename their channel at
any time, and YouTube's naming is unstable in ordinary ways too: spacing,
capitalisation, and suffixes like `-EN`.

The failure was silent and appeared only at import. A staged file carries the
artist tag yt-dlp wrote at download time, which is the channel's name *as of
that download*:

```
internal/library/staging.go   channelName := ChannelName(strings.TrimSpace(m.Artist))
                              s.linker.ResolveArtist(ctx, channelName)
                              -> SELECT artist_id FROM channels WHERE channel_name = ?
```

After a rename the lookup missed, the unknown-channel handler ran for **every
affected file**, and nothing was wrong with the download itself. This was not
hypothetical: running `channel backfill --dry-run` against the real library
reported **8 of 14 links renamed**, mostly channels dropping a `-EN` suffix.

## What the identity is

A YouTube channel ID, `UC...`, which never changes on rename:

```
channels.channel_id   UNIQUE (partial: NULLs excluded)
```

It is now the identity; the name is a label that import refreshes when it
drifts. The partial unique index matters for a second reason: the old
constraint was on `(artist_id, channel_name)`, which permits the *same*
channel to be linked to two artists under two spellings. That is the rename
bug in a different costume, and the ID makes it unrepresentable.

## Recording the identity

A download is the only moment the authoritative ID is available, so it is
captured there. `buildArgs` adds:

```
--print  yci-video \t %(id)s \t %(channel_id)s \t %(channel)s
```

`--print` rather than `--dump-json`, which **simulates** the download unless
`--no-simulate` is also passed and would silently turn every download into a
no-op. Progress goes to stderr, so the downloader pipes stdout, consumes its
own line, and forwards the rest to the user.

The mapping lands in `video_channels`, keyed by video ID:

```
video_channels (video_id PRIMARY KEY, channel_id)
```

Video ID because that is the staged file's name (`<video-id>.<ext>`), so import
already holds the join key. Rows are dropped once the track exists.

## At import

`StagingService.ResolveChannel` prefers the recorded ID over the tag:

1. look up `video_channels` for the file's video ID
2. resolve the artist by channel ID
3. refresh the stored name if it drifted
4. only if there is no record, fall back to matching the tag

Step 4 is what keeps this backwards compatible. Files placed in staging by
hand, and any file predating this work, still import through the name path.

## Links are born complete

Most links in a real library are created by **import**, not by a typed command.
When a staged file's channel has no link, the handler prompts, and if the user
attaches the channel to an artist the link is created:

```
internal/library/staging.go   res.LinkArtist ->
                               s.linker.Link(ctx, artistID, channelName, match.ChannelID)
```

That last argument is the point. At that moment `video_channels` already holds
the identity the download recorded, so the new link is created with it. Without
it, links were born incomplete and every one needed a repair pass before
attribution could be trusted — and since the prompt is the dominant creation
path, that was most of them.

`ChannelService.Link` takes the ID; an empty one is accepted for the cases with
no record to draw on (a file placed in staging by hand).

## Recovering links made before this

```
$ yci channel backfill --dry-run
```

Asks YouTube which channel each artist publishes on, using **one video per
artist**, and stores the ID. One request per artist rather than per track: the
real library has 341 tracks across 14 artists.

It refuses to guess. An artist with several channel links gives nothing to
resolve against, so those are reported as `unresolved` and left alone, because
attaching the wrong ID would file future downloads under the wrong artist. A
single link per artist is unambiguous even when the name has drifted, because
the rename is exactly the case being repaired.

Outcomes are `attached`, `renamed`, `already known`, `unresolved`, `failed`.

**It is paced and it narrates.** Requests use the same `download.delay` setting
as downloads (default `5:30`), because each lookup is a separate yt-dlp process
and yt-dlp's own `--sleep-interval` cannot apply *between* processes. An unspaced
loop over the same host is how a run turns into a 403 half way through, which is
the failure the download path already classifies.

Progress goes to stderr, so stdout stays reserved for the table:

```
[*] Resolving 14 artists over 14 links, one request per artist (this talks to YouTube).
[1/14] cecilia-immergreen
      cecilia-immergreen -> UCvN5h1ShZtc7nly3pezRayg (Cecilia Immergreen Ch. hololive)
```

Without this the run is an unresponsive terminal for the duration, which is
indistinguishable from a hang. There is no sleep after the final artist, and the
wait honours cancellation, so Ctrl-C returns immediately rather than sitting out
the timer.

A run **refreshes a drifted name as well as a missing ID**, and that includes
links that already carry an identity. Skipping those looks like an optimisation
and is not one: whether a *name* is current is not knowable from the ID, and a
link whose label has drifted is exactly the broken state this repairs. An
earlier version skipped them and left `IRyS Ch. hololive-EN` in place
indefinitely while reporting the link as known.

Each link is reconciled against the one channel the query answered about. An
artist publishing on several channels has more links than that, and the others
are left untouched: applying one channel's name to all of them would corrupt
them.

Re-running a converged library reports `14 already known` and changes nothing.

## Deliberately not done

- **No automatic backfill on upgrade.** It needs the network, and a command
  that quietly rewrites artist attribution should be run deliberately.
- **No channel merge UI.** Two links for one channel are prevented by the
  unique index rather than detected and repaired; correcting existing data is
  `channel delete` followed by `channel create`.
- **Names are not keys anywhere.** `FindArtistID` returns
  `ErrChannelNameAmbiguous` when one name spans several artists rather than
  picking the first row, which is what it used to do.
