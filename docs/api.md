# Remote API

Exposing yci as a network service, so a remote client can drive downloads and
browse the library without linking yt-dlp and ffprobe into itself.

- **Status:** six read endpoints, `POST /queue`, and the drain worker are
  implemented; everything else below is a proposal
- **Introduced:** `internal/api/`
- **Core package:** `internal/api/server.go`, `internal/dto/`
- **Related:** `internal/cli/serve.go`, `internal/app/app.go`

`yci serve` runs the HTTP API and drains the download queue in the background.
It is seven endpoints, not the collection sketched below: the tables below
separate what is built from what is only proposed, and a proposal row has no
route behind it.

## Why this is mostly a transport problem

The expensive property is already true of the codebase: every external tool
call sits behind an interface, and every service takes a
`ctx context.Context`. Neither was done for hosting, but both are what hosting
needs:

| Already present | Where |
|---|---|
| Composition root taking a `*sql.DB` | `app.NewContainer(dbConn, cfg)` |
| 11 context-scoped services | maps 1:1 onto a request context |
| 4 `exec.CommandContext` sites behind interfaces | `Downloader`, `PlaylistFetcher`, `VideoChannelResolver`, `Prober` |
| A persistent job table | `download_queue` has `status`, `attempts`, `error_msg` |
| Interrupted-row recovery | `ResetOrphanedDownloads`, run at boot |
| A versioned JSON contract | `TestJSONOutputSchemaIsStable` |

The read contract for a mobile app is therefore already written: the `--json`
DTOs for track, artist, folder, channel, playlist, source and the download
queue. Serving them is a response writer, not a design exercise.

## The one real change: the worker

`Enqueue` is fast. `ProcessQueue` blocks for minutes and is synchronous,
because a CLI can afford to wait. A server cannot.

```
today    enqueue ──► (user waits) ──► ProcessQueue ──► done

service  POST /queue ──► 201 ──┐
                                ├──► worker goroutine ──► ProcessQueue
         GET  /queue ◄─────────┘
```

`201`, not `202`: the queue row is a real resource created by this call. Only
the download behind it is asynchronous, and the caller can watch that through
`GET /queue`.

The worker is a goroutine started once at boot, looping over the queue. Its
first pass is immediate rather than one interval in, so a restart picks up
work that was pending when the process went down. `--poll-interval` tunes the
interval; the default is 30s.

**Not exactly one worker.** The claim in `StartDownload` is conditional on
`status = 'pending'`, so two drains racing each other cannot both take a row.
That is what makes it safe for the `serve` worker to overlap a manual
`download resume` — the case a single-worker design would have had to
forbid. An interrupted row is reclaimed by `ResetOrphanedDownloads` after
five minutes, kept fresh by a 30-second heartbeat, and released immediately on
cancellation. See `docs/download-queue.md`.

## Endpoints

Read, all backed by existing DTOs. `internal/dto` owns them: the API and the
CLI are two consumers of one contract, and a contract that exists once per
consumer drifts.

Implemented:

| Method | Path | Notes |
|---|---|---|
| GET | `/api/v1/tracks` | `track list --json`; no paging, see below |
| GET | `/api/v1/artists` | `artist list --json` |
| GET | `/api/v1/folders` | `folder list --json` |
| GET | `/api/v1/playlists` | `playlist list --json` |
| GET | `/api/v1/sources` | `source list --json` |
| GET | `/api/v1/queue` | `download queue --json` |
| POST | `/api/v1/queue` | `DownloadService.Enqueue`, see below |

Proposed. No route exists for any of these:

| Method | Path | Service |
|---|---|---|
| GET | `/api/v1/playlists/{name}` | `PlaylistService` |
| GET | `/api/v1/channels` | `ChannelService` |
| POST | `/api/v1/queue/retry` `{ids?}` | `DownloadService.RetryFailed` |
| POST | `/api/v1/sources/pull` | `SourceService.Pull` |
| DELETE | `/api/v1/tracks/{id}` | `TrackService.Delete` |
| POST | `/api/v1/playlinks` | `ChannelService.Link` |

`GET /tracks` ignores `limit` and `offset`. `track list` pages only when
asked to, so the CLI and the API agree on a single large response; that is
fine at a few hundred tracks and wrong at ten thousand.

## POST /api/v1/queue

```json
{ "urls": ["https://www.youtube.com/watch?v=BaW_jenozKc"],
  "force_playlist": false, "force": false }
```

Answers `201` with one item per video the URLs resolved to, each carrying
`url`, `video_id`, `skipped` and `reason`. Per-item outcomes are what make the
call useful: a caller posting five URLs needs to know which one was refused,
not that three succeeded.

`skip` reasons a caller will meet:

| `reason` | Meaning |
|---|---|
| `already in download queue` | the row exists |
| `already exists in database` | the track is in the library |
| `deleted from the library` | the deletion journal has the video |

Playlist URLs are `400`, pointing at `source add`, which is where a channel
choice is stored.

### The deletion journal

By default this endpoint honours the journal, the same way `source pull` does.
A caller posting from a phone is not present to know a video was deliberately
removed, and without this rule every client silently undoes a deletion made in
the CLI.

`force: true` skips the check. It exists because the journal cannot be strict:
it records only deletions made through `TrackService.Delete`, and only since
migration 015. A file removed in a file manager, or a track deleted before
that migration, leaves no record. So `force` covers the cases the journal
cannot speak for, such as deleting a track to replace a corrupt file and then
wanting it back.

`download <url>` in the CLI does **not** read the journal. That command is
attended and explicit, and a URL typed into a terminal is a decision by
someone who can see the library. The divergence is deliberate, not an
oversight.

Version prefix from day one. The DTOs are a contract a mobile app compiles
against; without a version it cannot be changed without breaking a shipped
app, and this library is not released.

## The interactive problem, and why it is smaller than it looks

Import asks a human when a downloaded video's channel has no linked artist.
The CLI supplies that answer through `LinkHandler`, an interface returning
`StagingResult` — it is not hardwired to a terminal. So a server supplies a
different implementation rather than being blocked by one.

Three policies, in increasing order of surprise:

- **Refuse.** `409` naming the channel that needs linking. The app prompts and
  retries with an artist. Safest, and the honest default.
- **Queue for review.** Record the unmatched channel, import nothing, let the
  user resolve later in `channel create`.
- **Auto-create an artist** from the channel name. Convenient and wrong: it
  invents library structure from remote metadata, which the import design
  deliberately refuses.

The same applies to the `huh` forms. Over HTTP they have no terminal; they
either become endpoints or are not exposed at all.

## Auth

Non-negotiable the moment the port is reachable. A bearer token in a config
key, compared with `crypto/subtle`. No per-user accounts: a user table
would be a feature nobody asked for.

Reachability is a separate problem and a better solved one: a VPN (Tailscale or
WireGuard) rather than an exposed port. Auth is defence in depth, not the
primary control.

The token comes from `YCI_SERVE_TOKEN` and deliberately is **not** a config
key: `config get` prints any key it knows, so a token in the TOML file would
be one command away from a terminal scrollback.

## Concurrency

SQLite in WAL mode with `busy_timeout` is already set per-connection through
`db.DSN` / `db.Open`. Readers never block the writer and the writer never
blocks readers, so concurrent API reads against a running worker are free.

One caveat worth stating: `ProcessQueue` writes through the transactor, and
long file moves are journaled and flushed post-commit. A crash leaves the
journal replayable, which is the property that makes this viable at all.

## Config at runtime

Config is env + TOML, loaded once at boot. That is fine on a server: settings
are static. The only question is whether `config set` should be reachable over
HTTP, and the answer should probably be **no** — it needs a restart to take
effect, so an API that appeared to apply it would be lying.

## Phasing

1. ~~**Read-only API** over the existing DTOs, plus auth.~~ Done: `yci serve`.
2. ~~**Worker** and `POST /queue`, with `GET /queue` polling.~~ Done.
3. **Import**, the missing half of the loop. A client can fetch audio but
   cannot move it into the library; `import staging` is the only way, and it is
   the one genuinely interactive command.
4. **Write endpoints**, the rest of the proposed table.
5. A client.

## Risks

- **Import prompting** is the real design question, not the transport.
- **The JSON contract becomes load-bearing.** It is pinned by a schema test
  today, which is exactly right for a published API and only roughly right for
  a CLI. Expect to stabilise it harder.
- **Stream discipline stops being cosmetic.** The stdout/stderr split that is
  currently an inconsistency in the CLI becomes a content-type decision here.