// Stage 6 review 11, item 11 (b): the outbox replays a DELETE only for the
// doors that queue one: the representative bout's remove, a kachinuki bout's
// removal, a team member's name cleared and a match's own lineup removed. A
// reset of a competition's pool-rank overrides is no outbox entry (no build
// that ever shipped queued one), so a stored entry naming it is not sent.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let _lsStore;
let _orig;

beforeEach(() => {
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
});

afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function tick(ms = 0) {
    await vi.advanceTimersByTimeAsync(ms);
    for (let i = 0; i < 30; i++) await Promise.resolve();
}

const stored = (key, url, kind, method = 'DELETE', payload = {}) => [key, {
    compID: 'c1', matchID: 'm1', password: 'pw', kind, terminal: true, method,
    url, enqueuedAt: Date.now(), payload,
}];

async function replay(entry) {
    _lsStore.bc_write_queue_v2 = JSON.stringify([entry]);
    const sent = [];
    global.fetch = vi.fn((url, opts) => {
        const u = String(url);
        if (u.includes('/api/time')) return Promise.reject(new TypeError('clock'));
        sent.push(`${(opts && opts.method) || 'GET'} ${u}`);
        return Promise.resolve({ ok: true, status: 200, json: async () => ({}) });
    });
    vi.resetModules();
    mod = await import('../api_client.jsx');
    const alerts = [];
    mod.subscribeQueueAlert((a) => alerts.push(a));
    window.dispatchEvent(new Event('online'));
    await tick(5_000);
    return { sent, alerts };
}

describe('the DELETEs the outbox replays', () => {
    it('sends a stored removal of a match\'s own lineup', async () => {
        const { sent } = await replay(stored('c1:m1\u001fa', '/api/competitions/c1/teams/t1/match-lineups/m1', 'lineup-remove'));
        expect(sent).toContain('DELETE /api/competitions/c1/teams/t1/match-lineups/m1');
    });

    it('sends a stored check-in taken back', async () => {
        const { sent } = await replay(stored('checkin:c1\u001fa', '/api/competitions/c1/participants/p1/checkin', 'checkin', 'DELETE', { modifiedAt: 5 }));
        expect(sent).toContain('DELETE /api/competitions/c1/participants/p1/checkin');
    });

    it('never sends a stored DELETE of a participant itself', async () => {
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const { sent } = await replay(stored('checkin:c1\u001fa', '/api/competitions/c1/participants/p1', 'checkin'));
        expect(sent.filter((s) => s.startsWith('DELETE'))).toEqual([]);
        warned.mockRestore();
    });

    it('never sends a stored DELETE one segment past a check-in', async () => {
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const { sent } = await replay(stored('checkin:c1\u001fa', '/api/competitions/c1/participants/p1/checkin/x', 'checkin'));
        expect(sent.filter((s) => s.startsWith('DELETE'))).toEqual([]);
        warned.mockRestore();
    });

    it('never sends a stored reset of the pool-rank overrides', async () => {
        // The rehydrate says it was not replayed.
        const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const { sent, alerts } = await replay(stored('pool-ranks:c1\u001fa', '/api/competitions/c1/overrides', 'pool-ranks'));
        expect(sent.filter((s) => s.startsWith('DELETE'))).toEqual([]);
        expect(alerts.map((a) => a.kind)).toContain('unreadable');
        expect(warned).toHaveBeenCalled();
        warned.mockRestore();
    });
});

// bc-obox: the four doors outside /api/competitions/… are named by their exact
// path and method. Each replays; a near miss of each (another method, a path
// one segment off, the reset and import doors beside them) is never sent.
describe('the doors outside a competition the outbox replays', () => {
    const doors = [
        ['POST', '/api/tournament/announce', 'announce', { id: 'a', message: 'x', durationMinutes: 5, modifiedAt: 5 }],
        ['DELETE', '/api/announcements/a1', 'announce-remove', {}],
        ['DELETE', '/api/announcements', 'announce-clear', { modifiedAt: 5 }],
        ['PUT', '/api/tournament', 'tournament', { name: 'T', tournamentVersion: 'v1', password: '' }],
    ];
    for (const [method, url, kind, payload] of doors) {
        it(`sends a stored ${method} ${url}`, async () => {
            const { sent } = await replay(stored(`${kind}\u001fa`, url, kind, method, payload));
            expect(sent).toContain(`${method} ${url}`);
        });
    }

    const nearMisses = [
        ['POST', '/api/tournament/reset'],
        ['POST', '/api/tournament/import'],
        ['POST', '/api/tournament'],
        ['PUT', '/api/tournament/x'],
        ['PUT', '/api/tournament/announce'],
        ['DELETE', '/api/tournament'],
        ['DELETE', '/api/announcements/a/b'],
        ['POST', '/api/announcements'],
        ['PUT', '/api/announcements/a1'],
        ['POST', '/api/tournament/announce/../reset'],
    ];
    for (const [method, url] of nearMisses) {
        it(`never sends a stored ${method} ${url}`, async () => {
            const warned = vi.spyOn(console, 'warn').mockImplementation(() => {});
            const { sent, alerts } = await replay(stored('x\u001fa', url, 'announce', method, { modifiedAt: 5 }));
            expect(sent.filter((s) => !s.startsWith('GET'))).toEqual([]);
            expect(alerts.map((a) => a.kind)).toContain('unreadable');
            warned.mockRestore();
        });
    }

    it('a stored tournament save is sent with no password, whatever storage holds', async () => {
        _lsStore.bc_write_queue_v2 = JSON.stringify([stored('tournament\u001fa', '/api/tournament', 'tournament', 'PUT', { name: 'T', tournamentVersion: 'v1', password: 'tampered' })]);
        const bodies = [];
        global.fetch = vi.fn((url, opts) => {
            if (String(url).includes('/api/time')) return Promise.reject(new TypeError('clock'));
            if (opts && opts.method === 'PUT') bodies.push(JSON.parse(opts.body));
            return Promise.resolve({ ok: true, status: 200, json: async () => ({ tournamentVersion: 'v2' }) });
        });
        vi.resetModules();
        mod = await import('../api_client.jsx');
        window.dispatchEvent(new Event('online'));
        await tick(5_000);
        expect(bodies).toHaveLength(1);
        expect(bodies[0].password).toBe('');
    });
});
