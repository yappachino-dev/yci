# Filesystem Action Journal (fs_journal)

Transactional outbox for physical file operations. Ensures that moving,
renaming, or deleting library files happens **only after** the corresponding
metadata transaction commits, and that crashes never leave the disk and the
database in divergent states.

- **Status:** implemented
- **Introduced:** migration `005_fs_journal.sql`
- **Core package:** `internal/db/file_action_outbox.go`
- **Related:** `internal/db/transactor.go`, `internal/app/app.go`, `internal/cli/root.go`, `internal/cli/repair.go`

---

## Problem being solved

Library services used to perform physical filesystem mutations *inside* SQLite
transactions (`diskMover.MoveFile(...)` within `WithinTransaction`). Failure
modes:

1. **Crash mid-transaction** — SQLite rolls back metadata, but files already
   moved on disk → tracks point at nonexistent paths, or orphaned files appear.
2. **Partial batch failure** — move 3 of 10 fails → DB rolls back, moves 1–2
   remain applied.
3. Same for `DeleteFile` (track deletion) and `MoveDirectory` (folder renames).

## Design: redo-only journal (transactional outbox)

The SQLite database itself is the durable log. A dedicated `fs_journal` table
records intended operations **inside the same transaction** as the metadata
change, giving atomicity for free. Execution happens strictly after commit.

```text
┌─ TX ───────────────────────────────────────────┐
│ 1. update tracks / folders rows                │
│ 2. INSERT planned fs ops into fs_journal       │  ← commits atomically together
└────────────────────────────────────────────────┘
3. AFTER COMMIT: post-commit hook executes pending journal entries on disk
4. each executed entry is marked done
```

**Why redo-only (forward recovery) and not undo:** file moves written
defensively are naturally idempotent; the DB remains the source of truth.
An undo log would require snapshotting directory trees — unnecessary here.

### Crash windows

| Crash point                              | Outcome on next startup                          |
|------------------------------------------|--------------------------------------------------|
| Before commit                             | Tx rolls back; journal rows never existed        |
| After commit, before execution            | Entries still `pending`; startup recovery replays|
| Mid-execution                             | Idempotent rules make replay converge (see below)|
| After execution, before marking done      | Replay detects completed work, marks done        |

---

## Schema (`005_fs_journal.sql`)

```sql
CREATE TABLE fs_journal (
    id INTEGER PRIMARY KEY,             -- insertion order == execution order
    op TEXT NOT NULL CHECK (op IN ('move_file', 'move_dir', 'delete_file')),
    src TEXT NOT NULL,                  -- absolute native path
    dst TEXT,                           -- NULL for delete_file
    status TEXT NOT NULL CHECK (status IN ('pending', 'done', 'failed'))
        DEFAULT 'pending',
    error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_fs_journal_status ON fs_journal (status);
```

Design choices:

- **Absolute native paths**, not library-relative: if the configured library
  root changes between record time and recovery, the journal still points at
  the physical locations involved when it was recorded.
- **No separate run/session ID**: single-process CLI; global rowid order is
  causal order across runs.
- Queries live in `db/queries/fs_journal.sql`; generated accessors in
  `internal/store/fs_journal.sql.go`.

## Components

All in `internal/db/file_action_outbox.go` unless noted.

### `FileActionOp` / `FileActionEntry`

Enumerates the physical filesystem operations that can be journaled. Every
operation must be safe to replay because the journal is re-executed after
crashes. Paths are absolute native paths (see above).

Ops: `OpMoveFile`, `OpMoveDir`, `OpDeleteFile`.

### `DiskMover`

Low-level physical primitive used to execute journal entries. Implemented by
`fs.OSDiskMover`. The journal layer never duplicates rename logic; it only adds
ordering, idempotency checks, and durability bookkeeping around this interface.

### `FileActionRepository`

Records and tracks planned filesystem operations in `fs_journal`.

- `Record(ctx, entries)` — persists entries **within the caller's transaction**
  when one is active (resolved via `appctx.TxFromContext`, same pattern as the
  SQLite repositories), otherwise directly on the database. Validates op names
  and non-empty `src`.
- `Pending(ctx)` — all unexecuted entries in insertion order. Insertion order
  *is* execution order: operations are recorded causally by the services.
- `Failed(ctx)` — entries that failed during a previous execution attempt.
- `ResetFailed(ctx)` — requeues failed entries for another attempt.
- `PurgeDone(ctx)` — deletes successfully executed entries (housekeeping).
- `markDone` / `markFailed` — internal state transitions.

### `JournaledMover`

Drop-in replacement for the raw mover wired into library services. Implements
both `library.PhysicalTrackMover` (`MoveFile`, `DeleteFile`) and
`library.PhysicalMover` (`MoveDirectory`) structurally — no service interfaces
had to change.

Behavior:

- **Inside a transaction:** only *records* the intended operation into the
  journal. No disk mutation. Actual execution happens after commit via the
  transactor's post-commit hook.
- **Outside a transaction:** falls back to direct execution through the
  underlying `DiskMover`.

Call sites currently journaled (previously direct disk mutations):

| Call site | Operation |
|---|---|
| `library/staging.go` — staging import | `move_file` (staging → library) |
| `library/track.go` — `MoveByArtist`   | `move_file` per track |
| `library/track.go` — `Delete`         | `delete_file` |
| `library/folder.go` — `Update`        | `move_dir` (folder rename/move) |

### `FileActionExecutor`

Replays journal entries against the physical filesystem.

- `ExecutePending(ctx)` — applies every pending entry in order. On the first
  failure it marks that entry `failed` (with the error text) and stops;
  remaining entries stay `pending` and are retried on the next run or via
  `yci repair`. A joined error is returned if marking the failure also fails.
- Delegates for the CLI: `Pending`, `Failed`, `ResetFailed`, `PurgeDone`.

### Post-commit flush (`internal/db/transactor.go`)

`SQLiteTransactor.SetPostCommit(fn)` registers a hook run after every
successfully committed transaction. Wired to `executor.ExecutePending` in
`app.NewContainer`. Semantics:

- A hook failure does **not** undo the commit; the metadata change stands and
  the error surfaces to the caller while pending journal entries remain
  replayable.

### Startup recovery (`internal/cli/root.go`)

After migrations and container init, every command run replays leftover
journals (`appl.FileActions.ExecutePending`) so the physical library converges
on the committed metadata state before doing anything else. Skipped for
`yci debug migration` invocations that opt out of auto-migration (the table
may not exist yet).

---

## Idempotency rules (redo semantics)

`ExecuteFileAction` decides based on filesystem inspection:

| State                                   | Result                                  |
|-----------------------------------------|-----------------------------------------|
| move: src exists, dst absent            | MkdirAll(dst dir), then rename          |
| move: src missing, dst exists           | already done by earlier attempt → OK    |
| move: both exist                        | **hard failure** (refuses to overwrite) |
| move: neither exists                    | **hard failure** (data lost)            |
| delete: src exists                      | remove                                  |
| delete: src missing                     | already deleted → OK                    |

Without these rules, a crash between "rename done" and "mark done" would make
recovery impossible (or destructive).

Ordering guarantee: entries are executed strictly in rowid order, which matches
the causal recording order (e.g., a folder `move_dir` recorded before track
moves that depend on the new layout).

---

## Operations

### Automatic

Nothing to do — every command start recovers pending journals; every committed
transaction flushes its own journal immediately after commit.

### `yci repair`

Manual intervention tool:

1. Lists `failed` and `pending` entries with their error messages.
2. Resets `failed` → `pending`.
3. Replays all pending entries (fix the underlying cause first — e.g. remove a
   conflicting destination file — then re-run).
4. Purges `done` entries.

Typical flow after a reported failure:

```sh
yci repair            # shows the failing entry + reason, then retries
# resolve the conflict on disk if it fails again, then:
yci repair
```

---

## Testing

`internal/db/file_action_outbox_test.go` (all passing):

| Test | Covers |
|---|---|
| `TestExecutorMovesPendingEntries` | basic replay, dst dir creation, journal drained |
| `TestExecutorIsIdempotentOnReplay` | crash between rename and mark-done |
| `TestExecutorFailsHardOnConflicts` | both-exist refusal, `failed` marking, reset+retry convergence |
| `TestExecutorDeletesAreIdempotent` | delete replay of removed files |
| `TestJournalCommitsWithTransactionAndFlushesAfterCommit` | tx-aware recording + post-commit flush |
| `TestRollbackDiscardsRecordedActions` | rollback leaves zero journal rows and zero disk mutation |
| `TestDirectoryMoveViaJournal` | `move_dir` incl. nested dst creation |
| `TestExecutionStopsAtFirstFailureKeepingRestPending` | fail-fast, remaining stay pending |

---

## Known limitations / follow-ups

- **Cross-device moves**: `os.Rename` fails across mount points. Staging and
  library are assumed to share a filesystem; if that ever changes, the executor
  needs a copy→fsync→rename→delete fallback in `performMove`.
- **Durability nit**: parent directories are not fsync'd after renames; an
  optional best-effort dir sync could be added for power-loss safety.
- **Nested transactions**: resolved. `WithinTransaction` joins an active
  transaction instead of opening a second one, so `ArtistService.Update` →
  `TrackService.MoveByArtist` composes into a single unit rather than
  deadlocking or silently splitting.
- **Concurrency**: safe under the current single-process CLI usage only; a
  process lock would be needed before any parallel invocation is supported.
