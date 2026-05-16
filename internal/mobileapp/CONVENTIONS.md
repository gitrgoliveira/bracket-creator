# `internal/mobileapp` conventions

Reference notes for handlers in this package. Keep edits tight — this is a developer-facing pointer doc, not a tutorial.

## State-store lock pattern (current state, T017a)

The mobileapp HTTP layer holds **no mutexes of its own**. Every handler that needs concurrency safety routes its mutation through one of the `state.Store` atomic primitives below, which take the appropriate lock internally. This means:

- The `Server` / `Hub` / handler closures do not synchronize directly.
- Concurrency correctness is defined entirely by which `Store` method a handler calls (and by extension, which lock that method takes).
- Tests can assume that a successful handler return implies the store mutation already committed under the right lock.

### Locks owned by `state.Store`

| Lock | Scope | Source |
|---|---|---|
| `s.mu sync.RWMutex` | Store-wide state (folder ops, competition list iteration, tournament file-cache invalidation when paired with `s.tournamentMu`). | `state/store.go` |
| `s.compMu sync.Map` | Per-competition `sync.RWMutex`, accessed via `s.getCompLock(id)`. Each competition's read/write operations serialize on its own lock; different competitions are independent. | `state/store.go` |
| `s.compRenameMu sync.Mutex` | Coarser than per-comp locks, finer than `s.mu`. Serializes "uniqueness-check + save" sequences across all competitions to close the AB-BA rename race (two concurrent renames of different comps to the same name). Acquired via `Store.WithCompetitionRenameLock`. | `state/store.go` |
| `s.tournamentMu sync.RWMutex` | Tournament file cache + tournament write operations. | `state/store.go` |
| `fileCache.mu sync.RWMutex` | Per-(competition, filename) cache invalidation lock, used inside the `loadCached` helper. Handlers never touch this directly. | `state/store.go` |

Engine-level state (`standingsCache`, `standingsFlight` in `engine/engine.go`) uses lock-free `sync.Map` + `sync.Once` for the cold-cache flight; no exclusive mutex.

### Atomic primitives — handlers should call these

These are the load + transform + save primitives. **Use them instead of `Load…` followed by `Save…` whenever the save depends on the loaded state.** Sequential Load + Save without a shared lock has a TOCTOU window that concurrent writers can land mutations into; the primitives close that window by holding the appropriate lock across both halves.

| Primitive | Lock held across the closure | When to use |
|---|---|---|
| `Store.UpdateCompetitionChanged(id, transform)` | per-competition write lock | Mutating a competition's `config.md` based on its current state (status transitions, settings merges). |
| `Store.UpdateTournamentChanged(desired, transform)` | `s.mu` + `s.tournamentMu` | Mutating `tournament.md` based on its current state (e.g. password preserve-on-empty). |
| `Store.UpdatePoolMatchByID(compID, matchID, mutate)` | per-competition write lock | Mutating a single pool match's score / status. |
| `Store.UpdateBracket(compID, mutate)` | per-competition write lock | Mutating any bracket match's score / propagating winners. |
| `Store.WithCompetitionRenameLock(fn)` | `s.compRenameMu` | Wrapping a "uniqueness-check + save" sequence (POST /competitions, PUT /competitions/:id rename, POST /playoffs). |

If you need a sequence the existing primitives don't cover, add a new one rather than reaching past the lock — the `Store` is the only place that knows the lock layout.

### Lock ordering rules (today)

1. **`s.compRenameMu` is acquired BEFORE per-competition locks.** Inside a `WithCompetitionRenameLock` closure, calls to `LoadCompetition` / `SaveCompetitionChanged` / `UpdateCompetitionChanged` for any `id` are safe — the rename mutex is a different mutex from any per-comp lock, and the per-comp locks are taken one at a time inside the closure. No AB-BA possible because the rename mutex serializes the outer operation.

2. **Transforms passed to `UpdateXxxChanged` MUST NOT recursively call into the same lock.** `sync.Mutex` is non-recursive — a recursive call would deadlock. Specifically: the transform inside `UpdateCompetitionChanged(id, …)` MUST NOT call `SaveCompetition`, `SaveCompetitionChanged`, `UpdateCompetitionChanged`, or any other Store method that takes the per-comp lock for that same `id`. Cross-resource work (e.g. SaveParticipants) must run AFTER the `UpdateCompetitionChanged` call returns. Same advisory for `UpdateTournamentChanged`, `UpdateBracket`, `UpdatePoolMatchByID`, and `WithCompetitionRenameLock`.

3. **Transforms MAY call into methods that take OTHER locks.** For example, an `UpdateCompetitionChanged` transform calling `LoadCompetition` for a DIFFERENT comp ID (as `checkUniqueCompName` does inside `WithCompetitionRenameLock`) is safe — the per-comp locks are independent.

4. **Read operations should use `Load…` (not the atomic primitives).** `LoadCompetition`, `LoadTournament`, `LoadParticipants`, etc. take read locks. The atomic primitives are for write paths.

### Planned: `Store.WithTransaction` (Slice 6 / NFR-010)

Slice 6 lands `Store.WithTransaction(fn func(tx StoreTx) error) error` (tasks T155/T156) which formalizes the lock-ordering rule into a single primitive. The rough shape:

- `WithTransaction` acquires the relevant locks in a fixed order — **competition mutex first, then any match-level mutex** (anticipating per-match locks added in later work, though the current tree has none).
- The `tx StoreTx` handle is restricted to lock-free read/write methods, so transforms can't accidentally re-enter a lock.
- Commit happens on `fn` returning `nil`; rollback (no save) on error.

Three handlers are scheduled to migrate first (T156): the score-update path, the lineup PUT, and the decision endpoint. Until that lands, the slice-3 race-condition guards (T105 CHK047, T128a CHK048) use the existing per-competition lock via `UpdateCompetitionChanged` — this CONVENTIONS.md is the reference they cite.

### When in doubt

- Adding a new write handler? Find the resource it mutates, look up the matching `Update…Changed` / `Save…Changed` primitive in the table above, and call that. Don't open-code Load + mutate + Save.
- Adding a new read handler? Use the `Load…` family. They take read locks internally and return deep copies, so the returned value is safe to mutate without affecting cached state.
- Need a sequence the primitives don't cover? Add a new primitive in `state/`. Don't reach past the lock from a handler.
