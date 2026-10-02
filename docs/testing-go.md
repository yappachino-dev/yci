# Testing in Go — Project Standard

How tests are written in this repository: what to test, how to structure
tests, which doubles are allowed where, and what "done" means for each change
type. The canonical reference for style details is the standard library and
`testing` package idioms; this document adapts them to yci.

---

## 1. The policy (short version)

1. **Bug fixes require a regression test** that fails without the fix.
2. **Behavior changes require tests**; pure refactors rely on existing
   coverage — which obligates us to keep coverage real.
3. **Storage is tested against real SQLite and real temp directories.** This
   project's storage is embedded, so "integration" testing costs nothing and
   mocks would only re-implement SQLite badly.
4. **Mocks/fakes exist only at true external boundaries** — shelling out to
   yt-dlp and ffprobe, and nothing else. Both are behind interfaces
   (`Downloader`, `PlaylistFetcher`, `probe.Prober`) so they can be faked
   without a toolchain or a network. Never fake the database or the
   filesystem; both are embedded and real temp directories cost nothing.
   A package-level `exec.CommandContext` reached directly from a loop is the
   thing to look for: it means the code above it cannot be tested at all.
5. Tests run with `-race`, always (`make test`).

## 2. What to test, per layer

| Layer | Test type | Against what | Example |
|---|---|---|---|
| Pure logic (`url.go`, `filename.go`, slug functions) | table-driven unit | nothing — in/out | hybrid URL parsing, filename sanitization |
| Domain services over storage | integration | real SQLite + temp dirs | folder rename rewrites track paths |
| Durability machinery | failure-injection | real SQLite + temp dirs + failing movers | journal replay after partial execution |
| Repositories | thin integration | real SQLite via migrations | `UpdatePathsByPrefix` matches expected rows |
| CLI | golden output / e2e | built binary against sandboxed `$HOME` | `ls tracks` column format |

Priorities when writing coverage into an untested area: **(1)** path math and
state machines (highest blast radius), **(2)** parsers, **(3)** transactional
flows end-to-end, **(4)** presentation. Never start with trivial getters.

## 3. Canonical structure

### Table-driven tests — the default form

Use whenever multiple inputs exercise one behavior:

```go
func TestParseURL(t *testing.T) {
    tests := []struct {
        name      string
        rawURL    string
        wantType  URLType
        wantVideo string
        wantList  string
    }{
        {
            name:      "standard watch url",
            rawURL:    "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
            wantType:  URLTypeVideo,
            wantVideo: "dQw4w9WgXcQ",
        },
        {
            name:     "youtu.be short link",
            rawURL:   "https://youtu.be/dQw4w9WgXcQ?t=42",
            wantType: URLTypeVideo,
            ...
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseURL(tt.rawURL)
            if err != nil {
                t.Fatalf("ParseURL(%q) returned error: %v", tt.rawURL, err)
            }
            if got.Type != tt.wantType {
                t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
            }
            ...
        })
    }
}
```

Rules:

- Subtests get descriptive lowercase names — they become failure output.
- `t.Fatalf` to stop when continuing is meaningless (setup failed);
  `t.Errorf` to collect all failures in one run.
- Failure messages contain got/want and enough context to reproduce
  (`%q` around inputs).
- One logical assertion family per test function; don't merge unrelated
  behaviors into one table.

### Per-test environment constructors

When several tests need the same scaffold, build it in a helper:

```go
func newTestEnv(t *testing.T) *testEnv {
    t.Helper()
    dbConn := newTestDB(t)          // registers t.Cleanup(Close)
    ...
}

func mustWrite(t *testing.T, path, content string) {
    t.Helper()                      // failure lines point at the caller
    ...
}
```

- `t.Helper()` on every helper that asserts or fatals.
- Cleanup via `t.Cleanup(...)` and `t.TempDir()` — never `os.RemoveAll` by
  hand, never shared state between tests.
- Reference implementations: `internal/db/file_action_outbox_test.go`.

### Failure injection

Durability code is tested by simulating its failure modes explicitly:

```go
// Crash between rename and mark-done: physical move happened,
// journal entry still pending — replay must converge.
mustWrite(t, dst, "audio")
env.repo.Record(ctx, []FileActionEntry{{Op: OpMoveFile, Src: src, Dst: dst}})
if err := env.executor.ExecutePending(ctx); err != nil { ... }
```

Every durable component gets: happy path, replay-after-crash, conflict,
rollback-discards-everything, stop-at-first-failure. If you add durability
machinery and cannot name its five failure-mode tests, the design isn't done.

## 4. Assertions

- Stdlib `testing` only — no assertion libraries. Comparisons are explicit;
  helpers like a generic `wantEqual[T comparable]` may be added once
  repetition justifies it.
- Errors: assert identity/behavior with `errors.Is`/`errors.As`, not string
  matching. Where error *text* is user-facing contract, one dedicated test
  pins it.
- Compare whole structs where practical (`got != want` on value types,
  `reflect.DeepEqual` sparingly, `cmp.Diff` if introduced later) — field-by-
  field checks hide missing-field bugs.

## 5. Parallelism & isolation

- Default: sequential within a package (shared temp DBs make ordering
  assumptions tempting — don't).
- Use `t.Parallel()` only when tests provably share no state (pure-function
  tables may).
- No `time.Sleep` synchronization anywhere. Poll with deadlines:

```go
waitFor(t, func() bool { return fileExists(path) })
```

- No cross-test dependencies, no ordered test suites. Each test constructs
  its world from zero.

## 6. Naming & placement

- Same package as the code (`package db`), file `foo_test.go`. A separate
  `_test` package is reserved for future e2e suites exercising the public CLI.
- Test names describe behavior: `TestRollbackDiscardsRecordedActions`, not
  `TestWithinTransaction2`.
- Helper names follow repo conventions: `newTestX` builders, `mustX`
  fail-fast actions, `want*` fields in tables.

## 7. Coverage discipline

- Measure honestly: `go test ./... -race -coverprofile=cover.out && go tool
  cover -func=cover.out`.
- No global percentage target — instead: **every package containing logic
  beyond wiring has meaningful coverage of its decision points**. Wiring
  (`internal/app`) and generated code (`internal/store`) are exempt.
- Uncovered branches in durable code (journal, transactor, queue retries) are
  treated as bugs waiting to be written.

## 8. What NOT to test

- Generated sqlc code directly (covered transitively through repositories).
- Trivial delegation with no logic (`All → repo.All`) unless it anchors an
  API contract worth pinning.
- Cobra flag plumbing mechanics; test command *bodies* via services where
  possible, and reserve binary-level e2e for a handful of smoke tests.

## 9. Adding tests to untested code

Pattern this project actually used to retire debt: before refactoring a module,

1. Write characterization tests around current behavior using real storage.
2. Make them pass without touching production code.
3. Only then refactor, keeping tests green — adjusting expectations only when
   behavior change is deliberate and stated in the commit message.

This converts "rewrite and pray" into verifiable migration, and every such
pass permanently retires an untested module.
