import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
    deriveLinkState,
    applyPatchToTree,
    mergeSnapshotIntoTree,
    resolveCompId,
    freshnessMs,
    getLastBroadcastAt,
    setSnapshotProvider,
    setDisplayCourt,
    openBridge,
    _tabId,
    _resetModuleStateForTests,
} from '../court_bridge.jsx';

// court_bridge.jsx: mp-9ukk Phase 2 unit tests.
//
// Three logical suites:
//   1. deriveLinkState: pure truth-table (boundary at freshnessMs)
//   2. applyPatchToTree: tree adapter (right competition updated, others
//      untouched, new reference returned)
//   3. openBridge: publish/subscribe, snapshot handshake, self-echo suppression
//      (all via a BroadcastChannel mock so no real browser channel is needed)

// -------------------------------------------------------------------------
// 1. deriveLinkState truth table
// -------------------------------------------------------------------------
describe('deriveLinkState', () => {
    const now = 1_000_000;

    it('returns connected when sseConnected is true, regardless of lastBroadcastAt', () => {
        expect(deriveLinkState({ sseConnected: true, lastBroadcastAt: null, now })).toBe('connected');
        expect(deriveLinkState({ sseConnected: true, lastBroadcastAt: now - 1, now })).toBe('connected');
        expect(deriveLinkState({ sseConnected: true, lastBroadcastAt: now - freshnessMs * 2, now })).toBe('connected');
    });

    it('returns stale when sseConnected is false and lastBroadcastAt is null', () => {
        expect(deriveLinkState({ sseConnected: false, lastBroadcastAt: null, now })).toBe('stale');
    });

    it('returns local when server down and broadcast is 1ms under the threshold (just-under)', () => {
        // just under: elapsed = freshnessMs - 1 → still fresh
        const justUnder = now - (freshnessMs - 1);
        expect(deriveLinkState({ sseConnected: false, lastBroadcastAt: justUnder, now })).toBe('local');
    });

    it('returns stale when server down and broadcast is exactly at the threshold', () => {
        // elapsed = freshnessMs → NOT < freshnessMs, so stale
        const exactly = now - freshnessMs;
        expect(deriveLinkState({ sseConnected: false, lastBroadcastAt: exactly, now })).toBe('stale');
    });

    it('returns stale when server down and broadcast is 1ms over the threshold (just-over)', () => {
        const justOver = now - (freshnessMs + 1);
        expect(deriveLinkState({ sseConnected: false, lastBroadcastAt: justOver, now })).toBe('stale');
    });

    it('accepts a custom freshnessMs override for testing', () => {
        const customMs = 5000;
        const recentEnough = now - (customMs - 1);
        const tooOld = now - customMs;
        expect(deriveLinkState({ sseConnected: false, lastBroadcastAt: recentEnough, now, freshnessMs: customMs })).toBe('local');
        expect(deriveLinkState({ sseConnected: false, lastBroadcastAt: tooOld, now, freshnessMs: customMs })).toBe('stale');
    });
});

// -------------------------------------------------------------------------
// 2. applyPatchToTree: tree-level adapter
// -------------------------------------------------------------------------
describe('applyPatchToTree', () => {
    function makeTournament() {
        return {
            name: 'Test Tournament',
            competitions: [
                {
                    id: 'comp-A',
                    config: { id: 'comp-A' },
                    poolMatches: [
                        { id: 'p1', status: 'scheduled', court: 'A' },
                        { id: 'p2', status: 'scheduled', court: 'A' },
                    ],
                    bracket: { rounds: [] },
                },
                {
                    id: 'comp-B',
                    config: { id: 'comp-B' },
                    poolMatches: [
                        { id: 'b1', status: 'scheduled', court: 'B' },
                    ],
                    bracket: { rounds: [] },
                },
            ],
        };
    }

    it('returns the original tournament reference when msg type is not patch', () => {
        const t = makeTournament();
        expect(applyPatchToTree(t, { type: 'snapshot', compId: 'comp-A', payload: {} })).toBe(t);
    });

    it('returns the original tournament reference when compId does not match any competition', () => {
        const t = makeTournament();
        const msg = { type: 'patch', compId: 'no-such-comp', payload: { result: { id: 'p1', status: 'running' } } };
        expect(applyPatchToTree(t, msg)).toBe(t);
    });

    it('returns the original tournament reference when tournament is null', () => {
        expect(applyPatchToTree(null, { type: 'patch', compId: 'comp-A', payload: {} })).toBeNull();
    });

    it('returns the original tournament reference when competitions is empty', () => {
        const t = { name: 'T', competitions: [] };
        expect(applyPatchToTree(t, { type: 'patch', compId: 'comp-A', payload: { result: { id: 'p1' } } })).toBe(t);
    });

    it('updates only the targeted competition and returns a new tournament reference', () => {
        const t = makeTournament();
        const compABefore = t.competitions[0];
        const compBBefore = t.competitions[1];

        const msg = {
            type: 'patch',
            compId: 'comp-A',
            payload: { result: { id: 'p1', status: 'running', winner: null } },
        };
        const next = applyPatchToTree(t, msg);

        // New tournament reference
        expect(next).not.toBe(t);
        // Competition A got a new reference (it was patched)
        expect(next.competitions[0]).not.toBe(compABefore);
        // Competition B is the same reference (untouched)
        expect(next.competitions[1]).toBe(compBBefore);
        // The targeted match inside comp-A was updated
        const p1 = next.competitions[0].poolMatches.find(m => m.id === 'p1');
        expect(p1.status).toBe('running');
    });

    it('matches competition by config.id when top-level id is absent', () => {
        const t = {
            competitions: [
                {
                    // no top-level id
                    config: { id: 'nested-id' },
                    poolMatches: [{ id: 'm1', status: 'scheduled' }],
                    bracket: { rounds: [] },
                },
            ],
        };
        const msg = { type: 'patch', compId: 'nested-id', payload: { result: { id: 'm1', status: 'running' } } };
        const next = applyPatchToTree(t, msg);
        expect(next).not.toBe(t);
        expect(next.competitions[0].poolMatches[0].status).toBe('running');
    });

    it('returns original reference when patch produces no change (unknown match id)', () => {
        const t = makeTournament();
        const msg = {
            type: 'patch',
            compId: 'comp-A',
            payload: { result: { id: 'no-such-match', status: 'running' } },
        };
        // applyPatch returns prev when no ids match → applyPatchToTree must
        // propagate that identity and also return the original tournament.
        const next = applyPatchToTree(t, msg);
        expect(next).toBe(t);
    });

    it('treats a patch with no payload as a no-op (returns original reference)', () => {
        const t = makeTournament();
        expect(applyPatchToTree(t, { type: 'patch', compId: 'comp-A', payload: null })).toBe(t);
        expect(applyPatchToTree(t, { type: 'patch', compId: 'comp-A' })).toBe(t);
    });

    it('does not throw on a malformed payload and leaves the tree unchanged', () => {
        // Spy so any intentional error log here is captured, not flagged by the
        // setup's unexpected-error guard.
        const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
        const t = makeTournament();
        // results:[null] is now handled at the SOURCE: patch.jsx applyPatch skips
        // null/id-less entries before building its Map, so it no longer throws
        // and applyPatchToTree's try/catch backstop is not even exercised. Either
        // way the contract holds: no throw, tree unchanged. (The try/catch in
        // applyPatchToTree stays as defense-in-depth for any future throw path.)
        const msg = { type: 'patch', compId: 'comp-A', payload: { results: [null] } };
        let next;
        expect(() => { next = applyPatchToTree(t, msg); }).not.toThrow();
        expect(next).toBe(t);
        expect(errSpy).not.toHaveBeenCalled();
        errSpy.mockRestore();
    });

    it('does not match an id-less competition when compId is empty', () => {
        const t = { competitions: [{ poolMatches: [{ id: 'm1' }], bracket: { rounds: [] } }] };
        // An id-less comp resolves to '' but an empty compId must not target it.
        expect(applyPatchToTree(t, { type: 'patch', compId: '', payload: { result: { id: 'm1' } } })).toBe(t);
    });
});

// -------------------------------------------------------------------------
// 2b. resolveCompId: shape-tolerant competition id reader
// -------------------------------------------------------------------------
describe('resolveCompId', () => {
    it('reads the top-level id when present', () => {
        expect(resolveCompId({ id: 'top', config: { id: 'nested' } })).toBe('top');
    });
    it('falls back to config.id when top-level id is absent', () => {
        expect(resolveCompId({ config: { id: 'nested' } })).toBe('nested');
    });
    it('falls back to the legacy config.Id arm', () => {
        expect(resolveCompId({ config: { Id: 'legacy' } })).toBe('legacy');
    });
    it('returns empty string for null, primitives, and id-less objects', () => {
        expect(resolveCompId(null)).toBe('');
        expect(resolveCompId(undefined)).toBe('');
        expect(resolveCompId('x')).toBe('');
        expect(resolveCompId(42)).toBe('');
        expect(resolveCompId({})).toBe('');
        expect(resolveCompId({ config: {} })).toBe('');
    });
});

// -------------------------------------------------------------------------
// 2c. mergeSnapshotIntoTree: snapshot fold policy (bootstrap / drop / merge)
// -------------------------------------------------------------------------
describe('mergeSnapshotIntoTree', () => {
    it('returns prev unchanged when the payload is not an array', () => {
        const prev = { name: 'T', competitions: [] };
        expect(mergeSnapshotIntoTree(prev, null, { connected: false })).toBe(prev);
        expect(mergeSnapshotIntoTree(prev, { not: 'array' }, { connected: false })).toBe(prev);
        expect(mergeSnapshotIntoTree(prev, undefined, { connected: false })).toBe(prev);
    });

    it('bootstraps a minimal tournament when prev is null (cold start)', () => {
        const comps = [{ id: 'c1' }, { id: 'c2' }];
        const out = mergeSnapshotIntoTree(null, comps, { connected: false });
        expect(out).toEqual({ name: '', courts: [], competitions: comps });
    });

    it('bootstraps even when connected (an outage at load must not be starved)', () => {
        const comps = [{ id: 'c1' }];
        const out = mergeSnapshotIntoTree(null, comps, { connected: true });
        expect(out.competitions.map(c => c.id)).toEqual(['c1']);
    });

    it('filters null/primitive/id-less elements out of the cold-start bootstrap too', () => {
        const out = mergeSnapshotIntoTree(null, [null, 'x', {}, { config: {} }, { id: 'c1' }], { connected: false });
        expect(out.competitions).toEqual([{ id: 'c1' }]);
    });

    it('drops the snapshot (returns prev) when prev exists and SSE is connected', () => {
        const prev = { name: 'T', competitions: [{ id: 'c1', tag: 'server' }] };
        const out = mergeSnapshotIntoTree(prev, [{ id: 'c1', tag: 'operator' }], { connected: true });
        expect(out).toBe(prev);
        expect(out.competitions[0].tag).toBe('server');
    });

    it('replaces an existing competition by id when offline', () => {
        const prev = { name: 'T', courts: ['A'], competitions: [{ id: 'c1', tag: 'server' }, { id: 'c2', tag: 'server' }] };
        const out = mergeSnapshotIntoTree(prev, [{ id: 'c1', tag: 'operator' }], { connected: false });
        expect(out).not.toBe(prev);
        expect(out.name).toBe('T');           // surrounding fields preserved
        expect(out.courts).toEqual(['A']);
        expect(out.competitions.find(c => c.id === 'c1').tag).toBe('operator'); // replaced
        expect(out.competitions.find(c => c.id === 'c2').tag).toBe('server');   // untouched
    });

    it('appends a competition not already present when offline', () => {
        const prev = { competitions: [{ id: 'c1' }] };
        const out = mergeSnapshotIntoTree(prev, [{ id: 'c2', tag: 'new' }], { connected: false });
        expect(out.competitions.map(c => c.id)).toEqual(['c1', 'c2']);
    });

    it('matches existing competitions by config.id (detail-endpoint shape)', () => {
        const prev = { competitions: [{ config: { id: 'c1' }, tag: 'server' }] };
        const out = mergeSnapshotIntoTree(prev, [{ config: { id: 'c1' }, tag: 'operator' }], { connected: false });
        expect(out.competitions).toHaveLength(1);
        expect(out.competitions[0].tag).toBe('operator');
    });

    it('skips null, primitive, and id-less elements in the payload', () => {
        const prev = { competitions: [{ id: 'c1', tag: 'server' }] };
        const out = mergeSnapshotIntoTree(prev, [null, 'x', {}, { config: {} }, { id: 'c2', tag: 'ok' }], { connected: false });
        expect(out.competitions.map(c => c.id || 'none')).toEqual(['c1', 'c2']);
        expect(out.competitions.find(c => c.id === 'c1').tag).toBe('server');
    });

    it('treats a missing prev.competitions as an empty list when offline', () => {
        const prev = { name: 'T' };
        const out = mergeSnapshotIntoTree(prev, [{ id: 'c1' }], { connected: false });
        expect(out.competitions.map(c => c.id)).toEqual(['c1']);
    });

    it('defaults connected to falsey when the options object is omitted', () => {
        const prev = { competitions: [{ id: 'c1', tag: 'server' }] };
        const out = mergeSnapshotIntoTree(prev, [{ id: 'c1', tag: 'operator' }]);
        // No options → connected is undefined (falsey) → offline merge runs.
        expect(out.competitions[0].tag).toBe('operator');
    });
});

// -------------------------------------------------------------------------
// 3. openBridge: BroadcastChannel mock + publish/subscribe/snapshot/self-echo
// -------------------------------------------------------------------------

// Minimal BroadcastChannel mock. Instances are tracked by channel name so
// tests can inject cross-tab messages with a spoofed origin (bypassing the
// self-echo guard that would otherwise silence same-module delivery).
class MockBroadcastChannel {
    static _instances = new Map(); // name → Set<instance>
    // Last message sent via postMessage, for shape assertions.
    static _lastSent = null;

    constructor(name) {
        this._name = name;
        this.onmessage = null;
        if (!MockBroadcastChannel._instances.has(name)) {
            MockBroadcastChannel._instances.set(name, new Set());
        }
        MockBroadcastChannel._instances.get(name).add(this);
    }

    postMessage(data) {
        MockBroadcastChannel._lastSent = data;
        // Deliver to all OTHER channel instances (normal BroadcastChannel semantics).
        // In tests all bridges share the same _tabId, so the bridge's own self-echo
        // guard will drop these. Use injectFrom() below to simulate cross-tab delivery
        // with a different origin.
        const peers = MockBroadcastChannel._instances.get(this._name) || new Set();
        for (const peer of peers) {
            if (peer !== this && peer.onmessage) {
                peer.onmessage({ data });
            }
        }
    }

    // injectFrom: deliver a message to all instances on this channel as if it
    // came from a different tab (spoofed origin). Bypasses self-echo suppression
    // so tests can verify the receive path.
    static injectFrom(channelName, data) {
        const peers = MockBroadcastChannel._instances.get(channelName) || new Set();
        for (const peer of peers) {
            if (peer.onmessage) peer.onmessage({ data });
        }
    }

    close() {
        const peers = MockBroadcastChannel._instances.get(this._name);
        if (peers) peers.delete(this);
    }

    static reset() {
        MockBroadcastChannel._instances.clear();
        MockBroadcastChannel._lastSent = null;
    }
}

describe('openBridge', () => {
    let origBC;

    beforeEach(() => {
        origBC = global.BroadcastChannel;
        global.BroadcastChannel = MockBroadcastChannel;
        MockBroadcastChannel.reset();
        // Reset all module-level state (lastBroadcastAt, displayCourt,
        // snapshotProvider) so tests do not depend on execution order.
        _resetModuleStateForTests();
    });

    afterEach(() => {
        global.BroadcastChannel = origBC;
        MockBroadcastChannel.reset();
    });

    it('returns a no-op bridge when BroadcastChannel is unavailable', () => {
        delete global.BroadcastChannel;
        const b = openBridge();
        // Should not throw and should have the expected shape
        expect(typeof b.publish).toBe('function');
        expect(typeof b.onMessage).toBe('function');
        expect(typeof b.close).toBe('function');
        // no-op: calling publish and onMessage should not throw
        b.publish('patch', 'A', 'comp-1', {});
        const unsub = b.onMessage(() => {});
        expect(typeof unsub).toBe('function');
        b.close();
        global.BroadcastChannel = MockBroadcastChannel;
    });

    it('publishes a versioned patch message with the correct shape', () => {
        // In this process all bridges share the same _tabId, so the self-echo
        // guard suppresses same-module delivery. We verify the outgoing message
        // shape directly from MockBroadcastChannel._lastSent (what postMessage
        // actually put on the wire), then verify the receive path separately by
        // injecting a cross-tab message with a different origin.
        const b = openBridge();
        const received = [];
        b.onMessage(msg => received.push(msg));

        b.publish('patch', 'A', 'comp-1', { result: { id: 'm1', status: 'running' } });

        // Verify outgoing wire shape.
        const sent = MockBroadcastChannel._lastSent;
        expect(sent).not.toBeNull();
        expect(sent.type).toBe('patch');
        expect(sent.v).toBe(1);
        expect(sent.court).toBe('A');
        expect(sent.compId).toBe('comp-1');
        expect(sent.payload.result.id).toBe('m1');

        // Verify receive path: inject a message from a different origin so the
        // self-echo guard passes.
        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'patch', origin: 'other-tab-id',
            court: 'A', compId: 'comp-1',
            payload: { result: { id: 'm1', status: 'running' } },
        });
        expect(received).toHaveLength(1);
        expect(received[0].type).toBe('patch');
        expect(received[0].origin).toBe('other-tab-id');

        b.close();
    });

    it('does not deliver a message to the sender itself (self-echo suppression)', () => {
        // Both bridges share the same _tabId (same module instance), so any
        // message from one will carry origin === _tabId and be dropped by the
        // other bridge's handler when they share a tab. Here we open two bridges
        // in the same process (same _tabId) to confirm self-echo suppression.
        const b1 = openBridge();
        const b2 = openBridge();
        const received = [];
        // b1 listens; b1 publishes. Since they share _tabId, b2 should drop it,
        // but more importantly b1 itself should not echo. We verify via b2.
        b2.onMessage(msg => received.push(msg));

        // Manually simulate: the mock channel calls onmessage on the OTHER
        // instance. b2's channel gets the message from b1. b2 checks origin ===
        // _tabId and drops it.
        b1.publish('patch', 'A', 'comp-1', {});

        // b2 receives the raw postMessage but drops it because origin === _tabId
        expect(received).toHaveLength(0);

        b1.close();
        b2.close();
    });

    it('unsubscribe function removes the handler', () => {
        const b = openBridge();
        const received = [];
        const unsub = b.onMessage(msg => received.push(msg));
        unsub(); // remove before any message arrives

        // Inject a cross-tab message (different origin bypasses self-echo guard).
        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'patch', origin: 'other-tab-id',
            court: 'A', compId: 'comp-1', payload: null,
        });
        // The unsubbed handler must not fire.
        expect(received).toHaveLength(0);

        b.close();
    });

    it('auto-responds to snapshot-req from a different origin with a snapshot for the right court', () => {
        // The self-echo guard drops messages whose origin === _tabId, so a
        // same-module publish() never reaches the responder. Inject the
        // snapshot-req from a spoofed different origin to bypass that guard
        // and actually exercise the auto-reply path.
        const competitions = [{ id: 'comp-A', poolMatches: [] }];
        setSnapshotProvider((court) => court === 'A' ? competitions : null);

        openBridge(); // activate the onmessage responder

        MockBroadcastChannel._lastSent = null;

        // Inject a snapshot-req from a different tab for court 'A'.
        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'snapshot-req', origin: 'other-tab-id',
            court: 'A', compId: '', payload: null,
        });

        // The responder should have called postMessage with a snapshot reply.
        const sent = MockBroadcastChannel._lastSent;
        expect(sent).not.toBeNull();
        expect(sent.type).toBe('snapshot');
        expect(sent.court).toBe('A');
        expect(sent.compId).toBe('');
        expect(sent.origin).toBe(_tabId);
        expect(sent.payload).toBe(competitions);
    });

    it('does not reply to a snapshot-req when the provider has no data for the requested court', () => {
        // Provider only has data for court 'A'; request for court 'B' should
        // produce no reply (last-sent stays null).
        const competitions = [{ id: 'comp-A', poolMatches: [] }];
        setSnapshotProvider((court) => court === 'A' ? competitions : null);

        openBridge();

        MockBroadcastChannel._lastSent = null;

        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'snapshot-req', origin: 'other-tab-id',
            court: 'B', compId: '', payload: null,
        });

        // No data for court B: the responder must not call postMessage.
        expect(MockBroadcastChannel._lastSent).toBeNull();
    });

    it('never forwards snapshot-req to onMessage subscribers, with or without a provider', () => {
        // snapshot-req is always fully handled inside the channel.onmessage
        // responder; no subscriber (app.jsx only acts on 'patch'/'snapshot')
        // should ever see it, whether or not a provider happens to be
        // registered for the requesting court.
        setSnapshotProvider(null); // no provider registered
        const b = openBridge();
        const received = [];
        b.onMessage(msg => received.push(msg));

        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'snapshot-req', origin: 'other-tab-id',
            court: 'A', compId: '', payload: null,
        });
        expect(received).toHaveLength(0);

        // Same assertion with a provider registered (and a court it can answer).
        setSnapshotProvider((court) => (court === 'A' ? [{ id: 'comp-A', poolMatches: [] }] : null));
        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'snapshot-req', origin: 'other-tab-id',
            court: 'A', compId: '', payload: null,
        });
        expect(received).toHaveLength(0);
    });

    it('getLastBroadcastAt returns null before any publish and updates after', () => {
        // Note: getLastBroadcastAt is module-level state. Other tests that call
        // openBridge().publish() will have already set it, so we only verify
        // it is a number (or null) and advances monotonically.
        const before = getLastBroadcastAt();
        const b = openBridge();
        const t0 = Date.now();
        b.publish('patch', 'A', 'comp-1', {});
        const after = getLastBroadcastAt();
        // after must be set and >= t0
        expect(typeof after).toBe('number');
        expect(after).toBeGreaterThanOrEqual(t0);
        // If before was already a number, after must be >= before
        if (before !== null) {
            expect(after).toBeGreaterThanOrEqual(before);
        }
        b.close();
    });

    it('close() stops further message delivery to registered handlers', () => {
        const b = openBridge();
        const received = [];
        b.onMessage(msg => received.push(msg));
        b.close();

        // After close the channel instance is removed from MockBroadcastChannel._instances,
        // so injectFrom no longer reaches it.
        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'patch', origin: 'other-tab-id',
            court: 'B', compId: 'comp-2', payload: null,
        });
        // Bridge is closed: no delivery expected.
        expect(received).toHaveLength(0);
    });
});

// -------------------------------------------------------------------------
// 4. setDisplayCourt: court-scoped inbound recency clock (FIX 2)
//
// When a display court is set, only inbound patches/snapshots for that
// court advance _lastBroadcastAt. When null, all inbound messages advance it.
// -------------------------------------------------------------------------
describe('setDisplayCourt inbound recency scoping', () => {
    let origBC;

    beforeEach(() => {
        origBC = global.BroadcastChannel;
        global.BroadcastChannel = MockBroadcastChannel;
        MockBroadcastChannel.reset();
        // Reset all module-level state so tests do not depend on execution order.
        _resetModuleStateForTests();
    });

    afterEach(() => {
        global.BroadcastChannel = origBC;
        MockBroadcastChannel.reset();
        setDisplayCourt(null);
    });

    it('does not advance _lastBroadcastAt when a patch arrives for a different court', () => {
        setDisplayCourt('A');
        openBridge(); // open a bridge so the channel.onmessage handler is active

        const before = getLastBroadcastAt();

        // Inject a patch for court B (not the display court).
        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'patch', origin: 'other-tab-id',
            court: 'B', compId: 'comp-1', payload: { result: { id: 'm1' } },
        });

        expect(getLastBroadcastAt()).toBe(before);
    });

    it('advances _lastBroadcastAt when a patch arrives for the display court', () => {
        setDisplayCourt('A');
        openBridge();

        const t0 = Date.now();

        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'patch', origin: 'other-tab-id',
            court: 'A', compId: 'comp-1', payload: { result: { id: 'm1' } },
        });

        const after = getLastBroadcastAt();
        expect(typeof after).toBe('number');
        expect(after).toBeGreaterThanOrEqual(t0);
    });

    it('advances _lastBroadcastAt for any court when setDisplayCourt(null) is called', () => {
        setDisplayCourt(null);
        openBridge();

        const t0 = Date.now();

        // Court B patch should advance the clock when no display court is set.
        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'patch', origin: 'other-tab-id',
            court: 'B', compId: 'comp-2', payload: { result: { id: 'm2' } },
        });

        const after = getLastBroadcastAt();
        expect(typeof after).toBe('number');
        expect(after).toBeGreaterThanOrEqual(t0);
    });

    it('does not advance _lastBroadcastAt for snapshot of a different court', () => {
        setDisplayCourt('A');
        openBridge();

        const before = getLastBroadcastAt();

        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'snapshot', origin: 'other-tab-id',
            court: 'C', compId: '', payload: [],
        });

        expect(getLastBroadcastAt()).toBe(before);
    });

    it('advances _lastBroadcastAt for snapshot matching the display court', () => {
        setDisplayCourt('A');
        openBridge();

        const t0 = Date.now();

        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'snapshot', origin: 'other-tab-id',
            court: 'A', compId: '', payload: [],
        });

        const after = getLastBroadcastAt();
        expect(typeof after).toBe('number');
        expect(after).toBeGreaterThanOrEqual(t0);
    });

    it('resets _lastBroadcastAt when the display court actually changes', () => {
        setDisplayCourt('A');
        openBridge();

        // Court A becomes fresh.
        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'patch', origin: 'other-tab-id',
            court: 'A', compId: 'comp-A', payload: { result: { id: 'm1' } },
        });
        expect(typeof getLastBroadcastAt()).toBe('number');

        // Switching to a new court must clear the recency clock so the new
        // court's dot starts 'stale' until its own operator broadcasts.
        setDisplayCourt('B');
        expect(getLastBroadcastAt()).toBeNull();
    });

    it('does not reset _lastBroadcastAt when setDisplayCourt is called with the same court', () => {
        setDisplayCourt('A');
        openBridge();

        MockBroadcastChannel.injectFrom('bc-court-hub-v1', {
            v: 1, type: 'patch', origin: 'other-tab-id',
            court: 'A', compId: 'comp-A', payload: { result: { id: 'm1' } },
        });
        const before = getLastBroadcastAt();
        expect(typeof before).toBe('number');

        // Re-asserting the same court is a no-op for the recency clock.
        setDisplayCourt('A');
        expect(getLastBroadcastAt()).toBe(before);
    });
});
