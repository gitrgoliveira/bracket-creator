# Spec 004 : Elevated (second) password for destructive operations

**Issue:** mp-e21
**Status:** Draft : awaiting approval before implementation
**Author:** Claude (Opus 4.8) with Ricardo Oliveira
**Date:** 2026-05-29

---

## 0. Revision history

- **v2 (2026-05-29, post critical-thinking review)** : fixed two fatal flaws in v1:
  1. v1 said to store/return `adminPassword` mirroring `Password`. That would have
     **leaked** the elevated password via `GET /tournament` and **allowed
     overwrite** via `PUT /tournament` : both gated only by the main password,
     defeating the feature. v2 makes the field **write-only** (`json:"-"`, §6) and
     moves setting it to a **dedicated elevated-gated endpoint** (§6a).
  2. Added an explicit **threat model** (§1a) : this is an insider speed bump, not
     a network control; over HTTP both secrets are sniffable.

## 1. Problem

Today the mobile app has a single admin credential, resolved at startup by one
`PasswordVerifier` ([internal/mobileapp/auth_source.go](../../internal/mobileapp/auth_source.go)):

- **file mode** (default) : plaintext compare against `password:` in `tournament.md`.
- **locked mode** (`--lock-password` + `TOURNAMENT_PASSWORD_HASH`) : bcrypt compare.

Any operator who knows the admin password can perform *every* action, including
irreversible ones (deleting a competition, wiping a generated draw, rewriting the
participant roster). During a live event the admin password is often shared with
table staff. The operator wants a **second, separately-held password** that gates
only the genuinely destructive operations, so routine staff can score matches and
manage check-in without being able to destroy data.

## 1a. Threat model (what this does and does NOT defend)

State this in the operator docs so the feature is not over-trusted.

**Defends against:** an *insider* who legitimately holds the shared main password
(table/scoring staff) but is not entrusted with the elevated password : accidental
or unauthorized deletion of competitions, draws, overrides, and roster edits. This
is the operator's actual stated need.

**Does NOT defend against:**
- A **network attacker** on an HTTP (non-TLS) deployment : the documented LAN
  default. Both `X-Tournament-Password` and `X-Admin-Password` travel in cleartext
  and are sniffable. The second password adds nothing here. Operators wanting a
  real network boundary must run behind TLS and/or `--lock-password`.
- Anyone with **filesystem access** to `tournament.md` (file mode) : they read
  both plaintext passwords directly. The credential boundary is API-level only.

The feature is a **privilege-separation speed bump for shared-credential
operation**, not a cryptographic access-control system. RBAC / per-user identity
is explicitly out of scope (§12).

## 2. Decisions (locked with the user : 2026-05-29)

| # | Question | Decision |
|---|----------|----------|
| D1 | Which operations are gated? | **Data-destructive ops only** (see §3). |
| D2 | Session model? | **Re-prompt every time.** No elevation token, no `/auth/elevate` endpoint, no in-memory token store. The password travels in a per-request header. |
| D3 | Locked-mode storage? | **Separate bcrypt env var** `TOURNAMENT_ADMIN_PASSWORD_HASH`. If unset, gated endpoints return **503**. |
| D4 | Does the elevated password gate `/api/tournament/reset`? | **No.** That endpoint is the *forgotten-main-password recovery path* : it is intentionally public and already protected by a same-origin check + locked-mode 404. Gating it behind a second password would create a recovery lockout. |
| D5 | Gate draw/override deletions + invalidate? | **Yes** : they discard generated bracket/pool data. |

### Key correction captured during research
The bd issue framed "tournament reset" as "wipes all tournament data." That is
**inaccurate**: `POST /api/tournament/reset`
([handlers_reset.go](../../internal/mobileapp/handlers_reset.go)) only **rotates the
admin password** and is the recovery path for a forgotten password. There is **no
data-wipe endpoint** in the codebase. Per D4 the recovery endpoint is left untouched.

## 3. Scope : gated operations

All gated endpoints already sit behind `AuthMiddleware` (main password). The
elevated check is an **additional** middleware layered on top, so a gated request
must present **both** `X-Tournament-Password` **and** `X-Admin-Password`.

**Gated (require `X-Admin-Password`):**

| Method & path | Handler | Why |
|---|---|---|
| `DELETE /api/competitions/:id` | handlers_competition.go:830 | Deletes a competition + all its data |
| `POST /api/competitions/:id/invalidate` | handlers_competition.go:854 | Discards results, returns comp to draft |
| `DELETE /api/competitions/:id/draw` | handlers_competition.go:1048 | Discards a generated draw |
| `DELETE /api/competitions/:id/overrides` | handlers_competition.go:1355 | Discards manual rank overrides |
| `POST /api/competitions/:id/participants` | handlers_participants.go:34 | Roster add / full-list replace |
| `PUT /api/competitions/:id/participants/:pid` | handlers_participants.go:222 | Roster edit |
| `PUT /api/competitions/:id` *(when body has non-nil `players`)* | handlers_competition.go:396 | **Bulk roster writer** : persists participants/seeds via `SaveParticipants`/`SaveSeeds`. The SPA's *primary* roster flow (paste/import, seed edits go through `API.updateCompetition`). Gated **inline** (not via route middleware, which runs before the body is bound) using the shared `enforceElevated`. Settings-only PUTs (`players == nil`) stay single-factor. **Added after Copilot caught that gating only the dedicated participant endpoints left this path open (PR #193).** |
| `POST /api/tournament/import` | handlers_import.go:57 | CSV import : replaces roster wholesale |

> **Why the bulk PUT needs an inline gate, not route middleware.** `RequireElevatedPassword`
> runs before the handler binds the JSON body, so it can't see whether the request
> carries a roster (`players`) vs. a settings-only change. The handler therefore calls
> `enforceElevated(c, elevated)` itself, immediately after binding, only when
> `comp.Players != nil`. Both paths share the one `enforceElevated` decision so the
> status codes / fail-closed semantics stay identical.
>
> **Seeds:** the dedicated `PUT /api/competitions/:id/seeds` endpoint (which calls only
> `SaveSeeds`, never the roster file) remains **ungated** per the "seeds are a ranking
> aid" decision. But the SPA's seed-reorder UI submits the *full roster payload* through
> `API.updateCompetition`, which rewrites `participants.csv` : so those flows take the
> roster gate. This is correct: anything that rewrites the roster file is gated; the
> seed-only endpoint that never touches it is not. The roster file cannot be replaced
> through any ungated path. (`POST /competitions` rejects an existing ID with 400, so it
> can only create new competitions, never overwrite an existing roster.)

**NOT gated (routine live-tournament ops : main password only):**

- Check-in toggles: `PUT/DELETE .../checkin`, `POST .../checkin-bulk`
- Seeds: `PUT /api/competitions/:id/seeds` (ranking aid, not roster membership)
- Competition lifecycle: `start`, `generate-draw`, `complete`, `playoffs`
- Match scoring, decisions, lineups, daihyosen, swiss, announcements
- `POST /api/tournament/reset` (recovery path : see D4)
- All `GET` / viewer / public endpoints

> **Open confirmation:** there is no `DELETE participant` route today : roster
> deletion happens via the full-list `POST .../participants` body. Gating the POST
> covers it.

## 4. Auth core

New file: **`internal/mobileapp/auth_admin.go`**.

```go
// ElevatedVerifier gates the destructive-op middleware. It is intentionally
// NARROWER than PasswordVerifier: no bootstrap, no reset semantics, no
// stored-password redaction policy : those belong to the primary credential.
type ElevatedVerifier interface {
    // GateActive reports whether the middleware should enforce at all.
    //   file mode:   true only when an admin password has been set.
    //   locked mode: always true (fail-closed).
    GateActive() bool

    // Configured reports whether a credential exists to compare against.
    //   file mode:   admin password non-empty (== GateActive there).
    //   locked mode: TOURNAMENT_ADMIN_PASSWORD_HASH was a valid bcrypt hash.
    Configured() bool

    // Verify checks the presented X-Admin-Password value.
    Verify(presented string) (bool, error)

    // Mode mirrors PasswordVerifier.Mode for /auth-config ("file"|"locked").
    Mode() string
}
```

Two implementations, mirroring `auth_source.go`:

- **`fileElevatedVerifier{store}`** : loads `tournament.md`, reads new
  `AdminPassword` field, plaintext compares. `GateActive()`/`Configured()` ==
  `AdminPassword != ""`.
- **`bcryptElevatedVerifier{hash []byte}`** : locked mode, env-var bcrypt. Reuses
  the exact hardening already proven in `bcryptPasswordVerifier` (length pre-check
  at `bcryptMaxInputBytes`, `errors.Is` on `ErrMismatchedHashAndPassword` /
  `ErrPasswordTooLong`, no timing/differential-error leak). `GateActive()` always
  true. A `bcryptElevatedVerifier` with no/invalid hash is represented by a
  `lockedUnconfiguredElevatedVerifier` whose `GateActive()==true`,
  `Configured()==false` → drives the 503 branch.

### Construction ([cmd/mobile_app.go](../../cmd/mobile_app.go))
- file mode → `NewFileElevatedVerifier(store)`.
- locked mode → read `TOURNAMENT_ADMIN_PASSWORD_HASH`:
  - valid bcrypt → `bcryptElevatedVerifier`.
  - empty/invalid → `lockedUnconfiguredElevatedVerifier` (do **not** fail startup; the operator may not need destructive ops; they get 503 only if they try one). Log a `slog.Warn` so the gap is visible.

## 5. Middleware ([internal/mobileapp/middleware.go](../../internal/mobileapp/middleware.go))

```go
func RequireElevatedPassword(ev ElevatedVerifier) gin.HandlerFunc {
    return func(c *gin.Context) {
        if !ev.GateActive() {          // file mode, no admin pw set → back-compat no-op
            c.Next(); return
        }
        if !ev.Configured() {          // locked mode, env hash unset → fail-closed
            c.JSON(http.StatusServiceUnavailable,
                gin.H{"error": "admin password not configured"})
            c.Abort(); return
        }
        ok, err := ev.Verify(c.GetHeader("X-Admin-Password"))
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "admin auth verification failed"})
            c.Abort(); return
        }
        if !ok {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid admin password"})
            c.Abort(); return
        }
        c.Next()
    }
}
```

Runs **after** `AuthMiddleware` (the admin group already ran it). Applied
per-route, not per-group, because the gated set is a *subset* of `adminSmallBody`.
Add `X-Admin-Password` to the CORS `Access-Control-Allow-Headers` list in
server.go.

### Wiring
Two viable shapes; recommend **(a)** for minimal blast radius:

- **(a) Per-route** : `RegisterCompetitionHandlers` / `RegisterParticipantHandlers`
  / `RegisterImportHandlers` gain an `elevated ElevatedVerifier` param and attach
  `RequireElevatedPassword(elevated)` to exactly the gated routes via
  `group.DELETE(path, RequireElevatedPassword(ev), handler)`.
- (b) A dedicated sub-group : awkward because gated routes are interleaved with
  non-gated ones sharing the same `:id` prefix.

## 6. Storage ([internal/state/models.go](../../internal/state/models.go))

> **⚠ Corrected after critical review (see §0).** The elevated password is a
> *higher* privilege than the main password that gates `/tournament`. It must
> therefore be **write-only over the API in every mode** : never serialized in a
> response, never settable through the main-password-gated PUT. This is the
> opposite of how `Password` is handled, and the difference is the whole point of
> the feature.

Add to `Tournament`:
```go
// AdminPassword gates destructive operations (spec 004). It is WRITE-ONLY at
// the API boundary: `json:"-"` guarantees it is never emitted in any response
// AND never populated by binding a request body into a Tournament : so it
// cannot be read or overwritten through the main-password-gated /tournament
// endpoints. It is set only via the dedicated, elevated-gated endpoint in §6a.
// Persisted to tournament.md via the yaml tag.
AdminPassword string `yaml:"admin_password,omitempty" json:"-"`
```

- **`json:"-"` closes Findings 1 & 2 by construction**: `GET /tournament`
  (handlers_tournament.go:196) can no longer leak it, and `PUT /tournament`
  (`ShouldBindJSON` into `Tournament`) can no longer set it.
- **Preserve-on-write**: the existing `PUT /tournament` transform replaces the
  stored record. Because the bound struct's `AdminPassword` is always `""`
  (json:"-"), the transform MUST copy `AdminPassword` from the current on-disk
  record forward (exactly as it already does for `Password` at
  handlers_tournament.go:298-303) : otherwise a routine settings save would wipe
  the elevated password. Add a regression test for this.
- **Locked mode**: `AdminPassword` is irrelevant to auth (env hash wins). With
  `json:"-"` it is never emitted regardless of mode, so no extra
  `RedactStoredPassword`-style branch is needed for it. A stale on-disk value is
  inert.

## 6a. Setting / rotating the elevated password : dedicated endpoint

A new endpoint owns the credential, so the privilege rules live in one place:

```
PUT /api/auth/admin-password      (file mode only; 404 in locked mode)
body: { "newPassword": "...", "currentPassword": "..." (required iff already set) }
```

Gating logic (bootstrap = trust-on-first-use, rotation = prove-you-hold-it):

| State | Auth required | Rationale |
|-------|---------------|-----------|
| No admin password set yet | Main password only (`X-Tournament-Password`) | TOFU bootstrap. There is no elevated secret to prove yet; the operator setting it for the first time is the same trust level as configuring the tournament. |
| Admin password already set | Main password **AND** `X-Admin-Password` matching the *current* value | Rotation must prove possession of the current elevated secret : otherwise a main-password holder could silently re-set it (Finding 2). Equivalent to "change-password requires old password". |
| Locked mode | : | `404`. The credential is the env-var hash; not settable via API. SPA shows the read-only "controlled by env var" message. |

This endpoint lives **outside** the bulk `/tournament` PUT precisely so the
conditional gating (TOFU vs prove-current) is explicit and testable, not smuggled
into a handler whose other fields only need the main password.

## 7. UI ([web-mobile/](../../web-mobile/))

- **Settings page** : new "Admin / destructive-ops password" control next to the
  existing tournament password input. The current value is **never displayed**
  (write-only, §6) : the field is an empty "set / change" input, like any
  change-password form.
  - file mode → editable; saved via the **dedicated** `PUT /api/auth/admin-password`
    endpoint (§6a), **not** the general tournament PUT. When a value is already
    set, the form also collects the current admin password to authorize rotation.
  - locked mode → read-only, shows "controlled by `TOURNAMENT_ADMIN_PASSWORD_HASH`".
  - State (set vs unset vs env-controlled) comes from `/auth-config` (§8).
- **Dangerous-op flow** (re-prompt every time, D2): a small `promptAdminPassword()`
  modal. When a gated action fires (delete competition, edit/import roster), prompt,
  then send the value in the `X-Admin-Password` header for that one request.
  - `401` → "Incorrect admin password", re-prompt.
  - `503` → "Admin password not configured on this server."
  - No caching (per D2). A single multi-field roster edit may reuse the value for
    its own submit only.
- A shared client helper (`web-mobile/js/api*.js`) wraps gated calls so the header
  plumbing lives in one place. Mirror existing `X-Tournament-Password` handling in
  `api.js`.

## 8. auth-config exposure ([handlers_auth_config.go](../../internal/mobileapp/handlers_auth_config.go))

Extend `authConfigResponse`:
```go
ElevatedRequired  bool `json:"elevatedRequired"`  // ev.GateActive()
ElevatedEditable  bool `json:"elevatedEditable"`  // file mode only
ElevatedConfigured bool `json:"elevatedConfigured"` // ev.Configured()
```
The SPA reads these on mount to decide whether to prompt for the admin password
at all and whether to render the Settings field editable.

## 9. Migration / back-compat

- **File mode**: existing deployments have no `admin_password` → `GateActive()==false`
  → destructive ops behave exactly as today (main password only). Opt-in by setting
  the field in Settings. **No breaking change.**
- **Locked mode**: a deployment that upgrades and does *not* set
  `TOURNAMENT_ADMIN_PASSWORD_HASH` will get **503 on the gated endpoints** (per D3).
  This **is** a behavior change for locked deployments : documented as a release
  note and in `docs/user-guide/mobile-app.md`. Rationale: locked mode is the
  hardened, internet-exposed posture; fail-closed on destructive ops is the
  correct default there.

## 10. Tests

- **Write-only guarantee (regression for Findings 1 & 2)**: `GET /tournament` in
  file mode never includes `admin_password`/`adminPassword` in the JSON body even
  when set on disk; `PUT /tournament` with `adminPassword` in the body does NOT
  change the stored elevated password; a routine `PUT /tournament` (other fields)
  preserves the existing `AdminPassword` rather than wiping it.
- **Dedicated endpoint** (`PUT /api/auth/admin-password`): TOFU first-set with main
  password only; rotation requires correct current admin password (wrong → 401/403);
  404 in locked mode.
- **Verifier** (`auth_admin_test.go`): file mode set/unset; locked configured;
  locked unconfigured; bcrypt length/mismatch hardening parity with
  `auth_source_test.go`; GateActive short-circuits before Verify so an empty
  on-disk admin password + empty header never vacuously matches (F4-style guard).
- **Middleware**: `GateActive==false` pass-through; 503 when unconfigured; 401 on
  wrong header; 200 on correct header; verifies it runs *after* main-password auth
  (a request missing `X-Tournament-Password` still 401s at the outer layer).
- **Endpoint integration**: each gated route returns 401/503 without/with bad
  `X-Admin-Password` and 2xx with the correct one; each non-gated route is
  unaffected.
- **Storage**: `AdminPassword` round-trips through `tournament.md`; redacted in
  locked-mode responses; preserved on locked-mode writes.
- **auth-config**: the three new bits reflect each mode.
- **Frontend** (vitest): prompt-on-gated-action, 401 re-prompt, 503 message,
  Settings field editable/read-only per mode.

## 11. Coordination

- **mp-j39** (Remove reserved slots) is **in_progress** and also touches
  `handlers_competition.go` + participant endpoints. Per its design note, **land
  mp-j39 first**, then rebase this branch. The reserved-slots routes
  (`/reserved-slots`) are being removed by mp-j39 and are *not* in this spec's gated
  set, so the conflict is limited to nearby lines in the same files.

## 12. Out of scope

- Elevation tokens / sessions (explicitly rejected, D2).
- Gating routine lifecycle/scoring ops.
- A data-wipe endpoint (none exists; not requested).
- Per-user identities / RBAC (single shared elevated secret only).

## 13. Phased delivery

1. **Phase 1 : core**: `auth_admin.go` + verifier tests; `AdminPassword` field
   (`json:"-"`, write-only) + PUT-preserve logic; the dedicated
   `PUT /api/auth/admin-password` endpoint (§6a) with TOFU/rotation gating;
   construction in `cmd/mobile_app.go`.
2. **Phase 2 : middleware + wiring**: `RequireElevatedPassword`, per-route
   attachment, CORS header, endpoint integration tests.
3. **Phase 3 : auth-config**: extend response + tests.
4. **Phase 4 : UI**: Settings field, prompt modal, client helper, vitest.
5. **Phase 5 : docs**: `docs/user-guide/mobile-app.md`, release note for the
   locked-mode 503 behavior change.
