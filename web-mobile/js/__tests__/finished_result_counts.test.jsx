// bc-evts stage 3, review round 2, R5: `terminal` on an outbox entry says how
// it is sent (its own method and url), not that it is a finished result. A
// court move, a time change, a lineup save or a reopen is a CHANGE: it is
// never counted as a finished result in the held-writes counts, the sign-out
// dialog or the pass's "sent" alert. One predicate, isFinishedResultWrite
// (write_result.jsx), says which entries are finished results.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { isFinishedResultWrite, queuedWritesNoun, heldWritesText, heldWriteWhat, heldWriteWhatWhere, heldWriteLine, heldWriteDiscardConfirm } from '../write_result.jsx';

let API;
let mod;
let _lsStore;
let _orig;

beforeEach(async () => {
    vi.useFakeTimers();
    _orig = { fetch: global.fetch, ES: global.EventSource, ls: global.localStorage };
    global.EventSource = class FakeES { constructor() { this.close = () => {}; } };
    global.EventSource.OPEN = 1;
    _lsStore = {};
    global.localStorage = {
        getItem: (k) => (k in _lsStore ? _lsStore[k] : null),
        setItem: (k, v) => { _lsStore[k] = String(v); },
        removeItem: (k) => { delete _lsStore[k]; },
        clear: () => { _lsStore = {}; },
    };
    global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
    vi.resetModules();
    mod = await import('../api_client.jsx');
    API = mod.API;
});

afterEach(() => {
    if (API && typeof API.clearQueue === 'function') API.clearQueue();
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }

describe('isFinishedResultWrite (R5)', () => {
    it('names a finished score, a decision and a representative bout change', () => {
        expect(isFinishedResultWrite({ kind: 'score', terminal: true })).toBe(true);
        expect(isFinishedResultWrite({ kind: 'decision', terminal: true })).toBe(true);
        expect(isFinishedResultWrite({ kind: 'daihyosen', terminal: true })).toBe(true);
    });
    it('does not name a running score write, a tap or any other change', () => {
        expect(isFinishedResultWrite({ kind: 'score', terminal: false })).toBe(false);
        expect(isFinishedResultWrite({ kind: 'evt', terminal: false })).toBe(false);
        for (const kind of ['override', 'court', 'time', 'lineup', 'reopen', 'requeue', 'revert', 'kachinuki', 'member', 'reinstate']) {
            expect(isFinishedResultWrite({ kind, terminal: true })).toBe(false);
        }
    });
    it('words a count with no finished result as changes', () => {
        expect(queuedWritesNoun(0, 1).noun).toBe('change');
        expect(queuedWritesNoun(0, 3).noun).toBe('changes');
        expect(queuedWritesNoun(2, 3).noun).toBe('finished results');
        expect(heldWritesText('offline', { total: 2, terminal: 0 })).toBe('Offline: 2 changes not sent');
    });
});

describe('the outbox counts only finished results as results (R5)', () => {
    it('a queued court move, time change, lineup save and reopen count as changes', async () => {
        API.moveMatchCourt('c1', 'm1', 'B', 'pw');
        API.updateMatchTime('c1', 'm1', '10:00', 'pw');
        API.putMatchLineup('c1', 't1', 'm1', { senpo: 'A' }, 'pw', undefined, ['senpo']);
        API.reopenMatch('c1', 'm2', 'pw');
        await tick();
        const counts = API.unsentWrites();
        expect(counts.total).toBe(4);
        expect(counts.terminal).toBe(0);
    });

    it('a match holding only a court move, a time change or a lineup save has no pending finished result', async () => {
        API.moveMatchCourt('c1', 'm1', 'B', 'pw');
        API.updateMatchTime('c1', 'm1', '10:00', 'pw');
        API.putMatchLineup('c1', 't1', 'm1', { senpo: 'A' }, 'pw', undefined, ['senpo']);
        expect(API.hasPendingTerminalWrite('c1', 'm1'), 'the editors would show a held result banner').toBe(false);
        API.recordDecision('c1', 'm1', { decision: 'fusensho', decisionBy: 'a' }, 'pw');
        expect(API.hasPendingTerminalWrite('c1', 'm1')).toBe(true);
    });

    it('a held court move that lands raises no "results sent" alert', async () => {
        const alerts = [];
        mod.subscribeQueueAlert((a) => alerts.push(a));
        API.moveMatchCourt('c1', 'm1', 'B', 'pw');
        await tick(); // the first send fails offline: the entry is held
        global.fetch = vi.fn((url) => (String(url).includes('/api/time')
            ? Promise.reject(new TypeError('clock'))
            : Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: async () => ({}), text: async () => '{}' })));
        window.dispatchEvent(new Event('online'));
        await tick(10000);
        expect(API.unsentWrites().total).toBe(0);
        expect(alerts.filter((a) => a.kind === 'sent')).toEqual([]);
    });
});

// bc-obox: the check-in, court-call and settings doors are outbox doors, so
// their held writes are listed, counted and offered for discard like any
// other. Each is named as what it is, never "score update", counted as a
// change, and its discard confirm points at where to check it, not a match.
describe('the check-in, court-call and settings doors are named as what they are (bc-obox)', () => {
    const KINDS = {
        'checkin': 'check-in',
        'checkin-bulk': 'check-in',
        'announce': 'court call',
        'announce-remove': 'court call withdrawal',
        'announce-clear': 'clear of court calls',
        'tournament': 'tournament settings',
        'awards': 'fighting-spirit awards',
    };

    it('heldWriteWhat names each kind, and none is a finished result', () => {
        for (const [kind, what] of Object.entries(KINDS)) {
            expect(heldWriteWhat({ kind, terminal: true }), kind).toBe(what);
            expect(isFinishedResultWrite({ kind, terminal: true }), kind).toBe(false);
        }
        expect(heldWriteLine({ kind: 'checkin' })).toBe('Check-in: waiting to be sent');
        expect(heldWriteLine({ kind: 'announce-clear', keepsFailing: true })).toBe('Clear of court calls: the server keeps refusing it');
    });

    it('heldWriteWhatWhere keeps only what the door\'s label adds to the kind\'s name', () => {
        expect(heldWriteWhatWhere({ kind: 'checkin', where: 'Check-in: Ren Abe' })).toBe('check-in (Ren Abe)');
        expect(heldWriteWhatWhere({ kind: 'checkin-bulk', where: 'Check-in: 3 competitors' })).toBe('check-in (3 competitors)');
        expect(heldWriteWhatWhere({ kind: 'checkin', where: '' })).toBe('check-in');
        expect(heldWriteWhatWhere({ kind: 'announce', where: 'Court call: "Shiaijo A: Ren Abe"' })).toBe('court call ("Shiaijo A: Ren Abe")');
        expect(heldWriteWhatWhere({ kind: 'announce-remove', where: 'Court call' })).toBe('court call withdrawal');
        expect(heldWriteWhatWhere({ kind: 'announce-clear', where: 'Every court call' })).toBe('clear of court calls');
        expect(heldWriteWhatWhere({ kind: 'tournament', where: 'Tournament settings' })).toBe('tournament settings');
        expect(heldWriteWhatWhere({ kind: 'awards', where: 'Fighting-spirit awards' })).toBe('fighting-spirit awards');
        // A match's label is kept whole.
        expect(heldWriteWhatWhere({ kind: 'reopen', where: 'Check-in: Pool A' })).toBe('reopen (Check-in: Pool A)');
    });

    it('the discard confirm points at where to check each kind, never at a match', () => {
        const after = {
            'checkin': 'Check the competitor at the registration desk afterwards, and tap them again if it is still needed.',
            'checkin-bulk': 'Check the competitors at the registration desk afterwards, and check them in again if it is still needed.',
            'announce': 'Check the viewer screens afterwards, and make the call again if it is still needed.',
            'announce-remove': 'Check the viewer screens afterwards, and withdraw the call again if it is still showing.',
            'announce-clear': 'Check the viewer screens afterwards, and clear the court calls again if any are still showing.',
            'tournament': 'Check the tournament settings afterwards, and save them again if it is still needed.',
            'awards': 'Check the fighting-spirit awards afterwards, and save them again if it is still needed.',
        };
        for (const [kind, line] of Object.entries(after)) {
            const { message } = heldWriteDiscardConfirm({ kind, keepsFailing: true });
            expect(message, kind).toBe(`The server keeps refusing the ${KINDS[kind]} held on this device, so it may never be sent. Discard it? Nothing else is discarded. ${line}`);
            expect(message, kind).not.toContain('the match');
        }
        expect(heldWriteDiscardConfirm({ kind: 'checkin', where: 'Check-in: Ren Abe' }).message)
            .toContain('the check-in (Ren Abe) held on this device');
        // The match kinds keep their own line.
        expect(heldWriteDiscardConfirm({ kind: 'reopen' }).message).toContain('Check the match afterwards');
        expect(heldWriteDiscardConfirm({ kind: 'lineup' }).message).toContain('Check the lineup afterwards');
        expect(heldWriteDiscardConfirm().message).toContain('Check the match afterwards');
    });

    it('the doors\' held writes are listed by their names and counted as changes', async () => {
        API.toggleCheckIn('c1', 'p1', true, 'pw', { name: 'Ren Abe' });
        API.bulkCheckIn('c1', ['p2', 'p3'], 'pw');
        API.sendAnnouncement('Shiaijo A: Ren Abe', 5, 'pw');
        API.clearAnnouncements('pw');
        API.updateTournament({ name: 'Cup', tournamentVersion: 'v0' }, 'pw');
        API.updateCompetitionAwards('c1', [], 'pw', 'a0');
        await tick();
        const counts = API.unsentWrites();
        expect(counts.total).toBe(6);
        expect(counts.terminal).toBe(0);
        expect(heldWritesText('offline', counts)).toBe('Offline: 6 changes not sent');
        expect(API.heldWrites().map(heldWriteWhatWhere).sort()).toEqual([
            'check-in (2 competitors)',
            'check-in (Ren Abe)',
            'clear of court calls',
            'court call ("Shiaijo A: Ren Abe")',
            'fighting-spirit awards',
            'tournament settings',
        ]);
    });
});
