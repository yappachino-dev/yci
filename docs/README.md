# Documentation Index

Design docs and feature notes. Each doc is self-contained: read it for context
without scanning the implementation.

Everything here is **proven** information that was worth keeping. Working
material — checklists, diffs, command output, anything still being decided —
belongs in `.agent/` instead, which is gitignored and cleared when a task
ends. If a file here stops being true, fix or delete it; do not let it
accumulate status updates.

For AI-assistant session context (working agreements, momentum, environment
quirks), see `AGENTS.md` at the repository root.

| Doc | Feature | Status |
|---|---|---|
| [title-interpretation.md](title-interpretation.md) | Title interpretation — stripping vs inference, why only stripping is applied, what is stored, and what is deliberately not done | implemented |
| [playlists.md](playlists.md) | Playlists — DB-owned ordered track sequences, m3u export into the library, relative-path portability | implemented |
| [config.md](config.md) | Configuration system — layered sources, strict TOML parsing, `yci config` command group, deliberate non-features | implemented |
| [fs-journal.md](fs-journal.md) | Filesystem action journal — transactional outbox making file moves/deletes crash-safe (redo replay, post-commit flush, startup recovery, `yci repair`) | implemented |
| [style-guide.md](style-guide.md) | Production-grade style guide — design principles, layering, naming, errors, SQL, CLI, comment length, tooling gates | living document |
| [technical-debt.md](technical-debt.md) | Debt register — open flaws only, with severity and remediation plan; entries are deleted when fixed | living document |
| [testing-go.md](testing-go.md) | Testing standard — what to test per layer, table-driven form, failure injection, coverage rules | living document |
| [error-handling-go.md](error-handling-go.md) | Error handling standard — wrapping, sentinels, message craft, layer responsibilities, review checklist | living document |

## Conventions

- One markdown file per feature/major subsystem, named after the feature
  (e.g. `fs-journal.md`), kept at the repo root of `docs/`.
- Each doc starts with a header block: status, introducing migration, core
  package(s), and related files — so it can be located and trusted quickly.
- Docs describe *why* and *how it behaves*; the code remains the source of
  truth for *exact* signatures. Update the doc in the same change as the code.
- Code comments are kept short and precise; `make comment-check` reports
  blocks over the limits in the style guide.
- `technical-debt.md` holds **open** items only. An entry is deleted when its
  work lands rather than marked resolved — `git log` already records
  finished work, and two records of it only drift apart.
