// The server-sent event stream: one shared, ref-counted connection, its
// watchdog and reconnect backoff.
//
// A piece of the outbox that api_client.jsx boots (bc-jsxs). Declaration-only: at
// load it runs nothing but literals, its own earlier declarations and calls into
// leaves (api_client_ids.jsx and the modules outside this family), so the import
// cycles between the pieces are safe (check-load-cycles.mjs). The load-time
// statements, the API object and the window mirrors live in api_client.jsx; never
// import api_client.jsx from here (it would run the outbox twice).

import {
    _learnServerClockOffset, _relearnClockThrottled, _serverNowMs, CLOCK_TRIPWIRE_MS,
} from './api_client_clock.jsx';
// bc-evts S2: the build this page runs against the server's, and the reload rule.
import { noteServerBuild } from './app_version.jsx';
import { _tellTurnedAway } from './api_client_taps.jsx';
import { _forgetShownMatches, _forgetAllShownMatches } from './api_client_status.jsx';
import { _fetchJson, readHeaders, setReadAudience, noteRefusedCredential } from './api_client_requests.jsx';

// ---------------------------------------------------------------------------
// Shared ref-counted SSE singleton
// ---------------------------------------------------------------------------
// All subscribeToEvents callers share ONE EventSource connection. A Set of
// subscriber records ({callback, onStatus}) is maintained; when the last
// subscriber unsubscribes the source is closed and nulled out.
//
// F2: SSE silence watchdog: if no message (including heartbeat) arrives for
// 35 s (>2× the server's 15 s heartbeat interval, with margin for slow courts)
// the connection is assumed stale and is torn down for a fresh reconnect.
// _lastActivityAt is updated on every onopen and every onmessage.
//
// F3: Reconnect uses exponential backoff + jitter (1–30 s) instead of a fixed
// 5 s delay. _reconnectAttempt increments on each onerror and resets to 0 in
// onopen, so a stable connection always reconnects fast after the first error.
// ---------------------------------------------------------------------------
/** @type {EventSource|null} */
let _sharedSource = null;
/** @type {ReturnType<typeof setTimeout>|null} */
let _retryTimer = null;
/** @type {Set<{callback: Function, onStatus: Function|undefined}>} */
const _subscribers = new Set();

// The last event id the stream delivered (the server's seq, from its `id:`
// line). A reconnect opens a NEW EventSource, which sends no Last-Event-ID
// header, so the id rides on the URL and the hub replays every event after
// it (or answers resync_required when it cannot). A resync_required with no
// id (a restarted server that has sent nothing yet) resets it, since the
// browser keeps the old id when a frame carries none.
let _lastEventId = '';
function _noteEventId(id, parsed) {
    if (parsed && parsed.type === 'resync_required' && !(parsed.seq > 0)) {
        _lastEventId = '';
        return;
    }
    if (id) _lastEventId = id;
}
function _eventsUrl(ticket) {
    const params = [];
    if (ticket) params.push(`ticket=${encodeURIComponent(ticket)}`);
    if (_lastEventId) params.push(`lastEventId=${encodeURIComponent(_lastEventId)}`);
    return params.length ? `/api/events?${params.join('&')}` : '/api/events';
}

// bc-hnam: the stream is masked per subscriber, by the credential its
// connection was opened with. An EventSource sends no headers, so a page that
// holds a credential (the organiser's password on an admin route, reveal keys
// on a public page; readHeaders decides) first asks POST /api/events/ticket
// with it as headers and opens /api/events?ticket=..., the secret never in a
// URL. A ticket is good for ONE connection within a minute, so EVERY connect
// asks a new one: the first, each reconnect after an error, the silence
// watchdog's, a resume's. A page with no credential opens the public stream
// with no ticket. A ticket the server will not give is a failed connect,
// retried with the backoff, and NEVER a fall back to the public stream: a
// signed-in console must not land on a masked stream without a word. The one
// exception is the server saying no ticket is needed (event_ticket_not_needed:
// reveal keys none of which is well formed, or a self-run tournament, which
// hides no names), where the public stream is what the page is owed anyway;
// never for a refused password (401).
//
// _ticketGen tells a ticket's answer whether it is still wanted: a reconnect,
// the last unsubscribe or a new credential bumps it, and an answer for an
// older generation opens nothing.
let _ticketGen = 0;
let _ticketPending = false;
// A ticket was refused for the organiser's password: nothing is retried until
// the audience changes (setAudienceAndReconnect).
let _credentialRefused = false;
const EVENT_TICKET_NOT_NEEDED = 'event_ticket_not_needed';

function _cancelTicket() {
    _ticketGen++;
    _ticketPending = false;
}

// F2: timestamp of the last received SSE activity (open or message).
let _lastActivityAt = 0;
/** @type {ReturnType<typeof setInterval>|null} */
let _watchdogTimer = null;
const SSE_WATCHDOG_INTERVAL_MS = 10000;  // poll every 10 s
const SSE_SILENCE_THRESHOLD_MS = 35000; // reconnect if silent for >35 s

// F3: reconnect backoff state.
let _reconnectAttempt = 0;

/** Compute the next reconnect delay: exponential backoff 1–30 s plus up to 1 s of jitter. */
function _reconnectDelay() {
    return Math.min(30000, 1000 * Math.pow(2, _reconnectAttempt)) + Math.random() * 1000;
}

/** Arm the silence watchdog. Idempotent: clears any prior interval first. */
function _armWatchdog() {
    _disarmWatchdog();
    _watchdogTimer = setInterval(() => {
        // If the source is gone already (e.g. onerror cleared it) do nothing: 
        // the reconnect timer will re-arm the watchdog when the new source opens.
        if (!_sharedSource) return;
        if (Date.now() - _lastActivityAt > SSE_SILENCE_THRESHOLD_MS) {
            // Silence detected: tear down the stale source and reconnect.
            _sharedSource.close();
            _sharedSource = null;
            _fanOutStatus('error');
            if (_subscribers.size > 0) {
                if (_retryTimer) { clearTimeout(_retryTimer); }
                _retryTimer = setTimeout(_ensureConnected, _reconnectDelay());
                _reconnectAttempt++;
            }
            // Watchdog re-arms itself after _ensureConnected → onopen.
            _disarmWatchdog();
        }
    }, SSE_WATCHDOG_INTERVAL_MS);
}

/** Disarm the silence watchdog (call when no subscribers remain). */
function _disarmWatchdog() {
    if (_watchdogTimer !== null) { clearInterval(_watchdogTimer); _watchdogTimer = null; }
}

function _emitStatus(sub, s) {
    if (typeof sub.onStatus === 'function') {
        try { sub.onStatus(s); } catch (err) { console.error('SSE status callback failed:', err); }
    }
}

function _fanOutStatus(s) {
    for (const sub of _subscribers) {
        _emitStatus(sub, s);
    }
}

function _ensureConnected() {
    // Guard: the retry timer may fire after all subscribers have unsubscribed
    // (clearTimeout only cancels a pending callback: it cannot stop one that is
    // already queued in the event loop). No-op when no one is listening so we
    // don't open a zombie EventSource that can never be closed.
    if (_subscribers.size === 0) return;
    // Whoever calls this is establishing the connection NOW, so cancel any
    // pending reconnect timer: otherwise a retry queued by a previous onerror
    // can fire later and open a second EventSource (a leaked SSE connection).
    if (_retryTimer) {
        clearTimeout(_retryTimer);
        _retryTimer = null;
    }
    // Guard against double-connect: only one shared source may exist at a time,
    // and a ticket already asked for is that source on its way.
    if (_sharedSource || _ticketPending) return;
    const headers = readHeaders();
    if (Object.keys(headers).length === 0) {
        _openSource(_eventsUrl());
        return;
    }
    const gen = ++_ticketGen;
    _ticketPending = true;
    _credentialRefused = false;
    _fetchJson('/api/events/ticket', { method: 'POST', headers }).then(({ res, body }) => {
        if (gen !== _ticketGen) return;
        _ticketPending = false;
        if (res.ok && body && typeof body.ticket === 'string' && body.ticket) {
            _openSource(_eventsUrl(body.ticket));
            return;
        }
        if (res.status === 400 && body && body.code === EVENT_TICKET_NOT_NEEDED) {
            _openSource(_eventsUrl());
            return;
        }
        // A ticket refused for the organiser's password is a stale credential,
        // which no retry can mend: the page is told (it drops the password and
        // asks for it again) and the change of audience reopens the stream, so
        // no ticket is asked for again with the same password.
        if (noteRefusedCredential(res.status)) {
            _credentialRefused = true;
            _fanOutStatus('error');
            return;
        }
        throw new Error(`no event-stream ticket (HTTP ${res.status})`);
    }).catch((err) => {
        if (gen !== _ticketGen) return;
        _ticketPending = false;
        console.warn('[sse] could not open the event stream:', err);
        _connectFailed();
    });
}

// _connectFailed: a connect that never got a source (its ticket was refused or
// never answered). Told and retried exactly as a source that errored is.
function _connectFailed() {
    _disarmWatchdog();
    _fanOutStatus('error');
    if (_subscribers.size > 0) {
        if (_retryTimer) clearTimeout(_retryTimer);
        _retryTimer = setTimeout(_ensureConnected, _reconnectDelay());
        _reconnectAttempt++;
    }
}

// _openSource opens the shared EventSource on `url` and binds its handlers.
function _openSource(url) {
    // Bind handlers to THIS instance via a local const. Each handler ignores
    // events from a superseded source (`source !== _sharedSource`) so a stale
    // instance: should one ever fire after being replaced: can't close the
    // live connection or fan out a false status/message to current subscribers.
    const source = new EventSource(url);
    _sharedSource = source;

    // F2: arm the watchdog at connect time, not just in onopen: a CONNECTING
    // stall (half-open / captive / saturated WiFi where onopen, and sometimes
    // even onerror, never fire) is the exact silent-staleness this watchdog must
    // catch. Stamp activity now so the 35s timer measures from the attempt.
    _lastActivityAt = Date.now();
    _armWatchdog();

    source.onopen = () => {
        if (source !== _sharedSource) return;
        // F2: record activity and arm the watchdog on every (re)connect.
        _lastActivityAt = Date.now();
        _armWatchdog();
        // F3: a successful open resets the backoff counter so the next error
        // reconnects fast (starting from 1 s) rather than inheriting a long delay.
        _reconnectAttempt = 0;
        // mp-y3nk: (re)learn the server-clock offset on every connect so writes
        // stamped after a reconnect stay in the server's frame.
        _learnServerClockOffset();
        _fanOutStatus('open');
    };

    source.onmessage = (event) => {
        if (source !== _sharedSource) return;
        // F2: every message (including heartbeat {type:"heartbeat"}) resets the
        // silence watchdog. No seq field on heartbeats: just update the timestamp.
        _lastActivityAt = Date.now();
        let parsed;
        try {
            parsed = JSON.parse(event.data);
        } catch (err) {
            console.error('Error parsing SSE event:', err);
            return;
        }
        // bc-cse: heartbeat clock TRIPWIRE. The 15s heartbeat carries the
        // server's nowMs, which makes it a free, continuous check on whether this
        // device is still in the server's frame - so a clock that jumps mid-event
        // (an NTP correction, an operator "fixing" the tablet's time) is caught
        // within one heartbeat instead of at the next refused result.
        //
        // It is a DETECTOR, NEVER A SOURCE. nowMs is never assigned into the
        // offset, because a one-way push carries no round-trip to correct for:
        // the frame arrives some unknown latency after it was stamped, and on the
        // congested court wifi this exists for, that latency is exactly when it is
        // largest. Applying it directly would push this device's frame BACKWARDS
        // by that latency - manufacturing the behind-the-clock skew the whole
        // mechanism is here to prevent. So the tripwire only ever asks for a real
        // GET /api/time, whose RTT-halving math is the one thing entitled to move
        // the offset. A spurious trip costs one throttled GET; a corrupted offset
        // costs the operator's data.
        //
        // Threshold sits below the server's 5s refusal margin (see
        // CLOCK_TRIPWIRE_MS) so the heal happens before writes start bouncing.
        if (parsed && parsed.type === 'heartbeat' && typeof parsed.nowMs === 'number') {
            if (Math.abs(parsed.nowMs - _serverNowMs()) > CLOCK_TRIPWIRE_MS) {
                _relearnClockThrottled();
            }
        }
        // S2: the stream's first frame names the server's build.
        if (parsed && parsed.type === 'heartbeat' && parsed.hello) noteServerBuild(parsed.build);
        if (parsed && (parsed.type === 'draw_generated' || parsed.type === 'draw_discarded')) {
            _forgetShownMatches(parsed.data && parsed.data.competitionId);
        }
        if (parsed && (parsed.type === 'tournament_updated' || parsed.type === 'password_reset')) _forgetAllShownMatches();
        // A push of a match names the taps the server turned away since their
        // answer: this device's are told (_tellTurnedAway).
        if (parsed && parsed.type === 'match_updated' && parsed.data && parsed.data.result) {
            const r = parsed.data.result;
            _tellTurnedAway(parsed.data.competitionId, parsed.data.matchId || r.id, r);
        }
        _noteEventId(event.lastEventId, parsed);
        for (const sub of _subscribers) {
            try { sub.callback(parsed); } catch (err) { console.error('SSE callback failed:', err); }
        }
    };

    source.onerror = () => {
        if (source !== _sharedSource) return;
        source.close();
        _sharedSource = null;
        // F2: disarm the watchdog: the source is gone; a new one will re-arm it.
        _disarmWatchdog();
        _fanOutStatus('error');
        if (_subscribers.size > 0) {
            // Clear any prior timer before re-arming so repeated error events
            // can't queue multiple concurrent reconnects.
            if (_retryTimer) clearTimeout(_retryTimer);
            // F3: exponential backoff + jitter instead of fixed 5 s.
            _retryTimer = setTimeout(_ensureConnected, _reconnectDelay());
            _reconnectAttempt++;
        }
    };
}

// _subscribeToEvents (API.subscribeToEvents): subscribe to the server's SSE
// event stream. `callback` is fired
// for each parsed event. `onStatus` (optional) is fired with
// 'open' when the EventSource transitions to open and 'error' when
// it transitions away: used by the display surfaces (FR-011
// scenario 4 / T063) to render a reconnect indicator when the
// browser is between connections. Existing call sites that omit
// the second arg are unaffected.
//
// All callers share ONE module-level EventSource (ref-counted). The
// source is opened on the first subscribe and closed when the last
// subscriber unsubscribes. A subscriber joining an already-OPEN source
// receives an immediate onStatus('open') replay so it starts in the
// correct connected state without waiting for the next onopen event.
// The SSE state is written by the SSE code alone (bc-jsxs).
export function _subscribeToEvents(callback, onStatus) {
    const record = { callback, onStatus };
    _subscribers.add(record);

    if (_sharedSource === null) {
        // No source yet: open one. The new subscriber keeps its optimistic
        // default until this source's onopen/onerror fires.
        _ensureConnected();
    } else if (_sharedSource.readyState === EventSource.OPEN) {
        // Source is already connected: replay 'open' to this subscriber.
        _emitStatus(record, 'open');
    }
    // Else the source exists but is still CONNECTING (a non-null source is
    // only ever CONNECTING or OPEN: we null it on close). Replay NOTHING
    // and let the bound onopen/onerror fan-out deliver the first
    // authoritative status, exactly as the first subscriber experiences.
    // Replaying 'error' here would flash a false "Reconnecting…" on admin
    // load, where AdminTopbar/AdminDashboard subscribe mid-handshake.

    // Idempotent unsubscribe: guard with a `done` flag so double-calling
    // never double-decrements the ref count.
    let done = false;
    return () => {
        if (done) return;
        done = true;
        _subscribers.delete(record);
        if (_subscribers.size === 0) {
            clearTimeout(_retryTimer);
            _retryTimer = null;
            // F2: disarm the watchdog when no subscribers remain: no point
            // in polling for silence when no one is listening.
            _disarmWatchdog();
            // A ticket still being asked for opens nothing now.
            _cancelTicket();
            if (_sharedSource) {
                _sharedSource.close();
                _sharedSource = null;
            }
        }
    };
}

// -------------------------------------------------------------------------
// F2/F3: _reconnectEvents (API.reconnectEvents): force a fresh SSE connection.
// Closes the current shared source (if any), resets the reconnect backoff
// counter, and calls _ensureConnected(). Safe to call repeatedly: the
// double-connect guard in _ensureConnected() prevents duplicate sources.
// Exposed as API.reconnectEvents / window.API.reconnectEvents so other
// modules loaded as window globals can reach it without an ES import.
// -------------------------------------------------------------------------
export function _reconnectEvents() {
    if (_sharedSource) {
        _sharedSource.close();
        _sharedSource = null;
        _disarmWatchdog();
    }
    // A ticket asked for with the old credential opens nothing: the connect
    // below asks a new one (bc-hnam).
    _cancelTicket();
    if (_retryTimer) { clearTimeout(_retryTimer); _retryTimer = null; }
    _reconnectAttempt = 0;
    _ensureConnected();
}

// The two event-stream members of the API object: a piece api_client.jsx
// spreads into `API` beside the other domain pieces (api_client_tournament.jsx
// and the rest).
export const eventsApi = {
    // Subscribe to the server's SSE event stream: one shared, ref-counted
    // EventSource (_subscribeToEvents).
    subscribeToEvents(callback, onStatus) {
        return _subscribeToEvents(callback, onStatus);
    },
    // F2/F3: reconnectEvents: force a fresh SSE connection (_reconnectEvents).
    reconnectEvents() {
        _reconnectEvents();
    },
};

// bc-hnam: setAudienceAndReconnect (app.jsx, as the route, the sign-in or the
// reveal keys held change): the credential this page reads with from now on
// (api_client_requests.jsx setReadAudience), and, when it changed while the
// stream is open or opening, a new connection with a ticket for it, so the
// stream is never left masked, or unmasked, for the old audience. True when
// the credential changed.
export function setAudienceAndReconnect(kind, password) {
    const changed = setReadAudience(kind, password);
    if (changed && (_sharedSource || _ticketPending || _retryTimer || _credentialRefused)) _reconnectEvents();
    return changed;
}
