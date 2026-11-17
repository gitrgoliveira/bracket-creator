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
// Empty-body methods (overridePoolRanks,
// updateMatchTime, moveMatchCourt,
// deleteCompetition) deliberately return `true`
// rather than `res.json()`: calling res.json() on a 200/204 with no
// body throws SyntaxError per the Fetch spec, which used to surface as
// "alert: Unexpected end of JSON input" right after a successful save.
// See __tests__/api.test.jsx for the regression coverage.
//
// Wifi-hardening additions (mp-gpra):
//   F1: fetchWithTimeout: per-request AbortController, 12 s default.
//   F2: SSE silence watchdog: reconnects if no message for 35 s (>2× heartbeat).
//   F3: SSE reconnect exponential backoff + jitter (1–30 s, reset on open).
//   F4: Write queue persisted to localStorage (no age limit, try/catch safe).
//   F5: Terminal writes (decision, completed score, lineup) durable via queue.

import { normalizeCompetitionDetail, toBackendMatchResult, buildPlayerMetadata } from './api_serializers.jsx';
import {
    writeDidNotLand, writeWasSuperseded, writeWasRefusedForClock, SUPERSEDED_REASON, SUPERSEDED_ADVICE,
    CLOCK_SKEW_REASON_TEXT, CLOCK_SKEW_ADVICE, isFinishedResultWrite, isOffline, NOT_SENT_NO_CONNECTION,
} from './write_result.jsx';
import { TOURNAMENT_BASE, awardsBase, tournamentRefusal } from './document_versions.jsx';
// bc-evts S2: the build this page runs against the server's, and the reload rule.
import { setOutboxIdleProbe, setBeforeReload } from './app_version.jsx';
import { LANDED_TAB_BEAT_MS } from './landed_taps.jsx';
import { TOLD_TAPS_PREFIX } from './told_taps.jsx';
import { MINTED_MEMBERS_KEY } from './minted_members.jsx';
import {
    fetchWithTimeout, reopenBody, normalizeViewerCompItem, START_CHECK_TIMEOUT_MS, _serverKeepsRetrying,
    START_CHECK_NOT_FOUND_MESSAGE, START_CHECK_WRITE_KINDS, _reasonHumanError,
    parseSkippedCompetitionsHeader, _fetchJson, _readLineup, _directFetch, _backupRequest, _backupFilename,
    _restoreForm,
} from './api_client_requests.jsx';
import {
    _clockOffsetLearned, _learnServerClockOffset, _relearnClockThrottled, _serverNowMs, _stampAfterSeen,
    _seenOfScore, _stampPayload, _nextRev, _revSession, _afterOwnStamp, _noteOwnStamp,
} from './api_client_clock.jsx';
import {
    _revKey, QUEUE_STORAGE_KEY, LEGACY_QUEUE_STORAGE_KEY, subscribeQueueAlert, _notifyQueueAlert,
    _countTerminalQueued, _persistQueue, _writeQueue, _queueSet, _markSettled, _dequeue, _wipeQueue,
    _baseOf, _poolRanksBase, _byEntryOrder, _entriesFor, _entryHooks, _settleKey, _parkStoredEntries,
} from './api_client_queue.jsx';
import {
    START_BROADCAST_DROPS, _publishMatchPatch, _sideIdFields, _heldFlagCounts,
    _attachSendBackDisplayPutBack, _ownStatusMove, _statusEntriesByBase, _subscribeOwnStatusMoves,
    _clearOwnStatusAnswers, _forgetOwnStatusMove, _forgetOwnStatusMovesNotIn, subscribeLocalMatchPatch,
    _emitLocalMatchPatch, _pendingMatchFields, _overlayPendingFields, _CHECKIN_KINDS,
    subscribeLocalParticipantPatch, _checkInBase, _checkInsOf, _emitLocalCheckIns, _checkInLabel,
    _ANNOUNCE_CLEAR_BASE, _announceBase, _courtCallLabel, currentDocVersion, rebaseQueuedVersion,
    overlayPendingTournament,
} from './api_client_overlays.jsx';
import {
    _eventSession, _latestDerived, _derivedListeners, _pendingTapListeners, _tapOutcomeListeners,
    _pruneToldOnBoard, _nextTapSeq, _claimToldHere, _forgetTurnedAwayAndTold, _forgetDerivedCopies,
    _onToldTapsStored, _queueBoardTold, _listenOn, _pendingTapsFor, _publishPendingTaps, _landed, _told,
    _landedBeat, _forgetLandedTaps,
} from './api_client_taps.jsx';
import {
    subscribeTerminalWriteFailed, _keepsFailing, _refusedByServer, _heldWriteItem, _noteShownMatch,
    _forgetShownMatches, _forgetAllShownMatches, _noteMatchLabel, _discardIfFailing, _recomputeSyncStatus,
    _unsentCounts, subscribeUnsentWrites, subscribeSyncStatus,
} from './api_client_status.jsx';
import {
    _inFlight, _offlineFlag, _afterDiscard, _resetFlushBackoff, _setOffline, _bumpQueueGen, _restampFor,
    _flushQueue, _outboxIdle, _blockerOf, _queueIndex, _newWalk, _rerunFlushSoon,
} from './api_client_flush.jsx';
import {
    _outboxWrite, _lastEntryKey, _isFollowed, _migratedLegacyLineup, _queuedLineupSave, _lineupKey,
    _storedEntryProblem, _commitEnqueue, _lastEnqueuePersisted, enqueueRunningWrite,
    _withoutDownstreamConfirmation, _onStoredQueueChanged, _takeOverLegacyQueue, _daihyosenRequest,
} from './api_client_enqueue.jsx';
import {
    _withListedMembersForgotten, _minted, _memberWrite, _announceKeptMember, _resumeMintedReads,
    _pauseMintedReads, _wipeMintedRecords, _learnDropped, _newClientId, _shownMember,
} from './api_client_members.jsx';
import { _subscribeToEvents, _reconnectEvents } from './api_client_events.jsx';
export { subscribeLocalMatchPatch, subscribeLocalParticipantPatch } from './api_client_overlays.jsx';

if (typeof setInterval === 'function') {
    const beat = setInterval(_landedBeat, LANDED_TAB_BEAT_MS);
    if (beat && typeof beat.unref === 'function') beat.unref();
}
if (typeof window !== 'undefined' && typeof window.addEventListener === 'function') {
    window.addEventListener('pagehide', () => _landed.leave());
}
// A hidden page's timers slow or stop: it beats as it is hidden and shown, so
// the other tabs read it alive for longer while hidden (stage 6 review 9, F6),
// and a page shown again tells at once what it may tell now, a hidden tab's
// taps included (stage 6 review 10, M1).
if (typeof document !== 'undefined' && typeof document.addEventListener === 'function') {
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'hidden') _landed.beat({ hidden: true });
        else {
            _landed.beat({ hidden: false });
            _landedBeat();
        }
    });
}

// Remove-then-add for the same reason as the online handler below.
if (typeof window !== 'undefined') {
    if (window.__bcStoredQueueHandler) window.removeEventListener('storage', window.__bcStoredQueueHandler);
    const onStorage = (e) => {
        if (e.key === QUEUE_STORAGE_KEY) _onStoredQueueChanged(e.oldValue, e.newValue);
        if (typeof e.key === 'string' && e.key.startsWith(TOLD_TAPS_PREFIX)) _onToldTapsStored(e.key, e.oldValue, e.newValue);
        // Another tab learned something of a minted member: a member that
        // never was comes off this tab's writes, and a write held back for one
        // may now go (minted_members.jsx).
        if (e.key === MINTED_MEMBERS_KEY) {
            const { dropped, kept, unblocks } = _minted.onStored(e.oldValue, e.newValue);
            for (const m of kept) _announceKeptMember(m);
            if (dropped.length > 0) _learnDropped(dropped);
            else if (unblocks) _rerunFlushSoon();
        }
        // An older tab wrote what it queues under the old key: take it over (F4).
        if (e.key === LEGACY_QUEUE_STORAGE_KEY && e.newValue && _takeOverLegacyQueue(e.newValue)) {
            _recomputeSyncStatus();
            _flushQueue();
        }
    };
    window.__bcStoredQueueHandler = onStorage;
    window.addEventListener('storage', onStorage);
}

// Flush the queue whenever the browser comes back online.
// Remove-then-add so re-evaluating this module (tests with resetModules, hot
// reload, multi-bundle inclusion) replaces the listener instead of stacking up
// orphaned ones bound to stale module state. The handler reference is parked on
// `window` (which survives module re-eval) so the prior one can be detached.
if (typeof window !== 'undefined') {
    if (window.__bcOnlineFlushHandler) {
        window.removeEventListener('online', window.__bcOnlineFlushHandler);
    }
    const onlineHandler = () => {
        // bc-cse: reconnect is also the moment an OS is most likely to have
        // NTP-jumped this device's clock (a tablet that slept through a wifi gap
        // resyncs on the way back). The SSE onopen relearn only helps if SSE
        // actually opens, which on a court that has just come back is neither
        // immediate nor guaranteed - so relearn here too, and do it BEFORE the
        // flush and regardless of whether anything is queued: the writes at risk
        // are the live ones the operator makes in the next few seconds.
        // Throttled, so a flapping connection costs one GET per 5s at worst.
        _relearnClockThrottled();
        if (_writeQueue.size > 0) {
            _resetFlushBackoff();
            _flushQueue();
        }
    };
    window.__bcOnlineFlushHandler = onlineHandler;
    window.addEventListener('online', onlineHandler);
}

// ---------------------------------------------------------------------------
// F4: Rehydrate write queue from localStorage on module load
// ---------------------------------------------------------------------------
// Parse any entries persisted by a previous page session, whatever their age
// (bc-mrgc phase 3: the server orders an old write by its stamp and keeps what
// a newer change outranks in the match's history, so nothing is dropped for
// being old). A queue persisted by an earlier build (one entry per match, keyed
// by the match itself) loads as it is: its key is its own base (_baseOf).
// Valid entries are added to _writeQueue and an immediate flush is triggered
// so they are delivered as soon as the page has network. All localStorage access is wrapped in try/catch for
// private-browsing / quota safety.
// ---------------------------------------------------------------------------

// mp-y3nk: learn the server-clock offset at load so the first writes are already
// stamped in the server's frame (refreshed again on each SSE (re)connect).
//
// bc-cse: retry until the FIRST learn succeeds. A single fire-and-forget attempt
// leaves the offset at 0 for the whole session when the load-time GET fails and
// no SSE connection ever opens - and that precondition ("SSE never opens") is
// exactly why there is no other trigger to lean on. Backoff 5s -> 10s -> 20s ->
// 40s, capped at 60s, indefinitely: one tiny GET per minute is the steady state,
// and it stops the moment anything (here or the SSE-connect relearn) succeeds.
//
// The `typeof fetch` guard wraps the WHOLE chain, not just the first attempt, so
// a host without fetch schedules no timers at all. Note it does NOT keep the
// chain out of the unit suite: jsdom + node give `fetch` here, so the poll runs
// in any test that imports this module (see the clock-poll note in
// __tests__/sync_queue.test.jsx, whose fetch counts are scoped because of it).
const CLOCK_LEARN_RETRY_BASE_MS = 5000;
const CLOCK_LEARN_RETRY_MAX_MS = 60000;
if (typeof fetch === 'function') {
    (function _learnClockUntilSuccess(delayMs) {
        _learnServerClockOffset().finally(() => {
            if (_clockOffsetLearned) return;
            setTimeout(() => {
                // Re-check on FIRE, not just on schedule: an SSE-connect learn or a
                // supersede relearn can land while this timer is pending.
                if (_clockOffsetLearned) return;
                _learnClockUntilSuccess(Math.min(delayMs * 2, CLOCK_LEARN_RETRY_MAX_MS));
            }, delayMs);
        });
    })(CLOCK_LEARN_RETRY_BASE_MS);
}

;(function _rehydrateQueue() {
    try {
        const raw = localStorage.getItem(QUEUE_STORAGE_KEY);
        if (!raw) return;
        const entries = JSON.parse(raw);
        if (!Array.isArray(entries)) return;
        let anyLoaded = false;
        // bc-qttl: every drop below is COUNTED and reported. These entries are
        // finished operator work that never reached the server, and the previous
        // code discarded them with a bare `continue` - no log, no signal, nothing
        // the operator could ever notice. Counted terminal separately from
        // running, because "3 results were lost" and "3 writes were lost" are
        // different claims and only the terminal ones are a finished result.
        // The one class left is a corrupt entry: nothing is dropped for its age
        // (bc-mrgc phase 3).
        let unreadable = 0, unreadableTerminal = 0, migrated = false;
        // A dropped entry is marked settled, so the persist below takes it out
        // of storage: the merge otherwise keeps what this tab did not write.
        const dropUnreadable = (key, d) => {
            unreadable++;
            if (isFinishedResultWrite(d)) unreadableTerminal++;
            if (key) _markSettled(key, d);
        };
        for (const item of entries) {
            // Defensive: a single malformed element (corrupt/tampered storage) must
            // not throw and abort the whole rehydrate: destructuring a non-array in
            // the for-of header would do exactly that, dropping ALL valid queued
            // writes. Validate the tuple shape first and skip bad items individually.
            if (!Array.isArray(item) || item.length < 2) { dropUnreadable(null, null); continue; }
            const [key, descriptor] = item;
            if (!key || !descriptor || typeof descriptor !== 'object') { dropUnreadable(key, null); continue; }
            if (_storedEntryProblem(descriptor)) { dropUnreadable(key, descriptor); continue; }
            // Only restore if not already superseded by a same-session write.
            if (!_writeQueue.has(key)) {
                // A write a previous page left behind has already waited: it
                // is held, and its landing is told (the 'sent' alert).
                descriptor.held = true;
                // A confirmation an older build stored is never replayed: it
                // answered a dialog on a page that is gone.
                if (descriptor.payload && descriptor.payload.forceDownstreamReopen) {
                    descriptor.payload = _withoutDownstreamConfirmation(descriptor.payload);
                }
                // A match lineup an older build queued under its own base goes
                // onto its match's (R9); the old key leaves storage below.
                const moved = _migratedLegacyLineup(key, descriptor);
                const at = moved || key;
                if (moved) { _markSettled(key, descriptor); migrated = true; }
                if (!_writeQueue.has(at)) {
                    _queueSet(at, descriptor);
                    _noteOwnStamp(at, descriptor);
                    anyLoaded = true;
                }
            }
        }
        // Persist the pruned queue whenever anything was dropped. This is what makes
        // the alert below fires ONCE rather than on every reload forever: rehydrate
        // never used to write back, so a dropped entry stayed in localStorage
        // untouched, and each refresh would re-read, re-drop and re-announce it.
        // With the queue now empty _persistQueue() stores it as "[]".
        if (unreadable > 0 || migrated) _persistQueue();
        if (unreadable > 0) {
            console.warn(`[sync] discarded ${unreadable} unreadable queued write(s) on rehydration`);
            _notifyQueueAlert({ kind: 'unreadable', count: unreadable, terminalCount: unreadableTerminal });
        }
        if (anyLoaded) {
            _recomputeSyncStatus();
            _flushQueue();
        }
    } catch (_e) { /* private-browsing / malformed JSON: silently skip */ }
}());

// After this build's own queue: what an older build left under the old key
// (F4). A key both hold keeps this build's entry.
;(function _takeOverLegacyQueueOnLoad() {
    try {
        if (_takeOverLegacyQueue(localStorage.getItem(LEGACY_QUEUE_STORAGE_KEY))) {
            _recomputeSyncStatus();
            _flushQueue();
        }
    } catch (_e) { /* private-browsing: nothing to take over */ }
}());

const API = {
    async fetchTournament() {
        const res = await fetch('/api/viewer/tournament');
        // "No tournament configured yet" is a normal bootstrap state, not an
        // error: return null so callers open the create-tournament gate quietly
        // instead of logging a console error. Newer servers signal it with a 200
        // and a null JSON body (handled by the res.json() return below); older
        // servers used a 404 (handled here). Genuine failures (5xx, network)
        // still throw below.
        if (res.status === 404) {
            return null;
        }
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch tournament (Status ${res.status})`);
        }
        // A settings save this device still has queued shows (G4b).
        return overlayPendingTournament(await res.json());
    },
    // Public endpoint: returns {mode: "file"|"locked", resetEnabled: bool}.
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
    async fetchVersion() {
        try {
            const res = await fetch('/api/version');
            if (!res.ok) {
                return null;
            }
            return await res.json();
        } catch {
            return null;
        }
    },
    // Reset the admin password. Unauthenticated by design: the
    // server enforces "is this endpoint enabled" via the verifier's
    // ResetEnabled() (locked mode returns 404). In file mode it answers
    // only a request made on the server machine (loopback peer and
    // loopback Host), else 403 reset_local_only. Throws on non-2xx so
    // the caller can surface the server's error message (including the
    // 404 "reset disabled" case if the SPA's cached authConfig was
    // stale).
    //
    // `originatorId` is a per-tab client ID echoed back on the SSE
    // password_reset event payload so the originating tab can ignore
    // its own broadcast and avoid clobbering the just-written
    // localStorage credential. Optional: when absent the server
    // broadcasts an empty originator and ALL tabs log out.
    async resetPassword(newPassword, originatorId) {
        const body = { password: newPassword };
        if (originatorId) body.originatorId = originatorId;
        const res = await _directFetch('/api/tournament/reset', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to reset password (Status ${res.status})`);
            e.status = res.status;
            // The refusal code (reset_local_only) lets the form word the
            // case; absent on a bare error.
            if (err.code) e.code = err.code;
            throw e;
        }
        // A reset: no learned match label is trusted (K8).
        _forgetAllShownMatches();
        // Backend returns 204 No Content: calling .json() on an empty
        // body throws SyntaxError per the Fetch spec (same pattern as
        // overridePoolRanks etc. above).
        return true;
    },
    // `bounded` gives up after FETCH_TIMEOUT_MS, the body read included
    // (_fetchJson), instead of waiting on a connection that never answers.
    // The organiser's refresh after a write asks for it (admin.jsx
    // refreshCompsBestEffort): the score editor that made the write waits on
    // that refresh, and it is best-effort. Other callers wait as long as the
    // aggregate takes, which can be long for a large tournament on a slow link.
    async fetchCompetitions({ bounded = false } = {}) {
        if (bounded) {
            const { res, body } = await _fetchJson('/api/viewer/competitions', {});
            if (!res.ok) throw new Error(body.error || `Failed to fetch competitions (Status ${res.status})`);
            // _fetchJson reads an unreadable body as {}: never hand that on as
            // the competitions.
            if (!Array.isArray(body)) throw new Error('Failed to fetch competitions (unreadable answer)');
            return body.map((c) => _overlayPendingFields(normalizeViewerCompItem(c)));
        }
        const res = await fetch('/api/viewer/competitions');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch competitions (Status ${res.status})`);
        }
        const comps = await res.json();
        if (!Array.isArray(comps)) return comps;
        // A court or time this device still has queued shows (R4).
        return comps.map((c) => _overlayPendingFields(normalizeViewerCompItem(c)));
    },

    // fetchCourtMatches returns the court-scoped match feed: only the
    // competitions that have a real match physically on `court` right now, each
    // with its full {config, poolMatches, bracket} payload (same per-comp shape
    // as fetchCompetitions). The operator console sources its cross-competition
    // court view from this instead of the whole-tournament aggregate. Returns a
    // normalized competitions array so callers can run tournamentMatches() over
    // { competitions } exactly as they do for the aggregate.
    async fetchCourtMatches(court) {
        const res = await fetch(`/api/viewer/court/${encodeURIComponent(court)}/matches`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch court matches (Status ${res.status})`);
        }
        const data = await res.json();
        const comps = Array.isArray(data.competitions) ? data.competitions : [];
        return comps.map((c) => _overlayPendingFields(normalizeViewerCompItem(c)));
    },
    async fetchCompetitionDetails(id) {
        const res = await fetch(`/api/viewer/competitions/${id}`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to fetch competition details (Status ${res.status})`);
            // The server answered: a caller can tell that from a request
            // that never reached it (the data-issue banner's Reload).
            e.status = res.status;
            throw e;
        }
        const data = await res.json();
        return _overlayPendingFields(normalizeCompetitionDetail(data), id);
    },
    // Bootstrap a fresh tournament. In file mode the call is
    // unauthenticated (the AuthMiddleware's uninitialized-bootstrap
    // branch lets it through). In locked mode the server requires the
    // env-var password to authorize even the initial POST: pass
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
        const res = await _directFetch('/api/tournament', {
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
    // bc-obox G4b: the tournament settings, an outbox entry under the base
    // `tournament`, answered at once like every outbox door ({ pending,
    // settled }); `settled` resolves with the saved settings (their
    // tournamentVersion with them) or rejects with the refusal (409
    // tournament_changed carries `code` and `tournament`, the copy now). The
    // body never carries a password: an empty one keeps the stored one, so a
    // descriptor at rest holds none. A save that CHANGES the password (a body
    // password other than the session's, `password`) is the auth door and
    // stays direct (updateTournamentPassword), answering as it always did.
    async updateTournament(config, password, opts = {}) {
        const body = config || {};
        if (typeof body.password === 'string' && body.password !== '' && body.password !== password) {
            return API.updateTournamentPassword(body, password);
        }
        const { competitions: _comps, ...settings } = body;
        return _outboxWrite({
            kind: 'tournament', compID: '', matchID: '', method: 'PUT', url: '/api/tournament',
            payload: { ...settings, password: '', tournamentVersion: currentDocVersion(TOURNAMENT_BASE, settings.tournamentVersion) },
            password, onRefusal: opts && opts.onRefusal, base: TOURNAMENT_BASE, label: 'Tournament settings',
        });
    },
    // The password change: direct (_directFetch), with the version it was made
    // from. A save of the settings this device still has queued is made stale
    // by it, so the caller waits for those first (API.queuedWritesOf).
    async updateTournamentPassword(config, password) {
        const { competitions: _comps, ...settings } = config || {};
        const sent = currentDocVersion(TOURNAMENT_BASE, settings.tournamentVersion);
        const res = await _directFetch('/api/tournament', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ ...settings, tournamentVersion: sent })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw tournamentRefusal(err, res.status);
        }
        const saved = await res.json();
        rebaseQueuedVersion(TOURNAMENT_BASE, 'tournamentVersion', sent, saved && saved.tournamentVersion);
        return saved;
    },
    async updateCompetition(id, config, password) {
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
                    // object without danGrade must be passed through unchanged: if we
                    // destructure it, rest would be empty and metadata[0] would be lost.
                    if (!('danGrade' in player)) return player;
                    const { danGrade, metadata: _m, ...p } = player;
                    const metadata = buildPlayerMetadata(danGrade, player.metadata);
                    return metadata === undefined ? p : { ...p, metadata };
                }),
            };
        }
        const res = await _directFetch(`/api/competitions/${id}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || "Failed to update competition");
            // A roster PUT built from a roster that is no longer the stored
            // one (another desk added, removed or seeded a competitor since):
            // nothing was saved, and the answer carries the roster as it
            // stands, normalized as a fetched detail's, for the page to lay
            // its change on and send again (stage 6 review 12, C1).
            if (res.status === 409 && err.code === 'roster_changed') {
                e.code = 'roster_changed';
                e.players = normalizeCompetitionDetail({ config: { players: err.players || [] } }).config.players;
                e.rosterVersion = err.rosterVersion || '';
            }
            if (res.status === 409 && err.code === 'config_changed') {
                e.code = 'config_changed';
                e.configVersion = err.configVersion || '';
                e.competition = err.competition || null;
            }
            if (res.status === 409 && ['undo_stale', 'retime_retry', 'retime_unavailable', 'retime_past_midnight', 'court_past_midnight'].includes(err.code)) {
                e.code = err.code;
            }
            throw e;
        }
        return res.json();
    },
    async createCompetition(config, password) {
        const res = await _directFetch('/api/competitions', {
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
    // exportPDFs POSTs to /api/print/:type (admin-gated) and returns
    // { blob, skipped }: `blob` is a ZIP of the produced PDFs, `skipped` is
    // the parsed X-Skipped-Competitions header as [{id, reason}, ...] (empty
    // array when the header is absent, i.e. nothing was skipped). `type` is
    // one of registration|names|tags|pools-trees|full-bracket|all.
    //
    // Throws with the server's error message on non-2xx (e.g. 503 when
    // LibreOffice is absent, 422 when every competition was skipped or no
    // pages were produced); the thrown Error also carries a `skipped` array
    // (from the JSON body's `skipped` field, e.g. the all-skipped 422 case),
    // empty when the body carries none. Generation runs LibreOffice and can
    // take 30–60s, so callers should show a busy state.
    async exportPDFs(type, password) {
        const res = await _directFetch(`/api/print/${type}`, {
            method: 'POST',
            headers: password ? { 'X-Tournament-Password': password } : {}
        }, NOT_SENT_NO_CONNECTION); // generating PDFs changes nothing
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || "Failed to generate PDFs");
            e.skipped = Array.isArray(err.skipped) ? err.skipped : [];
            throw e;
        }
        const skipped = parseSkippedCompetitionsHeader(res.headers.get('X-Skipped-Competitions'));
        const blob = await res.blob();
        return { blob, skipped };
    },
    // exportResults GETs the RESULTS-populated workbook (played scores,
    // standings, winners, and decision suffixes written as literal values)
    // from the admin-gated /api/competitions/:id/export-results endpoint. This
    // is distinct from AdminExport's "Download .xlsx", which posts to the public
    // /create path and yields a BLANK formula template. Returns an .xlsx Blob;
    // throws the server's error message on non-2xx.
    async exportResults(compID, password) {
        const res = await _directFetch(`/api/competitions/${compID}/export-results`, {
            method: 'GET',
            headers: password ? { 'X-Tournament-Password': password } : {}
        }, NOT_SENT_NO_CONNECTION); // a download changes nothing
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to export results");
        }
        return res.blob();
    },
    async startCompetition(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/start`, {
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
        const res = await _directFetch(`/api/competitions/${id}/generate-draw`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to generate draw");
        }
        _forgetShownMatches(id);
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    // fetchDrawWarnings: GET /competitions/:id/draw-warnings. The seeding rules
    // never refuse a draw; where a configuration cannot satisfy them all, the
    // deepest constraint gives way and the operator is told what was relaxed.
    // This is that advice, and it is admin-only (the public viewer surfaces
    // deliberately do not carry it).
    //
    // Advisory, so a failure is swallowed into an empty list: a competition
    // page must never break, or lose its draw, over a warning it could not
    // fetch.
    async fetchDrawWarnings(id, password) {
        try {
            const res = await fetch(`/api/competitions/${id}/draw-warnings`, {
                headers: password ? { 'X-Tournament-Password': password } : {}
            });
            if (!res.ok) return [];
            const data = await res.json();
            return data.warnings || [];
        } catch {
            return [];
        }
    },
    async discardDraw(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/draw`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to discard draw");
        }
        _forgetShownMatches(id);
    },
    // quarantineBracket: POST /competitions/:id/bracket/quarantine. The way out
    // of a bracket.json that will not parse, which otherwise blocks every score
    // write for that competition. Renames the broken file aside (it is NEVER
    // deleted: it is the only record of the knockout results it holds, and
    // because it does not parse nothing can count them). What happens next
    // depends on the format, and the response says which: `rebuilt` is true when
    // the knockout was rebuilt from pools.csv (mixed), false when there was no
    // knockout stage to rebuild and moving the file aside was the whole repair
    // (league, Swiss). 400 when the bracket reads fine, and 400 with a reason
    // for any competition that draws its bracket DIRECTLY, where the file is the
    // only record of the draw and rebuilding would invent different pairings
    // rather than restore these.
    async quarantineBracket(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/bracket/quarantine`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        const body = await res.json().catch(() => ({}));
        if (!res.ok) {
            throw new Error(body.error || "Failed to quarantine the bracket file");
        }
        return body;
    },
    // completeCompetition: POST /competitions/:id/complete. The only trigger
    // for a bracket-based competition (knockout, or mixed once its knockout is
    // running) to reach status "completed": MaybeAutoCompletePools only
    // auto-transitions the League format, so a finished bracket otherwise sits
    // in "pools"/"knockout" forever and the public viewer's Awards tab (gated
    // on status === "completed") never becomes reachable. 400 when the
    // competition isn't in "pools" or "knockout" status (already completed, or
    // not started yet), or a naginata 3rd-place match is still unscored; 404
    // when the competition doesn't exist.
    async completeCompetition(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/complete`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to complete competition");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    // Subscribe to the server's SSE event stream: one shared, ref-counted
    // EventSource (_subscribeToEvents).
    subscribeToEvents(callback, onStatus) {
        return _subscribeToEvents(callback, onStatus);
    },
    /**
     * Record a match score or status change.
     *
     * The patch passed as `result` may carry request-only `forceCourt: true`:
     * an organiser's confirmed start on a busy shiaijo; sent on the payload,
     * kept through the outbox, merged into a write that takes a queued one's
     * place, never broadcast to a display.
     *
     * @param {string} compID
     * @param {string} matchID
     * @param {object} result
     * @param {string} password
     * @param {object} [match]
     * @param {object} [opts]
     */
    async recordScore(compID, matchID, result, password, match, opts = {}) {
        _noteShownMatch(compID, match);
        const payload = toBackendMatchResult(result, match || result);
        if (result && result.forceCourt) {
            payload.forceCourt = true;
        }
        // Request-only like forceCourt: the organiser's "Change it anyway" on
        // a finished league's correction (write_result.jsx attemptScoreWrite).
        if (result && result.confirmFinished) {
            payload.confirmFinished = true;
        }
        // Request-only too: the organiser's "Replace the lots" on a result for
        // the drawn tie-breaker their lots settled.
        if (result && result.confirmLots) {
            payload.confirmLots = true;
        }
        // mp-y3nk: stamp in server-relative time for last-write-wins reconciliation.
        // Stamped when it is made (a running match's scoring is taps, each
        // stamped at its own tap, so no score write carries an older edit).
        // Never older than the match it was made against (bc-hlck), nor than
        // this device's last write of the match (_outboxWrite, _afterOwnStamp),
        // so the outbox's order and the stamps agree.
        const seen = _seenOfScore(result, match);
        _stampPayload(payload, _serverNowMs(), seen);

        // C2: stamp monotonic rev on running-status writes so the server's
        // rev-guard can hold out-of-order deliveries (e.g. from reconnect flush).
        // Completed writes do not need a rev: the guard is gated on status=running.
        const isRunning = payload?.status === 'running';
        if (isRunning) {
            payload.rev = _nextRev(compID, matchID);
            payload.revSession = _revSession;
        }

        // mp-9ukk Phase 2: broadcast helper. Publishes a match patch on the
        // court-local BroadcastChannel so the display tab can update without
        // a server round-trip during offline periods.
        //
        // The envelope mirrors the SSE match_updated shape including sideAId and
        // sideBId so the display tab's normalizeMatch can resolve participants by
        // UUID rather than falling back to name-key lookup (which can mis-resolve
        // same-name participants). winnerId is already in rest when present.
        // rev/revSession, and the groups the write names (`changed`, bc-mrgc),
        // are stripped so internal write-ordering metadata is not propagated
        // to the display tab.
        //
        // bc-evts stage 4 review 2 (G1): a start only starts. The server keeps
        // the score a sent-back match holds (engine.keepQueuedScore), so a
        // start-only write publishes no scoreline field at all (a key present
        // as undefined would still wipe the display's copy in the merge), and
        // states the flag counts the match it starts already holds: the
        // display reads a result naming a status that omits a count as 0
        // (mergeMatchPatch), which turned a kept 2-1 into 0-0. `startOnly` and
        // `forceCourt` (and `confirmFinished`) are request-only and stay off the display's copy.
        const _broadcastPatch = (fields) => {
            const {
                rev: _r, revSession: _rs, changed: _ch,
                writeId: _w, basis: _b, session: _s, forceDownstreamReopen: _f,
                startOnly, forceCourt: _fc, confirmFinished: _cf, confirmLots: _cl, ...rest
            } = fields || {};
            if (startOnly) {
                for (const k of START_BROADCAST_DROPS) delete rest[k];
                Object.assign(rest, _heldFlagCounts(match));
            }
            // Do not emit an unscoped (court-less) broadcast: a display can only
            // safely apply a patch it can attribute to its court, and the display
            // guard drops court-less messages anyway. In production every
            // recordScore caller passes the match (so court is set); the
            // court-less case is test-only (match=null), where no broadcast is
            // expected.
            _publishMatchPatch(compID, match, {
                id: matchID,
                ..._sideIdFields(match),
                ...rest,
            });
        };

        // bc-evts (the governing rule): EVERY score write is the outbox's, at
        // once, a running one (an autosave, Start match, Record bout) included:
        // stored in this tick on the match's base and sent strictly after what
        // the match already holds. A running write takes the place of the
        // match's last entry only when that is a running score write not yet
        // out (_coalesceTarget), claiming its groups and carrying a pending
        // kachinukiBoutFinal (_placeWrite). The caller carries on with the
        // pending answer; `settled` is the server's answer when it comes, and a
        // refusal nobody claims (opts.onRefusal) is told like a replay's.
        // `result.after` (request-only, like seenModifiedAt: toBackendMatchResult
        // copies named fields only) names another match's queued entry this
        // write must follow. The display overlay is the optimistic result.
        const answer = _outboxWrite({
            kind: 'score', compID, matchID, method: 'PUT',
            url: `/api/competitions/${compID}/matches/${matchID}/score`,
            payload, password, terminal: !isRunning, after: result && result.after,
            // `result.seenScoreVersion` (request-only, never sent): the server
            // copy the individual editor's board showed, the score-events
            // answer's when the board took it (m1); else the host's match.
            seen: result && result.seenScoreVersion !== undefined ? result.seenScoreVersion : match && match.scoreVersion,
            onRefusal: opts && opts.onRefusal,
        });
        _broadcastPatch(payload);
        return answer;
    },
    // T093–T095: kiken / fusenpai / fusensho / daihyosen: server auto-fills
    // scoreline and Winner from {decision, decisionBy, encho}. Body shape is
    // defined by mobileapp.DecisionRequest in handlers_decision.go; decisionBy
    // MUST be "shiro" or "aka", decisionReason is optional and ≤200 chars.
    // Response is the updated state.MatchResult.
    //
    // F1: bounded (12 s abort on stalled wifi), the body read included.
    // F5: on network failure / abort / 5xx / 429 the decision is enqueued as a
    // terminal write for durable re-delivery. 4xx (including a 409
    // on the DIRECT call) always throw: the score editor relies on a thrown 409
    // to show the refusal. A queued replay's 409 is reported too
    // (_flushQueue), never taken as success (bc-mrgc phase 3).
    async recordDecision(compID, matchID, body, password, opts = {}) {
        const decisionUrl = `/api/competitions/${compID}/matches/${matchID}/decision`;
        // Stamp in server-relative time, exactly as recordScore does (mp-y3nk),
        // when the operator acted, so a queued replay carries that time (a
        // match closed while still `scheduled` has no earlier write to inherit
        // one from, mp-jnvl). `seenModifiedAt` in the body is the match the
        // decision was recorded on (bc-hlck): it floors the stamp and is never
        // sent.
        const { seenModifiedAt: seen, ...rest } = body || {};
        const payload = _stampPayload({ ...rest }, _serverNowMs(), seen);
        // bc-evts: the outbox's, at once, behind this device's earlier writes
        // for the match. `opts.seen` is the score log line count the screen
        // showed (the match's scoreVersion): a correction over a finished match
        // carries the basis that says it saw the result (I1).
        return _outboxWrite({
            kind: 'decision', compID, matchID, method: 'POST', url: decisionUrl, payload, password,
            seen: opts && opts.seen, onRefusal: opts && opts.onRefusal,
        });
    },
    // T098: competitor eligibility statuses produced by kiken/fusenpai
    // decisions. Endpoint is read-only and unauthenticated: same contract as
    // the other /api/competitions/:id/* GETs surfaced to the viewer.
    async fetchCompetitorStatuses(compID) {
        const res = await fetch(`/api/competitions/${compID}/competitor-status`);
        if (!res.ok) throw new Error("Failed to load competitor statuses");
        const data = await res.json();
        return data.statuses || [];
    },
    async reinstateCompetitor(compID, playerID, password, opts = {}) {
        // R1: an outbox entry under the competitor's own base, answered at
        // once; `settled` resolves with the status the server stored, or
        // rejects with its refusal (`opts.onRefusal` may claim it).
        return _outboxWrite({
            kind: 'reinstate', compID, matchID: '', method: 'POST',
            url: `/api/competitions/${compID}/competitors/${playerID}/reinstate`,
            payload: {}, password, onRefusal: opts && opts.onRefusal,
            base: `competitor:${compID}:${playerID}`,
        });
    },
    // overridePoolRanks records a whole group of pool ranks, a chusen's order,
    // as ONE write: ranks is [{ playerId, rank }], and the server records
    // every rank together and answers for the final order only. Sent one rank
    // at a time, the order passed through states nobody chose, so a knockout
    // match whose place the final order keeps could be named and reopened.
    //
    // playerId is REQUIRED on every entry (operator ruling bc-pnum): the
    // server resolves a pool member by id only (poolHasPlayerID,
    // handlers_competition.go) and rejects the request with 400 when it is
    // missing -- two pool members can legally share a display name from
    // different dojos, so a name can never disambiguate them. Neither a name
    // nor a dojo is sent: the server never reads them.
    //
    // forceDownstreamReopen is the operator's confirmation of a 409
    // downstream_knockout_played refusal: a new order that moves a qualifier
    // out of a knockout match the old one already fought is refused exactly
    // like a pool result correction (and one being fought is refused outright,
    // downstream_knockout_running). Both are parsed into the same structured
    // errors recordScore throws, so the chusen panel offers the same
    // confirm-and-retry (write_result.jsx's attemptScoreWrite); `ranking`
    // marks the refusal as a rank change rather than a corrected result, for
    // the dialog's copy.
    overridePoolRanks(compID, poolID, ranks, password, forceDownstreamReopen, opts = {}) {
        // bc-evts stage 3 review 11, Y7 (the governing rule: "ALL goes to the
        // outbox ... but never blocks the UI"): an outbox entry under the
        // competition's pool-rank base, so a chusen's order and a reset of
        // the overrides are sent strictly in the order they were made,
        // answered pending at once. `settled` carries the server's answer, or
        // rejects with the refusal (_doorRefusal 'pool-ranks': a downstream
        // refusal is the structured error attemptScoreWrite reads, marked as
        // a ranking), which `opts.onRefusal` may claim. An older server's
        // empty 200 body is a success.
        const payload = { ranks };
        if (forceDownstreamReopen) payload.forceDownstreamReopen = true;
        return _outboxWrite({
            kind: 'pool-ranks', compID, matchID: '', method: 'PUT',
            url: `/api/competitions/${compID}/pools/${encodeURIComponent(poolID)}/override-rank`,
            payload, password, onRefusal: opts && opts.onRefusal,
            base: _poolRanksBase(compID),
        });
    },
    async updateMatchTime(compID, matchID, scheduledAt, password, opts = {}) {
        // bc-evts M5: an entry of the match, in its order, answered at once;
        // a refusal comes through `settled` or `opts.onRefusal`.
        // R4: shown at once, and `opts.was` (the time it had) put back on a refusal.
        const answer = _outboxWrite({
            kind: 'time', compID, matchID, method: 'PUT',
            url: `/api/competitions/${compID}/matches/${matchID}/time`,
            payload: { scheduledAt }, password, onRefusal: opts && opts.onRefusal, was: opts && opts.was,
        });
        _emitLocalMatchPatch(compID, matchID, { scheduledAt });
        return answer;
    },
    // Stateless estimate for a competition not yet saved, GET /api/schedule/draft-estimate.
    async estimateDraft(shape, password, signal) {
        const params = new URLSearchParams();
        const src = shape || {};
        const keys = [
            'format', 'participants', 'kind', 'teamSize', 'teamMatchType',
            'poolSize', 'poolSizeMode', 'poolWinners', 'poolFormat', 'roundRobin',
            'extraQualifiers', 'twoThirdPlaces', 'naginata', 'swissRounds',
            'poolMatchDurationSeconds', 'knockoutMatchDurationSeconds',
            'startTime', 'date',
        ];
        keys.forEach((k) => {
            const v = src[k];
            if (v !== undefined && v !== null && v !== "" && !Number.isNaN(v)) {
                params.append(k, v);
            }
        });
        let courtsNum;
        if (Array.isArray(src.courts)) {
            courtsNum = src.courts.length;
        } else if (src.courts !== undefined && src.courts !== null) {
            courtsNum = Number(src.courts);
        }
        if (Number.isFinite(courtsNum) && courtsNum > 0) {
            params.append('courts', courtsNum);
        }
        const qs = params.toString();
        const url = qs ? `/api/schedule/draft-estimate?${qs}` : '/api/schedule/draft-estimate';
        const res = await fetch(url, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Could not work out an estimate right now.");
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
    // Court (shiaijo) clashes between this competition and every other one
    // (same day, shared court, overlapping time windows). Returns a possibly
    // empty array of ClashWarning objects. Non-blocking: surfaced as a warning.
    async getScheduleClashes(compID, password, signal) {
        const res = await fetch(`/api/competitions/${compID}/schedule/clashes`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to check schedule clashes");
        }
        return res.json();
    },
    async fetchCalendar(password, signal) {
        const res = await fetch('/api/schedule/calendar', {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load the calendar");
        }
        return res.json();
    },
    async previewMove({ competitionId, date, startTime, courts } = {}, password, signal) {
        const params = new URLSearchParams();
        if (competitionId !== undefined && competitionId !== null && competitionId !== "") {
            params.append('competitionId', competitionId);
        }
        if (date !== undefined && date !== null && date !== "") {
            params.append('date', date);
        }
        if (startTime !== undefined && startTime !== null && startTime !== "") {
            params.append('startTime', startTime);
        }
        if (Array.isArray(courts) && courts.length > 0) {
            params.append('courts', courts.join(','));
        }
        const qs = params.toString();
        const url = qs ? `/api/schedule/calendar/preview?${qs}` : '/api/schedule/calendar/preview';
        const res = await fetch(url, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to preview the move");
        }
        return res.json();
    },
    // Preview of the number prefix a save would assign when the field is
    // empty (bc-pnum G2/R6): the CREATE form calls this to pre-fill the
    // prefix field with the SAME value assignDefaultNumberPrefix would pick
    // server-side, so what the operator sees before saving matches what
    // would actually land. Create only: the competition doesn't exist yet,
    // so there is nothing to exclude from the taken set. bc-pnum B5: an
    // excludeID parameter used to exist for a settings-screen pre-fill
    // caller that was built and retired before this endpoint's first
    // release; it never had a second caller and is removed, not merely
    // unused.
    async getNumberPrefixDefault(name, password, signal) {
        const params = new URLSearchParams();
        if (name) params.append('name', name);
        const res = await fetch(`/api/number-prefix-default?${params.toString()}`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to derive number prefix");
        }
        return res.json();
    },
    async moveMatchCourt(compID, matchID, court, password, opts = {}) {
        // bc-evts M5: an entry of the match, in its order, answered at once;
        // a refusal comes through `settled` or `opts.onRefusal`.
        // R4: shown at once (_emitLocalMatchPatch), and `opts.was` (the
        // court it had) is put back on a refusal.
        const payload = opts && opts.stopsRunning ? { court, stopsRunning: true } : { court };
        const answer = _outboxWrite({
            kind: 'court', compID, matchID, method: 'PUT',
            url: `/api/competitions/${compID}/matches/${matchID}/court`,
            payload, password, onRefusal: opts && opts.onRefusal, was: opts && opts.was,
        });
        _emitLocalMatchPatch(compID, matchID, { court });
        if (opts && opts.stopsRunning && opts.match) {
            const oldCourt = (opts.was && opts.was.court) || opts.match.court || '';
            const oldMatch = { ...opts.match, court: oldCourt };
            _attachSendBackDisplayPutBack(compID, matchID, oldMatch, answer, court);
        }
        return answer;
    },
    async revertMatchToQueue(compID, matchID, password, opts = {}) {
        // bc-evts: the outbox's, with a JSON body carrying writeId, basis and
        // session; `settled` resolves true, or rejects with the refusal.
        const answer = _outboxWrite({
            kind: 'revert', compID, matchID, method: 'POST',
            url: `/api/competitions/${compID}/matches/${matchID}/revert-to-queue`,
            payload: {}, password, seen: opts && opts.seen, onRefusal: opts && opts.onRefusal,
        });
        // bc-stpf: `match`, the bout as the caller shows it (its court, sides
        // and flag counts), lets a display on this machine show the send back
        // at once, and the bout running again when it does not land.
        const match = opts && opts.match;
        if (match) {
            _attachSendBackDisplayPutBack(compID, matchID, match, answer);
        }
        return answer;
    },

    // bc-stpf (operator ruling 2026-10-12, option A): would starting `matchID`
    // be accepted once the bout running on `court` is sent back? A direct read
    // (GET .../start-check), never the outbox: it writes nothing and must never
    // be replayed. It waits at most START_CHECK_TIMEOUT_MS, the body read
    // included, and never throws. Answers {ok: true}; {refused: Error} for a
    // definite answer that the start would fail: a 409 (the Error carrying the
    // server's sentence as the start's own refusal does, _reasonHumanError), a
    // 401 (auth failure), a 404 (the server no longer holds the match or its
    // competition, so the start would 404 too) or another 4xx; or
    // {unknown: true} when there is no such answer (no answer in time, a
    // network failure, or an answer the outbox would retry on, _serverKeepsRetrying), on
    // which the court console asks the operator.
    async checkStart(compID, matchID, court, password) {
        const url = `/api/competitions/${encodeURIComponent(compID)}/matches/${encodeURIComponent(matchID)}/start-check?court=${encodeURIComponent(court || '')}`;
        try {
            const { res, body } = await _fetchJson(url, {
                method: 'GET',
                headers: password ? { 'X-Tournament-Password': password } : {},
            }, START_CHECK_TIMEOUT_MS);
            if (res.ok && body && body.ok === true) return { ok: true };
            if (res.status === 409) return { refused: _reasonHumanError(body || {}, 'Could not start the match') };
            if (res.status === 401) return { refused: new Error('The password was not accepted. Sign in again, then start the match.') };
            if (res.status === 404) return { refused: new Error(START_CHECK_NOT_FOUND_MESSAGE) };
            if (res.status >= 400 && !_serverKeepsRetrying(res.status)) {
                return { refused: _reasonHumanError(body || {}, 'Could not start the match') };
            }
            return { unknown: true };
        } catch (_e) {
            return { unknown: true };
        }
    },

    // bc-stpf: true when a start check's answer could not be trusted, so the
    // court console asks the operator without asking the server: the device
    // is offline (the browser says so, or the outbox's last pass failed on the
    // network), or the outbox still holds a write of `compID` the server has
    // not seen that moves who is fighting, where, or who is barred
    // (START_CHECK_WRITE_KINDS: a finish, a decision, a start, a send back, a
    // court move, a reinstatement), whose effect the check would miss. A tap is
    // not counted: it moves points, never a status or a bar, and taps leave in
    // a microtask. Nor is a lineup, a team member or a time write: none of
    // them changes a match's status, its court, its sides or a bar, so one held
    // or failing for a while does not make every pick ask the operator.
    startCheckUntrusted(compID) {
        if (isOffline()) return true;
        if (_offlineFlag) return true;
        return API.queuedWritesOf(compID, START_CHECK_WRITE_KINDS) > 0;
    },
    // bc-obox C1/T1: the ONE reader of "this competition still has queued
    // writes of these kinds": how many entries this device's outbox holds for
    // `compID` whose kind is one of `kinds` (an array or a Set; a write an
    // older build stored with no kind is a score write). Pure: it asks nothing
    // about the connection. `compID` null counts every competition's, and the
    // writes that belong to none (the tournament settings, `''`). Re-read it on
    // subscribeUnsentWrites, which is told whenever an entry joins or leaves.
    // The draw waits for API.CHECK_IN_WRITE_KINDS; a move for ['time'].
    queuedWritesOf(compID, kinds) {
        const set = kinds instanceof Set ? kinds : new Set(Array.isArray(kinds) ? kinds : []);
        let n = 0;
        for (const d of _writeQueue.values()) {
            if (!d || !set.has(d.kind || 'score')) continue;
            if (compID === null || compID === undefined || d.compID === compID) n++;
        }
        return n;
    },
    // bc-obox C1: what this device's queued check-ins for `compID` found on
    // their rows, per participant id: the `was` of the EARLIEST queued entry
    // naming the id, what the row showed before this device touched it (the
    // check-in as last fetched or pushed). An id no queued entry names is not
    // in the map; one whose entry carries no `was` for it reads false. Pure,
    // like queuedWritesOf; the draw's "now anyway" confirm counts off it
    // (serverHeldCheckIns, qualifier_preview.jsx).
    queuedCheckInsWere(compID) {
        const out = new Map();
        for (const [, d] of _entriesFor(_checkInBase(compID))) {
            const was = d && d.was && typeof d.was === 'object' ? d.was : {};
            for (const pid of _checkInsOf(d).keys()) {
                if (!out.has(pid)) out.set(pid, Object.prototype.hasOwnProperty.call(was, pid) && !!was[pid]);
            }
        }
        return out;
    },
    CHECK_IN_WRITE_KINDS: Object.freeze([..._CHECKIN_KINDS]),
    // mp-gmcg: reopen a COMPLETED kachinuki team match, or (bc-tmfn) any
    // completed match a withdrawal decided: status back to running,
    // winner/decision cleared, fought bouts kept.
    //
    // The kachinuki Reopen sends NO REASON, deliberately (operator ruling). An
    // operator who ended a match BY MISTAKE at a shiaijo must be able to get
    // back into it in ONE TAP; making them justify the mistake before they may
    // undo it is friction at the worst possible moment, and ending the match
    // again asks for no reason either (operator ruling 2026-09-25: a match can
    // be reopened without any reason, and nothing is gated on that). Clear
    // withdrawal and reopen is one tap with no reason too; opts.reason stays
    // for any caller that has one to record. opts.force is the operator's confirmation after a
    // downstream_knockout_played refusal. opts.forceCourt is sent in the body beside reason
    // for an organiser's confirmed reopen on a busy shiaijo. The body stays a JSON object (never
    // absent) so a handler that binds JSON still parses the request.
    //
    // 400 = neither kachinuki nor withdrawal-decided; 409 = not completed / a
    // later match in progress or resolved by a bye / a later match with its
    // own result (downstream_knockout_played, parsed like the score path's,
    // so the caller confirms and retries with force) / another match is
    // already running on this match's court. The editor shows whichever came
    // back verbatim, so the unwrap keeps the server's own words. Resolves to
    // the server's { reopenedMatches: [{id, number, label}] }.
    //
    // TWO 409 SHAPES: most carry the sentence in `error`, but the court-busy
    // conflict reuses the score path's structured payload, where `error` is
    // the machine code "court_busy", the sentence is in `message`, and
    // court/matchId/compId identify the BLOCKING match. Prefer `message` so the
    // operator never reads a bare code, and carry the blocking match's identity
    // onto the thrown Error: without it the editor could only print the
    // conflict, and a busy court would be a dead end (kachinuki reopen is the
    // ONLY way to fix a bout log). The server broadcasts match_updated on
    // success.
    /**
     * Reopen a completed match.
     *
     * @param {string} compID
     * @param {string} matchID
     * @param {string} password
     * @param {object} [opts]
     * @param {string} [opts.reason=""]
     * @param {boolean} [opts.force=false]
     * @param {boolean} [opts.forceCourt=false] - sent in the body beside reason for an organiser's confirmed reopen on a busy shiaijo
     * @param {*} [opts.seen]
     * @param {Function} [opts.onRefusal]
     */
    async reopenMatch(compID, matchID, password, { reason = "", force = false, forceCourt = false, confirmFinished = false, confirmLots = false, seen, onRefusal } = {}) {
        // bc-evts: the outbox's; `settled` resolves the server body or rejects
        // with reopenFailureError's error (court_busy, downstream, ...).
        return _outboxWrite({
            kind: 'reopen', compID, matchID, method: 'POST',
            url: `/api/competitions/${compID}/matches/${matchID}/reopen`,
            payload: reopenBody(reason, force, forceCourt, confirmFinished, confirmLots), password, seen, onRefusal,
        });
    },
    // mp-gmcg (review A4): atomically requeue the match holding the court AND
    // reopen the target, in ONE server call under one court lock — replaces the
    // former two-call revert-then-reopen, which raced (a peer could take the
    // freed court between the calls). Surfaces the SAME court_busy detail shape
    // as reopenMatch, so the panel re-offers the remedy when a DIFFERENT match
    // has since taken the court. opts as reopenMatch's: the remedy carries the
    // same reason and confirmation the reopen it retries did.
    async requeueBlockerAndReopen(targetComp, targetMatch, blockerComp, blockerMatch, password, { reason = "", force = false, confirmFinished = false, confirmLots = false, seen, onRefusal, match, blockerLabel } = {}) {
        // bc-evts: the outbox's, on the TARGET match's base (the one reopened),
        // and ordered under the blocker's too (M5): it follows the blocker's
        // last queued write (`after`), and a write of the blocker made after it
        // follows it (`holds`, _heldBy), so neither overtakes the other.
        // `match` (the target as shown) and `blockerLabel` (the blocker as the
        // court_busy answer names it) name the two if either is held (H5).
        _noteShownMatch(targetComp, match);
        _noteMatchLabel(blockerComp, blockerMatch, blockerLabel);
        const blockerBase = _revKey(blockerComp, blockerMatch);
        return _outboxWrite({
            kind: 'requeue', compID: targetComp, matchID: targetMatch, method: 'POST',
            url: `/api/competitions/${targetComp}/matches/${targetMatch}/requeue-blocker-and-reopen`,
            payload: { blockerCompId: blockerComp, blockerMatchId: blockerMatch, ...reopenBody(reason, force, false, confirmFinished, confirmLots) },
            password, seen, onRefusal,
            after: _lastEntryKey(blockerBase), holds: [blockerBase],
        });
    },
    // mp-gmcg: remove a trailing UNSCORED kachinuki bout appended by mistake
    // ([Record bout] / [Add next bout]). Kachinuki-only; targets a numbered
    // bout, never a daihyosen. Returns the updated MatchResult (envelope
    // unwrapped, like removeDaihyosen). A 409 ("no unscored bout to remove" /
    // "match is not running") surfaces as the thrown Error's message.
    async removeKachinukiBout(compID, matchID, password, opts = {}) {
        // bc-evts: the outbox's; `settled` resolves the stored match the
        // server answered with (body.result), or rejects with the refusal.
        return _outboxWrite({
            kind: 'kachinuki', compID, matchID, method: 'DELETE',
            url: `/api/competitions/${compID}/matches/${matchID}/kachinuki-bout`,
            payload: {}, password, seen: opts && opts.seen, onRefusal: opts && opts.onRefusal,
        });
    },
    async importCompetitions(formData, password) {
        const res = await _directFetch('/api/tournament/import', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: formData
        });
        if (!res.ok) {
            const body = await res.json().catch(() => ({}));
            throw new Error(body.error || 'Import failed');
        }
        // An import brings competitions whose matches may reuse ids a label
        // was learned for: none is trusted (K8).
        _forgetAllShownMatches();
        return res.json();
    },
    // Deleting a competition asks for the password again: `confirmPassword` is
    // what the operator typed in that moment, sent beside the sign-in password.
    // A refusal of it is a 401 that carries a `confirm_password_*` code; it is
    // thrown as an Error with that `code` and nothing else (no queue, no parking,
    // no sign-out), so the dialog that asked can show the sentence and ask again.
    async deleteCompetition(id, password, confirmPassword) {
        const res = await _directFetch(`/api/competitions/${id}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password, 'X-Confirm-Password': confirmPassword ?? '' }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw Object.assign(new Error(err.error || "Failed to delete competition"), { code: err.code });
        }
        // Its match ids are free for a competition made again under the same
        // name (X4); another device hears of it as a tournament_updated push.
        _forgetShownMatches(id);
        return true;
    },
    async backupTournament(password) {
        const res = await _backupRequest('/api/tournament/backup', 'Failed to download tournament backup', { password });
        const filename = _backupFilename(res);
        const blob = await res.blob();
        return { blob, filename };
    },
    async backupCompetition(id, password) {
        const res = await _backupRequest(`/api/competitions/${encodeURIComponent(id)}/backup`, 'Failed to download competition backup', { password });
        const filename = _backupFilename(res);
        const blob = await res.blob();
        return { blob, filename };
    },
    planRestore({ file, from, only, undo, decisions } = {}, password) {
        return _backupRequest('/api/tournament/restore/plan', 'Failed to plan restore', {
            method: 'POST', password, body: _restoreForm({ file, from, only, undo, decisions }),
            sentence: NOT_SENT_NO_CONNECTION // the plan is a dry run: it changes nothing
        }).then(r => r.json());
    },
    async restore({ file, from, only, undo, decisions, allowRunning } = {}, password, confirmPassword) {
        const fd = _restoreForm({ file, from, only, undo, decisions });
        if (allowRunning === true) fd.append('allowRunning', 'true');
        const res = await _backupRequest('/api/tournament/restore', 'Failed to restore', {
            method: 'POST', password, confirmPassword: confirmPassword ?? '', body: fd
        });
        _forgetAllShownMatches();
        return res.json();
    },
    async listBackups(password) {
        const data = await (await _backupRequest('/api/tournament/backups', 'Failed to list backups', { password })).json();
        return data.backups || [];
    },
    downloadBackup(name, password) {
        return _backupRequest(`/api/tournament/backups/${encodeURIComponent(name)}`, 'Failed to download backup', { password }).then(r => r.blob());
    },
    deleteBackup(name, password, confirmPassword) {
        return _backupRequest(`/api/tournament/backups/${encodeURIComponent(name)}`, 'Failed to delete backup', {
            method: 'DELETE', password, confirmPassword: confirmPassword ?? ''
        }).then(r => r.json());
    },
    // Returns the updated competition JSON (including the new `status`) so
    // callers can apply it to local state immediately instead of waiting for
    // the SSE refresh / tournament refetch to land.
    async invalidateCompetition(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/invalidate`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to invalidate competition");
        }
        return res.json();
    },
    // Replace the full fighting-spirit awards list for a competition.
    // awards: array of { title, recipientName, recipientDojo? }.
    // awardsVersion: the version of the list this save was made from (the
    // competition detail's awardsVersion, or the last save's answer); a save
    // from an older list is refused (409 awards_changed).
    // bc-obox G4b: an outbox entry under the base `awards:<comp>`, answered at
    // once ({ pending, settled }); `settled` resolves with the competition as
    // saved (its awardsVersion with it) or rejects with the refusal (409
    // awards_changed carries `code`, `awardsVersion` and
    // `fightingSpiritAwards`, the list now). The whole list is sent, so a save
    // made while one is queued takes its place (_coalesceTarget).
    async updateCompetitionAwards(id, awards, password, awardsVersion, opts = {}) {
        const base = awardsBase(id);
        return _outboxWrite({
            kind: 'awards', compID: id, matchID: '', method: 'PUT', url: `/api/competitions/${id}/awards`,
            payload: { fightingSpiritAwards: awards, awardsVersion: currentDocVersion(base, awardsVersion) },
            password, onRefusal: opts && opts.onRefusal, base, label: 'Fighting-spirit awards',
        });
    },
    // The GET answers saved: false when nothing is stored; lineupOrNull
    // makes that null ("blank, editable"). A 404 means the competition does
    // not exist and throws. PUT replaces; DELETE clears. The read is of the
    // exact round: the Lineups page reads a team's starting lineup (round 0)
    // this way, and the match surfaces (score sheet, displays, the at-court
    // lineup panel) do not read rounds at all: they read fetchLineupInForce
    // through resolveMatchLineup.
    async fetchTeamLineup(compID, teamId, round) {
        return _readLineup(`/api/competitions/${compID}/teams/${teamId}/lineups/${round}`);
    },
    // memberIds (bc-tmid pass 3) is optional and keyed by the same position
    // as positions: the squad member id half of a lineup, sent alongside
    // the name so the server can persist both together. Omitted entirely
    // when the caller passes nothing, so a caller that never adopted squad
    // members (or an older bundle) round-trips exactly as before.
    //
    // changed (operator decision 2026-10-07, "Only changed positions") names the
    // positions this save changes: the server reads only those and lands them
    // on the lineup it holds when the save arrives, so a position another device
    // changed meanwhile stays, and the answer is the whole lineup it then holds
    // (lineup_save.jsx builds the body: positions and memberIds for the changed
    // positions only). Omitted, the body is a whole lineup that replaces the
    // stored one.
    async putTeamLineup(compID, teamId, round, positions, password, memberIds, changed, opts = {}) {
        // bc-evts M5: an outbox entry under the lineup's own base (a starting
        // lineup belongs to no match), answered at once like every write; a
        // save made while the last one is queued joins it (_coalesceTarget).
        return _outboxWrite({
            kind: 'lineup', compID, matchID: '', method: 'PUT',
            url: `/api/competitions/${compID}/teams/${teamId}/lineups/${round}`,
            payload: {
                teamId, competitionId: compID, round, positions,
                ...(memberIds ? { memberIds } : {}), ...(changed ? { changed } : {}),
            },
            password, onRefusal: opts && opts.onRefusal,
            base: _lineupKey(compID, teamId, { round }),
        });
    },
    // bc-tmid pass 3: a team's squad, the actual people on it, lives in its
    // own per-competition store (team-members.yaml), keyed by the team's
    // participant id -- see internal/state/squad.go. Returns the whole
    // map ({ teamId: [{id, index, name}, …] }) since the lineup editor
    // needs its own team's list, not one member at a time.
    // bc-mrgc: a match's write history, oldest change first: every write that
    // reached the match, with the values of a change kept rather than applied
    // because a newer change to the same thing was recorded first. An
    // organiser read (main-gated in self-run); the score editors' History
    // disclosure (match_history_view.jsx) is its one reader.
    async fetchMatchHistory(compID, matchID, password) {
        const res = await fetchWithTimeout(`/api/competitions/${encodeURIComponent(compID)}/matches/${encodeURIComponent(matchID)}/history`, {
            headers: password ? { 'X-Tournament-Password': password } : {}
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load the match history");
        }
        const data = await res.json();
        return Array.isArray(data) ? data : [];
    },
    async fetchSquads(compID, password) {
        const res = await fetch(`/api/competitions/${compID}/team-members`, {
            headers: password ? { 'X-Tournament-Password': password } : {}
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load team members");
        }
        const data = await res.json();
        // A member the server lists exists, whatever was known of them.
        return _withListedMembersForgotten(data.teamMembers, compID) || {};
    },
    // The server gives the new member its display index (operator ruling:
    // assigned automatically as members are added); clearTeamMember below is
    // the nearest counterpart and only blanks a name: an index, once minted, is
    // never freed. Every member write's `settled` resolves with the member as
    // the server stamped it, and the server announces the change
    // (lineup_updated, with the team) to every device.
    async addTeamMember(compID, teamId, name, password, opts = {}) {
        // R1: the id is minted here, so the lineup save naming the member is
        // queued behind the add (putMatchLineup's `after`) with the id it will
        // have; a resend of the add is answered as stored. The member shown
        // carries no stamp: nothing holds it yet, and the server's copy, when
        // a list brings it, stands over it.
        const id = _newClientId();
        const trimmed = String(name || '').trim();
        return _memberWrite({
            compID, teamId, method: 'POST',
            url: `/api/competitions/${compID}/teams/${teamId}/members`,
            payload: { id, name: trimmed }, password,
            member: { ...(opts.index ? { index: opts.index } : {}), id, name: trimmed },
            opts,
        });
    },
    // Keeps memberId's id and index; only the display name changes. Answers 200
    // with the member, stamped.
    async renameTeamMember(compID, teamId, memberId, name, password, opts = {}) {
        // R1: an outbox entry; `opts.member` is the member as shown, which the
        // answer keeps (its number) with the new name, and which a refusal
        // puts back (`revertMember`).
        const trimmed = String(name || '').trim();
        const before = opts.member || { id: memberId };
        return _memberWrite({
            compID, teamId, method: 'PUT',
            url: `/api/competitions/${compID}/teams/${teamId}/members/${memberId}`,
            payload: { name: trimmed }, password,
            member: _shownMember(before, { id: memberId, name: trimmed }),
            opts: { ...opts, was: before },
        });
    },
    // The operator's "removal": blanks memberId's Name back to "" and
    // leaves the id and display index untouched, so a bout already fought
    // that names this position keeps meaning the same person (bc-pnum).
    // Answers 200 with the member, nameless and stamped. Refused with a 409
    // once the competition has started (state.ErrTeamMemberClearAfterStart);
    // the server's own message is operator-facing, so it is surfaced verbatim
    // rather than remapped here.
    async clearTeamMember(compID, teamId, memberId, password, opts = {}) {
        // R1: an outbox entry, as renameTeamMember; the member shows unnamed at once.
        const before = opts.member || { id: memberId };
        return _memberWrite({
            compID, teamId, method: 'DELETE',
            url: `/api/competitions/${compID}/teams/${teamId}/members/${memberId}`,
            payload: {}, password,
            member: _shownMember(before, { id: memberId, name: '' }),
            opts: { ...opts, was: before },
        });
    },
    // mp-825 / mp-bkg: per-match lineup endpoints. Match ID takes the
    // place of the round key: successive encounters between the same
    // two teams each carry an independent lineup entry. Nothing here reads
    // the lineup saved for exactly one match: what a team fields at a match
    // is fetchLineupInForce.
    // The lineup a team fields at a match (operator ruling 2026-10-05: a team
    // carries the lineup of its previous match unless one is entered for the
    // match). The server owns the rule (engine/lineup_in_force.go): the
    // match's own lineup, else the latest of the team's earlier matches'
    // lineups, else its starting lineup. `saved: false` -> null (nothing
    // applies). A lineup carries where it was saved: `sourceMatchId` (this
    // match, or an earlier one it is carried from) or `sourceRound` (0, the
    // starting lineup). A 404 means the competition does not exist.
    async fetchLineupInForce(compID, teamId, matchId) {
        return _readLineup(`/api/competitions/${compID}/teams/${teamId}/lineup-in-force/${matchId}`);
    },
    // bc-kfnl: a kachinuki encounter's fighters each side has left, for the
    // advisory line above the live bout (kachinuki_advisory.jsx). The server
    // reads it through the same call that appends the next pairing. Public,
    // bounded like every read (_fetchJson). It NEVER throws: the line is a
    // guide only, so any failure, a timeout, a non-OK answer (400 not
    // kachinuki, 404 unknown match) or an answer without both sides reads as
    // null, and the editor then shows no line.
    async fetchKachinukiRoster(compID, matchID) {
        try {
            const { res, body } = await _fetchJson(
                `/api/competitions/${encodeURIComponent(compID)}/matches/${encodeURIComponent(matchID)}/kachinuki-roster`, {});
            if (!res.ok || !body || !body.sideA || !body.sideB) return null;
            return body;
        } catch (_e) {
            return null;
        }
    },
    // memberIds (bc-pnum gap closure) is optional and keyed by the same
    // position as positions, exactly like putTeamLineup's own memberIds
    // above: the squad member id half of a lineup, sent alongside the name
    // so the server can persist both together. Omitted entirely when the
    // caller passes nothing, so a caller that never adopted squad members
    // (or an older bundle) round-trips exactly as before; the outbox entry
    // carries them, so a replayed lineup write carries the ids too. `changed`
    // names the positions
    // the save changes, as for putTeamLineup above.
    async putMatchLineup(compID, teamId, matchId, positions, password, memberIds, changed, opts = {}) {
        // bc-evts M5: an entry of the MATCH, sent strictly in its order with
        // the match's other writes, and answered at once ({pending, settled}):
        // nothing waits on the network. `settled` resolves with the whole
        // lineup the server holds, or rejects with its refusal (a finished
        // match's 409 carries its code in `error` and its sentence in
        // `message`, kept by _refusalError); `opts.onRefusal` may claim it.
        // writeId, session and basis ride along; the lineup PUT reads none.
        // R1: it follows the team's member writes queued before it (`after`
        // their last entry, strict order on their base), so a member it names
        // by a minted id is added, and a rename lands, before the lineup that
        // places them: _followMintedMembers, as it is queued (K4).
        return _outboxWrite({
            kind: 'lineup', compID, matchID: matchId, method: 'PUT',
            url: `/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`,
            payload: {
                teamId, competitionId: compID, matchId, positions,
                ...(memberIds ? { memberIds } : {}), ...(changed ? { changed } : {}),
            },
            password, onRefusal: opts && opts.onRefusal,
        });
    },
    // Removes the match's own lineup: an outbox entry of the match (kind
    // `lineup-remove`, R1), answered at once and sent in the match's order like
    // every write of it; `settled` resolves once the server has removed it. A
    // lineup that is not there is removed already (a 404 settles as done).
    async deleteMatchLineup(compID, teamId, matchId, password, opts = {}) {
        // R1: an entry of the MATCH, after every write of it queued before it
        // (a save of the lineup included, so no wait for one is needed), and
        // answered at once. A 404 (nothing saved to remove) is the outcome
        // asked for. `settled` resolves true, or rejects with the refusal.
        return _outboxWrite({
            kind: 'lineup-remove', compID, matchID: matchId, method: 'DELETE',
            url: `/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`,
            payload: {}, password, onRefusal: opts && opts.onRefusal,
        });
    },
    // Whether a save of this lineup is still queued. `target` is { matchId } for a
    // match's own lineup or { round } for a team's Lineups-page round (0: its
    // starting lineup). The team sheet asks it before it shows a read of the
    // lineup (a read answered while a save is queued lacks that save). Nothing
    // is composed on a queued save: a new save of the lineup joins it and names
    // what it changed, and a removal is ordered after it by its own stamp.
    queuedLineupSave(compID, teamId, target) {
        return _queuedLineupSave(compID, teamId, target || {});
    },
    // T141: daihyosen (representative bout) appended after a knockout team
    // match ties on IV and PW. Server validates the tie + eligibility and
    // returns the updated MatchResult with a new SubMatchResult whose
    // decision="daihyosen" and Position=-1. The error codes (not_tied,
    // pool_match, insufficient_eligibility) are surfaced verbatim by the
    // caller: see TeamScoreEditorModal for the user-visible mapping.
    // Stamped like recordDecision (bc-dhas), so the add competes on
    // timestamps: see _daihyosenOutcome for what comes back.
    // seenModifiedAt (optional): the match as the sheet showed it (bc-hlck).
    async recordDaihyosen(compID, matchID, password, seenModifiedAt, opts = {}) {
        return _daihyosenRequest('POST', compID, matchID, password, seenModifiedAt, opts);
    },
    // T141: remove an unscored daihyosen placeholder from a knockout team match.
    // Returns the updated MatchResult on 200. Throws on 404 (no daihyosen or
    // match not found) or 409 (daihyosen already scored: clear scores first).
    // Stamped like the add, in a JSON body (the handler binds one on DELETE
    // too).
    async removeDaihyosen(compID, matchID, password, seenModifiedAt, opts = {}) {
        return _daihyosenRequest('DELETE', compID, matchID, password, seenModifiedAt, opts);
    },
    // T190-T193 (US13: Swiss format). Generate the next Swiss round.
    // Backend pre-conditions: format=swiss; all matches in the current
    // round must be completed; swissCurrentRound must be < swissRounds.
    // Returns { round, matches, swissCurrentRound } on 201, throws on
    // 4xx/5xx. 409 with code="round_incomplete" surfaces a friendly
    // "current round still has incomplete matches" message in the UI;
    // the caller checks `e.code === "round_incomplete"` to branch.
    async swissGenerateRound(compID, password) {
        const res = await _directFetch(`/api/competitions/${compID}/swiss/generate-round`, {
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
    // T190-T193 (US13). Public endpoint: returns cumulative Swiss
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
    async leagueStandings(compID) {
        const res = await fetch(`/api/competitions/${compID}/league/standings`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load league standings");
        }
        return res.json();
    },
    // bc-obox C1/C2: a check-in tap (checkedIn true) or its undo (false), an
    // outbox entry under the competition's base `checkin:<comp>`, answered at
    // once ({ pending, settled }) and shown at once (subscribeLocalParticipantPatch,
    // and every fetch lays it over its players until it lands). The stamp is
    // the tap's, so the later tap wins whatever arrives first. `settled`
    // resolves with the server's answer: the participant as stored, or 200
    // { applied: false, reason: 'superseded', participant } when another
    // desk's later tap already changed them (the desk takes `participant`;
    // when it is not what was asked the operator is told once, through
    // `opts.onRefusal` if it returns true, else the queue alert). A refusal
    // (409 check_in_not_in_self_run, a 404) rejects. opts: `was` ({ [pid]:
    // bool }, what the row showed before the tap, put back on a refusal or a
    // discard; default the opposite of `checkedIn`), `name` (how the
    // held-writes list names it), `onRefusal`.
    async toggleCheckIn(compID, pid, checkedIn, password, opts = {}) {
        const answer = _outboxWrite({
            kind: 'checkin', compID, matchID: '', method: checkedIn ? 'PUT' : 'DELETE',
            url: `/api/competitions/${encodeURIComponent(compID)}/participants/${encodeURIComponent(pid)}/checkin`,
            payload: { modifiedAt: _serverNowMs() },
            password, onRefusal: opts && opts.onRefusal, base: _checkInBase(compID),
            was: opts && opts.was && typeof opts.was === 'object' ? opts.was : { [pid]: !checkedIn },
            label: _checkInLabel(opts && opts.name),
        });
        _emitLocalCheckIns(compID, [[pid, !!checkedIn]]);
        return answer;
    },
    // Bulk check-in for one competition. participantIds is an array of pids as
    // built by checkinApiPid/rdApiPid: a stable UUID, or "" for a legacy
    // UUID-less row (bc-pnum operator ruling: this write is id only, never
    // the "name|dojo" composite -- name and dojo are operator-editable after
    // the draw, so that composite is not a safe wire identifier). An
    // id-less pid resolves to nothing server-side and is reported back via
    // notFound. An outbox entry like toggleCheckIn's, in the same base;
    // `settled` resolves with { checkedIn, alreadyCheckedIn, notFound,
    // superseded: [participant as stored] }, the superseded ones taken as
    // stored (told once when not checked in). opts.was is per id (the ids of
    // one tap may have started in different states; default not checked in).
    // Used by the Registration desk's "check in a whole dojo" action.
    async bulkCheckIn(compID, participantIds, password, opts = {}) {
        const ids = Array.isArray(participantIds) ? participantIds : [];
        const answer = _outboxWrite({
            kind: 'checkin-bulk', compID, matchID: '', method: 'POST',
            url: `/api/competitions/${encodeURIComponent(compID)}/participants/checkin-bulk`,
            payload: { participantIds: ids, modifiedAt: _serverNowMs() },
            password, onRefusal: opts && opts.onRefusal, base: _checkInBase(compID),
            was: opts && opts.was && typeof opts.was === 'object'
                ? opts.was : Object.fromEntries(ids.filter(Boolean).map((id) => [id, false])),
            label: _checkInLabel(opts && opts.name, ids.filter(Boolean).length),
        });
        _emitLocalCheckIns(compID, ids.filter(Boolean).map((id) => [id, true]));
        return answer;
    },
    async addParticipant(compID, payload, password) {
        const res = await _directFetch(`/api/competitions/${encodeURIComponent(compID)}/participants`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to add participant');
        }
        return res.json();
    },
    async replaceParticipant(compID, pid, payload, password) {
        const res = await _directFetch(`/api/competitions/${encodeURIComponent(compID)}/participants/${encodeURIComponent(pid)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to replace participant');
        }
        return res.json();
    },
    // bc-obox A1: a court call, an outbox entry under its own base
    // `announce:<id>`. The device makes the id (a lowercase UUID v4) and the
    // answer carries it at once ({ pending, settled, id }), so a withdrawal can
    // name the call before it has landed. `settled` resolves with the call as
    // the server stored it (`recorded` when the id was already known,
    // `expired` when its time was over on arrival), or { discarded: true,
    // withdrawn: true } when it was withdrawn before it was sent; a refusal
    // rejects.
    async sendAnnouncement(message, durationMinutes, password, opts = {}) {
        const id = _newClientId();
        const answer = _outboxWrite({
            kind: 'announce', compID: '', matchID: '', method: 'POST', url: '/api/tournament/announce',
            payload: { id, message, durationMinutes, modifiedAt: _serverNowMs() },
            password, onRefusal: opts && opts.onRefusal, base: _announceBase(id), label: _courtCallLabel(message),
        });
        return { ...answer, id };
    },
    async fetchAnnouncements() {
        const res = await fetch('/api/tournament/announcements');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch announcements (Status ${res.status})`);
        }
        return res.json();
    },
    // bc-obox A1: a court call withdrawn, an outbox entry in the call's base,
    // so it goes after the call's send. `settled` resolves true once the call
    // is down, a 404 included (it is already gone: it expired, or another
    // console withdrew it first, which is the outcome asked for, bc-cdbl); a
    // refusal rejects. A withdrawal made while the call's send is still
    // queued and not yet out takes the send out of the outbox, so the venue
    // screens never show a call already taken back: the send settles
    // { discarded: true, withdrawn: true }. The withdrawal is queued all the
    // same: another tab of this device may have that send out, and it can land
    // after the withdrawal does. The server remembers a withdrawal it could not
    // match (AnnouncementStore.Remove) and records the late send without
    // showing it, so the answer is a 404 or a 200, either one is done.
    async deleteAnnouncement(id, password, opts = {}) {
        const base = _announceBase(id);
        const send = _entriesFor(base).find(([k, d]) => d.kind === 'announce' && !_inFlight.has(d) && !_isFollowed(k));
        if (send) {
            const [key, d] = send;
            _dequeue(key, d, { value: { discarded: true, withdrawn: true } });
        }
        return _outboxWrite({
            kind: 'announce-remove', compID: '', matchID: '', method: 'DELETE',
            url: `/api/announcements/${encodeURIComponent(id)}`,
            payload: {}, password, onRefusal: opts && opts.onRefusal, base,
            label: _courtCallLabel(opts && opts.message),
        });
    },
    // bc-obox A1: every court call made at or before this moment taken down,
    // an outbox entry under its own base `announce-clear`, stamped
    // (`modifiedAt`): the server clears what was made at or before it and
    // keeps that floor, so a call made before the clear that arrives after it
    // shows nothing, and one made after it stands whatever the arrival order.
    // `settled` resolves with the calls left showing; a refusal rejects.
    async clearAnnouncements(password, opts = {}) {
        return _outboxWrite({
            kind: 'announce-clear', compID: '', matchID: '', method: 'DELETE', url: '/api/announcements',
            payload: { modifiedAt: _serverNowMs() },
            password, onRefusal: opts && opts.onRefusal, base: _ANNOUNCE_CLEAR_BASE, label: 'Every court call',
        });
    },
    // mp-c38: sponsor logo upload (multipart) and delete.
    async uploadSponsor({ file, name, link, password }) {
        const fd = new FormData();
        fd.append('name', name);
        if (link) fd.append('link', link);
        fd.append('file', file);
        const res = await _directFetch('/api/sponsors', {
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
        const res = await _directFetch(`/api/sponsors/${index}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete sponsor (Status ${res.status})`);
        }
    },
    // mp-scf: tournament branding: logo upload/delete.
    async uploadBrandingLogo({ file, password }) {
        const fd = new FormData();
        fd.append('file', file);
        const res = await _directFetch('/api/branding/logo', {
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
        const res = await _directFetch('/api/branding/logo', {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete logo (Status ${res.status})`);
        }
    },

    // Phase 3b (mp-8rc9): league tie-breaker operator API.
    //
    // leagueTiebreakCandidates: GET /competitions/:id/league-tiebreak/candidates
    // Returns { candidates: [{teamNames, teams, minPosition, maxPosition}] }
    // where teams is [{id, name, dojo}] (id "" for a legacy competitor): groupTeamIds
    // in admin_pools.jsx derives the teamIds a namesake group needs from it.
    async leagueTiebreakCandidates(compID) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak/candidates`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to load league tie-breaker candidates (${res.status})`);
        }
        return res.json();
    },

    // chusenCandidates: GET /competitions/:id/chusen-candidates
    // Consequential team-pool ties the daihyosen could not settle; the operator
    // resolves each by chusen (drawing lots), recorded via overridePoolRanks.
    async chusenCandidates(compID, password) {
        // Lives in the admin-gated competition router, so it needs the
        // admin password header (unlike the public league candidates GET).
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/chusen-candidates`, {
            headers: { 'X-Tournament-Password': password || '' }
        });
        if (!res.ok) {
            // Surface the server-provided error + status so auth/config failures
            // are diagnosable from the admin UI, matching the other helpers.
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to load chusen candidates (${res.status})`);
        }
        const data = await res.json();
        // candidates: ties still waiting for a chusen. recorded: ties a chusen
        // already settled, each with the recorded `ranks` parallel to `teams`,
        // so a chusen recorded in the wrong order can be changed.
        return { candidates: data.candidates || [], recorded: data.recorded || [] };
    },

    // leagueTiebreakGenerate: POST /competitions/:id/league-tiebreak
    // Body: { teamNames: string[], teamIds: string[] }. teamIds is REQUIRED
    // (operator ruling bc-pnum): the server selects the tied group by id
    // only and 400s outright when teamIds is missing, has fewer than two
    // entries, carries a blank entry, or has a duplicate -- teamNames alone
    // is ambiguous the moment two tied teams share a display name across
    // dojos, which this project explicitly allows. The caller MUST NOT
    // invoke this without a real id for every team (admin_pools.jsx gates
    // the "Run tie-breaker" button on groupTeamIds(...) !== undefined for
    // exactly this reason, disabling it rather than sending a request that
    // can only 400).
    // Returns { matches: MatchResult[] } on 201.
    // Throws on 400 (invalid group), 409 (matches already exist).
    async leagueTiebreakGenerate(compID, teamNames, password, teamIds) {
        const body = { teamNames, teamIds };
        const res = await _directFetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to generate league tie-breaker (${res.status})`);
            if (err.error) e.code = err.error;
            throw e;
        }
        return res.json();
    },

    // leagueTiebreakRemove: DELETE /competitions/:id/league-tiebreak
    // Body: { teamNames: string[], teamIds: string[] }: the tied group whose
    // unscored matches to remove. teamIds is REQUIRED, same contract as
    // leagueTiebreakGenerate above -- a namesake-holding group can only ever
    // have been CREATED via teamIds, so it must be removable the same way
    // or it could never be deleted again. Same UI-level gating applies: the
    // "Remove unscored tie-breaker" button is disabled when groupTeamIds(...)
    // is undefined.
    // Returns { deleted: number } on 200.
    // Throws on 404 (no matches found), 409 (any match already scored).
    async leagueTiebreakRemove(compID, teamNames, password, teamIds) {
        const body = { teamNames, teamIds };
        const res = await _directFetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak`, {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to remove league tie-breaker matches (${res.status})`);
            if (err.error) e.code = err.error;
            throw e;
        }
        return res.json();
    },

    // runTiebreakerRound: POST /competitions/:id/pools/:poolId/tiebreak-round
    // "Run another tie-breaker round" over an individual tie a finished
    // round left level. Body { playerIds }: one run of the
    // chusen candidates' stillLevel, by participant id only. Returns
    // { matches } on 201, the bouts added. Throws on 400 with the server's
    // sentence (no longer such a run, the lots already ordered it, the pools
    // are over) and 404 (competition or pool not found).
    async runTiebreakerRound(compID, poolName, playerIds, password) {
        const res = await _directFetch(`/api/competitions/${encodeURIComponent(compID)}/pools/${encodeURIComponent(poolName)}/tiebreak-round`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify({ playerIds }),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Could not add the round. Check the connection and try again.");
        }
        return res.json();
    },

    // F2/F3: reconnectEvents: force a fresh SSE connection (_reconnectEvents).
    reconnectEvents() {
        _reconnectEvents();
    },

    // -------------------------------------------------------------------------
    // F5: hasPendingTerminalWrite: returns true when the queue holds a TERMINAL
    // entry for the given (compID, matchID) score/decision key.
    // The score editor uses this to show a "not yet saved / retry" indicator
    // when a completed score or decision is waiting for connectivity.
    // Exposed as API.hasPendingTerminalWrite / window.API.hasPendingTerminalWrite.
    // -------------------------------------------------------------------------
    hasPendingTerminalWrite(compID, matchID) {
        // A finished result of the match (isFinishedResultWrite, R5): the
        // editors' held-result banner reads it, so a queued court move, time
        // change or lineup save of the match is not one.
        return _entriesFor(_revKey(compID, matchID)).some(([, d]) => isFinishedResultWrite(d));
    },

    // The way past a held write the server keeps refusing (a 5xx on every
    // retry, past the server-error notice threshold). It holds back no other
    // write, but it is retried for as long as the page is open, keeps the
    // pill on "Not saving" and is counted by the logout gate, so the operator
    // can discard it, and only it, from the match's editor after a confirm
    // (HeldWriteDiscard, admin_scoring_shared.jsx). Every other write held for
    // the match, and every other match's, is left alone.
    // Whether this device still holds ANY write for the match (a running
    // update, a result, a decision): the editors' pending banner stays while
    // one does and goes when none is left, landed or discarded
    // (useMatchHeldWrite, admin_scoring_shared.jsx).
    hasHeldWrite(compID, matchID) {
        // Only a write that already failed a pass (or came back from storage)
        // is held: a Finish on its first send is not "Not sent yet" (bc-evts).
        return _entriesFor(_revKey(compID, matchID)).some(([, d]) => d.held);
    },

    // bc-evts S3a: one scoring tap, as an event for the score-events door.
    // SYNCHRONOUS: the tap is in the persisted outbox when this returns, and
    // the send is kicked a microtask later, so taps made in one tick go in one
    // batch. `ev` is a score_events.jsx makeEvent() shape (kind, pos, side,
    // letter, target, value, memberId, name); `match` is the match as shown
    // (its modifiedAt floors the stamp, bc-hlck). Never coalesced: each tap is
    // its own entry (_coalesceTarget names only score and lineup writes).
    recordTap(compID, matchID, ev, password, match) {
        _noteShownMatch(compID, match);
        const base = _revKey(compID, matchID);
        const seq = _nextTapSeq(base);
        const id = `${_eventSession}.${seq}`;
        const seen = Number(match && match.modifiedAt) || 0;
        const stamp = _afterOwnStamp(base, _stampAfterSeen(_serverNowMs(), seen));
        const payload = { ...ev, id, session: _eventSession, seq, stamp };
        const descriptor = {
            compID, matchID, payload, password: password || '', kind: 'evt', terminal: false,
            method: 'POST', url: `/api/competitions/${compID}/matches/${matchID}/score-events`,
            enqueuedAt: Date.now(),
            ...(seen > 0 ? { seenModifiedAt: seen } : {}),
        };
        // Through the one enqueue (_commitEnqueue), the flush kicked in a
        // microtask so the taps of one tick go in one batch.
        _commitEnqueue(base, descriptor, null, { nextTick: true });
        const answer = { id, stamp, seq };
        if (!_lastEnqueuePersisted) answer.persisted = false;
        return answer;
    },
    // The taps this device still holds for a match, in send order (event
    // payloads), for an editor's board and the same-device TV overlay.
    pendingTaps(compID, matchID) {
        return _pendingTapsFor(_revKey(compID, matchID));
    },
    subscribePendingTaps(compID, matchID, fn) {
        return _listenOn(_pendingTapListeners, _revKey(compID, matchID), fn);
    },
    // The newest derived match a score-events answer handed back (broadcast
    // copy with markIds and scoreVersion; kept by the larger scoreVersion).
    latestDerivedMatch(compID, matchID) {
        return _latestDerived.get(_revKey(compID, matchID)) || null;
    },
    subscribeDerivedMatch(compID, matchID, fn) {
        return _listenOn(_derivedListeners, _revKey(compID, matchID), fn);
    },
    // A tap the server did not apply: fn({ id, outcome, reason, message }).
    subscribeTapOutcome(compID, matchID, fn) {
        return _listenOn(_tapOutcomeListeners, _revKey(compID, matchID), fn);
    },
    // tellPendingTapRefused: the board found a pending tap of this device the
    // server will not apply (a tap on a bout the match's copy shows removed,
    // applyPending's BOUT_REMOVED): told once, now, to the match's editor and
    // as one 'not_applied' alert, and not again when the server's answer for
    // that tap arrives (stage 6 review 7). Never waits: told after the render.
    tellPendingTapRefused(compID, matchID, outcome, tap) {
        if (!compID || !matchID || !outcome || !outcome.id || !_claimToldHere(outcome.id)) return;
        // This tab's editor hears it; the operator's alert is raised once per
        // device (the first board to tell it), one alert for the taps told
        // together (F4).
        const first = _told.tell(compID, matchID, outcome.id);
        _queueBoardTold(compID, matchID, outcome, tap, first);
    },
    // noteShownMatch: the match an editor shows, so a held write of it, or of
    // one of its teams, is named by where it is (H5, _whereOf).
    noteShownMatch(compID, match) {
        _noteShownMatch(compID, match);
    },

    heldWriteKeepsFailing(compID, matchID) {
        return API.heldWriteState(compID, matchID).keepsFailing;
    },
    // heldWriteState: both answers the editors' banner reads, from ONE read
    // of the queue (stage 3 review 5, H2: the two asked apart scanned and
    // sorted it at every step of the walk): `keepsFailing`, whether a write
    // of the match or one it waits on keeps failing, and `blocker`, that
    // write when it is another base's (heldWriteBlocker), else null.
    heldWriteState(compID, matchID) {
        const base = _revKey(compID, matchID);
        const index = _queueIndex();
        const own = index.byBase.get(base) || [];
        // The match's own write is the one the server keeps refusing: that
        // is what the banner says, and what its Discard discards.
        if (own.some(([, d]) => _refusedByServer(d))) return { keepsFailing: true, blocker: null };
        // Otherwise the first write of ANOTHER base it waits on that keeps
        // failing, walked through its own entries (one of them on a wait
        // cycle, H1, is passed through to the write of the other match on it).
        const stuck = (d, key) => _baseOf(key) !== base && _keepsFailing(d);
        const walk = _newWalk();
        for (const [key] of own) {
            const found = _blockerOf(key, stuck, walk, index);
            if (found) return { keepsFailing: true, blocker: _heldWriteItem(found, _writeQueue.get(found)) };
        }
        return { keepsFailing: own.some(([, d]) => _keepsFailing(d)), blocker: null };
    },
    // heldWriteBlocker: the write of ANOTHER base this match's writes wait on
    // that the server keeps refusing (stage 3 review 4, G2: a member add a
    // lineup save or a representative pick follows), as a heldWrites() item;
    // null when the match's own writes keep failing (those are its own to
    // say), or when nothing it waits on does. Found through what each entry
    // follows and what its own base sends before it (_blockerOf).
    heldWriteBlocker(compID, matchID) {
        return API.heldWriteState(compID, matchID).blocker;
    },
    discardFailingHeldWrites(compID, matchID) {
        let discarded = 0;
        // What holds the match back from another base (G2), when its own
        // writes are not the ones failing; asked before anything goes.
        const blocker = API.heldWriteBlocker(compID, matchID);
        for (const [key, d] of _entriesFor(_revKey(compID, matchID))) {
            if (_discardIfFailing(key, d)) discarded++;
        }
        if (blocker && _discardIfFailing(blocker.key, _writeQueue.get(blocker.key))) discarded++;
        if (discarded > 0) _afterDiscard();
        return discarded;
    },

    // Every write held on this device, in the order it replays, for the admin
    // topbar's held-writes list (HeldWritesPanel, held_writes_panel.jsx). It is where
    // a held write with no editor to discard it from is reached: a running
    // autosave or a lineup save the server keeps refusing.
    // Each item names what it is (kind, terminal; for a lineup its team and
    // round), whether it keeps failing, and the key discardHeldWrite takes. A
    // write still sending, or only waiting for the connection, is listed
    // without a way to discard it: it will land on its own.
    heldWrites() {
        const all = [..._writeQueue.entries()].filter(([, d]) => !!d).sort(_byEntryOrder);
        return all.map(([key, d]) => _heldWriteItem(key, d));
    },

    // The version banner's Reload stores the outbox once more before the
    // page goes (S2); every entry is stored as it is made anyway.
    persistQueue() {
        return _persistQueue();
    },
    // Discards ONE held write, by the key heldWrites gave, and only while the
    // server keeps refusing it (a write waiting for the connection is never
    // discarded through here). Returns whether it was.
    discardHeldWrite(key) {
        const ok = _discardIfFailing(key, _writeQueue.get(key));
        if (ok) _afterDiscard();
        return ok;
    },

    // mp-gpra (security): clearQueue: drop all queued writes (in-memory + the
    // persisted queue, an older build's key included) and reset sync state. Called on logout /
    // password_reset so a stale plaintext password and any pending writes don't
    // linger in localStorage past credential revocation: bringing the stored queue
    // to the same lifecycle as bc_password. Pending offline writes are discarded:
    // on a password_reset they would 401 on retry anyway, and logout is explicit.
    /**
     * bc-qttl: how much unconfirmed operator work the queue is still holding.
     * Read by the logout gate, which must not let a sign-out silently destroy
     * results that never reached the server.
     * @returns {{total: number, terminal: number, authBlocked: number}}
     */
    unsentWrites() {
        return _unsentCounts();
    },

    // R4: the court moves and time changes this device makes, told in the tick
    // they are made and again when a refusal puts the old value back.
    // The status move this device made to a match that the court console
    // shows over the server's copy (_ownStatusMove, M4), and a subscription
    // told whenever one changes (queued, answered, refused or discarded).
    ownStatusMove(compID, matchID) {
        return _ownStatusMove(compID, matchID);
    },
    subscribeOwnStatusMoves(fn) {
        return _subscribeOwnStatusMoves(fn);
    },
    // The feed shows the move, or a later change: the answered record goes.
    forgetOwnStatusMove(compID, matchID) {
        _forgetOwnStatusMove(compID, matchID);
    },
    // The feed no longer holds the match (_forgetOwnStatusMovesNotIn).
    forgetOwnStatusMovesNotIn(matches) {
        _forgetOwnStatusMovesNotIn(matches);
    },
    subscribeLocalMatchPatch(cb) {
        return subscribeLocalMatchPatch(cb);
    },
    // bc-obox C1: a check-in this device makes, told in the tick it is made,
    // and again when a refusal or a discard puts back what the row had or a
    // superseded answer puts the participant as stored: { type:
    // 'checkin_updated', local: true, data: { competitionId, participants:
    // [{ id, checkedIn }] } }.
    subscribeLocalParticipantPatch(cb) {
        return subscribeLocalParticipantPatch(cb);
    },
    // Whether a court move of this match is still queued here: a feed that
    // shows the match on that court (the overlay above) is not yet the server's.
    courtMoveQueued(compID, matchID) {
        return 'court' in _pendingMatchFields(compID, matchID);
    },
    // Whether a move that stops this running match is still queued here (one
    // already out included): the server stops it with that move, so a later
    // move finds it waiting, and the copy shown still says running.
    stoppingCourtMoveQueued(compID, matchID) {
        return _entriesFor(_revKey(compID, matchID))
            .some(([, d]) => d.kind === 'court' && d.payload && d.payload.stopsRunning === true);
    },

    /**
     * bc-qttl: un-park writes held on a 401 and retry them with a freshly
     * authenticated credential, and give that credential to every other
     * queued write still carrying the password the server refused.
     *
     * Necessary because each descriptor captures the password it was enqueued
     * with, so a parked entry retried as-is can only reproduce the same 401
     * forever. Re-stamping is the whole point: it is what turns "parked" into a
     * state the operator can actually clear, and therefore what makes parking a
     * better answer than dropping the write.
     *
     * The rejection counter is reset too: those failures were against the old
     * credential and say nothing about whether the new one will be refused.
     * @param {string} password the newly authenticated password
     * @returns {number} how many parked writes were resumed
     */
    resumeAfterAuth(password) {
        // The passwords the server refused. A write queued after the parked
        // one, while it was offline say, carries the same stale password
        // without having been answered yet: it gets the new one too, or it
        // would meet the same 401 and ask for a second sign-in. An EMPTY
        // password is never treated as stale on an unparked entry: a self-run
        // participant's write carries none and must never be sent with the
        // organiser's (a parked entry has its password blanked and resumes on
        // its own flag anyway).
        const stale = new Set();
        for (const d of _writeQueue.values()) {
            if (d && d.authBlocked && d.password) stale.add(d.password);
        }
        let resumed = 0;
        for (const d of _writeQueue.values()) {
            if (!d) continue;
            const parked = !!d.authBlocked;
            if (!parked && !(password && d.password && stale.has(d.password))) continue;
            d.authBlocked = false;
            if (password) d.password = password;
            d.attempts = 0;
            if (parked) resumed++;
        }
        // The members reads go again, with the new password (_resumeMintedReads).
        _resumeMintedReads(password);
        if (resumed > 0) {
            _persistQueue();
            _resetFlushBackoff();
            _recomputeSyncStatus();
            _flushQueue();
        }
        return resumed;
    },

    /**
     * bc-qttl: the credential was revoked out from under this tablet (a
     * password_reset broadcast by another admin). Scrub the dead password from
     * every queued write and PARK them, instead of destroying results nobody
     * here chose to discard.
     *
     * This keeps both properties that matter. The security intent of calling
     * clearQueue() on this path was that a stale plaintext password must not
     * linger in localStorage past revocation, and emptying the password field
     * achieves exactly that: the password was the secret, the operator's match
     * results never were. The writes stay queued and unsendable (authBlocked)
     * until resumeAfterAuth() stamps the new credential, which is precisely the
     * state a 401 would have put them in anyway.
     *
     * The interactive logout path still uses clearQueue(), because there the
     * operator is asked first and explicitly chooses to discard.
     * @returns {number} how many writes were parked
     */
    parkQueueForReauth() {
        // Bump the generation FIRST so an in-flight _flushQueue loop cannot send
        // the rest of its snapshot with the now-revoked password.
        _bumpQueueGen();
        let parked = 0;
        for (const d of _writeQueue.values()) {
            if (!d) continue;
            d.password = '';
            d.authBlocked = true;
            parked++;
        }
        _resetFlushBackoff();
        _pauseMintedReads();
        _setOffline(false);
        // Every stored entry too, whether or not this tab holds any.
        _parkStoredEntries();
        // The landed-tap record and the copies seen stay: the same operator
        // signs in again, and is told of a tap turned away meanwhile (stage 6
        // review 8, m10). The board's notices stay for the taps still queued
        // here, to be answered after the sign-in; the rest go (m12).
        _pruneToldOnBoard();
        if (parked > 0) {
            _persistQueue();
            _notifyQueueAlert({ kind: 'auth_required', count: parked, terminalCount: _countTerminalQueued() });
        }
        _recomputeSyncStatus();
        return parked;
    },

    clearQueue() {
        // bc-qttl: announce what is being destroyed. This is a credential
        // revocation, so these writes genuinely cannot be delivered (the password
        // they carry is dead), but "cannot be delivered" is never a licence to
        // discard an operator's results in silence. The interactive logout path
        // asks BEFORE reaching here; this covers the involuntary route, a
        // password_reset broadcast from another admin, where nobody at this
        // tablet chose anything.
        const discarding = _countTerminalQueued();
        const discardingTotal = _writeQueue.size;
        _forgetTurnedAwayAndTold();
        _clearOwnStatusAnswers();
        if (discardingTotal > 0) {
            _notifyQueueAlert({ kind: 'discarded', count: discardingTotal, terminalCount: discarding });
        }
        // Bump the generation FIRST so an in-flight _flushQueue loop (mid-await on
        // a network write) aborts instead of sending the rest of its snapshot with
        // the now-revoked password.
        _bumpQueueGen();
        _wipeQueue();
        for (const k of _entryHooks.keys()) _settleKey(k, { value: { discarded: true } });
        for (const b of _pendingTapListeners.keys()) _publishPendingTaps(b);
        try { localStorage.removeItem(QUEUE_STORAGE_KEY); } catch (_e) { /* ignore */ }
        // An older build's queue carries the same revoked password (F4).
        try { localStorage.removeItem(LEGACY_QUEUE_STORAGE_KEY); } catch (_e) { /* ignore */ }
        _wipeMintedRecords();
        // The copies score-events answers handed back go with the session
        // they were answered to (X4).
        _forgetDerivedCopies();
        // The landed-tap record goes with the operator: this tab's taps and
        // those of tabs no longer alive (stage 6 reviews 7 and 8).
        _forgetLandedTaps();
        _resetFlushBackoff();
        _setOffline(false);
        _recomputeSyncStatus();
    },
};

export {
    API, START_CHECK_TIMEOUT_MS, subscribeSyncStatus, subscribeTerminalWriteFailed, subscribeQueueAlert,
    subscribeUnsentWrites,
    enqueueRunningWrite,
    writeDidNotLand, writeWasSuperseded, writeWasRefusedForClock,
    SUPERSEDED_REASON, SUPERSEDED_ADVICE,
    CLOCK_SKEW_REASON_TEXT, CLOCK_SKEW_ADVICE,
    // Exported for tests only: the take-the-older rule is a safety property, and
    // reaching it through a full offline/refuse/reconnect fixture would test the
    // queue rather than the arithmetic. No production consumer outside this file.
    _restampFor,
    // Exported for tests only: whether a tap makes the status-move index again
    // shows only in its identity (stage 6 review 10, m5).
    _statusEntriesByBase,
    normalizeViewerCompItem,
};

// S2: a new build reloads the page only once the outbox is idle, and the
// queue is persisted first.
setOutboxIdleProbe(_outboxIdle);
setBeforeReload(() => { _persistQueue(); });

if (typeof window !== 'undefined') {
    window.API = API;
    // C2: expose sync-status pub/sub so components loaded as window.* globals
    // (admin_scoring_modal.jsx, etc.) can subscribe without an ES import.
    window.subscribeSyncStatus = subscribeSyncStatus;
    // bc-offl: the held-writes count, for the admin topbar (admin_shell.jsx is
    // script-tagged and cannot ES-import this script-tagged module).
    window.subscribeUnsentWrites = subscribeUnsentWrites;
    // mp-gpra: terminal-write failure pub/sub: lets the score editor show an
    // explicit "not saved" state when a queued terminal write is permanently dropped.
    window.subscribeTerminalWriteFailed = subscribeTerminalWriteFailed;
    window.subscribeQueueAlert = subscribeQueueAlert;
    // No `window` mirror for the not-landed predicates or their banner copy:
    // every consumer imports write_result.jsx directly. That module is
    // import-only (never script-tagged), so even a script-tagged surface can
    // import it without the double-module-eval hazard the mirrors existed to
    // dodge.
    // mp-gpra: reconnectEvents and hasPendingTerminalWrite are on window.API
    // (via the API object above): no separate window assignments needed.
}
