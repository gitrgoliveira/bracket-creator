package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Body-size caps for mp-663 Phase 3. The MaxBodyBytes middleware
// enforces the actual request-size cap; the import handler's existing
// ParseMultipartForm(64<<20) is only a memory threshold for form parsing
// (parts above the threshold spill to disk rather than RAM) and does
// not by itself limit total request size. Keeping MaxImportBodyBytes
// at the same numeric value just lets the two layers reason about
// "64 MB" consistently — but the request-size cap lives here, in the
// MaxBodyBytes middleware applied to the import group in server.go.
const (
	// AnnouncementMaxBodyBytes caps POST /api/tournament/announce.
	// Worst-case JSON encoding: 200-char message × 6 bytes/escape + keys
	// ≈ 1242 bytes; 4096 gives ≈3× headroom. Applied via MaxBodyBytes
	// before AuthMiddleware on the /api announce route group in server.go.
	AnnouncementMaxBodyBytes int64 = 4096

	// ResetMaxBodyBytes caps POST /api/tournament/reset.
	// Worst-case JSON: password 256 chars × 6 bytes/escape + originatorId
	// 128 × 6 + overhead ≈ 2344 bytes; 4096 gives ≈1.7× headroom. Applied
	// inside the handler (not as group middleware) to preserve the locked-mode
	// 404 response before any body is read — a per-group cap would reveal the
	// route's existence via 413 vs 404 on locked deployments.
	ResetMaxBodyBytes int64 = 4096

	// DefaultMaxBodyBytes caps non-import admin request bodies. 1 MB is
	// far above any legitimate JSON payload on this server (the largest
	// is a competition with embedded roster ~ a few KB per player ×
	// thousands of players = low hundreds of KB) and well below the
	// point where streaming the body to BindJSON becomes a memory-
	// exhaustion vector.
	DefaultMaxBodyBytes int64 = 1 << 20 // 1 MB

	// MaxImportBodyBytes is the request-size cap for /tournament/import.
	// The handler's ParseMultipartForm(64<<20) is a memory threshold, not
	// a request-size cap — without this middleware-level limit a client
	// could stream a 10 GB body that the form parser would happily spill
	// to disk. 64 MB is sized for a worst-case full-roster CSV plus
	// metadata; raise here if real tournaments outgrow it.
	MaxImportBodyBytes int64 = 64 << 20 // 64 MB
)

// MaxBodyBytes returns a Gin middleware that rejects requests whose
// body exceeds n bytes. Two checks, in order of cost:
//
//  1. Fast path — if Content-Length is set and > n, return 413 before
//     reading a single byte (defends against malicious clients that
//     truthfully advertise a huge body so we can drop them cheaply).
//  2. Defensive wrap — replace c.Request.Body with http.MaxBytesReader
//     so handlers that don't trust Content-Length (or where the client
//     lied / used chunked encoding) still trip the limit during
//     reading. Handlers that read past n get an *http.MaxBytesError
//     surfaced via c.ShouldBindJSON; the existing 500 path renders the
//     error reasonably even if not optimally — a follow-up could map
//     that specific error to 413 inside BindJSON wrappers.
//
// Skips GET/HEAD/DELETE/OPTIONS — those don't carry a body in
// practice and wrapping a nil body would surface false errors.
func MaxBodyBytes(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodDelete, http.MethodOptions:
			c.Next()
			return
		}
		if c.Request.ContentLength > n {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "request body too large",
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		c.Next()
	}
}

// adminGroup creates an /api route group with MaxBodyBytes(capBytes) wired
// BEFORE AuthMiddleware, enforcing the cap→auth ordering that prevents an
// unauthenticated attacker from consuming auth overhead before hitting 413.
// Use this for every protected admin sub-group so the ordering can't be
// accidentally reversed when adding new groups.
func adminGroup(r *gin.Engine, capBytes int64, verifier PasswordVerifier, store *state.Store) *gin.RouterGroup {
	g := r.Group("/api")
	g.Use(MaxBodyBytes(capBytes))
	g.Use(AuthMiddleware(verifier, store))
	return g
}

// requireValidCompID extracts the `:id` URL parameter and validates it
// via state.ValidateCompetitionID. Rejects:
//   - empty
//   - > 64 chars
//   - any character outside [a-zA-Z0-9_-]
//   - a leading non-alphanumeric character (so "_foo", "-foo" are
//     rejected even though "_" and "-" are allowed elsewhere in the
//     string — the regex is ^[a-zA-Z0-9][a-zA-Z0-9_-]*$)
//
// On invalid input, writes a 400 response and returns ("", false); the
// caller should `return` immediately.
//
// Every handler that reads `c.Param("id")` and passes it to
// store.compPath(id, ...) must use this helper. compPath does
// filepath.Clean(filepath.Join(folder, "competitions", id, ...)) — an
// id like "../../../etc/passwd" would cleanly escape the data dir.
//
// Called from BOTH authenticated routes (handlers_competition.go gated
// by AuthMiddleware via X-Tournament-Password) AND the public viewer
// detail route (handlers_viewer.go GET /api/viewer/competitions/:id,
// no auth). Path-traversal defense therefore matters on unauthenticated
// inputs too — anyone on the network can hit the viewer route. Keep
// the regex narrow and apply at every handler entry point.
func requireValidCompID(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if err := state.ValidateCompetitionID(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return "", false
	}
	return id, true
}

// RequireElevatedPassword gates destructive operations behind a SECOND
// password supplied in the X-Admin-Password header (spec 004 / mp-e21). It
// is layered on top of AuthMiddleware — the route group already verified the
// main password, so this only adds the second factor. Attach it per-route to
// the gated subset (competition delete / invalidate / draw / overrides,
// participant roster mutations, CSV import) rather than to the whole group,
// since most admin routes stay single-factor.
//
// Three outcomes, driven by the ElevatedVerifier contract:
//
//   - GateActive() == false  → c.Next(). File mode with no admin password
//     set: the feature is opt-in, so destructive ops behave exactly as
//     before (main password only). This is the back-compat path.
//   - Configured() == false  → 503. Locked mode with no/invalid
//     TOURNAMENT_ADMIN_PASSWORD_HASH: fail closed rather than silently
//     allowing destructive ops with the main password alone.
//   - otherwise               → Verify(X-Admin-Password); 401 on mismatch.
//
// Per the product decision (re-prompt every time) there is no elevation
// token or session — the password travels on each gated request.
func RequireElevatedPassword(ev ElevatedVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enforceElevated(c, ev) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// enforceElevated runs the elevated-password gate decision and returns true
// when the request may proceed. On a blocked request it writes the
// appropriate response (503 unconfigured / 401 wrong / 500 verify-error) and
// returns false — the caller must stop (Abort in middleware, or `return` in a
// handler).
//
// It exists as a standalone function — not just the middleware closure —
// because some gated mutations can't be caught by route-level middleware:
// PUT /api/competitions/:id persists the roster (SaveParticipants/SaveSeeds)
// only when the bound body has a non-nil Players field, so its gate must run
// INSIDE the handler after binding. Sharing this one decision keeps the
// inline check and the middleware byte-for-byte identical (same status codes,
// same fail-closed semantics) — Copilot PR #193 caught that gating only the
// dedicated participant endpoints left the bulk-PUT roster path open.
func enforceElevated(c *gin.Context, ev ElevatedVerifier) bool {
	if !ev.GateActive() {
		return true
	}
	if !ev.Configured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "admin password not configured"})
		return false
	}
	ok, err := ev.Verify(c.GetHeader("X-Admin-Password"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "admin auth verification failed"})
		return false
	}
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid admin password"})
		return false
	}
	return true
}

// AuthMiddleware gates admin endpoints behind the X-Tournament-Password
// header. The actual credential check is delegated to the PasswordVerifier
// so file-based and locked (bcrypt-env-var) modes share a single middleware.
//
// The store reference is needed for the "uninitialized tournament"
// bootstrap branch — that gate fires when no tournament.md exists and we
// must let through the very first POST /api/tournament. The verifier
// owns the policy: file verifier allows anonymous bootstrap (no
// credential exists yet); bcrypt verifier requires the env-var password
// even at bootstrap so an internet-exposed fresh deployment can't be
// race-claimed by a network-reachable attacker.
func AuthMiddleware(verifier PasswordVerifier, store *state.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load tournament config"})
			c.Abort()
			return
		}

		// If no tournament config exists yet (or it's the default blank one in file mode),
		// only allow creating one. In locked mode the stored Password is always empty
		// (auth is bcrypt from env) so the name+password sentinel must be suppressed —
		// otherwise a legitimately-named "New Tournament" record written during locked
		// bootstrap would permanently appear uninitialized (Password == "" matches).
		// EnforceEmptyStoredGuard() is true only in file mode, so the compound check
		// is safe in all cases: in locked mode only t == nil triggers bootstrap.
		if t == nil || (verifier.EnforceEmptyStoredGuard() && t.Name == "New Tournament" && t.Password == "") {
			isBootstrapWrite := (c.Request.Method == http.MethodPut || c.Request.Method == http.MethodPost) && c.FullPath() == "/api/tournament"
			if !isBootstrapWrite {
				c.JSON(http.StatusForbidden, gin.H{"error": "tournament not configured yet"})
				c.Abort()
				return
			}
			if verifier.AllowsFileBootstrap() {
				// File mode: no credential exists on disk yet; let the
				// CreateTournament POST through unauthenticated and the
				// operator picks the password as part of the body.
				c.Next()
				return
			}
			// Locked mode: the env-var hash IS the credential from
			// request 1. Require the header even at bootstrap so a
			// network-reachable attacker can't race-claim the initial
			// tournament record on a fresh deployment.
			ok, verr := verifier.Verify(c.GetHeader("X-Tournament-Password"))
			if verr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "auth verification failed"})
				c.Abort()
				return
			}
			if !ok {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid tournament password"})
				c.Abort()
				return
			}
			c.Next()
			return
		}

		// Self-run mode (mp-7h7): skip the main-password gate for operational
		// routes (scoring, check-in, start, complete, generate draw, etc.) —
		// there is no dedicated table operator, so participants drive their own
		// flow. Destructive routes (delete competition, invalidate, draw,
		// overrides, participant roster mutations, import) remain gated by the
		// EXISTING RequireElevatedPassword / enforceElevated decorators that
		// already fire downstream on those specific routes. No new allowlist
		// needed — the elevated-decorator set IS the allowlist (DRY).
		//
		// Exception — tournament configuration routes (PUT /api/tournament):
		// these mutate setup data including the main password, courts, and
		// check-in windows. In self-run mode there is no separate main-password
		// gate, so these routes fall through to the standard auth path below.
		// The SPA always sends the main password as X-Tournament-Password on
		// PUT /api/tournament even in self-run mode, so this is transparent to
		// existing clients.
		//
		// Officiated mode (the default) skips this block entirely → no regression.
		//
		// Note: LoadTournament returns tournament data with the Mode field. For
		// older files where Mode is empty, ApplyTournamentDefaults normalises it
		// to TournamentModeOfficiated before this comparison — but we don't call
		// ApplyTournamentDefaults on the loaded record here to avoid unexpected
		// mutations. Comparing explicitly against TournamentModeSelfRun is safe:
		// the empty string (old files) and "officiated" both fall through to the
		// standard auth path below.
		isTournamentConfigMutation := c.Request.Method == http.MethodPut && c.FullPath() == "/api/tournament"
		if t.Mode == state.TournamentModeSelfRun && !isTournamentConfigMutation {
			c.Next()
			return
		}

		// Defense-in-depth for the F4 sentinel-into-auth-field scenario
		// (file mode only). The plaintext comparison done by the file
		// verifier is satisfied vacuously when both sides are "" — an
		// unauthenticated client sending no `X-Tournament-Password`
		// header would match an empty stored password and reach c.Next().
		// The POST + PUT handlers in handlers_tournament.go block writes
		// that would land an empty Password through the API, but an
		// operator who manually edits tournament.md (or any out-of-band
		// write bypassing the handlers) could still land an empty
		// Password on disk. The uninitialized branch above only covers
		// the literal "New Tournament" + empty case — a real-named
		// tournament with empty Password is a misconfiguration, and
		// refusing to authorize is the safer fail-closed choice. The
		// 403 message tells the operator to fix the password rather than
		// the misleading 401 ("invalid tournament password" — which
		// would imply the request is wrong, not the server state).
		//
		// In locked mode the stored Password is irrelevant (auth comes
		// from the bcrypt env-var hash), so this guard is suppressed via
		// verifier.EnforceEmptyStoredGuard() — otherwise it would 403
		// every request whenever the operator leaves the on-disk
		// password empty (or migrates from a fresh install).
		//
		// Recovery (file mode): since this branch returns 403 BEFORE
		// the password check OR the uninitialized-bootstrap branch can
		// run, there's no API path to fix the password (the bootstrap
		// exception only matches the literal "New Tournament" default
		// name). Operator can either repair the file out-of-band, OR
		// use the new POST /api/tournament/reset endpoint (added with
		// the locked-password mode work) which is unauthenticated by
		// design and writes a new Password to the existing record.
		if verifier.EnforceEmptyStoredGuard() && t.Password == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "tournament misconfigured: password is not set"})
			c.Abort()
			return
		}

		ok, verr := verifier.Verify(c.GetHeader("X-Tournament-Password"))
		if verr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "auth verification failed"})
			c.Abort()
			return
		}
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid tournament password"})
			c.Abort()
			return
		}

		c.Next()
	}
}
