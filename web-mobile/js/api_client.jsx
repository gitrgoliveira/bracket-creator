// HTTP client for the mobile-app API.
//
// Split out of api.jsx (T006 / NFR-006). The serializer helpers
// (normalizeMatch, normalizeCompetitionDetail, etc.) live in
// api_serializers.jsx and are imported here for use on responses.
//
// Public function signatures intentionally mirror the original API
// object so importers (admin.jsx, viewer.jsx) keep working unchanged
// when they go through the api.jsx re-export shim.
//
// Auth: mutations send an `X-Tournament-Password` header. Read endpoints
// (/api/viewer/*) are unauthenticated.
//
// Error contract: every method that throws does so with `new Error(msg)`
// where `msg` is the server-reported `error` field if present, else a
// per-method default string. Callers decide whether to .catch and toast.
//
// Empty-body methods (overridePoolRank, overrideBracketWinner,
// resetOverrides, updateMatchTime, moveMatchCourt, updateSchedule,
// deleteCompetition) deliberately return `true`
// rather than `res.json()` — calling res.json() on a 200/204 with no
// body throws SyntaxError per the Fetch spec, which used to surface as
// "alert: Unexpected end of JSON input" right after a successful save.
// See __tests__/api.test.jsx for the regression coverage.

import { normalizeCompetitionDetail, normalizePlayer, toBackendMatchResult, buildPlayerMetadata } from './api_serializers.jsx';

// adminHdr returns the X-Admin-Password header object for the elevated
// (destructive-ops) password (spec 004 / mp-e21), or an empty object when no
// admin password is supplied. Destructive methods spread it into their
// headers so a request can carry BOTH X-Tournament-Password (main) and
// X-Admin-Password (elevated). When the gate is inactive on the server
// (file mode, no admin pw set) callers pass "" and the header is omitted.
function adminHdr(adminPassword) {
    return adminPassword ? { 'X-Admin-Password': adminPassword } : {};
}

const API = {
    async fetchTournament() {
        const res = await fetch('/api/viewer/tournament');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch tournament (Status ${res.status})`);
        }
        return res.json();
    },
    // Public endpoint — returns {mode: "file"|"locked", resetEnabled: bool}.
    // Mounted on App() boot to decide whether to render the "Forgot
    // password?" link in AuthModal and whether the /reset SPA route
    // should show a form or an "operator-disabled" message. Always
    // resolves (never rejects): non-2xx responses, network failures, and
    // JSON parse errors all fall back to the file-mode default so a
    // fresh-deploy SPA pointed at an older server without this endpoint
    // still works, and any transport error doesn't break sign-in.
    async fetchAuthConfig() {
        try {
            const res = await fetch('/api/auth-config');
            if (!res.ok) {
                return { mode: 'file', resetEnabled: true };
            }
            return await res.json();
        } catch {
            return { mode: 'file', resetEnabled: true };
        }
    },
    // Reset the tournament password. Unauthenticated by design — the
    // server enforces "is this endpoint enabled" via the verifier's
    // ResetEnabled() (locked mode returns 404). Throws on non-2xx so
    // the caller can surface the server's error message (including the
    // 404 "reset disabled" case if the SPA's cached authConfig was
    // stale).
    //
    // `originatorId` is a per-tab client ID echoed back on the SSE
    // password_reset event payload so the originating tab can ignore
    // its own broadcast and avoid clobbering the just-written
    // localStorage credential. Optional — when absent the server
    // broadcasts an empty originator and ALL tabs log out.
    async resetPassword(newPassword, originatorId) {
        const body = { password: newPassword };
        if (originatorId) body.originatorId = originatorId;
        const res = await fetch('/api/tournament/reset', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to reset password (Status ${res.status})`);
            e.status = res.status;
            throw e;
        }
        // Backend returns 204 No Content — calling .json() on an empty
        // body throws SyntaxError per the Fetch spec (same pattern as
        // overridePoolRank etc. above).
        return true;
    },
    // Set or rotate the elevated (destructive-ops) admin password (spec 004
    // / mp-e21). File mode only — locked mode 404s (credential is the
    // TOURNAMENT_ADMIN_PASSWORD_HASH env var). `password` is the MAIN
    // tournament password (gates the endpoint via AuthMiddleware).
    // `currentAdminPassword` is required only when an admin password is
    // already set (rotation); on first-time set (TOFU) the server ignores it.
    // The endpoint never echoes any password back.
    async setAdminPassword(newPassword, currentAdminPassword, password) {
        const body = { newPassword };
        if (currentAdminPassword) body.currentPassword = currentAdminPassword;
        const res = await fetch('/api/auth/admin-password', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to set admin password (Status ${res.status})`);
            e.status = res.status;
            throw e;
        }
        return res.json();
    },
    async fetchCompetitions() {
        const res = await fetch('/api/viewer/competitions');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch competitions (Status ${res.status})`);
        }
        const comps = await res.json();
        if (!Array.isArray(comps)) return comps;
        return comps.map(item => {
            const c = item.config || item;
            const norm = {
                ...c,
                poolMatches: item.poolMatches,
                bracket: item.bracket,
                players: (c.players || []).map(normalizePlayer),
            };
            // Run normalization on the matches as well
            return normalizeCompetitionDetail(norm);
        });
    },
    async fetchCompetitionDetails(id) {
        const res = await fetch(`/api/viewer/competitions/${id}`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch competition details (Status ${res.status})`);
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    // Bootstrap a fresh tournament. In file mode the call is
    // unauthenticated (the AuthMiddleware's uninitialized-bootstrap
    // branch lets it through). In locked mode the server requires the
    // env-var password to authorize even the initial POST — pass
    // `authPassword` (typed by the operator into the locked-mode
    // CreateTournament form) so we can attach it as
    // X-Tournament-Password. The handler discards the body's
    // `password` field in locked mode but the header is what gets
    // verified.
    async createTournament(config, authPassword) {
        const headers = { 'Content-Type': 'application/json' };
        if (authPassword) {
            headers['X-Tournament-Password'] = authPassword;
        }
        const res = await fetch('/api/tournament', {
            method: 'POST',
            headers,
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create tournament");
        }
        return res.json();
    },
    async updateTournament(config, password) {
        const res = await fetch('/api/tournament', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update tournament");
        }
        return res.json();
    },
    async updateCompetition(id, config, password, adminPassword) {
        // Go's domain.Player uses json:"metadata" (array); the JS layer uses the
        // friendlier danGrade string. Convert before encoding so the backend can
        // unmarshal the dan grade into Player.Metadata.
        let body = config;
        if (config.players) {
            body = {
                ...config,
                players: config.players.map((player) => {
                    // Only transform players that have been through normalizePlayer
                    // (which always adds an explicit danGrade key). A Go-sourced player
                    // object without danGrade must be passed through unchanged — if we
                    // destructure it, rest would be empty and metadata[0] would be lost.
                    if (!('danGrade' in player)) return player;
                    const { danGrade, metadata: _m, ...p } = player;
                    const metadata = buildPlayerMetadata(danGrade, player.metadata);
                    return metadata === undefined ? p : { ...p, metadata };
                }),
            };
        }
        const res = await fetch(`/api/competitions/${id}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password,
                // Roster-bearing updates (non-null players) hit the server's
                // elevated-password gate (spec 004); the caller threads the
                // admin password for those. Settings-only saves omit it.
                ...adminHdr(adminPassword)
            },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update competition");
        }
        return res.json();
    },
    async createCompetition(config, password) {
        const res = await fetch('/api/competitions', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create competition");
        }
        return res.json();
    },
    // exportPDFs POSTs to /api/print/:type (admin-gated) and returns the
    // response as a Blob (a ZIP of the produced PDFs). `type` is one of
    // registration|names|tags|pools-trees|full-bracket|all. Throws with the
    // server's error message on non-2xx (e.g. 503 when LibreOffice is absent,
    // 422 when no pages were produced). Generation runs LibreOffice and can
    // take 30–60s, so callers should show a busy state.
    async exportPDFs(type, password) {
        const res = await fetch(`/api/print/${type}`, {
            method: 'POST',
            headers: password ? { 'X-Tournament-Password': password } : {}
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to generate PDFs");
        }
        return res.blob();
    },
    async startCompetition(id, password) {
        const res = await fetch(`/api/competitions/${id}/start`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to start competition");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    async generateDraw(id, password) {
        const res = await fetch(`/api/competitions/${id}/generate-draw`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to generate draw");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    async discardDraw(id, password, adminPassword) {
        const res = await fetch(`/api/competitions/${id}/draw`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to discard draw");
        }
    },
    // Subscribe to the server's SSE event stream. `callback` is fired
    // for each parsed event. `onStatus` (optional) is fired with
    // 'open' when the EventSource transitions to open and 'error' when
    // it transitions away — used by the display surfaces (FR-011
    // scenario 4 / T063) to render a reconnect indicator when the
    // browser is between connections. Existing call sites that omit
    // the second arg are unaffected.
    subscribeToEvents(callback, onStatus) {
        let source = null;
        let retryTimer = null;
        let cancelled = false;

        const status = (s) => {
            if (typeof onStatus === 'function') {
                try { onStatus(s); } catch (err) { console.error('SSE status callback failed:', err); }
            }
        };

        const connect = () => {
            if (cancelled) return;
            source = new EventSource('/api/events');
            source.onopen = () => status('open');
            source.onmessage = (event) => {
                try {
                    callback(JSON.parse(event.data));
                } catch (err) {
                    console.error("Error parsing SSE event:", err);
                }
            };
            source.onerror = () => {
                if (source) source.close();
                source = null;
                status('error');
                if (!cancelled) retryTimer = setTimeout(connect, 5000);
            };
        };

        connect();

        return () => {
            cancelled = true;
            clearTimeout(retryTimer);
            if (source) source.close();
        };
    },
    async recordScore(compID, matchID, result, password, match) {
        const payload = toBackendMatchResult(result, match || result);
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/score`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to record score");
        }
        return res.json();
    },
    // T093–T095: kiken / fusenpai / fusensho / daihyosen — server auto-fills
    // scoreline and Winner from {decision, decisionBy, encho}. Body shape is
    // defined by mobileapp.DecisionRequest in handlers_decision.go; decisionBy
    // MUST be "shiro" or "aka", decisionReason is optional and ≤200 chars.
    // Response is the updated state.MatchResult.
    async recordDecision(compID, matchID, body, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/decision`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to record decision");
        }
        return res.json();
    },
    // T098: competitor eligibility statuses produced by kiken/fusenpai
    // decisions. Endpoint is read-only and unauthenticated — same contract as
    // the other /api/competitions/:id/* GETs surfaced to the viewer.
    async fetchCompetitorStatuses(compID) {
        const res = await fetch(`/api/competitions/${compID}/competitor-status`);
        if (!res.ok) throw new Error("Failed to load competitor statuses");
        const data = await res.json();
        return data.statuses || [];
    },
    async reinstateCompetitor(compID, playerID, password) {
        const res = await fetch(`/api/competitions/${compID}/competitors/${playerID}/reinstate`, {
            method: 'POST',
            headers: {
                'X-Tournament-Password': password
            }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to reinstate competitor");
        }
        return res.json();
    },
    async overridePoolRank(compID, poolID, playerName, rank, password) {
        const res = await fetch(`/api/competitions/${compID}/pools/${poolID}/override-rank`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ playerName, rank })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to override rank");
        }
        // Backend returns 200 with empty body. Calling .json() on an
        // empty body throws SyntaxError per the Fetch spec, which would
        // surface to the user as an alert("Failed: Unexpected end of
        // JSON input") right after a successful save.
        return true;
    },
    async overrideBracketWinner(compID, matchID, winnerName, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/override-winner`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ winnerName })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to override winner");
        }
        // Backend returns 200 with empty body — see overridePoolRank.
        return true;
    },
    async resetOverrides(compID, password, adminPassword) {
        const res = await fetch(`/api/competitions/${compID}/overrides`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to reset overrides");
        }
        // Backend returns 204 No Content — .json() would reject.
        return true;
    },
    async updateMatchTime(compID, matchID, scheduledAt, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/time`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ scheduledAt })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update match time");
        }
        return true;
    },
    async estimateSchedule(args, password, signal) {
        const params = new URLSearchParams();
        Object.entries(args).forEach(([k, v]) => {
            if (v !== undefined && v !== null && v !== "" && !Number.isNaN(v)) {
                params.append(k, v);
            }
        });
        const res = await fetch(`/api/schedule/estimate?${params.toString()}`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to estimate schedule");
        }
        return res.json();
    },
    async estimateCompetitionSchedule(compID, password, signal) {
        const res = await fetch(`/api/competitions/${compID}/schedule/estimate`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to estimate schedule");
        }
        return res.json();
    },
    async moveMatchCourt(compID, matchID, court, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/court`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ court })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to move match court");
        }
        return true;
    },
    async updateSchedule(compID, entries, password) {
        const res = await fetch(`/api/competitions/${compID}/schedule`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(entries)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update schedule");
        }
        return true;
    },
    async importCompetitions(formData, password, adminPassword) {
        const res = await fetch('/api/tournament/import', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) },
            body: formData
        });
        if (!res.ok) {
            const body = await res.json().catch(() => ({}));
            throw new Error(body.error || 'Import failed');
        }
        return res.json();
    },
    async deleteCompetition(id, password, adminPassword) {
        const res = await fetch(`/api/competitions/${id}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to delete competition");
        }
        return true;
    },
    // Returns the updated competition JSON (including the new `status`) so
    // callers can apply it to local state immediately instead of waiting for
    // the SSE refresh / tournament refetch to land.
    async invalidateCompetition(id, password, adminPassword) {
        const res = await fetch(`/api/competitions/${id}/invalidate`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to invalidate competition");
        }
        return res.json();
    },
    // Replace the full fighting-spirit awards list for a competition.
    // awards: array of { title, recipientName, recipientDojo? }.
    // Requires the elevated (admin) password.
    async updateCompetitionAwards(id, awards, password, adminPassword) {
        const res = await fetch(`/api/competitions/${id}/awards`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password,
                ...adminHdr(adminPassword)
            },
            body: JSON.stringify({ fightingSpiritAwards: awards })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update competition awards");
        }
        return res.json();
    },
    // T129/T130: per-round team lineups (FR-040). GET returns the persisted
    // TeamLineup for (compId, teamId, round) — 404 when no lineup has been
    // submitted yet, which the form treats as "blank, editable". PUT replaces
    // the lineup; the server rejects with 409 (ErrLineupLocked) once the
    // round's first match has gone live. DELETE clears the lineup so an
    // operator can revise pre-lock.
    async fetchTeamLineup(compID, teamId, round) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/lineups/${round}`);
        if (res.status === 404) return null;
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load lineup");
        }
        return res.json();
    },
    async putTeamLineup(compID, teamId, round, positions, password) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/lineups/${round}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ teamId, competitionId: compID, round, positions })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to save lineup");
        }
        return res.json();
    },
    async deleteTeamLineup(compID, teamId, round, password) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/lineups/${round}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok && res.status !== 404) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to delete lineup");
        }
        return true;
    },
    // mp-825 / mp-bkg: per-match lineup endpoints. Match ID takes the
    // place of the round key — successive encounters between the same
    // two teams each carry an independent, lockable lineup entry.
    // 404 → null (no lineup saved yet; form treats as blank/editable).
    // 409 ErrLineupLocked on PUT → the match is already live; surface
    // as a clear error (same pattern as round-scoped PUT).
    async fetchMatchLineup(compID, teamId, matchId) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`);
        if (res.status === 404) return null;
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load match lineup");
        }
        return res.json();
    },
    async putMatchLineup(compID, teamId, matchId, positions, password) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ teamId, competitionId: compID, matchId, positions })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to save match lineup");
        }
        return res.json();
    },
    async deleteMatchLineup(compID, teamId, matchId, password) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok && res.status !== 404) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to delete match lineup");
        }
        return true;
    },
    // T141: daihyosen (representative bout) appended after a knockout team
    // match ties on IV and PW. Server validates the tie + eligibility and
    // returns the updated MatchResult with a new SubMatchResult whose
    // decision="daihyosen" and Position=-1. The error codes (not_tied,
    // pool_match, insufficient_eligibility) are surfaced verbatim by the
    // caller — see TeamScoreEditorModal for the user-visible mapping.
    async recordDaihyosen(compID, matchID, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/daihyosen`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to add daihyosen");
        }
        return res.json();
    },
    // T190-T193 (US13 — Swiss format). Generate the next Swiss round.
    // Backend pre-conditions: format=swiss; all matches in the current
    // round must be completed; swissCurrentRound must be < swissRounds.
    // Returns { round, matches, swissCurrentRound } on 201, throws on
    // 4xx/5xx. 409 with code="round_incomplete" surfaces a friendly
    // "current round still has incomplete matches" message in the UI;
    // the caller checks `e.code === "round_incomplete"` to branch.
    async swissGenerateRound(compID, password) {
        const res = await fetch(`/api/competitions/${compID}/swiss/generate-round`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            // Preserve the structured 409 payload (code + round) on the
            // thrown Error so the caller can show a precise message. The
            // generic .message stays as the server-reported error string.
            const e = new Error(err.error || "Failed to generate Swiss round");
            if (err.code) e.code = err.code;
            if (err.round !== undefined) e.round = err.round;
            throw e;
        }
        return res.json();
    },
    // T190-T193 (US13). Public endpoint — returns cumulative Swiss
    // standings ranked by wins > points > head-to-head. Returns []
    // when the competition hasn't started yet. Each entry is a
    // state.PlayerStanding (same shape used by pool standings).
    async swissStandings(compID) {
        const res = await fetch(`/api/competitions/${compID}/swiss/standings`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load Swiss standings");
        }
        return res.json();
    },
    async toggleCheckIn(compID, pid, checkedIn, password) {
        const method = checkedIn ? 'PUT' : 'DELETE';
        const res = await fetch(`/api/competitions/${compID}/participants/${pid}/checkin`, {
            method,
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to ${checkedIn ? 'check in' : 'undo check-in'} participant`);
        }
        return res.json();
    },
    async addParticipant(compID, payload, password, adminPassword) {
        const res = await fetch(`/api/competitions/${compID}/participants`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password, ...adminHdr(adminPassword) },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to add participant');
        }
        return res.json();
    },
    async replaceParticipant(compID, pid, payload, password, adminPassword) {
        const res = await fetch(`/api/competitions/${compID}/participants/${pid}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password, ...adminHdr(adminPassword) },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to replace participant');
        }
        return res.json();
    },
    async sendAnnouncement(message, durationMinutes, password) {
        const res = await fetch('/api/tournament/announce', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ message, durationMinutes })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to send announcement (Status ${res.status})`);
        }
        return res.json();
    },
    async fetchAnnouncements() {
        const res = await fetch('/api/tournament/announcements');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch announcements (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteAnnouncement(id, password) {
        const res = await fetch(`/api/announcements/${encodeURIComponent(id)}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete announcement (Status ${res.status})`);
        }
    },
    async clearAnnouncements(password) {
        const res = await fetch('/api/announcements', {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to clear announcements (Status ${res.status})`);
        }
    },
    // mp-c38: sponsor logo upload (multipart) and delete.
    async uploadSponsor({ file, name, link, password }) {
        const fd = new FormData();
        fd.append('name', name);
        if (link) fd.append('link', link);
        fd.append('file', file);
        const res = await fetch('/api/sponsors', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: fd,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to upload sponsor (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteSponsor(index, password) {
        const res = await fetch(`/api/sponsors/${index}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete sponsor (Status ${res.status})`);
        }
    },
    // mp-scf: tournament branding — logo upload/delete.
    async uploadBrandingLogo({ file, password }) {
        const fd = new FormData();
        fd.append('file', file);
        const res = await fetch('/api/branding/logo', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: fd,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to upload logo (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteBrandingLogo(password) {
        const res = await fetch('/api/branding/logo', {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete logo (Status ${res.status})`);
        }
    }
};

export { API };

if (typeof window !== 'undefined') {
    window.API = API;
}
