// bc-obox: the check-in, court-call and settings doors are outbox doors. Each
// answers at once ({ pending, settled }), is held through a dropout and sent
// in its own base's order: a competition's check-ins (`checkin:<comp>`,
// stamped, the later tap winning), a court call and its withdrawal
// (`announce:<id>`), the clear (`announce-clear`), the tournament settings
// (`tournament`) and a competition's awards (`awards:<comp>`), the two
// documents saved against a version.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let mod;
let API;
let _lsStore;
let _orig;

beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetModules();
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
    mod = await import('../api_client.jsx');
    API = mod.API;
    vi.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
    if (API && typeof API.clearQueue === 'function') API.clearQueue();
    console.warn.mockRestore();
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
    global.localStorage = _orig.ls;
});

async function flushMicrotasks() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
async function tick(ms = 0) { await vi.advanceTimersByTimeAsync(ms); await flushMicrotasks(); }
const isClock = (url) => String(url).includes('/api/time');
const respond = (status, body) => Promise.resolve({ ok: status >= 200 && status < 300, status, json: async () => body });
// The server: `impl(url, opts)` answers [status, body]; every write is kept.
function server(impl) {
    const sent = [];
    global.fetch = vi.fn((url, opts = {}) => {
        if (isClock(url)) return Promise.reject(new TypeError('clock'));
        const req = { method: opts.method || 'GET', url: String(url), body: opts.body ? JSON.parse(opts.body) : undefined };
        if (req.method !== 'GET') sent.push(req);
        const out = impl(req);
        return out && typeof out.then === 'function' ? out : respond(...out);
    });
    return sent;
}
const stored = () => JSON.parse(_lsStore.bc_write_queue_v2 || '[]');
const baseOf = (key) => key.split('\u001f')[0];
const alertsHeard = () => { const a = []; mod.subscribeQueueAlert((x) => a.push(x)); return a; };
const patchesHeard = () => { const p = []; API.subscribeLocalParticipantPatch((e) => p.push(e)); return p; };

describe('check-ins (C1, C2)', () => {
    it('a tap answers at once under the competition\'s base, stamped, and is shown at once', async () => {
        server(() => new Promise(() => {}));
        const patches = patchesHeard();
        const res = await API.toggleCheckIn('c1', 'p1', true, 'pw', { name: 'Ren Abe' });
        expect(res.pending).toBe(true);
        expect(baseOf(res.key)).toBe('checkin:c1');
        const [[, d]] = stored();
        expect(d.method).toBe('PUT');
        expect(d.url).toBe('/api/competitions/c1/participants/p1/checkin');
        expect(d.payload.modifiedAt).toBeGreaterThan(0);
        expect(d.label).toBe('Check-in: Ren Abe');
        expect(patches[0]).toEqual({ type: 'checkin_updated', local: true, data: { competitionId: 'c1', participants: [{ id: 'p1', checkedIn: true }] } });
    });

    it('an undo is a DELETE with its stamp, after the tap it takes back', async () => {
        server(() => new Promise(() => {}));
        await API.toggleCheckIn('c1', 'p1', true, 'pw');
        await API.toggleCheckIn('c1', 'p1', false, 'pw');
        const entries = stored().map(([, d]) => d);
        expect(entries.map((d) => d.method)).toEqual(['PUT', 'DELETE']);
        expect(entries[1].payload.modifiedAt).toBeGreaterThan(entries[0].payload.modifiedAt);
    });

    it('waits through a dropout and is sent when the connection returns', async () => {
        const res = await API.toggleCheckIn('c1', 'p1', true, 'pw');
        await tick(0);
        expect(API.unsentWrites().total).toBe(1);
        const sent = server(() => [200, { id: 'p1', checkedIn: true }]);
        window.dispatchEvent(new Event('online'));
        await tick(60_000);
        await expect(res.settled).resolves.toEqual({ id: 'p1', checkedIn: true });
        expect(sent.map((r) => r.method)).toEqual(['PUT']);
    });

    it('a superseded answer holding what the tap asked settles quietly', async () => {
        server(() => [200, { applied: false, reason: 'superseded', participant: { id: 'p1', name: 'Ren Abe', checkedIn: true } }]);
        const alerts = alertsHeard();
        const res = await API.toggleCheckIn('c1', 'p1', true, 'pw');
        await expect(res.settled).resolves.toMatchObject({ applied: false, reason: 'superseded' });
        expect(alerts.filter((a) => a.kind === 'checkin_superseded')).toEqual([]);
    });

    it('a superseded answer the other way shows the stored participant and is told once', async () => {
        server(() => [200, { applied: false, reason: 'superseded', participant: { id: 'p1', name: 'Ren Abe', checkedIn: false } }]);
        const alerts = alertsHeard();
        const patches = patchesHeard();
        const res = await API.toggleCheckIn('c1', 'p1', true, 'pw');
        await res.settled;
        await tick(0);
        const told = alerts.filter((a) => a.kind === 'checkin_superseded');
        expect(told).toHaveLength(1);
        expect(told[0].detail).toBe('Ren Abe was changed on another desk: now not checked in.');
        expect(patches.at(-1).data.participants).toEqual([{ id: 'p1', checkedIn: false }]);
    });

    it('a caller that claims the superseded answer tells it itself (no alert)', async () => {
        server(() => [200, { applied: false, reason: 'superseded', participant: { id: 'p1', name: 'Ren Abe', checkedIn: false } }]);
        const alerts = alertsHeard();
        const claimed = [];
        const res = await API.toggleCheckIn('c1', 'p1', true, 'pw', { onRefusal: (e) => { claimed.push(e); return true; } });
        await res.settled;
        await tick(0);
        expect(claimed).toHaveLength(1);
        expect(claimed[0].reason).toBe('superseded');
        expect(alerts.filter((a) => a.kind === 'checkin_superseded')).toEqual([]);
    });

    it('a clock_skew answer is re-stamped and sent again (its own base, not a match\'s)', async () => {
        let n = 0;
        const sent = server(() => (++n === 1
            ? [200, { applied: false, reason: 'clock_skew', serverNowMs: Date.now() }]
            : [200, { id: 'p1', checkedIn: true }]));
        const res = await API.toggleCheckIn('c1', 'p1', true, 'pw');
        await tick(0);
        await expect(res.settled).resolves.toEqual({ id: 'p1', checkedIn: true });
        expect(sent).toHaveLength(2);
        expect(sent[1].body.writeId).toBe(sent[0].body.writeId);
    });

    it('a refusal rejects and puts back what the row had', async () => {
        server(() => [409, { error: 'check_in_not_in_self_run', message: 'Self-run competitions have no check-in.' }]);
        const patches = patchesHeard();
        const res = await API.toggleCheckIn('c1', 'p1', true, 'pw', { was: { p1: false } });
        await expect(res.settled).rejects.toThrow('Self-run competitions have no check-in.');
        expect(patches.map((e) => e.data.participants)).toEqual([[{ id: 'p1', checkedIn: true }], [{ id: 'p1', checkedIn: false }]]);
    });

    it('a refusal puts back the value a later queued tap holds, not `was`', async () => {
        let n = 0;
        server(() => (++n === 1 ? [400, { error: 'bad' }] : new Promise(() => {})));
        const patches = patchesHeard();
        const first = await API.toggleCheckIn('c1', 'p1', true, 'pw', { was: { p1: false } });
        await API.toggleCheckIn('c1', 'p2', true, 'pw');
        await API.toggleCheckIn('c1', 'p1', true, 'pw', { was: { p1: true } });
        await expect(first.settled).rejects.toThrow();
        expect(patches.at(-1).data.participants).toEqual([{ id: 'p1', checkedIn: true }]);
    });

    it('every fetch of the competition lays the queued check-ins over its players', async () => {
        await API.toggleCheckIn('c1', 'p1', true, 'pw');
        await API.toggleCheckIn('c1', 'p2', false, 'pw');
        global.fetch = vi.fn((url) => {
            if (isClock(url)) return Promise.reject(new TypeError('clock'));
            if (String(url).endsWith('/api/viewer/competitions')) {
                return respond(200, [{ id: 'c1', name: 'C1', players: [{ id: 'p1', name: 'A', checkedIn: false }, { id: 'p2', name: 'B', checkedIn: true }, { id: 'p3', name: 'C', checkedIn: false }] }]);
            }
            if (String(url).endsWith('/api/viewer/competitions/c1')) {
                return respond(200, { config: { id: 'c1', players: [{ id: 'p1', name: 'A', checkedIn: false }] } });
            }
            return Promise.reject(new TypeError('offline'));
        });
        const [c] = await API.fetchCompetitions();
        expect(c.players.map((p) => [p.id, p.checkedIn])).toEqual([['p1', true], ['p2', false], ['p3', false]]);
        const detail = await API.fetchCompetitionDetails('c1');
        expect(detail.config.players[0].checkedIn).toBe(true);
    });

    it('a bulk check-in shows every id at once and adopts the superseded ones as stored', async () => {
        server(() => [200, { checkedIn: 1, alreadyCheckedIn: 0, notFound: [], superseded: [{ id: 'p2', name: 'Kai', checkedIn: false }] }]);
        const alerts = alertsHeard();
        const patches = patchesHeard();
        const res = await API.bulkCheckIn('c1', ['p1', 'p2', ''], 'pw');
        expect(baseOf(res.key)).toBe('checkin:c1');
        expect(patches[0].data.participants).toEqual([{ id: 'p1', checkedIn: true }, { id: 'p2', checkedIn: true }]);
        await expect(res.settled).resolves.toMatchObject({ checkedIn: 1 });
        await tick(0);
        expect(patches.at(-1).data.participants).toEqual([{ id: 'p2', checkedIn: false }]);
        expect(alerts.filter((a) => a.kind === 'checkin_superseded').map((a) => a.detail)).toEqual(['Kai was changed on another desk: now not checked in.']);
    });

    it('queuedWritesOf counts this competition\'s queued writes of the kinds asked, and nothing else', async () => {
        server(() => new Promise(() => {}));
        await API.toggleCheckIn('c1', 'p1', true, 'pw');
        await API.bulkCheckIn('c1', ['p2'], 'pw');
        await API.toggleCheckIn('c2', 'p9', true, 'pw');
        expect(API.queuedWritesOf('c1', API.CHECK_IN_WRITE_KINDS)).toBe(2);
        expect(API.queuedWritesOf('c2', API.CHECK_IN_WRITE_KINDS)).toBe(1);
        expect(API.queuedWritesOf('c1', ['time'])).toBe(0);
        expect(API.queuedWritesOf(null, new Set(['checkin']))).toBe(2);
        // A check-in changes nothing a start check judges.
        expect(API.startCheckUntrusted('c1')).toBe(false);
    });

    it('the held-writes list names the check-in\'s participants', async () => {
        await API.toggleCheckIn('c1', 'p1', true, 'pw');
        await tick(0);
        const [item] = API.heldWrites();
        expect(item.kind).toBe('checkin');
        expect(item.participantIds).toEqual(['p1']);
    });
});

describe('court calls (A1)', () => {
    it('a call answers at once with the id the device made, under its own base', async () => {
        server(() => new Promise(() => {}));
        const res = await API.sendAnnouncement('Shiaijo A: Ren Abe', 5, 'pw');
        expect(res.pending).toBe(true);
        expect(res.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
        expect(baseOf(res.key)).toBe(`announce:${res.id}`);
        const [[, d]] = stored();
        expect(d.payload).toMatchObject({ id: res.id, message: 'Shiaijo A: Ren Abe', durationMinutes: 5 });
        expect(d.payload.modifiedAt).toBeGreaterThan(0);
    });

    it('settles with the call as stored', async () => {
        server((r) => [200, { id: r.body.id, message: r.body.message }]);
        const res = await API.sendAnnouncement('Call', 5, 'pw');
        await expect(res.settled).resolves.toEqual({ id: res.id, message: 'Call' });
    });

    it('a withdrawal made while the call is still queued takes both out: nothing is sent', async () => {
        const send = await API.sendAnnouncement('Call', 5, 'pw');
        await tick(0);
        const sent = server(() => [200, {}]);
        const gone = await API.deleteAnnouncement(send.id, 'pw');
        await expect(gone.settled).resolves.toBe(true);
        await expect(send.settled).resolves.toEqual({ discarded: true, withdrawn: true });
        window.dispatchEvent(new Event('online'));
        await tick(60_000);
        expect(sent).toEqual([]);
        expect(stored()).toEqual([]);
    });

    it('a withdrawal made while the call is out goes after it, and a 404 is done', async () => {
        let release;
        const sent = server((r) => (r.method === 'POST'
            ? new Promise((ok) => { release = () => ok({ ok: true, status: 200, json: async () => ({ id: r.body.id }) }); })
            : [404, { error: 'Announcement not found' }]));
        const send = await API.sendAnnouncement('Call', 5, 'pw');
        await tick(0);
        const gone = await API.deleteAnnouncement(send.id, 'pw');
        await tick(0);
        expect(sent.map((r) => r.method)).toEqual(['POST']);
        release();
        await expect(gone.settled).resolves.toBe(true);
        expect(sent.map((r) => `${r.method} ${r.url}`)).toEqual(['POST /api/tournament/announce', `DELETE /api/announcements/${send.id}`]);
    });

    it('one call the server keeps refusing holds back no other call', async () => {
        const sent = server((r) => (r.body && r.body.message === 'bad' ? [500, {}] : [200, { id: r.body.id }]));
        await API.sendAnnouncement('bad', 5, 'pw');
        const other = await API.sendAnnouncement('good', 5, 'pw');
        await expect(other.settled).resolves.toEqual({ id: other.id });
        expect(sent.some((r) => r.body.message === 'good')).toBe(true);
    });

    it('a clear is stamped, under its own base, and settles with the calls left', async () => {
        const sent = server(() => [200, []]);
        const res = await API.clearAnnouncements('pw');
        expect(baseOf(res.key)).toBe('announce-clear');
        await expect(res.settled).resolves.toEqual([]);
        expect(sent[0].method).toBe('DELETE');
        expect(sent[0].url).toBe('/api/announcements');
        expect(sent[0].body.modifiedAt).toBeGreaterThan(0);
    });

    it('a call refused for clock_skew is sent again', async () => {
        let n = 0;
        const sent = server((r) => (++n === 1 ? [200, { applied: false, reason: 'clock_skew' }] : [200, { id: r.body.id }]));
        const res = await API.sendAnnouncement('Call', 5, 'pw');
        await tick(0);
        await expect(res.settled).resolves.toEqual({ id: res.id });
        expect(sent).toHaveLength(2);
    });
});

describe('the tournament settings (G4b)', () => {
    const settings = (over = {}) => ({ name: 'Cup', venue: 'Hall', tournamentVersion: 'v0', password: 'pw', competitions: [{ id: 'c1' }], ...over });

    it('a save keeping the password is an outbox entry whose body carries no password and no competitions', async () => {
        server(() => new Promise(() => {}));
        const res = await API.updateTournament(settings(), 'pw');
        expect(res.pending).toBe(true);
        expect(baseOf(res.key)).toBe('tournament');
        const [[, d]] = stored();
        expect(d.method).toBe('PUT');
        expect(d.url).toBe('/api/tournament');
        expect(d.payload.password).toBe('');
        expect(d.payload.competitions).toBeUndefined();
        expect(d.payload.tournamentVersion).toBe('v0');
    });

    it('a save that changes the password is sent directly and never stored', async () => {
        const sent = server(() => [200, { name: 'Cup', tournamentVersion: 'v1' }]);
        const saved = await API.updateTournament(settings({ password: 'new-secret' }), 'pw');
        expect(saved).toEqual({ name: 'Cup', tournamentVersion: 'v1' });
        expect(stored()).toEqual([]);
        expect(sent[0].body.password).toBe('new-secret');
        expect(sent[0].body.tournamentVersion).toBe('v0');
    });

    it('a save made while one is queued takes its place, keeping the version it was made against', async () => {
        await API.updateTournament(settings({ venue: 'A' }), 'pw');
        // Offline: the first save's send failed and it waits, not out.
        await tick(0);
        const second = await API.updateTournament(settings({ venue: 'B', tournamentVersion: 'v9' }), 'pw');
        const entries = stored();
        expect(entries).toHaveLength(1);
        expect(entries[0][0]).toBe(second.key);
        expect(entries[0][1].payload).toMatchObject({ venue: 'B', tournamentVersion: 'v0' });
    });

    it('a save queued behind one in flight takes the version the landed save answered', async () => {
        let release;
        const sent = server((r) => (r.body.venue === 'A'
            ? new Promise((ok) => { release = () => ok({ ok: true, status: 200, json: async () => ({ tournamentVersion: 'v1' }) }); })
            : [200, { tournamentVersion: 'v2' }]));
        const first = await API.updateTournament(settings({ venue: 'A' }), 'pw');
        await tick(0);
        const second = await API.updateTournament(settings({ venue: 'B' }), 'pw');
        release();
        await expect(first.settled).resolves.toEqual({ tournamentVersion: 'v1' });
        await expect(second.settled).resolves.toEqual({ tournamentVersion: 'v2' });
        expect(sent.map((r) => r.body.tournamentVersion)).toEqual(['v0', 'v1']);
        // A save made later from the copy the first save replaced takes it too.
        server(() => new Promise(() => {}));
        await API.updateTournament(settings({ venue: 'C' }), 'pw');
        expect(stored()[0][1].payload.tournamentVersion).toBe('v2');
    });

    it('a stale save rejects with the current copy; one queued behind it at that version goes with it, told once', async () => {
        let release;
        server(() => new Promise((ok) => {
            release = () => ok({ ok: false, status: 409, json: async () => ({ code: 'tournament_changed', error: 'The tournament settings changed, reload them.', tournament: { name: 'Theirs', tournamentVersion: 'v5' } }) });
        }));
        const alerts = alertsHeard();
        const first = await API.updateTournament(settings({ venue: 'A' }), 'pw');
        await tick(0);
        const second = await API.updateTournament(settings({ venue: 'B' }), 'pw');
        release();
        const err = await first.settled.catch((e) => e);
        expect(err.code).toBe('tournament_changed');
        expect(err.tournament).toEqual({ name: 'Theirs', tournamentVersion: 'v5' });
        await expect(second.settled).rejects.toBe(err);
        await tick(0);
        const rejected = alerts.filter((a) => a.kind === 'rejected');
        expect(rejected).toHaveLength(1);
        expect(rejected[0].code).toBe('tournament_changed');
        expect(stored()).toEqual([]);
    });

    it('a fetch of the tournament shows the save still queued', async () => {
        await API.updateTournament(settings({ venue: 'Queued hall' }), 'pw');
        global.fetch = vi.fn((url) => (String(url).endsWith('/api/viewer/tournament')
            ? respond(200, { name: 'Cup', venue: 'Stored hall', tournamentVersion: 'v0' })
            : Promise.reject(new TypeError('offline'))));
        const t = await API.fetchTournament();
        expect(t.venue).toBe('Queued hall');
        expect(t.tournamentVersion).toBe('v0');
        expect(t.password).toBeUndefined();
    });
});

describe('the fighting-spirit awards (G4b)', () => {
    const list = [{ title: 'Spirit', recipientName: 'Ren Abe' }];

    it('a save is an outbox entry under the competition\'s awards base', async () => {
        server(() => new Promise(() => {}));
        const res = await API.updateCompetitionAwards('c1', list, 'pw', 'a0');
        expect(baseOf(res.key)).toBe('awards:c1');
        expect(stored()[0][1].payload).toMatchObject({ fightingSpiritAwards: list, awardsVersion: 'a0' });
    });

    it('a stale save rejects with the list now and its version', async () => {
        server(() => [409, { code: 'awards_changed', error: 'The awards changed, reload them.', awardsVersion: 'a5', fightingSpiritAwards: [{ title: 'X' }] }]);
        const res = await API.updateCompetitionAwards('c1', list, 'pw', 'a0');
        const err = await res.settled.catch((e) => e);
        expect(err.code).toBe('awards_changed');
        expect(err.awardsVersion).toBe('a5');
        expect(err.fightingSpiritAwards).toEqual([{ title: 'X' }]);
    });

    it('the competition detail shows the list still queued', async () => {
        await API.updateCompetitionAwards('c1', list, 'pw', 'a0');
        global.fetch = vi.fn((url) => (String(url).endsWith('/api/viewer/competitions/c1')
            ? respond(200, { config: { id: 'c1', fightingSpiritAwards: [] }, awardsVersion: 'a0' })
            : Promise.reject(new TypeError('offline'))));
        const detail = await API.fetchCompetitionDetails('c1');
        expect(detail.config.fightingSpiritAwards).toEqual(list);
        expect(detail.config.awardsVersion).toBe('a0');
    });
});
