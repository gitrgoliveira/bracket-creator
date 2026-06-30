// court_bridge.jsx: BroadcastChannel-based court-local data hub.
//
// Phase 2 of the mp-gpra flaky-wifi hardening initiative (mp-9ukk). When
// the server link is down, the operator tab becomes the display tab's local
// data hub: match patches are broadcast on confirm or enqueue, and the
// display tab can bootstrap from the operator tab's competition snapshot on
// cold load.
//
// ## Message schema (versioned)
//
//   { v: 1, type: 'patch'|'snapshot-req'|'snapshot',
//     origin: <string tabId>,
//     court:  <string, e.g. "A">,
//     compId: <string>,          // patch + snapshot; empty for snapshot-req
//     payload: <object|null> }   // match-patch event data | court slice of competitions
//
// ## Self-echo suppression
//
// Each tab generates a random `_tabId` on first import. Outbound messages
// carry `origin: _tabId`. Inbound messages where `msg.origin === _tabId`
// are silently dropped so no tab reacts to its own broadcasts.
//
// ## Snapshot handshake
//
// On load, the display tab publishes `snapshot-req` for its court. Any
// operator tab that has registered a snapshot provider via
// `setSnapshotProvider(fn)` replies with a `snapshot` containing that
// court's competitions slice. If multiple operator tabs answer, the display
// uses the first reply (the update is idempotent: same court data).
//
// ## Graceful degrade
//
// When BroadcastChannel is unavailable (very old browser) openBridge()
// returns a no-op handle. Behavior degrades to today's SSE-only flow with
// no regression.

import { applyPatch as _applyPatch } from './patch.jsx';

// freshnessMs: the amber->red dot threshold.
// 20 s sits below the 35 s SSE silence watchdog and above the 15 s SSE
// heartbeat, so a quiet (but open) operator tab does NOT flap to stale
// between matches, yet a crashed/closed operator tab becomes stale promptly.
// Exported as a named constant so tests and callers share one value.
export const freshnessMs = 20000;

// BroadcastChannel name. All same-origin tabs share one channel; messages
// are scoped by the `court` field inside each message payload.
const CHANNEL_NAME = 'bc-court-hub-v1';

// Module-level tab identity. Stable for the lifetime of this module instance
// (one per tab). crypto.randomUUID is available in modern browsers and jsdom.
export const _tabId = (typeof crypto !== 'undefined' && crypto.randomUUID)
    ? crypto.randomUUID()
    : `t${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;

// -----------------------------------------------------------------------
// deriveLinkState: pure exported function so vitest can test it without
// mounting any component.
//
// Returns:
//   'connected' : SSE to server is up (sseConnected === true)
//   'local'     : server down, but operator-tab broadcast is fresh
//                 (lastBroadcastAt within freshnessMs of now)
//   'stale'     : no server AND no recent operator broadcast
//
// `fm` defaults to the module constant so callers can pass a custom value
// for testing without altering the module-level default.
// -----------------------------------------------------------------------
export function deriveLinkState({ sseConnected, lastBroadcastAt, now, freshnessMs: fm = freshnessMs }) {
    if (sseConnected) return 'connected';
    if (lastBroadcastAt !== null && (now - lastBroadcastAt) < fm) return 'local';
    return 'stale';
}

// -----------------------------------------------------------------------
// applyPatchToTree: tree-level adapter.
//
// Takes the full in-memory `tournament` (with `tournament.competitions[]`)
// plus one broadcast `msg` (type: 'patch', compId, payload), locates the
// competition by `compId`, delegates the single-competition merge to the
// existing `applyPatch` from patch.jsx, and returns a new tournament object.
//
// Only the touched competition changes reference; all other competitions
// and the surrounding tournament fields are preserved by shallow spread.
// Returns the original `tournament` reference when nothing matched.
// -----------------------------------------------------------------------
export function applyPatchToTree(tournament, msg) {
    // A patch with no payload is a no-op: bail before the findIndex scan and
    // before _applyPatch can dereference a missing payload.
    if (!tournament || !msg || msg.type !== 'patch' || !msg.payload) return tournament;
    const competitions = tournament.competitions;
    if (!competitions || competitions.length === 0) return tournament;

    const compId = msg.compId;
    const idx = competitions.findIndex(c => {
        // competitions[] entries can have id at different depths depending
        // on whether they come from the viewer aggregate or the detail endpoint.
        return c.id === compId
            || (c.config && (c.config.id === compId || c.config.Id === compId));
    });
    if (idx === -1) return tournament;

    const prevComp = competitions[idx];
    // Wrap the broadcast payload in a synthetic SSE match_updated envelope
    // so applyPatch (which expects {type, data}) can process it directly.
    const syntheticEvent = {
        type: 'match_updated',
        data: msg.payload,
    };
    // A malformed payload (e.g. results:[null]) can make _applyPatch throw.
    // This runs inside a React state updater (deferred, outside any handler
    // try/catch), so a throw here would crash the display board into the error
    // boundary. Absorb it: a bad patch leaves the tree unchanged.
    let nextComp;
    try {
        nextComp = _applyPatch(prevComp, syntheticEvent);
    } catch (err) {
        console.error('court_bridge: applyPatch failed for a malformed payload:', err);
        return tournament;
    }
    if (nextComp === prevComp) return tournament;

    const nextCompetitions = competitions.slice();
    nextCompetitions[idx] = nextComp;
    return { ...tournament, competitions: nextCompetitions };
}

// -----------------------------------------------------------------------
// Module-level recency tracker. Shared across all bridge instances in this
// tab (there should only ever be one open bridge per tab in normal use).
// Updated on every outbound patch/snapshot AND every inbound patch/snapshot,
// so the display tab's recency clock also advances when it receives a reply.
// -----------------------------------------------------------------------
let _lastBroadcastAt = null;

// -----------------------------------------------------------------------
// Court-scope filter for inbound recency tracking (FIX 2).
//
// When a display court is set, only patches/snapshots for that court
// advance the recency clock. This prevents a court-B patch from making
// the court-A display dot falsely show 'local'.
// When null (operator tabs, no specific display court), all inbound
// messages advance the clock.
// -----------------------------------------------------------------------
let _displayCourt = null;

export function setDisplayCourt(c) {
    const next = c || null;
    // Reset the recency clock when the court actually changes so the dot does
    // not inherit the previous court's freshness: after a ?court= switch the
    // new court starts 'stale' until its own operator broadcasts.
    if (_displayCourt !== next) _lastBroadcastAt = null;
    _displayCourt = next;
}

// getLastBroadcastAt: read the module-level recency timestamp.
// Called by app.jsx in its linkState-derivation tick.
export function getLastBroadcastAt() {
    return _lastBroadcastAt;
}

// -----------------------------------------------------------------------
// Module-level snapshot provider registry.
//
// The operator tab calls setSnapshotProvider(fn) after loading its
// competition data. When a snapshot-req arrives, openBridge replies
// automatically using this provider. Only one provider is registered at a
// time; re-registering replaces the previous one (correct because one
// operator tab drives one court).
//
// fn signature: (court: string) => competitions[] | null
//   Return null when the tab has no data for the requested court.
// -----------------------------------------------------------------------
let _snapshotProvider = null;

export function setSnapshotProvider(fn) {
    _snapshotProvider = typeof fn === 'function' ? fn : null;
}

// Testing only: reset all module-level state so test suites that assert on
// _lastBroadcastAt, _displayCourt, or _snapshotProvider do not depend on
// test-execution order. Call in beforeEach of any suite that touches these.
export function _resetModuleStateForTests() {
    _lastBroadcastAt = null;
    _displayCourt = null;
    _snapshotProvider = null;
}

// -----------------------------------------------------------------------
// openBridge: create a BroadcastChannel handle.
//
// Returns a no-op object when BroadcastChannel is unavailable OR when
// constructing it throws (e.g. a SecurityError in a sandboxed/blocked
// context), so all callers work without guards. This must not throw: the
// module-level `bridge` singleton is created at import time, so a throw here
// would crash the whole app instead of degrading to the SSE-only path.
// -----------------------------------------------------------------------
const _noopBridge = {
    publish: () => {},
    onMessage: () => () => {},
    close: () => {},
};

export function openBridge() {
    if (typeof BroadcastChannel === 'undefined') {
        return _noopBridge;
    }

    let channel;
    try {
        channel = new BroadcastChannel(CHANNEL_NAME);
    } catch (err) {
        console.error('court_bridge: BroadcastChannel unavailable, degrading to SSE-only:', err);
        return _noopBridge;
    }
    const handlers = new Set();

    channel.onmessage = (evt) => {
        const msg = evt.data;
        if (!msg || msg.v !== 1) return;
        // Self-echo suppression.
        if (msg.origin === _tabId) return;

        // Update recency tracker on inbound patch or snapshot data.
        // When a display court is set, only messages for that court advance
        // the clock; when null (operator tabs) all inbound messages advance it.
        if ((msg.type === 'patch' || msg.type === 'snapshot') &&
            (!_displayCourt || msg.court === _displayCourt)) {
            _lastBroadcastAt = Date.now();
        }

        // Automatic snapshot-req responder: if this tab has a provider for
        // the requested court, reply with the court's competitions slice.
        if (msg.type === 'snapshot-req' && _snapshotProvider) {
            const court = msg.court || '';
            // One try/catch covers both the provider call and postMessage: either
            // failing just logs and continues (the requester re-requests on its tick).
            try {
                const slice = _snapshotProvider(court);
                if (slice && slice.length > 0) {
                    // Reply with one aggregate snapshot (compId empty, payload = the
                    // court's competitions slice) so the display bootstraps its whole
                    // court view in one message.
                    channel.postMessage({
                        v: 1,
                        type: 'snapshot',
                        origin: _tabId,
                        court,
                        compId: '',
                        payload: slice,
                    });
                    _lastBroadcastAt = Date.now();
                }
            } catch (err) {
                console.error('court_bridge: snapshot-req responder failed:', err);
            }
            return; // snapshot-req is fully handled here; do not forward to subscriber handlers
        }

        for (const h of handlers) {
            try { h(msg); } catch (err) { console.error('court_bridge handler error:', err); }
        }
    };

    // Surface structured-clone deserialization failures (otherwise the browser
    // swallows them silently) so a bad cross-tab message is visible in DevTools.
    channel.onmessageerror = (evt) => {
        console.error('court_bridge: message deserialization error:', evt);
    };

    const bridge = {
        // publish: send a versioned message on the channel.
        //   type   : 'patch' | 'snapshot-req' | 'snapshot'
        //   court  : court label e.g. "A"
        //   compId : competition id (may be '' for snapshot-req)
        //   payload: message body
        publish(type, court, compId, payload) {
            try {
                channel.postMessage({
                    v: 1,
                    type,
                    origin: _tabId,
                    court: court || '',
                    compId: compId || '',
                    payload: payload || null,
                });
                if (type === 'patch' || type === 'snapshot') {
                    _lastBroadcastAt = Date.now();
                }
            } catch (err) {
                console.error('court_bridge: postMessage failed:', err);
            }
        },

        // onMessage: register an inbound message handler.
        // Returns an unsubscribe function.
        onMessage(handler) {
            handlers.add(handler);
            return () => handlers.delete(handler);
        },

        close() {
            handlers.clear();
            channel.close();
        },
    };

    return bridge;
}

// -----------------------------------------------------------------------
// Singleton bridge: one shared BroadcastChannel handle per tab.
// Both api_client.jsx (publisher) and app.jsx (display subscriber) import
// this same reference so only one BroadcastChannel is opened per tab.
// The no-op-degrade path inside openBridge() runs exactly once at module
// load time.
// -----------------------------------------------------------------------
export const bridge = openBridge();
