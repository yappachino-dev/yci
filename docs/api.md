# Remote API

Exposing yci as a network service, so a remote client can drive downloads and
browse the library without linking yt-dlp and ffprobe into itself.

- **Status:** proposed — no code exists yet
- **Introduced:** n/a
- **Core package:** proposed `internal/api/`
- **Related:** `internal/app/app.go`, `internal/download/service.go`,
  `internal/library/staging.go`, `db/queries/queue.sql`

This is a design sketch, not documentation of behaviour. Nothing here is
built, and every endpoint, path and payload below is open to change.

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

service  POST /queue ──► 202 ──┐
                                ├──► worker goroutine ──► ProcessQueue
         GET  /queue ◄─────────┘
```

The worker is a goroutine started once at boot, looping over the queue.
`StartDownload` already flips `pending` → `processing` and increments
`attempts`, so a worker that dies mid-row leaves an orphan that
`ResetOrphanedDownloads` reclaims on next start. That recovery path already
exists because it was needed for Ctrl-C.

**Exactly one worker.** Two `ProcessQueue` calls racing each other would both
claim rows. A single goroutine is sufficient for a single-user deployment and
is far easier to reason about than leases.

## Endpoints

Read, all backed by existing DTOs:

| Method | Path | Notes |
|---|---|---|
| GET | `/api/v1/tracks?limit=&offset=` | `track list --json` |
| GET | `/api/v1/artists` | `artist list --json` |
| GET | `/api/v1/folders` | `folder list --json` |
| GET | `/api/v1/channels` | |
| GET | `/api/v1/playlists`, `/playlists/{name}` | |
| GET | `/api/v1/sources` | `source list --json` |
| GET | `/api/v1/queue` | `download queue --json` |

Write, each mapping onto one existing service method:

| Method | Path | Service |
|---|---|---|
| POST | `/api/v1/queue` `{url}` | `DownloadService.Enqueue` |
| POST | `/api/v1/queue/retry` `{ids?}` | `DownloadService.RetryFailed` |
| POST | `/api/v1/sources/pull` | `SourceService.Pull` |
| DELETE | `/api/v1/tracks/{id}` | `TrackService.Delete` |
| POST | `/api/v1/playlinks` | `ChannelService.Link` |

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

1. **Read-only API** over the existing DTOs, plus auth. Verifies the contract
   against a real consumer before anything can write.
2. **Worker** and `POST /queue`, with `GET /queue` polling. This is what makes
   the server useful.
3. **Write endpoints**, deciding the import policy above.
4. Mobile app.

## Risks

- **Import prompting** is the real design question, not the transport.
- **The JSON contract becomes load-bearing.** It is pinned by a schema test
  today, which is exactly right for a published API and only roughly right for
  a CLI. Expect to stabilise it harder.
- **Stream discipline stops being cosmetic.** The stdout/stderr split that is
  currently an inconsistency in the CLI becomes a content-type decision here.