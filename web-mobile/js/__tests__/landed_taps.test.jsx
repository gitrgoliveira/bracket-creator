// landed_taps.jsx, the one owner of the landed-tap record (stage 6 review 7).
import { describe, it, expect } from 'vitest';
import {
    createLandedTaps, landedTapsKey, LANDED_TAPS_LEGACY_KEY, LANDED_TABS_KEY, LANDED_TAB_LIVE_MS, LANDED_TAPS_PER_MATCH,
    LANDED_TAPS_INDEX, LANDED_TAPS_MAX,
} from '../landed_taps.jsx';

function fakeStorage() {
    const data = {};
    let refuse = false;
    const writes = [];
    return {
        data, writes,
        refuse(v) { refuse = v; },
        getItem: (k) => (k in data ? data[k] : null),
        setItem: (k, v) => {
            if (refuse) throw new Error('QuotaExceededError');
            writes.push([k, String(v).length]);
            data[k] = String(v);
        },
        removeItem: (k) => { delete data[k]; },
        keys: () => Object.keys(data),
    };
}

// The characters the landed-tap record holds in storage.
const landedChars = (storage) => Object.entries(storage.data)
    .filter(([k]) => k.startsWith('bc_landed_taps'))
    .reduce((n, [k, v]) => n + k.length + v.length, 0);
const landedEntries = (storage) => Object.entries(storage.data)
    .filter(([k]) => k.startsWith('bc_landed_taps_v2:'))
    .reduce((n, [, v]) => n + Object.keys(JSON.parse(v)).length, 0);

const rec = (id) => ({ id, where: 'Pool A · Match 1', kind: 'ippon.add', side: 'a', pos: 1, letter: 'M' });

describe('landed taps', () => {
    it('one tab tells a tap: the one that noted it, else the live tab with the lowest id', () => {
        const storage = fakeStorage();
        let t = 1_000;
        const now = () => t;
        const b = createLandedTaps({ storage, now, tab: 'b' });
        const a = createLandedTaps({ storage, now, tab: 'a' });
        const c = createLandedTaps({ storage, now, tab: 'c' });
        b.note('c1', 'm1', [rec('x'), rec('y')]);
        expect(a.claim('c1', 'm1', ['x'])).toEqual([]);
        expect(c.claim('c1', 'm1', ['x'])).toEqual([]);
        expect(b.claim('c1', 'm1', ['x']).map((r) => r.id)).toEqual(['x']);
        expect(b.claim('c1', 'm1', ['x']), 'told once').toEqual([]);
        // b goes: the lowest live tab, a, tells y; c never does.
        b.leave();
        expect(c.claim('c1', 'm1', ['y'])).toEqual([]);
        expect(a.claim('c1', 'm1', ['y']).map((r) => r.id)).toEqual(['y']);
        // A tab whose heartbeat stopped (a crash) is gone after LANDED_TAB_LIVE_MS.
        a.note('c1', 'm2', [rec('z')]);
        t += LANDED_TAB_LIVE_MS + 1;
        c.beat();
        expect(c.claim('c1', 'm2', ['z']).map((r) => r.id), 'c is the only live tab').toEqual(['z']);
    });

    it('keeps in memory what storage refuses, and never puts back what another tab removed', () => {
        const storage = fakeStorage();
        const a = createLandedTaps({ storage, tab: 'a' });
        storage.refuse(true);
        a.note('c1', 'm1', [rec('x')]);
        expect(a.has('c1', 'm1', 'x'), 'kept in memory').toBe(true);
        storage.refuse(false);
        a.note('c1', 'm1', [rec('y')]);
        expect(storage.data[landedTapsKey('c1', 'm1')]).toContain('"x"');
        // Another tab's sign-out removes the record: not put back.
        delete storage.data[landedTapsKey('c1', 'm1')];
        expect(a.has('c1', 'm1', 'x')).toBe(false);
        a.note('c1', 'm1', [rec('w')]);
        expect(storage.data[landedTapsKey('c1', 'm1')]).not.toContain('"x"');
    });

    it("a sign-out takes this tab's taps and a gone tab's, never another live tab's", () => {
        const storage = fakeStorage();
        let t = 1_000;
        const now = () => t;
        const a = createLandedTaps({ storage, now, tab: 'a' });
        const b = createLandedTaps({ storage, now, tab: 'b' });
        const gone = createLandedTaps({ storage, now, tab: 'g' });
        a.note('c1', 'm1', [rec('x')]);
        b.note('c1', 'm1', [rec('y')]);
        b.note('c2', 'm9', [rec('z')]);
        gone.note('c1', 'm2', [rec('w')]);
        gone.leave();
        a.forget();
        expect(a.has('c1', 'm1', 'x'), "this tab's").toBe(false);
        expect(a.has('c1', 'm2', 'w'), "a gone tab's, which this tab would tell").toBe(false);
        expect(b.has('c1', 'm1', 'y'), "another live tab's stays for it").toBe(true);
        expect(b.has('c2', 'm9', 'z')).toBe(true);
        b.forget();
        expect(Object.keys(storage.data).filter((k) => k.startsWith('bc_landed_taps'))).toEqual([]);
    });

    // m10: the index key can be lost (two tabs racing, a refused write), and a
    // record it no longer lists must still go at a sign-out.
    it('a sign-out reaches a match record the index lost', () => {
        const storage = fakeStorage();
        const a = createLandedTaps({ storage, tab: 'a' });
        a.note('c1', 'm1', [rec('x')]);
        a.note('c1', 'm2', [rec('y')]);
        delete storage.data[LANDED_TAPS_INDEX];
        a.forget();
        expect(Object.keys(storage.data).filter((k) => k.startsWith('bc_landed_taps'))).toEqual([]);
    });

    // M3: the record grew without bound (a match's entries were pruned only
    // when that match was noted again: about 392 K characters a day). Six days
    // of 100 matches of 12 taps a day: it levels off at about a day's taps.
    it('levels off: a day of taps at most, whatever the days before held', () => {
        const storage = fakeStorage();
        let t = 1_760_000_000_000;
        const a = createLandedTaps({ storage, now: () => t, tab: 'a' });
        const perDay = [];
        for (let day = 0; day < 6; day++) {
            for (let m = 0; m < 100; m++) {
                for (let i = 0; i < 12; i++) {
                    t += 1000;
                    a.note('c1', `Pool ${day}-${m}`, [rec(`${'0'.repeat(36)}.${day}.${m}.${i}`)]);
                }
            }
            t += 12 * 3600 * 1000;
            a.beat();
            perDay.push(landedChars(storage));
        }
        expect(landedEntries(storage)).toBeLessThanOrEqual(1200);
        expect(perDay[5], `characters held after each day: ${perDay}`).toBeLessThanOrEqual(perDay[1]);
        expect(perDay[5]).toBeLessThan(300_000);
    });

    it('keeps at most LANDED_TAPS_MAX in all, the latest', () => {
        const storage = fakeStorage();
        let t = 1_000_000;
        const a = createLandedTaps({ storage, now: () => ++t, tab: 'a' });
        for (let m = 0; m < 300; m++) {
            a.note('c1', `m${m}`, Array.from({ length: 10 }, (_, i) => rec(`t${m}-${i}`)));
        }
        a.sweep();
        expect(landedEntries(storage)).toBe(LANDED_TAPS_MAX);
        expect(a.has('c1', 'm299', 't299-9'), 'the latest kept').toBe(true);
        expect(a.has('c1', 'm0', 't0-0'), 'the oldest gone').toBe(false);
        expect(storage.data[LANDED_TAPS_INDEX], 'the index lists only what is held').not.toContain('"bc_landed_taps_v2:c1\u001fm0"');
    });

    it("takes an older page's single record over, as this tab's, once", () => {
        const storage = fakeStorage();
        storage.data[LANDED_TAPS_LEGACY_KEY] = JSON.stringify({ x: { compID: 'c1', matchID: 'm1', at: Date.now(), kind: 'ippon.add' } });
        const a = createLandedTaps({ storage, tab: 'a' });
        expect(storage.data[LANDED_TAPS_LEGACY_KEY]).toBeUndefined();
        expect(a.claim('c1', 'm1', ['x']).map((r) => r.id)).toEqual(['x']);
        expect(JSON.parse(storage.data[LANDED_TABS_KEY])).toHaveProperty('a');
    });

    // Minor 14: a batch's answer read and rewrote the device's whole record
    // (about 170 KB at 1000 taps). Now it writes its own match's record:
    // measured here at 1000 taps landed over 50 matches.
    it("a batch's answer writes its own match's record only, however many taps the device landed", () => {
        const storage = fakeStorage();
        const a = createLandedTaps({ storage, tab: 'a' });
        for (let m = 0; m < 50; m++) {
            a.note('c1', `m${m}`, Array.from({ length: 20 }, (_, i) => rec(`${'0'.repeat(30)}-${m}-${i}`)));
        }
        const total = Object.entries(storage.data).filter(([k]) => k.startsWith('bc_landed_taps_v2:')).reduce((n, [, v]) => n + v.length, 0);
        storage.writes.length = 0;
        a.note('c1', 'm7', [rec('fresh')]);
        const written = storage.writes.reduce((n, [, len]) => n + len, 0);
        expect(total).toBeGreaterThan(100_000);
        expect(written, `bytes written for one batch, of ${total} held`).toBeLessThan(total / 40);
    });

    it('keeps at most LANDED_TAPS_PER_MATCH of a match, the latest', () => {
        const storage = fakeStorage();
        let t = 0;
        const a = createLandedTaps({ storage, now: () => ++t, tab: 'a' });
        a.note('c1', 'm1', Array.from({ length: LANDED_TAPS_PER_MATCH + 3 }, (_, i) => rec(`t${i}`)));
        a.note('c1', 'm1', [rec('last')]);
        const obj = JSON.parse(storage.data[landedTapsKey('c1', 'm1')]);
        expect(Object.keys(obj)).toHaveLength(LANDED_TAPS_PER_MATCH);
        expect(obj).toHaveProperty('last');
    });
});
