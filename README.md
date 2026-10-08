# yci

An automated, local-first music ingestion engine, library manager, and headless audio daemon written in Go and SQLite.

`yci` bridges remote audio sources (YouTube, channel uploads, and curated playlists) with a local, curated, and Syncthing-replicated library. It pairs a terminal-driven CLI with an authenticated HTTP daemon, orchestrating extraction via `yt-dlp` and `ffmpeg`, and storing metadata in SQLite using clean, crash-resilient transactional patterns.

---

## Why `yci`?

Most personal music collection workflows are fragmented: downloading with arbitrary scripts, manually editing ID3 tags, struggling with erratic YouTube title formats, and dealing with broken playlist paths when syncing files between PC and mobile devices.

`yci` was engineered as a production-grade personal infrastructure system to solve these problems end-to-end:

* **Crash-Consistent Filesystem Mutexes:** Uses a **Transactional Outbox / Journal (`fs_journal`)** ensuring that file operations (moves, directory relocations, deletions) only happen *after* metadata commits to SQLite.
* **Queue Concurrency & Race-Free Reclaims:** Atomic claims (`UPDATE ... WHERE status = 'pending'`), process heartbeats, and chronologically verified staleness bounds allow overlapping manual jobs and background daemon workers without duplicate work.
* **Channel Identity Resolution:** Tracks performers by immutable YouTube Channel IDs (`UC...`) rather than mutable display names, self-healing channel renames without orphan file drifts.
* **Cross-Platform Path & Unicode Normalization:** Actively detects and reconciles NFC vs. NFD Unicode byte drift across filesystems (e.g., ext4 vs. macOS/Android HFS/APFS), generating strictly relative-path `.m3u` playlists for seamless Syncthing synchronization.
* **CJK & Terminal Formatting Rigor:** Custom table layouts calculate visual cell width via East Asian width tables (`runewidth`), preventing ragged ASCII tables on Japanese, Korean, and Chinese tracks.
* **Non-Destructive Title Parsing:** Distinguishes deterministic marker peeling (e.g., stripping `【MV】`, `(Cover)`) from brittle structural inference, guaranteeing track names never get corrupted or inverted.

---

## System Architecture

```text
                                [ YouTube / Remote Sources ]
                                             │
                                  (yt-dlp metadata & audio)
                                             ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                INGESTION & PIPELINE                                    │
│                                                                                        │
│   ┌─────────────────┐       claims        ┌──────────────────┐     extracts to         │
│   │ download_queue  │ ──────────────────► │  Download Engine │ ────────────────┐       │
│   │ (SQLite Table)  │ ◄────────────────── │ (Heartbeat/Retry)│                 │       │
│   └─────────────────┘      state sync     └──────────────────┘                 ▼       │
│             ▲                                                           ┌────────────┐ │
│             │ poll / enqueue                                            │  Staging   │ │
│   ┌─────────────────┐                                                   │  Directory │ │
│   │ playlist_sources│                                                   └────────────┘ │
│   └─────────────────┘                                                          │       │
│                                                                                ▼       │
│                                      link / import                      ┌────────────┐ │
│                      ┌───────────────────────────────────────────────── │ Tag Reader │ │
│                      │                                                  │ & Prober   │ │
│                      ▼                                                  └────────────┘ │
│       ┌───────────────────────────┐                                            │       │
│       │  Channel Identity Seam    │                                            │       │
│       │  (video_channels UC... ID)│                                            │       │
│       └───────────────────────────┘                                            │       │
└────────────────────────────────────────────────────────────────────────────────┼───────┘
                                                                                 │
                                                                                 ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                               CORE STORAGE & CONSISTENCY                               │
│                                                                                        │
│   ┌────────────────────────────────────────────────────────────────────────────────┐   │
│   │ SQLite Transaction Unit (Single atomic commit)                                 │   │
│   │                                                                                │   │
│   │ 1. Insert / Update Metadata (tracks, artists, folders, playlists)              │   │
│   │ 2. Insert Physical Actions into `fs_journal` (`move_file`, `delete_file`)      │   │
│   └────────────────────────────────────────────────────────────────────────────────┘   │
│                                            │                                           │
│                                   Commit Succeeded                                     │
│                                            ▼                                           │
│   ┌────────────────────────────────────────────────────────────────────────────────┐   │
│   │ Post-Commit Executor (Transactional Outbox)                                    │   │
│   │ • Executes atomic OS file operations                                           │   │
│   │ • Idempotent replay capability on boot (`yci repair`)                          │   │
│   │ • Transitions journal rows: pending ──► done / failed                          │   │
│   └────────────────────────────────────────────────────────────────────────────────┘   │
│                                            │                                           │
│                                            ▼                                           │
│                               ┌───────────────────────────────┐                        │
│                               │  Managed Music Library        │                        │
│                               │  (Folders / Artists / Files)  │                        │
│                               └───────────────────────────────┘                        │
│                                            │                                           │
│                      regenerates           │                                           │
│                           ▼                ▼                                           │
│                ┌────────────────────┐   ┌────────────────────┐                         │
│                │ Relative .m3u      │   │ Syncthing Mirror   │                         │
│                │ Playlists          │   │ (PC ◄──► Mobile)   │                         │
│                └────────────────────┘   └────────────────────┘                         │
└────────────────────────────────────────────────────────────────────────────────────────┘

```

---

## Technical Deep-Dives

### 1. Crash-Resilient Filesystem Outbox (`fs_journal`)

Physical file mutations performed inside database transactions create consistency nightmares if an application crashes mid-transaction.

* **The Solution:** `yci` isolates disk mutation behind a redo-only journal outbox pattern (`db/migrations/005_fs_journal.sql`).
* When a folder is renamed, an artist is moved, or a staged track is imported, the database changes and the planned filesystem actions (`OpMoveFile`, `OpMoveDir`, `OpDeleteFile`) are recorded in the **same atomic SQLite transaction**.
* A post-commit hook flushes pending entries to disk. If the process is terminated mid-flight, `yci` replays pending actions idempotently on startup. Conflicting or missing states trigger hard fail-safes without corrupting metadata.

### 2. Multi-Worker Queue Concurrency & Staleness Windows

The download subsystem supports concurrent execution across interactive terminals and the headless daemon without distributed locks or leases:

* **Atomic Work Claims:** `StartDownload` executes a conditional update:
```sql
UPDATE download_queue
SET status = 'processing', attempts = attempts + 1, updated_at = CURRENT_TIMESTAMP
WHERE id = :id AND status = 'pending';

```


Workers check the affected row count—if `0`, another worker claimed it and execution advances cleanly without retrying or collision.
* **Chronological Heartbeats:** Active downloads run an asynchronous 30-second heartbeat (`touch`). Dead jobs are only reclaimed if their heartbeat is older than `5 minutes`.
* **Stringent Type Binding:** Timestamps are formatted and cast to SQLite's native `CURRENT_TIMESTAMP` layout (`YYYY-MM-DD HH:MM:SS`) to prevent Go's monotonic `time.Time` string representations from inverting chronological comparisons in SQL.

### 3. Immutable Channel Identity (`014_channel_identity.sql`)

Artists frequently rename their YouTube channels (e.g., dropping tags, altering spacing, or updating stage names).

* Traditional tools match file tags against stored artist names, causing batch import failures whenever a performer renames their channel.
* `yci` captures YouTube's immutable `UC...` identifier on download via `--print` metadata templates into an ephemeral `video_channels` mapping.
* At import time, files resolve against `channels.channel_id`. If the display name has drifted, `yci` updates its human-readable label in place without breaking attribution or prompting the user.

### 4. Deterministic Unicode Normalization & CJK Formatting

* **Path Reconciliation:** Many audio tools write Unicode filenames in NFD (decomposed) while Linux filesystems like ext4 compare byte-for-byte in NFC (composed). `fs.ResolveExisting` and `yci track reconcile-paths` check both forms on disk, preventing "file not found" errors on tracks containing CJK characters (e.g., `音楽_Track01`).
* **East Asian Width Alignment:** Standard standard-library tabwriters measure string length in bytes or runes. Because CJK characters occupy 2 visual cells on terminal screens, traditional tables break. `yci` calculates width using `runewidth.StringWidth`, ensuring stable column boundaries regardless of script.

### 5. Deterministic Playlist Engine with Seed Recipes

* Traditional "smart" or dynamic playlists silently alter membership whenever tracks are added, breaking synchronized `.m3u` files on secondary devices (such as phones running media players via Syncthing).
* `yci` enforces **static, recipe-seeded playlists**: recipes store token fragments (e.g. `["Acoustic", "Live"]`), combination modes (`AND` / `ANY`), and explicit manual exclusions.
* Re-deriving membership via `yci playlist reseed` calculates diffs and prompts the user before applying updates. Exported `.m3u` files use relative paths (`../Artists/...`) and strip line breaks, preventing path traversal issues across operating systems.

---

## Core Capabilities

* **YouTube Ingestion:** Single videos, entire playlists, or hybrid URLs with audio extraction directly to `.opus`, `.flac`, `.mp3`, or `.m4a`.
* **Automated Channel Watching (`source`):** Track remote YouTube playlists. `yci source pull` fetches new uploads, filters against stored channel selections, and skips previously deleted tracks via an append-only `track_journal`.
* **Guided Staging Importer:** Staged files are analyzed via `dhowden/tag` and `ffprobe`. Matches automatically route into folder hierarchies; unknown channels can be permanently mapped to an artist or routed to a folder.
* **Duration Probing:** Asynchronously probes track stream durations using `ffprobe` with parallel workers, safely updating tracks marked with missing lengths.
* **Audit & Self-Healing:** Detects missing physical files, orphan artist links, and dangling database entries with automatic queue re-population (`yci sync`).
* **HTTP Daemon & Background Worker:** `yci serve` exposes the library over an authenticated REST API and automatically drains the download queue on an interval.

---

## Installation & Prerequisites

### Dependencies

Ensure the following tools are installed and accessible in your `$PATH`:

* **Go** (1.24 or later)
* **yt-dlp** (Required for metadata and audio extraction)
* **ffmpeg & ffprobe** (Required for audio processing, tag reading, and stream duration probing)

### Building from Source

```bash
# Clone the repository
git clone https://github.com/yourusername/yci.git
cd yci

# Build the binary
make build

# Install to ~/.local/bin (with shell completions for Fish if present)
make install

```

To install pre-commit validation hooks (gofmt, vet, race detector, sqlc drift checks):

```bash
make hooks

```

---

## Configuration

`yci` uses strict, layered configuration with the following precedence (highest to lowest):

1. **Command-line flags** (`--delay`, `--cookies`)
2. **Environment variables**
3. **Configuration file** (`$XDG_CONFIG_HOME/yci/config.toml`)
4. **Built-in defaults**

### Default Locations

* Configuration & Database: `~/.config/yci/config.toml` & `~/.config/yci/data.db`
* Managed Library Root: `~/Music/Managed`
* Staging Directory: `~/Music/Staging`

### Example `config.toml`

```toml
library_dir = "/home/user/Music/Managed"
staging_dir = "/home/user/Music/Staging"

[download]
delay = "5:30"                          # Randomized min:max sleep interval between downloads (seconds)
cookies_file = "/home/user/cookies.txt" # Optional cookies export for age-gated or restricted content

```

Inspect and modify configuration via the CLI:

```bash
# View active config path
yci config path

# View all keys, active values, and resolution layer (default, file, env)
yci config list

# Set a key directly
yci config set download.delay "10:45"

```

---

## CLI Usage Walkthrough

### 1. Ingesting Tracks from YouTube

```bash
# Enqueue and download a single track
yci download "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

# Enqueue without immediately downloading
yci download --queue-only "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

# Resume or drain pending downloads in queue
yci download resume

# Inspect active queue status
yci download queue

```

### 2. Processing the Staging Area

Once downloaded, files sit in staging waiting for ingestion:

```bash
# Run interactive import wizard
yci import staging

# Non-interactive / headless import (automatically skips unknown channels)
yci import staging --skip-unknown

```

### 3. Library Management (Artists & Folders)

```bash
# List folders hierarchically
yci folder list

# Create a parent folder and artist leaf simultaneously
yci artist create --name "Artist Name" --parent "rock"

# List all tracks with display-width aware tables
yci track list --limit 25

# Output track list as JSON
yci track list --json

```

### 4. Following Remote Channels (`source`)

Follow remote YouTube playlists to automatically ingest new releases:

```bash
# Add a remote playlist and interactively select which channels to track
yci source add "https://www.youtube.com/playlist?list=PL1234567890" --name "synthwave-covers"

# Poll all watched sources and queue newly discovered tracks
yci source pull

# List watched sources and inspect linked artist coverage
yci source list

```

### 5. Managing Static Playlists

```bash
# Seed a playlist from an existing track (derives token recipes)
yci playlist create "Chill Vibes" --from-track 5ead5f5e --accept-all

# Preview re-seeding against newly added library tracks
yci playlist reseed "Chill Vibes"

# Export all playlists as relative .m3u files for Syncthing / media players
yci playlist export

```

---

## Engineering Standards & Testing

The project maintains high engineering standards enforced by automated tooling and strict architecture patterns:

### Testing Philosophy

* **Real Storage Integration:** Storage and repository tests execute against real SQLite instances and ephemeral temporary directories. In-memory SQLite (`:memory:`) or disk-backed databases are used throughout; driver-level behavior is never mocked.
* **Real Audio Synthesis:** Audio format and metadata tag reading tests synthesize genuine audio files via `ffmpeg` sine wave generators (`testsupport.SynthesizeAudio`) rather than relying on mock byte buffers.
* **Interactive TTY Test Harness:** Interactive terminal prompts (`huh`) are tested end-to-end using virtual pseudo-terminals via `creack/pty`, verifying key decoding (`\r`, arrow escape codes, spaces) and full rendering lifecycles without human intervention.
* **Failure Injection:** Transaction rollbacks, foreign filesystem errors, and interrupted downloads are explicitly simulated to verify convergence and journal safety.

### Running Quality Checks

```bash
# Run all tests with the Go race detector enabled
make test

# Verify database query code generation (sqlc)
make sqlc-check

# Enforce comment length and architectural documentation limits
make comment-check

# Run full CI suite locally (gofmt, vet, build, test, sqlc, comment check)
make check

```

---

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
